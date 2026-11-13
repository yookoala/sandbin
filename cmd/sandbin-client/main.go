// Command sandbin-client uploads files to a sandbin bin so an agent can hand a human
// plain-text preview URLs. See README.md.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
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
// args, a stdin and writers. Failures are reported by the verbs and never on stdout.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return newClientApp(stdin, stdout, stderr).Run(append([]string{"sandbin-client"}, reorderDeleteFlags(args)...))
}

// deleteValueFlags are the value-carrying flags delete defines; they are what may also
// be spelled after delete's file arguments.
var deleteValueFlags = map[string]bool{
	"url": true, "api-key": true, "bin": true,
	"title": true, "description": true, "folder": true,
}

// reorderDeleteFlags moves delete's flag tokens (and the values they carry) ahead of its
// file arguments: urfave stops flag parsing at the first positional argument, yet
// `delete <path> --folder <folder>` is the spelling a user reaches for first and must
// come out request-for-request like the flags-first spelling. An unknown --token moves
// to the flag side too, so misuse there stays a usage error instead of becoming a path
// to delete. Names after a standalone -- stay names where they were, and single-dash
// tokens are names (a dash-leading name goes after --), so they are left alone.
func reorderDeleteFlags(args []string) []string {
	if len(args) == 0 || args[0] != "delete" {
		return args
	}
	rest := args[1:]
	cut := -1
	for i, a := range rest {
		if a == "--" {
			cut = i
			break
		}
	}
	tail := []string{}
	if cut >= 0 {
		tail = rest[cut:]
		rest = rest[:cut]
	}
	flags, names := []string{}, []string{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if len(a) > 2 && strings.HasPrefix(a, "--") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && deleteValueFlags[a[2:]] && i+1 < len(rest) {
				i++
				flags = append(flags, rest[i])
			}
			continue
		}
		names = append(names, a)
	}
	out := append(append([]string{"delete"}, flags...), names...)
	return append(out, tail...)
}

// newClientApp builds the app and its verbs with the writers wired: apps are stateful,
// so a fresh one per run keeps every invocation independent. Writer takes --help and
// bare runs, ErrWriter takes the usage-error rendering; the verbs print their own
// operational failures.
func newClientApp(stdin io.Reader, stdout, stderr io.Writer) *cli.App {
	hook := usageError(stderr)
	return &cli.App{
		Name:         "sandbin-client",
		Usage:        "publish previews to a sandbin bin: upload files, describe the bin, take paths out, or init this directory",
		Version:      "dev",
		Writer:       stdout,
		ErrWriter:    stderr,
		OnUsageError: hook,
		// urfave's own cli.Exit errors (the "No help topic for ..." refusals) would otherwise
		// print through HandleExitCoder and os.Exit inside app.Run. Swallowing the exit here
		// lets run map the error to an exit code like every other failure. A verb's own
		// failures have already been printed by the verb, so only ExitCoder messages echo.
		ExitErrHandler: func(_ *cli.Context, err error) {
			if exitErr, ok := err.(cli.ExitCoder); ok && exitErr.Error() != "" {
				fmt.Fprintln(stderr, exitErr.Error())
			}
		},
		Commands: []*cli.Command{
			uploadCommand(stdin, stdout, stderr, hook),
			initCommand(stdin, stdout, stderr, hook),
			deleteCommand(stdout, stderr, hook),
			describeCommand(stdout, stderr, hook),
		},
	}
}

// usageError renders urfave's usage failures onto stderr: v2's default renderer writes them
// to the app's stdout Writer, and a misuse is not a help request.
func usageError(stderr io.Writer) cli.OnUsageErrorFunc {
	return func(c *cli.Context, err error, isCommand bool) error {
		fmt.Fprintf(stderr, "Incorrect Usage: %s\n\n", err)
		switch {
		case !isCommand:
			cli.HelpPrinter(stderr, cli.AppHelpTemplate, c.App)
		case len(c.Command.Subcommands) > 0:
			cli.HelpPrinter(stderr, cli.SubcommandHelpTemplate, c.Command)
		default:
			cli.HelpPrinter(stderr, cli.CommandHelpTemplate, c.Command)
		}
		return err
	}
}

// settingFlags are the two settings every verb reads the same ladder by: the flag, then
// its environment variable, then this directory's config file. The flag carries no default
// so a value it does not carry can fall through.
func settingFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "url", EnvVars: []string{"SANDBIN_URL"},
			Usage: "sandbin server base URL"},
		&cli.StringFlag{Name: "api-key", EnvVars: []string{"SANDBIN_API_KEY"},
			Usage: "pre-shared API key to send as X-API-Key"},
	}
}

// metadataFlags are the two bin fields upload and delete carry; a flag that was given says
// something about the field even when the value it carries is the empty string.
func metadataFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "title", Usage: "single-line title to give the bin, sent as X-Bin-Title"},
		&cli.StringFlag{Name: "description", Usage: "single-line description to give the bin, sent as X-Bin-Description"},
	}
}

// settings is a verb's ladder answer for the url, the API key and the config file that
// sits under both. The file is read before the key guard: a corrupt one must fail loudly
// and make no request, not quietly fall through to a second bin.
type settings struct {
	cfg      clientConfig
	cfgFound bool
	baseURL  string
	apiKey   string
}

func readSettings(c *cli.Context, dir string) (settings, error) {
	var s settings
	cfg, cfgFound, err := readConfig(dir)
	if err != nil {
		return s, err
	}
	s.cfg, s.cfgFound = cfg, cfgFound
	s.baseURL, s.apiKey = c.String("url"), c.String("api-key")
	if cfgFound {
		if s.baseURL == "" && cfg.URL != "" {
			s.baseURL = cfg.URL
		}
		if s.apiKey == "" && cfg.APIKey != "" {
			s.apiKey = cfg.APIKey
		}
	}
	if s.baseURL == "" {
		s.baseURL = defaultURL
	}
	return s, nil
}

// reportFailure writes one operational failure the way sandbin-client always has: a
// "sandbin-client:" line on stderr, and when the server answered 401 for a bin password
// the run was not given, the line naming SANDBIN_BIN_PASSWORD. The error itself goes
// back to run, which maps it to the exit code; urfave prints nothing extra for a plain
// action error.
func reportFailure(stderr io.Writer, err error) {
	fmt.Fprintln(stderr, "sandbin-client:", err)
	var api *apiError
	if errors.As(err, &api) && api.status == http.StatusUnauthorized {
		fmt.Fprintln(stderr, "sandbin-client: set SANDBIN_BIN_PASSWORD to this bin's password")
	}
}

// uploadCommand is sandbin-client upload: file arguments and piped stdin go into this
// directory's bin, and a metadata flag on its own describes the bin without uploading.
func uploadCommand(stdin io.Reader, stdout, stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	flags := append(settingFlags(),
		&cli.StringFlag{Name: "bin", Usage: "an existing bin hash to upload into"},
		&cli.BoolFlag{Name: "new", Usage: "create a fresh bin instead of reusing this directory's"},
		&cli.StringFlag{Name: "name", Usage: "the path to upload piped-in stdin as"},
	)
	flags = append(flags, metadataFlags()...)
	flags = append(flags,
		&cli.StringFlag{Name: "archive", Usage: "upload this folder as one tar.gz the server unpacks into the bin"},
		&cli.BoolFlag{Name: "no-extract", Usage: "send every upload with X-Extract, so archive bodies are stored raw"},
	)
	cmd := &cli.Command{
		Name:      "upload",
		Usage:     "upload files and piped stdin into this directory's bin",
		ArgsUsage: "[file...]",
		Flags:     flags,
		Action: func(c *cli.Context) error {
			return runUpload(c, stdin, stdout, stderr)
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

// runUpload is the whole upload verb: it refuses what the server would refuse before making
// a request, reads stdin before choosing a bin, and holds its stdout lines until every
// request has succeeded.
func runUpload(c *cli.Context, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	defer func() {
		if err != nil {
			reportFailure(stderr, err)
		}
	}()

	binFlag, newBin := c.String("bin"), c.Bool("new")
	if binFlag != "" && newBin {
		return errors.New("use either --bin or --new, not both")
	}
	// A metadata flag that was given says something about the bin's fields, even when the
	// value it carries is the empty string.
	titleGiven, descriptionGiven := c.IsSet("title"), c.IsSet("description")
	archiveGiven := c.IsSet("archive")
	titleFlag, descriptionFlag := c.String("title"), c.String("description")

	// Metadata is checked before any request, the way an upload name is, because argv can
	// carry a newline the transport would not carry.
	if err := cleanMetadata("title", titleFlag, maxTitleBytes); err != nil {
		return err
	}
	if err := cleanMetadata("description", descriptionFlag, maxDescriptionBytes); err != nil {
		return err
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	s, err := readSettings(c, dir)
	if err != nil {
		return err
	}

	// A write run without the key would only charge this address a 30 minute block, so it
	// makes no request at all.
	if s.apiKey == "" {
		return errors.New("no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init")
	}

	// The bin password is env-only on purpose: no flag puts it on a command line.
	cl, err := newClient(s.baseURL, s.apiKey, os.Getenv("SANDBIN_BIN_PASSWORD"))
	if err != nil {
		return err
	}
	cl.noExtract = c.Bool("no-extract")

	// A run with no upload targets is refused, unless it came to say what the bin holds:
	// a metadata flag is a job of its own, and --archive brings its own target — though
	// piped stdin still needs a --name whichever run it is.
	metaRun := titleGiven || descriptionGiven

	var names []string
	var stdinName string
	args := c.Args().Slice()
	nameFlag := c.String("name")
	if len(args) > 0 || nameFlag != "" || (!archiveGiven && !metaRun) {
		names, stdinName, err = uploadTargets(args, nameFlag, stdin)
		if err != nil {
			return err
		}
	} else if archiveGiven && stdinCarriesUpload(stdin) {
		// --archive brings its own upload target, but it does not make a piped stdin
		// meaningless: what was piped in still needs a --name to say what it uploads, the
		// same refusal a run without --archive gives. Silently dropping it is not.
		return errors.New("stdin needs --name to say what it uploads")
	}
	// stdin is read before the bin is chosen, so an oversized stream makes no request and
	// leaves no bin behind.
	var stdinBody []byte
	if stdinName != "" {
		stdinBody, err = readStdin(stdin)
		if err != nil {
			return err
		}
	}

	// --archive is checked and built before the bin is chosen too: a folder that cannot be
	// uploaded makes no request and leaves no bin behind. The temp file is removed on every
	// exit path, including an upload failure.
	var archiveFile, archiveName string
	if archiveGiven {
		folder, err := cleanArchiveFolder(c.String("archive"))
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

	bin, protected, justCreated, err := resolveBin(cl, dir, s.cfg, s.cfgFound, binFlag, newBin, titleFlag, descriptionFlag)
	if err != nil {
		return err
	}
	if protected && cl.password == "" {
		fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
	}

	// Lines are held until every upload has succeeded, so a failed run prints nothing on
	// stdout.
	lines := make([]string, 0, len(names)+2)
	if metaRun && !justCreated {
		// A bin this run did not create takes its metadata as one PATCH before any upload,
		// so a later upload failure still leaves the bin saying what it was meant to hold.
		// A given flag replaces its field, and the empty value one carries clears it.
		if err := patchBinMeta(cl, bin, titleGiven, titleFlag, descriptionGiven, descriptionFlag); err != nil {
			return err
		}
		lines = append(lines, "updated /"+bin+"/")
	}
	for _, name := range names {
		created, extracted, err := uploadFile(cl, bin, name)
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, cl.fileURL(bin, name)))
	}
	if stdinName != "" {
		created, extracted, err := cl.putFile(bin, stdinName, bytes.NewReader(stdinBody))
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, cl.fileURL(bin, stdinName)))
	}
	if archiveFile != "" {
		// The folder archive uploads last, after the file arguments and stdin of the same
		// run. --no-extract sends the opt-out header, so there the archive itself is what
		// lands in the bin.
		f, err := os.Open(archiveFile)
		if err != nil {
			return err
		}
		defer f.Close()
		created, extracted, err := cl.putFile(bin, archiveName, f)
		if err != nil {
			return err
		}
		lines = append(lines, resultLine(created, extracted, cl.fileURL(bin, archiveName)))
	}

	lines = append(lines, cl.binURL(bin))
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// patchBinMeta sends the metadata of a bin this run did not create as one PATCH: a given
// flag replaces its field, and the empty value one carries clears it.
func patchBinMeta(cl *client, bin string, titleGiven bool, titleFlag string, descriptionGiven bool, descriptionFlag string) error {
	var titlePatch, descriptionPatch *string
	if titleGiven {
		titlePatch = &titleFlag
	}
	if descriptionGiven {
		descriptionPatch = &descriptionFlag
	}
	return cl.patchBinMeta(bin, titlePatch, descriptionPatch)
}

// deleteCommand is sandbin-client delete: named paths and folders come out of this
// directory's bin and nothing goes in, so the flags that put things in are not even
// defined on it. Metadata flags ride along, and the PATCH they carry runs first.
func deleteCommand(stdout, stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	flags := append(settingFlags(),
		&cli.StringFlag{Name: "bin", Usage: "an existing bin hash to delete from"},
	)
	flags = append(flags, metadataFlags()...)
	flags = append(flags,
		&cli.StringSliceFlag{Name: "folder", Usage: "delete this folder and everything under it; repeat it per folder"},
	)
	cmd := &cli.Command{
		Name:      "delete",
		Usage:     "take named paths and folders out of this directory's bin",
		ArgsUsage: "[path...]",
		Flags:     flags,
		Action: func(c *cli.Context) error {
			return runDelete(c, stdout, stderr)
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

// runDelete is the whole delete verb: a delete run reads this directory's bin exactly like
// describe does and never makes one, and its lines are held until every request has
// succeeded.
func runDelete(c *cli.Context, stdout, stderr io.Writer) (err error) {
	defer func() {
		if err != nil {
			reportFailure(stderr, err)
		}
	}()

	paths, folders := c.Args().Slice(), c.StringSlice("folder")
	if len(paths) == 0 && len(folders) == 0 {
		return errors.New("delete names the paths to take out: pass file arguments or --folder NAME")
	}
	// Delete names are checked the way upload names are, before any request: a path
	// ending in a slash names a folder and --folder is the flag that names one.
	targets, err := cleanDeleteTargets(paths, folders)
	if err != nil {
		return err
	}

	titleGiven, descriptionGiven := c.IsSet("title"), c.IsSet("description")
	titleFlag, descriptionFlag := c.String("title"), c.String("description")
	if err := cleanMetadata("title", titleFlag, maxTitleBytes); err != nil {
		return err
	}
	if err := cleanMetadata("description", descriptionFlag, maxDescriptionBytes); err != nil {
		return err
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	s, err := readSettings(c, dir)
	if err != nil {
		return err
	}
	if s.apiKey == "" {
		return errors.New("no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init")
	}

	cl, err := newClient(s.baseURL, s.apiKey, os.Getenv("SANDBIN_BIN_PASSWORD"))
	if err != nil {
		return err
	}

	bin, protected, err := resolveExistingBin(cl, dir, s.cfg, s.cfgFound, c.String("bin"), "delete")
	if err != nil {
		return err
	}
	if protected && cl.password == "" {
		fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
	}

	// Lines are held until every request has succeeded, the way an upload run holds its
	// own, so a failed run prints nothing on stdout.
	var lines []string
	if titleGiven || descriptionGiven {
		// The bin says what it holds before the paths come out of it, so the metadata
		// answer is the run's first line.
		if err := patchBinMeta(cl, bin, titleGiven, titleFlag, descriptionGiven, descriptionFlag); err != nil {
			return err
		}
		lines = append(lines, "updated /"+bin+"/")
	}
	for _, target := range targets {
		deleted, err := cl.deletePaths(bin, target.name, target.folder)
		if err != nil {
			// The refusal names the path this run aimed at, which is what a reader needs
			// to fix the command, and keeps the server's own error reachable, which is how
			// a 401 still earns its SANDBIN_BIN_PASSWORD hint.
			return fmt.Errorf("delete %q: %w", target.name, err)
		}
		for _, name := range deleted {
			lines = append(lines, "deleted "+cl.fileURL(bin, name))
		}
	}
	lines = append(lines, cl.binURL(bin))
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// describeCommand is sandbin-client describe: it prints the title and description of the
// bin this directory points at. It writes nothing, so it needs no API key.
func describeCommand(stdout, stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	flags := append(settingFlags(),
		&cli.StringFlag{Name: "bin", Usage: "an existing bin hash to read"},
	)
	cmd := &cli.Command{
		Name:      "describe",
		Usage:     "print this bin's title and description; writes nothing and needs no API key",
		ArgsUsage: "(no arguments)",
		Flags:     flags,
		Action: func(c *cli.Context) error {
			return runDescribe(c, stdout, stderr)
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

func runDescribe(c *cli.Context, stdout, stderr io.Writer) (err error) {
	defer func() {
		if err != nil {
			reportFailure(stderr, err)
		}
	}()

	if extra := c.Args().Slice(); len(extra) > 0 {
		return fmt.Errorf("describe takes no arguments: it reads a bin, it does not take one as an argument: %q", strings.Join(extra, " "))
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	s, err := readSettings(c, dir)
	if err != nil {
		return err
	}

	cl, err := newClient(s.baseURL, s.apiKey, os.Getenv("SANDBIN_BIN_PASSWORD"))
	if err != nil {
		return err
	}

	bin, protected, err := resolveExistingBin(cl, dir, s.cfg, s.cfgFound, c.String("bin"), "describe")
	if err != nil {
		return err
	}
	if protected && cl.password == "" {
		fmt.Fprintf(stderr, "sandbin-client: warning: bin %s has a password and SANDBIN_BIN_PASSWORD is not set\n", bin)
	}
	title, description, err := cl.describeBin(bin)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "title: %s\ndescription: %s\n", title, description)
	return nil
}

// initCommand is sandbin-client init: it settles this project's .sandbin.yaml with a url
// and key the server has just accepted.
func initCommand(stdin io.Reader, stdout, stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	cmd := &cli.Command{
		Name:      "init",
		Usage:     "settle this project's .sandbin.yaml with a url and key the server has just accepted",
		ArgsUsage: "(no arguments)",
		Flags: append(settingFlags(),
			&cli.BoolFlag{Name: "reinit", Usage: "overwrite an existing .sandbin.yaml instead of refusing"},
		),
		Action: func(c *cli.Context) error {
			return runInit(c, stdin, stdout, stderr)
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

// runInit answers init: values come from flag, then env, then the existing file; what is
// still missing on a terminal is asked, what is missing without one falls back (the url)
// or refuses (the key). Nothing is written until the server has verified the key, so init
// never saves a broken setup. A project whose file already holds both a url and a key is
// re-initialized only by consent: a y at the terminal question, or --reinit for a script.
func runInit(c *cli.Context, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	// init reports its own failures without the 401 bin-password hint line: it verifies an
	// API key, and a bin password has no part in that.
	defer func() {
		if err != nil {
			fmt.Fprintln(stderr, "sandbin-client:", err)
		}
	}()

	if extra := c.Args().Slice(); len(extra) > 0 {
		return errors.New("init takes no file arguments: its settings are --url and --api-key")
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, cfgFound, err := readConfig(dir)
	if err != nil {
		return err
	}

	// The re-initialization guard: the file holding both a sandbin_url and an api_key is
	// the marker — a partial file is not initialized, and neither are flags or the
	// environment, because supplying new values is exactly the overwrite the guard exists
	// to confirm. It sits before the ladder, any prompt, the verification and any write,
	// so a refusal or a cancel requests and writes nothing.
	reader := bufio.NewReader(stdin)
	reinit := c.Bool("reinit")
	if cfgFound && cfg.URL != "" && cfg.APIKey != "" && !reinit {
		if !isTerminal(stdin) {
			return fmt.Errorf(".sandbin.yaml is already initialized for %s: pass --reinit to overwrite it, or delete the file to start over", cfg.URL)
		}
		fmt.Fprintf(stderr, "sandbin-client: warning: .sandbin.yaml is already initialized for %s\n", cfg.URL)
		fmt.Fprint(stdout, "re-initialize? [y/N]: ")
		line, _ := reader.ReadString('\n')
		if answer := strings.ToLower(strings.TrimRight(line, "\r\n")); answer != "y" && answer != "yes" {
			fmt.Fprintf(stdout, "left the existing initialization for %s untouched\n", cfg.URL)
			return nil
		}
	}

	// The flag > env > file ladder, same as an upload verb; the flags carry no default
	// precisely so a value they do not carry can fall through to a prompt.
	base := c.String("url")
	if base == "" && cfgFound {
		base = cfg.URL
	}
	key := c.String("api-key")
	if key == "" && cfgFound {
		key = cfg.APIKey
	}

	if base == "" && isTerminal(stdin) {
		fmt.Fprintf(stdout, "sandbin_url [%s]: ", defaultURL)
		line, _ := reader.ReadString('\n')
		base = strings.TrimRight(line, "\r\n")
	}
	if base == "" {
		base = defaultURL
	}
	if key == "" && isTerminal(stdin) {
		// shortcut: visible echo for the key prompt; upgrade with golang.org/x/term ReadPassword if agents ever need masking
		fmt.Fprint(stdout, "api_key: ")
		line, _ := reader.ReadString('\n')
		key = strings.TrimRight(line, "\r\n")
	}
	if key == "" {
		return errors.New("no API key: pass --api-key, set SANDBIN_API_KEY or answer the init prompt")
	}

	cl, err := newClient(base, key, "")
	if err != nil {
		return err
	}
	if err := cl.verifyKey(); err != nil {
		return err
	}

	// The file's bin and protected belong to its url: they survive only when that url is
	// the one being saved. Writing the verified key is init's job and only init's.
	next := clientConfig{URL: base, APIKey: key, Bin: cfg.Bin, Protected: cfg.Protected}
	if !cfgFound || cfg.URL != base {
		next.Bin, next.Protected = "", false
	}
	if err := writeConfig(dir, next); err != nil {
		return err
	}
	if err := ensureGitignore(dir); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "initialized %s\n", base)
	return nil
}

// deleteTarget is one path a delete run takes out of the bin, marked with the folder shape
// the server reads it by.
type deleteTarget struct {
	name   string
	folder bool
}

// cleanDeleteTargets cleans every delete path and --folder name the way an upload name is
// cleaned, before any request. The file paths come first and the folders after, each in
// the order the run gave them.
func cleanDeleteTargets(files, folders []string) ([]deleteTarget, error) {
	targets := make([]deleteTarget, 0, len(files)+len(folders))
	for _, raw := range files {
		name, err := cleanDeleteName(raw)
		if err != nil {
			return nil, err
		}
		targets = append(targets, deleteTarget{name: name})
	}
	for _, raw := range folders {
		name, err := cleanDeleteFolderName(raw)
		if err != nil {
			return nil, err
		}
		targets = append(targets, deleteTarget{name: name, folder: true})
	}
	return targets, nil
}

// cleanDeleteName cleans one delete path with the upload name's rules. A name ending in a
// slash names a folder, and --folder is the flag that names a folder, so the refusal points
// there with the name it wants.
func cleanDeleteName(raw string) (string, error) {
	if strings.HasSuffix(raw, "/") {
		folder := strings.TrimSuffix(raw, "/")
		if cleaned, err := cleanUploadName(folder); err == nil {
			folder = cleaned
		}
		return "", fmt.Errorf("delete %q names a folder: use --folder %s", raw, folder)
	}
	name, err := cleanUploadName(raw)
	if err != nil {
		return "", fmt.Errorf("delete %q: %v", raw, err)
	}
	return name, nil
}

// cleanDeleteFolderName cleans one --folder name with the upload name's rules. The slash
// that marks a folder goes on the request, so a name carrying one is refused rather than
// sent doubled.
func cleanDeleteFolderName(raw string) (string, error) {
	if strings.HasSuffix(raw, "/") {
		return "", fmt.Errorf("--folder %q needs no trailing slash: the client sends it", raw)
	}
	name, err := cleanUploadName(raw)
	if err != nil {
		return "", fmt.Errorf("--folder %q: %v", raw, err)
	}
	return name, nil
}

// resolveBin picks the bin to upload into: --bin, then this directory's config file when
// its sandbin_url matches the url in use, then a new bin written to the config file so the
// next run reuses it. --new skips straight to a new bin. A written file keeps whatever
// api_key it already held — an upload run never newly persists one. A bin made here takes
// its metadata in the creation POST, and justCreated is what tells the caller no second
// request is needed for it.
func resolveBin(c *client, dir string, cfg clientConfig, cfgFound bool, binFlag string, newBin bool, title, description string) (string, bool, bool, error) {
	if binFlag != "" {
		hash, err := cleanBinHash(binFlag)
		return hash, false, false, err
	}

	base := c.base.String()
	if !newBin && cfgFound && cfg.Bin != "" && cfg.URL == base {
		return cfg.Bin, cfg.Protected, false, nil
	}

	hash, err := c.createBin(title, description)
	if err != nil {
		return "", false, false, err
	}
	protected := c.password != ""
	if err := writeConfig(dir, clientConfig{URL: base, APIKey: cfg.APIKey, Bin: hash, Protected: protected}); err != nil {
		// The bin exists on the server now, so the run must report its hash: rerunning
		// would only create a second bin, not recover this one.
		return "", false, false, fmt.Errorf("created bin %s but could not save it: %v", hash, err)
	}
	return hash, protected, true, nil
}

// resolveExistingBin is the bin a describe or delete run uses: --bin, or this directory's
// config file when its sandbin_url matches the url in use. Neither kind of run makes a bin,
// so a directory with neither gets an error naming what it needs, in the words of the run
// that asked.
func resolveExistingBin(c *client, dir string, cfg clientConfig, cfgFound bool, binFlag, runName string) (string, bool, error) {
	if binFlag != "" {
		hash, err := cleanBinHash(binFlag)
		return hash, false, err
	}
	if !cfgFound || cfg.Bin == "" || cfg.URL != c.base.String() {
		return "", false, fmt.Errorf("%s has no bin for this directory: pass --bin or upload something first", runName)
	}
	return cfg.Bin, cfg.Protected, nil
}

// uploadTargets cleans every upload name and checks each local file exists and fits the
// 1 MiB body cap, all before the first request, so a bad argument cannot leave half a
// prototype uploaded. The cleaned stdin name is returned separately.
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
	// --name with a terminal for stdin uploads whatever gets typed, or when nothing is
	// piped at all an empty file that a rerun would report as a success.
	if isTerminal(stdin) {
		return nil, "", fmt.Errorf("--name %q got no stdin: pipe the content in or pass a file argument", nameFlag)
	}
	return names, stdinName, nil
}

// cleanUploadName canonicalises an upload name the way the server reads it and refuses what
// the server would refuse, with the server's own wording, so a bad name fails before a
// request. The server stays the authority.
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
	// The ".." check has to see the segments the caller wrote, because path.Clean would eat
	// them along with "./", "//" and a trailing slash.
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", errors.New("path must not escape the bin")
		}
	}
	name := path.Clean(strings.TrimPrefix(raw, "./"))
	if name == "." || name == "" {
		return "", errors.New("empty path")
	}
	// The limit is measured on the cleaned name because that is what the server receives:
	// a "./" prefix must not eat into it here and not there.
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

// cleanBinHash accepts a --bin hash, and tolerates the "/abc1234/" shape a created bin was
// printed as.
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

// cleanArchiveFolder cleans the --archive folder name the way an upload name is cleaned and
// insists it names an existing directory relative to the current directory: the base path
// of a bin must name a folder, so "." and "./" and absolute paths are refused.
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

// buildArchive walks the folder with filepath.WalkDir, which is lexicographic so runs are
// deterministic, and refuses everything the server would refuse anyway: non-regular
// entries, files over the 1 MiB body cap, more than the 1000 entries an archive may hold,
// more total bytes than an extraction may unpack, entry paths the server's cleanPath would
// refuse, and a folder that holds no files at all. What survives goes into a fresh
// os.CreateTemp .tar.gz whose entries carry the folder's relative path verbatim, so the
// bin's paths come out exactly folder-relative. The archive file itself is capped at
// 1 MiB, the cap every PUT body obeys. On any failure the temp file is already removed;
// the caller removes it on success.
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

// resultLine is what one upload adds to stdout. An answer that carried X-Extracted-Count
// says how many files the server unpacked and where they live; the file URL it names is
// the archive's, and the extracted files sit under the archive name's directory in the bin.
func resultLine(created bool, extracted int, url string) string {
	if extracted > 0 {
		return fmt.Sprintf("extracted %d files into %s", extracted, url)
	}
	if created {
		return "created " + url
	}
	return "updated " + url
}

// isTerminal reports whether stdin is an interactive terminal instead of a pipe or a
// redirected file. A character device such as /dev/null reads as a terminal here, which is
// the honest answer: it carries no upload.
func isTerminal(stdin io.Reader) bool {
	f, ok := stdin.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// stdinCarriesUpload reports whether the run was handed something that could hold an upload
// on stdin. A run handed no reader at all — which only the tests do, since main always
// hands over os.Stdin — was handed nothing.
func stdinCarriesUpload(stdin io.Reader) bool {
	return stdin != nil && !isTerminal(stdin)
}
