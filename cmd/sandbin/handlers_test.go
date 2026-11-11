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
