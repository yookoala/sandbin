# Using this instance with Agent skill

This instance ships an agent skill (`skills/sandbin-preview/SKILL.md`) that teaches an agent to
work with this server through `sandbin-client`: first-time `init` in a project directory,
uploading files with `upload`, uploading a whole folder with `upload --archive`, reading a bin
with `describe` and listing the instance's bins with `list`, and what the caps refuse. The
skill keeps its own client binary: it tells the agent to build it into the skill folder with
`GOBIN="$SKILL" go install
github.com/yookoala/sandbin/cmd/sandbin-client@latest` and to always run `"$SKILL/sandbin-client"`
from there.

## General Install

The server serves a small installer at `GET /skills/sandbin-preview/install.sh`, and one command
installs the skill into `$HOME/.agents/skills/sandbin-preview` — a folder OpenCode picks up in
every project without any configuration:

```sh
curl -fsSL {{ .Instance.URL }}/skills/sandbin-preview/install.sh | bash
```

When the machine has Go the installer also builds the client binary into the skill folder, and
the build is pinned to the serving server's own release: on a plain `vMAJOR.MINOR.PATCH`
release, installer, server and built client share one version string end to end, and the built
client's `--version` names that tag alone because the tag travels inside the downloaded module
zip; [Running the server](sandbin-serve.md) explains how that stamping works. Otherwise the pin
is `@latest`, which resolves to the newest published tag. Without Go, or if that install fails,
the installer says so, exits 0 anyway, and the skill builds the client on first use instead.

With the client binary in place the installer also initializes the current directory. At a
terminal with `/dev/tty` available it runs `sandbin-client init --url {{ .Instance.URL }}`
against the serving address, so the single question it asks is the API key — the url arrives
with the installer. The re-initialization guard question for an already initialized project
arrives the same way: answering `n` leaves that project alone and the install still stands; a
failed init prints a `sandbin-installer:` warning naming the command and `--reinit` and still
exits 0, because the skill files are installed regardless. Without a terminal nothing is asked
and the command is printed instead:

```sh
# sandbin-installer: run "$HOME/.agents/skills/sandbin-preview/sandbin-client init --url {{ .Instance.URL }} --api-key YOUR_KEY" to finish setup
```

so an agent or a CI job finishes the setup with that one line.

## Install for Specific AI Harness

Not every harness reads `$HOME/.agents/skills`. The installer takes an optional `-d` or `--dest`
to pick the skills folder instead, passed through the pipe with `bash -s --`; for a single
project that means:

```sh
curl -fsSL {{ .Instance.URL }}/skills/sandbin-preview/install.sh | bash -s -- --dest .opencode/skills
```

and for an agent with its own folder, for example Claude, `--dest "$HOME/.claude/skills"`.
`--help` prints the usage; anything else stops with `unknown argument` and installs nothing.

The landing page at `GET /` presents exactly these commands as a harness picker: the sentence
"I use …" opens on a combobox listing every harness, and the row you pick fills a readonly
command field below it — the generic row is the no-`--dest` one-liner for `$HOME/.agents/skills`,
every other row the same pipe with `--dest "$HOME/<folder>"` for that harness's own skills
folder — above a "Skill will be installed to:" line naming the folder the shown command
installs into. The field's inner copy icon copies the command, flashing a brief "Copied" over
the field; clicking the field selects the whole command for manual copying.
