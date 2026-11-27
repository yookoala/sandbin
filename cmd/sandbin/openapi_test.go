package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The contract pins of docs/decisions/0030-api-servemux-openapi.md: kin-openapi — a
// test-only dependency — reads the committed document and holds the server to it. The
// document and the dispatch can never drift silently, and the live JSON answers must pass
// the document's own schemas, not a hand-written mirror of them.

// openapiSpec loads the committed cmd/sandbin/openapi.json (tests run in the package dir).
func openapiSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("openapi.json")
	if err != nil {
		t.Fatalf("load the committed openapi.json: %v", err)
	}
	return doc
}

// openapiOperation finds one published operation, failing the test when the document names
// no such path or method.
func openapiOperation(t *testing.T, doc *openapi3.T, path, method string) *openapi3.Operation {
	t.Helper()
	item := doc.Paths.Value(path)
	if item == nil {
		t.Fatalf("the document names no path %s", path)
	}
	var op *openapi3.Operation
	switch method {
	case http.MethodGet:
		op = item.Get
	case http.MethodHead:
		op = item.Head
	case http.MethodPost:
		op = item.Post
	case http.MethodPatch:
		op = item.Patch
	case http.MethodDelete:
		op = item.Delete
	default:
		t.Fatalf("no test reads a %s operation", method)
	}
	if op == nil {
		t.Fatalf("the document names no %s operation for %s", method, path)
	}
	return op
}

// operationSecurity reads an operation's security line; a missing line means no
// requirement, since the document carries no document-level security to inherit.
func operationSecurity(t *testing.T, op *openapi3.Operation) openapi3.SecurityRequirements {
	t.Helper()
	if op.Security == nil {
		return openapi3.SecurityRequirements{}
	}
	return *op.Security
}

// responseJSONSchema reads the application/json schema an operation's pinned success status
// answers with.
func responseJSONSchema(t *testing.T, doc *openapi3.T, path, method, status string) *openapi3.Schema {
	t.Helper()
	op := openapiOperation(t, doc, path, method)
	resp := op.Responses.Value(status)
	if resp == nil || resp.Value == nil {
		t.Fatalf("the document names no %s response for %s %s", status, method, path)
	}
	media, ok := resp.Value.Content["application/json"]
	if !ok || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("the %s %s %s response carries no application/json schema", method, path, status)
	}
	return media.Schema.Value
}

// componentSchema reads one named components schema — the document's own spelling of a body
// type, apispec-generated or pinned by openapi-fix.jq.
func componentSchema(t *testing.T, doc *openapi3.T, name string) *openapi3.Schema {
	t.Helper()
	ref, ok := doc.Components.Schemas[name]
	if !ok || ref == nil || ref.Value == nil {
		t.Fatalf("the document names no components schema %s", name)
	}
	return ref.Value
}

// validateBodyAgainst is the live half of the honesty pin: a body the real server answered
// must pass the committed document's own schema. And the pin must check something: a body
// that is plainly not the promised object must fail the same schema, so a vacuous schema
// (an empty one, say) can never pass for an answer.
func validateBodyAgainst(t *testing.T, what string, schema *openapi3.Schema, body []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("%s body %q is not JSON: %v", what, body, err)
	}
	if err := schema.VisitJSON(value, openapi3.EnableJSONSchema2020()); err != nil {
		t.Errorf("%s body fails the document's schema: %v", what, err)
	}
	if err := schema.VisitJSON("plainly not the promised object", openapi3.EnableJSONSchema2020()); err == nil {
		t.Errorf("%s schema accepts a bare string where the body is an object; it checks nothing", what)
	}
}

// TestOpenAPIDocValidatesAsOpenAPI31 is the document's own gate: kin-openapi parses the
// committed file and its Validate finds nothing — the file the server serves is a
// conformant OpenAPI 3.1 document, not just plausible-looking JSON.
func TestOpenAPIDocValidatesAsOpenAPI31(t *testing.T) {
	doc := openapiSpec(t)
	if !strings.HasPrefix(doc.OpenAPI, "3.1.") {
		t.Errorf("openapi version = %q, want 3.1.x", doc.OpenAPI)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("the committed document fails kin-openapi validation: %v", err)
	}
}

// TestOpenAPISpecMatchesRouteTable is the cross-check both directions: every path×method the
// document names is in the authoritative route table, every table entry is in the document,
// and the two sets are equal — nothing extra, nothing missing. The document is generated
// from the same registrations the table records, and this test is what makes that a pin
// instead of a habit.
func TestOpenAPISpecMatchesRouteTable(t *testing.T) {
	s := newTestServer(t, "test-key")
	_, table := s.newAPIRoutes()
	doc := openapiSpec(t)

	spec := map[string][]string{}
	for path, item := range doc.Paths.Map() {
		methods := make([]string, 0, 8)
		for method := range item.Operations() {
			methods = append(methods, method)
		}
		sortAPIMethods(methods)
		spec[path] = methods
	}

	if len(spec) != len(table) {
		t.Fatalf("the document names %d paths, the route table %d: document %v, table %v", len(spec), len(table), spec, table)
	}
	for path, wantMethods := range table {
		got, ok := spec[path]
		if !ok {
			t.Errorf("the route table registers %s but the document names no such path", path)
			continue
		}
		if !slices.Equal(got, wantMethods) {
			t.Errorf("document methods for %s = %v, route table = %v; want the same methods", path, got, wantMethods)
		}
	}
	for path := range spec {
		if _, ok := table[path]; !ok {
			t.Errorf("the document names path %s but the route table registers it not", path)
		}
	}
}

// TestOpenAPISecurityMatchesWrapper pins the document's security story to the code's: the
// five withAPIKey-wrapped operations carry exactly the apiKey requirement, the five key-free
// operations (the index GET+HEAD, health GET+HEAD and the document itself) carry none,
// inspect carries the optional-Basic pair the bin gate really is, and the two schemes are
// spelled exactly as the decision doc pins them.
func TestOpenAPISecurityMatchesWrapper(t *testing.T) {
	doc := openapiSpec(t)

	keyChecked := []struct{ path, method string }{
		{"/api/key", http.MethodPost},
		{"/api/bins", http.MethodGet},
		{"/api/bins", http.MethodPost},
		{"/api/bins/{hash}", http.MethodPatch},
		{"/api/bins/{hash}", http.MethodDelete},
	}
	for _, op := range keyChecked {
		security := operationSecurity(t, openapiOperation(t, doc, op.path, op.method))
		if len(security) != 1 {
			t.Errorf("%s %s security = %v, want exactly one apiKey requirement", op.method, op.path, security)
			continue
		}
		if len(security[0]) != 1 {
			t.Errorf("%s %s requirement = %v, want it to name apiKey alone", op.method, op.path, security[0])
			continue
		}
		if scopes, ok := security[0]["apiKey"]; !ok || len(scopes) != 0 {
			t.Errorf("%s %s requirement = %v, want apiKey with no scopes", op.method, op.path, security[0])
		}
	}

	keyFree := []struct{ path, method string }{
		{"/api", http.MethodGet},
		{"/api", http.MethodHead},
		{"/api/health", http.MethodGet},
		{"/api/health", http.MethodHead},
		{"/openapi.json", http.MethodGet},
	}
	for _, op := range keyFree {
		security := operationSecurity(t, openapiOperation(t, doc, op.path, op.method))
		if len(security) != 0 {
			t.Errorf("%s %s security = %v, want none", op.method, op.path, security)
		}
	}

	inspect := operationSecurity(t, openapiOperation(t, doc, "/api/bins/{hash}", http.MethodGet))
	if len(inspect) != 2 {
		t.Fatalf("GET /api/bins/{hash} security = %v, want the optional-Basic pair [{}, {binBasic: []}]", inspect)
	}
	if len(inspect[0]) != 0 {
		t.Errorf("inspect's first requirement = %v, want the empty one (the bin may be open)", inspect[0])
	}
	if scopes, ok := inspect[1]["binBasic"]; !ok || len(inspect[1]) != 1 || len(scopes) != 0 {
		t.Errorf("inspect's second requirement = %v, want binBasic with no scopes", inspect[1])
	}

	schemes := doc.Components.SecuritySchemes
	if len(schemes) != 2 {
		t.Fatalf("components securitySchemes name %d schemes, want exactly apiKey and binBasic", len(schemes))
	}
	apiKey := schemes["apiKey"]
	if apiKey == nil || apiKey.Value == nil {
		t.Fatalf("components name no apiKey scheme")
	}
	if apiKey.Value.Type != "apiKey" || apiKey.Value.In != "header" || apiKey.Value.Name != "X-API-Key" {
		t.Errorf("apiKey scheme = type %q in %q name %q, want apiKey/header/X-API-Key", apiKey.Value.Type, apiKey.Value.In, apiKey.Value.Name)
	}
	binBasic := schemes["binBasic"]
	if binBasic == nil || binBasic.Value == nil {
		t.Fatalf("components name no binBasic scheme")
	}
	if binBasic.Value.Type != "http" || binBasic.Value.Scheme != "basic" {
		t.Errorf("binBasic scheme = type %q scheme %q, want http/basic", binBasic.Value.Type, binBasic.Value.Scheme)
	}
}

// TestOpenAPIServedBytesMatchTheCommittedFile is the golden pin: GET /openapi.json answers
// the embedded copy of the committed file byte for byte, with the JSON content type, no key
// asked for anywhere.
func TestOpenAPIServedBytesMatchTheCommittedFile(t *testing.T) {
	s := newTestServer(t, "test-key")
	want, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("read the committed openapi.json: %v", err)
	}

	rec := get(t, s, "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("served %d bytes, the committed file holds %d; the served document is not the committed one", rec.Body.Len(), len(want))
	}
}

// TestOpenAPIServeShapes pins the endpoint's shapes: the route table names GET only, so HEAD
// and POST are the envelope 405 the table gives every named path on a method it does not list,
// and every spelling the table does not name — the trailing slash, a near-miss name — is the
// plain 404 unknown bin.
func TestOpenAPIServeShapes(t *testing.T) {
	s := newTestServer(t, "test-key")

	req := httptest.NewRequest(http.MethodHead, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != apiEnvelope(405, "method not allowed") {
		t.Errorf("HEAD /openapi.json = %d, body %q; want 405 with the method not allowed envelope", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/openapi.json", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != apiEnvelope(405, "method not allowed") {
		t.Errorf("POST /openapi.json = %d, body %q; want 405 with the method not allowed envelope", rec.Code, rec.Body.String())
	}

	rec = get(t, s, "/openapi.json/")
	if rec.Code != http.StatusNotFound || rec.Body.String() != "unknown bin\n" {
		t.Errorf("GET /openapi.json/ = %d, body %q; want 404 unknown bin", rec.Code, rec.Body.String())
	}

	rec = get(t, s, "/openapix.json")
	if rec.Code != http.StatusNotFound || rec.Body.String() != "unknown bin\n" {
		t.Errorf("GET /openapix.json = %d, body %q; want 404 unknown bin", rec.Code, rec.Body.String())
	}
}

// TestOpenAPINeverCharges pins the document route outside the charging machinery: no key and
// a wrong key answer it alike, RecordFailure never fires for it — proven because the same
// wrong key still gets its first 403 from a write after all those document reads — and even
// an address in the block still gets the document.
func TestOpenAPINeverCharges(t *testing.T) {
	s := newTestServer(t, "test-key")
	const addr = "198.51.100.44:1234"

	rec := getFrom(t, s, addr, "/openapi.json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json without a key = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	for range 5 {
		rec = getFrom(t, s, addr, "/openapi.json", "wrong-key")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /openapi.json with a wrong key = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.44"); ok || failures != 0 {
		t.Fatalf("six document reads charged 198.51.100.44 = failures %d, row present %v; want no charge at all", failures, ok)
	}

	// The same wrong key now meets a write and gets its first 403. Had any document read
	// charged, this would already be a 429 behind a block.
	rec = postFrom(t, s, addr, "wrong-key")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the write with the same wrong key = %d, want %d; body %q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if failures, ok := ipBlockRow(t, s.store, "198.51.100.44"); !ok || failures != 1 {
		t.Fatalf("after the refused write the row holds failures %d, present %v; want the write to be the only charge", failures, ok)
	}

	// One failure blocks the address (the 0029 rule). The document answers anyway.
	rec = postFrom(t, s, addr, "test-key")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the blocked write = %d, want %d; the address is not blocked, so this pin proves nothing", rec.Code, http.StatusTooManyRequests)
	}
	for _, key := range []string{"", "wrong-key", "test-key"} {
		rec = getFrom(t, s, addr, "/openapi.json", key)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /openapi.json while blocked (key %q) = %d, want %d; the document must answer even while the address is blocked", key, rec.Code, http.StatusOK)
		}
	}
	if failures, _ := ipBlockRow(t, s.store, "198.51.100.44"); failures != 2 {
		t.Errorf("document reads while blocked left failures %d, want the blocked write's 2 alone; a document read charged", failures)
	}
}

// TestOpenAPIBodySchemasMatchLiveAnswers validates bodies the real server answered against
// the committed document's schemas — create, inspect, list, the single-file PUT's stored row
// (fileJSON, the document's own spelling of every listed file), the archive PUT's counts (the
// pinned archiveAnswer), the GET /api index (the named apiIndexDoc component, pinned as the
// closed DTO the handler marshals) and the two ex-204 successes (both named okBody) — through
// kin-openapi's own schema validation.
func TestOpenAPIBodySchemasMatchLiveAnswers(t *testing.T) {
	s := newTestServer(t, "test-key")
	doc := openapiSpec(t)

	rec := postAPIBins(t, s, "", "test-key", createBodyJSON(t, "Contract pin", "the live body matches the document", ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/bins = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	validateBodyAgainst(t, "create 201", responseJSONSchema(t, doc, "/api/bins", http.MethodPost, "201"), rec.Body.Bytes())
	created := createdBin(t, rec)

	rec = get(t, s, "/api/bins/"+created.Bin)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins/%s = %d, want %d; body %q", created.Bin, rec.Code, http.StatusOK, rec.Body.String())
	}
	validateBodyAgainst(t, "inspect 200", responseJSONSchema(t, doc, "/api/bins/{hash}", http.MethodGet, "200"), rec.Body.Bytes())

	rec = listFrom(t, s, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/bins = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	validateBodyAgainst(t, "list 200", responseJSONSchema(t, doc, "/api/bins", http.MethodGet, "200"), rec.Body.Bytes())

	rec = put(t, s, created.URL+"notes.md", "test-key", "# Notes")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT notes.md = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	validateBodyAgainst(t, "single-file PUT 201", componentSchema(t, doc, "fileJSON"), rec.Body.Bytes())

	zip := makeZip(t, zipEntry{name: "one.txt", body: "one"}, zipEntry{name: "two.txt", body: "two"})
	rec = put(t, s, created.URL+"bundle.zip", "test-key", string(zip))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT bundle.zip = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	validateBodyAgainst(t, "archive PUT 201", componentSchema(t, doc, "archiveAnswer"), rec.Body.Bytes())

	// The index itself: a live GET /api — no key asked, the index charges nothing — must pass
	// the document's own spelling of the index body. That spelling is the named component
	// apiIndexDoc the 200 references: a placeholder object would accept any object-shaped body
	// and pin nothing. And the component is the closed DTO the handler marshals — every field
	// required (its json tags carry no omitempty), exactly the properties the DTO has, and
	// additionalProperties: false at every object level, because the DTOs are closed.
	rec = get(t, s, "/api")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	indexDoc := componentSchema(t, doc, "apiIndexDoc")
	if responseJSONSchema(t, doc, "/api", http.MethodGet, "200") != indexDoc {
		t.Errorf("the GET /api 200 schema is not the document's named component apiIndexDoc; the response must reference it")
	}
	validateBodyAgainst(t, "index 200", indexDoc, rec.Body.Bytes())

	// The HEAD side of the same endpoint: its 200 in the committed document carries no content
	// map. The HEAD 200 must answer headers only, and filter correction 16 keeps it that way —
	// this pin notices if a generated body schema ever lands back on it.
	headResp := openapiOperation(t, doc, "/api", http.MethodHead).Responses.Value("200")
	if headResp == nil || headResp.Value == nil {
		t.Fatalf("the document names no 200 response for HEAD /api")
	}
	if len(headResp.Value.Content) != 0 {
		t.Errorf("the document's HEAD /api 200 carries a content map of %d media types; the HEAD 200 must answer headers only — filter correction 16 keeps it that way", len(headResp.Value.Content))
	}

	// The three nested DTO levels, read through the component's own references — named
	// components or resolved inline spellings, either way the document's own schemas.
	endpoints := indexDoc.Properties["endpoints"]
	if endpoints == nil || endpoints.Value == nil || endpoints.Value.Items == nil || endpoints.Value.Items.Value == nil {
		t.Fatalf("apiIndexDoc spells endpoints as no array of item objects")
	}
	methods := endpoints.Value.Items.Value.Properties["methods"]
	if methods == nil || methods.Value == nil || methods.Value.Items == nil || methods.Value.Items.Value == nil {
		t.Fatalf("apiIndexEndpoint spells methods as no array of item objects")
	}
	link := indexDoc.Properties["openapi"]
	if link == nil || link.Value == nil {
		t.Fatalf("apiIndexDoc names no openapi object")
	}
	for _, level := range []struct {
		what       string
		schema     *openapi3.Schema
		properties []string
	}{
		{"apiIndexDoc", indexDoc, []string{"endpoints", "openapi", "status", "version"}},
		{"apiIndexEndpoint", endpoints.Value.Items.Value, []string{"methods", "url"}},
		{"apiIndexMethod", methods.Value.Items.Value, []string{"description", "method"}},
		{"apiIndexLink", link.Value, []string{"description", "url"}},
	} {
		gotProps := slices.Sorted(maps.Keys(level.schema.Properties))
		if !slices.Equal(gotProps, level.properties) {
			t.Errorf("properties of %s = %v, want %v: the handler marshals exactly those and nothing else", level.what, gotProps, level.properties)
		}
		gotRequired := slices.Sorted(slices.Values(level.schema.Required))
		if !slices.Equal(gotRequired, level.properties) {
			t.Errorf("required of %s = %v, want every property it has: no json tag of the DTO carries omitempty", level.what, gotRequired)
		}
		if ap := level.schema.AdditionalProperties; ap.Has == nil || *ap.Has || ap.Schema != nil {
			t.Errorf("%s is not additionalProperties: false; the marshaled DTO is closed", level.what)
		}
	}

	// The two successes the 204s retired: POST /api/key and DELETE /api/bins/{hash} answer 200
	// with the okBody body, and both the document's 200 responses and a live answer must speak
	// of that one named component — the same reference-pin shape the index 200 holds.
	rec = postAPIKeyFrom(t, s, "198.51.100.99:1234", "test-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/key = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	okBody := componentSchema(t, doc, "okBody")
	if responseJSONSchema(t, doc, "/api/key", http.MethodPost, "200") != okBody {
		t.Errorf("the POST /api/key 200 schema is not the document's named component okBody; the response must reference it")
	}
	validateBodyAgainst(t, "key check 200", okBody, rec.Body.Bytes())

	rec = deleteBinFrom(t, s, "198.51.100.99:1234", "/api/bins/"+created.Bin, "test-key", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/bins/%s = %d, want %d; body %q", created.Bin, rec.Code, http.StatusOK, rec.Body.String())
	}
	if responseJSONSchema(t, doc, "/api/bins/{hash}", http.MethodDelete, "200") != okBody {
		t.Errorf("the DELETE /api/bins/{hash} 200 schema is not the document's named component okBody; the response must reference it")
	}
	validateBodyAgainst(t, "delete bin 200", okBody, rec.Body.Bytes())
}

// TestKinOpenapiStaysTestOnly guards the dependency rule: kin-openapi enters the module to
// hold the server to its contract, never to ship inside it. No production source file of
// cmd/sandbin may import it.
func TestKinOpenapiStaysTestOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/sandbin: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err == nil && strings.HasPrefix(path, "github.com/getkin/kin-openapi") {
				t.Errorf("%s imports %s; kin-openapi may only enter through _test files", name, path)
			}
		}
	}
}
