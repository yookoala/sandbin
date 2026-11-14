# Point sandbin-client at a server

`sandbin-client init` settles one project: it collects a url and a key, checks them against the
server before writing anything, and remembers them in `.sandbin.yaml` next to the work. On a
terminal it prompts for whatever is missing; Enter on the url prompt keeps the shown default:

```sh
sandbin-client init
# sandbin_url [http://localhost:8080]:
# api_key:
# initialized http://localhost:8080
```

The check before writing is a `POST /verify-key` with the candidate key: a wrong key answers
`server rejected this API key` and a locked-out address answers `server is blocking this
address: too many bad API keys`, and either way init writes nothing — a project never holds a
setup that cannot upload. The prompt echoes what you type, key included, the way any terminal
prompt does; no client output ever prints the key on its own. Without a terminal — a script, a
CI job, an agent — the same verb reads flags and variables; a missing url falls back to
`http://localhost:8080` and a missing key is a refusal naming `--api-key`, `SANDBIN_API_KEY` and
the prompt.

Re-running `init` in a project whose `.sandbin.yaml` already holds both a url and a key is an
explicit act. At a terminal it warns `sandbin-client: warning: .sandbin.yaml is already
initialized for http://localhost:8080` and asks `re-initialize? [y/N]: `; `y` or `yes` continues
the wizard as before, and anything else — bare Enter included — prints `left the existing
initialization for http://localhost:8080 untouched` and exits 0 with nothing requested and
nothing rewritten. Without a terminal nobody is there to answer, so the run refuses with
`sandbin-client: .sandbin.yaml is already initialized for http://localhost:8080: pass --reinit
to overwrite it, or delete the file to start over`. Flags and environment are not consent: a
fresh `--url` or `--api-key` is exactly the overwrite the guard asks about. `--reinit` is the
consent a script or CI job passes, and on a directory that is not initialized it is a harmless
no-op. A confirmed re-init re-verifies and rewrites, which is how a key or a server gets
changed, keeps `bin` and `protected` while the url stays the same, and with unchanged inputs
the file comes out byte-identical.

```sh
sandbin-client init
# sandbin-client: warning: .sandbin.yaml is already initialized for http://localhost:8080
# re-initialize? [y/N]: n
# left the existing initialization for http://localhost:8080 untouched
```

```sh
SANDBIN_URL=http://sandbin.example SANDBIN_API_KEY=your-key sandbin-client init
# initialized http://sandbin.example
sandbin-client init --reinit --url http://other.example --api-key your-key
# initialized http://other.example
```

The file `init` writes:

```yaml
sandbin_url: http://localhost:8080
api_key: your-key
bin: abc1234
protected: false
```

It is mode `0600`, and `init` makes sure the directory's `.gitignore` names `.sandbin.yaml` —
creating the file if it is absent, appending the line if it is missing, and leaving an existing
`.gitignore` exactly as written if the line is already there. `init` is the only run that writes
an `api_key`, at your explicit request; an ordinary upload run writes `sandbin_url`, `bin` and
`protected` and keeps whatever key the file already holds. The bin password stays env-only, as
before.

Settings resolve flag first, environment second, the file third, the built-in default last:
`--url` > `SANDBIN_URL` > `sandbin_url` > `http://localhost:8080`, and `--api-key` >
`SANDBIN_API_KEY` > `api_key`. The environment beating the file is what lets a CI job or an
agent override a project without editing it.

The state file earlier versions of this client kept as JSON is gone without migration or
fallback: run `init` once in a project that has one and delete the old file.
