package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// ipUsage is what an ip run without a working verb gets.
const ipUsage = `usage:
  sandbin ip list [--json]     every row of the block table
  sandbin ip unblock <ip>      delete one address's block
  sandbin ip clear             delete every block
Each verb takes --db <file> (default sandbin.db) and needs no API key.
`

// runIP is the whole sandbin ip family, so a test can hand it args and writers.
// Every failure is reported on stderr; runIP's caller only maps it to an exit code.
func runIP(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, ipUsage)
		return ipError(stderr, "no command: pass list, unblock <ip> or clear")
	}

	var err error
	switch args[0] {
	case "list":
		err = ipList(args[1:], stdout, stderr)
	case "unblock":
		err = ipUnblock(args[1:], stdout, stderr)
	case "clear":
		err = ipClear(args[1:], stdout, stderr)
	case "help", "--help":
		fmt.Fprint(stdout, ipUsage)
	default:
		fmt.Fprint(stderr, ipUsage)
		err = ipError(stderr, "unknown command %q: pass list, unblock <ip> or clear", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		// The verb's flag list has already been printed; asking for it is not a failure.
		return nil
	}
	return err
}

func ipList(args []string, stdout, stderr io.Writer) error {
	fs, dbPath := ipFlagSet("list", stderr)
	jsonOut := fs.Bool("json", false, "print the rows as a json array")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if extra := fs.Args(); len(extra) > 0 {
		return ipError(stderr, "list takes no arguments, got %q", strings.Join(extra, " "))
	}

	store, err := openIPStore(*dbPath, stderr)
	if err != nil {
		return err
	}
	defer store.Close()

	blocks, err := store.ListBlocks()
	if err != nil {
		return ipError(stderr, "list blocks: %v", err)
	}

	if *jsonOut {
		if err := writeBlocksJSON(stdout, blocks); err != nil {
			return ipError(stderr, "write json: %v", err)
		}
		return nil
	}
	if len(blocks) == 0 {
		fmt.Fprintln(stdout, "no blocked addresses")
		return nil
	}

	now := time.Now()
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "IP\tFAILURES\tBLOCKED UNTIL\tREMAINING\tLAST FAILURE")
	for _, b := range blocks {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", b.IP, b.Failures, b.BlockedUntil, remainingText(b.BlockedUntil, now), b.UpdatedAt)
	}
	return w.Flush()
}

func ipUnblock(args []string, stdout, stderr io.Writer) error {
	fs, dbPath := ipFlagSet("unblock", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return ipError(stderr, "unblock needs an IP address")
	}
	if len(rest) > 1 {
		// shortcut: Go's flag parser stops at the first non-flag argument, so a verb's
		// flags have to be typed before the address it takes.
		return ipError(stderr, "unblock takes one IP address after its flags, got %q", strings.Join(rest, " "))
	}

	ip, err := canonicalIP(rest[0])
	if err != nil {
		return ipError(stderr, "%s", err)
	}

	store, err := openIPStore(*dbPath, stderr)
	if err != nil {
		return err
	}
	defer store.Close()

	gone, err := store.UnblockIP(ip)
	if err != nil {
		return ipError(stderr, "unblock %s: %v", ip, err)
	}
	if !gone {
		return ipError(stderr, "no block for %s", ip)
	}
	fmt.Fprintf(stdout, "unblocked %s\n", ip)
	return nil
}

func ipClear(args []string, stdout, stderr io.Writer) error {
	fs, dbPath := ipFlagSet("clear", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if extra := fs.Args(); len(extra) > 0 {
		return ipError(stderr, "clear takes no arguments, got %q", strings.Join(extra, " "))
	}

	store, err := openIPStore(*dbPath, stderr)
	if err != nil {
		return err
	}
	defer store.Close()

	cleared, err := store.ClearBlocks()
	if err != nil {
		return ipError(stderr, "clear blocks: %v", err)
	}
	fmt.Fprintf(stdout, "cleared %d blocks\n", cleared)
	return nil
}

// ipFlagSet is one verb's flags. --db names the same file serve uses, and reading that
// file is the only authority these verbs need: they make no network request.
func ipFlagSet(verb string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("sandbin ip "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("db", "sandbin.db", "sqlite file holding all bins")
}

// openIPStore opens the --db file the way serve opens it. A --db that does not
// exist yet is still created, exactly as serve would create it, but the run says
// so on stderr: a typo must not read as a silent all-clear over the real file.
func openIPStore(dbPath string, stderr io.Writer) (*Store, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "warning: %s does not exist; created empty\n", dbPath)
	}
	store, err := openStore(dbPath)
	if err != nil {
		return nil, ipError(stderr, "open store: %v", err)
	}
	return store, nil
}

// ipError writes a failure to stderr and returns it, so each failure is reported once.
func ipError(stderr io.Writer, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	fmt.Fprintln(stderr, err)
	return err
}

// remainingText says how much of a block is left at now. An expired row is listed on
// purpose because its failure count still shapes the next penalty. An unparsable
// blocked_until reads as expired, the way IsBlocked reads it.
func remainingText(blockedUntil string, now time.Time) string {
	until, err := time.Parse(time.RFC3339Nano, blockedUntil)
	if err != nil || !until.After(now) {
		return "expired"
	}
	return time.Until(until).Round(time.Second).String()
}

// jsonIPBlock is one row in the --json shape: exactly these four fields.
type jsonIPBlock struct {
	IP           string `json:"ip"`
	Failures     int    `json:"failures"`
	BlockedUntil string `json:"blocked_until"`
	UpdatedAt    string `json:"updated_at"`
}

// writeBlocksJSON renders the rows as a json array, [] when there is no row at all.
func writeBlocksJSON(stdout io.Writer, blocks []IPBlock) error {
	rows := make([]jsonIPBlock, 0, len(blocks))
	for _, b := range blocks {
		rows = append(rows, jsonIPBlock{IP: b.IP, Failures: b.Failures, BlockedUntil: b.BlockedUntil, UpdatedAt: b.UpdatedAt})
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(out))
	return nil
}

// canonicalIP spells an address the way clientIP spells a charged one, so an operator's
// ::ffff:1.2.3.4 or fe80::1%eth0 reaches the key the server stored.
func canonicalIP(raw string) (string, error) {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", fmt.Errorf("%q is not an IP address", raw)
	}
	return addr.Unmap().WithZone("").String(), nil
}
