package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// maxJSONBodyBytes caps every JSON body on /api, which is a much smaller shape than the
// 1 MiB a file PUT may carry.
const maxJSONBodyBytes = 64 << 10

// apiRoutes dispatches everything under /api. "api" is no eight-hex hash, so the prefix can
// never name or shadow a bin, same reasoning as the static routes below it. It answers POST
// /api/key, GET|HEAD /api/health and POST /api/bins today; GET and DELETE on /api/bins are the
// verbs Tasks 5 and 6 add, so they answer the plain 405 for now. A spelling the dispatch does
// not name — trailing-slash variants included — is the plain 404 unknown bin, and a known path
// on a wrong method is the plain 405.
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
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiCreateBin(w, r)
	default:
		http.Error(w, "unknown bin", http.StatusNotFound)
	}
}

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

// decodeBinRequest reads the capped JSON body of a bin write. No body is the same request as
// {}, which is how the decoder reports it too: an empty stream is EOF. The body is read whole
// first, because a decoder aimed straight at the struct takes null — which is no object — as an
// empty request, and takes a second value after the object as noise it may ignore. A field name
// this API does not have is the loud refusal, and so is anything that is not one JSON object,
// or a field that is not null or a string. ok says whether the handler may go on; when it is
// false the refusal is already written.
func decodeBinRequest(w http.ResponseWriter, r *http.Request) (binRequest, bool) {
	var req binRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	dec := json.NewDecoder(r.Body)
	var body json.RawMessage
	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
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

	req, ok := decodeBinRequest(w, r)
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
