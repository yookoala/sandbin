// Command sandbin is a PUT/GET server that lets sandboxed CLI agents publish
// HTML, markdown and ANSI previews. See README.md.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	apiKey := flag.String("api-key", os.Getenv("SANDBIN_API_KEY"), "pre-shared API key clients send as X-API-Key")
	dbPath := flag.String("db", "sandbin.db", "sqlite file holding all bins")
	addr := flag.String("addr", ":8080", "address to listen on")
	trustedProxy := flag.String("trusted-proxy", os.Getenv("SANDBIN_TRUSTED_PROXIES"),
		"comma-separated proxy IPs and CIDRs whose X-Forwarded-For is believed")
	trustedProxyFile := flag.String("trusted-proxy-file", os.Getenv("SANDBIN_TRUSTED_PROXIES_FILE"),
		"file of trusted proxy IPs and CIDRs, one per line, # comments allowed")
	flag.Parse()

	// Without a key there is no way to tell clients apart, so refuse to start.
	if *apiKey == "" {
		log.Fatal("sandbin: no API key: pass --api-key or set SANDBIN_API_KEY")
	}

	// An unreadable or unparsable proxy list would silently change who is believed,
	// so it gets the same refuse-to-start guard as the missing key.
	proxies, err := loadTrustedProxies(*trustedProxy, *trustedProxyFile)
	if err != nil {
		log.Fatalf("sandbin: %v", err)
	}

	store, err := openStore(*dbPath)
	if err != nil {
		log.Fatalf("sandbin: open store: %v", err)
	}
	defer store.Close()

	srv := &server{store: store, apiKey: []byte(*apiKey), trustedProxies: proxies}
	log.Printf("sandbin listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv))
}
