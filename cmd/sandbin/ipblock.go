package main

import (
	"net"
	"net/http"
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

// clientIP is the address a failure is charged to. The header is only believed when the
// connection came from loopback or a private network, i.e. a local proxy put it there.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLocalAddr(host) {
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
