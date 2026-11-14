// Command sandbin is a PUT/GET server that lets sandboxed CLI agents publish
// HTML, markdown and ANSI previews. See README.md.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/yookoala/sandbin/internal/version"
)

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch runs one whole command line on a fresh urfave app: apps are stateful, so a fresh
// one per call keeps every test invocation independent. Failures are reported by the verbs
// themselves; dispatch only maps a returned error to an exit code.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if err := newServerApp(stdout, stderr).Run(append([]string{"sandbin"}, args...)); err != nil {
		return 1
	}
	return 0
}

// newServerApp builds the app and its verbs with the writers wired: Writer takes --help and
// bare runs, ErrWriter takes the usage-error rendering and every operational failure.
func newServerApp(stdout, stderr io.Writer) *cli.App {
	hook := usageError(stderr)
	return &cli.App{
		Name:  "sandbin",
		Usage: "publish HTML, markdown and ANSI previews; serve runs the server, ip reads and changes its block table",
		// internal/version picks what this build can honestly say: a value the Makefile or the
		// Containerfile injected, this checkout's commit hash, the tag a release embedded, or dev.
		Version:      version.String(),
		Writer:       stdout,
		ErrWriter:    stderr,
		OnUsageError: hook,
		// urfave's own cli.Exit errors (the "No help topic for ..." refusals) would otherwise
		// print through HandleExitCoder and os.Exit inside app.Run. Swallowing the exit here
		// lets dispatch map the error to an exit code like every other failure. A verb's own
		// failures have already been printed by the verb, so only ExitCoder messages echo.
		ExitErrHandler: func(_ *cli.Context, err error) {
			if exitErr, ok := err.(cli.ExitCoder); ok && exitErr.Error() != "" {
				fmt.Fprintln(stderr, exitErr.Error())
			}
		},
		Commands: []*cli.Command{serveCommand(stderr, hook), ipCommand(stdout, stderr, hook)},
	}
}

// usageError renders urfave's usage failures onto stderr: v2's default renderer writes them
// to the app's stdout Writer, and a misuse is not a help request.
func usageError(stderr io.Writer) cli.OnUsageErrorFunc {
	return func(c *cli.Context, err error, isCommand bool) error {
		fmt.Fprintf(stderr, "Incorrect Usage: %s\n\n", err)
		switch {
		case !isCommand:
			cli.HelpPrinter(stderr, cli.AppHelpTemplate, c.App)
		case len(c.Command.Subcommands) > 0:
			cli.HelpPrinter(stderr, cli.SubcommandHelpTemplate, c.Command)
		default:
			cli.HelpPrinter(stderr, cli.CommandHelpTemplate, c.Command)
		}
		return err
	}
}

// serveConfig is what serve's flags say the server should be.
type serveConfig struct {
	apiKey, dbPath, addr, socketPath, trustedProxy, trustedProxyFile string
}

// serveFlags are serve's flags, built fresh for every app because a urfave flag carries its
// parsed value. Every flag falls back to its environment variable through EnvVars, which is
// how the container sets its settings without a command line.
func serveFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "api-key", EnvVars: []string{"SANDBIN_API_KEY"},
			Usage: "pre-shared API key clients send as X-API-Key"},
		&cli.StringFlag{Name: "db", Value: "sandbin.db", Usage: "sqlite file holding all bins"},
		&cli.StringFlag{Name: "addr", Value: ":8080", Usage: "address to listen on"},
		&cli.StringFlag{Name: "socket", EnvVars: []string{"SANDBIN_SOCKET"},
			Usage: "control socket the ip commands reach this server through"},
		&cli.StringFlag{Name: "trusted-proxy", EnvVars: []string{"SANDBIN_TRUSTED_PROXIES"},
			Usage: "comma-separated proxy IPs and CIDRs whose X-Forwarded-For is believed"},
		&cli.StringFlag{Name: "trusted-proxy-file", EnvVars: []string{"SANDBIN_TRUSTED_PROXIES_FILE"},
			Usage: "file of trusted proxy IPs and CIDRs, one per line, # comments allowed"},
	}
}

// serveConfigFromContext reads serve's settings off a parsed context. The control socket
// rides next to the db it manages, so a run that only names --db can never drift away from
// what the ip verbs look for.
func serveConfigFromContext(c *cli.Context) serveConfig {
	cfg := serveConfig{
		apiKey:           c.String("api-key"),
		dbPath:           c.String("db"),
		addr:             c.String("addr"),
		socketPath:       c.String("socket"),
		trustedProxy:     c.String("trusted-proxy"),
		trustedProxyFile: c.String("trusted-proxy-file"),
	}
	if cfg.socketPath == "" {
		cfg.socketPath = cfg.dbPath + ".sock"
	}
	return cfg
}

// serveRefusal reports one of serve's own failures on stderr and returns it, so each
// failure is reported once.
func serveRefusal(stderr io.Writer, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	fmt.Fprintln(stderr, err)
	return err
}

// serveCommand is sandbin serve: its flags, then the server. A stray argument says nothing
// the server reads, so it is refused like an unknown flag rather than ignored.
func serveCommand(stderr io.Writer, hook cli.OnUsageErrorFunc) *cli.Command {
	cmd := &cli.Command{
		Name:  "serve",
		Usage: "run the publish/read server",
		Flags: serveFlags(),
		Action: func(c *cli.Context) error {
			if extra := c.Args().Slice(); len(extra) > 0 {
				return serveRefusal(stderr, "serve takes no arguments, got %q", strings.Join(extra, " "))
			}
			return runServe(serveConfigFromContext(c))
		},
	}
	cmd.OnUsageError = hook
	return cmd
}

// parseServeFlags parses serve's flags on an app of their own, so a test can check the
// parsing without a listening server: the action captures the settings instead of starting
// the server.
func parseServeFlags(args []string, stderr io.Writer) (serveConfig, error) {
	var cfg serveConfig
	app := &cli.App{
		Name:           "sandbin serve",
		Writer:         io.Discard,
		ErrWriter:      stderr,
		OnUsageError:   usageError(stderr),
		ExitErrHandler: func(_ *cli.Context, _ error) {},
		Flags:          serveFlags(),
		Action: func(c *cli.Context) error {
			if extra := c.Args().Slice(); len(extra) > 0 {
				return serveRefusal(stderr, "serve takes no arguments, got %q", strings.Join(extra, " "))
			}
			cfg = serveConfigFromContext(c)
			return nil
		},
	}
	if err := app.Run(append([]string{"sandbin serve"}, args...)); err != nil {
		return serveConfig{}, err
	}
	return cfg, nil
}

// runServe starts the server with the parsed settings. The refuse-to-start guards and the
// log lines are the ones main() has always had; since log.Fatal exits the process, runServe
// never returns.
func runServe(cfg serveConfig) error {
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

	// The control socket comes up before the public listener: an operator who cannot
	// reach the block table should not get a server that starts anyway.
	controlListener, err := listenControl(cfg.socketPath)
	if err != nil {
		log.Fatalf("sandbin: %v", err)
	}
	controlServer := &http.Server{Handler: controlMux(store)}
	go func() {
		if err := controlServer.Serve(controlListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("sandbin: control socket: %v", err)
		}
	}()
	log.Printf("sandbin control socket on %s", cfg.socketPath)

	srv := &server{store: store, apiKey: []byte(cfg.apiKey), trustedProxies: proxies}
	log.Printf("sandbin listening on %s", cfg.addr)
	log.Fatal(http.ListenAndServe(cfg.addr, srv))
	return nil // unreachable: log.Fatal exits the process first
}
