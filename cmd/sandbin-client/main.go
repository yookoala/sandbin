// Command sandbin-client uploads files to a sandbin bin so an agent can hand a human
// plain-text preview URLs. See README.md.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

const (
	defaultURL   = "http://localhost:8080"
	maxBodyBytes = 1 << 20 // the same 1 MiB cap the server puts on a PUT body
	maxPathLen   = 256
	maxPathSegs  = 16
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

// run is the whole client, so main is only the exit code and a test can hand it
// args, a stdin and writers. Failures are written to stderr and never to stdout.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("sandbin-client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("url", envOr("SANDBIN_URL", defaultURL), "sandbin server base URL")
	apiKey := fs.String("api-key", os.Getenv("SANDBIN_API_KEY"), "pre-shared API key to send as X-API-Key")
	binFlag := fs.String("bin", "", "an existing bin hash to upload into")
	newBin := fs.Bool("new", false, "create a fresh bin instead of reusing this directory's")
	nameFlag := fs.String("name", "", "the path to upload piped-in stdin as")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// The usage has already been printed; asking for it is not a failure.
			return nil
		}
		return err
	}

	defer func() {
		if err == nil {
			return
		}
		fmt.Fprintln(stderr, "sandbin-client:", err)
		var api *apiError
		if errors.As(err, &api) && api.status == http.StatusUnauthorized {
			fmt.Fprintln(stderr, "sandbin-client: set SANDBIN_BIN_PASSWORD to this bin's password")
		}
	}()

	if *binFlag != "" && *newBin {
		return errors.New("use either --bin or --new, not both")
	}
	// A run without the key would only charge this address a 30 minute block, so it
	// makes no request at all.
	if *apiKey == "" {
		return errors.New("no API key: pass --api-key or set SANDBIN_API_KEY")
	}

	// The bin password is env-only on purpose: no flag puts it on a command line.
	c, err := newClient(*baseURL, *apiKey, os.Getenv("SANDBIN_BIN_PASSWORD"))
	if err != nil {
		return err
	}

	names, stdinName, err := uploadTargets(fs.Args(), *nameFlag, stdin)
	if err != nil {
		return err
	}
	// stdin is read before the bin is chosen, so an oversized stream makes no
	// request and leaves no bin behind.
	var stdinBody []byte
	if stdinName != "" {
		stdinBody, err = readStdin(stdin)
		if err != nil {
			return err
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	bin, protected, err := resolveBin(c, dir, *binFlag, *newBin)
	if err != nil {
		return err
	}
	if protected && c.password == "" {
		fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
	}

	// Lines are held until every upload has succeeded, so a failed run prints
	// nothing on stdout.
	lines := make([]string, 0, len(names)+2)
	for _, name := range names {
		created, err := uploadFile(c, bin, name)
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, c.fileURL(bin, name)))
	}
	if stdinName != "" {
		created, err := c.putFile(bin, stdinName, bytes.NewReader(stdinBody))
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, c.fileURL(bin, stdinName)))
	}

	lines = append(lines, c.binURL(bin))
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// resolveBin picks the bin to upload into: --bin, then this directory's state file
// when its url matches the url in use, then a new bin written to the state file so
// the next run reuses it. --new skips straight to a new bin.
func resolveBin(c *client, dir, binFlag string, newBin bool) (string, bool, error) {
	if binFlag != "" {
		hash, err := cleanBinHash(binFlag)
		return hash, false, err
	}

	base := c.base.String()
	if !newBin {
		state, found, err := readState(dir)
		if err != nil {
			return "", false, err
		}
		if found && state.URL == base {
			return state.Bin, state.Protected, nil
		}
	}

	hash, err := c.createBin()
	if err != nil {
		return "", false, err
	}
	protected := c.password != ""
	if err := writeState(dir, binState{URL: base, Bin: hash, Protected: protected}); err != nil {
		return "", false, err
	}
	return hash, protected, nil
}

// uploadTargets cleans every upload name and checks each local file exists and fits
// the 1 MiB body cap, all before the first request, so a bad argument cannot leave
// half a prototype uploaded. The cleaned stdin name is returned separately.
func uploadTargets(args []string, nameFlag string, stdin io.Reader) ([]string, string, error) {
	if len(args) == 0 && nameFlag == "" {
		if isTerminal(stdin) {
			return nil, "", errors.New("no files to upload: pass file arguments or pipe stdin with --name")
		}
		return nil, "", errors.New("stdin needs --name to say what it uploads")
	}

	var names []string
	for _, arg := range args {
		name, err := cleanUploadName(arg)
		if err != nil {
			return nil, "", fmt.Errorf("%q: %v", arg, err)
		}
		info, err := os.Stat(name)
		if err != nil {
			return nil, "", err
		}
		if info.IsDir() {
			return nil, "", fmt.Errorf("%s: is a directory, not a file", name)
		}
		if info.Size() > maxBodyBytes {
			return nil, "", fmt.Errorf("%s: body over 1 MiB", name)
		}
		names = append(names, name)
	}
	if nameFlag == "" {
		return names, "", nil
	}

	stdinName, err := cleanUploadName(nameFlag)
	if err != nil {
		return nil, "", fmt.Errorf("--name %q: %v", nameFlag, err)
	}
	return names, stdinName, nil
}

// cleanUploadName canonicalises an upload name the way the server reads it and
// refuses what the server would refuse, with the server's own wording, so a bad
// name fails before a request. The server stays the authority.
func cleanUploadName(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty path")
	}
	if len(raw) > maxPathLen {
		return "", errors.New("path too long")
	}
	if strings.ContainsRune(raw, '\\') {
		return "", errors.New("path may not contain backslashes")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("path may not contain control characters")
		}
	}
	if strings.HasPrefix(raw, "/") {
		return "", errors.New("path must be relative")
	}
	// The ".." check has to see the segments the caller wrote, because path.Clean
	// would eat them along with "./", "//" and a trailing slash.
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", errors.New("path must not escape the bin")
		}
	}
	name := path.Clean(strings.TrimPrefix(raw, "./"))
	if name == "." || name == "" {
		return "", errors.New("empty path")
	}
	if segs := strings.Split(name, "/"); len(segs) > maxPathSegs {
		return "", errors.New("path has too many segments")
	}
	return name, nil
}

// cleanBinHash accepts a --bin hash, and tolerates the "/abc1234/" shape a created
// bin was printed as.
// shortcut: --bin names a hash, not a full URL; paste the hash, not the printed line.
func cleanBinHash(raw string) (string, error) {
	notABin := fmt.Errorf("--bin %q is not a bin hash", raw)
	hash := strings.Trim(raw, "/")
	if hash == "" || strings.Contains(hash, "/") {
		return "", notABin
	}
	for _, r := range hash {
		if r < 0x20 || r == 0x7f {
			return "", notABin
		}
	}
	return hash, nil
}

// uploadFile PUTs one local file under the bin.
func uploadFile(c *client, bin, name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return c.putFile(bin, name, f)
}

// readStdin takes the piped-in body, capped at the same 1 MiB the server enforces.
func readStdin(stdin io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(stdin, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %v", err)
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("stdin over 1 MiB")
	}
	return body, nil
}

func resultLine(created bool, url string) string {
	if created {
		return "created " + url
	}
	return "updated " + url
}

// isTerminal reports whether stdin is an interactive terminal instead of a pipe or
// a redirected file. A character device such as /dev/null reads as a terminal here,
// which is the honest answer: it carries no upload.
func isTerminal(stdin io.Reader) bool {
	f, ok := stdin.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
