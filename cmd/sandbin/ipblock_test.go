package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientIPNoPort(t *testing.T) {
	// A socket address without a port is used as it stands.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7"
	if got := clientIP(r, nil); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.7")
	}

	// A loopback socket without a port is still local, so the header is honoured.
	r.RemoteAddr = "127.0.0.1"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, nil); got != "198.51.100.4" {
		t.Errorf("clientIP = %q, want the forwarded address 198.51.100.4", got)
	}
}

func TestClientIPIPv6(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::1]:1234"
	if got := clientIP(r, nil); got != "::1" {
		t.Errorf("clientIP = %q, want %q", got, "::1")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r, nil); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want %q from a loopback socket", got, "203.0.113.7")
	}

	// A public IPv6 socket ignores the header.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, nil); got != "2001:db8::1" {
		t.Errorf("clientIP = %q, want the socket address 2001:db8::1", got)
	}
}

func TestClientIPXFFForms(t *testing.T) {
	// The socket is private, so the header is believed, but only its rightmost
	// usable entry.
	cases := []struct {
		xff  string
		want string
	}{
		{"", "10.0.0.1"},                                 // empty header
		{",", "10.0.0.1"},                                // only a comma
		{", ,", "10.0.0.1"},                              // commas and spaces
		{"   ", "10.0.0.1"},                              // only whitespace
		{"203.0.113.7", "203.0.113.7"},                   // one entry
		{"203.0.113.7, 198.51.100.4", "198.51.100.4"},    // rightmost entry wins
		{" 203.0.113.7 , 198.51.100.4 ", "198.51.100.4"}, // entries trimmed
		{"203.0.113.7,", "10.0.0.1"},                     // trailing comma leaves nothing
		{"203.0.113.7, 198.51.100.4, 192.0.2.66", "192.0.2.66"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r, nil); got != c.want {
			t.Errorf("X-Forwarded-For %q: clientIP = %q, want %q", c.xff, got, c.want)
		}
	}
}

func TestClientIPPublicSocketIgnoresXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, nil); got != "8.8.8.8" {
		t.Errorf("clientIP = %q, want the socket address 8.8.8.8", got)
	}

	// An unparsable socket host falls back to the raw host part, header or not.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-a-host:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, nil); got != "not-a-host" {
		t.Errorf("clientIP = %q, want the host part of the raw remote address", got)
	}
}

func TestClientIPLinkLocalSocket(t *testing.T) {
	// A link-local socket looks like a local proxy hop, so the header is honoured.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "169.254.5.5:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r, nil); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want %q from a link-local socket", got, "203.0.113.7")
	}
}

func TestIsLocalAddr(t *testing.T) {
	local := []string{"127.0.0.1", "127.0.0.56", "::1", "0:0:0:0:0:0:0:1",
		"10.0.0.1", "192.168.1.1", "172.16.0.1", "172.31.255.255",
		"169.254.1.1", "fe80::1", "fc00::1"}
	public := []string{"8.8.8.8", "203.0.113.7", "172.15.0.1", "172.32.0.1",
		"169.253.1.1", "2001:db8::1", "", "not-an-ip", "127.1"}
	for _, host := range local {
		if !isLocalAddr(host) {
			t.Errorf("isLocalAddr(%q) = false, want true", host)
		}
	}
	for _, host := range public {
		if isLocalAddr(host) {
			t.Errorf("isLocalAddr(%q) = true, want false", host)
		}
	}
}

// proxyStrings renders a proxyList as prefix strings for comparison.
func proxyStrings(list proxyList) []string {
	var out []string
	for _, prefix := range list {
		out = append(out, prefix.String())
	}
	return out
}

func TestLoadTrustedProxiesCSV(t *testing.T) {
	cases := []struct {
		csv  string
		want []string
	}{
		{"", nil},   // an empty CSV is no extra trust
		{"  ", nil}, // blank entries are skipped
		{"203.0.113.7", []string{"203.0.113.7/32"}},  // a bare IPv4 becomes a /32
		{"2001:db8::1", []string{"2001:db8::1/128"}}, // a bare IPv6 becomes a /128
		{"173.245.48.0/20", []string{"173.245.48.0/20"}},
		{"0.0.0.0/0,::/0", []string{"0.0.0.0/0", "::/0"}}, // full ranges parse
		{"fe80::1%eth0", []string{"fe80::1/128"}},         // a bare entry keeps no zone
	}
	for _, c := range cases {
		list, err := loadTrustedProxies(c.csv, "")
		if err != nil {
			t.Errorf("loadTrustedProxies(%q, \"\") error: %v", c.csv, err)
			continue
		}
		got := proxyStrings(list)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("loadTrustedProxies(%q, \"\") = %v, want %v", c.csv, got, c.want)
		}
	}
}

func TestLoadTrustedProxiesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.txt")
	// A real-world looking file: # comments, blank lines, CRLF endings, trailing spaces.
	content := "# Cloudflare IPv4\r\n\r\n173.245.48.0/20\r\n  203.0.113.7   \r\n\r\n# end\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	list, err := loadTrustedProxies("", path)
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	want := []string{"173.245.48.0/20", "203.0.113.7/32"}
	if got := proxyStrings(list); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("file list = %v, want %v", got, want)
	}
}

func TestLoadTrustedProxiesMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	_, err := loadTrustedProxies("", missing)
	if err == nil {
		t.Fatalf("missing file %q: no error, want a refusal naming it", missing)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want it to name %q", err, missing)
	}
}

func TestLoadTrustedProxiesGarbage(t *testing.T) {
	_, err := loadTrustedProxies("203.0.113.7,not-an-ip", "")
	if err == nil {
		t.Fatal("garbage CSV entry: no error, want a refusal naming it")
	}
	if !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("CSV error = %q, want it to name the offending entry", err)
	}

	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte("203.0.113.7\nnot-an-ip\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err = loadTrustedProxies("", path)
	if err == nil {
		t.Fatal("garbage file entry: no error, want a refusal naming it")
	}
	if !strings.Contains(err.Error(), "not-an-ip") || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("file error = %q, want it to name the entry and its line", err)
	}
}

func TestLoadTrustedProxiesMergesFileFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte("203.0.113.7\n198.51.100.0/24\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// The CSV repeats 203.0.113.7; merging keeps both lists with the file first and
	// does not dedup, because duplicates change nothing about contains().
	list, err := loadTrustedProxies("203.0.113.7,2001:db8::1", path)
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	want := []string{"203.0.113.7/32", "198.51.100.0/24", "203.0.113.7/32", "2001:db8::1/128"}
	if got := proxyStrings(list); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("merged list = %v, want %v", got, want)
	}
}

func TestProxyListZoneMatching(t *testing.T) {
	list, err := loadTrustedProxies("fe80::/64", "")
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	zoned := netip.MustParseAddr("fe80::1%eth0")
	// A socket keeps its interface zone, but matching is zone-free, which is why
	// clientIP strips the zone before calling contains.
	if list.contains(zoned) {
		t.Errorf("contains(fe80::1%%eth0) = true, want false: zones make netip addresses distinct")
	}
	if !list.contains(zoned.WithZone("")) {
		t.Errorf("contains(fe80::1) = false, want true: fe80::1%%eth0 must match the fe80::/64 entry")
	}
	if !list.containsHost("fe80::1%eth0") {
		t.Errorf("containsHost(fe80::1%%eth0) = false, want true")
	}
	if list.containsHost("not-a-host") {
		t.Errorf("containsHost(\"not-a-host\") = true, want false for an unparsable socket host")
	}
}

func TestClientIPChargingWithProxyList(t *testing.T) {
	cases := []struct {
		proxies    string // CSV loaded for this case
		remoteAddr string
		xff        string
		want       string
		why        string
	}{
		{"173.245.48.0/20", "173.245.48.9:43210", "203.0.113.7, 198.51.100.4", "198.51.100.4",
			"a public socket inside a trusted /20 is charged to the header's rightmost entry"},
		{"173.245.48.0/20", "173.245.64.9:43210", "203.0.113.7, 198.51.100.4", "173.245.64.9",
			"the same header from a public socket outside the list is charged to the socket"},
		{"", "10.1.2.3:43210", "203.0.113.7", "203.0.113.7",
			"a private socket is charged from the header with no configuration at all"},
		{"198.51.100.7/32", "198.51.100.7:43210", "203.0.113.7", "203.0.113.7",
			"a configured /32 matches its one address exactly"},
		{"198.51.100.7/32", "198.51.100.8:43210", "203.0.113.7", "198.51.100.8",
			"the neighbour of that /32 is not trusted"},
		{"2001:db8::/32", "[2001:db8::5%ens3]:43210", "203.0.113.7", "203.0.113.7",
			"an IPv6 socket zone is stripped so it matches a configured range"},
	}
	for _, c := range cases {
		proxies, err := loadTrustedProxies(c.proxies, "")
		if err != nil {
			t.Fatalf("loadTrustedProxies(%q): %v", c.proxies, err)
		}
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = c.remoteAddr
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r, proxies); got != c.want {
			t.Errorf("%s: RemoteAddr %q, X-Forwarded-For %q, clientIP = %q, want %q",
				c.why, c.remoteAddr, c.xff, got, c.want)
		}
	}
}

// TestClientIPChargesRealAddresses asserts the charged value is a parsed canonical
// address or the socket host, never the header string verbatim. A reviewer measured a
// 200 KB header landing as a 200000-byte ip_blocks primary key and growing the db
// 425984 bytes in one request, with keys like "fe80::1%eth0" and "203.0.113.7:8080"
// that no real socket would ever charge.
func TestClientIPChargesRealAddresses(t *testing.T) {
	cases := []struct {
		xff  string
		want string
		why  string
	}{
		{"2001:DB8::1", "2001:db8::1", "a header address is charged in netip's canonical form"},
		{"fe80::1%eth0", "fe80::1", "the zone is stripped from the charged address"},
		{"::ffff:203.0.113.7", "203.0.113.7", "an IPv4-mapped header address is charged unmapped"},
		{"203.0.113.7:8080", "10.0.0.1", "an entry with a port is not an address, so the socket is charged"},
		{"0);)", "10.0.0.1", "garbage is not an address, so the socket is charged"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		r.Header.Set("X-Forwarded-For", c.xff)
		if got := clientIP(r, nil); got != c.want {
			t.Errorf("X-Forwarded-For %q: clientIP = %q, want %q (%s)", c.xff, got, c.want, c.why)
		}
	}

	// A 200 KB garbage header charges the socket host, and nothing longer than
	// that socket string is charged.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", strings.Repeat("x", 200_000))
	got := clientIP(r, nil)
	if got != "10.0.0.1" {
		t.Errorf("200 KB garbage header: clientIP = %q of %d bytes, want the socket host 10.0.0.1", got, len(got))
	}
	if len(got) > len("10.0.0.1") {
		t.Errorf("charged value %q is longer than the socket string", got)
	}
}

// TestClientIPZeroPrefixTrustsEverySocket is the deliberate footgun test: an operator who
// puts 0.0.0.0/0 (or ::/0) in the list makes every IPv4 (or IPv6) socket header-trusted,
// so any connection can name whichever address gets blocked. sandbin accepts such entries
// as an explicit operator choice, never as a default.
func TestClientIPZeroPrefixTrustsEverySocket(t *testing.T) {
	proxies, err := loadTrustedProxies("0.0.0.0/0", "")
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "8.8.8.8:43210"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, proxies); got != "198.51.100.4" {
		t.Errorf("with 0.0.0.0/0: clientIP = %q, want the header address 198.51.100.4", got)
	}

	// 0.0.0.0/0 says nothing about IPv6: netip prefixes only match their own family,
	// so an IPv6 socket still pays for itself until ::/0 is configured too.
	r = httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "[2001:4860:4860::8888]:43210"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r, proxies); got != "2001:4860:4860::8888" {
		t.Errorf("with 0.0.0.0/0: IPv6 clientIP = %q, want the socket address", got)
	}

	proxies, err = loadTrustedProxies("::/0", "")
	if err != nil {
		t.Fatalf("loadTrustedProxies: %v", err)
	}
	if got := clientIP(r, proxies); got != "198.51.100.4" {
		t.Errorf("with ::/0: clientIP = %q, want the header address 198.51.100.4", got)
	}
}
