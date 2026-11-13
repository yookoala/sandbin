package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// initConfig reads back the file an init run wrote, mode included.
func initConfig(t *testing.T) clientConfig {
	t.Helper()
	raw, info := readConfigFile(t)
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("%s mode = %o, want 0600", configFileName, perm)
	}
	var cfg clientConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	return cfg
}

// gitignoreBytes reads the .gitignore of the test's working directory.
func gitignoreBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(gitignoreFileName)
	if err != nil {
		t.Fatalf("read %s: %v", gitignoreFileName, err)
	}
	return raw
}

// noInitFiles asserts that a refused init wrote neither the config file nor a .gitignore.
func noInitFiles(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(configFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after the refusal, want nothing written", configFileName)
	}
	if _, err := os.Stat(gitignoreFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after the refusal, want nothing written", gitignoreFileName)
	}
}

// ttyReader hands the wizard canned answers and reports itself as a terminal, which
// is how these tests drive the prompts without a real pty.
type ttyReader struct{ r *strings.Reader }

func (tr *ttyReader) Read(p []byte) (int, error) { return tr.r.Read(p) }
func (tr *ttyReader) Stat() (os.FileInfo, error) { return ttyInfo{}, nil }

// ttyInfo is the Mode a terminal's Stat reports.
type ttyInfo struct{}

func (ttyInfo) Name() string       { return "stdin" }
func (ttyInfo) Size() int64        { return 0 }
func (ttyInfo) Mode() os.FileMode  { return os.ModeCharDevice }
func (ttyInfo) ModTime() time.Time { return time.Time{} }
func (ttyInfo) IsDir() bool        { return false }
func (ttyInfo) Sys() any           { return nil }

func TestInitWritesTheConfigFileAndGitignore(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")

	stdout, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key")
	if err != nil {
		t.Fatalf("init: %v; stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing on a successful init", stderr)
	}
	if want := "initialized " + url1 + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if strings.Contains(stdout, "test-key") || strings.Contains(stderr, "test-key") {
		t.Errorf("stdout %q / stderr %q, want no trace of the key in any output", stdout, stderr)
	}

	cfg := initConfig(t)
	if cfg.URL != url1 || cfg.APIKey != "test-key" || cfg.Bin != "" || cfg.Protected {
		t.Errorf("%s = %+v, want the url and the key, and no bin yet", configFileName, cfg)
	}
	if got := string(gitignoreBytes(t)); got != configFileName+"\n" {
		t.Errorf("%s = %q, want the one line naming the config file", gitignoreFileName, got)
	}
	if f.requestsSeen() != 1 || f.binCount() != 0 {
		t.Errorf("requests = %d, bins = %d; want the one verify request and no bin", f.requestsSeen(), f.binCount())
	}
}

// TestInitReRunIsIdempotent: --reinit re-reads its own file when no flag or env carries
// a value, and a re-run leaves both files byte-identical.
func TestInitReRunIsIdempotent(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	if _, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key"); err != nil {
		t.Fatalf("first init: %v; stderr %s", err, stderr)
	}
	firstCfg, _ := readConfigFile(t)
	firstGit := gitignoreBytes(t)

	t.Setenv("SANDBIN_URL", "")
	t.Setenv("SANDBIN_API_KEY", "")
	stdout, stderr, err := runClient(t, nil, "init", "--reinit")
	if err != nil {
		t.Fatalf("--reinit: %v; stderr %s", err, stderr)
	}
	if want := "initialized " + url1 + "\n"; stdout != want {
		t.Errorf("stdout = %q, want the file's own url confirmed: %q", stdout, want)
	}
	secondCfg, _ := readConfigFile(t)
	if string(secondCfg) != string(firstCfg) {
		t.Errorf("re-init rewrote %s:\n%s\nwant it byte-identical to\n%s", configFileName, secondCfg, firstCfg)
	}
	if got := gitignoreBytes(t); string(got) != string(firstGit) {
		t.Errorf("re-init rewrote %s to %q, want it untouched", gitignoreFileName, got)
	}
}

func TestInitAppendsOneLineToAnExistingGitignore(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, gitignoreFileName, "build/\n")

	if _, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key"); err != nil {
		t.Fatalf("init: %v; stderr %s", err, stderr)
	}
	if got := string(gitignoreBytes(t)); got != "build/\n"+configFileName+"\n" {
		t.Errorf("%s = %q, want exactly one line appended", gitignoreFileName, got)
	}
}

func TestInitLeavesAGitignoreThatAlreadyNamesTheFile(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	before := "build/\n" + configFileName + "\ncatchme*\n"
	writeWorkDirFile(t, gitignoreFileName, before)

	if _, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key"); err != nil {
		t.Fatalf("init: %v; stderr %s", err, stderr)
	}
	if got := string(gitignoreBytes(t)); got != before {
		t.Errorf("%s = %q, want the file untouched byte for byte", gitignoreFileName, got)
	}
}

func TestInitWrongKeyWritesNothing(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")

	stdout, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "wrong-key")
	if err == nil {
		t.Fatalf("init with a wrong key succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "server rejected this API key") {
		t.Errorf("stderr = %q, want the refused-key wording", stderr)
	}
	if strings.Contains(stdout, "initialized") {
		t.Errorf("stdout = %q, want nothing claimed on a failed init", stdout)
	}
	noInitFiles(t)
	if f.binCount() != 0 {
		t.Errorf("bins = %d, want init to create no bin", f.binCount())
	}
}

// TestInitBlockedAddressRefusal: the first wrong key already blocked this address, so
// even the right key now comes back as the blocking wording — and still nothing is written.
func TestInitBlockedAddressRefusal(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	if _, _, err := runClient(t, nil, "init", "--url", url1, "--api-key", "wrong-key"); err == nil {
		t.Fatalf("the wrong-key init should have failed")
	}

	_, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key")
	if err == nil {
		t.Fatalf("the blocked init succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "server is blocking this address: too many bad API keys") {
		t.Errorf("stderr = %q, want the blocked-address wording", stderr)
	}
	noInitFiles(t)
}

func TestInitDeadPortNamesTheUrl(t *testing.T) {
	f := newFake(t, "test-key")
	_, stderr, err := runClient(t, nil, "init", "--url", "http://127.0.0.1:1", "--api-key", "test-key")
	if err == nil {
		t.Fatalf("init against a dead port succeeded, want an error")
	}
	if !strings.Contains(stderr, "cannot reach http://127.0.0.1:1") {
		t.Errorf("stderr = %q, want it to name the url it could not reach", stderr)
	}
	noInitFiles(t)
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want the fake server untouched", f.requestsSeen())
	}
}

// TestInitNonTerminalMissingKeyRefuses is the scripted-agent path: no terminal to ask,
// no key anywhere, so the refusal names all three ways in and nothing else happens.
func TestInitNonTerminalMissingKeyRefuses(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_API_KEY", "")

	stdout, stderr, err := runClient(t, nil, "init")
	if err == nil {
		t.Fatalf("init without a key or a terminal succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "no API key: pass --api-key, set SANDBIN_API_KEY or answer the init prompt") {
		t.Errorf("stderr = %q, want the init refusal wording", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	noInitFiles(t)
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

// TestInitNonTerminalUrlTakesDefault: a missing url on a non-terminal quietly falls to
// the default — the refusal below proves it is the url that was tried.
func TestInitNonTerminalUrlTakesDefault(t *testing.T) {
	newFake(t, "test-key")
	t.Setenv("SANDBIN_URL", "")
	t.Setenv("SANDBIN_API_KEY", "")

	_, stderr, err := runClient(t, nil, "init", "--api-key", "test-key")
	if err == nil {
		t.Fatalf("init against the default url succeeded, want an error")
	}
	if !strings.Contains(stderr, "cannot reach "+defaultURL) {
		t.Errorf("stderr = %q, want it to name the default url it tried", stderr)
	}
	noInitFiles(t)
}

// TestInitWizardPromptsAndWrites drives the whole wizard on a fake terminal: Enter was
// not taken — both values were typed — and the key literal never reaches stdout.
func TestInitWizardPromptsAndWrites(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	t.Setenv("SANDBIN_URL", "")
	t.Setenv("SANDBIN_API_KEY", "")

	stdin := &ttyReader{strings.NewReader(url1 + "\ntest-key\n")}
	stdout, stderr, err := runClient(t, stdin, "init")
	if err != nil {
		t.Fatalf("wizard init: %v; stderr %s", err, stderr)
	}
	for _, want := range []string{
		"sandbin_url [" + defaultURL + "]: ",
		"api_key: ",
		"initialized " + url1 + "\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to carry %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "test-key") || strings.Contains(stderr, "test-key") {
		t.Errorf("stdout %q / stderr %q, want the typed key echoed by no output of the client", stdout, stderr)
	}
	cfg := initConfig(t)
	if cfg.URL != url1 || cfg.APIKey != "test-key" {
		t.Errorf("%s = %+v, want the wizard's answers stored", configFileName, cfg)
	}
	if got := string(gitignoreBytes(t)); got != configFileName+"\n" {
		t.Errorf("%s = %q, want the one line naming the config file", gitignoreFileName, got)
	}
	if f.requestsSeen() != 1 {
		t.Errorf("requests = %d, want the one verify request", f.requestsSeen())
	}
}

// TestInitWizardEnterKeepsDefault: Enter on the url prompt takes the shown default, and
// the attempt proves it was the url used.
func TestInitWizardEnterKeepsDefault(t *testing.T) {
	newFake(t, "test-key")
	t.Setenv("SANDBIN_URL", "")
	t.Setenv("SANDBIN_API_KEY", "")

	stdin := &ttyReader{strings.NewReader("\ntest-key\n")}
	stdout, stderr, err := runClient(t, stdin, "init")
	if err == nil {
		t.Fatalf("init against the default url succeeded, want an error")
	}
	if !strings.Contains(stdout, "sandbin_url ["+defaultURL+"]: ") {
		t.Errorf("stdout = %q, want the prompt showing the default", stdout)
	}
	if !strings.Contains(stderr, "cannot reach "+defaultURL) {
		t.Errorf("stderr = %q, want it to name the default url Enter accepted", stderr)
	}
	noInitFiles(t)
}

// TestInitWizardEmptyKeyRefuses: an empty answer at the key prompt stays missing, and
// the refusal is the same wording the non-terminal run gives.
func TestInitWizardEmptyKeyRefuses(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	t.Setenv("SANDBIN_URL", "")
	t.Setenv("SANDBIN_API_KEY", "")

	stdin := &ttyReader{strings.NewReader(url1 + "\n\n")}
	_, stderr, err := runClient(t, stdin, "init")
	if err == nil {
		t.Fatalf("init with an empty key answer succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "no API key: pass --api-key, set SANDBIN_API_KEY or answer the init prompt") {
		t.Errorf("stderr = %q, want the init refusal wording", stderr)
	}
	noInitFiles(t)
}

// TestInitStoresTheEnvKey: storing a key that came only from the env is init's job —
// it verifies the key and writes it into the file.
func TestInitStoresTheEnvKey(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\n")
	t.Setenv("SANDBIN_API_KEY", "test-key")

	if _, stderr, err := runClient(t, nil, "init"); err != nil {
		t.Fatalf("init with an env key: %v; stderr %s", err, stderr)
	}
	cfg := initConfig(t)
	if cfg.APIKey != "test-key" || cfg.URL != url1 {
		t.Errorf("%s = %+v, want init to store the verified env key next to the url", configFileName, cfg)
	}
}

// TestInitEnvUrlOverridesTheFileUrl is the ladder plus the bin-drop rule: the env's url
// beats the file's, and a bin made for the file's server does not travel. The file is a
// fully initialized setup, so the run carries --reinit: flags and env alone are not
// consent for overwriting one.
func TestInitEnvUrlOverridesTheFileUrl(t *testing.T) {
	f1 := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\napi_key: test-key\nbin: bin0001\nprotected: true\n")
	f2, url2 := newFakeAt(t, "test-key")
	t.Setenv("SANDBIN_URL", url2)
	t.Setenv("SANDBIN_API_KEY", "test-key")

	if _, stderr, err := runClient(t, nil, "init", "--reinit"); err != nil {
		t.Fatalf("init on the env url: %v; stderr %s", err, stderr)
	}
	if f1.requestsSeen() != 0 {
		t.Errorf("requests at the file's server = %d, want the env's url verified instead", f1.requestsSeen())
	}
	if f2.requestsSeen() != 1 {
		t.Errorf("requests at the env's server = %d, want the one verify request", f2.requestsSeen())
	}
	cfg := initConfig(t)
	if cfg.URL != url2 || cfg.Bin != "" || cfg.Protected {
		t.Errorf("%s = %+v, want the new url with the stale bin dropped", configFileName, cfg)
	}
}

func TestInitFlagUrlBeatsTheEnv(t *testing.T) {
	f1 := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	f2, url2 := newFakeAt(t, "test-key")
	t.Setenv("SANDBIN_URL", url2)

	stdout, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key")
	if err != nil {
		t.Fatalf("init with the flag url: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(stdout, url1) {
		t.Errorf("stdout = %q, want the flag's url confirmed", stdout)
	}
	if f2.requestsSeen() != 0 {
		t.Errorf("requests at the env's server = %d, want the flag's url verified instead", f2.requestsSeen())
	}
	if f1.requestsSeen() != 1 {
		t.Errorf("requests at the flag's server = %d, want the one verify request", f1.requestsSeen())
	}
	cfg := initConfig(t)
	if cfg.URL != url1 {
		t.Errorf("%s = %+v, want the flag's url stored", configFileName, cfg)
	}
}

// TestInitKeepsTheBinOnTheSameUrl: re-initing at the same url keeps the file's bin and
// protected — init re-verifies the setup, it does not reset it.
func TestInitKeepsTheBinOnTheSameUrl(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	stdout, stderr, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("protected upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	t.Setenv("SANDBIN_BIN_PASSWORD", "")

	if _, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key"); err != nil {
		t.Fatalf("init: %v; stderr %s", err, stderr)
	}
	cfg := initConfig(t)
	if cfg.Bin != hash || !cfg.Protected {
		t.Errorf("%s = %+v, want bin %s kept protected on the same url", configFileName, cfg, hash)
	}
}

// TestInitRefusesPositionalArguments: init is a settings verb; a stray argument is a
// typo, not something to ignore.
func TestInitRefusesPositionalArguments(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	_, stderr, err := runClient(t, nil, "init", "index.html")
	if err == nil {
		t.Fatalf("init with a positional argument succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "init takes no file arguments") {
		t.Errorf("stderr = %q, want it to refuse the positional argument", stderr)
	}
	noInitFiles(t)
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

// initializedProject inits this directory the ordinary way and returns what its two
// files hold plus the request count the fake server has seen, which is the baseline
// the guard tests compare against.
func initializedProject(t *testing.T, f *fakeServer) (url string, cfgRaw, gitRaw []byte, seen int) {
	t.Helper()
	url = os.Getenv("SANDBIN_URL")
	if _, stderr, err := runClient(t, nil, "init", "--url", url, "--api-key", "test-key"); err != nil {
		t.Fatalf("first init: %v; stderr %s", err, stderr)
	}
	cfgRaw, _ = readConfigFile(t)
	gitRaw = gitignoreBytes(t)
	return url, cfgRaw, gitRaw, f.requestsSeen()
}

// assertProjectUntouched compares both files against a recorded state, byte for byte.
func assertProjectUntouched(t *testing.T, cfgRaw, gitRaw []byte) {
	t.Helper()
	secondCfg, _ := readConfigFile(t)
	if string(secondCfg) != string(cfgRaw) {
		t.Errorf("%s changed:\n%s\nwant it byte-identical to\n%s", configFileName, secondCfg, cfgRaw)
	}
	if got := gitignoreBytes(t); string(got) != string(gitRaw) {
		t.Errorf("%s changed to %q, want it untouched", gitignoreFileName, got)
	}
}

// TestInitRerunOnInitializedProjectRefuses: a project whose file holds both a url and a
// key is not re-initialized without consent. Without a terminal the refusal is
// word-for-word and nothing is requested or rewritten.
func TestInitRerunOnInitializedProjectRefuses(t *testing.T) {
	f := newFake(t, "test-key")
	url1, cfgRaw, gitRaw, seen := initializedProject(t, f)

	stdout, stderr, err := runClient(t, nil, "init")
	if err == nil {
		t.Fatalf("re-init without --reinit succeeded, want the refusal")
	}
	want := "sandbin-client: .sandbin.yaml is already initialized for " + url1 +
		": pass --reinit to overwrite it, or delete the file to start over\n"
	if stderr != want {
		t.Errorf("stderr = %q, want the refusal word-for-word:\n%q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on stdout", stdout)
	}
	assertProjectUntouched(t, cfgRaw, gitRaw)
	if f.requestsSeen() != seen {
		t.Errorf("requests = %d after the refusal, want none beyond the first init's %d", f.requestsSeen(), seen)
	}
}

// TestInitRerunFlagsAreNotConsent: supplying fresh --url/--api-key values is exactly the
// overwrite the guard exists to confirm, so it never satisfies the guard — and a wrong
// --api-key charges nothing, because the guard precedes the ladder and the verification.
func TestInitRerunFlagsAreNotConsent(t *testing.T) {
	f := newFake(t, "test-key")
	url1, cfgRaw, gitRaw, seen := initializedProject(t, f)
	wantRefusal := "sandbin-client: .sandbin.yaml is already initialized for " + url1 +
		": pass --reinit to overwrite it, or delete the file to start over\n"

	stdout, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "wrong-key")
	if err == nil {
		t.Fatalf("init --api-key wrong-key over an initialized project succeeded, want the refusal")
	}
	if stderr != wantRefusal {
		t.Errorf("stderr = %q, want the guard refusal, not a server answer:\n%q", stderr, wantRefusal)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if f.requestsSeen() != seen {
		t.Errorf("requests = %d, want zero verify-key requests: the guard precedes the verification", f.requestsSeen())
	}
	assertProjectUntouched(t, cfgRaw, gitRaw)
}

// TestInitRerunNewFlagUrlIsNotConsent: the refusal names the existing initialization,
// not a fresh --url that was not consented to.
func TestInitRerunNewFlagUrlIsNotConsent(t *testing.T) {
	f := newFake(t, "test-key")
	url1, cfgRaw, gitRaw, seen := initializedProject(t, f)
	f2, url2 := newFakeAt(t, "test-key")

	_, stderr, err := runClient(t, nil, "init", "--url", url2, "--api-key", "test-key")
	if err == nil {
		t.Fatalf("init --url over an initialized project succeeded, want the refusal")
	}
	want := "sandbin-client: .sandbin.yaml is already initialized for " + url1 +
		": pass --reinit to overwrite it, or delete the file to start over\n"
	if stderr != want {
		t.Errorf("stderr = %q, want the refusal naming the existing url word-for-word:\n%q", stderr, want)
	}
	assertProjectUntouched(t, cfgRaw, gitRaw)
	if f.requestsSeen() != seen || f2.requestsSeen() != 0 {
		t.Errorf("requests = %d at the file's server and %d at the flag's, want none at either", f.requestsSeen(), f2.requestsSeen())
	}
}

// TestInitGuardAsksAtTerminal: at a terminal the guard warns, then asks; y/yes in any
// case proceeds with the wizard, everything else — bare Enter and EOF included — cancels
// with exit 0, nothing requested and nothing rewritten.
func TestInitGuardAsksAtTerminal(t *testing.T) {
	answers := []struct {
		answer  string
		proceed bool
	}{
		{"y\n", true}, {"Y\n", true}, {"yes\n", true}, {"Yes\n", true}, {"YES\n", true},
		{"n\n", false}, {"N\n", false}, {"no\n", false}, {"No\n", false},
		{"\n", false}, {"maybe\n", false}, {"", false},
	}
	for _, c := range answers {
		t.Run("answer "+strconv.Quote(c.answer), func(t *testing.T) {
			f := newFake(t, "test-key")
			url1, cfgRaw, gitRaw, seen := initializedProject(t, f)

			stdin := &ttyReader{strings.NewReader(c.answer)}
			stdout, stderr, err := runClient(t, stdin, "init")
			if err != nil {
				t.Fatalf("guarded init: %v; stderr %s", err, stderr)
			}
			wantWarn := "sandbin-client: warning: .sandbin.yaml is already initialized for " + url1 + "\n"
			if stderr != wantWarn {
				t.Errorf("stderr = %q, want the warning word-for-word:\n%q", stderr, wantWarn)
			}
			wantQuestion := "re-initialize? [y/N]: "
			var want string
			if c.proceed {
				want = wantQuestion + "initialized " + url1 + "\n"
				if stdout != want {
					t.Errorf("stdout = %q, want the pinned question and then the wizard's finish: %q", stdout, want)
				}
				if f.requestsSeen() != seen+1 {
					t.Errorf("requests = %d, want the wizard's one verify request", f.requestsSeen())
				}
			} else {
				want = wantQuestion + "left the existing initialization for " + url1 + " untouched\n"
				if stdout != want {
					t.Errorf("stdout = %q, want the pinned question and then the cancel wording: %q", stdout, want)
				}
				if f.requestsSeen() != seen {
					t.Errorf("requests = %d after the cancel, want none", f.requestsSeen())
				}
			}
			assertProjectUntouched(t, cfgRaw, gitRaw)
		})
	}
}

// TestInitReinitSkipsTheTerminalQuestion: --reinit is the scripts' consent path — even
// at a terminal it asks and warns nothing, it just re-initializes.
func TestInitReinitSkipsTheTerminalQuestion(t *testing.T) {
	f := newFake(t, "test-key")
	url1, cfgRaw, gitRaw, seen := initializedProject(t, f)

	stdin := &ttyReader{strings.NewReader("")}
	stdout, stderr, err := runClient(t, stdin, "init", "--reinit")
	if err != nil {
		t.Fatalf("init --reinit at a terminal: %v; stderr %s", err, stderr)
	}
	if want := "initialized " + url1 + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q with no question asked", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want no warning when --reinit was passed", stderr)
	}
	if f.requestsSeen() != seen+1 {
		t.Errorf("requests = %d, want the one re-verify request", f.requestsSeen())
	}
	assertProjectUntouched(t, cfgRaw, gitRaw)
}

// TestInitReinitOnFreshDirectory: --reinit on a directory that is not initialized is a
// harmless no-op — an ordinary init.
func TestInitReinitOnFreshDirectory(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")

	stdout, stderr, err := runClient(t, nil, "init", "--reinit")
	if err != nil {
		t.Fatalf("init --reinit on a fresh directory: %v; stderr %s", err, stderr)
	}
	if want := "initialized " + url1 + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	cfg := initConfig(t)
	if cfg.URL != url1 || cfg.APIKey != "test-key" || cfg.Bin != "" || cfg.Protected {
		t.Errorf("%s = %+v, want the ordinary init's file", configFileName, cfg)
	}
	if got := string(gitignoreBytes(t)); got != configFileName+"\n" {
		t.Errorf("%s = %q, want the one line naming the config file", gitignoreFileName, got)
	}
	if f.requestsSeen() != 1 {
		t.Errorf("requests = %d, want the one verify request", f.requestsSeen())
	}
}

// TestInitReinitKeepsBinAndProtected: --reinit over a fully initialized project keeps
// today's semantics — re-verify and rewrite, bin and protected preserved on the same
// url, the file byte-identical.
func TestInitReinitKeepsBinAndProtected(t *testing.T) {
	newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	stdout, stderr, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("protected upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	t.Setenv("SANDBIN_BIN_PASSWORD", "")

	if _, stderr, err := runClient(t, nil, "init", "--url", url1, "--api-key", "test-key"); err != nil {
		t.Fatalf("init: %v; stderr %s", err, stderr)
	}
	firstCfg, _ := readConfigFile(t)

	if _, stderr, err := runClient(t, nil, "init", "--reinit"); err != nil {
		t.Fatalf("init --reinit: %v; stderr %s", err, stderr)
	}
	secondCfg, _ := readConfigFile(t)
	if string(secondCfg) != string(firstCfg) {
		t.Errorf("--reinit rewrote %s:\n%s\nwant it byte-identical to\n%s", configFileName, secondCfg, firstCfg)
	}
	cfg := initConfig(t)
	if cfg.Bin != hash || !cfg.Protected {
		t.Errorf("%s = %+v, want bin %s kept protected", configFileName, cfg, hash)
	}
}

// TestInitPartialConfigIsNotInitialized: the file is the marker and only a full one
// counts — a url-only or key-only .sandbin.yaml gets the ordinary init, with no
// warning and no question even at a terminal.
func TestInitPartialConfigIsNotInitialized(t *testing.T) {
	t.Run("url only", func(t *testing.T) {
		newFake(t, "test-key")
		url1 := os.Getenv("SANDBIN_URL")
		writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\n")

		stdout, stderr, err := runClient(t, nil, "init")
		if err != nil {
			t.Fatalf("init over a url-only file: %v; stderr %s", err, stderr)
		}
		if want := "initialized " + url1 + "\n"; stdout != want {
			t.Errorf("stdout = %q, want the ordinary init with no guard wording", stdout)
		}
		if strings.Contains(stdout+stderr, "re-initialize") {
			t.Errorf("stdout %q / stderr %q, want no guard question on a partial file", stdout, stderr)
		}
		cfg := initConfig(t)
		if cfg.URL != url1 || cfg.APIKey != "test-key" {
			t.Errorf("%s = %+v, want the env key stored next to the file's url", configFileName, cfg)
		}
	})
	t.Run("key only", func(t *testing.T) {
		newFake(t, "test-key")
		url1 := os.Getenv("SANDBIN_URL")
		writeWorkDirFile(t, configFileName, "api_key: test-key\n")

		stdout, stderr, err := runClient(t, nil, "init", "--url", url1)
		if err != nil {
			t.Fatalf("init over a key-only file: %v; stderr %s", err, stderr)
		}
		if want := "initialized " + url1 + "\n"; stdout != want {
			t.Errorf("stdout = %q, want the ordinary init with no guard wording", stdout)
		}
		if strings.Contains(stdout+stderr, "re-initialize") {
			t.Errorf("stdout %q / stderr %q, want no guard question on a partial file", stdout, stderr)
		}
		cfg := initConfig(t)
		if cfg.URL != url1 || cfg.APIKey != "test-key" {
			t.Errorf("%s = %+v, want the flag's url stored next to the file's key", configFileName, cfg)
		}
	})
	t.Run("url only at a terminal asks nothing", func(t *testing.T) {
		newFake(t, "test-key")
		url1 := os.Getenv("SANDBIN_URL")
		writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\n")

		stdin := &ttyReader{strings.NewReader("")}
		stdout, stderr, err := runClient(t, stdin, "init")
		if err != nil {
			t.Fatalf("init over a url-only file at a terminal: %v; stderr %s", err, stderr)
		}
		if want := "initialized " + url1 + "\n"; stdout != want {
			t.Errorf("stdout = %q, want the ordinary init with no guard question", stdout)
		}
		if strings.Contains(stdout+stderr, "re-initialize") {
			t.Errorf("stdout %q / stderr %q, want no guard question on a partial file", stdout, stderr)
		}
	})
}
