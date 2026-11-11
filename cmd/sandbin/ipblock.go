package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

const (
	blockStep = 30 * time.Minute // each failure adds this much
	blockMax  = 24 * time.Hour   // a single block never gets longer than this

	// shortcut: an address idle for 30 days forgets its failure count; raise the window if
	// long-lived deployments need escalation to survive that long.
	blockIdlePrune = 30 * 24 * time.Hour
)

// proxyList holds the configured addresses whose X-Forwarded-For is believed.
type proxyList []netip.Prefix

// loadTrustedProxies merges the comma separated list with the file list, file first.
// Blank lines, blank CSV entries and '#' comment lines are ignored; a bad entry or an
// unreadable file is an error naming the offending value, so the server can refuse to
// start instead of quietly believing less or more than the operator configured.
func loadTrustedProxies(csv, path string) (proxyList, error) {
	var list proxyList
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy file %q: %w", path, err)
		}
		defer f.Close()
		lines := bufio.NewScanner(f)
		for n := 1; lines.Scan(); n++ {
			// TrimSpace covers the leftover \r of a CRLF file and trailing spaces.
			entry := strings.TrimSpace(lines.Text())
			if entry == "" || strings.HasPrefix(entry, "#") {
				continue
			}
			prefix, err := parseProxyEntry(entry)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy file %q line %d: %v", path, n, err)
			}
			list = append(list, prefix)
		}
		if err := lines.Err(); err != nil {
			return nil, fmt.Errorf("trusted proxy file %q: %w", path, err)
		}
	}
	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := parseProxyEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %v", entry, err)
		}
		list = append(list, prefix)
	}
	return list, nil
}

// parseProxyEntry reads one IP or CIDR. A bare IP becomes a /32 or /128 prefix, and a
// bare IP keeps no zone so an entry matches a socket regardless of its interface.
func parseProxyEntry(entry string) (netip.Prefix, error) {
	if strings.ContainsRune(entry, '/') {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("entry %q is not an IP or CIDR: %w", entry, err)
		}
		return prefix, nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("entry %q is not an IP or CIDR: %w", entry, err)
	}
	addr = addr.WithZone("")
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	return netip.PrefixFrom(addr, 128), nil
}

// shortcut: a linear scan per key-checked request; build a prefix tree if a list grows
// past a few thousand entries.
func (l proxyList) contains(ip netip.Addr) bool {
	for _, prefix := range l {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// containsHost reports whether a socket host string names a configured proxy. The zone
// is dropped so a socket like fe80::1%eth0 can match a fe80::/64 entry.
func (l proxyList) containsHost(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return l.contains(addr.WithZone(""))
}

// clientIP charges a request to the client address, which is the header's rightmost entry
// when the socket itself belongs to a proxy we trust. Loopback, private and link-local
// sockets are always trusted and checked first; the configured list only adds to them.
func clientIP(r *http.Request, proxies proxyList) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLocalAddr(host) && !proxies.containsHost(host) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		entries := strings.Split(xff, ",")
		// The rightmost entry is the one a proxy appends; anything left of it is client fiction.
		if last := strings.TrimSpace(entries[len(entries)-1]); last != "" {
			return last
		}
	}
	return host
}

// isLocalAddr reports whether a proxy on the same machine or network could have
// appended X-Forwarded-For for this socket host.
func isLocalAddr(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
