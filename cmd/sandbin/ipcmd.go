package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v2"
)

// ipCommand is the sandbin ip family on urfave: the four verbs as subcommands, each with its
// own --db/--socket flags (--json on list), so every verb reads its flags right after its
// name the way it always did.
func ipCommand(stdout, stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	verb := func(name, usage, argsUsage string, jsonOut bool, action cli.ActionFunc) *cli.Command {
		flags := []cli.Flag{
			&cli.StringFlag{Name: "db", Value: "sandbin.db",
				Usage: "serve's db; its control socket <db>.sock is what this verb asks"},
			&cli.StringFlag{Name: "socket", Usage: "control socket of the running serve (default <db>.sock)"},
		}
		if jsonOut {
			flags = append(flags, &cli.BoolFlag{Name: "json", Usage: "print the rows as a json array"})
		}
		cmd := &cli.Command{Name: name, Usage: usage, ArgsUsage: argsUsage, Flags: flags, Action: action}
		cmd.OnUsageError = hook
		return cmd
	}

	cmd := &cli.Command{
		Name:        "ip",
		Usage:       "read and change the block table of a running serve",
		Description: `Each verb reaches the running serve through its control socket, which serve keeps next to --db (default sandbin.db, so sandbin.db.sock); --socket <path> names another socket. A live serve is required and no API key is sent: with nothing listening the verb changes nothing.`,
		Subcommands: []*cli.Command{
			verb("list", "every row of the block table", "", true, func(c *cli.Context) error {
				return ipList(c.String("db"), c.String("socket"), c.Bool("json"), c.Args().Slice(), stdout, stderr)
			}),
			verb("show", "one address's row in the block table", "<ip>", false, func(c *cli.Context) error {
				return ipShow(c.String("db"), c.String("socket"), c.Args().Slice(), stdout, stderr)
			}),
			verb("unblock", "delete one address's block", "<ip>", false, func(c *cli.Context) error {
				return ipUnblock(c.String("db"), c.String("socket"), c.Args().Slice(), stdout, stderr)
			}),
			verb("clear", "delete every block", "", false, func(c *cli.Context) error {
				return ipClear(c.String("db"), c.String("socket"), c.Args().Slice(), stdout, stderr)
			}),
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

// runIP is the test-callable half of the sandbin ip family: hand it the arguments after the
// verb and it runs the whole app. Every failure is reported on stderr; runIP's caller only
// maps the error to an exit code.
func runIP(args []string, stdout, stderr io.Writer) error {
	return newServerApp(stdout, stderr).Run(append([]string{"sandbin", "ip"}, args...))
}

func ipList(dbPath, socketPath string, jsonOut bool, args []string, stdout, stderr io.Writer) error {
	if extra := args; len(extra) > 0 {
		return ipError(stderr, "list takes no arguments, got %q", strings.Join(extra, " "))
	}

	socket := controlSocketPath(dbPath, socketPath)
	code, body, err := controlRequest(stderr, http.MethodGet, socket, "/ip")
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return controlFailure(stderr, "list blocks", socket, code, body)
	}

	if jsonOut {
		// The server's array is already the four-field shape --json prints.
		if _, err := stdout.Write(body); err != nil {
			return ipError(stderr, "write json: %v", err)
		}
		return nil
	}

	blocks, err := parseBlocks(body)
	if err != nil {
		return ipError(stderr, "list blocks: %v", err)
	}
	if len(blocks) == 0 {
		fmt.Fprintln(stdout, "no blocked addresses")
		return nil
	}

	return writeBlocksTable(stdout, blocks, time.Now())
}

// ipShow names one address's row: the same table as list, one row, and the same refusal as
// unblock when the row is not there.
func ipShow(dbPath, socketPath string, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return ipError(stderr, "show needs an IP address")
	}
	if len(args) > 1 {
		// shortcut: Go's flag parser stops at the first non-flag argument, so a verb's
		// flags have to be typed before the address it takes.
		return ipError(stderr, "show takes one IP address after its flags, got %q", strings.Join(args, " "))
	}

	ip, err := canonicalIP(args[0])
	if err != nil {
		return ipError(stderr, "%s", err)
	}

	socket := controlSocketPath(dbPath, socketPath)
	code, body, err := controlRequest(stderr, http.MethodGet, socket, "/ip/"+ip)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return ipError(stderr, "no block for %s", ip)
	}
	if code != http.StatusOK {
		return controlFailure(stderr, "show "+ip, socket, code, body)
	}

	block, err := parseBlock(body)
	if err != nil {
		return ipError(stderr, "show %s: %v", ip, err)
	}
	return writeBlocksTable(stdout, []IPBlock{block}, time.Now())
}

func ipUnblock(dbPath, socketPath string, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return ipError(stderr, "unblock needs an IP address")
	}
	if len(args) > 1 {
		// shortcut: Go's flag parser stops at the first non-flag argument, so a verb's
		// flags have to be typed before the address it takes.
		return ipError(stderr, "unblock takes one IP address after its flags, got %q", strings.Join(args, " "))
	}

	ip, err := canonicalIP(args[0])
	if err != nil {
		return ipError(stderr, "%s", err)
	}

	socket := controlSocketPath(dbPath, socketPath)
	code, body, err := controlRequest(stderr, http.MethodDelete, socket, "/ip/"+ip)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return ipError(stderr, "no block for %s", ip)
	}
	if code != http.StatusOK {
		return controlFailure(stderr, "unblock "+ip, socket, code, body)
	}

	fmt.Fprintf(stdout, "unblocked %s\n", ip)
	return nil
}

func ipClear(dbPath, socketPath string, args []string, stdout, stderr io.Writer) error {
	if extra := args; len(extra) > 0 {
		return ipError(stderr, "clear takes no arguments, got %q", strings.Join(extra, " "))
	}

	socket := controlSocketPath(dbPath, socketPath)
	code, body, err := controlRequest(stderr, http.MethodDelete, socket, "/ip")
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return controlFailure(stderr, "clear blocks", socket, code, body)
	}

	var answer clearedBody
	if err := json.Unmarshal(body, &answer); err != nil {
		return ipError(stderr, "clear blocks: %v", err)
	}
	fmt.Fprintf(stdout, "cleared %d blocks\n", answer.Cleared)
	return nil
}

// controlSocketPath is where a verb looks for the live serve: --socket when it is given,
// otherwise the socket serve derives from the very same --db path.
func controlSocketPath(dbPath, socketPath string) string {
	if socketPath != "" {
		return socketPath
	}
	return dbPath + ".sock"
}

// controlClient is the one client every verb uses: a request dialed through the control
// socket, with a ten second ceiling.
func controlClient(socketPath string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

// controlRequest makes one verb's request over the control socket. A socket nobody answers
// on is the whole story an operator needs, so it is reported as one line naming the socket;
// every other answer comes back for the verb to read.
func controlRequest(stderr io.Writer, method, socketPath, path string) (int, []byte, error) {
	req, err := http.NewRequest(method, "http://localhost"+path, nil)
	if err != nil {
		return 0, nil, ipError(stderr, "%s %s: %v", method, path, err)
	}
	resp, err := controlClient(socketPath).Do(req)
	if err != nil {
		// A live server behind a socket the caller may not connect must not read as
		// "nothing is listening", or the operator restarts serve instead of fixing the mode.
		if errors.Is(err, os.ErrPermission) {
			return 0, nil, ipError(stderr, "cannot connect to %s: permission denied", socketPath)
		}
		return 0, nil, ipError(stderr, "no sandbin serve listening on %s", socketPath)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, ipError(stderr, "read the answer from %s: %v", socketPath, err)
	}
	return resp.StatusCode, body, nil
}

// controlFailure turns an answer that is not the row or rows the verb asked for into that
// verb's error, in the server's own words whenever it sent any.
func controlFailure(stderr io.Writer, what, socketPath string, code int, body []byte) error {
	var answer errorBody
	if err := json.Unmarshal(body, &answer); err == nil && answer.Error != "" {
		return ipError(stderr, "%s: %s", what, answer.Error)
	}
	return ipError(stderr, "%s: %s answered %s", what, socketPath, http.StatusText(code))
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

// writeBlocksTable renders rows as list's text table: a header line and one line per row,
// with REMAINING worked out at now. list hands it every row and show hands it one.
func writeBlocksTable(stdout io.Writer, blocks []IPBlock, now time.Time) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "IP\tFAILURES\tBLOCKED UNTIL\tREMAINING\tLAST FAILURE")
	for _, b := range blocks {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", b.IP, b.Failures, b.BlockedUntil, remainingText(b.BlockedUntil, now), b.UpdatedAt)
	}
	return w.Flush()
}

// jsonIPBlock is one row in the json shape: exactly these four fields, which is also what
// the control API answers with.
type jsonIPBlock struct {
	IP           string `json:"ip"`
	Failures     int    `json:"failures"`
	BlockedUntil string `json:"blocked_until"`
	UpdatedAt    string `json:"updated_at"`
}

// parseBlocks reads the json array the control API answers with back into rows.
func parseBlocks(body []byte) ([]IPBlock, error) {
	var rows []jsonIPBlock
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	blocks := make([]IPBlock, 0, len(rows))
	for _, row := range rows {
		blocks = append(blocks, IPBlock{IP: row.IP, Failures: row.Failures, BlockedUntil: row.BlockedUntil, UpdatedAt: row.UpdatedAt})
	}
	return blocks, nil
}

// parseBlock reads the one row object a show answer carries.
func parseBlock(body []byte) (IPBlock, error) {
	var row jsonIPBlock
	if err := json.Unmarshal(body, &row); err != nil {
		return IPBlock{}, err
	}
	return IPBlock{IP: row.IP, Failures: row.Failures, BlockedUntil: row.BlockedUntil, UpdatedAt: row.UpdatedAt}, nil
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
