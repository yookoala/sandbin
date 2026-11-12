package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// controlTestSocket is where a control test binds its socket.
func controlTestSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sandbin.db.sock")
}

// controlTestClient is an http.Client dialed through a unix socket, which is the only way
// this API can be reached.
func controlTestClient(socketPath string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}}
}

// newControlTestServer binds a control socket for a fresh store and serves the control API
// over it, returning the socket path, the store behind it and a client dialed through it.
func newControlTestServer(t *testing.T) (string, *Store, *http.Client) {
	t.Helper()
	store := newTestStore(t)
	socketPath := controlTestSocket(t)
	listener, err := listenControl(socketPath)
	if err != nil {
		t.Fatalf("listenControl(%s): %v", socketPath, err)
	}
	server := &http.Server{Handler: controlMux(store)}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("control server on %s: %v", socketPath, err)
		}
	}()
	t.Cleanup(func() { server.Close() })
	return socketPath, store, controlTestClient(socketPath)
}

// controlProbe sends one request through the socket and returns its status and body, the
// way curl --unix-socket reports them.
func controlProbe(t *testing.T, client *http.Client, method, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, "http://localhost"+path, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s over the control socket: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the body of %s %s: %v", method, path, err)
	}
	return resp.StatusCode, body
}

// controlRows reads a list answer back as json objects, so a test can check the fields one
// at a time.
func controlRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("control answer does not parse as json rows: %v\n%s", err, body)
	}
	return rows
}

// controlRow reads a show answer back as the one json object it is.
func controlRow(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var row map[string]any
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatalf("control answer does not parse as one json row: %v\n%s", err, body)
	}
	return row
}

// wantRow compares one answer against the row the test seeded.
func wantRow(t *testing.T, row map[string]any, want IPBlock) {
	t.Helper()
	if len(row) != 4 {
		t.Errorf("control row = %v, want exactly ip, failures, blocked_until, updated_at", row)
		return
	}
	fields := map[string]any{
		"ip": want.IP, "failures": float64(want.Failures),
		"blocked_until": want.BlockedUntil, "updated_at": want.UpdatedAt,
	}
	for key, value := range fields {
		if row[key] != value {
			t.Errorf("control row field %q = %v, want %v", key, row[key], value)
		}
	}
}

func TestControlListEmpty(t *testing.T) {
	_, _, client := newControlTestServer(t)

	code, body := controlProbe(t, client, http.MethodGet, "/ip")
	if code != http.StatusOK {
		t.Fatalf("GET /ip on an empty table = %d, want 200 (body %q)", code, body)
	}
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Errorf("GET /ip on an empty table = %q, want %q", got, "[]")
	}

	// This API is json and nothing else.
	resp, err := client.Get("http://localhost/ip")
	if err != nil {
		t.Fatalf("GET /ip for its Content-Type: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /ip Content-Type = %q, want application/json", ct)
	}
}

func TestControlListShowsRows(t *testing.T) {
	_, store, client := newControlTestServer(t)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)
	expired := seedExpiredIPBlock(t, store, "10.0.0.1", 5)

	code, body := controlProbe(t, client, http.MethodGet, "/ip")
	if code != http.StatusOK {
		t.Fatalf("GET /ip = %d, want 200 (body %q)", code, body)
	}
	rows := controlRows(t, body)
	if len(rows) != 2 {
		t.Fatalf("GET /ip returned %d rows, want 2:\n%s", len(rows), body)
	}
	// Ordered by ip, and the expired row is included on purpose.
	wantRow(t, rows[0], expired)
	wantRow(t, rows[1], live)
}

func TestControlShowHit(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "10.0.0.1", 5)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)

	code, body := controlProbe(t, client, http.MethodGet, "/ip/203.0.113.7")
	if code != http.StatusOK {
		t.Fatalf("GET /ip/203.0.113.7 = %d, want 200 (body %q)", code, body)
	}
	wantRow(t, controlRow(t, body), live)
}

// TestControlShowMiss is the server half of the wording the client prints for it.
func TestControlShowMiss(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	code, body := controlProbe(t, client, http.MethodGet, "/ip/198.51.100.4")
	if code != http.StatusNotFound {
		t.Fatalf("GET /ip/198.51.100.4 = %d, want 404 (body %q)", code, body)
	}
	if got := strings.TrimSpace(string(body)); got != `{"error":"no block for 198.51.100.4"}` {
		t.Errorf("GET /ip/198.51.100.4 body = %q, want %q", got, `{"error":"no block for 198.51.100.4"}`)
	}
	if _, ok := ipBlockRow(t, store, "203.0.113.7"); !ok {
		t.Errorf("203.0.113.7 vanished along with the miss")
	}
}

func TestControlUnblockHit(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)
	kept := seedLiveIPBlock(t, store, "198.51.100.4", 1)

	code, body := controlProbe(t, client, http.MethodDelete, "/ip/203.0.113.7")
	if code != http.StatusOK {
		t.Fatalf("DELETE /ip/203.0.113.7 = %d, want 200 (body %q)", code, body)
	}
	if got := strings.TrimSpace(string(body)); got != `{"unblocked":"203.0.113.7"}` {
		t.Errorf("DELETE /ip/203.0.113.7 body = %q, want %q", got, `{"unblocked":"203.0.113.7"}`)
	}
	// The row and its failure count are really gone, and only that row.
	if failures, ok := ipBlockRow(t, store, "203.0.113.7"); ok {
		t.Errorf("203.0.113.7 still has failures %d after the unblock", failures)
	}
	if failures, ok := ipBlockRow(t, store, kept.IP); !ok || failures != 1 {
		t.Errorf("%s = failures %d, ok %v; want it untouched", kept.IP, failures, ok)
	}

	if code, body := controlProbe(t, client, http.MethodGet, "/ip/203.0.113.7"); code != http.StatusNotFound {
		t.Errorf("GET /ip/203.0.113.7 after the unblock = %d (body %q), want 404", code, body)
	}
}

func TestControlUnblockMiss(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	code, body := controlProbe(t, client, http.MethodDelete, "/ip/198.51.100.4")
	if code != http.StatusNotFound {
		t.Fatalf("DELETE /ip/198.51.100.4 = %d, want 404 (body %q)", code, body)
	}
	if got := strings.TrimSpace(string(body)); got != `{"error":"no block for 198.51.100.4"}` {
		t.Errorf("DELETE /ip/198.51.100.4 body = %q, want %q", got, `{"error":"no block for 198.51.100.4"}`)
	}
	if _, ok := ipBlockRow(t, store, "203.0.113.7"); !ok {
		t.Errorf("203.0.113.7 vanished along with the miss")
	}
}

func TestControlClearCounts(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "10.0.0.1", 1)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)
	seedExpiredIPBlock(t, store, "198.51.100.4", 3)

	code, body := controlProbe(t, client, http.MethodDelete, "/ip")
	if code != http.StatusOK {
		t.Fatalf("DELETE /ip = %d, want 200 (body %q)", code, body)
	}
	if got := strings.TrimSpace(string(body)); got != `{"cleared":3}` {
		t.Errorf("DELETE /ip body = %q, want %q", got, `{"cleared":3}`)
	}
	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("after DELETE /ip the table holds %d rows, want 0", len(blocks))
	}

	code, body = controlProbe(t, client, http.MethodDelete, "/ip")
	if code != http.StatusOK || strings.TrimSpace(string(body)) != `{"cleared":0}` {
		t.Errorf("DELETE /ip on an empty table = %d %q, want 200 %q", code, body, `{"cleared":0}`)
	}
}

// TestControlWrongMethod proves the mux answers 405 for a method an endpoint does not take,
// so a management request cannot arrive as some other verb.
func TestControlWrongMethod(t *testing.T) {
	_, store, client := newControlTestServer(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/ip"},
		{http.MethodPut, "/ip"},
		{http.MethodPut, "/ip/203.0.113.7"},
		{http.MethodPost, "/ip/203.0.113.7"},
	} {
		code, body := controlProbe(t, client, c.method, c.path)
		if code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d (body %q), want 405", c.method, c.path, code, body)
		}
	}

	// HEAD reads, so Go's mux answers it the way it answers GET, which is how the bin
	// server's own healthz route treats HEAD too.
	code, body := controlProbe(t, client, http.MethodHead, "/ip")
	if code != http.StatusOK {
		t.Errorf("HEAD /ip = %d (body %q), want 200: a read is not a wrong method", code, body)
	}
	if failures, ok := ipBlockRow(t, store, "203.0.113.7"); !ok || failures != 2 {
		t.Errorf("203.0.113.7 = failures %d, ok %v; want the refused requests to change nothing", failures, ok)
	}
}

// TestControlHasNoBinRoutes is the authority claim: the control mux shares nothing with the
// bin server, so no bin, file or healthz route is reachable through the socket.
func TestControlHasNoBinRoutes(t *testing.T) {
	socketPath, store, client := newControlTestServer(t)
	binPath := "/1a50eb32/"
	if _, err := store.CreateBin("", "", ""); err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	for _, path := range []string{"/", "/healthz", binPath, binPath + "index.html", "/ip/203.0.113.7/extra", "/ip/"} {
		code, body := controlProbe(t, client, http.MethodGet, path)
		if code != http.StatusNotFound {
			t.Errorf("GET %s over %s = %d (body %q), want 404: the control mux has no bin, file or healthz route",
				path, socketPath, code, body)
		}
	}

	code, body := controlProbe(t, client, http.MethodPut, binPath+"index.html")
	if code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Errorf("PUT %s over %s = %d (body %q), want 404 or 405", binPath+"index.html", socketPath, code, body)
	}
}

// TestControlStoreErrorIs500 is the arm a corrupt row reaches: only raw SQL can store a
// failures column that is not an integer, and the answer is a readable 500 rather than a
// half-rendered row.
func TestControlStoreErrorIs500(t *testing.T) {
	_, store, client := newControlTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.Exec(`INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)`,
		"1.2.3.9", "abc", now, now); err != nil {
		t.Fatalf("seed a row with failures=abc: %v", err)
	}

	code, body := controlProbe(t, client, http.MethodGet, "/ip/1.2.3.9")
	if code != http.StatusInternalServerError {
		t.Fatalf("GET /ip/1.2.3.9 with a row its read cannot decode = %d (body %q), want 500", code, body)
	}
	var answer errorBody
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the 500 body is not the error object: %v\n%s", err, body)
	}
	if answer.Error == "" {
		t.Errorf("the 500 body = %s, want an error the operator can read", body)
	}

	// list hits the same row and answers the same way.
	if code, body := controlProbe(t, client, http.MethodGet, "/ip"); code != http.StatusInternalServerError {
		t.Errorf("GET /ip with an undecodable row = %d (body %q), want 500", code, body)
	}
}

// TestControlSocketModeIs0600 proves the permission that is the whole authority of this API.
func TestControlSocketModeIs0600(t *testing.T) {
	socketPath, _, _ := newControlTestServer(t)

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat %s: %v", socketPath, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stat %s mode = %v, want a socket", socketPath, info.Mode())
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("control socket %s mode = %o, want 0600", socketPath, perm)
	}
}

// TestControlRefusesALiveSocket is the guard that keeps a second serve from taking the
// management address of the one that is answering.
func TestControlRefusesALiveSocket(t *testing.T) {
	socketPath, _, client := newControlTestServer(t)

	listener, err := listenControl(socketPath)
	if err == nil {
		listener.Close()
		t.Fatalf("listenControl(%s) over a live socket: no error, want a refusal", socketPath)
	}
	if !strings.Contains(err.Error(), "another sandbin serve is already listening on") ||
		!strings.Contains(err.Error(), socketPath) {
		t.Errorf("refusal = %v, want one naming the live socket %s", err, socketPath)
	}

	// The refusal left the live server answering.
	if code, body := controlProbe(t, client, http.MethodGet, "/ip"); code != http.StatusOK {
		t.Errorf("GET /ip after the refusal = %d (body %q), want the live server untouched", code, body)
	}
}

// TestControlRefusesASocketItMayNotReach is the same guard for a live socket this run is not
// allowed to dial: refusing keeps a second serve from unlinking another owner's listener.
func TestControlRefusesASocketItMayNotReach(t *testing.T) {
	socketPath, _, _ := newControlTestServer(t)
	if err := os.Chmod(socketPath, 0400); err != nil {
		t.Fatalf("chmod %s 0400: %v", socketPath, err)
	}

	listener, err := listenControl(socketPath)
	if err == nil {
		listener.Close()
		t.Fatalf("listenControl(%s) over a socket this run may not dial: no error, want a refusal", socketPath)
	}
	if !strings.Contains(err.Error(), "already listening on") {
		t.Errorf("refusal = %v, want one naming the socket it may not reach", err)
	}
	if info, err := os.Stat(socketPath); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Errorf("stat %s after the refusal = %v, %v; want the other owner's socket left alone", socketPath, info, err)
	}
}

// TestControlReplacesAStaleSocket is the guard a killed serve leaves behind: a socket nobody
// answers on is unlinked and the path is bound again.
func TestControlReplacesAStaleSocket(t *testing.T) {
	socketPath := controlTestSocket(t)

	// A listener that keeps its file on close is exactly what a killed serve leaves: a
	// socket file with nothing behind it.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("bind the stale socket %s: %v", socketPath, err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if info, err := os.Stat(socketPath); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stat %s after the stale close = %v, %v; want the socket file left behind", socketPath, info, err)
	}

	listener, err := listenControl(socketPath)
	if err != nil {
		t.Fatalf("listenControl over the stale socket: %v", err)
	}
	store := newTestStore(t)
	server := &http.Server{Handler: controlMux(store)}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("control server on the replaced socket: %v", err)
		}
	}()
	t.Cleanup(func() { server.Close() })

	// The replacement answers, and it is a fresh 0600 socket.
	if code, body := controlProbe(t, controlTestClient(socketPath), http.MethodGet, "/ip"); code != http.StatusOK {
		t.Errorf("GET /ip over the replaced socket = %d (body %q), want 200", code, body)
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat %s: %v", socketPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("replaced control socket %s mode = %o, want 0600", socketPath, perm)
	}
}

// TestControlRefusesANonSocket is the guard that keeps serve from eating an operator's file
// at the socket path.
func TestControlRefusesANonSocket(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		socketPath := controlTestSocket(t)
		const content = "not a socket\n"
		if err := os.WriteFile(socketPath, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", socketPath, err)
		}

		listener, err := listenControl(socketPath)
		if err == nil {
			listener.Close()
			t.Fatalf("listenControl(%s) over a regular file: no error, want a refusal", socketPath)
		}
		if !strings.Contains(err.Error(), "not a socket") || !strings.Contains(err.Error(), socketPath) {
			t.Errorf("refusal = %v, want one naming %s as not a socket", err, socketPath)
		}
		survivor, err := os.ReadFile(socketPath)
		if err != nil || string(survivor) != content {
			t.Errorf("the file at the socket path = %q, %v; want it untouched", survivor, err)
		}
	})

	t.Run("symlink to a regular file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "operator.txt")
		const content = "do not eat this\n"
		if err := os.WriteFile(target, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
		link := filepath.Join(dir, "sandbin.db.sock")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink %s -> %s: %v", link, target, err)
		}

		listener, err := listenControl(link)
		if err == nil {
			listener.Close()
			t.Fatalf("listenControl(%s) over a symlink: no error, want a refusal", link)
		}
		if !strings.Contains(err.Error(), "not a socket") {
			t.Errorf("refusal = %v, want one naming %s as not a socket", err, link)
		}
		survivor, err := os.ReadFile(target)
		if err != nil || string(survivor) != content {
			t.Errorf("the symlink's target = %q, %v; want it untouched", survivor, err)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("lstat %s = %v, %v; want the symlink left alone", link, info, err)
		}
	})
}
