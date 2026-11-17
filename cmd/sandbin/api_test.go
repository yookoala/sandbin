package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

// TestAPIUnknownShapes keeps the /api skeleton to what it answers: a spelling it does not
// own — trailing-slash variants included — is the plain 404 unknown bin, and a known path on
// a wrong method is the plain 405. /api/bins is the name Tasks 3–7 fill in, so today it
// answers no method at all.
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
