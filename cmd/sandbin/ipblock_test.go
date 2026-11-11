package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPNoPort(t *testing.T) {
	// A socket address without a port is used as it stands.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7"
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.7")
	}

	// A loopback socket without a port is still local, so the header is honoured.
	r.RemoteAddr = "127.0.0.1"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r); got != "198.51.100.4" {
		t.Errorf("clientIP = %q, want the forwarded address 198.51.100.4", got)
	}
}

func TestClientIPIPv6(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::1]:1234"
	if got := clientIP(r); got != "::1" {
		t.Errorf("clientIP = %q, want %q", got, "::1")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want %q from a loopback socket", got, "203.0.113.7")
	}

	// A public IPv6 socket ignores the header.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r); got != "2001:db8::1" {
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
		if got := clientIP(r); got != c.want {
			t.Errorf("X-Forwarded-For %q: clientIP = %q, want %q", c.xff, got, c.want)
		}
	}
}

func TestClientIPPublicSocketIgnoresXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r); got != "8.8.8.8" {
		t.Errorf("clientIP = %q, want the socket address 8.8.8.8", got)
	}

	// An unparsable socket host falls back to the raw host part, header or not.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-a-host:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r); got != "not-a-host" {
		t.Errorf("clientIP = %q, want the host part of the raw remote address", got)
	}
}

func TestClientIPLinkLocalSocket(t *testing.T) {
	// A link-local socket looks like a local proxy hop, so the header is honoured.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "169.254.5.5:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
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
