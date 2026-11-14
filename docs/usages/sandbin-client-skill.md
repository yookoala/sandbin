# Give the skill to your agent

The repo ships `skills/sandbin-preview/SKILL.md`: an agent skill that teaches an agent to run
this client — first-time `init` in a project directory, uploading files with `upload`, uploading
a whole folder with `upload --archive`, and what the caps refuse. The skill keeps its own client
binary: it tells the agent to build it into the skill folder with
`GOBIN="$SKILL" go install github.com/yookoala/sandbin/cmd/sandbin-client@latest` and to always
run `"$SKILL/sandbin-client"` from there.

The served skill is a text/template: `GET /skills/sandbin-preview/SKILL.md` renders every
address in it to the one the request arrived at (the same detection the front page uses), so a
downloaded skill names exactly your server, frontmatter description included. The copy in git
carries the raw `{{.Base}}` tokens — install from a running server, not from a checkout. The
server also serves a small installer at `GET /skills/sandbin-preview/install.sh` with the
server's own address filled in, so one command installs the skill into
`$HOME/.agents/skills/sandbin-preview` — a folder OpenCode picks up in every project without any
configuration:

```sh
curl -fsSL http://localhost:8080/skills/sandbin-preview/install.sh | bash
```

The installer takes an optional `-d` or `--dest` to pick the skills folder instead, passed
through the pipe with `bash -s --`; for a single project that means:

```sh
curl -fsSL http://localhost:8080/skills/sandbin-preview/install.sh | bash -s -- --dest .opencode/skills
```

and for an agent with its own folder, for example Claude, `--dest "$HOME/.claude/skills"`.
`--help` prints the usage; anything else stops with `unknown argument` and installs nothing.
When the machine has Go, the installer also builds the client binary into the skill folder
(`GOBIN` at the skill dir, `go install github.com/yookoala/sandbin/cmd/sandbin-client@latest`);
without Go, or if that install fails, it says so, exits 0 anyway, and the skill builds the
client on first use instead.

With the client binary in place the installer also initializes the current directory. At a
terminal with `/dev/tty` available it runs `sandbin-client init --url http://localhost:8080`
against the serving address, so the single question it asks is the API key — the url arrives
with the served script. The re-initialization guard question for an already initialized project
arrives the same way: answering `n` leaves that project alone and the install still stands; a
failed init prints a `sandbin-installer:` warning naming the command and `--reinit` and still
exits 0, because the skill files are installed regardless. Without a terminal nothing is asked
and the command is printed instead:

```sh
# sandbin-installer: run "$HOME/.agents/skills/sandbin-preview/sandbin-client init --url http://localhost:8080 --api-key YOUR_KEY" to finish setup
```

so an agent or a CI job finishes the setup with that one line.

A client the installer built with `go install ...@latest` is a proxy download, so its
`--version` line names the tag alone (`v0.2.0`, newest tag wins) with no commit hash: that
binary is exactly the tagged commit, and a module zip carries no commit hash to print.
