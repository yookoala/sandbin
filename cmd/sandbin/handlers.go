package main

import (
	"bytes"
	"crypto/subtle"
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
	apiKeyHeader      = "X-API-Key"
	binPasswordHeader = "X-Bin-Password"
	maxBodyBytes      = 1 << 20 // 1 MiB per PUT
	maxPathLen        = 256
	maxPathSegs       = 16
)

type server struct {
	store  *Store
	apiKey []byte
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

// binRoutes serves GET and PUT under one bin hash.
func (s *server) binRoutes(w http.ResponseWriter, r *http.Request, hash, sub string) {
	switch r.Method {
	case http.MethodGet:
		s.getFile(w, r, hash, sub)
	case http.MethodPut:
		s.putFile(w, r, hash, sub)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// createBin makes a bin and answers with the path it lives under.
func (s *server) createBin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
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

	hash, err := s.store.CreateBin(passwordHash)
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
		renderListing(w, hash, files)
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
	ip := clientIP(r)
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

// renderListing writes the directory listing a bin base path gets when it has
// no index.html.
func renderListing(w http.ResponseWriter, hash string, files []FileEntry) {
	head := "/" + hash + "/"
	var b strings.Builder
	b.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<title>" + html.EscapeString(head) + "</title>\n")
	b.WriteString("<h1>" + html.EscapeString(head) + "</h1>\n")
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
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return "application/octet-stream"
	}
	return "text/plain; charset=utf-8"
}
