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
	flag.Parse()

	// Without a key there is no way to tell clients apart, so refuse to start.
	if *apiKey == "" {
		log.Fatal("sandbin: no API key: pass --api-key or set SANDBIN_API_KEY")
	}

	store, err := openStore(*dbPath)
	if err != nil {
		log.Fatalf("sandbin: open store: %v", err)
	}
	defer store.Close()

	srv := &server{store: store, apiKey: []byte(*apiKey)}
	log.Printf("sandbin listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.mux()))
}
