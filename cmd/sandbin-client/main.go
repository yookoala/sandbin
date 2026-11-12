// Command sandbin-client uploads files to a sandbin bin so an agent can hand a human
// plain-text preview URLs. See README.md.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	defaultURL   = "http://localhost:8080"
	maxBodyBytes = 1 << 20 // the same 1 MiB cap the server puts on a PUT body
	maxPathLen   = 256
	maxPathSegs  = 16
	// The same caps the server puts on the two metadata headers.
	maxTitleBytes       = 200
	maxDescriptionBytes = 2000
	// The same caps the server puts on an extracted archive.
	maxArchiveEntries = 1000
	maxExtractTotal   = 32 << 20
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
	titleFlag := fs.String("title", "", "single-line title to give the bin, sent as X-Bin-Title")
	descriptionFlag := fs.String("description", "", "single-line description to give the bin, sent as X-Bin-Description")
	describe := fs.Bool("describe", false, "print this bin's title and description instead of uploading")
	archiveFlag := fs.String("archive", "", "upload this folder as one tar.gz the server unpacks into the bin")
	noExtract := fs.Bool("no-extract", false, "send every upload with X-Extract, so archive bodies are stored raw")
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
	// A metadata flag that was given says something about the bin's fields, even when the
	// value it carries is the empty string.
	titleGiven := flagGiven(fs, "title")
	descriptionGiven := flagGiven(fs, "description")
	archiveGiven := flagGiven(fs, "archive")
	// --describe reads one bin and writes nothing, so the flags that write have no place in it.
	if *describe {
		if *newBin {
			return errors.New("--describe reads the bin this run would reuse: --new makes a fresh one")
		}
		if fs.NArg() > 0 || *nameFlag != "" || archiveGiven {
			return errors.New("--describe uploads nothing: pass no file arguments, no --name and no --archive")
		}
		if titleGiven || descriptionGiven {
			return errors.New("--describe only reads: set --title and --description on a run that writes")
		}
	}
	// Metadata is checked before any request, the way an upload name is, because argv can
	// carry a newline the transport would not carry.
	if err := cleanMetadata("title", *titleFlag, maxTitleBytes); err != nil {
		return err
	}
	if err := cleanMetadata("description", *descriptionFlag, maxDescriptionBytes); err != nil {
		return err
	}
	// A write run without the key would only charge this address a 30 minute block, so it
	// makes no request at all. --describe writes nothing and needs no key.
	if !*describe && *apiKey == "" {
		return errors.New("no API key: pass --api-key or set SANDBIN_API_KEY")
	}

	// The bin password is env-only on purpose: no flag puts it on a command line.
	c, err := newClient(*baseURL, *apiKey, os.Getenv("SANDBIN_BIN_PASSWORD"))
	if err != nil {
		return err
	}
	c.noExtract = *noExtract

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	if *describe {
		bin, protected, err := resolveExistingBin(c, dir, *binFlag)
		if err != nil {
			return err
		}
		if protected && c.password == "" {
			fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
		}
		title, description, err := c.describeBin(bin)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "title: %s\ndescription: %s\n", title, description)
		return nil
	}

	// A run with no upload targets is refused, unless it came to say what the bin holds:
	// a metadata flag is a job of its own, and --archive brings its own target.
	metaRun := titleGiven || descriptionGiven

	var names []string
	var stdinName string
	if fs.NArg() > 0 || *nameFlag != "" || (!archiveGiven && !metaRun) {
		names, stdinName, err = uploadTargets(fs.Args(), *nameFlag, stdin)
		if err != nil {
			return err
		}
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

	// --archive is checked and built before the bin is chosen too: a folder that
	// cannot be uploaded makes no request and leaves no bin behind. The temp file
	// is removed on every exit path, including an upload failure.
	var archiveFile, archiveName string
	if archiveGiven {
		folder, err := cleanArchiveFolder(*archiveFlag)
		if err != nil {
			return err
		}
		archiveFile, err = buildArchive(folder)
		if err != nil {
			return err
		}
		defer os.Remove(archiveFile)
		archiveName = path.Base(folder) + ".tar.gz"
	}

	bin, protected, justCreated, err := resolveBin(c, dir, *binFlag, *newBin, *titleFlag, *descriptionFlag)
	if err != nil {
		return err
	}
	if protected && c.password == "" {
		fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
	}

	// Lines are held until every upload has succeeded, so a failed run prints
	// nothing on stdout.
	lines := make([]string, 0, len(names)+2)
	if metaRun && !justCreated {
		// A bin this run did not create takes its metadata as one PATCH before any upload,
		// so a later upload failure still leaves the bin saying what it was meant to hold.
		// A given flag replaces its field, and the empty value one carries clears it.
		var titlePatch, descriptionPatch *string
		if titleGiven {
			titlePatch = titleFlag
		}
		if descriptionGiven {
			descriptionPatch = descriptionFlag
		}
		if err := c.patchBinMeta(bin, titlePatch, descriptionPatch); err != nil {
			return err
		}
		lines = append(lines, "updated /"+bin+"/")
	}
	for _, name := range names {
		created, extracted, err := uploadFile(c, bin, name)
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, c.fileURL(bin, name)))
	}
	if stdinName != "" {
		created, extracted, err := c.putFile(bin, stdinName, bytes.NewReader(stdinBody))
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, c.fileURL(bin, stdinName)))
	}
	if archiveFile != "" {
		// The folder archive uploads last, after the file arguments and stdin of the
		// same run. --no-extract sends the opt-out header, so there the archive
		// itself is what lands in the bin.
		f, err := os.Open(archiveFile)
		if err != nil {
			return err
		}
		defer f.Close()
		created, extracted, err := c.putFile(bin, archiveName, f)
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, c.fileURL(bin, archiveName)))
	}

	lines = append(lines, c.binURL(bin))
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// flagGiven reports whether a flag appeared on the command line, which is the difference
// between "set this field to the empty string" and "say nothing about it". FlagSet.Visit only
// visits the flags a run actually set.
func flagGiven(fs *flag.FlagSet, name string) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

// resolveBin picks the bin to upload into: --bin, then this directory's state file
// when its url matches the url in use, then a new bin written to the state file so
// the next run reuses it. --new skips straight to a new bin. A bin made here takes its
// metadata in the creation POST, and justCreated is what tells the caller no second
// request is needed for it.
func resolveBin(c *client, dir, binFlag string, newBin bool, title, description string) (string, bool, bool, error) {
	if binFlag != "" {
		hash, err := cleanBinHash(binFlag)
		return hash, false, false, err
	}

	base := c.base.String()
	if !newBin {
		state, found, err := readState(dir)
		if err != nil {
			return "", false, false, err
		}
		if found && state.URL == base {
			return state.Bin, state.Protected, false, nil
		}
	}

	hash, err := c.createBin(title, description)
	if err != nil {
		return "", false, false, err
	}
	protected := c.password != ""
	if err := writeState(dir, binState{URL: base, Bin: hash, Protected: protected}); err != nil {
		// The bin exists on the server now, so the run must report its hash: rerunning
		// would only create a second bin, not recover this one.
		return "", false, false, fmt.Errorf("created bin %s but could not save it: %v", hash, err)
	}
	return hash, protected, true, nil
}

// resolveExistingBin is the bin --describe reads: --bin, or this directory's state file when
// its url matches the url in use. A read-only run makes no bin, so a directory with neither
// gets an error naming what it needs.
func resolveExistingBin(c *client, dir, binFlag string) (string, bool, error) {
	if binFlag != "" {
		hash, err := cleanBinHash(binFlag)
		return hash, false, err
	}

	state, found, err := readState(dir)
	if err != nil {
		return "", false, err
	}
	if !found || state.URL != c.base.String() {
		return "", false, fmt.Errorf("--describe has no bin for this directory: pass --bin or upload something first")
	}
	return state.Bin, state.Protected, nil
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
	// --name with a terminal for stdin uploads whatever gets typed, or when nothing
	// is piped at all an empty file that a rerun would report as a success.
	if isTerminal(stdin) {
		return nil, "", fmt.Errorf("--name %q got no stdin: pipe the content in or pass a file argument", nameFlag)
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
	// The limit is measured on the cleaned name because that is what the server
	// receives: a "./" prefix must not eat into it here and not there.
	if len(name) > maxPathLen {
		return "", errors.New("path too long")
	}
	if segs := strings.Split(name, "/"); len(segs) > maxPathSegs {
		return "", errors.New("path has too many segments")
	}
	return name, nil
}

// cleanMetadata refuses a title or description the way the server refuses it, with the
// server's own wording, so a bad flag fails before a request. Bytes are what the cap counts,
// and a value typed into argv can carry a newline the transport would never carry.
func cleanMetadata(name, raw string, maxBytes int) error {
	if len(raw) > maxBytes {
		return fmt.Errorf("%s over %d bytes", name, maxBytes)
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s may not contain control characters", name)
		}
	}
	return nil
}

// cleanBinHash accepts a --bin hash, and tolerates the "/abc1234/" shape a created
// bin was printed as.
// shortcut: --bin names a hash, not a full URL; paste the hash, not the printed line.
func cleanBinHash(raw string) (string, error) {
	notABin := fmt.Errorf("--bin %q is not a bin hash", raw)
	hash := strings.Trim(raw, "/")
	if hash == "" || hash == "." || hash == ".." || strings.Contains(hash, "/") {
		return "", notABin
	}
	for _, r := range hash {
		if r < 0x20 || r == 0x7f {
			return "", notABin
		}
	}
	return hash, nil
}

// cleanArchiveFolder cleans the --archive folder name the way an upload name is
// cleaned and insists it names an existing directory relative to the current
// directory: the base path of a bin must name a folder, so "." and "./" and
// absolute paths are refused.
func cleanArchiveFolder(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || path.Clean(raw) == "." {
		return "", errors.New("--archive names a folder")
	}
	name, err := cleanUploadName(raw)
	if err != nil {
		return "", fmt.Errorf("--archive %q: %v", raw, err)
	}
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", name)
	}
	return name, nil
}

// buildArchive walks the folder with filepath.WalkDir, which is lexicographic so
// runs are deterministic, and refuses everything the server would refuse anyway:
// non-regular entries, files over the 1 MiB body cap, more than the 1000 entries
// an archive may hold, more total bytes than an extraction may unpack, entry
// paths the server's cleanPath would refuse, and a folder that holds no files at
// all. What survives goes into a fresh os.CreateTemp .tar.gz whose entries carry
// the folder's relative path verbatim, so the bin's paths come out exactly
// folder-relative. The archive file itself is capped at 1 MiB, the cap every PUT
// body obeys. On any failure the temp file is already removed; the caller removes
// it on success.
func buildArchive(folder string) (name string, err error) {
	tmp, err := os.CreateTemp("", "sandbin-archive-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil || name != tmpName {
			os.Remove(tmpName)
		}
	}()

	var files, total int64
	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(folder, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil // a bin's directories are path prefixes, not stored entries
		}
		entry := filepath.ToSlash(p)
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", entry)
		}
		if _, err := cleanUploadName(entry); err != nil {
			return fmt.Errorf("%s: %v", entry, err)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxBodyBytes {
			return fmt.Errorf("%s is over 1 MiB", entry)
		}
		files++
		if files > maxArchiveEntries {
			return errors.New("folder has over 1000 files")
		}
		total += info.Size()
		if total > maxExtractTotal {
			return errors.New("folder unpacks over 32 MiB")
		}
		hdr := &tar.Header{Name: entry, Typeflag: tar.TypeReg, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return "", err
	}
	if files == 0 {
		return "", errors.New("folder contains no files")
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	info, err := tmp.Stat()
	if err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if info.Size() > maxBodyBytes {
		return "", errors.New("archive is over 1 MiB")
	}
	name = tmpName
	return name, nil
}

// uploadFile PUTs one local file under the bin.
func uploadFile(c *client, bin, name string) (bool, int, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, 0, err
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

// resultLine is what one upload adds to stdout. An answer that carried
// X-Extracted-Count says how many files the server unpacked and where they live;
// the file URL it names is the archive's, and the extracted files sit under the
// archive name's directory in the bin.
func resultLine(created bool, extracted int, url string) string {
	if extracted > 0 {
		return fmt.Sprintf("extracted %d files into %s", extracted, url)
	}
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
