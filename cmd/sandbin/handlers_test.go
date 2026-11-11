package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
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
