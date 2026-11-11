# Decisions: client defaults

Date: 2026-11-11. Confirmed with the user before `docs/plans/0005-client-cli.md`.

| Topic | Decision | Why |
|---|---|---|
| JS and CSS | The server serves `.js` and `.mjs` as `text/javascript` and `.css` as `text/css` | Today they are `text/plain`, so a browser refuses them and a multi-file HTML+JS prototype cannot work. |
| Bin selection | A `.sandbin.json` state file in the working directory, auto-created on first upload, overridable with `--bin` and `--url` | An agent re-runs one command after editing a file instead of remembering a hash. |
| Input | File arguments, plus stdin with `--name` | Redirecting command output into a preview should not need a temp file. |
| Bin password | Supported, through `SANDBIN_BIN_PASSWORD` only, never a flag | Keeps the secret off command lines and out of the state file. |
| Retrying | Never retry | A bad API key charges the client's address a 30 minute block that grows, so a retrying client digs its own hole. |
| Output | Plain text URLs, one per line, bin base URL last | An agent can read it and hand it to a human without a parser. |
