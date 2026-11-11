# Plan: a CLI client for agents to publish previews

Status: draft. Decisions in `docs/decisions/0005-client-defaults.md`.

## What it is

`cmd/sandbin-client`: a command an AI agent runs after it writes a prototype, so the human gets a
URL. It creates a bin on first use, uploads or replaces files, and prints plain-text URLs.

Not a browser tool, not a deploy tool, no local server. It is `curl` shaped for this API.

## First thing to fix: the server cannot serve a JS prototype today

`contentType` (`cmd/sandbin/handlers.go:365-376`) names only `.html`, `.md` and `.txt`. A `.js`
or `.css` file is served as `text/plain`, so a browser refuses to run `<script src="app.js">` or
apply `<link href="style.css">`. An HTML+JS prototype uploaded as separate files does not work
today, which is the stated target.

Add to that switch, with tests:

```go
case ".js", ".mjs":
	return "text/javascript; charset=utf-8"
case ".css":
	return "text/css; charset=utf-8"
```

Nothing else changes. This does not widen what a bin can do: `.html` already serves executable
markup.

## Client surface

```
sandbin-client [flags] file...
sandbin-client --name build.ansi < build.log
```

Environment, with flags that override it:

| Variable | Flag | Default |
|---|---|---|
| `SANDBIN_URL` | `--url` | `http://localhost:8080` |
| `SANDBIN_API_KEY` | `--api-key` | none; missing key is a startup error, no request is made |
| `SANDBIN_BIN_PASSWORD` | none on purpose | unset; only sent when set |

Other flags: `--bin <hash>` to name a bin explicitly, `--new` to force a fresh bin, `--name <path>`
required for stdin. That is the whole flag set. No `--json`, no `--verbose`, no `--delete`, no
`--list`.

## Which bin to use

A state file `.sandbin.json` in the working directory:

```json
{ "url": "http://localhost:8080", "bin": "abc1234", "protected": false }
```

Precedence, first match wins: `--bin`, then a readable state file whose url matches the resolved
url, then auto-create a bin and write the state file. `--new` skips straight to auto-create.
`protected` is written when the bin was created with a password, so the client can warn before
making a request. The file never holds the API key or the bin password, and it goes in
`.gitignore`.

A corrupt or unreadable state file is an error naming the file and what was expected, not a
silent fall-through to creating a second bin: two bins for one prototype is the confusing outcome.

## Behaviour details

- Upload name is the argument as given, `path.Clean`ed, with `./` stripped. Absolute paths, `..`
  segments, backslashes and paths over 256 characters or 16 segments are rejected locally with the
  same wording the server uses, and never sent. The server stays the authority; the client only
  fails faster.
- Files over 1 MiB are rejected before the request, naming the limit the server enforces.
- `201` prints `created <url>`, `200` prints `updated <url>`, and the bin base URL is printed last
  so the agent has one thing to hand to a human.
- Any non-2xx prints the status, the server's plain-text body, and exits 1. `429` additionally
  prints the `Retry-After` seconds. **No automatic retry anywhere**: a wrong API key would charge
  the address 30 minutes of lockout, and a retrying client would multiply its own penalty.
- `401` prints one extra line telling the agent to set `SANDBIN_BIN_PASSWORD`. When that variable
  is set, the client sends Basic Auth with the bin hash as the user name on every upload.
- `http.Client{Timeout: 30 * time.Second}`. stdin with no `--name` is an error.

Example session:

```sh
export SANDBIN_URL=http://localhost:8080 SANDBIN_API_KEY=your-key
sandbin-client prototype/index.html prototype/app.js prototype/style.css build.ansi
# created http://localhost:8080/abc1234/prototype/index.html
# created http://localhost:8080/abc1234/prototype/app.js
# created http://localhost:8080/abc1234/prototype/style.css
# http://localhost:8080/abc1234/          <- hand this to a human
make 2>&1 | sandbin-client --name build.ansi
# updated http://localhost:8080/abc1234/build.ansi
```

## Layout

```
cmd/sandbin-client/
  main.go     flag parsing, env, state resolution, exit codes
  client.go   createBin and putFile over net/http
  state.go    read and write .sandbin.json
  main_test.go
cmd/sandbin/
  handlers.go contentType gains .js, .mjs, .css
  handlers_test.go   asserts those three content types, and that .json stays text/plain
```

No shared package between the two commands. The API constants are four header names and a status
code or two; duplicating them in the client is cheaper than inventing an internal package. Client
tests run against a fake `httptest` handler that mirrors the real status codes, so no test needs a
network or the server binary.

## Milestones (one atomic commit each)

1. `feat: serve javascript and css as themselves` — the `contentType` cases and their tests.
2. `feat: sandbin-client uploads files to a bin` — the whole client plus `main_test.go`.
3. `docs: client usage for agents` — README section, `.sandbin.json` added to `.gitignore`.

## Implementer TODO

- [ ] `cmd/sandbin/handlers.go`: add the three content type cases. Test them the way the existing
      content-type tests are written, including a `.json` file still being `text/plain` so the
      boundary is explicit.
- [ ] `cmd/sandbin-client/client.go`: `createBin` (POST `/`, `X-API-Key`, optional
      `X-Bin-Password`, optional Basic Auth is not needed at creation) returning the hash parsed
      out of the response body, and `putFile` (PUT, `X-API-Key`, optional Basic Auth with the bin
      hash as user name). Both return a typed error carrying status, server message and
      `Retry-After` for `429`.
- [ ] `cmd/sandbin-client/state.go`: read, write, and the precedence rule above. Write with a
      temp file and rename, mode 0600, so an interrupted run cannot leave half a state file.
- [ ] `cmd/sandbin-client/main.go`: the flags and env above, the path and size rules, the print
      formats, exit 0 on success and exit 1 on any failure with the message on stderr.
- [ ] `cmd/sandbin-client/main_test.go` against a fake handler, asserting at least: happy path
      prints `created` lines and the bin URL; a second run reuses the state file and prints
      `updated`; `--new` creates a second bin; missing API key fails before any request reaches the
      handler; a wrong key reports `403 invalid API key` and does not retry (the fake handler counts
      requests to prove it); `429` reports the `Retry-After` value; `413` names the 1 MiB limit;
      `401` names `SANDBIN_BIN_PASSWORD`; stdin without `--name` fails; an over-long path, an
      absolute path, and a `..` path are refused locally, with zero requests recorded; a file over
      1 MiB is refused locally; a corrupt state file errors naming the file; `protected` state plus
      no password variable warns before the request.
- [ ] A test that the state file is not needed for a single run given `--bin` and `--url`.
- [ ] `.gitignore` gains `.sandbin.json`.
- [ ] README: a `## Upload previews with sandbin-client` section: the build command, the env
      variables, the example session, the state file and what is in it, the warning that a wrong
      API key locks the client's address out for 30 minutes growing from there so a failing script
      must not loop, and the note that a prototype needs `index.html` for the bin base URL to show
      it rather than the listing.
- [ ] `go build ./...` builds both binaries, `gofmt -l cmd` empty, `go vet ./...` clean,
      `go test -count=1 ./...` and `go test -race ./...` green.
- [ ] Live proof, cleaned up afterwards: start the real server on `127.0.0.1:18140` with a temp db
      under `/tmp/opencode`; run the client's example session in a temp working directory; `curl -I`
      each uploaded file and report the `Content-Type` for `.html`, `.js`, `.css` and `.ansi`;
      re-run the client to show `updated`; run `make`-style output through stdin; show the state
      file contents; show the `429` path by sending a wrong key twice and reporting what the client
      prints; kill the server and remove every file.

## Reviewer TODO

- [ ] Read README, `docs/decisions/0005-client-defaults.md`, `cmd/sandbin/handlers.go`, then the new
      client files.
- [ ] Content types against a live server on `127.0.0.1:18141` with `curl -I`: `.js`, `.mjs`, `.css`,
      `.html`, `.md`, `.txt`, `.json`, `.png`, `.svg`, and a `.js` file whose name is uppercase.
      Report the actual header values and confirm no case where a `.js` body is served as
      `text/plain`.
- [ ] Threat check on the change: confirm serving `text/javascript` adds nothing a bin could not
      already do through `.html`, and that a `.css` body cannot be executed as a script by
      sniffing. Say plainly what an attacker who can write to a bin can already do.
- [ ] Client contract fidelity against the server source: header names, bin hash shape, the `201`
      vs `200` distinction, `400`, `401` plus `WWW-Authenticate`, `403`, `404`, `413`, `429` plus
      `Retry-After`, the 1 MiB limit, the path rules (256 characters, 16 segments, no `..`, no
      leading `/`, no trailing slash), and that a subdirectory name like `prototype/app.js` is
      accepted by the server. Report any mismatch in either direction.
- [ ] Lockout interaction, the one most likely to bite an agent: prove with the live server that a
      client run with a wrong API key gets one `403`, exits 1, makes exactly one request, and that
      the next run from the same address gets `429` with a `Retry-After` the client reports. Confirm
      the client never retries, and that README warns about this.
- [ ] State file behaviour: run the client twice in one directory, then in a second directory, then
      with `--new`, then with a corrupt file, then with the file removed. Report what bin each run
      used and confirm no run silently created a second bin for the same prototype. Confirm mode
      0600, no secret in it, and that `.gitignore` keeps it out of commits.
- [ ] Injection hunt: file names with spaces, quotes, newlines, `#`, `?`, `%`, unicode, and a name
      that starts with `-`; confirm each reaches the server as a path and not as a flag, and that
      the printed URL is properly escaped or clearly raw. Confirm a `--name` value containing `?`
      or `#` cannot smuggle a query or fragment into the URL.
- [ ] Input limits: a 5 MiB file (must be refused locally), a 1 MiB file (must succeed), an empty
      file, a file containing NUL bytes, a 10 MB stdin stream, and no arguments at all. Report the
      exact messages.
- [ ] Exit codes and output discipline: every failure path exits 1 with the message on stderr and
      nothing on stdout, so an agent can tell success from failure by exit code alone. Verify with
      `echo $?` on each failing case.
- [ ] Timeouts: point `SANDBIN_URL` at an address that accepts but never answers, and at one that
      refuses; confirm the client stops within its timeout with a readable error instead of hanging.
- [ ] Run `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l cmd` yourself.
      Name every new code path with no test, including the stdin path and the state write failure.
- [ ] Repo shape: two buildable commands, no new dependency, no shared internal package, no change
      to bins, files, block tables, lockout rules, container files or ANSI rendering beyond the
      `contentType` cases. Report anything touched that should not have been.

## Skipped on purpose

`--delete`, `--list`, JSON output, `--capture` running a command for the agent, bundling JS and CSS
into one HTML file, uploading images with correct types (`.png`, `.svg` and friends stay
`text/plain` until someone needs them, and they are the same two-line change as the JS rule),
automatic retry, key storage in a config file, TLS or proxy configuration, an install target,
including the client in the container image, and any change to what the server accepts as a path.
