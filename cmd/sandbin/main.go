// Command sandbin is a PUT/GET server that lets sandboxed CLI agents publish
// HTML, markdown and ANSI previews. See README.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// usage is what every run without a working verb gets.
const usage = `usage:
  sandbin serve [--api-key k] [--db f] [--addr a] [--trusted-proxy l] [--trusted-proxy-file f]
  sandbin ip list [--db f] [--json]
  sandbin ip unblock [--db f] <ip>
  sandbin ip clear [--db f]
  sandbin help | --help
`

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch picks the verb from the first argument and hands the rest to that verb's
// flags. A run with no verb is refused: every invocation says which verb it means.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stderr)
	case "ip":
		// runIP has already reported its own failure on stderr.
		if err := runIP(args[1:], stdout, stderr); err != nil {
			return 1
		}
		return 0
	case "help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "sandbin: unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 1
	}
}

// serveConfig is what serve's flags say the server should be.
type serveConfig struct {
	apiKey, dbPath, addr, trustedProxy, trustedProxyFile string
}

// parseServeFlags parses serve's flags on their own FlagSet, so a test can check the
// parsing without a listening server. Every flag still falls back to its environment
// variable, which is how the container sets its settings without a command line.
func parseServeFlags(args []string, stderr io.Writer) (serveConfig, error) {
	fs := flag.NewFlagSet("sandbin serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg serveConfig
	fs.StringVar(&cfg.apiKey, "api-key", os.Getenv("SANDBIN_API_KEY"), "pre-shared API key clients send as X-API-Key")
	fs.StringVar(&cfg.dbPath, "db", "sandbin.db", "sqlite file holding all bins")
	fs.StringVar(&cfg.addr, "addr", ":8080", "address to listen on")
	fs.StringVar(&cfg.trustedProxy, "trusted-proxy", os.Getenv("SANDBIN_TRUSTED_PROXIES"),
		"comma-separated proxy IPs and CIDRs whose X-Forwarded-For is believed")
	fs.StringVar(&cfg.trustedProxyFile, "trusted-proxy-file", os.Getenv("SANDBIN_TRUSTED_PROXIES_FILE"),
		"file of trusted proxy IPs and CIDRs, one per line, # comments allowed")
	if err := fs.Parse(args); err != nil {
		return serveConfig{}, err
	}
	if extra := fs.Args(); len(extra) > 0 {
		// A stray argument says nothing the server reads, so it is refused like an
		// unknown flag rather than ignored.
		err := fmt.Errorf("serve takes no arguments, got %q", strings.Join(extra, " "))
		fmt.Fprintln(stderr, err)
		return serveConfig{}, err
	}
	return cfg, nil
}

// runServe starts the server with the parsed settings. The refuse-to-start guards and
// the log line are the ones main() has always had.
func runServe(args []string, stderr io.Writer) int {
	cfg, err := parseServeFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// serve's flag list has already been printed; asking for it is not a failure.
			return 0
		}
		return 1
	}

	// Without a key there is no way to tell clients apart, so refuse to start.
	if cfg.apiKey == "" {
		log.Fatal("sandbin: no API key: pass --api-key or set SANDBIN_API_KEY")
	}

	// An unreadable or unparsable proxy list would silently change who is believed,
	// so it gets the same refuse-to-start guard as the missing key.
	proxies, err := loadTrustedProxies(cfg.trustedProxy, cfg.trustedProxyFile)
	if err != nil {
		log.Fatalf("sandbin: %v", err)
	}

	store, err := openStore(cfg.dbPath)
	if err != nil {
		log.Fatalf("sandbin: open store: %v", err)
	}
	defer store.Close()

	srv := &server{store: store, apiKey: []byte(cfg.apiKey), trustedProxies: proxies}
	log.Printf("sandbin listening on %s", cfg.addr)
	log.Fatal(http.ListenAndServe(cfg.addr, srv))
	return 1 // unreachable: log.Fatal exits the process first
}
