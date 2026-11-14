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
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/yookoala/sandbin"
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

// ServeHTTP sends "/" to bin creation (POST) or the front page (GET) and everything else to
// the bin subrouter. Path validation happens here, before anything touches SQL.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/") {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}

	if r.URL.Path == "/" {
		switch r.Method {
		case http.MethodPost:
			s.createBin(w, r)
		case http.MethodGet:
			s.renderFrontPage(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

	// "verify-key" holds a dash, so it can never name a hex bin hash, same reasoning as healthz.
	if r.URL.Path == "/verify-key" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.verifyKey(w, r)
		return
	}

	// "skills" is not a hex hash, so this exact path can never name a bin, same reasoning as
	// the two above. Only GET answers; a GET never charges and a 405 answers before any key.
	if r.URL.Path == "/skills/sandbin-preview/SKILL.md" || r.URL.Path == "/skills/sandbin-preview/install.sh" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Both files are text/templates whose {{.Base}} is filled with the address this
		// request arrived at, so a downloaded skill or installer names exactly the
		// server the visitor reached, behind proxies included.
		tmpl, contentType := skillTemplate, "text/markdown; charset=utf-8"
		if r.URL.Path == "/skills/sandbin-preview/install.sh" {
			tmpl, contentType = installTemplate, "text/plain; charset=utf-8"
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, skillData{Base: s.frontBase(r)}); err != nil {
			log.Printf("render %s: %v", r.URL.Path, err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(buf.Bytes())
		return
	}

	// "docs" is no eight-hex hash, so this branch can never name a bin, same reasoning as the
	// three above. Only GET and HEAD answer, and a page is chosen by looking its whole name up
	// in the map built at startup: nothing under /docs is ever joined onto a path.
	if r.URL.Path == "/docs" || r.URL.Path == "/docs/" || strings.HasPrefix(r.URL.Path, "/docs/") {
		s.serveDocs(w, r)
		return
	}

	hash, sub, ok := splitBinPath(r.URL.Path)
	if !ok {
		// "/{hash}" without its trailing slash redirects to "/{hash}/" (301) when that
		// bin exists, so relative links inside the bin resolve. GitHub does the same for
		// its pages. Unknown hashes and every method other than GET keep the plain 404:
		// a redirect would silently change what a PUT or DELETE meant.
		if r.Method == http.MethodGet {
			if seg, single := singleSegment(r.URL.Path); single {
				if _, exists, err := s.store.GetBin(seg); err != nil {
					log.Printf("get bin %q: %v", seg, err)
				} else if exists {
					target := r.URL.Path + "/"
					if r.URL.RawQuery != "" {
						target += "?" + r.URL.RawQuery
					}
					http.Redirect(w, r, target, http.StatusMovedPermanently)
					return
				}
			}
		}
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

// singleSegment reports the lone segment when urlPath is exactly "/seg" with a
// non-empty segment — the shape "/{hash}" missing its trailing slash.
func singleSegment(urlPath string) (string, bool) {
	seg := strings.TrimPrefix(urlPath, "/")
	if seg == "" || strings.Contains(seg, "/") {
		return "", false
	}
	return seg, true
}

// binRoutes serves GET, PUT, DELETE and PATCH under one bin hash.
func (s *server) binRoutes(w http.ResponseWriter, r *http.Request, hash, sub string) {
	switch r.Method {
	case http.MethodGet:
		s.getFile(w, r, hash, sub)
	case http.MethodPut:
		s.putFile(w, r, hash, sub)
	case http.MethodDelete:
		s.deletePath(w, r, hash, sub)
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

// verifyKey answers the key check: its guard is createBin's verbatim, so a wrong key
// through it charges exactly like a wrong key through POST /, and a match creates
// nothing and charges nothing.
func (s *server) verifyKey(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "key ok")
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
		// A bin whose files include a root 404.html answers an unmatched path with that page
		// and keeps the status 404, which is how a single-page app serves its own routes out
		// of a bin. Only the root file counts, mirroring the index.html check above, and only
		// a GET gets here at all.
		fallback, have, err := s.store.GetFile(hash, "404.html")
		if err != nil {
			log.Printf("get the fallback of bin %q: %v", hash, err)
			http.Error(w, "could not read file", http.StatusInternalServerError)
			return
		}
		if have {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write(fallback)
			return
		}
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

// deleteBody is the DELETE answer: the bin-relative paths that were removed, a folder
// listing every file it took.
type deleteBody struct {
	Deleted []string `json:"deleted"`
}

// deletePath removes one file, or a whole folder when the path ends in a slash. Its guards
// are putFile's in the same order: the block check that charges, the key check that charges,
// the path shape, the bin lookup and its password, and only then the body a DELETE must not
// carry. The bin base names no path, so a DELETE there is method not allowed: a bin itself is
// never deleted.
func (s *server) deletePath(w http.ResponseWriter, r *http.Request, hash, sub string) {
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

	// The trailing slash is the folder marker, so it is trimmed here rather than left for
	// cleanPath, which keeps refusing it for every other method.
	folder := strings.HasSuffix(sub, "/")
	sub = strings.TrimSuffix(sub, "/")
	if sub == "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	// A DELETE carries nothing. The cap is here so a body sent along with one is refused
	// before it is read.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) > 0 {
		http.Error(w, "delete takes no body", http.StatusBadRequest)
		return
	}

	var deleted []string
	if folder {
		deleted, err = s.store.DeleteFolder(hash, sub)
	} else {
		var gone bool
		gone, err = s.store.DeleteFile(hash, sub)
		if gone {
			deleted = []string{sub}
		}
	}
	if err != nil {
		log.Printf("delete %q from bin %q: %v", sub, hash, err)
		http.Error(w, "could not delete file", http.StatusInternalServerError)
		return
	}
	if len(deleted) == 0 {
		// An unknown file and a folder nothing lives under are the same refusal.
		http.Error(w, "unknown path", http.StatusNotFound)
		return
	}

	out, err := json.Marshal(deleteBody{Deleted: deleted})
	if err != nil {
		log.Printf("encode the deleted paths of bin %q: %v", hash, err)
		http.Error(w, "could not answer the delete", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(append(out, '\n'))
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

// The embedded skill and installer are text/templates; parsing them once at startup
// means a broken template stops the whole server instead of one request.
var (
	skillTemplate   = template.Must(template.New("skill").Parse(sandbin.PreviewSkillMD))
	installTemplate = template.Must(template.New("install").Parse(sandbin.InstallScript))
)

// skillData is what both templates receive: the one address the request arrived at.
type skillData struct{ Base string }

// frontHostRE bounds any host the page may echo: a plain hostname, a bracketed IPv6 or
// either with a port — nothing that could turn a snippet into other shell text.
var frontHostRE = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9.\-]*|\[[0-9A-Fa-f:.]+\])(:[0-9]+)?$`)

// renderFrontPage writes the branded page the root answers with on GET: the wordmark inline,
// the one-line tagline, the GitHub links, the installer one-liner with its copy button, and the
// links into the hosted docs. It says nothing about any bin, address or key; only the base URL
// of its one command is dynamic — see frontBase.
func (s *server) renderFrontPage(w http.ResponseWriter, r *http.Request) {
	const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sandbin</title>
<style>
<!--brand-css-->
</style>
</head>
<body>
<!--brand-head-->
<h1 id="sandbin">Sandbin</h1>
<p class="tagline">A PUT/GET preview server for CLI agents in a sandbox: submit HTML, ANSI and
prototype files, and hand a human plain preview URLs.</p>
<p><a href="https://github.com/yookoala/sandbin">github.com/yookoala/sandbin</a> &middot;
<a href="https://github.com/yookoala/sandbin/tree/main/docs/usages">the usage docs on GitHub</a></p>
<h2 id="give-the-skill-to-your-agent">Give the skill to your agent</h2>
<p>The <code>sandbin-preview</code> skill teaches an agent to set up <code>sandbin-client</code> in a
directory and upload files or whole folders for preview. Install it with one command:</p>
<div class="cmd">
<pre id="install-command">curl -fsSL https://this.host/skills/sandbin-preview/install.sh | bash</pre>
<button type="button" class="copy" onclick="sandbinCopyInstall(this)">copy</button>
</div>
<p>That installs into <code>$HOME/.agents/skills/sandbin-preview</code> and, at a terminal,
initializes the current directory by asking a single question, the API key. For a single project
instead, hand the installer a destination: <code>| bash -s -- --dest .opencode/skills</code>.</p>
<h2 id="the-hosted-docs">The hosted docs</h2>
<p>A running <code>sandbin</code> serves its own usage documentation: <a href="/docs">the docs
hub</a>, <a href="/docs/sandbin.html">sandbin, the server</a> and
<a href="/docs/sandbin-client.html">sandbin-client, the client</a>.</p>
<script>
function sandbinCopyInstall(button) {
  var command = document.getElementById("install-command").textContent;
  function done() { button.textContent = "copied"; }
  function fallback() {
    var area = document.createElement("textarea");
    area.value = command;
    document.body.appendChild(area);
    area.select();
    document.execCommand("copy");
    document.body.removeChild(area);
    done();
  }
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(command).then(done, fallback);
  } else {
    fallback();
  }
}
</script>
</body>
</html>
`
	head := "<header class=\"page-head\">\n" +
		"<a class=\"logo-link\" href=\"/\" aria-label=\"Sandbin home\">" + logoBlock("wordmark wordmark-hero") + "</a>\n" +
		"<nav><a href=\"/docs\">docs</a><a href=\"https://github.com/yookoala/sandbin\">source on GitHub</a></nav>\n" +
		"</header>\n"
	page := strings.NewReplacer(
		"<!--brand-css-->", brandCSS,
		"<!--brand-head-->", head,
		"https://this.host", s.frontBase(r),
	).Replace(pageTemplate)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, page)
}

// frontBase is the example prefix implied by the visitor's own request: the Host they
// reached — or the X-Forwarded-Host of a proxy the server already trusts, same socket
// rule as X-Forwarded-For — and https only for a TLS socket or a trusted proxy saying
// X-Forwarded-Proto: https, since a proxy hop to this server is plain HTTP. A Host
// header is attacker-supplied text landing in a snippet meant to be pasted into a
// shell, so it must pass frontHostRE whole; anything odd falls back to the placeholder.
func (s *server) frontBase(r *http.Request) string {
	socket, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		socket = r.RemoteAddr
	}
	trusted := isLocalAddr(socket) || s.trustedProxies.containsHost(socket)

	host := r.Host
	if trusted {
		if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
			entries := strings.Split(xfh, ",")
			// The rightmost entry is the nearest proxy's, as in clientIP.
			host = strings.TrimSpace(entries[len(entries)-1])
		}
	}
	if !frontHostRE.MatchString(host) {
		return "https://this.host"
	}
	if r.TLS != nil || (trusted && r.Header.Get("X-Forwarded-Proto") == "https") {
		return "https://" + host
	}
	return "http://" + host
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
