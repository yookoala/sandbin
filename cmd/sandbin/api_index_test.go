package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yookoala/sandbin/internal/version"
)

// indexAnswer is the test's own reading of the GET /api answer: the version stamp, the
// endpoint rows with their per-method descriptions, and the openapi sibling.
type indexAnswer struct {
	Version   string `json:"version"`
	Endpoints []struct {
		URL     string `json:"url"`
		Methods []struct {
			Method      string `json:"method"`
			Description string `json:"description"`
		} `json:"methods"`
	} `json:"endpoints"`
	OpenAPI struct {
		URL         string `json:"url"`
		Description string `json:"description"`
	} `json:"openapi"`
}

// readIndexAnswer decodes a GET /api answer into its pinned shape.
func readIndexAnswer(t *testing.T, rec *httptest.ResponseRecorder) indexAnswer {
	t.Helper()
	var a indexAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("index answer %q is not an index object: %v", rec.Body.String(), err)
	}
	return a
}

// TestAPIIndexBodyIsThePublishedSurface is the golden pin of the index format decided in
// docs/decisions/0031-api-index-endpoint.md: the whole body, byte for byte, against a pinned
// direct-Host context (Host 127.0.0.1:8080 on the loopback socket, the decision doc's own
// example). Paths, methods, full URLs and every description sentence are literal test-side
// copies of the decision doc's wording — only version.Value interpolates — so rewording a
// description must consciously move this test along with the map. A golden assembled from the
// map itself would pin nothing about wording.
func TestAPIIndexBodyIsThePublishedSurface(t *testing.T) {
	s := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	want := `{"version":"` + version.Value + `","endpoints":[` +
		`{"url":"http://127.0.0.1:8080/api/key","methods":[` +
		`{"method":"POST","description":"Checks an API key without touching a bin: a good key answers 204, a wrong key costs the requesting address a lockout step."}` +
		`]},` +
		`{"url":"http://127.0.0.1:8080/api/health","methods":[` +
		`{"method":"GET","description":"Unauthenticated liveness probe; answers 200 even while an address is blocked."},` +
		`{"method":"HEAD","description":"The same probe with no body."}` +
		`]},` +
		`{"url":"http://127.0.0.1:8080/api/bins","methods":[` +
		`{"method":"GET","description":"Lists bins with cursor paging; a wrong key costs the address a lockout step."},` +
		`{"method":"POST","description":"Creates a bin from a JSON body; 201 answers with the bin object and a Location header."}` +
		`]},` +
		`{"url":"http://127.0.0.1:8080/api/bins/{hash}","methods":[` +
		`{"method":"GET","description":"Inspects one bin; a password-protected bin needs the bin password."},` +
		`{"method":"PATCH","description":"Updates title, description, or password; a wrong key costs the address a lockout step."},` +
		`{"method":"DELETE","description":"Deletes the bin; the request takes no body."}` +
		`]}],` +
		`"openapi":{"url":"http://127.0.0.1:8080/openapi.json","description":"The OpenAPI 3.1 document describing this whole surface."}}` +
		"\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("GET /api body = %q, want the pinned index %q", got, want)
	}
}

// TestAPIIndexURLsFollowRequestContext pins the full URLs to the request context through
// frontBase unchanged: a trusted loopback socket carrying X-Forwarded-Host and
// X-Forwarded-Proto: https renders https:// with the rightmost forwarded host, and a Host
// failing frontHostRE falls back to the https://this.host placeholder. Nothing in the body
// is hardcoded; every URL is the base plus the table's own path spelling.
func TestAPIIndexURLsFollowRequestContext(t *testing.T) {
	s := newTestServer(t, "test-key")

	for _, c := range []struct {
		name       string
		host       string
		forwarded  string // X-Forwarded-Host
		proto      string // X-Forwarded-Proto
		remoteAddr string
		wantBase   string
	}{
		{"loopback proxy renders the rightmost forwarded host", "internal.socket:8080", "first.example.org, sandbox.example.org", "https", "127.0.0.1:1234", "https://sandbox.example.org"},
		{"hostile host falls back", "bad host<!<script>", "", "", "127.0.0.1:1234", "https://this.host"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api", nil)
		req.Host = c.host
		// httptest.NewRequest's default socket is not loopback; frontBase's trust rule reads
		// the socket, so the trusted-proxy context pins it explicitly, as the front-page tables do.
		req.RemoteAddr = c.remoteAddr
		if c.forwarded != "" {
			req.Header.Set("X-Forwarded-Host", c.forwarded)
		}
		if c.proto != "" {
			req.Header.Set("X-Forwarded-Proto", c.proto)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: GET /api = %d, want %d; body %q", c.name, rec.Code, http.StatusOK, rec.Body.String())
			continue
		}

		answer := readIndexAnswer(t, rec)
		wantURLs := []string{
			c.wantBase + "/api/key",
			c.wantBase + "/api/health",
			c.wantBase + "/api/bins",
			c.wantBase + "/api/bins/{hash}",
		}
		if len(answer.Endpoints) != len(wantURLs) {
			t.Fatalf("%s: the index names %d endpoints, want %d; body %q", c.name, len(answer.Endpoints), len(wantURLs), rec.Body.String())
		}
		for i, endpoint := range answer.Endpoints {
			if endpoint.URL != wantURLs[i] {
				t.Errorf("%s: endpoints[%d].url = %q, want %q", c.name, i, endpoint.URL, wantURLs[i])
			}
		}
		if answer.OpenAPI.URL != c.wantBase+"/openapi.json" {
			t.Errorf("%s: openapi.url = %q, want %q", c.name, answer.OpenAPI.URL, c.wantBase+"/openapi.json")
		}
	}
}

// TestAPIIndexShapes keeps the index's own dispatch shapes pinned: HEAD /api answers 200 —
// status and headers pinned, because httptest.NewRecorder keeps HEAD bodies and only the
// real server strips them, so the empty-body fact belongs to the scratch curl walk —
// POST, PUT, PATCH and DELETE on the bare /api are the envelope 405 the table gives every named
// path on a method it does not list, and /api/ is the envelope 404: the mux's would-be 301 is
// cut off by the classification like every other trailing-slash spelling.
func TestAPIIndexShapes(t *testing.T) {
	s := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodHead, "/api", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD /api = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("HEAD /api Content-Type = %q, want application/json", ct)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api", nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != apiEnvelope(405, "method not allowed") {
			t.Errorf("%s /api = %d, body %q; want 405 with the method not allowed envelope", method, rec.Code, rec.Body.String())
		}
	}

	rec = get(t, s, "/api/")
	if rec.Code != http.StatusNotFound || rec.Body.String() != apiEnvelope(404, "API endpoint not found") {
		t.Errorf("GET /api/ = %d, body %q; want 404 API endpoint not found, not the mux's 301", rec.Code, rec.Body.String())
	}
}

// TestAPIIndexNeverCharges pins the index outside the charging machinery, the health/document
// family rule: no key and wrong keys answer it alike and RecordFailure never fires — proven
// because the same wrong key still gets its first 403 from a write after all those index
// reads — and even an address in the block still gets its index, while the same wrong key on
// a write proves the block is real around it.
func TestAPIIndexNeverCharges(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.45:1234"

	for _, key := range []string{"", "wrong-key", "wrong-key", "test-key", "wrong-key"} {
		rec := getFrom(t, s, addr, "/api", key)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api (key %q) = %d, want %d; body %q", key, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.45"); ok || failures != 0 {
		t.Fatalf("five index reads charged 198.51.100.45 = failures %d, row present %v; want no charge at all", failures, ok)
	}

	// The same wrong key now meets a write and gets its first 403. Had any index read
	// charged, this would already be a 429 behind a block.
	if rec := postFrom(t, s, addr, "wrong-key"); rec.Code != http.StatusForbidden {
		t.Fatalf("the write with the same wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	// One failure blocks the address (the 0029 rule): the next wrong-key write is the 429.
	if rec := postFrom(t, s, addr, "wrong-key"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the blocked write = %d, want %d; the address is not blocked, so the next pin proves nothing", rec.Code, http.StatusTooManyRequests)
	}

	// Blocked, the index answers anyway.
	for _, key := range []string{"", "wrong-key", "test-key"} {
		rec := getFrom(t, s, addr, "/api", key)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /api while blocked (key %q) = %d, want %d; the index must answer even while the address is blocked", key, rec.Code, http.StatusOK)
		}
	}

	// The block is real around it: the same wrong key on the write is still a 429, and the
	// index reads added nothing to the row.
	if rec := postFrom(t, s, addr, "wrong-key"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the write after the blocked index reads = %d, want %d; the block around the index is not real", rec.Code, http.StatusTooManyRequests)
	}
	if failures, _ := ipBlockRow(t, s.store, "198.51.100.45"); failures != 3 {
		t.Errorf("after the blocked index reads the row holds failures %d, want the writes' 3 alone; an index read charged", failures)
	}
}

// TestAPIIndexDescriptionsCoverTheTableOneForOne is one half of the anti-drift triangle: the
// descriptions map covers every pattern the route table names, exactly — a route registered
// without a description fails here, and so does a description for a pattern the table does
// not name. The two index patterns are the single sanctioned exception: the index does not
// describe itself.
func TestAPIIndexDescriptionsCoverTheTableOneForOne(t *testing.T) {
	s := newTestServer(t, "test-key")
	_, table := s.newAPIRoutes()

	want := map[string]bool{}
	for path, methods := range table {
		for _, method := range methods {
			pattern := method + " " + path
			if pattern == apiPatternIndexGet || pattern == apiPatternIndexHead {
				continue
			}
			want[pattern] = true
		}
	}

	if len(apiIndexDescriptions) != len(want) {
		t.Fatalf("the descriptions map holds %d entries, want exactly %d (the table patterns minus the two index patterns)", len(apiIndexDescriptions), len(want))
	}
	for pattern := range want {
		if _, ok := apiIndexDescriptions[pattern]; !ok {
			t.Errorf("the table names %s but the descriptions map holds no sentence for it", pattern)
		}
	}
	for pattern := range apiIndexDescriptions {
		if !want[pattern] {
			t.Errorf("the descriptions map names %s, a pattern the table does not", pattern)
		}
	}
}

// TestAPIIndexPathOrderEqualsTheTable is the other half: the index's fixed path enumeration,
// plus the bare /api it does not list and the document it points at, equals the table's key
// set exactly — both directions. A new route that never joins the enumeration fails here even
// if it gained a description.
func TestAPIIndexPathOrderEqualsTheTable(t *testing.T) {
	s := newTestServer(t, "test-key")
	_, table := s.newAPIRoutes()

	enumerated := map[string]bool{"/api": true, "/openapi.json": true}
	for _, path := range apiIndexPathOrder {
		enumerated[path] = true
	}

	if len(enumerated) != len(table) {
		t.Fatalf("the enumeration plus /api and /openapi.json names %d paths, the table registers %d", len(enumerated), len(table))
	}
	for path := range table {
		if !enumerated[path] {
			t.Errorf("the table registers %s, which the index enumeration never lists", path)
		}
	}
	for path := range enumerated {
		if _, ok := table[path]; !ok {
			t.Errorf("the enumeration lists %s, a path the table does not register", path)
		}
	}
}
