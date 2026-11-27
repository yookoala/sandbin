package main

import (
	"bytes"
	"cmp"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// maxJSONBodyBytes caps every JSON body on /api, which is a much smaller shape than the
// 1 MiB a file PUT may carry.
const maxJSONBodyBytes = 64 << 10

// apiRoutes dispatches the whole /api surface — the API index on the bare /api, plus the GET
// /openapi.json document, whose path joins the table below — on an http.ServeMux, classified
// before it is delegated. "api" is no eight-hex hash and "openapi.json" holds dots, so neither
// can name or shadow a bin, same reasoning as the static routes below it. The mux and its
// authoritative route table build lazily inside apiOnce on the first such request, because every
// server is a composite literal. Dispatch names a request through the table and only then lets
// the mux answer it: a path the table does not name — trailing-slash variants and non-hash
// segments under /api/bins/ included — is the 404 envelope API endpoint not found (the bare
// /api is named now: it answers the API index), and a named path on a method its set does not
// allow — every method but GET and HEAD on the bare /api, HEAD on the per-bin spelling, HEAD
// and DELETE on the collection, every method but GET on the document — is the 405 envelope
// method not allowed. Nothing the table does not name reaches the mux, so the mux's built-in
// 404, 405 and 301 never answer.
func (s *server) apiRoutes(w http.ResponseWriter, r *http.Request) {
	s.apiOnce.Do(func() {
		s.apiMux, s.apiMethods = s.newAPIRoutes()
	})

	allowed := apiLookupMethodSet(s.apiMethods, r.URL.Path)
	if allowed == nil {
		s.writeAPIError(w, http.StatusNotFound, "API endpoint not found")
		return
	}
	if !slices.Contains(allowed, r.Method) {
		s.writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.apiMux.ServeHTTP(w, r)
}

// newAPIRoutes builds the /api mux and the authoritative route table (path → allowed
// methods) together, so the table is exactly what the mux was given. The index on the bare /api
// and /api/health each register their GET and HEAD spellings separately so the table derives
// HEAD from registration, not from the mux quirk that a GET method pattern also matches HEAD.
// Each path's method list is sorted, which makes the table deterministic whatever the
// registration order.
//
// The registrations are literal calls carrying their pattern constants — one const per
// published pattern, which the mux and the table read as one source. Static route readers
// like the OpenAPI generator (docs/decisions/0030-api-servemux-openapi.md) read the method,
// path and security wrapper off these call sites; a registration helper that passed the
// pattern as a parameter hides the wrapper from that analysis.
func (s *server) newAPIRoutes() (*http.ServeMux, map[string][]string) {
	mux := http.NewServeMux()
	methods := map[string][]string{}

	mux.Handle(apiPatternIndexGet, http.HandlerFunc(s.apiIndex))
	mux.Handle(apiPatternIndexHead, http.HandlerFunc(s.apiIndex))
	mux.Handle(apiPatternKey, s.withAPIKey(http.HandlerFunc(s.apiKeyCheck)))
	mux.Handle(apiPatternHealthGet, http.HandlerFunc(s.apiHealth))
	mux.Handle(apiPatternHealthHead, http.HandlerFunc(s.apiHealth))
	mux.Handle(apiPatternBinsPost, s.withAPIKey(http.HandlerFunc(s.apiCreateBin)))
	mux.Handle(apiPatternBinsGet, s.withAPIKey(http.HandlerFunc(s.apiListBins)))
	mux.Handle(apiPatternBinGet, http.HandlerFunc(s.apiInspectBin))
	mux.Handle(apiPatternBinPatch, s.withAPIKey(http.HandlerFunc(s.apiPatchBin)))
	mux.Handle(apiPatternBinDelete, s.withAPIKey(http.HandlerFunc(s.apiDeleteBin)))
	mux.Handle(apiPatternOpenAPI, http.HandlerFunc(s.serveOpenAPI))

	for _, pattern := range []string{
		apiPatternIndexGet, apiPatternIndexHead,
		apiPatternKey, apiPatternHealthGet, apiPatternHealthHead,
		apiPatternBinsPost, apiPatternBinsGet,
		apiPatternBinGet, apiPatternBinPatch, apiPatternBinDelete,
		apiPatternOpenAPI,
	} {
		apiRecordMethod(methods, pattern)
	}

	for _, allowed := range methods {
		sortAPIMethods(allowed)
	}
	return mux, methods
}

// The eleven published patterns — the ten /api routes and the OpenAPI document endpoint —
// each spelled once. The mux gets these exact strings and the table records method and path
// cut from the same const, so the two share one source and can never drift.
const (
	apiPatternIndexGet   = "GET /api"
	apiPatternIndexHead  = "HEAD /api"
	apiPatternKey        = "POST /api/key"
	apiPatternHealthGet  = "GET /api/health"
	apiPatternHealthHead = "HEAD /api/health"
	apiPatternBinsPost   = "POST /api/bins"
	apiPatternBinsGet    = "GET /api/bins"
	apiPatternBinGet     = "GET /api/bins/{hash}"
	apiPatternBinPatch   = "PATCH /api/bins/{hash}"
	apiPatternBinDelete  = "DELETE /api/bins/{hash}"
	apiPatternOpenAPI    = "GET /openapi.json"
)

// apiMethodOrder is the canonical order a path's allowed methods are recorded in: the order
// the HTTP surface is published in — GET, HEAD, POST, PUT, PATCH, DELETE — and any other
// method ranks after them, ordered by name. Sorting the table's method lists this way makes
// them deterministic whatever the registration order.
var apiMethodOrder = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

func sortAPIMethods(allowed []string) {
	slices.SortFunc(allowed, func(a, b string) int {
		rankA, rankB := apiMethodRank(a), apiMethodRank(b)
		if rankA != rankB {
			return cmp.Compare(rankA, rankB)
		}
		return strings.Compare(a, b)
	})
}

func apiMethodRank(method string) int {
	if i := slices.Index(apiMethodOrder, method); i >= 0 {
		return i
	}
	return len(apiMethodOrder)
}

// apiRecordMethod is the table half of a route's birth: it records the pattern's method
// under the pattern's path. The mux half is newAPIRoutes' literal Handle call; both read
// the same pattern const, so the table is always exactly what the mux was given and the
// two can never drift.
func apiRecordMethod(methods map[string][]string, pattern string) {
	method, path, _ := strings.Cut(pattern, " ")
	methods[path] = append(methods[path], method)
}

// apiLookupMethodSet finds the table entry that names a concrete path: a literal hit, but
// only for table keys that name a concrete spelling — a key holding a {wildcard} is reached
// only through the hex gate, so the literal brace spelling /api/bins/{hash}, raw or
// percent-encoded, names no entry either. Else the per-bin entry registered as
// /api/bins/{hash} when apiBinPathRE matches. That hex gate is what keeps {hash} from
// broadening the matching: /api/bins/xyz and /api/bins/deadbeef/ name no entry and stay the
// plain 404, uncharged whatever the key.
func apiLookupMethodSet(methods map[string][]string, path string) []string {
	if allowed, ok := methods[path]; ok && !strings.Contains(path, "{") {
		return allowed
	}
	if apiBinPathRE.MatchString(path) {
		return methods["/api/bins/{hash}"]
	}
	return nil
}

// withAPIKey is the wrapper every key-checked endpoint registers through: it runs today's
// requireKey unchanged before the handler, so each route's pinned order — key before body,
// before lookup, before the bin gate — is preserved by construction. Its shape is what the
// OpenAPI document's securityMappings follow to attach the apiKey requirement.
func (s *server) withAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireKey(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiBinPathRE names the one per-bin spelling /api owns: /api/bins/{hash} with the store's own
// eight-hex hash and nothing after it. The trailing-slash spelling and any segment that is not
// a hash stay the plain 404 they always were, whatever the method.
var apiBinPathRE = regexp.MustCompile(`^/api/bins/([0-9a-f]{8})$`)

// apiKeyCheck answers the key check. It registers through the withAPIKey wrapper, whose
// guard is requireKey's verbatim, so a wrong key through it charges exactly like a wrong key
// through a write, and a match creates nothing, charges nothing and answers 204 with an
// empty body.
func (s *server) apiKeyCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// healthBody is the /api/health answer.
type healthBody struct {
	Status string `json:"status"`
}

// apiHealth is the container probe's route: it says nothing about bins, needs no key and
// charges nothing, so it answers even while its address is blocked.
func (s *server) apiHealth(w http.ResponseWriter, r *http.Request) {
	if err := writeJSON(w, http.StatusOK, healthBody{Status: "ok"}); err != nil {
		log.Printf("encode the health answer: %v", err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not answer health")
	}
}

// writeJSON answers status with v as JSON plus a trailing newline, the shape every JSON
// success on /api carries. Refusals on the API dispatcher's surface answer the JSON envelope
// through writeAPIError; http.Error stays outside it.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	out, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(append(out, '\n'))
	return err
}

// apiErrorBody is the envelope every refusal on the API dispatcher's surface answers: status
// says which half of the contract this body is, code repeats the transport status so the body
// alone is self-explanatory, message is the human sentence.
type apiErrorBody struct {
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// writeAPIError answers status with the error envelope plus a trailing newline, the one shape
// every refusal on the API dispatcher's surface carries. code is always the status just
// written, so the two can never disagree.
func (s *server) writeAPIError(w http.ResponseWriter, status int, message string) {
	out, _ := json.Marshal(apiErrorBody{Status: "error", Code: status, Message: message})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(out, '\n'))
}

// binRequest is the JSON body a bin write carries. Every field stays raw so an absent one
// remains distinguishable from one given as empty (fieldValue), and DisallowUnknownFields means
// a name this API does not have fails the decode instead of being ignored.
type binRequest struct {
	Title       json.RawMessage `json:"title"`
	Description json.RawMessage `json:"description"`
	Password    json.RawMessage `json:"password"`
}

// fieldValue reads one raw field: absent (nil) is ("", false); an explicit null or "" is
// ("", true), which is "given as empty"; a JSON string is its value. Create treats !given and
// given-empty alike as empty; a patch tells them apart, leave versus clear.
func fieldValue(raw json.RawMessage) (value string, given bool) {
	if raw == nil {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", true
	}
	return s, true
}

// fieldIsJSONText reports whether a raw field carries a value this API reads: nothing at all,
// null, or a JSON string. A number, an object or an array is a malformed request, never a value
// to quietly treat as empty — a mistyped password must not make an open bin.
func fieldIsJSONText(raw json.RawMessage) bool {
	if raw == nil {
		return true
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// decodeBinRequest reads the capped JSON body of a bin write. bodyOptional says what an empty
// body means: create takes no body as the same request as {} — which is how the decoder
// reports it too, an empty stream is EOF — while a patch requires a body and refuses an empty
// one as unreadable. The body is read whole first, because a decoder aimed straight at the
// struct takes null — which is no object — as an empty request, and takes a second value after
// the object as noise it may ignore. A field name this API does not have is the loud refusal,
// and so is anything that is not one JSON object, or a field that is not null or a string.
// ok says whether the handler may go on; when it is false the refusal is already written, in
// the error envelope like every other refusal the API dispatcher answers.
func decodeBinRequest(s *server, w http.ResponseWriter, r *http.Request, bodyOptional bool) (binRequest, bool) {
	var req binRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	dec := json.NewDecoder(r.Body)
	var body json.RawMessage
	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) && bodyOptional {
			return binRequest{}, true
		}
		s.writeAPIError(w, http.StatusBadRequest, "could not read the request")
		return binRequest{}, false
	}
	if len(body) == 0 || body[0] != '{' {
		s.writeAPIError(w, http.StatusBadRequest, "could not read the request")
		return binRequest{}, false
	}
	// The strict end of the stream: a second decode must run into io.EOF, so whitespace
	// (even a trailing newline) passes while any further value is refused. dec.More()
	// alone is not enough — it reports false on a stray ']' or '}' as if they closed an
	// enclosing container this body never had.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		s.writeAPIError(w, http.StatusBadRequest, "could not read the request")
		return binRequest{}, false
	}

	fields := json.NewDecoder(bytes.NewReader(body))
	fields.DisallowUnknownFields()
	if err := fields.Decode(&req); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			s.writeAPIError(w, http.StatusBadRequest, "unrecognized field in the request")
			return binRequest{}, false
		}
		s.writeAPIError(w, http.StatusBadRequest, "could not read the request")
		return binRequest{}, false
	}
	for _, raw := range []json.RawMessage{req.Title, req.Description, req.Password} {
		if !fieldIsJSONText(raw) {
			s.writeAPIError(w, http.StatusBadRequest, "could not read the request")
			return binRequest{}, false
		}
	}
	return req, true
}

// binJSON is the bin object every JSON answer about a bin carries: its hash, the path it lives
// under, its metadata, whether a password guards it, and when the store made it. Neither the
// password nor its hash is ever in it.
type binJSON struct {
	Bin         string `json:"bin"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	HasPassword bool   `json:"has_password"`
	CreatedAt   string `json:"created_at"`
}

// binToJSON spells one bin row as that object.
func binToJSON(b Bin) binJSON {
	return binJSON{
		Bin:         b.Hash,
		URL:         "/" + b.Hash + "/",
		Title:       b.Title,
		Description: b.Description,
		HasPassword: b.PasswordHash != "",
		CreatedAt:   b.CreatedAt,
	}
}

// apiCreateBin makes a bin from the JSON body and answers 201 with the bin object and its
// Location. Its guard is the withAPIKey wrapper around it — requireKey unchanged — so a wrong
// key through it charges exactly as the retired POST / did. An absent field and one given as
// empty both make an empty value, which is what a bin with nothing to say holds; a password
// longer than bcrypt can hold is refused before anything is written.
func (s *server) apiCreateBin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBinRequest(s, w, r, true)
	if !ok {
		return
	}

	// An absent field and one given as empty both make the empty value a bin with nothing to
	// say holds.
	title, _ := fieldValue(req.Title)
	description, _ := fieldValue(req.Description)
	if err := checkBinText("title", title, maxTitleBytes); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := checkBinText("description", description, maxDescriptionBytes); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// An empty password, given or not, leaves the bin open.
	password, _ := fieldValue(req.Password)
	if len(password) > 72 {
		s.writeAPIError(w, http.StatusBadRequest, "bin password too long")
		return
	}

	passwordHash := ""
	if password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("hash bin password: %v", err)
			s.writeAPIError(w, http.StatusInternalServerError, "could not set the bin password")
			return
		}
		passwordHash = string(hashed)
	}

	hash, err := s.store.CreateBin(passwordHash, title, description)
	if err != nil {
		log.Printf("create bin: %v", err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not create bin")
		return
	}

	// created_at is the store's own, so the answer reads the row back instead of guessing it.
	bin, ok, err := s.store.GetBin(hash)
	if err != nil || !ok {
		log.Printf("get bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not create bin")
		return
	}

	w.Header().Set("Location", "/api/bins/"+hash)
	created := binToJSON(bin)
	if err := writeJSON(w, http.StatusCreated, created); err != nil {
		log.Printf("encode the answer of bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not answer the bin")
	}
}

// The paging limits GET /api/bins serves: a page nobody sizes holds 30 bins, and no request
// gets more than 100, however big its limit is.
const (
	defaultListLimit = 30
	maxListLimit     = 100
)

// binListJSON is the GET /api/bins answer: the page of bin objects and, only when the store's
// probe found a further row, the cursor that resumes the walk after the page's last one.
type binListJSON struct {
	Bins       []binJSON `json:"bins"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// cursorSeparator splits a cursor's created_at from its hash. It is the unit separator, a
// character neither half can carry: created_at is RFC 3339 and a hash is eight hex.
const cursorSeparator = "\x1f"

// encodeCursor spells the keyset position after a page — the (created_at, hash) of its last
// row — as unpadded URL-safe base64, so a cursor never needs URL escaping.
func encodeCursor(createdAt, hash string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + cursorSeparator + hash))
}

// decodeCursor reads a cursor back into the cutoff ListBins resumes from. What is not unpadded
// URL-safe base64 holding a created_at, the separator, and a hash is no cursor this API made.
func decodeCursor(raw string) (BinCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return BinCursor{}, errors.New("cursor is not unpadded URL-safe base64")
	}
	// strings.Cut splits at the first separator, so the hash half is where a second one
	// could hide — and neither half may carry one.
	createdAt, hash, found := strings.Cut(string(decoded), cursorSeparator)
	if !found || createdAt == "" || hash == "" || strings.Contains(hash, cursorSeparator) {
		return BinCursor{}, errors.New("cursor names no created_at and hash")
	}
	return BinCursor{CreatedAt: createdAt, Hash: hash}, nil
}

// apiListBins answers GET /api/bins, the key-gated listing: its guard is the withAPIKey
// wrapper around it — requireKey unchanged — so a wrong key through it charges exactly like a
// wrong key through a write. The paging rules are pinned: limit default 30, a non-integer or a
// limit below 1 is the plain 400 invalid limit, anything above 100 clamps to 100; cursor is the
// unpadded URL-safe base64 a previous page answered, and a malformed one is the plain 400
// invalid cursor. Bins list complete — protected ones with their metadata and has_password in
// plain view, the owner's view.
func (s *server) apiListBins(w http.ResponseWriter, r *http.Request) {
	limit := defaultListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			s.writeAPIError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > maxListLimit {
			n = maxListLimit
		}
		limit = n
	}

	var cutoff *BinCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeCursor(raw)
		if err != nil {
			s.writeAPIError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cutoff = &cursor
	}

	bins, hasMore, err := s.store.ListBins(limit, cutoff)
	if err != nil {
		log.Printf("list bins: %v", err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not list bins")
		return
	}

	// The store hands back a nil slice for an empty page; the answer promises an array.
	answer := binListJSON{Bins: make([]binJSON, 0, len(bins))}
	for _, b := range bins {
		answer.Bins = append(answer.Bins, binToJSON(b))
	}
	if hasMore {
		last := bins[len(bins)-1]
		answer.NextCursor = encodeCursor(last.CreatedAt, last.Hash)
	}
	if err := writeJSON(w, http.StatusOK, answer); err != nil {
		log.Printf("encode the bins listing: %v", err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not list bins")
	}
}

// fileJSON spells one file row for the JSON answers that name a bin's contents.
type fileJSON struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
}

// binDetailJSON is the inspect answer: the bin object plus every file the bin holds. The bin's
// own HTML page hides that list whenever an index.html covers the base; the API never does.
type binDetailJSON struct {
	binJSON
	Files []fileJSON `json:"files"`
}

// apiInspectBin answers GET /api/bins/{hash}: no API key, the bin's row and its file list
// sorted by path. The hash it reads from the path is an eight-hex spelling because the table
// only names the {hash} route through the apiBinPathRE hex gate. A password-protected bin
// answers 401 until Basic opens it, and that 401 records no failure — the API-key lockout is
// not the bin's password gate.
func (s *server) apiInspectBin(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	bin, ok, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !ok {
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !s.binAuthorized(w, r, bin, true) {
		return
	}

	files, err := s.store.ListFiles(hash)
	if err != nil {
		log.Printf("list bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not list bin")
		return
	}

	detail := binDetailJSON{
		binJSON: binToJSON(bin),
		Files:   make([]fileJSON, 0, len(files)),
	}
	for _, f := range files {
		detail.Files = append(detail.Files, fileJSON{Path: f.Path, Size: f.Size, UpdatedAt: f.UpdatedAt})
	}
	if err := writeJSON(w, http.StatusOK, detail); err != nil {
		log.Printf("encode the answer of bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not answer the bin")
	}
}

// apiPatchBin merges a JSON body into a bin's row: a given field is set — null and "" clear
// theirs, a password is stored as a bcrypt hash — and an absent one leaves what the bin holds,
// which is what UpdateBinMeta's COALESCE reads from a NULL argument. The order is pinned: the
// wrapper's key, body validation, bin lookup, and only then the bin's own password gate, so a
// protected bin is changed only by a request that carries its current password, and a refused
// request changes nothing. {} is a no-op answering the current row; no body at all is refused
// as unreadable, unlike create's pinned empty request.
func (s *server) apiPatchBin(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")

	req, ok := decodeBinRequest(s, w, r, false)
	if !ok {
		return
	}

	// Fields are validated before the row is read: decoding touches no data, so a bad body is
	// a 400 even where the bin is unknown or guarded.
	title, titleGiven := fieldValue(req.Title)
	if titleGiven {
		if err := checkBinText("title", title, maxTitleBytes); err != nil {
			s.writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	description, descriptionGiven := fieldValue(req.Description)
	if descriptionGiven {
		if err := checkBinText("description", description, maxDescriptionBytes); err != nil {
			s.writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	password, passwordGiven := fieldValue(req.Password)
	if passwordGiven && len(password) > 72 {
		s.writeAPIError(w, http.StatusBadRequest, "bin password too long")
		return
	}

	bin, found, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !found {
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	// Clearing or rotating a protected bin's password goes through the same gate as every
	// other change to it: the current password first, and a 401 that charges nothing.
	if !s.binAuthorized(w, r, bin, true) {
		return
	}

	// A NULL argument is UpdateBinMeta's "leave it"; a Valid one writes the value, and a
	// password given as empty is the empty string that opens the bin.
	var titleArg, descriptionArg, passwordArg sql.NullString
	if titleGiven {
		titleArg = sql.NullString{String: title, Valid: true}
	}
	if descriptionGiven {
		descriptionArg = sql.NullString{String: description, Valid: true}
	}
	if passwordGiven {
		if password == "" {
			passwordArg = sql.NullString{String: "", Valid: true}
		} else {
			hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				log.Printf("hash bin password: %v", err)
				s.writeAPIError(w, http.StatusInternalServerError, "could not set the bin password")
				return
			}
			passwordArg = sql.NullString{String: string(hashed), Valid: true}
		}
	}

	updated, err := s.store.UpdateBinMeta(hash, titleArg, descriptionArg, passwordArg)
	if err != nil {
		log.Printf("update bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not update bin")
		return
	}
	if !updated {
		// The bin went away between the read and the write.
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}

	// The answer carries the row as it now stands, so the caller never has to guess what a
	// leave-and-replace mix left behind.
	bin, found, err = s.store.GetBin(hash)
	if err != nil {
		log.Printf("re-read bin %q after the update: %v", hash, err)
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !found {
		// The bin went away between the update and the re-read.
		log.Printf("re-read bin %q after the update: the bin is gone", hash)
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	patched := binToJSON(bin)
	if err := writeJSON(w, http.StatusOK, patched); err != nil {
		log.Printf("encode the answer of bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not answer the bin")
	}
}

// apiDeleteBin removes a bin and every file in it. The order is pinned: the wrapper's key, bin
// lookup, and only then the bin's own password gate, so a protected bin goes only to a request
// carrying its current password — and a 401 charges nothing while the bin survives. The body is
// not read at all: a DELETE has nothing to say. The answer is 204 with an empty body, because
// there is nothing left to report about a bin that is gone.
func (s *server) apiDeleteBin(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")

	bin, found, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !found {
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	if !s.binAuthorized(w, r, bin, true) {
		return
	}

	deleted, err := s.store.DeleteBin(hash)
	if err != nil {
		log.Printf("delete bin %q: %v", hash, err)
		s.writeAPIError(w, http.StatusInternalServerError, "could not delete bin")
		return
	}
	if !deleted {
		// The bin went away between the read and the delete.
		s.writeAPIError(w, http.StatusNotFound, "bin not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
