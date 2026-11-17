package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// maxJSONBodyBytes caps every JSON body on /api, which is a much smaller shape than the
// 1 MiB a file PUT may carry.
const maxJSONBodyBytes = 64 << 10

// apiRoutes dispatches everything under /api. "api" is no eight-hex hash, so the prefix can
// never name or shadow a bin, same reasoning as the static routes below it. It answers POST
// /api/key, GET|HEAD /api/health, POST|GET /api/bins — the GET is the key-gated paged listing
// — and, on the one spelling apiBinPathRE names, GET and PATCH /api/bins/{hash}. DELETE on
// that path is the plain 405 until Task 6 gives it its answer. A spelling the dispatch does
// not name — trailing-slash variants and non-hash segments under /api/bins/ included —
// is the plain 404 unknown bin, and a known path on a wrong method is the plain 405.
func (s *server) apiRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/key":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiKeyCheck(w, r)
	case "/api/health":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiHealth(w, r)
	case "/api/bins":
		switch r.Method {
		case http.MethodPost:
			s.apiCreateBin(w, r)
		case http.MethodGet:
			s.apiListBins(w, r)
		default:
			// HEAD, and DELETE until Task 6 gives it its answer.
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		if m := apiBinPathRE.FindStringSubmatch(r.URL.Path); m != nil {
			switch r.Method {
			case http.MethodGet:
				s.apiInspectBin(w, r, m[1])
			case http.MethodPatch:
				s.apiPatchBin(w, r, m[1])
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		http.Error(w, "unknown bin", http.StatusNotFound)
	}
}

// apiBinPathRE names the one per-bin spelling /api owns: /api/bins/{hash} with the store's own
// eight-hex hash and nothing after it. The trailing-slash spelling and any segment that is not
// a hash stay the plain 404 they always were, whatever the method.
var apiBinPathRE = regexp.MustCompile(`^/api/bins/([0-9a-f]{8})$`)

// apiKeyCheck answers the key check: its guard is requireKey's verbatim, so a wrong key
// through it charges exactly like a wrong key through a write, and a match creates nothing,
// charges nothing and answers 204 with an empty body.
func (s *server) apiKeyCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireKey(w, r) {
		return
	}
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
		http.Error(w, "could not answer health", http.StatusInternalServerError)
	}
}

// writeJSON answers status with v as JSON plus a trailing newline, the shape every JSON
// success on /api carries. Refusals stay http.Error plain text; only success bodies are JSON.
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
// ok says whether the handler may go on; when it is false the refusal is already written.
func decodeBinRequest(w http.ResponseWriter, r *http.Request, bodyOptional bool) (binRequest, bool) {
	var req binRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	dec := json.NewDecoder(r.Body)
	var body json.RawMessage
	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) && bodyOptional {
			return binRequest{}, true
		}
		http.Error(w, "could not read the request", http.StatusBadRequest)
		return binRequest{}, false
	}
	if len(body) == 0 || body[0] != '{' || dec.More() {
		http.Error(w, "could not read the request", http.StatusBadRequest)
		return binRequest{}, false
	}

	fields := json.NewDecoder(bytes.NewReader(body))
	fields.DisallowUnknownFields()
	if err := fields.Decode(&req); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			http.Error(w, "unrecognized field in the request", http.StatusBadRequest)
			return binRequest{}, false
		}
		http.Error(w, "could not read the request", http.StatusBadRequest)
		return binRequest{}, false
	}
	for _, raw := range []json.RawMessage{req.Title, req.Description, req.Password} {
		if !fieldIsJSONText(raw) {
			http.Error(w, "could not read the request", http.StatusBadRequest)
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
// Location. Its guard is requireKey, so a wrong key through it charges exactly as the retired
// POST / did. An absent field and one given as empty both make an empty value, which is what a
// bin with nothing to say holds; a password longer than bcrypt can hold is refused before
// anything is written.
func (s *server) apiCreateBin(w http.ResponseWriter, r *http.Request) {
	if !s.requireKey(w, r) {
		return
	}

	req, ok := decodeBinRequest(w, r, true)
	if !ok {
		return
	}

	// An absent field and one given as empty both make the empty value a bin with nothing to
	// say holds.
	title, _ := fieldValue(req.Title)
	description, _ := fieldValue(req.Description)
	if err := checkBinText("title", title, maxTitleBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := checkBinText("description", description, maxDescriptionBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// An empty password, given or not, leaves the bin open.
	password, _ := fieldValue(req.Password)
	if len(password) > 72 {
		http.Error(w, "bin password too long", http.StatusBadRequest)
		return
	}

	passwordHash := ""
	if password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("hash bin password: %v", err)
			http.Error(w, "could not set the bin password", http.StatusInternalServerError)
			return
		}
		passwordHash = string(hashed)
	}

	hash, err := s.store.CreateBin(passwordHash, title, description)
	if err != nil {
		log.Printf("create bin: %v", err)
		http.Error(w, "could not create bin", http.StatusInternalServerError)
		return
	}

	// created_at is the store's own, so the answer reads the row back instead of guessing it.
	bin, ok, err := s.store.GetBin(hash)
	if err != nil || !ok {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "could not create bin", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", "/api/bins/"+hash)
	if err := writeJSON(w, http.StatusCreated, binToJSON(bin)); err != nil {
		log.Printf("encode the answer of bin %q: %v", hash, err)
		http.Error(w, "could not answer the bin", http.StatusInternalServerError)
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
	createdAt, hash, found := strings.Cut(string(decoded), cursorSeparator)
	if !found || createdAt == "" || hash == "" {
		return BinCursor{}, errors.New("cursor names no created_at and hash")
	}
	return BinCursor{CreatedAt: createdAt, Hash: hash}, nil
}

// apiListBins answers GET /api/bins, the key-gated listing: its guard is requireKey, so a
// wrong key through it charges exactly like a wrong key through a write. The paging rules are
// pinned: limit default 30, a non-integer or a limit below 1 is the plain 400 invalid limit,
// anything above 100 clamps to 100; cursor is the unpadded URL-safe base64 a previous page
// answered, and a malformed one is the plain 400 invalid cursor. Bins list complete —
// protected ones with their metadata and has_password in plain view, the owner's view.
func (s *server) apiListBins(w http.ResponseWriter, r *http.Request) {
	if !s.requireKey(w, r) {
		return
	}

	limit := defaultListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
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
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cutoff = &cursor
	}

	bins, hasMore, err := s.store.ListBins(limit, cutoff)
	if err != nil {
		log.Printf("list bins: %v", err)
		http.Error(w, "could not list bins", http.StatusInternalServerError)
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
		http.Error(w, "could not list bins", http.StatusInternalServerError)
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
// sorted by path. A password-protected bin answers 401 until Basic opens it, and that 401
// records no failure — the API-key lockout is not the bin's password gate.
func (s *server) apiInspectBin(w http.ResponseWriter, r *http.Request, hash string) {
	bin, ok, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !ok {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !s.binAuthorized(w, r, bin) {
		return
	}

	files, err := s.store.ListFiles(hash)
	if err != nil {
		log.Printf("list bin %q: %v", hash, err)
		http.Error(w, "could not list bin", http.StatusInternalServerError)
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
		http.Error(w, "could not answer the bin", http.StatusInternalServerError)
	}
}

// apiPatchBin merges a JSON body into a bin's row: a given field is set — null and "" clear
// theirs, a password is stored as a bcrypt hash — and an absent one leaves what the bin holds,
// which is what UpdateBinMeta's COALESCE reads from a NULL argument. The order is pinned:
// key, body validation, bin lookup, and only then the bin's own password gate, so a protected
// bin is changed only by a request that carries its current password, and a refused request
// changes nothing. {} is a no-op answering the current row; no body at all is refused as
// unreadable, unlike create's pinned empty request.
func (s *server) apiPatchBin(w http.ResponseWriter, r *http.Request, hash string) {
	if !s.requireKey(w, r) {
		return
	}

	req, ok := decodeBinRequest(w, r, false)
	if !ok {
		return
	}

	// Fields are validated before the row is read: decoding touches no data, so a bad body is
	// a 400 even where the bin is unknown or guarded.
	title, titleGiven := fieldValue(req.Title)
	if titleGiven {
		if err := checkBinText("title", title, maxTitleBytes); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	description, descriptionGiven := fieldValue(req.Description)
	if descriptionGiven {
		if err := checkBinText("description", description, maxDescriptionBytes); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	password, passwordGiven := fieldValue(req.Password)
	if passwordGiven && len(password) > 72 {
		http.Error(w, "bin password too long", http.StatusBadRequest)
		return
	}

	bin, found, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !found {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	// Clearing or rotating a protected bin's password goes through the same gate as every
	// other change to it: the current password first, and a 401 that charges nothing.
	if !s.binAuthorized(w, r, bin) {
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
				http.Error(w, "could not set the bin password", http.StatusInternalServerError)
				return
			}
			passwordArg = sql.NullString{String: string(hashed), Valid: true}
		}
	}

	updated, err := s.store.UpdateBinMeta(hash, titleArg, descriptionArg, passwordArg)
	if err != nil {
		log.Printf("update bin %q: %v", hash, err)
		http.Error(w, "could not update bin", http.StatusInternalServerError)
		return
	}
	if !updated {
		// The bin went away between the read and the write.
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}

	// The answer carries the row as it now stands, so the caller never has to guess what a
	// leave-and-replace mix left behind.
	bin, found, err = s.store.GetBin(hash)
	if err != nil {
		log.Printf("re-read bin %q after the update: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !found {
		// The bin went away between the update and the re-read.
		log.Printf("re-read bin %q after the update: the bin is gone", hash)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if err := writeJSON(w, http.StatusOK, binToJSON(bin)); err != nil {
		log.Printf("encode the answer of bin %q: %v", hash, err)
		http.Error(w, "could not answer the bin", http.StatusInternalServerError)
	}
}
