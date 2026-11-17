package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// binHashRE matches the hash a bin answer names.
var binHashRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// postAPIBins POSTs a body to /api/bins from a fixed socket address and returns the recorder.
// An empty body sends no body at all, which is the empty request; an empty apiKey omits the
// header.
func postAPIBins(t *testing.T, h http.Handler, remoteAddr, apiKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bins", rdr)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// binAnswer is the test's own reading of the pinned bin object: the json spellings are what
// the API promises, so the tests hold them apart from the handler's own type.
type binAnswer struct {
	Bin         string `json:"bin"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	HasPassword bool   `json:"has_password"`
	CreatedAt   string `json:"created_at"`
}

// createdBin decodes a POST /api/bins answer into the bin object it carries.
func createdBin(t *testing.T, rec *httptest.ResponseRecorder) binAnswer {
	t.Helper()
	var b binAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("POST /api/bins answer %q is not a bin object: %v", rec.Body.String(), err)
	}
	return b
}

// TestAPIKeyCheck is POST /api/key, the retired verify-key's replacement: the correct key
// answers 204 with an empty body, a wrong key answers 403 and records the failure, and the
// blocked-IP refusal with its Retry-After is carried over: an attempt while blocked is
// refused even with the correct key and charges again.
func TestAPIKeyCheck(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := postAPIKeyFrom(t, s, "198.51.100.61:1234", "test-key")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /api/key with the correct key = %d, want %d; body %q", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("204 body = %q, want it empty", got)
	}

	rec = postAPIKeyFrom(t, s, "198.51.100.62:1234", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/key with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if got := rec.Body.String(); got != "invalid API key\n" {
		t.Errorf("403 body = %q, want the invalid API key message", got)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.62"); failures != 1 || dur != blockStep {
		t.Errorf("after a wrong key = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// The blocked address is refused even with the correct key, and the attempt charges.
	rec = postAPIKeyFrom(t, s, "198.51.100.62:1234", "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked POST /api/key = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too many bad API keys") {
		t.Errorf("429 body = %q, want the too many bad API keys message", rec.Body.String())
	}
	ra := retryAfter(t, rec)
	if ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.62"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked attempt = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}
}

// TestAPIHealth is the container probe's route: it answers 200 JSON with no key at all,
// takes GET and HEAD only, and a blocked address still gets its 200 and is charged nothing.
func TestAPIHealth(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := get(t, s, "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/health = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Errorf("GET /api/health body = %q, want {\"status\":\"ok\"} with its trailing newline", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	req := httptest.NewRequest(http.MethodHead, "/api/health", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD /api/health = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/health = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	const addr = "198.51.100.63:1234"
	if rec := postAPIKeyFrom(t, s, addr, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if rec := getFrom(t, s, addr, "/api/health", ""); rec.Code != http.StatusOK {
		t.Errorf("blocked GET /api/health = %d, body %q; want 200 even while the address is blocked", rec.Code, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "198.51.100.63"); failures != 1 || dur != blockStep {
		t.Errorf("the health GETs charged the block = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}
}

// TestAPIUnknownShapes keeps the /api surface to what it answers: a spelling it does not own —
// trailing-slash variants included — is the plain 404 unknown bin, and a known path on a wrong
// method is the plain 405. /api/bins answers POST today; its GET and DELETE belong to Tasks 5
// and 6, so they are 405 here.
func TestAPIUnknownShapes(t *testing.T) {
	s := newTestServer(t, "test-key")
	hash := strings.Trim(createTestBin(t, s, "test-key"), "/")

	for _, p := range []string{"/api", "/api/", "/api/bins/", "/api/bins/" + hash + "/", "/api/keyextra"} {
		rec := get(t, s, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		if rec.Body.String() != "unknown bin\n" {
			t.Errorf("GET %s body = %q, want the unknown bin message", p, rec.Body.String())
		}
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/key"},
		{http.MethodDelete, "/api/bins"},
		{http.MethodHead, "/api/bins"},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want %d; body %q", c.method, c.path, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
		}
		if rec.Body.String() != "method not allowed\n" {
			t.Errorf("%s %s body = %q, want the method not allowed message", c.method, c.path, rec.Body.String())
		}
	}
}

// TestOldRoutesRetired is the no-compatibility rule: /healthz and /verify-key are ordinary
// unknown paths now, answered with the plain 404 the router gives every other unknown shape.
func TestOldRoutesRetired(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := get(t, s, "/healthz")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /healthz = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if rec.Body.String() != "unknown bin\n" {
		t.Errorf("GET /healthz body = %q, want the unknown bin message", rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/verify-key", nil)
	req.Header.Set("X-API-Key", "test-key")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /verify-key = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if rec.Body.String() != "unknown bin\n" {
		t.Errorf("POST /verify-key body = %q, want the unknown bin message", rec.Body.String())
	}
}

// TestAPICreateBin is POST /api/bins with nothing to say: an empty request — no body, or {} —
// makes an open bin with no metadata, and the answer is the pinned bin object plus the
// Location of the bin it just made.
func TestAPICreateBin(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, body := range []string{"", "{}"} {
		rec := postAPIBins(t, s, "198.51.100.70:1234", "test-key", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /api/bins body %q = %d, want %d; body %q", body, rec.Code, http.StatusCreated, rec.Body.String())
		}
		bin := createdBin(t, rec)
		if !binHashRE.MatchString(bin.Bin) {
			t.Errorf("bin %q is not an eight-hex hash; body %q", bin.Bin, rec.Body.String())
			continue
		}
		if got := rec.Header().Get("Location"); got != "/api/bins/"+bin.Bin {
			t.Errorf("Location = %q, want /api/bins/%s", got, bin.Bin)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if want := "/" + bin.Bin + "/"; bin.URL != want {
			t.Errorf("url = %q, want %q", bin.URL, want)
		}
		if bin.Title != "" || bin.Description != "" {
			t.Errorf("title/description = %q / %q, want both empty", bin.Title, bin.Description)
		}
		if bin.HasPassword {
			t.Errorf("has_password = true, want false for a bin nobody gave a password")
		}
		if _, err := time.Parse(time.RFC3339, bin.CreatedAt); err != nil {
			t.Errorf("created_at %q is not RFC 3339: %v", bin.CreatedAt, err)
		}
		want := `{"bin":"` + bin.Bin + `","url":"/` + bin.Bin + `/","title":"","description":"","has_password":false,"created_at":"` + bin.CreatedAt + `"}` + "\n"
		if got := rec.Body.String(); got != want {
			t.Errorf("body = %q, want the pinned bin object %q", got, want)
		}

		stored, ok, err := s.store.GetBin(bin.Bin)
		if err != nil || !ok {
			t.Fatalf("GetBin(%q) = ok %v, err %v", bin.Bin, ok, err)
		}
		if stored.CreatedAt != bin.CreatedAt {
			t.Errorf("created_at = %q, want the store's own %q", bin.CreatedAt, stored.CreatedAt)
		}
		if stored.PasswordHash != "" {
			t.Errorf("stored password hash %q, want an open bin", stored.PasswordHash)
		}

		// The bin the answer names is open: writing and reading it needs no Basic.
		if rec := put(t, s, bin.URL+"page.html", "test-key", "<h1>Hello</h1>"); rec.Code != http.StatusCreated {
			t.Fatalf("PUT under the new bin = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
		}
		if rec := get(t, s, bin.URL+"page.html"); rec.Code != http.StatusOK || rec.Body.String() != "<h1>Hello</h1>" {
			t.Errorf("GET the new bin's file = %d, body %q", rec.Code, rec.Body.String())
		}
	}
}

// TestAPICreateBinMetadata is the metadata half of create: title and description travel in the
// body, land in the row, and come back in the answer. An explicit null and an explicit "" are
// an empty title, which is the same bin as one whose title was never given.
func TestAPICreateBinMetadata(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.71:1234"

	rec := postAPIBins(t, s, addr, "test-key", `{"title":"T","description":"D"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/bins with metadata = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	bin := createdBin(t, rec)
	if bin.Title != "T" || bin.Description != "D" {
		t.Errorf("answer metadata = %q / %q, want the T / D the body carried", bin.Title, bin.Description)
	}
	if title, description := storedMeta(t, s, bin.URL); title != "T" || description != "D" {
		t.Errorf("stored metadata = %q / %q, want the T / D the body carried", title, description)
	}

	for _, body := range []string{`{"title":null}`, `{"title":""}`} {
		rec = postAPIBins(t, s, addr, "test-key", body)
		if rec.Code != http.StatusCreated {
			t.Errorf("POST /api/bins body %s = %d, want %d; body %q", body, rec.Code, http.StatusCreated, rec.Body.String())
			continue
		}
		bin = createdBin(t, rec)
		if bin.Title != "" {
			t.Errorf("body %s answered title %q, want it empty", body, bin.Title)
		}
		if title, _ := storedMeta(t, s, bin.URL); title != "" {
			t.Errorf("body %s stored title %q, want it empty", body, title)
		}
	}
}

// TestAPICreateBinPassword is the password boundary at create: a given password protects the
// bin, is stored as a bcrypt hash, and never appears in a body; "" and null leave the bin
// open; a password longer than bcrypt can hold is refused without making a bin.
func TestAPICreateBinPassword(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.72:1234"
	before := storedBinCount(t, s)

	rec := postAPIBins(t, s, addr, "test-key", `{"password":"sesame"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/bins with a password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	bin := createdBin(t, rec)
	if !bin.HasPassword {
		t.Errorf("has_password = false, want true for a bin made with a password")
	}
	if got := rec.Body.String(); strings.Contains(got, "sesame") {
		t.Errorf("body %q carries the password, want it nowhere", got)
	}
	stored, ok, err := s.store.GetBin(bin.Bin)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", bin.Bin, ok, err)
	}
	if !strings.HasPrefix(stored.PasswordHash, "$2") {
		t.Errorf("stored password %q is not a bcrypt hash", stored.PasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("sesame")); err != nil {
		t.Errorf("the stored hash does not match sesame: %v", err)
	}
	if rec := put(t, s, bin.URL+"page.html", "test-key", "<p>x</p>"); rec.Code != http.StatusUnauthorized {
		t.Errorf("PUT without the bin password = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	req := httptest.NewRequest(http.MethodPut, bin.URL+"page.html", strings.NewReader("<p>x</p>"))
	req.Header.Set("X-API-Key", "test-key")
	req.SetBasicAuth(bin.Bin, "sesame")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT with the bin password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	for _, body := range []string{`{"password":""}`, `{"password":null}`} {
		rec = postAPIBins(t, s, addr, "test-key", body)
		if rec.Code != http.StatusCreated {
			t.Errorf("POST /api/bins body %s = %d, want %d; body %q", body, rec.Code, http.StatusCreated, rec.Body.String())
			continue
		}
		bin = createdBin(t, rec)
		if bin.HasPassword {
			t.Errorf("body %s answered has_password true, want an open bin", body)
		}
		stored, ok, err := s.store.GetBin(bin.Bin)
		if err != nil || !ok {
			t.Fatalf("GetBin(%q) = ok %v, err %v", bin.Bin, ok, err)
		}
		if stored.PasswordHash != "" {
			t.Errorf("body %s stored password hash %q, want the bin open", body, stored.PasswordHash)
		}
		if rec := put(t, s, bin.URL+"open.txt", "test-key", "open"); rec.Code != http.StatusCreated {
			t.Errorf("PUT in the bin %s made = %d, want %d", body, rec.Code, http.StatusCreated)
		}
		if rec := get(t, s, bin.URL+"open.txt"); rec.Code != http.StatusOK {
			t.Errorf("GET in the bin %s made = %d, want %d", body, rec.Code, http.StatusOK)
		}
	}

	long := `{"password":"` + strings.Repeat("p", 73) + `"}`
	rec = postAPIBins(t, s, addr, "test-key", long)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/bins with a 73 byte password = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := rec.Body.String(); got != "bin password too long\n" {
		t.Errorf("body = %q, want the bin password too long message", got)
	}
	if after := storedBinCount(t, s); after != before+3 {
		t.Errorf("bins after these creates = %d, want the %d the accepted bodies made only", after, before+3)
	}
}

// TestAPICreateBinRefusals is the create surface's no: a misspelled field fails loudly, a body
// that is not a JSON object is refused, so is one over 64 KiB, the metadata caps keep today's
// wording, and a wrong or missing key charges exactly as the retired create charged.
func TestAPICreateBinRefusals(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.73:1234"
	before := storedBinCount(t, s)

	for _, c := range []struct{ body, want string }{
		{`{"passwprd":"x"}`, "unrecognized field in the request\n"},
		{`{"desc":"x"}`, "unrecognized field in the request\n"},
		{`not json`, "could not read the request\n"},
		{`[1]`, "could not read the request\n"},
		{`"bare string"`, "could not read the request\n"},
		{`{"title":5}`, "could not read the request\n"},
		{`{"password":5}`, "could not read the request\n"},
		{`{"title":"` + strings.Repeat("t", maxTitleBytes+1) + `"}`, "title over 200 bytes\n"},
		{`{"description":"` + strings.Repeat("d", maxDescriptionBytes+1) + `"}`, "description over 2000 bytes\n"},
		{`{"title":"a\nb"}`, "title may not contain control characters\n"},
	} {
		rec := postAPIBins(t, s, addr, "test-key", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /api/bins body %s = %d, want %d; body %q", c.body, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("POST /api/bins body %s = %q, want %q", c.body, got, c.want)
		}
	}

	oversize := `{"title":"` + strings.Repeat("t", 64<<10) + `"}`
	rec := postAPIBins(t, s, addr, "test-key", oversize)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /api/bins with a %d byte body = %d, want %d; body %q", len(oversize), rec.Code, http.StatusBadRequest, rec.Body.String())
	} else if got := rec.Body.String(); got != "could not read the request\n" {
		t.Errorf("oversize body answer = %q, want the could not read the request message", got)
	}

	// A wrong key through the new route is the retired route's refusal and the retired
	// route's charge.
	rec = postAPIBins(t, s, "198.51.100.74:1234", "wrong", `{"title":"no"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/bins with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if got := rec.Body.String(); got != "invalid API key\n" {
		t.Errorf("403 body = %q, want the invalid API key message", got)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.74"); failures != 1 || dur != blockStep {
		t.Errorf("after a wrong key = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// A missing header is the same failure.
	rec = postAPIBins(t, s, "198.51.100.75:1234", "", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/bins without the header = %d, want %d", rec.Code, http.StatusForbidden)
	}

	// The blocked address is refused with an honest wait, and the attempt charges again.
	rec = postAPIBins(t, s, "198.51.100.74:1234", "test-key", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked POST /api/bins = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too many bad API keys") {
		t.Errorf("429 body = %q, want the too many bad API keys message", rec.Body.String())
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.74"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked attempt = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}

	if after := storedBinCount(t, s); after != before {
		t.Errorf("bins after the refusals = %d, want the same %d as before", after, before)
	}
}
