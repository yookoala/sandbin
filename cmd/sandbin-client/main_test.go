package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// fakeServer mirrors the real server's contract so the client is tested without a
// network or the server binary: one 403 for the first bad API key and then 429 with
// a growing Retry-After for every later key-checked request, 404 for an unknown bin,
// 401 for a missing bin password, 413 for an oversized body, and 201 or 200
// according to whether the path was new. It counts requests, which is how these
// tests prove the client never retries.
type fakeBin struct {
	password    string
	title       string
	description string
}

type fakeServer struct {
	apiKey     string
	bodyLimit  int
	log        io.Writer // optional sink that notes each arriving request
	createBody string    // when set, POST answers with this instead of "/abc1234/"

	// Archive knobs for the archive tests: unpack makes a PUT without X-Extract
	// extract a gzipped tar body the way the real server does, and putFail, when
	// non-zero, answers every PUT with that status.
	unpack  bool
	putFail int

	mu          sync.Mutex
	requests    int
	puts        int
	patches     int
	deletes     int
	failures    int
	extractSeen []string           // each PUT's X-Extract value, in arrival order
	putPaths    []string           // each PUT's URL path, in arrival order
	deletePaths []string           // each DELETE's URL path, in arrival order
	deleteAuths []string           // each DELETE's Basic Auth user name, "" when it sent none
	bins        map[string]fakeBin // bin hash -> its password and metadata
	files       map[string]string  // "bin/path" -> stored body
}

// newFake starts a fake server, exports its url and key as the client's env, and
// runs the test in a private working directory.
func newFake(t *testing.T, apiKey string) *fakeServer {
	t.Helper()
	f := &fakeServer{apiKey: apiKey, bodyLimit: 1 << 20, bins: map[string]fakeBin{}, files: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("SANDBIN_URL", srv.URL)
	t.Setenv("SANDBIN_API_KEY", apiKey)
	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	t.Chdir(t.TempDir())
	return f
}

// newFakeAt starts a second fake server without moving the test's working directory
// or env, for the precedence and bin-reuse tests that need two servers.
func newFakeAt(t *testing.T, apiKey string) (*fakeServer, string) {
	t.Helper()
	f := &fakeServer{apiKey: apiKey, bodyLimit: 1 << 20, bins: map[string]fakeBin{}, files: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests++
	if r.Method == http.MethodPut {
		f.puts++
		f.extractSeen = append(f.extractSeen, r.Header.Get("X-Extract"))
		f.putPaths = append(f.putPaths, r.URL.Path)
	}
	if r.Method == http.MethodPatch {
		f.patches++
	}
	if r.Method == http.MethodDelete {
		f.deletes++
		f.deletePaths = append(f.deletePaths, r.URL.Path)
		user, _, ok := r.BasicAuth()
		if !ok {
			user = ""
		}
		f.deleteAuths = append(f.deleteAuths, user)
	}
	if f.log != nil {
		fmt.Fprintln(f.log, "request", r.Method)
	}

	keyChecked := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
	if keyChecked && f.failures > 0 {
		// An attempt while blocked is another failure, so the wait walks up.
		f.failures++
		w.Header().Set("Retry-After", fmt.Sprintf("%d", 1800*f.failures))
		http.Error(w, "too many bad API keys", http.StatusTooManyRequests)
		return
	}
	if keyChecked && r.Header.Get("X-API-Key") != f.apiKey {
		f.failures++
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	if r.URL.Path == "/verify-key" {
		// The real server's contract: only POST answers, and the key guard above is
		// what charged any wrong key before this point.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "key ok")
		return
	}

	if r.Method == http.MethodPost {
		hash := fmt.Sprintf("bin%04d", len(f.bins)+1)
		f.bins[hash] = fakeBin{
			password:    r.Header.Get("X-Bin-Password"),
			title:       r.Header.Get("X-Bin-Title"),
			description: r.Header.Get("X-Bin-Description"),
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		if f.createBody != "" {
			io.WriteString(w, f.createBody)
			return
		}
		io.WriteString(w, "/"+hash+"/")
		return
	}

	first, rest, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !found || first == "" {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	bin, known := f.bins[first]
	if !known {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		// GET, PATCH and DELETE read, write or empty the bin itself; nothing else is on
		// this fake, and every one of them proves the bin password first.
		_, password, ok := r.BasicAuth()
		if bin.password != "" && (!ok || password != bin.password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="sandbin"`)
			http.Error(w, "bin password required", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			// The metadata of a bin base GET comes back as headers, empty fields omitted.
			if bin.title != "" {
				w.Header().Set("X-Bin-Title", bin.title)
			}
			if bin.description != "" {
				w.Header().Set("X-Bin-Description", bin.description)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, "<!doctype html>\n<title>/"+first+"/</title>\n")
			return
		case http.MethodPatch:
			if rest != "" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// A header that was sent replaces its field, an empty value clears it, and a
			// header that was not sent leaves it. The answer is the row as it now stands.
			if values, ok := r.Header["X-Bin-Title"]; ok {
				bin.title = values[0]
			}
			if values, ok := r.Header["X-Bin-Description"]; ok {
				bin.description = values[0]
			}
			f.bins[first] = bin
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "{\"title\":%q,\"description\":%q}\n", bin.title, bin.description)
			return
		case http.MethodDelete:
			if rest == "" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
			if err != nil {
				http.Error(w, "could not read body", http.StatusBadRequest)
				return
			}
			if len(body) > 0 {
				http.Error(w, "delete takes no body", http.StatusBadRequest)
				return
			}
			// The real server's rule: the trailing slash marks the folder, and a folder is
			// every path under it, answered sorted.
			folder := strings.HasSuffix(rest, "/")
			sub := strings.TrimSuffix(rest, "/")
			var deleted []string
			if folder {
				prefix := first + "/" + sub + "/"
				for key := range f.files {
					if strings.HasPrefix(key, prefix) {
						deleted = append(deleted, strings.TrimPrefix(key, first+"/"))
					}
				}
				sort.Strings(deleted)
				for _, name := range deleted {
					delete(f.files, first+"/"+name)
				}
			} else if _, stored := f.files[first+"/"+sub]; stored {
				delete(f.files, first+"/"+sub)
				deleted = []string{sub}
			}
			if len(deleted) == 0 {
				http.Error(w, "unknown path", http.StatusNotFound)
				return
			}
			out, err := json.Marshal(struct {
				Deleted []string `json:"deleted"`
			}{deleted})
			if err != nil {
				http.Error(w, "could not answer the delete", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(append(out, '\n'))
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest == "" {
		http.Error(w, "empty path", http.StatusBadRequest)
		return
	}
	if bin.password != "" {
		_, sent, ok := r.BasicAuth()
		if !ok || sent != bin.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="sandbin"`)
			http.Error(w, "bin password required", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(f.bodyLimit)+1))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}
	if len(body) > f.bodyLimit {
		http.Error(w, "body over 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	// The archive branch mirrors the real server's contract: presence of X-Extract
	// stores the body raw, and otherwise a gzipped tar body is unpacked into files
	// whose names carry the archive name's directory prefix — the archive name
	// itself is never stored. putFail answers every PUT with its status.
	if f.putFail != 0 {
		http.Error(w, "injected failure", f.putFail)
		return
	}
	if f.unpack && r.Header.Get("X-Extract") == "" && len(body) > 0 && body[0] == 0x1f && body[1] == 0x8b {
		names, created, err := f.unpackTarGz(first, rest, body)
		if err != nil {
			http.Error(w, "corrupt tar.gz archive", http.StatusBadRequest)
			return
		}
		w.Header().Set("X-Extracted-Count", strconv.Itoa(len(names)))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if created {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		for _, name := range names {
			fmt.Fprintf(w, "/%s/%s\n", first, name)
		}
		return
	}

	key := first + "/" + rest
	replaced := f.files[key] != ""
	f.files[key] = string(body)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if replaced {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	io.WriteString(w, "/"+key)
}

// addBin registers a bin with the fake without going through POST, for the test that
// names one with --bin.
func (f *fakeServer) addBin(password string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	hash := fmt.Sprintf("bin%04d", len(f.bins)+1)
	f.bins[hash] = fakeBin{password: password}
	return hash
}

// binMeta reads back what the fake holds for a bin, which is how the metadata tests say
// what the server actually stored.
func (f *fakeServer) binMeta(hash string) (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bin := f.bins[hash]
	return bin.title, bin.description
}

func (f *fakeServer) requestsSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// putsSeen counts only the uploads, which is the number a no-retry assertion cares
// about: the create-bin POST is a separate request.
func (f *fakeServer) putsSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts
}

// patchesSeen counts the PATCHes, which is how a test says a created bin took no second
// request and an existing one took exactly one.
func (f *fakeServer) patchesSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.patches
}

func (f *fakeServer) binCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bins)
}

func (f *fakeServer) stored(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[key]
}

// setBodyLimit and setLog change the fake from the test goroutine, so the change
// goes through the lock the handler reads under.
func (f *fakeServer) setBodyLimit(limit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodyLimit = limit
}

// setUnpack turns the fake's archive branch on, and setPutFail answers every PUT
// with the given status. Both change from the test goroutine under the handler's lock.
func (f *fakeServer) setUnpack(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unpack = on
}

func (f *fakeServer) setPutFail(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putFail = status
}

// extractValues is each PUT's X-Extract value in arrival order, "" when the header
// was absent, which is how the archive tests assert the opt-out header is sent.
func (f *fakeServer) extractValues() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.extractSeen...)
}

// putOrder is each PUT's URL path in arrival order, which is how the archive tests
// assert the upload order of a combined run.
func (f *fakeServer) putOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.putPaths...)
}

// deletesSeen counts the DELETEs, which is how a delete test says the client made exactly
// one request per path and never retried a refusal.
func (f *fakeServer) deletesSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deletes
}

// deleteOrder is each DELETE's URL path in arrival order.
func (f *fakeServer) deleteOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletePaths...)
}

// deleteAuthNames is each DELETE's Basic Auth user name, "" when the request sent none.
func (f *fakeServer) deleteAuthNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleteAuths...)
}

// unpackTarGz runs under ServeHTTP's lock. Entries land under the upload name's
// parent directory like the real server puts them, and the returned names are the
// bin paths the fake now holds.
func (f *fakeServer) unpackTarGz(bin, sub string, body []byte) ([]string, bool, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	prefix := path.Dir(sub) // "." for an archive at the bin root
	var names []string
	created := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names, created, nil
		}
		if err != nil {
			return nil, false, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name := hdr.Name
		if prefix != "." {
			name = path.Join(prefix, name)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, false, err
		}
		key := bin + "/" + name
		if _, seen := f.files[key]; !seen {
			created = true
		}
		f.files[key] = string(content)
		names = append(names, name)
	}
}

func (f *fakeServer) setLog(w io.Writer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = w
}

func (f *fakeServer) setCreateBody(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createBody = body
}

// runClient runs the client with its env already pointed at the fake and returns
// what it wrote to stdout and stderr.
func runClient(t *testing.T, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := run(args, stdin, &out, &errs)
	return out.String(), errs.String(), err
}

// writeWorkDirFile writes a file into the test's working directory, making its
// parent directory when the name has one.
func writeWorkDirFile(t *testing.T, name, body string) {
	t.Helper()
	if dir := filepath.Dir(name); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(name, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// readConfigFile reads .sandbin.yaml from the test's working directory.
func readConfigFile(t *testing.T) ([]byte, os.FileInfo) {
	t.Helper()
	info, err := os.Stat(configFileName)
	if err != nil {
		t.Fatalf("stat %s: %v", configFileName, err)
	}
	raw, err := os.ReadFile(configFileName)
	if err != nil {
		t.Fatalf("read %s: %v", configFileName, err)
	}
	return raw, info
}

// binHashOf pulls the hash out of the bin base URL the client printed last.
func binHashOf(t *testing.T, stdout string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	base := lines[len(lines)-1]
	hash := strings.Trim(strings.TrimPrefix(base, os.Getenv("SANDBIN_URL")+"/"), "/")
	if hash == "" || strings.Contains(hash, "/") {
		t.Fatalf("last stdout line %q is not a bin base URL", base)
	}
	return hash
}

func TestUploadsFilesAndPrintsTheBinURLLast(t *testing.T) {
	f := newFake(t, "test-key")
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		writeWorkDirFile(t, name, "body of "+name)
	}

	stdout, stderr, err := runClient(t, nil, "index.html", "app.js", "style.css")
	if err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing on a successful run", stderr)
	}

	base := os.Getenv("SANDBIN_URL")
	want := "created " + base + "/bin0001/index.html\n" +
		"created " + base + "/bin0001/app.js\n" +
		"created " + base + "/bin0001/style.css\n" +
		base + "/bin0001/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if f.requestsSeen() != 4 {
		t.Errorf("requests = %d, want 1 POST and 3 PUTs", f.requestsSeen())
	}
	if got := f.stored("bin0001/app.js"); got != "body of app.js" {
		t.Errorf("stored app.js = %q, want the file's bytes", got)
	}
}

func TestSecondRunReusesTheConfigFileAndReportsUpdated(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>First</h1>")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)

	writeWorkDirFile(t, "index.html", "<h1>Second</h1>")
	stdout, stderr, err = runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("second run: %v; stderr %s", err, stderr)
	}

	base := os.Getenv("SANDBIN_URL")
	want := "updated " + base + "/" + hash + "/index.html\n" + base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("second stdout =\n%s\nwant\n%s", stdout, want)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the config file's one bin", f.binCount())
	}
	if got := f.stored(hash + "/index.html"); got != "<h1>Second</h1>" {
		t.Errorf("stored index.html = %q, want the replaced bytes", got)
	}
}

func TestNewFlagCreatesASecondBin(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	first, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	oldHash := binHashOf(t, first)

	second, _, err := runClient(t, nil, "--new", "index.html")
	if err != nil {
		t.Fatalf("run with --new: %v", err)
	}
	newHash := binHashOf(t, second)
	if newHash == oldHash {
		t.Fatalf("--new reused bin %s, want a fresh bin", newHash)
	}
	if f.binCount() != 2 {
		t.Errorf("bins = %d, want 2", f.binCount())
	}

	cfgRaw, _ := readConfigFile(t)
	if !strings.Contains(string(cfgRaw), newHash) {
		t.Errorf("config file =\n%s\nwant it to remember the new bin %s", cfgRaw, newHash)
	}
	if strings.Contains(string(cfgRaw), oldHash) {
		t.Errorf("config file =\n%s\nwant the replaced bin gone", cfgRaw)
	}
}

func TestMissingAPIKeyFailsBeforeAnyRequest(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_API_KEY", "")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run without a key succeeded, want a startup error")
	}
	if !strings.Contains(stderr, "no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init") {
		t.Errorf("stderr = %q, want the refusal naming --api-key, SANDBIN_API_KEY and init", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestWrongAPIKeyReports403WithExactlyOneRequest(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_API_KEY", "wrong-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run with a wrong key succeeded, want an error")
	}
	if !strings.Contains(stderr, "403") || !strings.Contains(stderr, "invalid API key") {
		t.Errorf("stderr = %q, want 403 and the server's message", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	// One request is the proof of no retry: a retrying client would log two here and
	// hand this address a second 30 minute penalty.
	if f.requestsSeen() != 1 {
		t.Errorf("requests = %d, want exactly 1", f.requestsSeen())
	}
	if _, err := os.Stat(configFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after a failed run, want no config file", configFileName)
	}
}

func TestBlockedAddressReports429WithTheWait(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	t.Setenv("SANDBIN_API_KEY", "wrong-key")
	if _, _, err := runClient(t, nil, "index.html"); err == nil {
		t.Fatalf("the wrong-key run should have failed")
	}

	// The same address now holds the right key and is still blocked by its failure.
	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("the blocked run succeeded, want a 429")
	}
	if !strings.Contains(stderr, "429") || !strings.Contains(stderr, "too many bad API keys") {
		t.Errorf("stderr = %q, want 429 and the server's message", stderr)
	}
	// The fake blocks for 1800 seconds per failure, and this is the second failure.
	if !strings.Contains(stderr, "3600s") {
		t.Errorf("stderr = %q, want the Retry-After seconds", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if f.requestsSeen() != 2 {
		t.Errorf("requests = %d, want the wrong-key POST and the blocked POST", f.requestsSeen())
	}
}

func TestServerSide413NamesTheSizeLimit(t *testing.T) {
	f := newFake(t, "test-key")
	f.setBodyLimit(8) // smaller than the client's own cap, so the server gets to answer 413
	writeWorkDirFile(t, "index.html", strings.Repeat("x", 20))

	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against an oversized body succeeded, want a 413")
	}
	if !strings.Contains(stderr, "413") || !strings.Contains(stderr, "1 MiB") {
		t.Errorf("stderr = %q, want 413 and the 1 MiB limit", stderr)
	}
	if f.putsSeen() != 1 {
		t.Errorf("PUTs = %d, want exactly 1", f.putsSeen())
	}
}

func TestMissingBinPasswordNamesTheEnvironmentVariable(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run with the bin password: %v", err)
	}
	hash := binHashOf(t, stdout)

	// The config file now says the bin is protected, and the password is gone.
	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run without the bin password succeeded, want a 401")
	}
	if !strings.Contains(stderr, "401") || !strings.Contains(stderr, "bin password required") {
		t.Errorf("stderr = %q, want 401 and the server's message", stderr)
	}
	if !strings.Contains(stderr, "SANDBIN_BIN_PASSWORD") {
		t.Errorf("stderr = %q, want the hint naming SANDBIN_BIN_PASSWORD", stderr)
	}
	if got := f.stored(hash + "/index.html"); got != "<h1>x</h1>" {
		t.Errorf("stored file after the 401 = %q, want the first upload intact", got)
	}
}

func TestProtectedStateWarnsBeforeTheRequest(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	if _, _, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("run with the bin password: %v", err)
	}

	// The fake notes "request" into the same log the client writes to, so the order
	// of the lines says whether the warning came first.
	log := &orderLog{}
	f.setLog(log)
	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	var out bytes.Buffer
	err := run([]string{"index.html"}, nil, &out, log)
	if err == nil {
		t.Fatalf("run without the bin password succeeded, want a 401")
	}

	lines := log.snapshot()
	if len(lines) < 2 {
		t.Fatalf("log = %q, want a warning and a request", lines)
	}
	if !strings.Contains(lines[0], "warning") || !strings.Contains(lines[0], "SANDBIN_BIN_PASSWORD") {
		t.Errorf("first log line = %q, want the warning naming SANDBIN_BIN_PASSWORD", lines[0])
	}
	if !strings.Contains(lines[1], "request") {
		t.Errorf("second log line = %q, want the request after the warning", lines[1])
	}
}

func TestStdinNeedsAName(t *testing.T) {
	f := newFake(t, "test-key")

	stdout, stderr, err := runClient(t, strings.NewReader("make: nothing to be done\n"))
	if err == nil {
		t.Fatalf("run with piped stdin and no --name succeeded, want an error")
	}
	if !strings.Contains(stderr, "--name") {
		t.Errorf("stderr = %q, want it to name --name", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

func TestNoArgumentsAndNoStdinIsAnError(t *testing.T) {
	f := newFake(t, "test-key")
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	stdout, stderr, err := runClient(t, devNull)
	if err == nil {
		t.Fatalf("run with no files and no stdin succeeded, want an error")
	}
	if !strings.Contains(stderr, "no files to upload") {
		t.Errorf("stderr = %q, want it to say there is nothing to upload", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

func TestNamedStdinNeedsAPipe(t *testing.T) {
	f := newFake(t, "test-key")
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	stdout, stderr, err := runClient(t, devNull, "--name", "build.ansi")
	if err == nil {
		t.Fatalf("run with --name and no stdin piped in succeeded, want an error")
	}
	if !strings.Contains(stderr, "got no stdin") {
		t.Errorf("stderr = %q, want it to say nothing was piped in", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}

	// A real pipe carrying no bytes is still a deliberate empty upload.
	stdout, stderr, err = runClient(t, strings.NewReader(""), "--name", "empty.txt")
	if err != nil {
		t.Fatalf("run with an empty pipe: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(stdout, "created ") {
		t.Errorf("stdout = %q, want the created line for the empty upload", stdout)
	}
}

func TestUploadsStdinAsANamedPath(t *testing.T) {
	f := newFake(t, "test-key")

	stdout, stderr, err := runClient(t, strings.NewReader("\x1b[1mmake\x1b[0m ok\n"), "--name", "build.ansi")
	if err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	if !strings.HasPrefix(stdout, "created "+os.Getenv("SANDBIN_URL")+"/"+hash+"/build.ansi\n") {
		t.Errorf("stdout = %q, want a created line for build.ansi", stdout)
	}
	if got := f.stored(hash + "/build.ansi"); got != "\x1b[1mmake\x1b[0m ok\n" {
		t.Errorf("stored build.ansi = %q, want the piped bytes", got)
	}

	stdout, _, err = runClient(t, strings.NewReader("second run\n"), "--name", "build.ansi")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.HasPrefix(stdout, "updated "+os.Getenv("SANDBIN_URL")+"/"+hash+"/build.ansi\n") {
		t.Errorf("second stdout = %q, want an updated line", stdout)
	}
}

func TestRefusesBadNamesLocallyWithNoRequest(t *testing.T) {
	f := newFake(t, "test-key")

	for _, bad := range []struct {
		name string
		want string
	}{
		{"/etc/passwd", "path must be relative"},
		{"../secret", "path must not escape the bin"},
		{"a/../../secret", "path must not escape the bin"},
		{"a\\b", "path may not contain backslashes"},
		{strings.Repeat("x", maxPathLen+1), "path too long"},
		{strings.Repeat("a/", maxPathSegs) + "x", "path has too many segments"},
		{"", "empty path"},
		{".", "empty path"},
		{"bad\nname", "path may not contain control characters"},
	} {
		stdout, stderr, err := runClient(t, nil, bad.name)
		if err == nil {
			t.Errorf("run with %q succeeded, want a local refusal", bad.name)
			continue
		}
		if !strings.Contains(stderr, bad.want) {
			t.Errorf("stderr for %q = %q, want %q", bad.name, stderr, bad.want)
		}
		if stdout != "" {
			t.Errorf("stdout for %q = %q, want nothing", bad.name, stdout)
		}
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none: a bad name is refused locally", f.requestsSeen())
	}
}

func TestLengthLimitCountsTheUploadedName(t *testing.T) {
	f := newFake(t, "test-key")
	// 255 bytes, storable on disk, but 257 raw characters once "./" is added: only
	// the cleaned name, which is what the server sees, may touch the 256 limit.
	name := strings.Repeat("l", 251) + ".txt"
	writeWorkDirFile(t, name, "at the limit")

	stdout, stderr, err := runClient(t, nil, "./"+name)
	if err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	if got := f.stored(hash + "/" + name); got != "at the limit" {
		t.Errorf("stored %q, want the file's bytes", got)
	}

	// One character past the limit is refused locally, measured the way the server measures it.
	_, stderr, err = runClient(t, nil, "./"+strings.Repeat("x", maxPathLen+1))
	if err == nil {
		t.Fatalf("run with a %d character name succeeded, want a local refusal", maxPathLen+1)
	}
	if !strings.Contains(stderr, "path too long") {
		t.Errorf("stderr = %q, want it to name the length limit", stderr)
	}
}

func TestRefusesALargeFileLocallyWithNoRequest(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "huge.txt", strings.Repeat("x", 2*maxBodyBytes))

	_, stderr, err := runClient(t, nil, "huge.txt")
	if err == nil {
		t.Fatalf("run with a 2 MiB file succeeded, want a local refusal")
	}
	if !strings.Contains(stderr, "1 MiB") {
		t.Errorf("stderr = %q, want it to name the 1 MiB limit", stderr)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestAcceptsAFileAtTheSizeLimit(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "exact.txt", strings.Repeat("x", maxBodyBytes))

	if _, stderr, err := runClient(t, nil, "exact.txt"); err != nil {
		t.Fatalf("run at the 1 MiB limit: %v; stderr %s", err, stderr)
	}
	if len(f.stored("bin0001/exact.txt")) != maxBodyBytes {
		t.Errorf("stored size = %d, want %d", len(f.stored("bin0001/exact.txt")), maxBodyBytes)
	}
}

func TestRefusesOversizedStdinLocallyWithNoRequest(t *testing.T) {
	f := newFake(t, "test-key")

	_, stderr, err := runClient(t, strings.NewReader(strings.Repeat("x", 2*maxBodyBytes)), "--name", "stream.txt")
	if err == nil {
		t.Fatalf("run with 2 MiB of stdin succeeded, want a local refusal")
	}
	if !strings.Contains(stderr, "1 MiB") {
		t.Errorf("stderr = %q, want it to name the 1 MiB limit", stderr)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestCorruptConfigFileIsAnErrorNamingIt(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, configFileName, "not: [valid")

	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against a corrupt config file succeeded, want an error")
	}
	if !strings.Contains(stderr, configFileName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, configFileName)
	}
	// The point of the error: no silent second bin for one prototype.
	if f.binCount() != 0 || f.requestsSeen() != 0 {
		t.Errorf("bins = %d, requests = %d; want a run that made no request", f.binCount(), f.requestsSeen())
	}
}

func TestConfigFileWithoutABinGetsAFreshBin(t *testing.T) {
	// A file like the one init writes — a url, no bin yet — is a legal file, not
	// a corrupt one: the run makes the bin and writes it back.
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+os.Getenv("SANDBIN_URL")+"\n")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run against a bin-less config file: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the one fresh bin", f.binCount())
	}
	cfgRaw, _ := readConfigFile(t)
	var cfg clientConfig
	if err := yaml.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	if cfg.Bin != hash {
		t.Errorf("%s =\n%s\nwant the file rewritten with the bin %s", configFileName, cfgRaw, hash)
	}
}

func TestConfigFileForAnotherUrlStartsAFreshBin(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, configFileName, "sandbin_url: http://other.invalid\nbin: ffffffff\nprotected: false\n")

	stdout, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	hash := binHashOf(t, stdout)
	if hash == "ffffffff" {
		t.Fatalf("the client uploaded into another server's bin, want a new bin")
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want 1 newly created bin", f.binCount())
	}
	cfgRaw, _ := readConfigFile(t)
	if !strings.Contains(string(cfgRaw), os.Getenv("SANDBIN_URL")) {
		t.Errorf("config file =\n%s\nwant it rewritten to the url in use", cfgRaw)
	}
}

// TestURLPrecedenceFlagEnvFileDefault walks the whole url ladder: the flag beats the
// env, the env beats the file, the file beats the default.
func TestURLPrecedenceFlagEnvFileDefault(t *testing.T) {
	f1 := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	f2, url2 := newFakeAt(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	// file > default: with no env the file's url is the one in use.
	t.Setenv("SANDBIN_URL", "")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\n")
	if _, stderr, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("file-url run: %v; stderr %s", err, stderr)
	}
	if f1.requestsSeen() != 2 || f2.requestsSeen() != 0 {
		t.Errorf("requests = f1 %d, f2 %d; want the file's server used and the other idle", f1.requestsSeen(), f2.requestsSeen())
	}

	// env > file: the env points at the other server, and the file follows it.
	t.Setenv("SANDBIN_URL", url2)
	if _, stderr, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("env-url run: %v; stderr %s", err, stderr)
	}
	if f1.requestsSeen() != 2 || f2.requestsSeen() != 2 {
		t.Errorf("requests = f1 %d, f2 %d; want the env's server used and the file's idle", f1.requestsSeen(), f2.requestsSeen())
	}

	// flag > env: the flag points back at the first server.
	if _, stderr, err := runClient(t, nil, "--url", url1, "index.html"); err != nil {
		t.Fatalf("flag-url run: %v; stderr %s", err, stderr)
	}
	if f1.requestsSeen() != 4 || f2.requestsSeen() != 2 {
		t.Errorf("requests = f1 %d, f2 %d; want the flag's server used", f1.requestsSeen(), f2.requestsSeen())
	}

	// default: no env, no file, no flag — the refusal names the default url.
	t.Setenv("SANDBIN_URL", "")
	if err := os.Remove(configFileName); err != nil {
		t.Fatalf("remove %s: %v", configFileName, err)
	}
	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against the default url succeeded, want an error")
	}
	if !strings.Contains(stderr, defaultURL) {
		t.Errorf("stderr = %q, want it to name %s", stderr, defaultURL)
	}
	if f1.requestsSeen() != 4 || f2.requestsSeen() != 2 {
		t.Errorf("requests = f1 %d, f2 %d; want neither fake server touched", f1.requestsSeen(), f2.requestsSeen())
	}
}

// TestAPIKeyPrecedenceFlagEnvFile walks the key ladder: the flag beats the env, the
// env beats the file, and a file key alone is enough to run.
func TestAPIKeyPrecedenceFlagEnvFile(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	// file alone: with no env the file's key is the one sent.
	t.Setenv("SANDBIN_API_KEY", "")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\napi_key: test-key\n")
	if _, stderr, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("file-key run: %v; stderr %s", err, stderr)
	}
	if f.requestsSeen() != 2 {
		t.Errorf("requests = %d, want the file key accepted", f.requestsSeen())
	}

	// env > file: the file holds a wrong key, the env the right one.
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\napi_key: wrong-file-key\n")
	t.Setenv("SANDBIN_API_KEY", "test-key")
	if _, stderr, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("env-key run: %v; stderr %s", err, stderr)
	}
	if f.requestsSeen() != 4 {
		t.Errorf("requests = %d, want the env key accepted over the file key", f.requestsSeen())
	}

	// flag > env: the env holds a wrong key, the flag the right one.
	t.Setenv("SANDBIN_API_KEY", "wrong-env-key")
	if _, stderr, err := runClient(t, nil, "--api-key", "test-key", "index.html"); err != nil {
		t.Fatalf("flag-key run: %v; stderr %s", err, stderr)
	}
	// One PUT only: the flag key is accepted and the file's bin is the one reused.
	if f.requestsSeen() != 5 || f.binCount() != 2 {
		t.Errorf("requests = %d, bins = %d; want the flag key accepted over the env key reusing the file's bin", f.requestsSeen(), f.binCount())
	}
}

// TestEnvUrlOverrideDoesNotReuseTheConfigBin is the URL-match rule for real: a bin
// made for one server is never carried to another, and the new server gets its own bin.
func TestEnvUrlOverrideDoesNotReuseTheConfigBin(t *testing.T) {
	f1 := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	first, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !strings.Contains(first, url1) {
		t.Fatalf("first stdout =\n%s\nwant it under %s", first, url1)
	}

	f2, url2 := newFakeAt(t, "test-key")
	t.Setenv("SANDBIN_URL", url2)
	second, stderr, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run on the other url: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(second, url2) || strings.Contains(second, url1) {
		t.Fatalf("second stdout =\n%s\nwant it under %s alone, never the file's old server", second, url2)
	}
	hash2 := binHashOf(t, second)
	if f1.requestsSeen() != 2 {
		t.Errorf("requests at the file's server = %d, want the override run to leave it alone", f1.requestsSeen())
	}
	if f2.binCount() != 1 {
		t.Errorf("bins at the override server = %d, want one fresh bin", f2.binCount())
	}

	// Back under the file's url the new bin is reused as the file's own.
	t.Setenv("SANDBIN_URL", "")
	third, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run back on the file's url: %v", err)
	}
	if want := "updated " + url2 + "/" + hash2 + "/index.html\n"; !strings.HasPrefix(third, want) {
		t.Errorf("stdout =\n%s\nwant %s", third, want)
	}
	if f2.binCount() != 1 {
		t.Errorf("bins = %d, want the file's bin reused", f2.binCount())
	}
}

// TestNewRunKeepsTheConfigFileAPIKey: --new replaces url/bin/protected, and the key
// a previous init left in the file survives untouched.
func TestNewRunKeepsTheConfigFileAPIKey(t *testing.T) {
	f := newFake(t, "test-key")
	url1 := os.Getenv("SANDBIN_URL")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+url1+"\napi_key: test-key\n")
	t.Setenv("SANDBIN_API_KEY", "") // the key must come from the file for the run to work at all

	first, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	hash1 := binHashOf(t, first)

	second, _, err := runClient(t, nil, "--new", "index.html")
	if err != nil {
		t.Fatalf("run with --new: %v", err)
	}
	hash2 := binHashOf(t, second)
	if hash2 == hash1 {
		t.Fatalf("--new reused bin %s", hash2)
	}
	if f.binCount() != 2 {
		t.Errorf("bins = %d, want the first bin and the --new bin", f.binCount())
	}

	cfgRaw, _ := readConfigFile(t)
	var cfg clientConfig
	if err := yaml.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	if cfg.APIKey != "test-key" || cfg.Bin != hash2 || cfg.URL != url1 {
		t.Errorf("%s =\n%s\nwant the new bin, the same url and the file's api_key kept", configFileName, cfgRaw)
	}
}

// TestUploadRunNeverStoresAnEnvOrFlagKey is the api_key persistence rule: an ordinary
// upload run writes url/bin/protected and leaves the file's api_key empty.
func TestUploadRunNeverStoresAnEnvOrFlagKey(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	// The env key is spent on the request, never saved.
	if _, stderr, err := runClient(t, nil, "index.html"); err != nil {
		t.Fatalf("env-key run: %v; stderr %s", err, stderr)
	}
	var cfg clientConfig
	cfgRaw, _ := readConfigFile(t)
	if err := yaml.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	if cfg.APIKey != "" {
		t.Errorf("%s =\n%s\nwant no api_key written from the env", configFileName, cfgRaw)
	}

	// The same for a flag key, with the env empty and the file holding none.
	t.Setenv("SANDBIN_API_KEY", "")
	if _, stderr, err := runClient(t, nil, "--api-key", "test-key", "index.html"); err != nil {
		t.Fatalf("flag-key run: %v; stderr %s", err, stderr)
	}
	cfgRaw, _ = readConfigFile(t)
	if err := yaml.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	if cfg.APIKey != "" {
		t.Errorf("%s =\n%s\nwant the api_key still empty after a flag-key run", configFileName, cfgRaw)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the file's bin reused for the flag-key run", f.binCount())
	}
}

func TestBinAndUrlFlagsNeedNoConfigFile(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, stderr, err := runClient(t, nil, "--bin", hash, "index.html")
	if err != nil {
		t.Fatalf("run with --bin: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(stdout, "/"+hash+"/index.html") || !strings.HasSuffix(stdout, "/"+hash+"/\n") {
		t.Errorf("stdout =\n%s\nwant the named bin's URLs", stdout)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want only the named bin", f.binCount())
	}
	if _, err := os.Stat(configFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after a --bin run, want no config file", configFileName)
	}

	// The "/abc1234/" shape a created bin was printed as is accepted too.
	stdout, _, err = runClient(t, nil, "--bin", "/"+hash+"/", "index.html")
	if err != nil {
		t.Fatalf("run with --bin /hash/: %v", err)
	}
	if !strings.Contains(stdout, "updated "+os.Getenv("SANDBIN_URL")+"/"+hash+"/index.html") {
		t.Errorf("stdout =\n%s\nwant an updated line in the same bin", stdout)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want no extra bin", f.binCount())
	}
}

func TestConfigFileIsPrivateAndHoldsNoBinPassword(t *testing.T) {
	const apiKey = "sekret-api-key"
	const binPassword = "sekret-bin-password"
	newFake(t, apiKey)
	t.Setenv("SANDBIN_BIN_PASSWORD", binPassword)
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	hash := binHashOf(t, stdout)

	raw, info := readConfigFile(t)
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("%s mode = %o, want 0600", configFileName, perm)
	}
	if text := string(raw); strings.Contains(text, binPassword) || strings.Contains(text, apiKey) {
		t.Errorf("%s = %q, want neither secret in it: an upload run never stores a key", configFileName, text)
	}

	var cfg clientConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse %s: %v", configFileName, err)
	}
	if cfg.URL != os.Getenv("SANDBIN_URL") || cfg.Bin != hash || !cfg.Protected || cfg.APIKey != "" {
		t.Errorf("%s = %+v, want the url, the bin and protected true with no api_key", configFileName, cfg)
	}
}

func TestFailureAfterASuccessfulUploadPrintsNothingOnStdout(t *testing.T) {
	f := newFake(t, "test-key")
	f.setBodyLimit(8)
	writeWorkDirFile(t, "small.txt", "fits")
	writeWorkDirFile(t, "big.txt", strings.Repeat("x", 20))

	stdout, _, err := runClient(t, nil, "small.txt", "big.txt")
	if err == nil {
		t.Fatalf("run with a refused second file succeeded, want an error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when the run failed", stdout)
	}
	if f.putsSeen() != 2 {
		t.Errorf("PUTs = %d, want the accepted PUT and the refused PUT", f.putsSeen())
	}
}

func TestRefusedServerGivesAReadableError(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	// A url with nothing behind it: the client must answer, not hang.
	stdout, stderr, err := runClient(t, nil, "--url", "http://127.0.0.1:1", "index.html")
	if err == nil {
		t.Fatalf("run against a dead url succeeded, want an error")
	}
	if !strings.Contains(stderr, "http://127.0.0.1:1") {
		t.Errorf("stderr = %q, want it to name the url it tried", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

func TestRejectsAnInvalidBaseUrlBeforeAnyRequest(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	for _, bad := range []string{"not-a-url", "ftp://localhost:8080", "http://localhost:8080/?next=1", "http://localhost:8080/#top"} {
		_, stderr, err := runClient(t, nil, "--url", bad, "index.html")
		if err == nil {
			t.Errorf("--url %q succeeded, want a startup error", bad)
			continue
		}
		if !strings.Contains(stderr, bad) {
			t.Errorf("stderr for --url %q = %q, want it to name the url", bad, stderr)
		}
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestOddFileNamesReachTheServerAsAPath(t *testing.T) {
	f := newFake(t, "test-key")

	for _, name := range []string{"a b#c.js", "quote's.md", "percent%20.md", "ünïcödé.md", "sub dir/page 1.html", "./-dash.md"} {
		writeWorkDirFile(t, name, "body of "+name)
		stdout, stderr, err := runClient(t, nil, name)
		if err != nil {
			t.Errorf("run with %q: %v; stderr %s", name, err, stderr)
			continue
		}
		hash := binHashOf(t, stdout)
		stored := strings.TrimPrefix(name, "./")
		if got := f.stored(hash + "/" + stored); got != "body of "+name {
			t.Errorf("stored key for %q = %q, want the name as a path with no query or fragment", name, got)
		}
	}
}

func TestSubdirectoryNamesAreUploadedAsGiven(t *testing.T) {
	f := newFake(t, "test-key")
	if err := os.MkdirAll("prototype", 0755); err != nil {
		t.Fatalf("mkdir prototype: %v", err)
	}
	writeWorkDirFile(t, "prototype/app.js", "console.log(1)\n")

	stdout, stderr, err := runClient(t, nil, "prototype/app.js", "./prototype/app.js")
	if err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	base := os.Getenv("SANDBIN_URL") + "/" + hash
	want := "created " + base + "/prototype/app.js\n" +
		"updated " + base + "/prototype/app.js\n" +
		base + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if got := f.stored(hash + "/prototype/app.js"); got != "console.log(1)\n" {
		t.Errorf("stored prototype/app.js = %q, want the file's bytes", got)
	}
}

func TestBinAndNewTogetherAreRefused(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	_, stderr, err := runClient(t, nil, "--bin", "bin0001", "--new", "index.html")
	if err == nil {
		t.Fatalf("run with both --bin and --new succeeded, want an error")
	}
	if !strings.Contains(stderr, "--bin or --new") {
		t.Errorf("stderr = %q, want it to name the two flags it cannot combine", stderr)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestRejectsADotAsABinHash(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	for _, bad := range []string{".", "..", "./.."} {
		stdout, _, err := runClient(t, nil, "--bin", bad, "index.html")
		if err == nil {
			t.Errorf("run with --bin %q succeeded, want a refusal", bad)
			continue
		}
		if stdout != "" {
			t.Errorf("stdout for --bin %q = %q, want nothing", bad, stdout)
		}
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none: a dot hash never reaches the server", f.requestsSeen())
	}
}

func TestConfigFileWhoseBinNoLongerExistsReports404(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, configFileName, "sandbin_url: "+os.Getenv("SANDBIN_URL")+"\nbin: ffffffff\nprotected: false\n")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against a vanished bin succeeded, want a 404")
	}
	if !strings.Contains(stderr, "404") || !strings.Contains(stderr, "unknown bin") {
		t.Errorf("stderr = %q, want 404 and the server's message", stderr)
	}
	// The config file is honoured rather than quietly replaced by a new bin.
	if f.binCount() != 0 {
		t.Errorf("bins = %d, want no bin created behind the user's back", f.binCount())
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestRejectsACreateBinBodyThatIsNotABinPath(t *testing.T) {
	f := newFake(t, "test-key")
	f.setCreateBody("4f8a2b1c") // the slashes are missing, so this is not a bin path
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run accepted a create-bin body that is not a bin path, want an error")
	}
	if !strings.Contains(stderr, "4f8a2b1c") {
		t.Errorf("stderr = %q, want it to quote the body it refused", stderr)
	}
	if f.putsSeen() != 0 {
		t.Errorf("PUTs = %d, want none after refusing the body", f.putsSeen())
	}
	if _, err := os.Stat(configFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after the refusal, want no config file", configFileName)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestUnreadableConfigFileIsAnErrorNamingIt(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	// A present file the client cannot read at all.
	if err := os.Mkdir(configFileName, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", configFileName, err)
	}

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against an unreadable config file succeeded, want an error")
	}
	if !strings.Contains(stderr, configFileName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, configFileName)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

func TestConfigWriteFailureNamesTheFile(t *testing.T) {
	// The run reports a config write failure instead of pretending the bin was
	// saved. A missing directory is the failure a root-run test can force.
	newFake(t, "test-key")
	dir := filepath.Join(t.TempDir(), "gone")
	err := writeConfig(dir, clientConfig{URL: "http://127.0.0.1:1", Bin: "abc1234"})
	if err == nil {
		t.Fatalf("writeConfig into %s succeeded, want an error", dir)
	}
	if !strings.Contains(err.Error(), configFileName) {
		t.Errorf("writeConfig error = %q, want it to name %s", err, configFileName)
	}
}

func TestConfigWriteFailureNamesTheCreatedBin(t *testing.T) {
	// The bin exists on the server the moment createBin succeeds, so losing the
	// config file must still tell the agent which bin was created.
	f := newFake(t, "test-key")
	c, err := newClient(os.Getenv("SANDBIN_URL"), "test-key", "")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	_, _, _, err = resolveBin(c, filepath.Join(t.TempDir(), "gone"), clientConfig{}, false, "", false, "", "")
	if err == nil {
		t.Fatalf("resolveBin into a missing directory succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "bin bin0001") {
		t.Errorf("error = %q, want it to name the created bin", err)
	}
	if !strings.Contains(err.Error(), configFileName) {
		t.Errorf("error = %q, want it to name %s", err, configFileName)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the one bin resolveBin created", f.binCount())
	}
}

// TestMetadataOnlyRunUpdatesAnExistingBin is a run that uploads nothing and says what the
// bin holds: one PATCH, one line on stdout, and --describe reads it back.
func TestMetadataOnlyRunUpdatesAnExistingBin(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")

	stdout, stderr, err := runClient(t, nil, "--bin", hash, "--title", "Audit notes", "--description", "findings of the audit")
	if err != nil {
		t.Fatalf("metadata-only run: %v; stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing on a successful run", stderr)
	}
	base := os.Getenv("SANDBIN_URL")
	want := "updated /" + hash + "/\n" + base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if title, description := f.binMeta(hash); title != "Audit notes" || description != "findings of the audit" {
		t.Errorf("stored metadata = %q / %q, want the flags' values", title, description)
	}
	if f.requestsSeen() != 1 || f.patchesSeen() != 1 {
		t.Errorf("requests = %d (PATCHes %d), want exactly one PATCH", f.requestsSeen(), f.patchesSeen())
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want only the named bin", f.binCount())
	}
	if _, err := os.Stat(configFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after a --bin run, want no config file", configFileName)
	}

	// The same run twice is the same PATCH: a metadata flag is not an upload.
	if _, _, err := runClient(t, nil, "--bin", hash, "--description", "findings of the audit, again"); err != nil {
		t.Fatalf("second metadata-only run: %v", err)
	}
	if title, description := f.binMeta(hash); title != "Audit notes" || description != "findings of the audit, again" {
		t.Errorf("stored metadata = %q / %q, want the title kept and the description replaced", title, description)
	}

	// Reading it back is --describe's job.
	stdout, stderr, err = runClient(t, nil, "--describe", "--bin", hash)
	if err != nil {
		t.Fatalf("--describe: %v; stderr %s", err, stderr)
	}
	if want := "title: Audit notes\ndescription: findings of the audit, again\n"; stdout != want {
		t.Errorf("--describe stdout =\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("--describe stderr = %q, want nothing", stderr)
	}
	if f.requestsSeen() != 3 || f.patchesSeen() != 2 {
		t.Errorf("requests = %d (PATCHes %d), want two PATCHes and the one GET", f.requestsSeen(), f.patchesSeen())
	}
}

// TestMetadataFlagClearsAField is the empty value: --description "" clears that field and
// says nothing about the other one.
func TestMetadataFlagClearsAField(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")

	if _, _, err := runClient(t, nil, "--bin", hash, "--title", "kept title", "--description", "cleared description"); err != nil {
		t.Fatalf("run setting both fields: %v", err)
	}
	if _, stderr, err := runClient(t, nil, "--bin", hash, "--description", ""); err != nil {
		t.Fatalf("run clearing the description: %v; stderr %s", err, stderr)
	}
	if title, description := f.binMeta(hash); title != "kept title" || description != "" {
		t.Errorf("stored metadata = %q / %q, want the title kept and the description cleared", title, description)
	}
	if f.patchesSeen() != 2 {
		t.Errorf("PATCHes = %d, want one per metadata run", f.patchesSeen())
	}

	stdout, _, err := runClient(t, nil, "--describe", "--bin", hash)
	if err != nil {
		t.Fatalf("--describe after the clear: %v", err)
	}
	if stdout != "title: kept title\ndescription: \n" {
		t.Errorf("--describe stdout = %q, want the cleared field printed as empty", stdout)
	}
}

// TestCreatingABinCarriesMetadataInItsPOST is the no-second-request rule: a bin made here
// takes its metadata in the creation POST and never sees a PATCH.
func TestCreatingABinCarriesMetadataInItsPOST(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, stderr, err := runClient(t, nil, "--title", "Prototype", "--description", "the JS prototype", "index.html")
	if err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	if title, description := f.binMeta(hash); title != "Prototype" || description != "the JS prototype" {
		t.Errorf("stored metadata = %q / %q, want the creation headers", title, description)
	}
	if f.requestsSeen() != 2 || f.putsSeen() != 1 || f.patchesSeen() != 0 {
		t.Errorf("requests = %d (PUTs %d, PATCHes %d), want one POST and one PUT and no PATCH",
			f.requestsSeen(), f.putsSeen(), f.patchesSeen())
	}
	base := os.Getenv("SANDBIN_URL")
	want := "created " + base + "/" + hash + "/index.html\n" + base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// TestMetadataOnlyRunOnAFreshDirectoryCreatesOneBin is the metadata-only run with no bin to
// reuse: the created bin already carries the metadata, so the run makes one request.
func TestMetadataOnlyRunOnAFreshDirectoryCreatesOneBin(t *testing.T) {
	f := newFake(t, "test-key")

	stdout, stderr, err := runClient(t, nil, "--title", "Notes only", "--description", "no files yet")
	if err != nil {
		t.Fatalf("metadata-only run: %v; stderr %s", err, stderr)
	}
	if f.requestsSeen() != 1 || f.patchesSeen() != 0 {
		t.Errorf("requests = %d (PATCHes %d), want the creation POST alone", f.requestsSeen(), f.patchesSeen())
	}
	if title, description := f.binMeta("bin0001"); title != "Notes only" || description != "no files yet" {
		t.Errorf("stored metadata = %q / %q, want the creation headers", title, description)
	}
	if strings.Contains(stdout, "updated /") {
		t.Errorf("stdout =\n%s\nwant no updated line for a bin this run made", stdout)
	}
	if !strings.HasSuffix(stdout, os.Getenv("SANDBIN_URL")+"/bin0001/\n") {
		t.Errorf("stdout =\n%s\nwant the bin URL last", stdout)
	}

	cfgRaw, _ := readConfigFile(t)
	if !strings.Contains(string(cfgRaw), "bin0001") {
		t.Errorf("config file =\n%s\nwant it to remember the new bin", cfgRaw)
	}
}

// TestMetadataPATCHComesBeforeTheUploads is the ordering the decisions settle on: a reused
// bin is described first, so the line order says so too.
func TestMetadataPATCHComesBeforeTheUploads(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>one</h1>")

	first, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	hash := binHashOf(t, first)

	log := &orderLog{}
	f.setLog(log)
	stdout, stderr, err := runClient(t, nil, "--title", "Audit notes", "index.html")
	if err != nil {
		t.Fatalf("run with metadata: %v; stderr %s", err, stderr)
	}
	base := os.Getenv("SANDBIN_URL")
	want := "updated /" + hash + "/\n" +
		"updated " + base + "/" + hash + "/index.html\n" +
		base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}

	lines := log.snapshot()
	if len(lines) != 2 {
		t.Fatalf("server log = %q, want one PATCH and one PUT", lines)
	}
	if !strings.Contains(lines[0], "PATCH") || !strings.Contains(lines[1], "PUT") {
		t.Errorf("server log = %q, want the PATCH before the PUT", lines)
	}
	if f.patchesSeen() != 1 {
		t.Errorf("PATCHes = %d, want exactly one", f.patchesSeen())
	}
}

// TestFailedUploadStillLeavesTheBinDescribed is why the PATCH comes first: the run failed
// and printed nothing, but the bin says what it was meant to hold.
func TestFailedUploadStillLeavesTheBinDescribed(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>one</h1>")

	first, _, err := runClient(t, nil, "index.html")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	hash := binHashOf(t, first)

	f.setBodyLimit(8)
	writeWorkDirFile(t, "index.html", strings.Repeat("x", 20))
	stdout, _, err := runClient(t, nil, "--description", "what this bin was for", "index.html")
	if err == nil {
		t.Fatalf("run against an oversized body succeeded, want a 413")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when the run failed", stdout)
	}
	if _, description := f.binMeta(hash); description != "what this bin was for" {
		t.Errorf("stored description = %q, want the PATCH that ran before the failed upload", description)
	}
	if f.patchesSeen() != 1 {
		t.Errorf("PATCHes = %d, want exactly one", f.patchesSeen())
	}
}

// TestRefusesBadMetadataLocallyWithNoRequest mirrors the server's caps client-side, with the
// server's own wording, so a bad flag costs nothing.
func TestRefusesBadMetadataLocallyWithNoRequest(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	for _, bad := range []struct {
		flag, value, want string
	}{
		{"--title", strings.Repeat("t", maxTitleBytes+1), "title over 200 bytes"},
		// 101 two-byte characters are 202 bytes: the cap counts bytes, not characters.
		{"--title", strings.Repeat("é", maxTitleBytes/2+1), "title over 200 bytes"},
		{"--description", strings.Repeat("d", maxDescriptionBytes+1), "description over 2000 bytes"},
		{"--title", "a\nb", "title may not contain control characters"},
		{"--title", "a\tb", "title may not contain control characters"},
		{"--description", "a\x7fb", "description may not contain control characters"},
	} {
		stdout, stderr, err := runClient(t, nil, bad.flag, bad.value, "--bin", hash, "index.html")
		if err == nil {
			t.Errorf("run with %s %q succeeded, want a local refusal", bad.flag, bad.value)
			continue
		}
		if !strings.Contains(stderr, bad.want) {
			t.Errorf("stderr for %s %q = %q, want %q", bad.flag, bad.value, stderr, bad.want)
		}
		if stdout != "" {
			t.Errorf("stdout for %s %q = %q, want nothing", bad.flag, bad.value, stdout)
		}
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none: a bad metadata flag is refused locally", f.requestsSeen())
	}

	// Exactly at the cap is accepted, by the same measure.
	if _, stderr, err := runClient(t, nil, "--bin", hash,
		"--title", strings.Repeat("t", maxTitleBytes), "--description", strings.Repeat("d", maxDescriptionBytes)); err != nil {
		t.Fatalf("run at the caps: %v; stderr %s", err, stderr)
	}
	if title, description := f.binMeta(hash); len(title) != maxTitleBytes || len(description) != maxDescriptionBytes {
		t.Errorf("stored metadata = %d / %d bytes, want both at the cap", len(title), len(description))
	}
}

// TestDescribeNeedsNoAPIKey is the read-only rule: a run that writes nothing needs no key,
// reads the bin this directory's config file names, and makes exactly one request.
func TestDescribeNeedsNoAPIKey(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	stdout, _, err := runClient(t, nil, "--title", "Audit notes", "--description", "findings of the audit", "index.html")
	if err != nil {
		t.Fatalf("run with metadata: %v", err)
	}
	hash := binHashOf(t, stdout)
	requestsBefore := f.requestsSeen()

	t.Setenv("SANDBIN_API_KEY", "")
	stdout, stderr, err := runClient(t, nil, "--describe")
	if err != nil {
		t.Fatalf("--describe without an API key: %v; stderr %s", err, stderr)
	}
	if want := "title: Audit notes\ndescription: findings of the audit\n"; stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if f.requestsSeen() != requestsBefore+1 {
		t.Errorf("requests = %d, want the one GET this run made", f.requestsSeen())
	}

	// --bin reaches the same bin with no key and no config file either.
	if _, err := os.Stat(configFileName); err != nil {
		t.Fatalf("stat %s: %v", configFileName, err)
	}
	stdout, _, err = runClient(t, nil, "--describe", "--bin", "/"+hash+"/")
	if err != nil {
		t.Fatalf("--describe --bin: %v", err)
	}
	if want := "title: Audit notes\ndescription: findings of the audit\n"; stdout != want {
		t.Errorf("--describe --bin stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// TestDescribeHasNoBinToRead is the config file that does not match the url in use: a
// read-only run makes no bin behind the user's back.
func TestDescribeHasNoBinToRead(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, configFileName, "sandbin_url: http://other.invalid\nbin: ffffffff\nprotected: false\n")

	stdout, stderr, err := runClient(t, nil, "--describe")
	if err == nil {
		t.Fatalf("--describe against a non-matching config file succeeded, want an error")
	}
	if !strings.Contains(stderr, "--describe") {
		t.Errorf("stderr = %q, want it to name --describe", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 || f.binCount() != 0 {
		t.Errorf("stdout = %q, requests = %d, bins = %d; want none of them", stdout, f.requestsSeen(), f.binCount())
	}
}

// TestDescribeProtectedBinHonoursBinPassword keeps the bin password rule for the read-only
// run: without it the warning and the 401, with it the two lines.
func TestDescribeProtectedBinHonoursBinPassword(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("sesame")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")

	if _, stderr, err := runClient(t, nil, "--bin", hash, "--title", "Secret notes"); err != nil {
		t.Fatalf("run setting the title: %v; stderr %s", err, stderr)
	}

	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	stdout, stderr, err := runClient(t, nil, "--describe", "--bin", hash)
	if err == nil {
		t.Fatalf("--describe on a protected bin without the password succeeded, want a 401")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "401") || !strings.Contains(stderr, "bin password required") {
		t.Errorf("stderr = %q, want 401 and the server's message", stderr)
	}
	if !strings.Contains(stderr, "SANDBIN_BIN_PASSWORD") {
		t.Errorf("stderr = %q, want the hint naming SANDBIN_BIN_PASSWORD", stderr)
	}

	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	stdout, stderr, err = runClient(t, nil, "--describe", "--bin", hash)
	if err != nil {
		t.Fatalf("--describe with the bin password: %v; stderr %s", err, stderr)
	}
	if stdout != "title: Secret notes\ndescription: \n" {
		t.Errorf("stdout = %q, want the two lines", stdout)
	}
}

// TestDescribeRefusedWithWriteFlags keeps --describe to one job: it reads a bin, so the
// flags that make or write one have no place next to it.
func TestDescribeRefusedWithWriteFlags(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")

	for _, args := range [][]string{
		{"--describe", "--new"},
		{"--describe", "index.html"},
		{"--describe", "--name", "x.txt"},
		{"--describe", "--title", "t"},
		{"--describe", "--description", "d"},
		{"--describe", "--bin", "bin0001", "index.html"},
	} {
		stdout, stderr, err := runClient(t, nil, args...)
		if err == nil {
			t.Errorf("run %q succeeded, want a refusal", args)
			continue
		}
		if !strings.Contains(stderr, "--describe") {
			t.Errorf("stderr for %q = %q, want it to name --describe", args, stderr)
		}
		if stdout != "" {
			t.Errorf("stdout for %q = %q, want nothing", args, stdout)
		}
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

// TestRunWithNoTargetAndNoMetadataFlagIsStillRefused keeps the old refusal: metadata flags
// are what make a run with nothing to upload a run at all.
func TestRunWithNoTargetAndNoMetadataFlagIsStillRefused(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")

	stdout, stderr, err := runClient(t, nil, "--bin", hash)
	if err == nil {
		t.Fatalf("run with no file, no stdin and no metadata flag succeeded, want an error")
	}
	if !strings.Contains(stderr, "--name") {
		t.Errorf("stderr = %q, want the message naming --name", stderr)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

// orderLog records every line written to it, by the client and by the fake server,
// so a test can say plainly whether a warning came before a request.
type orderLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *orderLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

func (l *orderLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// seedFile puts a file in the fake's store without an upload, for the tests that name a
// bin with --bin instead of creating one.
func (f *fakeServer) seedFile(key, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = body
}

// TestDeleteFilePrintsDeletedLineAndKeepsTheBin is the single-file case: one DELETE, one
// line naming the file, the bin URL last, and the bin's other files untouched.
func TestDeleteFilePrintsDeletedLineAndKeepsTheBin(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "notes.md", "# Notes")
	writeWorkDirFile(t, "other.md", "# Other")

	stdout, stderr, err := runClient(t, nil, "notes.md", "other.md")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	base := os.Getenv("SANDBIN_URL")

	stdout, stderr, err = runClient(t, nil, "--delete", "notes.md")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing on a successful delete run", stderr)
	}
	want := "deleted " + base + "/" + hash + "/notes.md\n" + base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("delete stdout =\n%s\nwant\n%s", stdout, want)
	}
	if got := f.stored(hash + "/notes.md"); got != "" {
		t.Errorf("the server still holds notes.md = %q, want it gone", got)
	}
	if got := f.stored(hash + "/other.md"); got != "# Other" {
		t.Errorf("other.md = %q, want it untouched", got)
	}
	if f.deletesSeen() != 1 {
		t.Errorf("DELETEs = %d, want exactly one", f.deletesSeen())
	}
	if order := f.deleteOrder(); len(order) != 1 || order[0] != "/"+hash+"/notes.md" {
		t.Errorf("DELETE paths = %v, want one /%s/notes.md", order, hash)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the delete to leave the one bin", f.binCount())
	}
	if raw, _ := readConfigFile(t); !bytes.Contains(raw, []byte(hash)) {
		t.Errorf("the config file no longer names bin %s:\n%s", hash, raw)
	}
}

// TestDeleteKeepsTheOrderGiven is why the flags collect their values instead of being
// plain bools: repeats keep the order the run was written in.
func TestDeleteKeepsTheOrderGiven(t *testing.T) {
	f := newFake(t, "test-key")
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		writeWorkDirFile(t, name, "body of "+name)
	}
	stdout, stderr, err := runClient(t, nil, "a.md", "b.md", "c.md")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	base := os.Getenv("SANDBIN_URL")

	stdout, stderr, err = runClient(t, nil, "--delete", "c.md", "--delete", "a.md")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	want := "deleted " + base + "/" + hash + "/c.md\n" +
		"deleted " + base + "/" + hash + "/a.md\n" +
		base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("delete stdout =\n%s\nwant\n%s", stdout, want)
	}
	order := f.deleteOrder()
	if len(order) != 2 || order[0] != "/"+hash+"/c.md" || order[1] != "/"+hash+"/a.md" {
		t.Errorf("DELETE paths = %v, want c.md then a.md in the order the flags were given", order)
	}
	if got := f.stored(hash + "/b.md"); got != "body of b.md" {
		t.Errorf("b.md = %q, want it untouched", got)
	}
}

// TestDeleteFolderSendsTheSlashAndPrintsEveryPath is the folder form: one request naming
// the folder, one line per removed file, and a sibling whose name only looks alike left
// alone.
func TestDeleteFolderSendsTheSlashAndPrintsEveryPath(t *testing.T) {
	f := newFake(t, "test-key")
	seed := []string{"drafts/one.md", "drafts/deep/two.md", "draftsX/keep.md"}
	for _, name := range seed {
		writeWorkDirFile(t, name, "body of "+name)
	}
	stdout, stderr, err := runClient(t, nil, seed...)
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	base := os.Getenv("SANDBIN_URL")

	stdout, stderr, err = runClient(t, nil, "--delete-folder", "drafts")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	// The server answers the removed paths sorted, and the client echoes that order.
	want := "deleted " + base + "/" + hash + "/drafts/deep/two.md\n" +
		"deleted " + base + "/" + hash + "/drafts/one.md\n" +
		base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("delete stdout =\n%s\nwant\n%s", stdout, want)
	}
	order := f.deleteOrder()
	if len(order) != 1 || order[0] != "/"+hash+"/drafts/" {
		t.Errorf("DELETE paths = %v, want the one folder request /%s/drafts/", order, hash)
	}
	for _, key := range []string{"drafts/one.md", "drafts/deep/two.md"} {
		if got := f.stored(hash + "/" + key); got != "" {
			t.Errorf("%s = %q, want it gone with the folder", key, got)
		}
	}
	if got := f.stored(hash + "/draftsX/keep.md"); got != "body of draftsX/keep.md" {
		t.Errorf("draftsX/keep.md = %q, want it untouched by the folder delete", got)
	}
}

// TestDeleteUnknownPathNamesThePathAndMakesOneRequest is the no-retry rule on a delete: a
// path the bin does not hold is one request, an error naming that path, and nothing on
// stdout.
func TestDeleteUnknownPathNamesThePathAndMakesOneRequest(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "notes.md", "# Notes")
	stdout, stderr, err := runClient(t, nil, "notes.md")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)

	stdout, stderr, err = runClient(t, nil, "--delete", "missing.txt")
	if err == nil {
		t.Fatalf("delete of an unknown path returned no error; stdout %s", stdout)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing from a failed delete run", stdout)
	}
	if !strings.Contains(stderr, "missing.txt") || !strings.Contains(stderr, "404 unknown path") {
		t.Errorf("stderr = %q, want an error naming missing.txt and the server's 404 unknown path", stderr)
	}
	if f.deletesSeen() != 1 {
		t.Errorf("DELETEs = %d, want exactly one: the client never retries", f.deletesSeen())
	}
	if got := f.stored(hash + "/notes.md"); got != "# Notes" {
		t.Errorf("notes.md = %q, want the failed run to leave the bin as it was", got)
	}
}

// TestDeleteRefusesEveryUploadShape is the standalone rule: a run that deletes files does
// not upload any, and each refusal names the flag it refuses.
func TestDeleteRefusesEveryUploadShape(t *testing.T) {
	cases := []struct {
		what string
		args []string
		want string
	}{
		{"a file argument", []string{"--delete", "notes.md", "extra.md"}, "--delete uploads nothing: pass no file arguments"},
		{"piped stdin through --name", []string{"--delete", "notes.md", "--name", "piped.txt"}, "--delete uploads nothing: pass no --name"},
		{"a folder through --archive", []string{"--delete", "notes.md", "--archive", "hello"}, "--delete uploads nothing: pass no --archive"},
		{"a fresh bin through --new", []string{"--delete", "notes.md", "--new"}, "--delete reuses this directory's bin: --new makes a fresh one"},
		{"the read-only --describe", []string{"--delete", "notes.md", "--describe"}, "--describe only reads: --delete and --delete-folder write the bin"},
		{"a file argument for the folder flag", []string{"--delete-folder", "drafts", "extra.md"}, "--delete-folder uploads nothing: pass no file arguments"},
		{"--name for the folder flag", []string{"--delete-folder", "drafts", "--name", "piped.txt"}, "--delete-folder uploads nothing: pass no --name"},
		{"--new for the folder flag", []string{"--delete-folder", "drafts", "--new"}, "--new makes a fresh one"},
	}
	for _, tc := range cases {
		f := newFake(t, "test-key")
		writeWorkDirFile(t, "notes.md", "# Notes")

		stdout, stderr, err := runClient(t, nil, tc.args...)
		if err == nil {
			t.Errorf("%s: run %v returned no error; stdout %s", tc.what, tc.args, stdout)
			continue
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: stderr = %q, want it to carry %q", tc.what, stderr, tc.want)
		}
		if stdout != "" {
			t.Errorf("%s: stdout = %q, want nothing from a refused run", tc.what, stdout)
		}
		if f.requestsSeen() != 0 {
			t.Errorf("%s: requests = %d, want a refusal to make none", tc.what, f.requestsSeen())
		}
	}
}

// TestDeleteNamesAreCheckedBeforeAnyRequest reuses the local upload-name rules and the
// pointer from the file flag to the folder flag.
func TestDeleteNamesAreCheckedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		what string
		args []string
		want string
	}{
		{"a slash-terminated path", []string{"--delete", "hello/"}, `--delete "hello/" names a folder: use --delete-folder hello`},
		{"the folder flag with the slash it adds itself", []string{"--delete-folder", "hello/"}, `--delete-folder "hello/" needs no trailing slash`},
		{"an absolute path", []string{"--delete", "/etc/passwd"}, `--delete "/etc/passwd": path must be relative`},
		{"an escaping path", []string{"--delete", "../secret"}, `--delete "../secret": path must not escape the bin`},
		{"a control character", []string{"--delete", "bad\tname"}, "--delete \"bad\\tname\": path may not contain control characters"},
		{"an empty path", []string{"--delete", ""}, `--delete "": empty path`},
		{"an escaping folder", []string{"--delete-folder", ".."}, `--delete-folder "..": path must not escape the bin`},
		{"the current directory as a folder", []string{"--delete-folder", "."}, `--delete-folder "."`},
	}
	for _, tc := range cases {
		f := newFake(t, "test-key")
		_, stderr, err := runClient(t, nil, tc.args...)
		if err == nil {
			t.Errorf("%s: run %v returned no error", tc.what, tc.args)
			continue
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: stderr = %q, want it to carry %q", tc.what, stderr, tc.want)
		}
		if f.requestsSeen() != 0 {
			t.Errorf("%s: requests = %d, want a local refusal to make none", tc.what, f.requestsSeen())
		}
	}
}

// TestDeleteNeedsABinAndSaysWhereToFindIt is the read-the-same-bin rule: a delete run takes
// the config file or --bin, never creates a bin of its own.
func TestDeleteNeedsABinAndSaysWhereToFindIt(t *testing.T) {
	f := newFake(t, "test-key")

	stdout, stderr, err := runClient(t, nil, "--delete", "notes.md")
	if err == nil {
		t.Fatalf("a delete run with no bin returned no error; stdout %s", stdout)
	}
	if !strings.Contains(stderr, "--delete has no bin for this directory: pass --bin or upload something first") {
		t.Errorf("stderr = %q, want it to name --bin and an upload as the two ways to find a bin", stderr)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want a run with no bin to make none", f.requestsSeen())
	}

	// --bin names one, and the run does not write a config file or create a bin.
	hash := f.addBin("")
	f.seedFile(hash+"/notes.md", "# Notes")
	stdout, stderr, err = runClient(t, nil, "--bin", hash, "--delete", "notes.md")
	if err != nil {
		t.Fatalf("--bin delete run: %v; stderr %s", err, stderr)
	}
	want := "deleted " + os.Getenv("SANDBIN_URL") + "/" + hash + "/notes.md\n" + os.Getenv("SANDBIN_URL") + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want the named bin only", f.binCount())
	}
	if _, err := os.Stat(configFileName); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run wrote %s (%v), want a --bin run to leave no config file", configFileName, err)
	}
}

// TestDeleteNeedsTheAPIKeyLikeEveryWrite keeps the key guard in front of a delete run too.
func TestDeleteNeedsTheAPIKeyLikeEveryWrite(t *testing.T) {
	f := newFake(t, "test-key")
	hash := f.addBin("")
	f.seedFile(hash+"/notes.md", "# Notes")
	t.Setenv("SANDBIN_API_KEY", "")

	stdout, stderr, err := runClient(t, nil, "--bin", hash, "--delete", "notes.md")
	if err == nil {
		t.Fatalf("a delete run without a key returned no error; stdout %s", stdout)
	}
	if !strings.Contains(stderr, "no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init") {
		t.Errorf("stderr = %q, want the key refusal every write run gives", stderr)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want the missing key to make none", f.requestsSeen())
	}
}

// TestDeleteCarriesTheBinPasswordAsBasicAuth is the protected-bin rule on the client side,
// and the 401 that says the password is missing.
func TestDeleteCarriesTheBinPasswordAsBasicAuth(t *testing.T) {
	f := newFake(t, "test-key")
	t.Setenv("SANDBIN_BIN_PASSWORD", "sesame")
	writeWorkDirFile(t, "secret.txt", "protected body")

	stdout, stderr, err := runClient(t, nil, "secret.txt")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)

	stdout, stderr, err = runClient(t, nil, "--delete", "secret.txt")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(stdout, "/"+hash+"/secret.txt") {
		t.Errorf("stdout = %q, want the deleted file's URL", stdout)
	}
	if auths := f.deleteAuthNames(); len(auths) != 1 || auths[0] != hash {
		t.Errorf("DELETE Basic Auth user names = %v, want the bin hash %s", auths, hash)
	}
	if got := f.stored(hash + "/secret.txt"); got != "" {
		t.Errorf("secret.txt = %q, want it gone", got)
	}

	// Without the password the delete is the 401 that names the environment variable.
	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	stdout, stderr, err = runClient(t, nil, "--delete", "secret.txt")
	if err == nil {
		t.Fatalf("a delete run without the bin password returned no error; stdout %s", stdout)
	}
	if !strings.Contains(stderr, "bin password required") {
		t.Errorf("stderr = %q, want the server's 401 message", stderr)
	}
	if !strings.Contains(stderr, "set SANDBIN_BIN_PASSWORD to this bin's password") {
		t.Errorf("stderr = %q, want the hint naming SANDBIN_BIN_PASSWORD", stderr)
	}
	if got := f.stored(hash + "/secret.txt"); got != "" {
		t.Errorf("secret.txt = %q, want it gone from the earlier delete", got)
	}
}

// TestDeleteWithMetadataPatchesFirst is the combine rule: the metadata answer comes first,
// then the deleted lines, and the server saw the PATCH before the DELETE.
func TestDeleteWithMetadataPatchesFirst(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "notes.md", "# Notes")
	stdout, stderr, err := runClient(t, nil, "notes.md")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	base := os.Getenv("SANDBIN_URL")

	var logged bytes.Buffer
	f.setLog(&logged)
	stdout, stderr, err = runClient(t, nil, "--title", "New title", "--delete", "notes.md")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	want := "updated /" + hash + "/\n" +
		"deleted " + base + "/" + hash + "/notes.md\n" +
		base + "/" + hash + "/\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if got := logged.String(); got != "request PATCH\nrequest DELETE\n" {
		t.Errorf("the server saw %q, want the PATCH before the DELETE", got)
	}
	if title, _ := f.binMeta(hash); title != "New title" {
		t.Errorf("stored title = %q, want New title", title)
	}
}

// TestDeleteEmptiedBinStaysThisDirectoriesBin is the reuse rule after the bin is emptied:
// the config file still names it and --describe still reads it.
func TestDeleteEmptiedBinStaysThisDirectoriesBin(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "notes.md", "# Notes")
	stdout, stderr, err := runClient(t, nil, "--title", "Kept", "notes.md")
	if err != nil {
		t.Fatalf("upload run: %v; stderr %s", err, stderr)
	}
	hash := binHashOf(t, stdout)
	if raw, _ := readConfigFile(t); !bytes.Contains(raw, []byte(hash)) {
		t.Fatalf("the config file does not name bin %s:\n%s", hash, raw)
	}

	stdout, stderr, err = runClient(t, nil, "--delete", "notes.md")
	if err != nil {
		t.Fatalf("delete run: %v; stderr %s", err, stderr)
	}
	if f.binCount() != 1 {
		t.Fatalf("bins = %d, want the emptied bin to be the one bin", f.binCount())
	}

	stdout, stderr, err = runClient(t, nil, "--describe")
	if err != nil {
		t.Fatalf("--describe after the delete: %v; stderr %s", err, stderr)
	}
	if !strings.Contains(stdout, "title: Kept") {
		t.Errorf("--describe stdout = %q, want it to read the emptied bin", stdout)
	}
	if f.binCount() != 1 {
		t.Errorf("bins = %d, want no new bin after the delete", f.binCount())
	}
}

// TestDeleteFlagsAreInHelp keeps the usage text honest about the two new flags.
func TestDeleteFlagsAreInHelp(t *testing.T) {
	newFake(t, "test-key")

	stdout, stderr, err := runClient(t, nil, "--help")
	if err != nil {
		t.Fatalf("--help: %v; stderr %s", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want the usage on stderr only", stdout)
	}
	for _, want := range []string{"-delete", "-delete-folder"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the usage does not name %s:\n%s", want, stderr)
		}
	}
}
