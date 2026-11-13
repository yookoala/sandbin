package main

import (
	"os"
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

// TestInitReRunIsIdempotent: init re-reads its own file when no flag or env carries a
// value, and a re-run leaves both files byte-identical.
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
	stdout, stderr, err := runClient(t, nil, "init")
	if err != nil {
		t.Fatalf("second init: %v; stderr %s", err, stderr)
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
// beats the file's, and a bin made for the file's server does not travel.
func TestInitEnvUrlOverridesTheFileUrl(t *testing.T) {
	f1 := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\napi_key: test-key\nbin: bin0001\nprotected: true\n")
	f2, url2 := newFakeAt(t, "test-key")
	t.Setenv("SANDBIN_URL", url2)
	t.Setenv("SANDBIN_API_KEY", "test-key")

	if _, stderr, err := runClient(t, nil, "init"); err != nil {
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
