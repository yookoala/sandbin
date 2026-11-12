package main

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	apiKeyHeader         = "X-API-Key"
	binPasswordHeader    = "X-Bin-Password"
	binTitleHeader       = "X-Bin-Title"
	binDescriptionHeader = "X-Bin-Description"
	extractHeader        = "X-Extract"
	maxBodyBytes         = 1 << 20 // 1 MiB per PUT
	maxPathLen           = 256
	maxPathSegs          = 16
	maxTitleBytes        = 200
	maxDescriptionBytes  = 2000
)

type server struct {
	store  *Store
	apiKey []byte

	// trustedProxies adds configured proxy addresses to the loopback, private and
	// link-local sockets whose X-Forwarded-For clientIP already believes.
	trustedProxies proxyList
}

// ServeHTTP sends "/" to bin creation and everything else to the bin subrouter.
// Path validation happens here, before anything touches SQL.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/") {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}

	if r.URL.Path == "/" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.createBin(w, r)
		return
	}

	// Unauthenticated on purpose for the container HEALTHCHECK: it says nothing
	// about bins, and bin hashes are hex so "healthz" can never name one.
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok")
		return
	}

	hash, sub, ok := splitBinPath(r.URL.Path)
	if !ok {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if hash == "." || hash == ".." {
		http.Error(w, "path must not contain . or ..", http.StatusBadRequest)
		return
	}
	s.binRoutes(w, r, hash, sub)
}

// splitBinPath cuts "/{hash}/{sub}" into its bin hash and the raw sub path
// below it. ok is false when there is no bin segment to look for.
func splitBinPath(urlPath string) (hash, sub string, ok bool) {
	first, rest, found := strings.Cut(strings.TrimPrefix(urlPath, "/"), "/")
	if !found || first == "" {
		return "", "", false
	}
	return first, rest, true
}

// binRoutes serves GET, PUT and PATCH under one bin hash.
func (s *server) binRoutes(w http.ResponseWriter, r *http.Request, hash, sub string) {
	switch r.Method {
	case http.MethodGet:
		s.getFile(w, r, hash, sub)
	case http.MethodPut:
		s.putFile(w, r, hash, sub)
	case http.MethodPatch:
		s.patchBin(w, r, hash, sub)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// createBin makes a bin and answers with the path it lives under.
func (s *server) createBin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.trustedProxies)
	if until, blocked := s.store.IsBlocked(ip, time.Now()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
		// An attempt while blocked is itself a failure, so hammering walks the penalty up.
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "too many bad API keys", http.StatusTooManyRequests)
		return
	}

	if subtle.ConstantTimeCompare([]byte(r.Header.Get(apiKeyHeader)), s.apiKey) != 1 {
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	// An empty X-Bin-Password means the bin stays open.
	passwordHash := ""
	if plain := r.Header.Get(binPasswordHeader); plain != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
		if err != nil {
			if errors.Is(err, bcrypt.ErrPasswordTooLong) {
				http.Error(w, "bin password too long", http.StatusBadRequest)
				return
			}
			log.Printf("hash bin password: %v", err)
			http.Error(w, "could not set the bin password", http.StatusInternalServerError)
			return
		}
		passwordHash = string(hashed)
	}

	// An absent header leaves the field empty, which is the same bin as a bin nobody
	// gave metadata.
	title := r.Header.Get(binTitleHeader)
	description := r.Header.Get(binDescriptionHeader)
	if err := checkBinText("title", title, maxTitleBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := checkBinText("description", description, maxDescriptionBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash, err := s.store.CreateBin(passwordHash, title, description)
	if err != nil {
		log.Printf("create bin: %v", err)
		http.Error(w, "could not create bin", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, "/"+hash+"/")
}

func (s *server) getFile(w http.ResponseWriter, r *http.Request, hash, sub string) {
	if sub != "" {
		var err error
		if sub, err = cleanPath(sub); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

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

	if sub == "" {
		// The bin's own words ride the answer of its base path, on the raw index.html
		// branch as well as the listing one: the page's author owns that file, so the
		// metadata reaches a reader only as headers there.
		if bin.Title != "" {
			w.Header().Set(binTitleHeader, bin.Title)
		}
		if bin.Description != "" {
			w.Header().Set(binDescriptionHeader, bin.Description)
		}

		content, ok, err := s.store.GetFile(hash, "index.html")
		if err != nil {
			log.Printf("get index of bin %q: %v", hash, err)
			http.Error(w, "could not read bin", http.StatusInternalServerError)
			return
		}
		if ok {
			// shortcut: a stored index.html is served raw, so its author owns that page.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(content)
			return
		}
		files, err := s.store.ListFiles(hash)
		if err != nil {
			log.Printf("list bin %q: %v", hash, err)
			http.Error(w, "could not list bin", http.StatusInternalServerError)
			return
		}
		renderListing(w, bin, files)
		return
	}

	content, ok, err := s.store.GetFile(hash, sub)
	if err != nil {
		log.Printf("get %q in bin %q: %v", sub, hash, err)
		http.Error(w, "could not read file", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "unknown path", http.StatusNotFound)
		return
	}
	if strings.ToLower(path.Ext(sub)) == ".ansi" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, ansiToHTML(string(content)))
		return
	}
	w.Header().Set("Content-Type", contentType(sub, content))
	w.Write(content)
}

func (s *server) putFile(w http.ResponseWriter, r *http.Request, hash, sub string) {
	ip := clientIP(r, s.trustedProxies)
	if until, blocked := s.store.IsBlocked(ip, time.Now()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
		// An attempt while blocked is itself a failure, so hammering walks the penalty up.
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "too many bad API keys", http.StatusTooManyRequests)
		return
	}

	if subtle.ConstantTimeCompare([]byte(r.Header.Get(apiKeyHeader)), s.apiKey) != 1 {
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	if sub == "" {
		http.Error(w, "empty path", http.StatusBadRequest)
		return
	}
	sub, err := cleanPath(sub)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bin, ok, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	} else if !ok {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !s.binAuthorized(w, r, bin) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			http.Error(w, "body over 1 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	// X-Extract present at all, any value, is the opt-out: store and serve the body
	// raw, no inspection. Otherwise an archive name plus archive magic is extracted.
	if _, keep := r.Header[extractHeader]; !keep {
		kind, err := detectArchive(sub, content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if kind != archiveNone {
			s.putArchive(w, hash, sub, kind, content)
			return
		}
	}

	replaced, err := s.store.PutFile(hash, sub, content)
	if err != nil {
		log.Printf("put %q in bin %q: %v", sub, hash, err)
		http.Error(w, "could not store file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if replaced {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	io.WriteString(w, "/"+hash+"/"+sub)
}

// putArchive answers an extraction upload: the entries land under the upload name's
// parent directory in one transaction, the archive name itself is not stored, and the
// answer is the plain-PUT line pluralized, one per extracted file.
func (s *server) putArchive(w http.ResponseWriter, hash, sub string, kind archiveKind, body []byte) {
	files, err := extractArchive(kind, sub, body)
	if err != nil {
		if status := archiveErrStatus(err); status != http.StatusInternalServerError {
			http.Error(w, err.Error(), status)
			return
		}
		log.Printf("extract %q in bin %q: %v", sub, hash, err)
		http.Error(w, "could not extract archive", http.StatusInternalServerError)
		return
	}

	created, _, err := s.store.PutFiles(hash, files)
	if err != nil {
		log.Printf("put %d extracted files in bin %q: %v", len(files), hash, err)
		http.Error(w, "could not store file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Extracted-Count", strconv.Itoa(len(files)))
	if created >= 1 {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, "/"+hash+"/"+f.Path)
	}
	io.WriteString(w, strings.Join(lines, "\n"))
}

// binMetaBody is the PATCH answer: the bin's metadata as its row now holds it.
type binMetaBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// patchBin merges X-Bin-Title and X-Bin-Description into a bin's row: a header that is
// present replaces its field, an absent one leaves it, an empty value clears it. Only the
// bin base takes a PATCH, and the request body is ignored.
func (s *server) patchBin(w http.ResponseWriter, r *http.Request, hash, sub string) {
	if sub != "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := clientIP(r, s.trustedProxies)
	if until, blocked := s.store.IsBlocked(ip, time.Now()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
		// An attempt while blocked is itself a failure, so hammering walks the penalty up.
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "too many bad API keys", http.StatusTooManyRequests)
		return
	}

	if subtle.ConstantTimeCompare([]byte(r.Header.Get(apiKeyHeader)), s.apiKey) != 1 {
		if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
			log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
		}
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	// The headers are checked before any SQL runs, so a refused PATCH leaves the row as it was.
	titleRaw, titleGiven := headerValue(r.Header, binTitleHeader)
	descriptionRaw, descriptionGiven := headerValue(r.Header, binDescriptionHeader)
	if err := checkBinText("title", titleRaw, maxTitleBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := checkBinText("description", descriptionRaw, maxDescriptionBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bin, ok, err := s.store.GetBin(hash)
	if err != nil {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	} else if !ok {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if !s.binAuthorized(w, r, bin) {
		return
	}

	// A NULL argument is the "leave it" case the COALESCE in UpdateBinMeta reads.
	var title, description sql.NullString
	if titleGiven {
		title = sql.NullString{String: titleRaw, Valid: true}
	}
	if descriptionGiven {
		description = sql.NullString{String: descriptionRaw, Valid: true}
	}
	updated, err := s.store.UpdateBinMeta(hash, title, description)
	if err != nil {
		log.Printf("update metadata of bin %q: %v", hash, err)
		http.Error(w, "could not update bin", http.StatusInternalServerError)
		return
	}
	if !updated {
		// The bin went away between the read and the write.
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}

	bin, ok, err = s.store.GetBin(hash)
	if err != nil || !ok {
		log.Printf("get bin %q: %v", hash, err)
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}

	// The answer carries the row as it now stands, so the caller never has to guess what
	// a keep-and-replace mix left behind.
	out, err := json.Marshal(binMetaBody{Title: bin.Title, Description: bin.Description})
	if err != nil {
		log.Printf("encode metadata of bin %q: %v", hash, err)
		http.Error(w, "could not answer bin metadata", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(append(out, '\n'))
}

// headerValue reports a header's value and whether it was sent at all, which is the
// difference between "leave this field alone" and "clear it".
func headerValue(h http.Header, name string) (string, bool) {
	values := h.Values(name)
	if values == nil {
		return "", false
	}
	return values[0], true
}

// checkBinText weighs a title or description against its byte cap and refuses the control
// characters a single line of metadata cannot hold, with the wording POST and PATCH answer
// a refusal in.
func checkBinText(name, raw string, maxBytes int) error {
	if len(raw) > maxBytes {
		return fmt.Errorf("%s over %d bytes", name, maxBytes)
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s may not contain control characters", name)
		}
	}
	return nil
}

// binAuthorized answers 401 itself when the bin has a password the request does
// not carry. bcrypt.CompareHashAndPassword is constant time on its own.
func (s *server) binAuthorized(w http.ResponseWriter, r *http.Request, bin Bin) bool {
	if bin.PasswordHash == "" {
		return true
	}
	// The user name is conventionally the bin hash, but any name works.
	_, password, ok := r.BasicAuth()
	if !ok || len(password) > 72 {
		// bcrypt silently compares only the first 72 bytes, so a longer candidate
		// could match a shorter stored password.
		w.Header().Set("WWW-Authenticate", `Basic realm="sandbin"`)
		http.Error(w, "bin password required", http.StatusUnauthorized)
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(bin.PasswordHash), []byte(password)) == nil {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="sandbin"`)
	http.Error(w, "bin password required", http.StatusUnauthorized)
	return false
}

// renderListing writes the directory listing the bin base answers with when the bin holds no
// index.html. Its shape is the old one: doctype, charset, the bin path as title and heading,
// then the files. A bin that says what it holds gains one styled paragraph above the list,
// and a bin with nothing to say is written byte for byte as it always was.
func renderListing(w http.ResponseWriter, bin Bin, files []FileEntry) {
	b := new(strings.Builder)
	b.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<title>/" + bin.Hash + "/</title>\n")
	b.WriteString("<h1>/" + bin.Hash + "/</h1>\n")
	if meta := listingMetaLine(bin); meta != "" {
		b.WriteString("<style>.bin-meta{color:#555;margin:0 0 1em}</style>\n")
		b.WriteString("<p class=\"bin-meta\">" + meta + "</p>\n")
	}
	if len(files) == 0 {
		b.WriteString("<p>empty bin</p>\n")
	} else {
		b.WriteString("<ul>\n")
		for _, f := range files {
			b.WriteString(fmt.Sprintf("<li><a href=\"./%s\">%s</a> (%d bytes, updated %s)</li>\n",
				html.EscapeString(f.Path), html.EscapeString(f.Path), f.Size, html.EscapeString(f.UpdatedAt)))
		}
		b.WriteString("</ul>\n")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, b.String())
}

// listingMetaLine renders the title and description as the one line of already-escaped HTML a
// directory listing carries them in, or the empty string when the bin has neither.
func listingMetaLine(bin Bin) string {
	title := html.EscapeString(bin.Title)
	desc := html.EscapeString(bin.Description)
	switch {
	case title != "" && desc != "":
		return "<strong>" + title + "</strong><br>" + desc
	case title != "":
		return "<strong>" + title + "</strong>"
	default:
		return desc
	}
}

// cleanPath validates a client-supplied sub path before any SQL runs and returns
// its canonical form.
func cleanPath(raw string) (string, error) {
	if len(raw) > maxPathLen {
		return "", errors.New("path too long")
	}
	if strings.ContainsRune(raw, '\\') {
		return "", errors.New("path may not contain backslashes")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("path may not contain control characters")
		}
	}
	if strings.HasPrefix(raw, "/") {
		return "", errors.New("path must be relative")
	}
	cleaned := path.Clean(raw)
	if cleaned != raw {
		return "", errors.New("path must not contain ., .., // or a trailing slash")
	}
	segs := strings.Split(cleaned, "/")
	if len(segs) > maxPathSegs {
		return "", errors.New("path has too many segments")
	}
	// path.Clean leaves a leading ".." alone, so it needs its own check.
	for _, seg := range segs {
		if seg == ".." {
			return "", errors.New("path must not escape the bin")
		}
	}
	return cleaned, nil
}

func contentType(subPath string, content []byte) string {
	switch strings.ToLower(path.Ext(subPath)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".md", ".txt":
		return "text/plain; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return "application/octet-stream"
	}
	return "text/plain; charset=utf-8"
}
