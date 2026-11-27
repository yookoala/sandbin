package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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

// apiEnvelope spells the exact body a dispatcher refusal answers: the status just answered as
// the code, the sentence, in the pinned field order, with the trailing newline.
func apiEnvelope(status int, message string) string {
	return fmt.Sprintf(`{"status":"error","code":%d,"message":%q}`, status, message) + "\n"
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
	if got := rec.Body.String(); got != apiEnvelope(403, "invalid API key") {
		t.Errorf("403 body = %q, want the invalid API key envelope", got)
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
// trailing-slash variants included — is the 404 envelope API endpoint not found, and a known
// path on a wrong method is the 405 envelope method not allowed. The collection /api/bins
// answers POST and GET only: its DELETE is the 405 pinned here, while DELETE with a hash
// belongs to the apiBinPathRE spelling.
func TestAPIUnknownShapes(t *testing.T) {
	s := newTestServer(t, "test-key")
	hash := strings.Trim(createTestBin(t, s, "test-key"), "/")

	for _, p := range []string{"/api/", "/api/bins/", "/api/bins/" + hash + "/", "/api/keyextra"} {
		rec := get(t, s, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		if rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
			t.Errorf("GET %s body = %q, want the endpoint envelope", p, rec.Body.String())
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
		if rec.Body.String() != apiEnvelope(405, "method not allowed") {
			t.Errorf("%s %s body = %q, want the 405 envelope", c.method, c.path, rec.Body.String())
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
	if got := rec.Body.String(); got != apiEnvelope(400, "bin password too long") {
		t.Errorf("body = %q, want the bin password too long envelope", got)
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
		{`{"passwprd":"x"}`, apiEnvelope(400, "unrecognized field in the request")},
		{`{"desc":"x"}`, apiEnvelope(400, "unrecognized field in the request")},
		{`not json`, apiEnvelope(400, "could not read the request")},
		{`[1]`, apiEnvelope(400, "could not read the request")},
		{`"bare string"`, apiEnvelope(400, "could not read the request")},
		{`{"title":5}`, apiEnvelope(400, "could not read the request")},
		{`{"password":5}`, apiEnvelope(400, "could not read the request")},
		{`{"title":"` + strings.Repeat("t", maxTitleBytes+1) + `"}`, apiEnvelope(400, "title over 200 bytes")},
		{`{"description":"` + strings.Repeat("d", maxDescriptionBytes+1) + `"}`, apiEnvelope(400, "description over 2000 bytes")},
		{`{"title":"a\nb"}`, apiEnvelope(400, "title may not contain control characters")},
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
	} else if got := rec.Body.String(); got != apiEnvelope(400, "could not read the request") {
		t.Errorf("oversize body answer = %q, want the could not read the request envelope", got)
	}

	// A wrong key through the new route is the retired route's refusal and the retired
	// route's charge.
	rec = postAPIBins(t, s, "198.51.100.74:1234", "wrong", `{"title":"no"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/bins with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(403, "invalid API key") {
		t.Errorf("403 body = %q, want the invalid API key envelope", got)
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

// TestAPICreateBinBodyShapes is the "the top-level value must be a JSON object" rule against the
// shapes a decode aimed straight at a struct lets through: null is not an object and makes no
// bin, and nothing may trail the one object a body is allowed to hold.
func TestAPICreateBinBodyShapes(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.76:1234"
	before := storedBinCount(t, s)

	for _, body := range []string{
		"null",
		"{}{}",
		`{"title":"a"}{"title":"b"}`,
		`{"title":"a"} junk`,
		// A stray closing bracket is a second value all the same: it is not a marker
		// this top-level body may end with.
		`{"title":"a"}]`,
		`{"title":"a"}}`,
	} {
		rec := postAPIBins(t, s, addr, "test-key", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /api/bins body %s = %d, want %d; body %q", body, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != apiEnvelope(400, "could not read the request") {
			t.Errorf("POST /api/bins body %s = %q, want the could not read the request envelope", body, got)
		}
	}

	// Whitespace after the one object is not a second value: a body ending in a
	// newline is the same JSON object and stays accepted.
	rec := postAPIBins(t, s, addr, "test-key", "{\"title\":\"trailing newline\"}\n")
	if rec.Code != http.StatusCreated {
		t.Errorf("POST /api/bins with a trailing newline = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if after := storedBinCount(t, s); after != before+1 {
		t.Errorf("bins after the shape refusals = %d, want the same %d as before plus the one accepted body", after, before)
	}
}

// TestFieldValueStates is the tri-state table every bin write reads its fields through: an
// absent field is not given, null and "" are given as empty, a JSON string is its value, and a
// raw value that is none of those — which decodeBinRequest refuses before a handler sees it —
// reads as given-empty.
func TestFieldValueStates(t *testing.T) {
	for _, c := range []struct {
		raw       json.RawMessage
		wantValue string
		wantGiven bool
		why       string
	}{
		{nil, "", false, "an absent field is not given"},
		{json.RawMessage("null"), "", true, "an explicit null is given as empty"},
		{json.RawMessage(`""`), "", true, "an explicit empty string is given as empty"},
		{json.RawMessage(`"x"`), "x", true, "a JSON string is its value"},
		{json.RawMessage(`"a\nb"`), "a\nb", true, "the escapes a JSON string carries are read out of it"},
		{json.RawMessage("5"), "", true, "a raw number reads as given-empty, never as a value"},
	} {
		value, given := fieldValue(c.raw)
		if value != c.wantValue || given != c.wantGiven {
			t.Errorf("fieldValue(%s) = (%q, %v), want (%q, %v): %s", c.raw, value, given, c.wantValue, c.wantGiven, c.why)
		}
	}
}

// patchBinsJSON PATCHes a path from a fixed socket address: an empty apiKey omits the header,
// an empty body sends no body at all — which PATCH refuses as unreadable, pinned in
// TestAPIPatchJSON — and a non-empty basicUser sends Basic auth with basicPassword, which is
// how a protected bin is opened.
func patchBinsJSON(t *testing.T, h http.Handler, remoteAddr, path, apiKey, body, basicUser, basicPassword string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPatch, path, rdr)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPassword)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// getBasicFrom GETs a path from a fixed socket address with Basic credentials.
func getBasicFrom(t *testing.T, h http.Handler, remoteAddr, path, user, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	req.SetBasicAuth(user, password)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// readBinAnswer decodes a recorder's body into the bin object shape, wherever it answered.
func readBinAnswer(t *testing.T, rec *httptest.ResponseRecorder) binAnswer {
	t.Helper()
	var b binAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("answer %q is not a bin object: %v", rec.Body.String(), err)
	}
	return b
}

// binDetailAnswer is the test's own reading of the inspect answer: the bin object plus the
// files array it names, each with its path, size and updated_at.
type binDetailAnswer struct {
	binAnswer
	Files []fileAnswer `json:"files"`
}

type fileAnswer struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
}

// readBinDetail decodes a GET /api/bins/{hash} answer into the bin object and its files.
func readBinDetail(t *testing.T, rec *httptest.ResponseRecorder) binDetailAnswer {
	t.Helper()
	var d binDetailAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("inspect answer %q is not a bin detail object: %v", rec.Body.String(), err)
	}
	return d
}

// TestAPIInspectBin is GET /api/bins/{hash}: no API key, the bin object plus its file list
// sorted by path — an array, never null — and the list never hides, not when an index.html
// covers the bin's own page. A protected bin answers 401 until Basic opens it, and no 401,
// right or wrong, charges the address.
func TestAPIInspectBin(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	seed := map[string]string{
		"notes.md":      "body of notes.md",
		"a.txt":         "body of a.txt",
		"drafts/one.md": "body of drafts/one.md",
	}
	for name, body := range seed {
		if rec := put(t, s, base+name, "test-key", body); rec.Code != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want %d; body %q", name, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	rec := get(t, s, "/api/bins/"+hash)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins/%s with no API key = %d, want %d; body %q", hash, rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	detail := readBinDetail(t, rec)
	if detail.Bin != hash || detail.URL != base {
		t.Errorf("bin/url = %q / %q, want %q / %q", detail.Bin, detail.URL, hash, base)
	}
	if detail.HasPassword {
		t.Errorf("has_password = true, want false for an open bin")
	}
	if _, err := time.Parse(time.RFC3339, detail.CreatedAt); err != nil {
		t.Errorf("created_at %q is not RFC 3339: %v", detail.CreatedAt, err)
	}
	if len(detail.Files) != len(seed) {
		t.Fatalf("files = %d entries, want the %d seeded files; body %q", len(detail.Files), len(seed), rec.Body.String())
	}
	for i, f := range detail.Files {
		if want := []string{"a.txt", "drafts/one.md", "notes.md"}[i]; f.Path != want {
			t.Errorf("files[%d].path = %q, want %q: the list is sorted by path", i, f.Path, want)
		}
		if want := int64(len(seed[f.Path])); f.Size != want {
			t.Errorf("%s size = %d, want %d", f.Path, f.Size, want)
		}
		if _, err := time.Parse(time.RFC3339, f.UpdatedAt); err != nil {
			t.Errorf("%s updated_at %q is not RFC 3339: %v", f.Path, f.UpdatedAt, err)
		}
	}

	// An empty bin answers an array, never null.
	emptyHash := strings.Trim(createTestBin(t, s, "test-key"), "/")
	rec = get(t, s, "/api/bins/"+emptyHash)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the empty bin = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if detail = readBinDetail(t, rec); detail.Files == nil || len(detail.Files) != 0 {
		t.Errorf("empty bin files = %v, want [] ; body %q", detail.Files, rec.Body.String())
	}

	// A root index.html hides the listing from the bin's HTML page; the API list never hides.
	if rec := put(t, s, base+"index.html", "test-key", "<h1>Cover</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT index.html = %d, want %d", rec.Code, http.StatusCreated)
	}
	if page := get(t, s, base); page.Code != http.StatusOK || page.Body.String() != "<h1>Cover</h1>" {
		t.Fatalf("GET the covered bin = %d, body %q; want the stored index.html", page.Code, page.Body.String())
	} else if strings.Contains(page.Body.String(), "notes.md") {
		t.Errorf("the covered bin's page lists notes.md, want the file list hidden there:\n%s", page.Body.String())
	}
	rec = get(t, s, "/api/bins/"+hash)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins/%s with the cover in place = %d, want %d", hash, rec.Code, http.StatusOK)
	}
	detail = readBinDetail(t, rec)
	wantFiles := []string{"a.txt", "drafts/one.md", "index.html", "notes.md"}
	if len(detail.Files) != len(wantFiles) {
		t.Fatalf("covered bin files = %d entries, want %d; body %q", len(detail.Files), len(wantFiles), rec.Body.String())
	}
	for i, f := range detail.Files {
		if f.Path != wantFiles[i] {
			t.Errorf("files[%d].path = %q, want %q", i, f.Path, wantFiles[i])
		}
	}

	// A protected bin gates on the bin password, not on any API key.
	const addr = "198.51.100.82:1234"
	pbase := createTestBinWithPassword(t, s, "sesame")
	phash := strings.Trim(pbase, "/")
	req := httptest.NewRequest(http.MethodPut, pbase+"secret.txt", strings.NewReader("shh"))
	req.Header.Set("X-API-Key", "test-key")
	req.SetBasicAuth(phash, "sesame")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT the protected bin's file = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec = getFrom(t, s, addr, "/api/bins/"+phash, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET the protected bin without Basic = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(401, "bin password required") {
		t.Errorf("401 body = %q, want the bin password required envelope", got)
	}
	if www := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(www, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want a Basic challenge", www)
	}

	rec = getBasicFrom(t, s, addr, "/api/bins/"+phash, phash, "sesame")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the protected bin with the right Basic = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	detail = readBinDetail(t, rec)
	if !detail.HasPassword {
		t.Errorf("has_password = false, want true for the protected bin")
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "secret.txt" || detail.Files[0].Size != 3 {
		t.Errorf("files = %v, want the one secret.txt entry; body %q", detail.Files, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sesame") {
		t.Errorf("body %q carries the password, want it nowhere", rec.Body.String())
	}

	rec = getBasicFrom(t, s, addr, "/api/bins/"+phash, phash, "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET with the wrong Basic = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	// No 401 through a bin gate charges the address: the API-key lockout is not this gate.
	if _, ok := ipBlockRow(t, s.store, "198.51.100.82"); ok {
		t.Errorf("the bin-password 401s charged 198.51.100.82, want no row in ip_blocks")
	}
}

// TestAPIPatchJSON is the PATCH semantics table at the JSON surface: a given field replaces,
// an absent one leaves what the bin holds, null and "" clear theirs, and {} is a no-op that
// answers the unchanged row. The answer body is the full bin object as the row now stands.
func TestAPIPatchJSON(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "first title", "first description")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.80:1234"

	bin, ok, err := s.store.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", hash, ok, err)
	}
	createdAt := bin.CreatedAt

	for i, tc := range []struct {
		body                                 string
		wantTitle, wantDescription, wantBody string
	}{
		// {} touches nothing and answers the row as it was.
		{`{}`, "first title", "first description", `{"bin":"` + hash + `","url":"` + base + `","title":"first title","description":"first description","has_password":false,"created_at":"` + createdAt + `"}`},
		// One field replaces its value and says nothing about the other.
		{`{"title":"second title"}`, "second title", "first description", `{"bin":"` + hash + `","url":"` + base + `","title":"second title","description":"first description","has_password":false,"created_at":"` + createdAt + `"}`},
		{`{"description":"second description"}`, "second title", "second description", `{"bin":"` + hash + `","url":"` + base + `","title":"second title","description":"second description","has_password":false,"created_at":"` + createdAt + `"}`},
		{`{"title":"third title","description":"third description"}`, "third title", "third description", `{"bin":"` + hash + `","url":"` + base + `","title":"third title","description":"third description","has_password":false,"created_at":"` + createdAt + `"}`},
		// null and "" both clear their field and leave the other alone.
		{`{"title":null}`, "", "third description", `{"bin":"` + hash + `","url":"` + base + `","title":"","description":"third description","has_password":false,"created_at":"` + createdAt + `"}`},
		{`{"description":""}`, "", "", `{"bin":"` + hash + `","url":"` + base + `","title":"","description":"","has_password":false,"created_at":"` + createdAt + `"}`},
		// A cleared field stays cleared when the next PATCH does not mention it.
		{`{"title":"fourth title"}`, "fourth title", "", `{"bin":"` + hash + `","url":"` + base + `","title":"fourth title","description":"","has_password":false,"created_at":"` + createdAt + `"}`},
	} {
		rec := patchBinsJSON(t, s, addr, "/api/bins/"+hash, "test-key", tc.body, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH #%d body %s = %d, want %d; body %q", i+1, tc.body, rec.Code, http.StatusOK, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("PATCH #%d Content-Type = %q, want application/json", i+1, ct)
		}
		if want := tc.wantBody + "\n"; rec.Body.String() != want {
			t.Errorf("PATCH #%d body = %q, want the full bin object %q", i+1, rec.Body.String(), want)
		}
		if title, description := storedMeta(t, s, base); title != tc.wantTitle || description != tc.wantDescription {
			t.Errorf("after PATCH #%d: stored metadata = %q / %q, want %q / %q",
				i+1, title, description, tc.wantTitle, tc.wantDescription)
		}
	}

	// A malformed body is refused, and the row keeps what it held.
	for _, c := range []struct{ body, want string }{
		// R6 pin: no body at all is not a JSON object — PATCH refuses it, unlike POST,
		// whose pinned "no body is {}" the create tests hold.
		{"", apiEnvelope(400, "could not read the request")},
		{`{"titel":"x"}`, apiEnvelope(400, "unrecognized field in the request")},
		{`not json`, apiEnvelope(400, "could not read the request")},
		{`{"title":5}`, apiEnvelope(400, "could not read the request")},
		{`null`, apiEnvelope(400, "could not read the request")},
		{`{"title":"a"}{"title":"b"}`, apiEnvelope(400, "could not read the request")},
		// A stray closing bracket trails the object just like a second one, and is
		// refused the same way.
		{`{"title":"a"}]`, apiEnvelope(400, "could not read the request")},
		{`{"title":"` + strings.Repeat("t", maxTitleBytes+1) + `"}`, apiEnvelope(400, "title over 200 bytes")},
		{`{"title":"a\nb"}`, apiEnvelope(400, "title may not contain control characters")},
	} {
		rec := patchBinsJSON(t, s, addr, "/api/bins/"+hash, "test-key", c.body, "", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH body %s = %d, want %d; body %q", c.body, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("PATCH body %s = %q, want %q", c.body, got, c.want)
		}
		if title, description := storedMeta(t, s, base); title != "fourth title" || description != "" {
			t.Errorf("after the refusal of %s: stored metadata = %q / %q, want it unchanged", c.body, title, description)
		}
	}
}

// TestAPIPatchPassword is the password half of PATCH: rotation needs the current password on
// Basic, the new one is bcrypt-stored and the bin answers it from then on, clearing takes
// null or "" through the same gate, an over-long candidate is a 400, and an attempt without
// the current password is a 401 that leaves the row alone and charges nothing.
func TestAPIPatchPassword(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithPassword(t, s, "sesame")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.81:1234"
	apiPath := "/api/bins/" + hash

	// Rotation: the current password on Basic, the new one in the body.
	rec := patchBinsJSON(t, s, addr, apiPath, "test-key", `{"password":"new-pass"}`, hash, "sesame")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate with the current Basic = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	patched := readBinAnswer(t, rec)
	if !patched.HasPassword {
		t.Errorf("has_password = false, want true after the rotation")
	}
	if got := rec.Body.String(); strings.Contains(got, "sesame") || strings.Contains(got, "new-pass") {
		t.Errorf("body %q carries a password, want neither in any body", got)
	}
	stored, ok, err := s.store.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", hash, ok, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("new-pass")); err != nil {
		t.Fatalf("the stored hash does not match the new password: %v", err)
	}

	// The bin answers the new password and not the old one, on its page and on the API.
	if rec := getBasicFrom(t, s, addr, base, hash, "new-pass"); rec.Code != http.StatusOK {
		t.Errorf("GET with the new password = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec := getBasicFrom(t, s, addr, base, hash, "sesame"); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET with the old password = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec := getBasicFrom(t, s, addr, apiPath, hash, "new-pass"); rec.Code != http.StatusOK {
		t.Errorf("GET /api/bins/%s with the new password = %d, want %d", hash, rec.Code, http.StatusOK)
	}

	// A rotation attempt without the current password — no Basic or the wrong one — is a
	// 401 that leaves the row alone and charges nothing.
	for _, attempt := range []struct{ user, password, why string }{
		{"", "", "no Basic"},
		{hash, "sesame", "the superseded password"},
	} {
		rec := patchBinsJSON(t, s, addr, apiPath, "test-key", `{"password":"attacker"}`, attempt.user, attempt.password)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("rotate with %s = %d, want %d; body %q", attempt.why, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
		if got := rec.Body.String(); got != apiEnvelope(401, "bin password required") {
			t.Errorf("401 body = %q, want the bin password required envelope", got)
		}
		if _, ok := ipBlockRow(t, s.store, "198.51.100.81"); ok {
			t.Fatalf("rotate with %s charged 198.51.100.81, want no row in ip_blocks", attempt.why)
		}
		stored, _, err := s.store.GetBin(hash)
		if err != nil {
			t.Fatalf("GetBin after %s: %v", attempt.why, err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("attacker")); err == nil {
			t.Errorf("the row took the attacker password through %s, want the row unchanged", attempt.why)
		}
	}

	// A candidate longer than bcrypt can hold is a 400 — and it is one before the bin's
	// own gate, with or without Basic, because a bad body fails first.
	long := `{"password":"` + strings.Repeat("p", 73) + `"}`
	for _, attempt := range []struct{ user, password, why string }{
		{hash, "new-pass", "the current Basic"},
		{"", "", "no Basic"},
	} {
		rec := patchBinsJSON(t, s, addr, apiPath, "test-key", long, attempt.user, attempt.password)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("rotate with a 73-byte password with %s = %d, want %d; body %q", attempt.why, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		if got := rec.Body.String(); got != apiEnvelope(400, "bin password too long") {
			t.Errorf("body = %q, want the bin password too long envelope", got)
		}
	}

	// Clear with null: the current password first, then the bin is open again.
	rec = patchBinsJSON(t, s, addr, apiPath, "test-key", `{"password":null}`, hash, "new-pass")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear with null = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if patched = readBinAnswer(t, rec); patched.HasPassword {
		t.Errorf("has_password = true after clearing, want false")
	}
	if stored, _, err := s.store.GetBin(hash); err != nil || stored.PasswordHash != "" {
		t.Errorf("stored hash = %q (err %v), want the bin open", stored.PasswordHash, err)
	}
	if rec := get(t, s, base); rec.Code != http.StatusOK {
		t.Errorf("GET the cleared bin without Basic = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := get(t, s, apiPath); rec.Code != http.StatusOK {
		t.Errorf("GET /api/bins/%s without Basic after the clear = %d, want %d", hash, rec.Code, http.StatusOK)
	}

	// Clear with "": the same door on the other spelling, and the bin writes open.
	second := createTestBinWithPassword(t, s, "second")
	secondHash := strings.Trim(second, "/")
	rec = patchBinsJSON(t, s, addr, "/api/bins/"+secondHash, "test-key", `{"password":""}`, secondHash, "second")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear with \"\" = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if patched = readBinAnswer(t, rec); patched.HasPassword {
		t.Errorf("has_password = true after clearing with \"\", want false")
	}
	if rec := put(t, s, second+"open.txt", "test-key", "open"); rec.Code != http.StatusCreated {
		t.Errorf("PUT into the cleared bin without Basic = %d, want %d", rec.Code, http.StatusCreated)
	}

	// R6 pin: an OPEN bin is protected by a PATCH that gives a password, with no Basic
	// asked for — there is no current password to carry — and from then on the bin gates
	// its writes on the new password.
	open := createTestBin(t, s, "test-key")
	openHash := strings.Trim(open, "/")
	rec = patchBinsJSON(t, s, addr, "/api/bins/"+openHash, "test-key", `{"password":"new"}`, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH a password onto an open bin = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if patched = readBinAnswer(t, rec); !patched.HasPassword {
		t.Errorf("has_password = false, want true after protecting the open bin")
	}
	stored, ok, err = s.store.GetBin(openHash)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", openHash, ok, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("new")); err != nil {
		t.Errorf("the stored hash does not match the given password: %v", err)
	}
	if rec := put(t, s, open+"after.txt", "test-key", "after"); rec.Code != http.StatusUnauthorized {
		t.Errorf("PUT into the newly protected bin without Basic = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	req := httptest.NewRequest(http.MethodPut, open+"after.txt", strings.NewReader("after"))
	req.Header.Set("X-API-Key", "test-key")
	req.SetBasicAuth(openHash, "new")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("PUT with the new password = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestAPIPatchShapes keeps the per-bin surface to its pinned spellings: the old header PATCH
// answers the plain 405 here, a bin the store does not hold is the plain 404, trailing-slash
// and non-hash segments stay 404 whatever the method, an unknown field is a loud 400, and
// none of these touches a bin.
func TestAPIPatchShapes(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.83:1234"

	// The old header spelling retires here: the bin base answers the plain 405 every
	// unnamed method on a bin path gets, and a path below the bin keeps its 405.
	for _, p := range []string{base, base + "notes.md"} {
		rec := patchBinsJSON(t, s, addr, p, "test-key", `{"title":"no"}`, "", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("PATCH %s = %d, want %d; body %q", p, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
			continue
		}
		if rec.Body.String() != "method not allowed\n" {
			t.Errorf("PATCH %s body = %q, want the method not allowed message", p, rec.Body.String())
		}
	}

	// Unknown bins answer the envelope "bin not found"; the trailing-slash and non-hash
	// spellings are not bins at all, so they answer "API endpoint not found".
	for _, c := range []struct{ method, path, want string }{
		{http.MethodPatch, "/api/bins/deadbeef", apiEnvelope(404, "bin not found")},
		{http.MethodPatch, "/api/bins/" + hash + "/", apiEnvelope(404, "API endpoint not found")},
		{http.MethodPatch, "/api/bins/notahash", apiEnvelope(404, "API endpoint not found")},
		{http.MethodGet, "/api/bins/deadbeef", apiEnvelope(404, "bin not found")},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x"}`))
		req.RemoteAddr = addr
		req.Header.Set("X-API-Key", "test-key")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want %d; body %q", c.method, c.path, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		if rec.Body.String() != c.want {
			t.Errorf("%s %s body = %q, want %q", c.method, c.path, rec.Body.String(), c.want)
		}
	}

	// R6 pin: the 400 lands before the 404 — a body this API cannot read is refused
	// even where the bin is unknown, and the refusal names the field problem.
	rec := patchBinsJSON(t, s, addr, "/api/bins/deadbeef", "test-key", `{"titel":"x"}`, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PATCH an unknown bin with a bad body = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	} else if got := rec.Body.String(); got != apiEnvelope(400, "unrecognized field in the request") {
		t.Errorf("unknown bin with a bad body = %q, want the unrecognized field envelope", got)
	}

	// A field this API does not have fails loudly.
	rec = patchBinsJSON(t, s, addr, "/api/bins/"+hash, "test-key", `{"titel":"x"}`, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with an unknown field = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(400, "unrecognized field in the request") {
		t.Errorf("body = %q, want the unrecognized field envelope", got)
	}

	// A known bin path on a method the dispatch does not name is the envelope 405. DELETE is
	// no longer one of these: TestAPIDeleteBinShapes owns its answer now.
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodHead} {
		req := httptest.NewRequest(m, "/api/bins/"+hash, nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != apiEnvelope(405, "method not allowed") {
			t.Errorf("%s /api/bins/%s = %d, body %q; want 405 with the method not allowed envelope", m, hash, rec.Code, rec.Body.String())
		}
	}

	// The bin took none of this.
	if title, description := storedMeta(t, s, base); title != "" || description != "" {
		t.Errorf("stored metadata after the refusals = %q / %q, want it untouched", title, description)
	}
	if files, err := s.store.ListFiles(hash); err != nil || len(files) != 0 {
		t.Errorf("files after the refusals = %d (err %v), want none", len(files), err)
	}
}

// deleteBinFrom DELETEs a path from a fixed socket address: an empty apiKey omits the header,
// and a non-empty basicUser sends Basic auth with basicPassword. A DELETE carries no body.
func deleteBinFrom(t *testing.T, h http.Handler, remoteAddr, path, apiKey, basicUser, basicPassword string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPassword)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAPIDeleteBin is DELETE /api/bins/{hash} on an open bin: the API key, no Basic, and the
// bin is gone — 204 with an empty body. Afterwards every surface says so: the bin's HTML base,
// a path below it, and its inspect route all answer 404 unknown bin, and the bin no longer
// appears in the GET /api/bins listing. The orphaned-empty-bin case — a bin with no files —
// deletes just the same.
func TestAPIDeleteBin(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.84:1234"

	seed := map[string]string{
		"notes.md":      "body of notes.md",
		"drafts/one.md": "body of drafts/one.md",
	}
	for name, body := range seed {
		if rec := put(t, s, base+name, "test-key", body); rec.Code != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want %d; body %q", name, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	rec := listFrom(t, s, addr, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins before the delete = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	listed := false
	for _, b := range readListAnswer(t, rec).Bins {
		if b.Bin == hash {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("GET /api/bins does not yet name bin %s: %s", hash, rec.Body.String())
	}

	rec = deleteBinFrom(t, s, addr, "/api/bins/"+hash, "test-key", "", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/bins/%s = %d, want %d; body %q", hash, rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("204 body = %q, want it empty", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("204 Content-Type = %q, want it unset", ct)
	}

	// Every face of the bin now says it is gone: the bin paths keep the plain unknown bin,
	// the API path answers the envelope.
	for _, c := range []struct{ path, want string }{
		{base, "unknown bin\n"},
		{base + "notes.md", "unknown bin\n"},
		{base + "drafts/one.md", "unknown bin\n"},
		{"/api/bins/" + hash, apiEnvelope(404, "bin not found")},
	} {
		rec = get(t, s, c.path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("after the delete, GET %s = %d, want %d; body %q", c.path, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		if rec.Body.String() != c.want {
			t.Errorf("after the delete, GET %s body = %q, want %q", c.path, rec.Body.String(), c.want)
		}
	}

	// The listing no longer names the bin.
	rec = listFrom(t, s, addr, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins after the delete = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	answer := readListAnswer(t, rec)
	for _, b := range answer.Bins {
		if b.Bin == hash {
			t.Errorf("the listing still names the deleted bin %s: %+v", hash, answer.Bins)
		}
	}

	// The orphaned-empty-bin case: a bin with no files deletes just the same.
	emptyHash := strings.Trim(createTestBin(t, s, "test-key"), "/")
	if rec = deleteBinFrom(t, s, addr, "/api/bins/"+emptyHash, "test-key", "", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE the empty bin = %d, want %d; body %q", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if _, found, err := s.store.GetBin(emptyHash); err != nil || found {
		t.Errorf("empty bin after its delete: found=%v err=%v, want it gone", found, err)
	}
}

// TestAPIDeleteBinProtected is DELETE's bin-password gate: a protected bin goes only to a
// request carrying its current password. No Basic and wrong Basic both answer 401 with the
// challenge, the bin survives each one intact, and no 401 charges the address — the API-key
// lockout is not this gate. The right Basic deletes it with 204 and an empty body.
func TestAPIDeleteBinProtected(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithPassword(t, s, "sesame")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.85:1234"

	rec := deleteBinFrom(t, s, addr, "/api/bins/"+hash, "test-key", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE a protected bin without Basic = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(401, "bin password required") {
		t.Errorf("401 body = %q, want the bin password required envelope", got)
	}
	if www := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(www, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want a Basic challenge", www)
	}
	if _, found, err := s.store.GetBin(hash); err != nil || !found {
		t.Fatalf("the bin after the no-Basic 401: found=%v err=%v, want it intact", found, err)
	}

	rec = deleteBinFrom(t, s, addr, "/api/bins/"+hash, "test-key", hash, "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE with the wrong Basic = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(401, "bin password required") {
		t.Errorf("wrong-Basic 401 body = %q, want the bin password required envelope", got)
	}
	if _, found, err := s.store.GetBin(hash); err != nil || !found {
		t.Fatalf("the bin after the wrong-Basic 401: found=%v err=%v, want it intact", found, err)
	}

	// Neither 401 touched the stored state: the key was right, and a bin-password 401
	// records no failure.
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.85"); ok || failures != 0 {
		t.Errorf("the 401s charged the address = failures %d, row present=%v; want 0 failures, no row", failures, ok)
	}

	rec = deleteBinFrom(t, s, addr, "/api/bins/"+hash, "test-key", hash, "sesame")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE with the right Basic = %d, want %d; body %q", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("204 body = %q, want it empty", got)
	}
	if _, found, err := s.store.GetBin(hash); err != nil || found {
		t.Errorf("the bin after its delete: found=%v err=%v, want it gone", found, err)
	}
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.85"); ok || failures != 0 {
		t.Errorf("the protected DELETEs charged the address = failures %d, row present=%v; want 0 failures, no row", failures, ok)
	}
}

// TestAPIDeleteBinShapes keeps the delete surface to its pinned spellings: the trailing-slash
// variant and a non-hash are the dispatch's 404 "API endpoint not found" answers, a hash the
// store does not hold is the handler's 404 "bin not found" answer, HEAD on the known path is
// the envelope 405, and a missing or wrong API key
// is the 403 requireKey charges — before any bin lookup happens. The known bin took none of it.
func TestAPIDeleteBinShapes(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")
	const addr = "198.51.100.86:1234"

	for _, c := range []struct{ path, want string }{
		{"/api/bins/" + hash + "/", apiEnvelope(404, "API endpoint not found")},
		{"/api/bins/deadbeef", apiEnvelope(404, "bin not found")},
		{"/api/bins/notahash", apiEnvelope(404, "API endpoint not found")},
	} {
		rec := deleteBinFrom(t, s, addr, c.path, "test-key", "", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("DELETE %s = %d, want %d; body %q", c.path, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		if rec.Body.String() != c.want {
			t.Errorf("DELETE %s body = %q, want %q", c.path, rec.Body.String(), c.want)
		}
	}

	// HEAD on the known path is the envelope 405.
	req := httptest.NewRequest(http.MethodHead, "/api/bins/"+hash, nil)
	req.RemoteAddr = addr
	req.Header.Set("X-API-Key", "test-key")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != apiEnvelope(405, "method not allowed") {
		t.Errorf("HEAD /api/bins/%s = %d, body %q; want 405 with the method not allowed envelope", hash, rec.Code, rec.Body.String())
	}

	// None of those keyed refusals touched the stored state.
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.86"); ok || failures != 0 {
		t.Errorf("the keyed 404s and 405 charged the address = failures %d, row present=%v; want 0 failures, no row", failures, ok)
	}

	// No key at all is the 403 requireKey charges, whatever the hash.
	rec = deleteBinFrom(t, s, "198.51.100.87:1234", "/api/bins/"+hash, "", "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE with no key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(403, "invalid API key") {
		t.Errorf("no-key body = %q, want the invalid API key envelope", got)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.87"); failures != 1 || dur != blockStep {
		t.Errorf("after a keyless DELETE = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// A wrong key charges exactly the same, even on an unknown hash: the key guard runs
	// before the lookup.
	rec = deleteBinFrom(t, s, "198.51.100.88:1234", "/api/bins/deadbeef", "wrong", "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if failures, dur := storedBlock(t, s, "198.51.100.88"); failures != 1 || dur != blockStep {
		t.Errorf("after a wrong-key DELETE = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// The known bin took none of this.
	if _, found, err := s.store.GetBin(hash); err != nil || !found {
		t.Errorf("the bin after the refusals: found=%v err=%v, want it untouched", found, err)
	}
}

// listAnswer is the test's own reading of the GET /api/bins answer: the page of bin objects,
// and the next_cursor only a further page earns.
type listAnswer struct {
	Bins       []binAnswer `json:"bins"`
	NextCursor string      `json:"next_cursor"`
}

// readListAnswer decodes a GET /api/bins answer into its page and cursor.
func readListAnswer(t *testing.T, rec *httptest.ResponseRecorder) listAnswer {
	t.Helper()
	var l listAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("list answer %q is not a bins object: %v", rec.Body.String(), err)
	}
	return l
}

// assertKeysetOrder pins the newest-first spelling, created_at DESC then hash DESC, across a
// walk's rows: each row sits strictly ahead of the next, pages joined or not.
func assertKeysetOrder(t *testing.T, bins []binAnswer) {
	t.Helper()
	for i := 1; i < len(bins); i++ {
		prev, cur := bins[i-1], bins[i]
		if prev.CreatedAt < cur.CreatedAt || (prev.CreatedAt == cur.CreatedAt && prev.Bin <= cur.Bin) {
			t.Errorf("bins[%d] (%s %s) is not strictly ahead of bins[%d] (%s %s) on created_at DESC, hash DESC",
				i-1, prev.CreatedAt, prev.Bin, i, cur.CreatedAt, cur.Bin)
		}
	}
}

// checkListedBin holds one listed object against the bin object its create answered: the
// listing shows the row complete, the owner's view, with nothing redacted.
func checkListedBin(t *testing.T, want, got binAnswer) {
	t.Helper()
	if got.Bin != want.Bin || got.URL != want.URL || got.Title != want.Title ||
		got.Description != want.Description || got.HasPassword != want.HasPassword ||
		got.CreatedAt != want.CreatedAt {
		t.Errorf("listed %+v, want the created bin %+v", got, want)
	}
}

// listFrom GETs a /api/bins listing from a fixed socket address with the API key.
func listFrom(t *testing.T, h http.Handler, remoteAddr, query string) *httptest.ResponseRecorder {
	t.Helper()
	return getFrom(t, h, remoteAddr, "/api/bins"+query, "test-key")
}

// TestAPIListBins is GET /api/bins: the listing is key-gated — a wrong key charges exactly
// like a wrong key on a write, and a blocked address is refused with its wait — and it answers
// the store's newest-first pages: the default page is 30 rows, its next_cursor is the pinned
// encoding of the page's last row, following it returns the rest, and the last page names no
// cursor. A protected bin lists complete. An empty instance answers an array, not null.
func TestAPIListBins(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.90:1234"

	// An instance with no bins answers the array, not null, and no cursor.
	rec := listFrom(t, s, addr, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins on the empty instance = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{\"bins\":[]}\n" {
		t.Errorf("empty instance body = %q, want {\"bins\":[]} with its trailing newline", got)
	}

	// Seed 31 bins so the default page has a further page to name, plus a guarded one that
	// must list complete.
	want := make(map[string]binAnswer, 32)
	for i := range 31 {
		title := fmt.Sprintf("listed bin %02d", i)
		rec = postAPIBins(t, s, "198.51.100.95:1234", "test-key",
			`{"title":"`+title+`","description":"seed `+title+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST seed bin %02d = %d, want %d; body %q", i, rec.Code, http.StatusCreated, rec.Body.String())
		}
		bin := createdBin(t, rec)
		want[bin.Bin] = bin
	}
	rec = postAPIBins(t, s, "198.51.100.95:1234", "test-key", `{"title":"guarded","password":"sesame"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST the guarded bin = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	guarded := createdBin(t, rec)
	if !guarded.HasPassword {
		t.Fatalf("the guarded bin answers has_password false, want true")
	}
	want[guarded.Bin] = guarded

	// Page 1 at the default size: 30 newest-first complete objects and a next_cursor.
	rec = listFrom(t, s, addr, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	page1 := readListAnswer(t, rec)
	if len(page1.Bins) != 30 {
		t.Fatalf("page 1 = %d bins, want the default 30; body %q", len(page1.Bins), rec.Body.String())
	}
	assertKeysetOrder(t, page1.Bins)
	if page1.NextCursor == "" {
		t.Fatalf("page 1 with a further page names no next_cursor; body %q", rec.Body.String())
	}
	decoded, err := base64.RawURLEncoding.DecodeString(page1.NextCursor)
	if err != nil {
		t.Fatalf("next_cursor %q is not unpadded URL-safe base64: %v", page1.NextCursor, err)
	}
	last := page1.Bins[29]
	if string(decoded) != last.CreatedAt+"\x1f"+last.Bin {
		t.Errorf("next_cursor decodes to %q, want created_at + unit separator + hash of the last row (%s \x1f %s)",
			decoded, last.CreatedAt, last.Bin)
	}
	seen := make(map[string]bool, len(want))
	for _, b := range page1.Bins {
		checkListedBin(t, want[b.Bin], b)
		seen[b.Bin] = true
	}

	// Following the cursor returns the rest, and the last page omits next_cursor.
	rec = listFrom(t, s, addr, "?cursor="+page1.NextCursor)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins with the cursor = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	page2 := readListAnswer(t, rec)
	if len(page2.Bins) != 2 {
		t.Fatalf("page 2 = %d bins, want the remaining 2; body %q", len(page2.Bins), rec.Body.String())
	}
	if page2.NextCursor != "" || strings.Contains(rec.Body.String(), "next_cursor") {
		t.Errorf("last page body %q carries a next_cursor, want it omitted", rec.Body.String())
	}
	// The pages join without a gap: page 2's first row sits strictly after page 1's last.
	if first := page2.Bins[0]; !(last.CreatedAt > first.CreatedAt || (last.CreatedAt == first.CreatedAt && last.Bin > first.Bin)) {
		t.Errorf("page 2 starts at (%s %s), want strictly after page 1's last row (%s %s)",
			first.CreatedAt, first.Bin, last.CreatedAt, last.Bin)
	}
	for _, b := range page2.Bins {
		if seen[b.Bin] {
			t.Errorf("bin %s appears on both pages, want no repeats", b.Bin)
			continue
		}
		checkListedBin(t, want[b.Bin], b)
		seen[b.Bin] = true
	}
	if len(seen) != len(want) {
		t.Errorf("the walk covered %d bins, want all %d seeded; body %q", len(seen), len(want), rec.Body.String())
	}
	// The guarded bin listed complete: has_password true and its title in plain view.
	for _, b := range append(page1.Bins, page2.Bins...) {
		if b.Bin == guarded.Bin && (!b.HasPassword || b.Title != "guarded") {
			t.Errorf("the guarded bin listed has_password %v title %q, want the row shown complete", b.HasPassword, b.Title)
		}
	}

	// A wrong key through the listing is the writes' refusal and the writes' charge.
	rec = getFrom(t, s, "198.51.100.92:1234", "/api/bins", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/bins with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if got := rec.Body.String(); got != apiEnvelope(403, "invalid API key") {
		t.Errorf("403 body = %q, want the invalid API key envelope", got)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.92"); failures != 1 || dur != blockStep {
		t.Errorf("after a wrong key = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	// No key at all is the same refusal.
	if rec := get(t, s, "/api/bins"); rec.Code != http.StatusForbidden || rec.Body.String() != apiEnvelope(403, "invalid API key") {
		t.Errorf("GET /api/bins without the header = %d, body %q; want 403 invalid API key", rec.Code, rec.Body.String())
	}

	// The blocked address is refused with an honest wait, and the attempt charges again.
	rec = listFrom(t, s, "198.51.100.92:1234", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked GET /api/bins = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too many bad API keys") {
		t.Errorf("429 body = %q, want the too many bad API keys message", rec.Body.String())
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.92"); failures != 2 || dur != 2*blockStep {
		t.Errorf("after a blocked attempt = failures %d, block %v; want 2 failures, %v", failures, dur, 2*blockStep)
	}
}

// TestAPIListBinsLimits is the pinned limit rule: limit=2 pages a five-bin instance 2, 2, 1
// with no repeats; limit=0, a negative, and a non-integer are the plain 400 invalid limit;
// and a limit above the served maximum clamps to 100 — observable because the clamp half
// seeds 101 bins, per ruling R3.
func TestAPIListBinsLimits(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.93:1234"

	hashes := make(map[string]bool, 101)
	for i := range 5 {
		base := createTestBinWithMetadata(t, s, "test-key", fmt.Sprintf("limit bin %03d", i), "")
		hashes[strings.Trim(base, "/")] = true
	}

	// limit=2 walks the five bins in pages of 2, 2, 1 — each exactly once.
	seen := make(map[string]bool, 5)
	cursor := ""
	for page, wantCount := range []int{2, 2, 1} {
		query := "?limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		rec := listFrom(t, s, addr, query)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/bins%s = %d, want %d; body %q", query, rec.Code, http.StatusOK, rec.Body.String())
		}
		answer := readListAnswer(t, rec)
		if len(answer.Bins) != wantCount {
			t.Fatalf("page %d = %d bins, want %d; body %q", page+1, len(answer.Bins), wantCount, rec.Body.String())
		}
		for _, b := range answer.Bins {
			if !hashes[b.Bin] {
				t.Errorf("page %d lists %s, want only the seeded bins", page+1, b.Bin)
				continue
			}
			if seen[b.Bin] {
				t.Errorf("bin %s repeated across pages, want each exactly once", b.Bin)
			}
			seen[b.Bin] = true
		}
		if page == 2 {
			if answer.NextCursor != "" || strings.Contains(rec.Body.String(), "next_cursor") {
				t.Errorf("last page body %q carries a next_cursor, want it omitted", rec.Body.String())
			}
			break
		}
		if answer.NextCursor == "" {
			t.Fatalf("page %d with a further page names no next_cursor; body %q", page+1, rec.Body.String())
		}
		cursor = answer.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("the walk covered %d bins, want all 5 seeded", len(seen))
	}

	// limit=0, a negative and a non-integer are refused, and the instance is untouched.
	before := storedBinCount(t, s)
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=abc"} {
		rec := listFrom(t, s, addr, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET /api/bins%s = %d, want %d; body %q", q, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != apiEnvelope(400, "invalid limit") {
			t.Errorf("GET /api/bins%s body = %q, want the invalid limit envelope", q, got)
		}
	}
	if after := storedBinCount(t, s); after != before {
		t.Errorf("bins after the refusals = %d, want the same %d", after, before)
	}

	// Ruling R3: 101 bins are seeded, so limit=500 clamping to 100 is observable — page 1
	// holds exactly the 100 the maximum serves with a cursor, and the cursor names the last one.
	for i := 5; i < 101; i++ {
		base := createTestBinWithMetadata(t, s, "test-key", fmt.Sprintf("limit bin %03d", i), "")
		hashes[strings.Trim(base, "/")] = true
	}
	rec := listFrom(t, s, addr, "?limit=500")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins?limit=500 = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	clamped := readListAnswer(t, rec)
	if len(clamped.Bins) != 100 {
		t.Fatalf("limit=500 answered %d bins, want the clamped 100", len(clamped.Bins))
	}
	if clamped.NextCursor == "" {
		t.Fatalf("the 100-row page of 101 bins names no next_cursor; body %q", rec.Body.String())
	}
	rec = listFrom(t, s, addr, "?limit=500&cursor="+clamped.NextCursor)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the clamped cursor = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	tail := readListAnswer(t, rec)
	if len(tail.Bins) != 1 || tail.NextCursor != "" {
		t.Errorf("the clamped walk's last page = %d bins, next_cursor %q; want the 1 remaining and no cursor",
			len(tail.Bins), tail.NextCursor)
	}
}

// TestAPIListBinsCursorIntegrity is the cursor's two halves: a malformed cursor is the plain
// 400 invalid cursor, and a good one walks the store with no repeats and no gaps even when a
// newer bin lands between the pages — keyset stability. Bins the store holds at one identical
// instant page out by hash desc.
func TestAPIListBinsCursorIntegrity(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.94:1234"

	// A cursor this API never made is refused: nonsense decodes to bytes with no separator; a
	// clean base64 word holds none either; padding is not this cursor's alphabet; an empty
	// created_at half names no position; and a second separator hides inside the hash half,
	// which no created_at or hash can carry.
	for _, c := range []string{
		"nonsense",
		base64.RawURLEncoding.EncodeToString([]byte("no separator here")),
		"YWJjZA==",
		base64.RawURLEncoding.EncodeToString([]byte("\x1fdeadbeef")),
		base64.RawURLEncoding.EncodeToString([]byte("2020-01-01T00:00:00Z\x1fdeadbeef\x1fextra")),
	} {
		rec := listFrom(t, s, addr, "?cursor="+c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cursor %q = %d, want %d; body %q", c, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := rec.Body.String(); got != apiEnvelope(400, "invalid cursor") {
			t.Errorf("cursor %q body = %q, want the invalid cursor envelope", c, got)
		}
	}

	// Two bins the store holds at one identical instant, older than everything the API seeds,
	// so their paging can only come out by hash desc.
	for _, h := range []string{"0a0a0a0a", "0b0b0b0b"} {
		if _, err := s.store.db.Exec(
			`INSERT INTO bins (hash, password_hash, created_at, title, description) VALUES (?, '', ?, 'twin', '')`,
			h, "2020-01-01T00:00:00Z"); err != nil {
			t.Fatalf("insert the twin bin %s: %v", h, err)
		}
	}

	wanted := map[string]string{"0a0a0a0a": "twin", "0b0b0b0b": "twin"}
	for _, title := range []string{"walk one", "walk two", "walk three", "walk four"} {
		base := createTestBinWithMetadata(t, s, "test-key", title, "")
		wanted[strings.Trim(base, "/")] = title
	}

	// Walk the whole instance two rows at a time. After page 1, a newer bin lands.
	all := make([]binAnswer, 0, 7)
	seen := make(map[string]int, 7)
	cursor := ""
	for {
		query := "?limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		rec := listFrom(t, s, addr, query)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/bins%s = %d, want %d; body %q", query, rec.Code, http.StatusOK, rec.Body.String())
		}
		page := readListAnswer(t, rec)
		if len(page.Bins) == 0 {
			t.Fatalf("GET /api/bins%s answered no rows with a cursor in hand; body %q", query, rec.Body.String())
		}
		for _, b := range page.Bins {
			seen[b.Bin]++
		}
		all = append(all, page.Bins...)
		if page.NextCursor == "" {
			break
		}
		if len(page.Bins) != 2 {
			t.Fatalf("a page with a next_cursor holds %d rows, want the full 2; body %q", len(page.Bins), rec.Body.String())
		}
		cursor = page.NextCursor
		if len(all) > 8 {
			t.Fatalf("the walk did not end; last body %q", rec.Body.String())
		}
		if len(all) == 2 {
			// A newer bin lands between the pages: keyset paging must not shift or repeat
			// anything the walk already carried.
			createTestBinWithMetadata(t, s, "test-key", "late arrival", "")
		}
	}

	// No gaps and no repeats: every wanted bin was walked exactly once, the joined walk is in
	// keyset order across the page joints, and only the late bin may show up among the extras —
	// once, or not at all when its key sits ahead of a cutoff.
	for _, b := range all {
		if title, ok := wanted[b.Bin]; ok {
			if b.Title != title {
				t.Errorf("bin %s listed title %q, want %q", b.Bin, b.Title, title)
			}
			continue
		}
		if b.Title != "late arrival" {
			t.Errorf("the walk names unknown bin %s (%q), want only the seeded rows and the late one", b.Bin, b.Title)
		}
	}
	for h, title := range wanted {
		if seen[h] != 1 {
			t.Errorf("bin %s (%s) was walked %d times, want exactly once — no repeats, no gaps", h, title, seen[h])
		}
	}
	assertKeysetOrder(t, all)

	// The equal-created_at twins paged out last, in hash-desc order.
	if len(all) < 2 || all[len(all)-2].Bin != "0b0b0b0b" || all[len(all)-1].Bin != "0a0a0a0a" {
		got := ""
		if len(all) >= 2 {
			got = all[len(all)-2].Bin + ", " + all[len(all)-1].Bin
		}
		t.Errorf("the walk's last rows = %s, want 0b0b0b0b before 0a0a0a0a: equal created_at pages by hash DESC", got)
	}
}

// TestBinPageNoMetadataHeaders is ruling R1 at the page: the title and description a bin's
// listing shows come from the DB row, and they travel in the body only — the
// X-Bin-Title and X-Bin-Description response headers retire with the JSON PATCH surface,
// on the listing branch and the raw index.html one alike.
func TestBinPageNoMetadataHeaders(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithMetadata(t, s, "test-key", "Test report", "findings of the audit")
	hash := strings.Trim(base, "/")

	rec := get(t, s, base)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the bin base = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Test report") || !strings.Contains(body, "findings of the audit") {
		t.Errorf("the listing does not show the stored title/description:\n%s", body)
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "" {
		t.Errorf("X-Bin-Title = %q, want no metadata header on any bin page", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "" {
		t.Errorf("X-Bin-Description = %q, want no metadata header on any bin page", got)
	}

	// The raw index.html branch carries nothing either.
	if rec := put(t, s, base+"index.html", "test-key", "<h1>Custom index</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT index.html = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec = get(t, s, base)
	if got := rec.Body.String(); got != "<h1>Custom index</h1>" {
		t.Errorf("body = %q, want the stored index.html verbatim", got)
	}
	if got := rec.Header().Get("X-Bin-Title"); got != "" {
		t.Errorf("X-Bin-Title on the raw index.html = %q, want it absent", got)
	}
	if got := rec.Header().Get("X-Bin-Description"); got != "" {
		t.Errorf("X-Bin-Description on the raw index.html = %q, want it absent", got)
	}

	// The metadata itself is not gone: the JSON inspect answer still carries it.
	rec = get(t, s, "/api/bins/"+hash)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins/%s = %d, want %d", hash, rec.Code, http.StatusOK)
	}
	if b := readBinAnswer(t, rec); b.Title != "Test report" || b.Description != "findings of the audit" {
		t.Errorf("inspect metadata = %q / %q, want the stored Test report / findings of the audit", b.Title, b.Description)
	}
}

// TestAPIRouteTableIsPublishedSurface pins the authoritative route table to the
// published /api surface exactly: the four paths, their allowed methods, and nothing
// else. Dispatch, the charging gates and the generated OpenAPI document all read this
// table, so a registration that adds, drops or re-spells a route fails here. The table
// and the mux are built from one source, so the pin also pairs them: every table entry×
// method must match a real pattern in the returned mux (Go 1.26 ServeMux.Handler names
// the matched pattern), and a method the table does not list for a known path must match
// nothing — the mux alone must never answer a route the table has not named.
func TestAPIRouteTableIsPublishedSurface(t *testing.T) {
	s := newTestServer(t, "test-key")
	mux, methods := s.newAPIRoutes()

	want := map[string][]string{
		"/api":             {http.MethodGet, http.MethodHead},
		"/api/key":         {http.MethodPost},
		"/api/health":      {http.MethodGet, http.MethodHead},
		"/api/bins":        {http.MethodGet, http.MethodPost},
		"/api/bins/{hash}": {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/openapi.json":    {http.MethodGet},
	}
	if len(methods) != len(want) {
		t.Fatalf("the table names %d paths, want %d: %v", len(methods), len(want), methods)
	}
	for path, wantMethods := range want {
		got := methods[path]
		if got == nil {
			t.Errorf("the table has no entry for %s, want methods %v", path, wantMethods)
			continue
		}
		if len(got) != len(wantMethods) {
			t.Errorf("methods for %s = %v, want exactly %v", path, got, wantMethods)
			continue
		}
		for i := range wantMethods {
			if got[i] != wantMethods[i] {
				t.Errorf("methods for %s = %v, want exactly %v", path, got, wantMethods)
				break
			}
		}
		for _, method := range wantMethods {
			if _, pattern := mux.Handler(httptest.NewRequest(method, path, nil)); pattern == "" {
				t.Errorf("the table names %s %s but the mux holds no pattern matching it", method, path)
			}
		}
	}
	if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPut, "/api/bins", nil)); pattern != "" {
		t.Errorf("PUT /api/bins matches mux pattern %q; want an empty pattern: the table names PUT nowhere, so the mux must hold no route for it", pattern)
	}
}

// TestAPILiteralBraceSpellingIsAPINotFound is ruling R3's parity pin: the table's
// wildcard key /api/bins/{hash} names only hex spellings through the apiBinPathRE gate, so
// the literal spelling itself — raw braces, or percent-encoded, both parsing to URL.Path
// "/api/bins/{hash}" — is the 404 envelope API endpoint not found on every method, and a wrong
// key through it charges nothing, exactly like the retired switch never named it.
func TestAPILiteralBraceSpellingIsAPINotFound(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, spelling := range []string{"/api/bins/{hash}", "/api/bins/%7Bhash%7D"} {
		rec := get(t, s, spelling)
		if rec.Code != http.StatusNotFound || rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
			t.Errorf("GET %s = %d, body %q; want 404 API endpoint not found", spelling, rec.Code, rec.Body.String())
		}

		req := httptest.NewRequest(http.MethodHead, spelling, nil)
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
			t.Errorf("HEAD %s = %d, body %q; want 404 API endpoint not found, not a 405", spelling, rec.Code, rec.Body.String())
		}

		const addr = "198.51.100.97:1234"
		rec = patchBinsJSON(t, s, addr, spelling, "wrong-key", "{}", "", "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
			t.Errorf("PATCH %s with a wrong key = %d, body %q; want 404 API endpoint not found", spelling, rec.Code, rec.Body.String())
		}
		rec = deleteBinFrom(t, s, addr, spelling, "wrong-key", "", "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
			t.Errorf("DELETE %s with a wrong key = %d, body %q; want 404 API endpoint not found", spelling, rec.Code, rec.Body.String())
		}
		if failures, ok := ipBlockRow(t, s.store, "198.51.100.97"); ok || failures != 0 {
			t.Errorf("the wrong-key literal-brace attempts charged 198.51.100.97 = failures %d, row present=%v; want 0 failures, no row", failures, ok)
		}
	}
}

// TestAPIErrorEnvelopeShape is the envelope's pinned shape on a refusal the dispatcher answers:
// Content-Type application/json and exactly {"status":"error","code":<the status just
// answered>,"message":"<the existing sentence>"} in that field order, with the trailing
// newline. The 400 half of POST /api/bins' title cap is a dispatcher refusal; the decode half
// of create's 400s joins in a later task and is not this pin's.
func TestAPIErrorEnvelopeShape(t *testing.T) {
	s := newTestServer(t, "test-key")

	rec := postAPIBins(t, s, "198.51.100.71:1234", "test-key", `{"title":"`+strings.Repeat("t", maxTitleBytes+1)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/bins with an over-long title = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	want := "{\"status\":\"error\",\"code\":400,\"message\":\"title over 200 bytes\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("400 body = %q, want the envelope %q", got, want)
	}
}

// TestAPIEndpointNotFoundPair is the 404 pair: a path the API table does not name answers the
// envelope with API endpoint not found, and an endpoint whose hash the store does not hold
// answers the envelope with bin not found.
func TestAPIEndpointNotFoundPair(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, p := range []string{"/api/bananas", "/api/bins/"} {
		rec := get(t, s, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusNotFound, rec.Body.String())
			continue
		}
		want := "{\"status\":\"error\",\"code\":404,\"message\":\"API endpoint not found\"}\n"
		if got := rec.Body.String(); got != want {
			t.Errorf("GET %s body = %q, want the endpoint envelope %q", p, got, want)
		}
	}

	rec := get(t, s, "/api/bins/deadbeef")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/bins/deadbeef = %d, want %d; body %q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	want := "{\"status\":\"error\",\"code\":404,\"message\":\"bin not found\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("GET /api/bins/deadbeef body = %q, want the resource envelope %q", rec.Body.String(), want)
	}
}

// TestKeyRefusalIsEnvelope keeps the key guard's story inside the envelope: a wrong key is the
// 403 envelope invalid API key and still charges, and the lockout refusal 429 keeps its
// Retry-After header next to the envelope.
func TestKeyRefusalIsEnvelope(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.77:1234"

	rec := postAPIBins(t, s, addr, "wrong", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/bins with a wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("403 Content-Type = %q, want application/json", ct)
	}
	want := "{\"status\":\"error\",\"code\":403,\"message\":\"invalid API key\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("403 body = %q, want the key envelope %q", got, want)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.77"); failures != 1 || dur != blockStep {
		t.Errorf("after the envelope 403 = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	rec = postAPIBins(t, s, addr, "test-key", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked POST /api/bins = %d, want %d; body %q", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	want = "{\"status\":\"error\",\"code\":429,\"message\":\"too many bad API keys\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("429 body = %q, want the lockout envelope %q", got, want)
	}
	if ra := retryAfter(t, rec); ra <= 1800-30 || ra > 1800 {
		t.Errorf("Retry-After = %d, want the first block's 1800 seconds less the elapsed time", ra)
	}
}

// TestProtectedBinAPI401IsEnvelope proves the envelope does not disturb what the 401 means:
// PATCH of a protected bin without Basic answers the envelope bin password required and keeps
// the WWW-Authenticate challenge.
func TestProtectedBinAPI401IsEnvelope(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBinWithPassword(t, s, "sesame")
	hash := strings.Trim(base, "/")

	rec := patchBinsJSON(t, s, "198.51.100.78:1234", "/api/bins/"+hash, "test-key", `{"title":"x"}`, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH the protected bin without Basic = %d, want %d; body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	want := "{\"status\":\"error\",\"code\":401,\"message\":\"bin password required\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("401 body = %q, want the bin-password envelope %q", got, want)
	}
	if www := rec.Header().Get("WWW-Authenticate"); www != `Basic realm="sandbin"` {
		t.Errorf("WWW-Authenticate = %q, want the Basic realm challenge", www)
	}
}

// TestAPIRequestShapeRefusalsAreEnvelopes is the request-shape 400 family inside the envelope:
// a body this API cannot read, an unrecognized field, a limit below 1, a malformed cursor and
// an over-long title each answer the exact envelope — the pinned sentence verbatim as message,
// code equal to the status, application/json as the media type.
func TestAPIRequestShapeRefusalsAreEnvelopes(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.79:1234"

	for _, c := range []struct{ what, body, want string }{
		{"malformed JSON", "not json", apiEnvelope(400, "could not read the request")},
		{"unrecognized field", `{"titel":"x"}`, apiEnvelope(400, "unrecognized field in the request")},
		{"over-long title", `{"title":"` + strings.Repeat("t", 300) + `"}`, apiEnvelope(400, "title over 200 bytes")},
	} {
		rec := postAPIBins(t, s, addr, "test-key", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /api/bins with %s = %d, want %d; body %q", c.what, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("POST /api/bins with %s Content-Type = %q, want application/json", c.what, ct)
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("POST /api/bins with %s body = %q, want the envelope %q", c.what, got, c.want)
		}
	}

	for _, c := range []struct{ query, want string }{
		{"?limit=0", apiEnvelope(400, "invalid limit")},
		{"?cursor=nonsense", apiEnvelope(400, "invalid cursor")},
	} {
		rec := listFrom(t, s, addr, c.query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET /api/bins%s = %d, want %d; body %q", c.query, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/bins%s Content-Type = %q, want application/json", c.query, ct)
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("GET /api/bins%s body = %q, want the envelope %q", c.query, got, c.want)
		}
	}
}
