package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, apiKey string) http.Handler {
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

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	rec = put(t, h, base+"page.html", "wrong", "x")
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT with wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = put(t, h, base+"page.html", "", "x")
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
