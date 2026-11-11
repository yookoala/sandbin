package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runDispatch runs one whole command line and returns what it printed and the exit code.
func runDispatch(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := dispatch(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestDispatchNoArgs(t *testing.T) {
	stdout, stderr, code := runDispatch(t)
	if code != 1 {
		t.Errorf("bare sandbin exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "usage:") || !strings.Contains(stderr, "sandbin serve") {
		t.Errorf("bare sandbin stderr = %q, want the usage", stderr)
	}
	if stdout != "" {
		t.Errorf("bare sandbin stdout = %q, want nothing", stdout)
	}
}

func TestDispatchHelp(t *testing.T) {
	for _, verb := range []string{"help", "--help"} {
		stdout, stderr, code := runDispatch(t, verb)
		if code != 0 {
			t.Errorf("sandbin %s exited %d, want 0", verb, code)
		}
		if !strings.Contains(stdout, "usage:") || !strings.Contains(stdout, "sandbin ip list") {
			t.Errorf("sandbin %s stdout = %q, want the usage", verb, stdout)
		}
		if !strings.Contains(stdout, "sandbin ip show [--db f] <ip>") {
			t.Errorf("sandbin %s stdout = %q, want the usage to name sandbin ip show", verb, stdout)
		}
		if stderr != "" {
			t.Errorf("sandbin %s stderr = %q, want nothing", verb, stderr)
		}
	}
}

func TestDispatchUnknownVerb(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "bogus")
	if code != 1 {
		t.Errorf("sandbin bogus exited %d, want 1", code)
	}
	if !strings.Contains(stderr, `unknown command "bogus"`) || !strings.Contains(stderr, "usage:") {
		t.Errorf("sandbin bogus stderr = %q, want a refusal naming it and the usage", stderr)
	}
	if stdout != "" {
		t.Errorf("sandbin bogus stdout = %q, want nothing", stdout)
	}
}

// TestDispatchServeHelp covers serve's flag.ErrHelp arm: asking for the flag list is
// not a failure and starts no listener.
func TestDispatchServeHelp(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "serve", "--help")
	if code != 0 {
		t.Errorf("sandbin serve --help exited %d, want 0; stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "Usage of sandbin serve") {
		t.Errorf("sandbin serve --help stderr = %q, want the flag list", stderr)
	}
	if stdout != "" {
		t.Errorf("sandbin serve --help stdout = %q, want nothing", stdout)
	}
}

// TestServeFlagParsing checks every existing flag, its default and its env fallback
// without starting a listener.
func TestServeFlagParsing(t *testing.T) {
	var stdout bytes.Buffer
	cfg, err := parseServeFlags([]string{
		"--api-key", "k", "--db", "elsewhere.db", "--addr", "127.0.0.1:8081",
		"--trusted-proxy", "203.0.113.7", "--trusted-proxy-file", "proxies.txt",
	}, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags: %v", err)
	}
	want := serveConfig{
		apiKey: "k", dbPath: "elsewhere.db", addr: "127.0.0.1:8081",
		trustedProxy: "203.0.113.7", trustedProxyFile: "proxies.txt",
	}
	if cfg != want {
		t.Errorf("serve flags = %+v, want %+v", cfg, want)
	}

	// With nothing passed the defaults are what serve has always defaulted to.
	cfg, err = parseServeFlags(nil, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with no flags: %v", err)
	}
	want = serveConfig{dbPath: "sandbin.db", addr: ":8080"}
	if cfg != want {
		t.Errorf("serve defaults = %+v, want %+v", cfg, want)
	}

	// Each flag still falls back to its environment variable.
	t.Setenv("SANDBIN_API_KEY", "env-key")
	t.Setenv("SANDBIN_TRUSTED_PROXIES", "198.51.100.0/24")
	t.Setenv("SANDBIN_TRUSTED_PROXIES_FILE", "env-proxies.txt")
	cfg, err = parseServeFlags(nil, &stdout)
	if err != nil {
		t.Fatalf("parseServeFlags with the env set: %v", err)
	}
	want = serveConfig{
		apiKey: "env-key", dbPath: "sandbin.db", addr: ":8080",
		trustedProxy: "198.51.100.0/24", trustedProxyFile: "env-proxies.txt",
	}
	if cfg != want {
		t.Errorf("serve env fallbacks = %+v, want %+v", cfg, want)
	}

	if _, err := parseServeFlags([]string{"--bogus"}, &stdout); err == nil {
		t.Error("serve --bogus: no error, want a refusal")
	}
	if _, err := parseServeFlags([]string{"extra"}, &stdout); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("serve extra: err %v, want a refusal naming it", err)
	}
}

func TestDispatchServeBadFlag(t *testing.T) {
	stdout, stderr, code := runDispatch(t, "serve", "--bogus")
	if code != 1 {
		t.Errorf("sandbin serve --bogus exited %d, want 1", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined: -bogus") {
		t.Errorf("serve --bogus stderr = %q, want the flag refusal", stderr)
	}
	if !strings.Contains(stderr, "Usage of sandbin serve:") {
		t.Errorf("serve --bogus stderr = %q, want serve's flag list", stderr)
	}
	if stdout != "" {
		t.Errorf("serve --bogus stdout = %q, want nothing", stdout)
	}
}

// TestDispatchIPVerb shows dispatch hands the db file to the ip verbs.
func TestDispatchIPVerb(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	stdout, stderr, code := runDispatch(t, "ip", "list", "--db", dbPath)
	if code != 0 {
		t.Errorf("sandbin ip list exited %d with stderr %q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "203.0.113.7") {
		t.Errorf("sandbin ip list stdout = %q, want the blocked address", stdout)
	}
}

// newIPTestStore returns a db path and the store open on it, so a test can seed rows and
// then run an ip command against the same file.
func newIPTestStore(t *testing.T) (string, *Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return dbPath, store
}

// runIPTest runs one sandbin ip command against dbPath and returns what it printed and
// its error. --db goes right after the verb, the way Go's flag parser reads a flag.
func runIPTest(t *testing.T, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	verb, rest := args[0], args[1:]
	var stdout, stderr bytes.Buffer
	err := runIP(append([]string{verb, "--db", dbPath}, rest...), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// seedLiveIPBlock seeds one row whose block still has time left, relative to the real
// clock the list command reads.
func seedLiveIPBlock(t *testing.T, store *Store, ip string, failures int) IPBlock {
	t.Helper()
	now := time.Now().UTC()
	blockedUntil := now.Add(time.Duration(failures) * blockStep)
	seedIPBlock(t, store, ip, failures, blockedUntil, now)
	return IPBlock{IP: ip, Failures: failures, BlockedUntil: blockedUntil.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
}

// seedExpiredIPBlock seeds one row whose blocked_until is already past.
func seedExpiredIPBlock(t *testing.T, store *Store, ip string, failures int) IPBlock {
	t.Helper()
	now := time.Now().UTC()
	blockedUntil := now.Add(-time.Hour)
	seedIPBlock(t, store, ip, failures, blockedUntil, now.Add(-2*time.Hour))
	return IPBlock{IP: ip, Failures: failures, BlockedUntil: blockedUntil.Format(time.RFC3339Nano), UpdatedAt: (now.Add(-2 * time.Hour)).Format(time.RFC3339Nano)}
}

func TestIPNoVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runIP(nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("sandbin ip with no verb: no error, want a refusal")
	}
	if !strings.Contains(stderr.String(), "usage:") || !strings.Contains(stderr.String(), "sandbin ip unblock") {
		t.Errorf("sandbin ip stderr = %q, want the ip usage", stderr.String())
	}
	if !strings.Contains(stderr.String(), "sandbin ip show") || !strings.Contains(stderr.String(), "pass list, show <ip>, unblock <ip> or clear") {
		t.Errorf("sandbin ip stderr = %q, want the usage and the refusal to name show", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("sandbin ip stdout = %q, want nothing", stdout.String())
	}
}

func TestIPUnknownVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runIP([]string{"bogus"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("sandbin ip bogus: no error, want a refusal")
	}
	if !strings.Contains(stderr.String(), `unknown command "bogus"`) || !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("sandbin ip bogus stderr = %q, want a refusal naming it and the ip usage", stderr.String())
	}
	if !strings.Contains(stderr.String(), "pass list, show <ip>, unblock <ip> or clear") {
		t.Errorf("sandbin ip bogus stderr = %q, want the refusal to name show", stderr.String())
	}
}

func TestIPListEmpty(t *testing.T) {
	dbPath, _ := newIPTestStore(t)
	stdout, stderr, err := runIPTest(t, dbPath, "list")
	if err != nil {
		t.Fatalf("ip list on an empty table: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "no blocked addresses" {
		t.Errorf("ip list on an empty table = %q, want %q", got, "no blocked addresses")
	}
}

// TestIPMissingDbFileWarns proves a typo'd --db never reads as a silent all-clear:
// the empty file it gets is announced on stderr.
func TestIPMissingDbFileWarns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sanbin.db")
	var stdout, stderr bytes.Buffer
	err := runIP([]string{"list", "--db", dbPath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ip list with a --db that does not exist: %v (stderr %q)", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "sanbin.db") || !strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("stderr = %q, want a warning naming the file that was not there", stderr.String())
	}
	if !strings.Contains(stdout.String(), "no blocked addresses") {
		t.Errorf("stdout = %q, want the empty list", stdout.String())
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("stat %s: %v, want the file created the way serve would create it", dbPath, err)
	}
}

func TestIPListEmptyJSON(t *testing.T) {
	dbPath, _ := newIPTestStore(t)
	stdout, stderr, err := runIPTest(t, dbPath, "list", "--json")
	if err != nil {
		t.Fatalf("ip list --json on an empty table: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("ip list --json on an empty table = %q, want %q", got, "[]")
	}
}

func TestIPListShowsRows(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)
	expired := seedExpiredIPBlock(t, store, "10.0.0.1", 5)

	stdout, stderr, err := runIPTest(t, dbPath, "list")
	if err != nil {
		t.Fatalf("ip list: %v (stderr %q)", err, stderr)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("ip list printed %d lines, want a header and two rows:\n%s", len(lines), stdout)
	}
	for _, column := range []string{"IP", "FAILURES", "BLOCKED UNTIL", "REMAINING", "LAST FAILURE"} {
		if !strings.Contains(lines[0], column) {
			t.Errorf("list header = %q, want a %s column", lines[0], column)
		}
	}

	// 10.0.0.1 sorts first, and its row reads expired because its blocked_until is past.
	expiredFields := strings.Fields(lines[1])
	if len(expiredFields) != 5 {
		t.Fatalf("expired row = %q, want 5 columns", lines[1])
	}
	want := []string{expired.IP, "5", expired.BlockedUntil, "expired", expired.UpdatedAt}
	for i, field := range want {
		if expiredFields[i] != field {
			t.Errorf("expired row column %d = %q, want %q", i, expiredFields[i], field)
		}
	}

	liveFields := strings.Fields(lines[2])
	if len(liveFields) != 5 {
		t.Fatalf("live row = %q, want 5 columns", lines[2])
	}
	if liveFields[0] != live.IP || liveFields[1] != "2" || liveFields[2] != live.BlockedUntil || liveFields[4] != live.UpdatedAt {
		t.Errorf("live row = %q, want %s with 2 failures, blocked until %s and last failure %s",
			lines[2], live.IP, live.BlockedUntil, live.UpdatedAt)
	}
	if liveFields[3] == "expired" {
		t.Errorf("live row = %q, want a remaining time, not expired", lines[2])
	}
	left, err := time.ParseDuration(liveFields[3])
	if err != nil {
		t.Fatalf("live row REMAINING %q: %v", liveFields[3], err)
	}
	until, err := time.Parse(time.RFC3339Nano, live.BlockedUntil)
	if err != nil {
		t.Fatalf("parse the seeded blocked_until: %v", err)
	}
	if delta := left - time.Until(until); delta < -2*time.Second || delta > 2*time.Second {
		t.Errorf("live row REMAINING = %v, want about %v", left, time.Until(until))
	}
}

func TestIPListJSON(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)
	expired := seedExpiredIPBlock(t, store, "10.0.0.1", 5)

	stdout, stderr, err := runIPTest(t, dbPath, "list", "--json")
	if err != nil {
		t.Fatalf("ip list --json: %v (stderr %q)", err, stderr)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("ip list --json output does not parse: %v\n%s", err, stdout)
	}
	if len(rows) != 2 {
		t.Fatalf("ip list --json returned %d rows, want 2", len(rows))
	}
	want := []map[string]any{
		{"ip": expired.IP, "failures": float64(5), "blocked_until": expired.BlockedUntil, "updated_at": expired.UpdatedAt},
		{"ip": live.IP, "failures": float64(2), "blocked_until": live.BlockedUntil, "updated_at": live.UpdatedAt},
	}
	for i, row := range rows {
		if len(row) != 4 {
			t.Errorf("row %d has %d fields (%v), want exactly ip, failures, blocked_until, updated_at", i, len(row), row)
			continue
		}
		for key, value := range want[i] {
			if row[key] != value {
				t.Errorf("row %d field %q = %v, want %v", i, key, row[key], value)
			}
		}
	}
}

func TestIPListRejectsExtraArgs(t *testing.T) {
	dbPath, _ := newIPTestStore(t)
	stdout, _, err := runIPTest(t, dbPath, "list", "extra")
	if err == nil {
		t.Fatalf("ip list extra: no error, want a refusal (stdout %q)", stdout)
	}
	if _, stderr, _ := runIPTest(t, dbPath, "list", "--bogus"); !strings.Contains(stderr, "flag provided but not defined: -bogus") {
		t.Errorf("ip list --bogus stderr = %q, want the flag refusal", stderr)
	}
}

// TestIPShowPrintsOneRow is list's table with one row: the header plus exactly the address
// that was named, and nothing else.
func TestIPShowPrintsOneRow(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "10.0.0.1", 5)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)

	stdout, stderr, err := runIPTest(t, dbPath, "show", "203.0.113.7")
	if err != nil {
		t.Fatalf("ip show 203.0.113.7: %v (stderr %q)", err, stderr)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("ip show printed %d lines, want a header and one row:\n%s", len(lines), stdout)
	}
	for _, column := range []string{"IP", "FAILURES", "BLOCKED UNTIL", "REMAINING", "LAST FAILURE"} {
		if !strings.Contains(lines[0], column) {
			t.Errorf("show header = %q, want a %s column", lines[0], column)
		}
	}

	fields := strings.Fields(lines[1])
	if len(fields) != 5 {
		t.Fatalf("shown row = %q, want 5 columns", lines[1])
	}
	if fields[0] != live.IP || fields[1] != "2" || fields[2] != live.BlockedUntil || fields[4] != live.UpdatedAt {
		t.Errorf("shown row = %q, want %s with 2 failures, blocked until %s and last failure %s",
			lines[1], live.IP, live.BlockedUntil, live.UpdatedAt)
	}
	if fields[3] == "expired" {
		t.Errorf("shown row = %q, want a remaining time, not expired", lines[1])
	}
	left, err := time.ParseDuration(fields[3])
	if err != nil {
		t.Fatalf("shown row REMAINING %q: %v", fields[3], err)
	}
	until, err := time.Parse(time.RFC3339Nano, live.BlockedUntil)
	if err != nil {
		t.Fatalf("parse the seeded blocked_until: %v", err)
	}
	if delta := left - time.Until(until); delta < -2*time.Second || delta > 2*time.Second {
		t.Errorf("shown row REMAINING = %v, want about %v", left, time.Until(until))
	}

	if _, _, code := runDispatch(t, "ip", "show", "--db", dbPath, "203.0.113.7"); code != 0 {
		t.Errorf("sandbin ip show of a blocked address exited %d, want 0", code)
	}
}

// TestIPShowExpiredRow reads expired, the way list reads it, with the count that shapes
// the next penalty.
func TestIPShowExpiredRow(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	expired := seedExpiredIPBlock(t, store, "10.0.0.1", 5)

	stdout, stderr, err := runIPTest(t, dbPath, "show", "10.0.0.1")
	if err != nil {
		t.Fatalf("ip show 10.0.0.1: %v (stderr %q)", err, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("ip show printed %d lines, want a header and one row:\n%s", len(lines), stdout)
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 5 {
		t.Fatalf("shown row = %q, want 5 columns", lines[1])
	}
	want := []string{expired.IP, "5", expired.BlockedUntil, "expired", expired.UpdatedAt}
	for i, field := range want {
		if fields[i] != field {
			t.Errorf("shown row column %d = %q, want %q", i, fields[i], field)
		}
	}
}

// TestIPShowMiss is the exit-code half of the verb: the same wording and code unblock uses
// for the same situation, naming the canonical spelling of what was typed.
func TestIPShowMiss(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	stdout, stderr, err := runIPTest(t, dbPath, "show", "198.51.100.4")
	if err == nil {
		t.Fatalf("ip show of an unblocked address: no error, want a refusal (stdout %q)", stdout)
	}
	if got := strings.TrimSpace(stderr); got != "no block for 198.51.100.4" {
		t.Errorf("ip show miss stderr = %q, want %q", got, "no block for 198.51.100.4")
	}
	if stdout != "" {
		t.Errorf("ip show miss stdout = %q, want nothing", stdout)
	}

	if _, stderr, err := runIPTest(t, dbPath, "show", "::ffff:198.51.100.4"); err == nil ||
		strings.TrimSpace(stderr) != "no block for 198.51.100.4" {
		t.Errorf("ip show ::ffff:198.51.100.4 stderr = %q, err %v; want the miss named as %s",
			stderr, err, "198.51.100.4")
	}

	if _, ok := ipBlockRow(t, store, "203.0.113.7"); !ok {
		t.Errorf("203.0.113.7 vanished along with the miss")
	}
	if _, _, code := runDispatch(t, "ip", "show", "--db", dbPath, "198.51.100.4"); code != 1 {
		t.Errorf("sandbin ip show of a miss exited %d, want 1", code)
	}
}

// TestIPShowCanonicalises is the same spelling test unblock gets: the server stores
// canonical keys, so ::ffff:1.2.3.4 and fe80::1%eth0 must reach them.
func TestIPShowCanonicalises(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seeded := map[string]IPBlock{
		"1.2.3.4":     seedLiveIPBlock(t, store, "1.2.3.4", 1),
		"fe80::1":     seedLiveIPBlock(t, store, "fe80::1", 1),
		"2001:db8::1": seedLiveIPBlock(t, store, "2001:db8::1", 1),
	}

	for _, c := range []struct{ typed, stored string }{
		{"::ffff:1.2.3.4", "1.2.3.4"},
		{"fe80::1%eth0", "fe80::1"},
		{"2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
	} {
		stdout, stderr, err := runIPTest(t, dbPath, "show", c.typed)
		if err != nil {
			t.Fatalf("ip show %q: %v (stderr %q)", c.typed, err, stderr)
		}
		want := seeded[c.stored]
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("ip show %q printed %d lines, want a header and the %s row:\n%s", c.typed, len(lines), c.stored, stdout)
		}
		fields := strings.Fields(lines[1])
		if len(fields) != 5 {
			t.Fatalf("ip show %q row = %q, want 5 columns", c.typed, lines[1])
		}
		if fields[0] != want.IP || fields[1] != "1" || fields[2] != want.BlockedUntil || fields[4] != want.UpdatedAt {
			t.Errorf("ip show %q printed %q, want the %s row with 1 failure", c.typed, lines[1], c.stored)
		}
	}
}

// TestIPShowRefusesBadArguments is unblock's argument refusals, with show named in them.
func TestIPShowRefusesBadArguments(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "1.2.3.4", 1)

	for _, arg := range []string{"not-an-ip", "[1.2.3.4]", "1.2.3.4:8080", "203.0.113.7/32", ""} {
		stdout, stderr, err := runIPTest(t, dbPath, "show", arg)
		if err == nil {
			t.Errorf("ip show %q: no error, want a refusal", arg)
			continue
		}
		if !strings.Contains(stderr, "is not an IP address") {
			t.Errorf("ip show %q stderr = %q, want a refusal naming the argument", arg, stderr)
		}
		if stdout != "" {
			t.Errorf("ip show %q stdout = %q, want nothing", arg, stdout)
		}
	}

	if _, stderr, err := runIPTest(t, dbPath, "show"); err == nil || !strings.Contains(stderr, "show needs an IP address") {
		t.Errorf("ip show with no argument stderr = %q, err %v; want a refusal", stderr, err)
	}

	stdout, stderr, err := runIPTest(t, dbPath, "show", "1.2.3.4", "5.6.7.8")
	if err == nil || !strings.Contains(stderr, "show takes one IP address") {
		t.Errorf("ip show with two arguments stderr = %q, err %v; want a refusal", stderr, err)
	}
	if stdout != "" {
		t.Errorf("ip show with two arguments stdout = %q, want nothing", stdout)
	}

	// Flags typed after the address are refused too, because Go's flag parser stops at
	// the first non-flag argument.
	if _, stderr, err := runIPTest(t, dbPath, "show", "1.2.3.4", "--db", "elsewhere.db"); err == nil || !strings.Contains(stderr, "after its flags") {
		t.Errorf("ip show 1.2.3.4 --db stderr = %q, err %v; want a refusal naming the order", stderr, err)
	}

	if failures, ok := ipBlockRow(t, store, "1.2.3.4"); !ok || failures != 1 {
		t.Errorf("1.2.3.4 = failures %d, ok %v; want the refused runs to change nothing", failures, ok)
	}
}

// TestIPShowIsReadOnly is the verb's point: after a hit and a miss the table is exactly as
// it was, so a later list still prints both rows with their counts.
func TestIPShowIsReadOnly(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	live := seedLiveIPBlock(t, store, "203.0.113.7", 2)
	expired := seedExpiredIPBlock(t, store, "10.0.0.1", 5)

	if _, _, err := runIPTest(t, dbPath, "show", "203.0.113.7"); err != nil {
		t.Fatalf("ip show 203.0.113.7: %v", err)
	}
	if _, _, err := runIPTest(t, dbPath, "show", "198.51.100.4"); err == nil {
		t.Fatal("ip show of a free address: no error, want a refusal")
	}

	stdout, stderr, err := runIPTest(t, dbPath, "list")
	if err != nil {
		t.Fatalf("ip list after show: %v (stderr %q)", err, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("ip list after show printed %d lines, want a header and two rows:\n%s", len(lines), stdout)
	}
	for i, want := range [][]string{
		{expired.IP, "5", "expired"},
		{live.IP, "2"},
	} {
		fields := strings.Fields(lines[i+1])
		if len(fields) != 5 {
			t.Fatalf("row %d = %q, want 5 columns", i, lines[i+1])
		}
		if fields[0] != want[0] || fields[1] != want[1] {
			t.Errorf("row %d = %q, want the %s row with %s failures", i, lines[i+1], want[0], want[1])
		}
		if len(want) > 2 && fields[3] != want[2] {
			t.Errorf("row %d REMAINING = %q, want %s", i, fields[3], want[2])
		}
	}
}

// TestIPShowHelpIsNotAFailure covers show asking for its own flag list.
func TestIPShowHelpIsNotAFailure(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "1.2.3.4", 1)

	_, stderr, err := runIPTest(t, dbPath, "show", "--help")
	if err != nil {
		t.Fatalf("ip show --help: %v (stderr %q), want no error", err, stderr)
	}
	if !strings.Contains(stderr, "Usage of sandbin ip show:") || !strings.Contains(stderr, "-db") {
		t.Errorf("ip show --help stderr = %q, want the flag list", stderr)
	}
	if failures, ok := ipBlockRow(t, store, "1.2.3.4"); !ok || failures != 1 {
		t.Errorf("1.2.3.4 = failures %d, ok %v; want the help run to change nothing", failures, ok)
	}
}

func TestIPUnblockRemovesOnlyItsRow(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)
	kept := seedLiveIPBlock(t, store, "198.51.100.4", 1)

	stdout, stderr, err := runIPTest(t, dbPath, "unblock", "203.0.113.7")
	if err != nil {
		t.Fatalf("ip unblock 203.0.113.7: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "unblocked 203.0.113.7" {
		t.Errorf("ip unblock stdout = %q, want %q", got, "unblocked 203.0.113.7")
	}

	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].IP != kept.IP {
		t.Errorf("after unblock the table holds %+v, want only %s", blocks, kept.IP)
	}
}

func TestIPUnblockMiss(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)

	stdout, stderr, err := runIPTest(t, dbPath, "unblock", "198.51.100.4")
	if err == nil {
		t.Fatalf("ip unblock of an unblocked address: no error, want a refusal (stdout %q)", stdout)
	}
	if got := strings.TrimSpace(stderr); got != "no block for 198.51.100.4" {
		t.Errorf("ip unblock miss stderr = %q, want %q", got, "no block for 198.51.100.4")
	}
	if stdout != "" {
		t.Errorf("ip unblock miss stdout = %q, want nothing", stdout)
	}
	if _, ok := ipBlockRow(t, store, "203.0.113.7"); !ok {
		t.Errorf("203.0.113.7 vanished along with the miss")
	}
	if _, _, code := runDispatch(t, "ip", "unblock", "198.51.100.4", "--db", dbPath); code != 1 {
		t.Errorf("sandbin ip unblock of a miss exited %d, want 1", code)
	}
}

// TestIPUnblockCanonicalises is the operator's spelling test: the server stores canonical
// keys, so ::ffff:1.2.3.4 and fe80::1%eth0 must reach them.
func TestIPUnblockCanonicalises(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "1.2.3.4", 1)
	seedLiveIPBlock(t, store, "fe80::1", 1)
	seedLiveIPBlock(t, store, "2001:db8::1", 1)

	for _, c := range []struct{ typed, stored string }{
		{"::ffff:1.2.3.4", "1.2.3.4"},
		{"fe80::1%eth0", "fe80::1"},
		{"2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
	} {
		stdout, stderr, err := runIPTest(t, dbPath, "unblock", c.typed)
		if err != nil {
			t.Fatalf("ip unblock %q: %v (stderr %q)", c.typed, err, stderr)
		}
		if got := strings.TrimSpace(stdout); got != "unblocked "+c.stored {
			t.Errorf("ip unblock %q printed %q, want %q", c.typed, got, "unblocked "+c.stored)
		}
		if _, ok := ipBlockRow(t, store, c.stored); ok {
			t.Errorf("ip unblock %q left the %s row in place", c.typed, c.stored)
		}
	}
}

func TestIPUnblockRefusesNonAddresses(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "1.2.3.4", 1)

	for _, arg := range []string{"not-an-ip", "[1.2.3.4]", "1.2.3.4:8080", "203.0.113.7/32", ""} {
		_, stderr, err := runIPTest(t, dbPath, "unblock", arg)
		if err == nil {
			t.Errorf("ip unblock %q: no error, want a refusal", arg)
			continue
		}
		if !strings.Contains(stderr, "is not an IP address") {
			t.Errorf("ip unblock %q stderr = %q, want a refusal naming the argument", arg, stderr)
		}
	}
	if failures, ok := ipBlockRow(t, store, "1.2.3.4"); !ok || failures != 1 {
		t.Errorf("1.2.3.4 = failures %d, ok %v; want the refused run to change nothing", failures, ok)
	}
}

func TestIPUnblockArgumentCount(t *testing.T) {
	dbPath, _ := newIPTestStore(t)

	_, stderr, err := runIPTest(t, dbPath, "unblock")
	if err == nil || !strings.Contains(stderr, "unblock needs an IP address") {
		t.Errorf("ip unblock with no argument stderr = %q, err %v; want a refusal", stderr, err)
	}

	stdout, stderr, err := runIPTest(t, dbPath, "unblock", "1.2.3.4", "5.6.7.8")
	if err == nil || !strings.Contains(stderr, "unblock takes one IP address") {
		t.Errorf("ip unblock with two arguments stderr = %q, err %v; want a refusal", stderr, err)
	}
	if stdout != "" {
		t.Errorf("ip unblock with two arguments stdout = %q, want nothing", stdout)
	}

	// Flags typed after the address are refused too, because Go's flag parser stops at
	// the first non-flag argument.
	if _, stderr, err := runIPTest(t, dbPath, "unblock", "1.2.3.4", "--json"); err == nil || !strings.Contains(stderr, "after its flags") {
		t.Errorf("ip unblock 1.2.3.4 --json stderr = %q, err %v; want a refusal naming the order", stderr, err)
	}
}

func TestIPClearCounts(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "10.0.0.1", 1)
	seedLiveIPBlock(t, store, "203.0.113.7", 2)
	seedExpiredIPBlock(t, store, "198.51.100.4", 3)

	stdout, stderr, err := runIPTest(t, dbPath, "clear")
	if err != nil {
		t.Fatalf("ip clear: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "cleared 3 blocks" {
		t.Errorf("ip clear stdout = %q, want %q", got, "cleared 3 blocks")
	}
	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("after ip clear the table holds %d rows, want 0", len(blocks))
	}

	stdout, stderr, err = runIPTest(t, dbPath, "clear")
	if err != nil {
		t.Fatalf("ip clear on an empty table: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "cleared 0 blocks" {
		t.Errorf("ip clear on an empty table = %q, want %q", got, "cleared 0 blocks")
	}

	if _, _, code := runDispatch(t, "ip", "clear", "--db", dbPath); code != 0 {
		t.Errorf("sandbin ip clear on an empty table exited %d, want 0", code)
	}
	if _, stderr, err := runIPTest(t, dbPath, "clear", "extra"); err == nil || !strings.Contains(stderr, "clear takes no arguments") {
		t.Errorf("ip clear extra stderr = %q, err %v; want a refusal", stderr, err)
	}
}

// TestIPJSONIsListOnly keeps --json a flag of list: show, unblock and clear refuse it.
func TestIPJSONIsListOnly(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "1.2.3.4", 1)

	for _, verb := range []string{"show 1.2.3.4", "unblock 1.2.3.4", "clear"} {
		args := strings.Split(verb, " ")
		_, stderr, err := runIPTest(t, dbPath, append([]string{args[0], "--json"}, args[1:]...)...)
		if err == nil {
			t.Errorf("ip %s --json: no error, want a flag refusal", verb)
			continue
		}
		if !strings.Contains(stderr, "flag provided but not defined: -json") {
			t.Errorf("ip %s --json stderr = %q, want the flag refusal", verb, stderr)
		}
	}
	if failures, ok := ipBlockRow(t, store, "1.2.3.4"); !ok || failures != 1 {
		t.Errorf("1.2.3.4 = failures %d, ok %v; want the refused runs to change nothing", failures, ok)
	}
}

// TestIPDBFlagRunsEveryVerbOnOneFile is the point of --db: the ip verbs read and write the
// very file serve uses, by default the sandbin.db of the working directory.
func TestIPDBFlagRunsEveryVerbOnOneFile(t *testing.T) {
	dbPath, store := newIPTestStore(t)
	seedLiveIPBlock(t, store, "203.0.113.7", 1)
	seedLiveIPBlock(t, store, "198.51.100.4", 2)

	for _, verb := range [][]string{{"list"}, {"show", "203.0.113.7"}, {"unblock", "203.0.113.7"}, {"clear"}} {
		stdout, stderr, err := runIPTest(t, dbPath, verb...)
		if err != nil {
			t.Fatalf("ip %s --db %s: %v (stderr %q)", verb[0], dbPath, err, stderr)
		}
		if !strings.Contains(stdout, "203.0.113.7") && !strings.Contains(stdout, "cleared") {
			t.Errorf("ip %s --db %s stdout = %q, want the rows of that file", verb[0], dbPath, stdout)
		}
	}

	// With no --db at all the verbs use the same relative default serve uses.
	t.Chdir(t.TempDir())
	defaultStore, err := openStore("sandbin.db")
	if err != nil {
		t.Fatalf("open the default db: %v", err)
	}
	defer defaultStore.Close()
	seedLiveIPBlock(t, defaultStore, "203.0.113.7", 1)

	var stdout, stderr bytes.Buffer
	if err := runIP([]string{"list"}, &stdout, &stderr); err != nil {
		t.Fatalf("ip list with no --db: %v (stderr %q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "203.0.113.7") {
		t.Errorf("ip list with no --db stdout = %q, want the row of sandbin.db", stdout.String())
	}

	// show reads the same default file.
	var showStdout, showStderr bytes.Buffer
	if err := runIP([]string{"show", "203.0.113.7"}, &showStdout, &showStderr); err != nil {
		t.Fatalf("ip show with no --db: %v (stderr %q)", err, showStderr.String())
	}
	if !strings.Contains(showStdout.String(), "203.0.113.7") {
		t.Errorf("ip show with no --db stdout = %q, want the row of sandbin.db", showStdout.String())
	}
}
