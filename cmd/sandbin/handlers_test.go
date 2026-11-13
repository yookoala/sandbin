package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"
)

func newTestServer(t *testing.T, apiKey string) *server {
	t.Helper()
	store, err := openStore(filepath.Join(t.TempDir(), "sandbin.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &server{store: store, apiKey: []byte(apiKey)}
}

// createTestBin POSTs a new bin and returns its "/abc1234/" path.
func createTestBin(t *testing.T, h http.Handler, apiKey string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST / = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	body := rec.Body.String()
	if !regexp.MustCompile(`^/[0-9a-f]{8}/$`).MatchString(body) {
		t.Fatalf("POST / body %q is not a bin path", body)
	}
	return body
}

// createTestBinWithPassword POSTs a password protected bin and returns its path.
func createTestBinWithPassword(t *testing.T, h http.Handler, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "test-key")
	req.Header.Set("X-Bin-Password", password)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST with a bin password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	return rec.Body.String()
}

// createTestBinWithMetadata POSTs a bin carrying the two metadata headers and returns its
// "/abc1234/" path. An empty argument sends no header at all, which is a bin without metadata.
func createTestBinWithMetadata(t *testing.T, h http.Handler, apiKey, title, description string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", apiKey)
	if title != "" {
		req.Header.Set("X-Bin-Title", title)
	}
	if description != "" {
		req.Header.Set("X-Bin-Description", description)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST with bin metadata = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	body := rec.Body.String()
	if !regexp.MustCompile(`^/[0-9a-f]{8}/$`).MatchString(body) {
		t.Fatalf("POST with bin metadata body %q is not a bin path", body)
	}
	return body
}

// put writes a file with the API key and returns the recorder.
func put(t *testing.T, h http.Handler, path, apiKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	h := newTestServer(t, "test-key")

	// No API key is sent: /healthz must answer anyway.
	rec := get(t, h, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "ok" {
		t.Errorf("GET /healthz body = %q, want %q", got, "ok")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}

	req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD /healthz = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, "/healthz", nil)
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	// The branch must not shadow a real bin: a full round trip still works.
	base := createTestBin(t, h, "test-key")
	if rec := put(t, h, base+"page.html", "test-key", "<h1>Hello</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT after the healthz branch = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if rec := get(t, h, base+"page.html"); rec.Code != http.StatusOK || rec.Body.String() != "<h1>Hello</h1>" {
		t.Errorf("GET after the healthz branch = %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestFrontPageAtRoot(t *testing.T) {
	h := newTestServer(t, "test-key")

	// GET / answers the branded page without any key or charging.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("GET / Content-Type = %q, want %q", got, "text/html; charset=utf-8")
	}
	body := rec.Body.String()
	// The request is a plain loopback GET with Host example.com, so its own address is
	// what the examples echo.
	for _, want := range []string{"<title>Sandbin</title>", "<h1>Sandbin</h1>", "POST /", "&lt;h1&gt;Hello&lt;/h1&gt;",
		"<h2>Give the skill to your agent</h2>",
		"curl -fsSL http://example.com/skills/sandbin-preview/install.sh | bash",
		"--dest",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("front page missing %q in:\n%s", want, body)
		}
	}

	// The front page branch must not shadow creation or the old refusals.
	if base := createTestBin(t, h, "test-key"); base == "" {
		t.Fatal("POST / stopped creating a bin")
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s / = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestFrontPageEchoesTheVisitorsAddress(t *testing.T) {
	h := newTestServer(t, "test-key")
	cases := []struct {
		name       string
		host       string
		forwarded  string // X-Forwarded-Host
		proto      string // X-Forwarded-Proto
		remoteAddr string
		tls        bool
		want       string
	}{
		{"plain loopback", "example.com", "", "", "127.0.0.1:1234", false, "http://example.com"},
		{"tls socket", "bin.example.org", "", "", "127.0.0.1:1234", true, "https://bin.example.org"},
		{"loopback proxy https", "localhost:8080", "behind.example.org", "https", "127.0.0.1:1234", false, "https://behind.example.org"},
		{"proxy proto only", "bin.example.org", "", "https", "127.0.0.1:1234", false, "https://bin.example.org"},
		{"port survives", "bin.example.org:8443", "", "", "127.0.0.1:1234", false, "http://bin.example.org:8443"},
		{"ipv6 socket", "[::1]:8080", "", "", "[::1]:5", false, "http://[::1]:8080"},
		// Headers from a socket no proxy is trusted to be cannot dress up the page.
		{"untrusted socket ignores headers", "direct.example.org", "evil.example.org", "https", "203.0.113.7:9", false, "http://direct.example.org"},
		{"hostile host falls back", "bad host<!<script>", "", "", "127.0.0.1:1234", false, "https://this.host"},
		{"host with a path falls back", "evil.example.org/x", "", "", "127.0.0.1:1234", false, "https://this.host"},
		{"empty host falls back", "", "", "", "127.0.0.1:1234", false, "https://this.host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = c.host
			req.RemoteAddr = c.remoteAddr
			if c.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if c.forwarded != "" {
				req.Header.Set("X-Forwarded-Host", c.forwarded)
			}
			if c.proto != "" {
				req.Header.Set("X-Forwarded-Proto", c.proto)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET / = %d, want %d", rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			for _, want := range []string{c.want + "/skills/sandbin-preview/install.sh", c.want + "/abc1234/index.html"} {
				if !strings.Contains(body, want) {
					t.Errorf("front page missing %q in:\n%s", want, body)
				}
			}
		})
	}
}

func TestInstallScriptRoute(t *testing.T) {
	h := newTestServer(t, "test-key")

	// GET serves the script with the visitor's own base embedded; no template token
	// may survive into the response.
	req := httptest.NewRequest(http.MethodGet, "/skills/sandbin-preview/install.sh", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET install.sh = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("install.sh Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
	body := rec.Body.String()
	if strings.Contains(body, "{{") || strings.Contains(body, "__SANDBIN_BASE__") {
		t.Errorf("install.sh still carries a template token:\n%s", body)
	}
	if !strings.Contains(body, `BASE="http://example.com"`) {
		t.Errorf("install.sh missing the visitor's base in:\n%s", body)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/skills/sandbin-preview/install.sh", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s install.sh = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestInstallScriptBaseFollowsTheVisitor(t *testing.T) {
	h := newTestServer(t, "test-key")
	cases := []struct {
		name       string
		host       string
		forwarded  string
		proto      string
		remoteAddr string
		tls        bool
		want       string
	}{
		{"tls socket", "bin.example.org", "", "", "127.0.0.1:1234", true, "https://bin.example.org"},
		{"loopback proxy", "localhost:8080", "behind.example.org", "https", "127.0.0.1:1234", false, "https://behind.example.org"},
		{"hostile forwarded host falls back", "direct.example.org", "evil.example.org/x", "https", "127.0.0.1:1234", false, "https://this.host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/skills/sandbin-preview/install.sh", nil)
			req.Host = c.host
			req.RemoteAddr = c.remoteAddr
			if c.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if c.forwarded != "" {
				req.Header.Set("X-Forwarded-Host", c.forwarded)
			}
			if c.proto != "" {
				req.Header.Set("X-Forwarded-Proto", c.proto)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET install.sh = %d, want %d", rec.Code, http.StatusOK)
			}
			if want := `BASE="` + c.want + `"`; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("install.sh missing %q in:\n%s", want, rec.Body.String())
			}
		})
	}
}

// TestInstallScriptEndToEnd pipes the served script through bash exactly the way the
// documented one-liner does, against a real listening server. It stands down when the
// sandbox has no bash or curl to pipe with.
func TestInstallScriptEndToEnd(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash to pipe the installer into")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl to serve as the one-liner does")
	}
	h := newTestServer(t, "test-key")
	ts := httptest.NewServer(h)
	defer ts.Close()

	script, err := http.Get(ts.URL + "/skills/sandbin-preview/install.sh")
	if err != nil {
		t.Fatalf("fetch installer: %v", err)
	}
	defer script.Body.Close()
	raw, err := io.ReadAll(script.Body)
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}

	// The script runs against a controlled PATH so the go step is judged without
	// spending a toolchain or a download: a bin dir holding only curl and mkdir
	// stands for a machine without go, a stub go stands for one with it.
	toolDir := t.TempDir()
	for _, tool := range []string{"curl", "mkdir"} {
		toolPath, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("look up %s: %v", tool, err)
		}
		if err := os.Symlink(toolPath, filepath.Join(toolDir, tool)); err != nil {
			t.Fatalf("stub %s: %v", tool, err)
		}
	}

	// pipeWithPATH runs the fetched script exactly as curl | bash -s -- does, under
	// a PATH of the caller's choosing.
	pipeWithPATH := func(home, path string, args ...string) (string, error) {
		cmd := exec.Command(bash, append([]string{"-s", "--"}, args...)...)
		cmd.Env = []string{"HOME=" + home, "PATH=" + path}
		cmd.Stdin = bytes.NewReader(raw)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	pipeInstaller := func(home string, args ...string) (string, error) {
		return pipeWithPATH(home, toolDir, args...)
	}

	home := t.TempDir()
	out, err := pipeInstaller(home)
	if err != nil {
		t.Fatalf("piped installer failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no go on PATH") {
		t.Errorf("install without go did not say so (output:\n%s)", out)
	}
	wantFile := filepath.Join(home, ".agents", "skills", "sandbin-preview", "SKILL.md")
	got, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("default install missing %s: %v (output:\n%s)", wantFile, err, out)
	}
	// The installed skill must match what this very server serves — rendered with
	// this server's address, token-free.
	served, err := http.Get(ts.URL + "/skills/sandbin-preview/SKILL.md")
	if err != nil {
		t.Fatalf("fetch served skill: %v", err)
	}
	defer served.Body.Close()
	rawSkill, err := io.ReadAll(served.Body)
	if err != nil {
		t.Fatalf("read served skill: %v", err)
	}
	if string(got) != string(rawSkill) {
		t.Errorf("installed SKILL.md differs from the served one")
	}
	if strings.Contains(string(got), "{{") {
		t.Errorf("installed SKILL.md still carries a template token")
	}

	home = t.TempDir()
	out, err = pipeInstaller(home, "--dest", filepath.Join(home, "agent-skills"))
	if err != nil {
		t.Fatalf("--dest install failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "agent-skills", "sandbin-preview", "SKILL.md")); err != nil {
		t.Errorf("--dest install missing: %v (output:\n%s)", err, out)
	}

	// With go on PATH the installer must run the same contract the skill documents:
	// GOBIN set to the skill folder and the module installed @latest. A succeeding
	// and a failing stub prove both outcomes, and neither may sink the install.
	goDir := t.TempDir()
	goLog := filepath.Join(t.TempDir(), "go.calls")
	goStub := []byte("#!/bin/sh\necho \"GOBIN=$GOBIN $*\" >>\"" + goLog + "\"\nexit 0\n")
	if err := os.WriteFile(filepath.Join(goDir, "go"), goStub, 0o755); err != nil {
		t.Fatalf("write go stub: %v", err)
	}
	goFailDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goFailDir, "go"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing go stub: %v", err)
	}

	home = t.TempDir()
	out, err = pipeWithPATH(home, goDir+string(os.PathListSeparator)+toolDir, "--dest", filepath.Join(home, "agent-skills"))
	if err != nil {
		t.Fatalf("install with a working go failed: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(goLog)
	if err != nil {
		t.Fatalf("installer never called go (output:\n%s)", out)
	}
	wantCall := "GOBIN=" + filepath.Join(home, "agent-skills", "sandbin-preview") +
		" install github.com/yookoala/sandbin/cmd/sandbin-client@latest"
	if !strings.Contains(string(calls), wantCall) {
		t.Errorf("go call = %q, want it to contain %q", calls, wantCall)
	}
	if !strings.Contains(out, "sandbin-client (go install)") {
		t.Errorf("working go install not reported (output:\n%s)", out)
	}

	home = t.TempDir()
	out, err = pipeWithPATH(home, goFailDir+string(os.PathListSeparator)+toolDir)
	if err != nil {
		t.Errorf("failing go install sank the installer: %v\n%s", err, out)
	}
	if !strings.Contains(out, "go install failed") {
		t.Errorf("failing go install not reported (output:\n%s)", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "sandbin-preview", "SKILL.md")); err != nil {
		t.Errorf("go failure lost the skill install: %v", err)
	}

	home = t.TempDir()
	out, err = pipeInstaller(home, "--help")
	if err != nil {
		t.Fatalf("--help failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "usage:") {
		t.Errorf("--help output missing usage: %s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Errorf("--help installed something anyway")
	}

	home = t.TempDir()
	out, err = pipeInstaller(home, "--nonsense")
	if err == nil {
		t.Errorf("unknown argument accepted (output:\n%s)", out)
	}
	if !strings.Contains(out, "unknown argument") {
		t.Errorf("unknown argument refusal missing: %s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Errorf("unknown-argument run installed something anyway")
	}
}

func TestPostPutGetRoundTrip(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	rec := put(t, h, base+"page.html", "test-key", "<h1>Hello</h1>")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT new file = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if want := base + "page.html"; rec.Body.String() != want {
		t.Errorf("PUT body = %q, want %q", rec.Body.String(), want)
	}

	rec = get(t, h, base+"page.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "<h1>Hello</h1>" {
		t.Errorf("GET body = %q, want %q", got, "<h1>Hello</h1>")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}

	rec = put(t, h, base+"page.html", "test-key", "<h1>Replaced</h1>")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT replacing a file = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = put(t, h, base+"notes.md", "test-key", "# Notes\n")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT notes.md = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, h, base+"notes.md")
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("markdown Content-Type = %q, want text/plain; charset=utf-8", ct)
	}

	rec = put(t, h, base+"blob.dat", "test-key", "binary\x00data")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT blob.dat = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, h, base+"blob.dat")
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("NUL-byte Content-Type = %q, want application/octet-stream", ct)
	}
	if got := rec.Body.String(); got != "binary\x00data" {
		t.Errorf("blob body = %q, want the stored bytes back", got)
	}

	rec = get(t, h, base+"sub/deep/file.txt")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET unknown path = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPutUnknownBin(t *testing.T) {
	h := newTestServer(t, "test-key")

	rec := put(t, h, "/deadbeef/page.html", "test-key", "x")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown bin = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetUnknownBin(t *testing.T) {
	h := newTestServer(t, "test-key")

	rec := get(t, h, "/deadbeef/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown bin = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown bin") {
		t.Errorf("body = %q, want an unknown bin message", rec.Body.String())
	}
}

func TestWrongAPIKey(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	// Each attempt arrives from a fresh address, because only a first failure
	// from an address answers 403; escalation has its own tests below.
	rec := postFrom(t, h, "198.51.100.1:1234", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	rec = putFrom(t, h, "198.51.100.2:1234", base+"page.html", "wrong", "x")
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT with wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = putFrom(t, h, "198.51.100.3:1234", base+"page.html", "", "x")
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT with no key = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	req := httptest.NewRequest(http.MethodDelete, base+"page.html", nil)
	req.Header.Set("X-API-Key", "test-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	req = httptest.NewRequest(http.MethodPost, base+"page.html", nil)
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST under a bin = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestOversizeBody(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	oversize := strings.Repeat("x", maxBodyBytes+1)
	rec := put(t, h, base+"big.txt", "test-key", oversize)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT oversized body = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1 MiB") {
		t.Errorf("body = %q, want it to name the size limit", rec.Body.String())
	}

	rec = put(t, h, base+"big.txt", "test-key", strings.Repeat("x", 1<<20))
	if rec.Code != http.StatusCreated {
		t.Errorf("PUT at the 1 MiB limit = %d, want %d", rec.Code, http.StatusCreated)
	}

	rec = get(t, h, base+"big.txt")
	body, _ := io.ReadAll(rec.Body)
	if len(body) != 1<<20 {
		t.Errorf("stored file is %d bytes, want %d", len(body), 1<<20)
	}
}

func TestDirectoryListing(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	rec := get(t, h, base)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET empty bin = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("listing Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if !strings.Contains(rec.Body.String(), "empty bin") {
		t.Errorf("empty bin listing = %q, want it to say the bin is empty", rec.Body.String())
	}

	for _, path := range []string{"b.txt", "sub/a.md", "x<y.txt"} {
		if rec := put(t, h, base+path, "test-key", "body of "+path); rec.Code != http.StatusCreated {
			t.Fatalf("PUT %q = %d, want %d", path, rec.Code, http.StatusCreated)
		}
	}

	rec = get(t, h, base)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET bin base = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<ul>`,
		`<li><a href="./b.txt">b.txt</a> (13 bytes,`,
		`<li><a href="./sub/a.md">sub/a.md</a> (16 bytes,`,
		`<li><a href="./x&lt;y.txt">x&lt;y.txt</a>`,
		base,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("listing does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<x<y.txt>") {
		t.Errorf("listing escaped the filename wrong:\n%s", body)
	}
}

func TestIndexHTMLWinsOverListing(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	if rec := put(t, h, base+"page.html", "test-key", "<p>other</p>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT page.html = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec := put(t, h, base+"index.html", "test-key", "<h1>Custom index</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT index.html = %d, want %d", rec.Code, http.StatusCreated)
	}

	rec := get(t, h, base)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET bin base = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "<h1>Custom index</h1>" {
		t.Errorf("GET bin base body = %q, want the stored index.html verbatim", got)
	}
}

func TestBinPassword(t *testing.T) {
	h := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "test-key")
	req.Header.Set("X-Bin-Password", "sesame")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST with a bin password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	base := rec.Body.String()

	rec = get(t, h, base+"page.html")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without the bin password = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if www := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(www, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want a Basic challenge", www)
	}

	rec = get(t, h, base)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET the listing without the bin password = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	rec = put(t, h, base+"page.html", "test-key", "<p>x</p>")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("PUT without the bin password = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPut, base+"page.html", strings.NewReader("<h1>Secret</h1>"))
	req.Header.Set("X-API-Key", "test-key")
	req.SetBasicAuth(strings.Trim(base, "/"), "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PUT with a wrong bin password = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPut, base+"page.html", strings.NewReader("<h1>Secret</h1>"))
	req.Header.Set("X-API-Key", "test-key")
	req.SetBasicAuth("anyone", "sesame")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT with the bin password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec = get(t, h, base+"page.html")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without the password on a protected bin = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, base+"page.html", nil)
	req.SetBasicAuth("anyone", "sesame")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET with the bin password = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "<h1>Secret</h1>" {
		t.Errorf("body = %q, want %q", got, "<h1>Secret</h1>")
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	req.SetBasicAuth("anyone", "sesame")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the listing with the bin password = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "page.html") {
		t.Errorf("protected listing = %q, want it to list page.html", rec.Body.String())
	}
}

func TestBinPasswordTooLongIsRejected(t *testing.T) {
	h := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "test-key")
	req.Header.Set("X-Bin-Password", strings.Repeat("p", 100))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST with a 100 byte bin password = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "test-key")
	req.Header.Set("X-Bin-Password", strings.Repeat("p", 72))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST with a 72 byte bin password = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestPUTNeedsTheAPIKeyEvenWithTheBinPassword(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBinWithPassword(t, h, "sesame")

	req := httptest.NewRequest(http.MethodPut, base+"page.html", strings.NewReader("<p>x</p>"))
	req.SetBasicAuth("anyone", "sesame")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PUT with the bin password but no API key = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestBinPasswordLongerThanBcryptCanCompareIsUnauthorized(t *testing.T) {
	h := newTestServer(t, "test-key")
	// bcrypt only ever compares the first 72 bytes, so an 80 byte candidate whose
	// first 72 bytes match must still be refused.
	base := createTestBinWithPassword(t, h, strings.Repeat("p", 72))

	req := httptest.NewRequest(http.MethodGet, base, nil)
	req.SetBasicAuth("anyone", strings.Repeat("p", 80))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET with a longer matching prefix = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestANSIFileIsRendered(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	body := "\x1b[1mReport\x1b[0m done\n"
	if rec := put(t, h, base+"report.ansi", "test-key", body); rec.Code != http.StatusCreated {
		t.Fatalf("PUT report.ansi = %d, want %d", rec.Code, http.StatusCreated)
	}

	rec := get(t, h, base+"report.ansi")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET report.ansi = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	want := `<pre class="ansi"><span style="font-weight:bold">Report</span> done` + "\n</pre>"
	if got := rec.Body.String(); got != want {
		t.Errorf("rendered body = %q, want %q", got, want)
	}
}

// TestJavaScriptAndCSSContentTypes is the boundary the JS prototype needs: .js and .css
// come back as themselves so a browser runs and applies them, while extensions nobody
// asked for stay text/plain. A stored NUL byte still beats any extension, which
// TestPostPutGetRoundTrip asserts for blob.dat.
func TestJavaScriptAndCSSContentTypes(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	for _, want := range []struct {
		path string
		body string
		ct   string
	}{
		{"app.js", "document.title = 'x'\n", "text/javascript; charset=utf-8"},
		{"sub/app.mjs", "export const x = 1\n", "text/javascript; charset=utf-8"},
		{"style.css", "body { color: red }\n", "text/css; charset=utf-8"},
		{"App.JS", "an upper case extension is the same file\n", "text/javascript; charset=utf-8"},
		{"data.json", "{\"x\": 1}\n", "text/plain; charset=utf-8"},
		{"icon.png", "\x89PNG a fake png with no NUL byte\n", "text/plain; charset=utf-8"},
	} {
		if rec := put(t, h, base+want.path, "test-key", want.body); rec.Code != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want %d; body %q", want.path, rec.Code, http.StatusCreated, rec.Body.String())
		}
		rec := get(t, h, base+want.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want %d; body %q", want.path, rec.Code, http.StatusOK, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != want.ct {
			t.Errorf("GET %s Content-Type = %q, want %q", want.path, ct, want.ct)
		}
	}
}

func TestPathTraversal(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")

	for _, bad := range []string{
		"../secret",
		"a/../../secret",
		"a/../../x",
		"..",
		"../other/page.html",
		"./x",
		"a//b",
		"a/",
		"a/\\b",
		"/abs",
		strings.Repeat("a/", 20) + "x",
		strings.Repeat("a", maxPathLen+1),
	} {
		rec := put(t, h, base+bad, "test-key", "x")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %q = %d, want %d; body %q", bad, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		rec = get(t, h, base+bad)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q = %d, want %d", bad, rec.Code, http.StatusBadRequest)
		}
	}
}

// postFrom POSTs "/" from a fixed socket address; an empty apiKey omits the header.
func postFrom(t *testing.T, h http.Handler, remoteAddr, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = remoteAddr
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// putFrom PUTs under a bin from a fixed socket address; an empty apiKey omits the header.
func putFrom(t *testing.T, h http.Handler, remoteAddr, path, apiKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// getFrom GETs a path from a fixed socket address; an empty apiKey omits the header.
func getFrom(t *testing.T, h http.Handler, remoteAddr, path, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// patchFrom PATCHes a path from a fixed socket address; an empty apiKey omits the header.
// headers names which metadata headers to send, so a field that should stay absent is simply
// not in the map, and an empty value in it clears that field. The body is what a PATCH ignores.
func patchFrom(t *testing.T, h http.Handler, remoteAddr, path, apiKey string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader("a body a PATCH ignores"))
	req.RemoteAddr = remoteAddr
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// retryAfter parses the recorder's Retry-After header.
func retryAfter(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	value := rec.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		t.Fatalf("Retry-After = %q, want a positive whole number of seconds", value)
	}
	return seconds
}

// storedBlock reads the ip_blocks row for ip and reports the failure count and
// how long the stored block lasts, blocked_until minus updated_at.
func storedBlock(t *testing.T, s *server, ip string) (int, time.Duration) {
	t.Helper()
	var failures int
	var untilRaw, updatedRaw string
	err := s.store.db.QueryRow(`SELECT failures, blocked_until, updated_at FROM ip_blocks WHERE ip=?`, ip).
		Scan(&failures, &untilRaw, &updatedRaw)
	if err != nil {
		t.Fatalf("read ip_blocks row for %q: %v", ip, err)
	}
	until, err := time.Parse(time.RFC3339Nano, untilRaw)
	if err != nil {
		t.Fatalf("parse blocked_until %q: %v", untilRaw, err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedRaw)
	if err != nil {
		t.Fatalf("parse updated_at %q: %v", updatedRaw, err)
	}
	return failures, until.Sub(updated)
}

func TestWrongKeyBlocksAddress(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := postFrom(t, s, "203.0.113.7:1234", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("first wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra != "" {
		t.Errorf("403 carries Retry-After %q, want none", ra)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.7"); failures != 1 || dur != blockStep {
		t.Errorf("after the first failure = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// The address is now blocked, even with the correct key.
	rec = postFrom(t, s, "203.0.113.7:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked POST = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too many bad API keys") {
		t.Errorf("429 body = %q, want the too many bad API keys message", rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "203.0.113.7"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked attempt = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}
	ra1 := retryAfter(t, rec)
	if ra1 <= 1800-30 || ra1 > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra1)
	}

	// Attempting while blocked escalates to the second block.
	rec = postFrom(t, s, "203.0.113.7:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second blocked POST = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	ra2 := retryAfter(t, rec)
	if ra2 <= ra1 {
		t.Errorf("Retry-After went from %d to %d; the block must grow while hammered", ra1, ra2)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.7"); failures != 3 || dur != 3*blockStep {
		t.Errorf("after the third failure = failures %d, block %v; want 3 failures, %v", failures, dur, 3*blockStep)
	}

	// An unrelated address is untouched by another one's block.
	if rec := postFrom(t, s, "198.51.100.9:1234", "test-key"); rec.Code != http.StatusCreated {
		t.Errorf("unrelated POST = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestMissingAPIKeyCountsAsFailure(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := postFrom(t, s, "203.0.113.8:1234", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without the header = %d, want %d", rec.Code, http.StatusForbidden)
	}
	rec = postFrom(t, s, "203.0.113.8:1234", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second header-less POST = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.8"); failures != 2 || dur != 2*blockStep {
		t.Errorf("missing headers = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}
}

func TestBlockedAddressKeepsGetService(t *testing.T) {
	s := newTestServer(t, "test-key")

	const addr = "198.51.100.5:1234"
	rec := postFrom(t, s, addr, "test-key")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bin = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	base := rec.Body.String()
	if rec := putFrom(t, s, addr, base+"notes.md", "test-key", "# Notes"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT notes.md = %d, want %d", rec.Code, http.StatusCreated)
	}

	// One bad key blocks the address from writes.
	if rec := postFrom(t, s, addr, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}

	// GETs stay open for the blocked address, even with a wrong key attached.
	rec = getFrom(t, s, addr, base, "wrong")
	if rec.Code != http.StatusOK {
		t.Errorf("blocked GET listing = %d, want %d", rec.Code, http.StatusOK)
	} else if !strings.Contains(rec.Body.String(), "notes.md") {
		t.Errorf("listing = %q, want it to list notes.md", rec.Body.String())
	}
	if rec := getFrom(t, s, addr, base+"notes.md", ""); rec.Code != http.StatusOK || rec.Body.String() != "# Notes" {
		t.Errorf("blocked GET file = %d, body %q; want 200 and the stored body", rec.Code, rec.Body.String())
	}
	// A GET never charges, so the row still holds exactly one failure.
	if failures, dur := storedBlock(t, s, "198.51.100.5"); failures != 1 || dur != blockStep {
		t.Errorf("GETs charged the block = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// The blocked write is refused with an honest wait.
	rec = putFrom(t, s, addr, base+"other.md", "test-key", "x")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked PUT = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("blocked PUT Retry-After = %d, want about 1800", ra)
	}
}

func TestPutFailureEscalates(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	const addr = "198.51.100.6:1234"
	rec := putFrom(t, s, addr, base+"page.html", "wrong", "x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PUT with wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.6"); failures != 1 || dur != blockStep {
		t.Errorf("PUT failure = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	rec = putFrom(t, s, addr, base+"page.html", "wrong", "x")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked PUT = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	ra1 := retryAfter(t, rec)
	if ra1 <= 1800-30 || ra1 > 1800 {
		t.Errorf("Retry-After = %d, want about 1800", ra1)
	}

	rec = putFrom(t, s, addr, base+"page.html", "wrong", "x")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("hammered PUT = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	ra2 := retryAfter(t, rec)
	if ra2 <= ra1 {
		t.Errorf("PUT Retry-After went from %d to %d; want it to grow", ra1, ra2)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.6"); failures != 3 || dur != 3*blockStep {
		t.Errorf("after three PUT failures = failures %d, block %v; want 3 failures, %v", failures, dur, 3*blockStep)
	}
}

func TestBlockSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	s := &server{store: store, apiKey: []byte("test-key")}
	if rec := postFrom(t, s, "203.0.113.9:1234", "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("first wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	restarted, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { restarted.Close() })
	s2 := &server{store: restarted, apiKey: []byte("test-key")}

	rec := postFrom(t, s2, "203.0.113.9:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("POST after a restart = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After after the restart = %d, want about 1800", ra)
	}
}

func TestForgedXFFFromPublicAddress(t *testing.T) {
	s := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.4")
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forged-XFF POST = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if _, blocked := s.store.IsBlocked("8.8.8.8", time.Now()); !blocked {
		t.Errorf("8.8.8.8 not charged although it held the socket")
	}
	if _, blocked := s.store.IsBlocked("198.51.100.4", time.Now()); blocked {
		t.Errorf("198.51.100.4 blocked although it only appeared in a forged header")
	}

	// Rotating forged headers cannot buy a fresh identity.
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second POST from 8.8.8.8 = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	retryAfter(t, rec)

	// The address named in the forged header is unaffected.
	if rec := postFrom(t, s, "198.51.100.4:1234", "test-key"); rec.Code != http.StatusCreated {
		t.Errorf("POST from the named address = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestLocalProxyXFFChargesRightmost(t *testing.T) {
	s := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.4")
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("local-proxy POST = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if _, blocked := s.store.IsBlocked("198.51.100.4", time.Now()); !blocked {
		t.Errorf("198.51.100.4 not charged, although it is the rightmost X-Forwarded-For entry")
	}
	if _, blocked := s.store.IsBlocked("203.0.113.7", time.Now()); blocked {
		t.Errorf("203.0.113.7 charged, although only the rightmost header entry is believed")
	}
	if _, blocked := s.store.IsBlocked("127.0.0.1", time.Now()); blocked {
		t.Errorf("127.0.0.1 charged, although the header made it a proxy hop")
	}

	// The proxy address itself is a different identity and stays allowed.
	if rec := postFrom(t, s, "127.0.0.1:1234", "test-key"); rec.Code != http.StatusCreated {
		t.Errorf("header-less POST from 127.0.0.1 = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestTrustedProxyChargesHeaderAddress is the lockout asserted end to end through a
// configured public proxy: one wrong key from a header-named address blocks that
// address, not the proxy, and the proxy itself keeps working.
func TestTrustedProxyChargesHeaderAddress(t *testing.T) {
	proxies, err := loadTrustedProxies("198.51.100.0/24", "")
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	s := newTestServer(t, "test-key")
	s.trustedProxies = proxies

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.9:43210" // a socket inside the trusted range
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key behind the trusted proxy = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "203.0.113.9"); failures != 1 || dur != blockStep {
		t.Errorf("charged address = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}
	if _, blocked := s.store.IsBlocked("198.51.100.9", time.Now()); blocked {
		t.Errorf("the proxy socket 198.51.100.9 was blocked, although its header named 203.0.113.9")
	}

	// The blocked client address is refused even behind the proxy, and hammering escalates.
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.9:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the blocked client behind the proxy = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After = %d, want about 1800", ra)
	}

	// The proxy socket is a different identity and still works behind its own proxy.
	if rec := postFrom(t, s, "198.51.100.9:9999", "test-key"); rec.Code != http.StatusCreated {
		t.Errorf("header-less POST from the proxy socket = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestUntrustedPublicSocketChargesSocket keeps the plan 0003 outcome asserted for the
// same request shape when the range is not configured: the header never moves the penalty.
func TestUntrustedPublicSocketChargesSocket(t *testing.T) {
	s := newTestServer(t, "test-key") // no trusted proxies configured at all

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.9:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key from an untrusted public socket = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.9"); failures != 1 || dur != blockStep {
		t.Errorf("charged socket = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}
	if _, blocked := s.store.IsBlocked("203.0.113.9", time.Now()); blocked {
		t.Errorf("203.0.113.9 was blocked, although it only appeared in an untrusted header")
	}

	// Rotating the header cannot buy the socket a fresh identity.
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.9:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second POST from the same socket = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	retryAfter(t, rec)
}

// storedBinCount counts the rows of bins, which is how the refused-creation tests prove no
// bin was made behind the refusal.
func storedBinCount(t *testing.T, s *server) int {
	t.Helper()
	var count int
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&count); err != nil {
		t.Fatalf("count bins: %v", err)
	}
	return count
}

// storedMeta reads back the metadata a bin holds, so every metadata test can say what the
// row actually says after the request it just made.
func storedMeta(t *testing.T, s *server, base string) (string, string) {
	t.Helper()
	bin, ok, err := s.store.GetBin(strings.Trim(base, "/"))
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", base, ok, err)
	}
	return bin.Title, bin.Description
}

func TestPostBinMetadata(t *testing.T) {
	s := newTestServer(t, "test-key")

	base := createTestBinWithMetadata(t, s, "test-key", "Test report", "findings of the audit")
	if title, description := storedMeta(t, s, base); title != "Test report" || description != "findings of the audit" {
		t.Errorf("stored metadata = %q / %q, want the headers the POST carried", title, description)
	}

	// A bin made without the headers is the same bin as ever: empty metadata.
	plain := createTestBin(t, s, "test-key")
	if title, description := storedMeta(t, s, plain); title != "" || description != "" {
		t.Errorf("metadata of a header-less bin = %q / %q, want both empty", title, description)
	}
}

// TestPostRefusesBadMetadata is the cap and the single-line rule at creation: a refusal
// answers 400 with the wording the README carries, and no bin is created.
func TestPostRefusesBadMetadata(t *testing.T) {
	s := newTestServer(t, "test-key")
	before := storedBinCount(t, s)

	for _, bad := range []struct {
		title, description, want string
	}{
		{strings.Repeat("t", maxTitleBytes+1), "", "title over 200 bytes\n"},
		// 101 two-byte characters are 202 bytes, so the cap counts bytes and not characters.
		{strings.Repeat("é", maxTitleBytes/2+1), "", "title over 200 bytes\n"},
		{"", strings.Repeat("d", maxDescriptionBytes+1), "description over 2000 bytes\n"},
		{"a\nb", "", "title may not contain control characters\n"},
		{"a\tb", "", "title may not contain control characters\n"},
		{"a\x7fb", "", "title may not contain control characters\n"},
		{"fine title", "a\nb", "description may not contain control characters\n"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-API-Key", "test-key")
		req.Header.Set("X-Bin-Title", bad.title)
		req.Header.Set("X-Bin-Description", bad.description)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST with title %q / description %q = %d, want %d; body %q",
				bad.title, bad.description, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != bad.want {
			t.Errorf("POST with title %q body = %q, want %q", bad.title, got, bad.want)
		}
	}

	if after := storedBinCount(t, s); after != before {
		t.Errorf("bins after the refusals = %d, want the same %d as before", after, before)
	}

	// Exactly at the cap is accepted.
	if base := createTestBinWithMetadata(t, s, "test-key",
		strings.Repeat("t", maxTitleBytes), strings.Repeat("d", maxDescriptionBytes)); storedBinCount(t, s) != before+1 {
		t.Errorf("bins after a bin at the cap = %d, want %d", storedBinCount(t, s), before+1)
	} else if title, description := storedMeta(t, s, base); title == "" || description == "" {
		t.Errorf("metadata at the cap = %d / %d bytes, want both stored", len(title), len(description))
	}
}

// TestPatchBinMetadataCombinations is the semantics table: for each header on its own, a
// present header replaces, an absent one leaves, an empty value clears. The json answer is
// the row as it now stands, read back after every PATCH.
func TestPatchBinMetadataCombinations(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "first title", "first description")
	const addr = "198.51.100.11:1234"

	for i, tc := range []struct {
		headers                              map[string]string
		wantTitle, wantDescription, wantBody string
	}{
		// Neither header sent: nothing changes.
		{nil, "first title", "first description", `{"title":"first title","description":"first description"}`},
		// One header replaces its field and says nothing about the other.
		{map[string]string{"X-Bin-Title": "second title"}, "second title", "first description", `{"title":"second title","description":"first description"}`},
		{map[string]string{"X-Bin-Description": "second description"}, "second title", "second description", `{"title":"second title","description":"second description"}`},
		{map[string]string{"X-Bin-Title": "third title", "X-Bin-Description": "third description"}, "third title", "third description", `{"title":"third title","description":"third description"}`},
		// An empty value clears its field and leaves the other alone.
		{map[string]string{"X-Bin-Title": ""}, "", "third description", `{"title":"","description":"third description"}`},
		{map[string]string{"X-Bin-Description": ""}, "", "", `{"title":"","description":""}`},
		// A cleared field stays cleared when the next PATCH does not mention it.
		{map[string]string{"X-Bin-Title": "fourth title"}, "fourth title", "", `{"title":"fourth title","description":""}`},
	} {
		rec := patchFrom(t, s, addr, base, "test-key", tc.headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH #%d = %d, want %d; body %q", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("PATCH #%d Content-Type = %q, want application/json", i+1, ct)
		}
		if want := tc.wantBody + "\n"; rec.Body.String() != want {
			t.Errorf("PATCH #%d body = %q, want %q", i+1, rec.Body.String(), want)
		}
		if title, description := storedMeta(t, s, base); title != tc.wantTitle || description != tc.wantDescription {
			t.Errorf("after PATCH #%d: stored metadata = %q / %q, want %q / %q",
				i+1, title, description, tc.wantTitle, tc.wantDescription)
		}
	}
}

func TestPatchUnknownBinAndSubPath(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	rec := patchFrom(t, s, "198.51.100.12:1234", "/deadbeef/", "test-key", map[string]string{"X-Bin-Title": "x"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH unknown bin = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	// Only the bin base takes a PATCH: a sub path keeps answering 405.
	if rec := patchFrom(t, s, "198.51.100.12:1234", base+"page.html", "test-key", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH a sub path = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	// The body is ignored: a PATCH stores no file and leaves the bin's files as they were.
	if rec := patchFrom(t, s, "198.51.100.12:1234", base, "test-key", map[string]string{"X-Bin-Title": "a title"}); rec.Code != http.StatusOK {
		t.Fatalf("PATCH with a body = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	files, err := s.store.ListFiles(strings.Trim(base, "/"))
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("the PATCH body left %d files behind, want none", len(files))
	}
}

// TestPatchOverLongMetadataChangesNothing is the PATCH half of the cap: the refusal is a 400
// and the row keeps what it already held.
func TestPatchOverLongMetadataChangesNothing(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "kept title", "kept description")

	rec := patchFrom(t, s, "198.51.100.14:1234", base, "test-key", map[string]string{
		"X-Bin-Title":       strings.Repeat("t", maxTitleBytes+1),
		"X-Bin-Description": strings.Repeat("d", maxDescriptionBytes+1),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with over-long metadata = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := rec.Body.String(); got != "title over 200 bytes\n" {
		t.Errorf("body = %q, want the title refusal", got)
	}

	rec = patchFrom(t, s, "198.51.100.14:1234", base, "test-key", map[string]string{"X-Bin-Title": "a\nnewline"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with a control character = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := rec.Body.String(); got != "title may not contain control characters\n" {
		t.Errorf("body = %q, want the control character refusal", got)
	}

	if title, description := storedMeta(t, s, base); title != "kept title" || description != "kept description" {
		t.Errorf("stored metadata after the refusals = %q / %q, want the stored values unchanged", title, description)
	}
}

// TestPatchWrongKeyChargesTheAddress keeps the lockout rules for PATCH exactly what they are
// for POST and PUT: one bad key is a charged 403, a blocked attempt is a charged 429, and
// neither writes anything.
func TestPatchWrongKeyChargesTheAddress(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "kept title", "kept description")
	const addr = "203.0.113.20:1234"

	rec := patchFrom(t, s, addr, base, "wrong", map[string]string{"X-Bin-Title": "written?"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "203.0.113.20"); failures != 1 || dur != blockStep {
		t.Errorf("after a PATCH failure = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	rec = patchFrom(t, s, addr, base, "test-key", map[string]string{"X-Bin-Title": "written?"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("PATCH from the blocked address = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "203.0.113.20"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked PATCH = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}
	retryAfter(t, rec)

	rec = patchFrom(t, s, addr, base, "test-key", map[string]string{"X-Bin-Title": "written?"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("hammered PATCH = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.20"); failures != 3 || dur != 3*blockStep {
		t.Errorf("after three PATCH failures = failures %d, block %v; want 3 failures, %v", failures, dur, 3*blockStep)
	}

	if title, description := storedMeta(t, s, base); title != "kept title" || description != "kept description" {
		t.Errorf("stored metadata after the charged refusals = %q / %q, want it unchanged", title, description)
	}
}

// TestPatchProtectedBinChargesNothing is the PUT rule extended to PATCH: a wrong bin password
// is a 401 that does not touch the block table.
func TestPatchProtectedBinChargesNothing(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithPassword(t, s, "sesame")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.13:1234"

	rec := patchFrom(t, s, addr, base, "test-key", map[string]string{"X-Bin-Title": "quiet title"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH without the bin password = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if www := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(www, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want a Basic challenge", www)
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.13"); ok {
		t.Errorf("a wrong bin password charged 198.51.100.13, want no row in ip_blocks")
	}
	if title, _ := storedMeta(t, s, base); title != "" {
		t.Errorf("stored title after the 401 = %q, want the bin unchanged", title)
	}

	req := httptest.NewRequest(http.MethodPatch, base, nil)
	req.RemoteAddr = addr
	req.Header.Set("X-API-Key", "test-key")
	req.Header.Set("X-Bin-Title", "quiet title")
	req.SetBasicAuth(hash, "sesame")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH with the bin password = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != `{"title":"quiet title","description":""}`+"\n" {
		t.Errorf("body = %q, want the resulting row", rec.Body.String())
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.13"); ok {
		t.Errorf("the working PATCH charged 198.51.100.13, want no row in ip_blocks")
	}
}

// TestGetBinBaseCarriesMetadata is the machine route for metadata: the two headers answer a
// bin base GET, on the raw index.html branch as well as the listing one, and a sub-path GET
// carries neither.
func TestGetBinBaseCarriesMetadata(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "Test report", "findings of the audit")

	rec := get(t, s, base)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the bin base = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "Test report" {
		t.Errorf("X-Bin-Title = %q, want the stored title", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "findings of the audit" {
		t.Errorf("X-Bin-Description = %q, want the stored description", got)
	}

	if rec := put(t, s, base+"notes.md", "test-key", "# Notes"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT notes.md = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, s, base+"notes.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET notes.md = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "" {
		t.Errorf("X-Bin-Title on a sub path = %q, want no metadata there", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "" {
		t.Errorf("X-Bin-Description on a sub path = %q, want no metadata there", got)
	}

	// The raw index.html shortcut keeps the page verbatim and adds only the headers.
	if rec := put(t, s, base+"index.html", "test-key", "<h1>Custom index</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT index.html = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, s, base)
	if got := rec.Body.String(); got != "<h1>Custom index</h1>" {
		t.Errorf("body of the raw index.html = %q, want it served unmodified", got)
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "Test report" {
		t.Errorf("X-Bin-Title on the raw index.html = %q, want the stored title", got)
	}

	// Empty metadata sends no header, on either branch.
	plain := createTestBin(t, s, "test-key")
	rec = get(t, s, plain) // the listing branch
	if got := rec.Header().Get("X-Bin-Title"); got != "" {
		t.Errorf("X-Bin-Title of a bin with no title = %q, want it absent", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "" {
		t.Errorf("X-Bin-Description of a bin with no description = %q, want it absent", got)
	}
	if rec := put(t, s, plain+"index.html", "test-key", "<p>x</p>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT the plain bin's index.html = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, s, plain) // the raw index.html branch
	if got := rec.Body.String(); got != "<p>x</p>" {
		t.Errorf("body = %q, want the stored index.html verbatim", got)
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "" {
		t.Errorf("X-Bin-Title on a plain bin's raw index.html = %q, want it absent", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "" {
		t.Errorf("X-Bin-Description on a plain bin's raw index.html = %q, want it absent", got)
	}
}

// TestListingShowsBinMetadata compares each listing body byte for byte: a bin with nothing to
// say is listed exactly as it was listed before metadata existed, and a bin with metadata gains
// one styled paragraph above an otherwise unchanged file list.
func TestListingShowsBinMetadata(t *testing.T) {
	s := newTestServer(t, "test-key")

	const head = "<!doctype html>\n<meta charset=\"utf-8\">\n"
	const style = "<style>.bin-meta{color:#555;margin:0 0 1em}</style>\n"
	for _, tc := range []struct {
		title, description string
		meta               string // the HTML the listing must carry, empty for no paragraph
	}{
		{
			`x" onload="y`,
			"<script>alert(1)</script> & ünïcödé",
			"<strong>x&#34; onload=&#34;y</strong><br>&lt;script&gt;alert(1)&lt;/script&gt; &amp; ünïcödé",
		},
		{"Just a title", "", "<strong>Just a title</strong>"},
		{"", "Just a description", "Just a description"},
		{"", "", ""},
	} {
		base := createTestBinWithMetadata(t, s, "test-key", tc.title, tc.description)
		rec := get(t, s, base)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET the listing of bin %q = %d, want %d", base, rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("listing Content-Type = %q, want text/html; charset=utf-8", ct)
		}

		want := head + "<title>/%s/</title>\n<h1>/%s/</h1>\n"
		if tc.meta != "" {
			want += style + "<p class=\"bin-meta\">" + tc.meta + "</p>\n"
		}
		want += "<p>empty bin</p>\n"
		want = fmt.Sprintf(want, strings.Trim(base, "/"), strings.Trim(base, "/"))
		if got := rec.Body.String(); got != want {
			t.Errorf("listing of bin %q with metadata %q / %q =\n%q\nwant\n%q", base, tc.title, tc.description, got, want)
		}
	}

	// The quote-shaped title reaches the page as text, never as markup.
	base := createTestBinWithMetadata(t, s, "test-key", `x" onload="y`, "")
	if body := get(t, s, base).Body.String(); strings.Contains(body, `<h1>x" onload="y</h1>`) {
		t.Errorf("listing let a quote shape markup:\n%s", body)
	}

	// Above the file list, and the file list itself keeps the rows it always had.
	messy := createTestBinWithMetadata(t, s, "test-key", "Button grid", "A grid of pressable <buttons>")
	plain := createTestBinWithMetadata(t, s, "test-key", "", "")
	for _, bin := range []string{messy, plain} {
		rec := put(t, s, bin+"a.html", "test-key", "<p>a</p>")
		if rec.Code != http.StatusCreated {
			t.Fatalf("PUT a.html into %q = %d, want %d", bin, rec.Code, http.StatusCreated)
		}
	}
	messyBody := get(t, s, messy).Body.String()
	plainBody := get(t, s, plain).Body.String()
	// The rows are compared without their timestamps, which are written per PUT.
	rows := func(body string) []string {
		var out []string
		for _, line := range strings.Split(body, "\n") {
			if i := strings.Index(line, "<li>"); i >= 0 {
				out = append(out, strings.SplitN(line[i:], " (", 2)[0])
			}
		}
		return out
	}
	if got, want := fmt.Sprint(rows(messyBody)), fmt.Sprint(rows(plainBody)); got != want {
		t.Errorf("listed rows with metadata = %s, want the rows without it: %s", got, want)
	}
	if !strings.Contains(messyBody, "<p class=\"bin-meta\"><strong>Button grid</strong><br>A grid of pressable &lt;buttons&gt;</p>\n") {
		t.Errorf("listing carries no metadata paragraph:\n%s", messyBody)
	}
	if strings.Index(messyBody, "bin-meta") > strings.Index(messyBody, "<ul>") {
		t.Errorf("metadata paragraph does not come before the file list:\n%s", messyBody)
	}
}

// verifyKeyFrom POSTs /verify-key from a fixed socket address; an empty apiKey omits the header.
func verifyKeyFrom(t *testing.T, h http.Handler, remoteAddr, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/verify-key", nil)
	req.RemoteAddr = remoteAddr
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestVerifyKeyAnswersKeyOk is the key check's happy path: the correct key answers
// "key ok", and a correct call creates no bin and charges nothing.
func TestVerifyKeyAnswersKeyOk(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := verifyKeyFrom(t, s, "198.51.100.21:1234", "test-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /verify-key with the correct key = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "key ok" {
		t.Errorf("body = %q, want %q", got, "key ok")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.21"); ok {
		t.Errorf("a key-correct /verify-key charged 198.51.100.21, want no row in ip_blocks")
	}
	if count := storedBinCount(t, s); count != 0 {
		t.Errorf("bins = %d, want /verify-key to create no bin", count)
	}
}

// TestVerifyKeyWrongKeyChargesTheAddress keeps the lockout rules for /verify-key exactly
// what they are for POST /: one wrong or missing key is a charged 403, an attempt while
// blocked is a charged 429 with a Retry-After, and hammering walks the penalty up.
func TestVerifyKeyWrongKeyChargesTheAddress(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := verifyKeyFrom(t, s, "203.0.113.21:1234", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid API key") {
		t.Errorf("403 body = %q, want the invalid API key message", rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra != "" {
		t.Errorf("403 carries Retry-After %q, want none", ra)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.21"); failures != 1 || dur != blockStep {
		t.Errorf("after a wrong key = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// A missing header counts as a failure the way it does at POST /.
	rec = verifyKeyFrom(t, s, "203.0.113.22:1234", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing header = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "203.0.113.22"); failures != 1 || dur != blockStep {
		t.Errorf("after a missing header = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// The blocked address is refused even with the correct key, and the attempt
	// itself is charged.
	rec = verifyKeyFrom(t, s, "203.0.113.21:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked /verify-key = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too many bad API keys") {
		t.Errorf("429 body = %q, want the too many bad API keys message", rec.Body.String())
	}
	ra1 := retryAfter(t, rec)
	if ra1 <= 1800-30 || ra1 > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra1)
	}
	if failures, dur := storedBlock(t, s, "203.0.113.21"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked attempt = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}

	rec = verifyKeyFrom(t, s, "203.0.113.21:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second blocked /verify-key = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	ra2 := retryAfter(t, rec)
	if ra2 <= ra1 {
		t.Errorf("Retry-After went from %d to %d; the block must grow while hammered", ra1, ra2)
	}
}

// TestVerifyKeyIsNoKeyOracle is the 405 answer: only POST checks a key, so no other
// method can probe one — not for free, and not by charging either.
func TestVerifyKeyIsNoKeyOracle(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		req := httptest.NewRequest(method, "/verify-key", nil)
		req.RemoteAddr = "198.51.100.22:1234"
		req.Header.Set("X-API-Key", "test-key")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /verify-key = %d, want %d; body %q", method, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
		}
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.22"); ok {
		t.Errorf("the 405 answers charged 198.51.100.22, want no row in ip_blocks")
	}
}

// TestVerifyKeyDoesNotShadowBins proves the root branch stays clear of bin hashes: a
// file literally named verify-key under a bin is an ordinary file, stored and served.
func TestVerifyKeyDoesNotShadowBins(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	if rec := put(t, s, base+"verify-key", "test-key", "an ordinary file"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT %sverify-key = %d, want %d; body %q", base, rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec := get(t, s, base+"verify-key")
	if rec.Code != http.StatusOK || rec.Body.String() != "an ordinary file" {
		t.Errorf("GET %sverify-key = %d, body %q; want 200 and the stored body", base, rec.Code, rec.Body.String())
	}
}

// TestSkillRouteServesRepoSkillFile is the freshness guarantee: the skill GET answers the
// repo's skills/sandbin-preview/SKILL.md rendered through the same text/template the
// server uses, so the embed can never serve a stale fork of it. No key is sent:
// reading the skill needs none.
func TestSkillRouteServesRepoSkillFile(t *testing.T) {
	h := newTestServer(t, "test-key")

	rec := get(t, h, "/skills/sandbin-preview/SKILL.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /skills/sandbin-preview/SKILL.md = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/markdown; charset=utf-8", ct)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "skills", "sandbin-preview", "SKILL.md"))
	if err != nil {
		t.Fatalf("read repo skill file: %v", err)
	}
	// The repo file is a template; freshness compares the render with the base this
	// request arrived at (httptest defaults: Host example.com on loopback).
	tmpl, err := template.New("skill").Parse(string(want))
	if err != nil {
		t.Fatalf("repo skill file does not parse as a template: %v", err)
	}
	if !bytes.Contains(want, []byte("{{.Base}}")) {
		t.Errorf("repo skill file carries no {{.Base}} token to fill")
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Base": "http://example.com"}); err != nil {
		t.Fatalf("render repo skill file: %v", err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, buf.Bytes()) {
		t.Errorf("served body = %d bytes, rendered repo file = %d bytes; want them byte-identical", len(got), buf.Len())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("description:")) {
		t.Errorf("served skill carries no frontmatter description:\n%s", rec.Body.String())
	}
}

// TestSkillRouteFillsTheVisitorsHost is the point of the template: every address in
// the served skill is the one the request arrived at, no token survives, and the old
// stand-in host is gone.
func TestSkillRouteFillsTheVisitorsHost(t *testing.T) {
	h := newTestServer(t, "test-key")
	cases := []struct {
		name       string
		host       string
		forwarded  string
		proto      string
		remoteAddr string
		tls        bool
		want       string
	}{
		{"plain loopback", "skill.example.net", "", "", "127.0.0.1:1234", false, "http://skill.example.net"},
		{"tls socket", "bin.example.org", "", "", "127.0.0.1:1234", true, "https://bin.example.org"},
		{"loopback proxy https", "localhost:8080", "behind.example.org", "https", "127.0.0.1:1234", false, "https://behind.example.org"},
		{"hostile forwarded host falls back", "direct.example.org", "evil.example.org/x", "https", "127.0.0.1:1234", false, "https://this.host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/skills/sandbin-preview/SKILL.md", nil)
			req.Host = c.host
			req.RemoteAddr = c.remoteAddr
			if c.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if c.forwarded != "" {
				req.Header.Set("X-Forwarded-Host", c.forwarded)
			}
			if c.proto != "" {
				req.Header.Set("X-Forwarded-Proto", c.proto)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET SKILL.md = %d, want %d", rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			for _, want := range []string{
				"init --url " + c.want,
				"description: Upload files or whole folders to the sandbin server at " + c.want,
				"`" + c.want + "/<bin>/`",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("served skill missing %q in:\n%s", want, body)
				}
			}
			for _, absent := range []string{"{{", "sandbin.example"} {
				if strings.Contains(body, absent) {
					t.Errorf("served skill still carries %q:\n%s", absent, body)
				}
			}
		})
	}
}

// TestSkillRouteMethodDiscipline is the 405 answer and the charging rule: only GET
// answers, the 405 fires before any key is looked at (a wrong key is still a 405, not a
// 403), and neither a 405 nor a GET touches the block table.
func TestSkillRouteMethodDiscipline(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/skills/sandbin-preview/SKILL.md", nil)
		req.RemoteAddr = "198.51.100.31:1234"
		req.Header.Set("X-API-Key", "wrong")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s the skill path = %d, want %d; body %q", method, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
		}
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.31"); ok {
		t.Errorf("the 405 answers charged 198.51.100.31, want no row in ip_blocks")
	}
	if rec := get(t, s, "/skills/sandbin-preview/SKILL.md"); rec.Code != http.StatusOK {
		t.Errorf("GET after the 405 battery = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestOtherSkillPathsAreUnknownBins keeps the static route to its one exact path: every
// other /skills/... path falls through to the ordinary unknown-bin router.
func TestOtherSkillPathsAreUnknownBins(t *testing.T) {
	h := newTestServer(t, "test-key")

	for _, p := range []string{"/skills/other", "/skills/"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusNotFound, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unknown bin") {
			t.Errorf("GET %s body = %q, want an unknown bin message", p, rec.Body.String())
		}
	}
}

// TestSkillRouteDoesNotShadowBins is the review focus: a bin's own skills/... paths are
// ordinary files, stored, listed and served, although the root answers one skill path.
func TestSkillRouteDoesNotShadowBins(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	if rec := put(t, s, base+"skills/x.txt", "test-key", "an ordinary file"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT %sskills/x.txt = %d, want %d; body %q", base, rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec := get(t, s, base+"skills/x.txt")
	if rec.Code != http.StatusOK || rec.Body.String() != "an ordinary file" {
		t.Errorf("GET %sskills/x.txt = %d, body %q; want 200 and the stored body", base, rec.Code, rec.Body.String())
	}
	if body := get(t, s, base).Body.String(); !strings.Contains(body, "skills/x.txt") {
		t.Errorf("the listing does not show skills/x.txt:\n%s", body)
	}
}
