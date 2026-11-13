package main

import (
	"bytes"
	"strings"
	"testing"
)

// runDispatch runs one whole command line and returns what it printed and the exit code.
func runDispatch(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := dispatch(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

// TestDispatchNoArgs is the urfave shape: a verb-less run shows the help on stdout
// and succeeds; the refusal stays reserved for misuse that is not just asking.
func TestDispatchNoArgs(t *testing.T) {
	stdout, stderr, code := runDispatch(t)
	if code != 0 {
		t.Errorf("bare sandbin exited %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("bare sandbin stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{"USAGE", "sandbin", "serve", "ip"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("bare sandbin stdout = %q, want the help naming %q", stdout, want)
		}
	}
}

func TestDispatchHelp(t *testing.T) {
	for _, verb := range []string{"help", "--help", "-h"} {
		stdout, stderr, code := runDispatch(t, verb)
		if code != 0 {
			t.Errorf("sandbin %s exited %d, want 0", verb, code)
		}
		if !strings.Contains(stdout, "sandbin") || !strings.Contains(stdout, "serve") || !strings.Contains(stdout, "ip") {
			t.Errorf("sandbin %s stdout = %q, want the help naming the verbs", verb, stdout)
		}
		if stderr != "" {
			t.Errorf("sandbin %s stderr = %q, want nothing", verb, stderr)
		}
	}
}

// TestDispatchHelpSubcommands is urfave's help tree: help names one verb's flags,
// and a help topic that does not exist is refused.
func TestDispatchHelpSubcommands(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "help", "serve")
	if code != 0 {
		t.Errorf("sandbin help serve exited %d, want 0; stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "sandbin serve") || !strings.Contains(stdout, "--api-key") || !strings.Contains(stdout, "--socket") {
		t.Errorf("sandbin help serve stdout = %q, want serve's flag list", stdout)
	}

	stdout, stderr, code = runDispatch(t, "help", "ip")
	if code != 0 {
		t.Errorf("sandbin help ip exited %d, want 0; stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "sandbin ip") {
		t.Errorf("sandbin help ip stdout = %q, want the ip help", stdout)
	}

	stdout, stderr, code = runDispatch(t, "help", "bogus")
	if code != 1 {
		t.Errorf("sandbin help bogus exited %d, want 1", code)
	}
	if !strings.Contains(stderr, `No help topic for 'bogus'`) {
		t.Errorf("sandbin help bogus stderr = %q, want the help-topic refusal", stderr)
	}
	if stdout != "" {
		t.Errorf("sandbin help bogus stdout = %q, want nothing", stdout)
	}
}

func TestDispatchUnknownVerb(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "bogus")
	if code != 1 {
		t.Errorf("sandbin bogus exited %d, want 1", code)
	}
	if !strings.Contains(stderr, `No help topic for 'bogus'`) {
		t.Errorf("sandbin bogus stderr = %q, want a refusal naming it", stderr)
	}
	if stdout != "" {
		t.Errorf("sandbin bogus stdout = %q, want nothing", stdout)
	}
}

// TestDispatchEmptyVerb is the parser quirk accepted with the urfave move: an empty
// verb reads as no verb, which shows the help instead of refusing.
func TestDispatchEmptyVerb(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "")
	if code != 0 {
		t.Errorf(`sandbin "" exited %d, want 0; stderr %q`, code, stderr)
	}
	if !strings.Contains(stdout, "sandbin") {
		t.Errorf(`sandbin "" stdout = %q, want the help`, stdout)
	}

	stdout, stderr, code = runDispatch(t, "ip", "")
	if code != 0 {
		t.Errorf(`sandbin ip "" exited %d, want 0; stderr %q`, code, stderr)
	}
	if !strings.Contains(stdout, "list") {
		t.Errorf(`sandbin ip "" stdout = %q, want the ip help`, stdout)
	}
}

func TestDispatchVersion(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "--version")
	if code != 0 {
		t.Errorf("sandbin --version exited %d, want 0; stderr %q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "sandbin version dev" {
		t.Errorf("sandbin --version stdout = %q, want %q", got, "sandbin version dev")
	}
	if stderr != "" {
		t.Errorf("sandbin --version stderr = %q, want nothing", stderr)
	}
}

// TestDispatchServeHelp covers the --help arm: asking for the flag list is not a
// failure and starts no listener. urfave renders it on stdout.
func TestDispatchServeHelp(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "serve", "--help")
	if code != 0 {
		t.Errorf("sandbin serve --help exited %d, want 0; stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "sandbin serve") {
		t.Errorf("sandbin serve --help stdout = %q, want serve's flag list", stdout)
	}
	for _, want := range []string{"--api-key", "--db", "--addr", "--socket", "--trusted-proxy", "--trusted-proxy-file"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("sandbin serve --help stdout = %q, want the flag list to name %s", stdout, want)
		}
	}
	if stderr != "" {
		t.Errorf("sandbin serve --help stderr = %q, want nothing", stderr)
	}
}

// TestServeFlagParsing checks every existing flag, its default and its env fallback
// without starting a listener.
func TestServeFlagParsing(t *testing.T) {
	var stdout bytes.Buffer
	cfg, err := parseServeFlags([]string{
		"--api-key", "k", "--db", "elsewhere.db", "--addr", "127.0.0.1:8081",
		"--trusted-proxy", "203.0.113.7", "--trusted-proxy-file", "proxies.txt",
	}, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags: %v", err)
	}
	// With no --socket the control socket is the one derived from --db.
	want := serveConfig{
		apiKey: "k", dbPath: "elsewhere.db", addr: "127.0.0.1:8081", socketPath: "elsewhere.db.sock",
		trustedProxy: "203.0.113.7", trustedProxyFile: "proxies.txt",
	}
	if cfg != want {
		t.Errorf("serve flags = %+v, want %+v", cfg, want)
	}

	// With nothing passed the defaults are what serve has always defaulted to.
	cfg, err = parseServeFlags(nil, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with no flags: %v", err)
	}
	want = serveConfig{dbPath: "sandbin.db", addr: ":8080", socketPath: "sandbin.db.sock"}
	if cfg != want {
		t.Errorf("serve defaults = %+v, want %+v", cfg, want)
	}

	// Each flag still falls back to its environment variable.
	t.Setenv("SANDBIN_API_KEY", "env-key")
	t.Setenv("SANDBIN_SOCKET", "env.sock")
	t.Setenv("SANDBIN_TRUSTED_PROXIES", "198.51.100.0/24")
	t.Setenv("SANDBIN_TRUSTED_PROXIES_FILE", "env-proxies.txt")
	cfg, err = parseServeFlags(nil, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with the env set: %v", err)
	}
	want = serveConfig{
		apiKey: "env-key", dbPath: "sandbin.db", addr: ":8080", socketPath: "env.sock",
		trustedProxy: "198.51.100.0/24", trustedProxyFile: "env-proxies.txt",
	}
	if cfg != want {
		t.Errorf("serve env fallbacks = %+v, want %+v", cfg, want)
	}

	// A flag beats the environment variable, the top of the ladder.
	cfg, err = parseServeFlags([]string{"--api-key", "flag-key", "--socket", "flag.sock"}, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with flags over the env: %v", err)
	}
	if cfg.apiKey != "flag-key" || cfg.socketPath != "flag.sock" {
		t.Errorf("serve flag-over-env = api-key %q socket %q, want the flag values", cfg.apiKey, cfg.socketPath)
	}
	if cfg.trustedProxy != "198.51.100.0/24" {
		t.Errorf("serve --trusted-proxy env fallback = %q, want the env value", cfg.trustedProxy)
	}

	// --socket beats the derived default, which is how the socket lives somewhere else
	// than the db.
	cfg, err = parseServeFlags([]string{"--db", "elsewhere.db", "--socket", "/run/sandbin.sock"}, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with --socket: %v", err)
	}
	if cfg.socketPath != "/run/sandbin.sock" {
		t.Errorf("serve --socket = %q, want the path it was given", cfg.socketPath)
	}

	if _, err := parseServeFlags([]string{"--bogus"}, &stdout); err == nil {
		t.Error("serve --bogus: no error, want a refusal")
	}
	if _, err := parseServeFlags([]string{"extra"}, &stdout); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("serve extra: err %v, want a refusal naming it", err)
	}
}

func TestDispatchServeBadFlag(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "serve", "--bogus")
	if code != 1 {
		t.Errorf("sandbin serve --bogus exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined: -bogus") {
		t.Errorf("serve --bogus stderr = %q, want the flag refusal", stderr)
	}
	if !strings.Contains(stderr, "sandbin serve") || !strings.Contains(stderr, "--api-key") {
		t.Errorf("serve --bogus stderr = %q, want serve's flag list", stderr)
	}
	if stdout != "" {
		t.Errorf("serve --bogus stdout = %q, want nothing", stdout)
	}
}

// TestDispatchServeExtraArg keeps the stray-argument refusal: serve reads nothing
// from an argument, so it is refused like an unknown flag.
func TestDispatchServeExtraArg(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "serve", "extra")
	if code != 1 {
		t.Errorf("sandbin serve extra exited %d, want 1", code)
	}
	if !strings.Contains(stderr, `serve takes no arguments, got "extra"`) {
		t.Errorf("serve extra stderr = %q, want the refusal naming it", stderr)
	}
	if stdout != "" {
		t.Errorf("serve extra stdout = %q, want nothing", stdout)
	}
}

// TestDispatchIPVerb shows dispatch hands the ip verbs the live server next to the db file.
func TestDispatchIPVerb(t *testing.T) {
	dbPath, store := newIPControlServer(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	stdout, stderr, code := runDispatch(t, "ip", "list", "--db", dbPath)
	if code != 0 {
		t.Errorf("sandbin ip list exited %d with stderr %q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "203.0.113.7") {
		t.Errorf("sandbin ip list stdout = %q, want the blocked address", stdout)
	}
}
