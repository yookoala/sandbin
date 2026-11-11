package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeServer mirrors the real server's contract so the client is tested without a
// network or the server binary: one 403 for the first bad API key and then 429 with
// a growing Retry-After for every later key-checked request, 404 for an unknown bin,
// 401 for a missing bin password, 413 for an oversized body, and 201 or 200
// according to whether the path was new. It counts requests, which is how these
// tests prove the client never retries.
type fakeServer struct {
	apiKey     string
	bodyLimit  int
	log        io.Writer // optional sink that notes each arriving request
	createBody string    // when set, POST answers with this instead of "/abc1234/"

	mu       sync.Mutex
	requests int
	puts     int
	failures int
	bins     map[string]string // bin hash -> bin password, "" meaning open
	files    map[string]string // "bin/path" -> stored body
}

// newFake starts a fake server, exports its url and key as the client's env, and
// runs the test in a private working directory.
func newFake(t *testing.T, apiKey string) *fakeServer {
	t.Helper()
	f := &fakeServer{apiKey: apiKey, bodyLimit: 1 << 20, bins: map[string]string{}, files: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("SANDBIN_URL", srv.URL)
	t.Setenv("SANDBIN_API_KEY", apiKey)
	t.Setenv("SANDBIN_BIN_PASSWORD", "")
	t.Chdir(t.TempDir())
	return f
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests++
	if r.Method == http.MethodPut {
		f.puts++
	}
	if f.log != nil {
		fmt.Fprintln(f.log, "request")
	}

	keyChecked := r.Method == http.MethodPost || r.Method == http.MethodPut
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

	if r.Method == http.MethodPost {
		hash := fmt.Sprintf("bin%04d", len(f.bins)+1)
		f.bins[hash] = r.Header.Get("X-Bin-Password")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		if f.createBody != "" {
			io.WriteString(w, f.createBody)
			return
		}
		io.WriteString(w, "/"+hash+"/")
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	first, rest, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !found || first == "" {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	password, known := f.bins[first]
	if !known {
		http.Error(w, "unknown bin", http.StatusNotFound)
		return
	}
	if rest == "" {
		http.Error(w, "empty path", http.StatusBadRequest)
		return
	}
	if password != "" {
		_, sent, ok := r.BasicAuth()
		if !ok || sent != password {
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
	f.bins[hash] = password
	return hash
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

// readStateFile reads .sandbin.json from the test's working directory.
func readStateFile(t *testing.T) ([]byte, os.FileInfo) {
	t.Helper()
	info, err := os.Stat(stateFileName)
	if err != nil {
		t.Fatalf("stat %s: %v", stateFileName, err)
	}
	raw, err := os.ReadFile(stateFileName)
	if err != nil {
		t.Fatalf("read %s: %v", stateFileName, err)
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

func TestSecondRunReusesTheStateFileAndReportsUpdated(t *testing.T) {
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
		t.Errorf("bins = %d, want the state file's one bin", f.binCount())
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

	state, _ := readStateFile(t)
	if !strings.Contains(string(state), newHash) {
		t.Errorf("state file =\n%s\nwant it to remember the new bin %s", state, newHash)
	}
	if strings.Contains(string(state), oldHash) {
		t.Errorf("state file =\n%s\nwant the replaced bin gone", state)
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
	if !strings.Contains(stderr, "SANDBIN_API_KEY") {
		t.Errorf("stderr = %q, want it to name SANDBIN_API_KEY", stderr)
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
	if _, err := os.Stat(stateFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after a failed run, want no state file", stateFileName)
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

	// The state file now says the bin is protected, and the password is gone.
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

func TestCorruptStateFileIsAnErrorNamingIt(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, stateFileName, "this is not json {{{")

	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against a corrupt state file succeeded, want an error")
	}
	if !strings.Contains(stderr, stateFileName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, stateFileName)
	}
	// The point of the error: no silent second bin for one prototype.
	if f.binCount() != 0 || f.requestsSeen() != 0 {
		t.Errorf("bins = %d, requests = %d; want a run that made no request", f.binCount(), f.requestsSeen())
	}
}

func TestStateFileMissingAFieldIsAnError(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, stateFileName, `{"url": "http://127.0.0.1:1"}`)

	_, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against a half-empty state file succeeded, want an error")
	}
	if !strings.Contains(stderr, stateFileName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, stateFileName)
	}
	if f.requestsSeen() != 0 {
		t.Errorf("requests = %d, want none", f.requestsSeen())
	}
}

func TestStateFileForAnotherUrlStartsAFreshBin(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, stateFileName, `{"url": "http://other.invalid", "bin": "ffffffff", "protected": false}`)

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
	state, _ := readStateFile(t)
	if !strings.Contains(string(state), os.Getenv("SANDBIN_URL")) {
		t.Errorf("state file =\n%s\nwant it rewritten to the url in use", state)
	}
}

func TestBinAndUrlFlagsNeedNoStateFile(t *testing.T) {
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
	if _, err := os.Stat(stateFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after a --bin run, want no state file", stateFileName)
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

func TestStateFileIsPrivateAndHoldsNoSecret(t *testing.T) {
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

	raw, info := readStateFile(t)
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("%s mode = %o, want 0600", stateFileName, perm)
	}
	if text := string(raw); strings.Contains(text, apiKey) || strings.Contains(text, binPassword) {
		t.Errorf("%s = %q, want neither secret in it", stateFileName, text)
	}

	var state binState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("parse %s: %v", stateFileName, err)
	}
	if state.URL != os.Getenv("SANDBIN_URL") || state.Bin != hash || !state.Protected {
		t.Errorf("%s = %+v, want the url, the bin and protected true", stateFileName, state)
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

func TestStateFileWhoseBinNoLongerExistsReports404(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	writeWorkDirFile(t, stateFileName, `{"url": "`+os.Getenv("SANDBIN_URL")+`", "bin": "ffffffff", "protected": false}`)

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against a vanished bin succeeded, want a 404")
	}
	if !strings.Contains(stderr, "404") || !strings.Contains(stderr, "unknown bin") {
		t.Errorf("stderr = %q, want 404 and the server's message", stderr)
	}
	// The state file is honoured rather than quietly replaced by a new bin.
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
	if _, err := os.Stat(stateFileName); !os.IsNotExist(err) {
		t.Errorf("%s exists after the refusal, want no state file", stateFileName)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestUnreadableStateFileIsAnErrorNamingIt(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "index.html", "<h1>x</h1>")
	// A present file the client cannot read at all.
	if err := os.Mkdir(stateFileName, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", stateFileName, err)
	}

	stdout, stderr, err := runClient(t, nil, "index.html")
	if err == nil {
		t.Fatalf("run against an unreadable state file succeeded, want an error")
	}
	if !strings.Contains(stderr, stateFileName) {
		t.Errorf("stderr = %q, want it to name %s", stderr, stateFileName)
	}
	if stdout != "" || f.requestsSeen() != 0 {
		t.Errorf("stdout = %q, requests = %d; want neither", stdout, f.requestsSeen())
	}
}

func TestStateWriteFailureNamesTheFile(t *testing.T) {
	// The run reports a state write failure instead of pretending the bin was
	// saved. A missing directory is the failure a root-run test can force.
	newFake(t, "test-key")
	dir := filepath.Join(t.TempDir(), "gone")
	err := writeState(dir, binState{URL: "http://127.0.0.1:1", Bin: "abc1234"})
	if err == nil {
		t.Fatalf("writeState into %s succeeded, want an error", dir)
	}
	if !strings.Contains(err.Error(), stateFileName) {
		t.Errorf("writeState error = %q, want it to name %s", err, stateFileName)
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
