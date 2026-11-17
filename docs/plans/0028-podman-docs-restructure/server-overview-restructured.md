# sandbin — usage documentation

`sandbin` is the PUT/GET preview server itself: build it, run `serve`, and drive its HTTP API
with curl or any client. These pages cover the server side; the companion client lives in
[sandbin-client.md](sandbin-client.md). A running server hosts this whole set under `/docs`: the
hub at `/docs/`, this summary at `/docs/sandbin.html`, and each page below at its own
`/docs/<name>.html`.

| Page | What it covers |
|---|---|
| [Running the server](sandbin-serve.md) | downloading the source, building, the `serve` subcommand, its core settings and their environment variables, startup refusals |
| [Running with podman](sandbin-podman.md) | pulling and running the published `ghcr.io/yookoala/sandbin` image, building the image from a source checkout, ad-hoc run with volume/env file/health probe, the quadlet systemd service |
| [The HTTP API](sandbin-api.md) | bins and curl semantics, the 301 bin-base redirect, bin passwords, title/description metadata, `DELETE`, the `404.html` fallback, status codes, archive uploads |
| [IP Address Blocking](sandbin-ip-blocking.md) | why and how bad API keys block an address, which address a request charges, [Trusted proxies](sandbin-ip-blocking.md#trusted-proxies), the control socket, the `ip list|show|unblock|clear` family, the socket's own HTTP shape |

## Beyond the API

Two routes answer without belonging to the API. `GET /` answers the branded landing page: the
wordmark, a "This is a Sandbin instance" heading with its tagline, and the harness picker — a
combobox under "Tell Your Agent to use This" that names the harness you use and fills a
readonly command field with that harness's one-line installer, above a "Skill will be
installed to:" line naming the folder the shown command installs into (see [Using this instance
with Agent skill](sandbin-client-skill.md)). A `GET /robots.txt` answers a two-line file asking
search engines not to crawl anything on the instance. Neither needs a key and neither charges.

Every client user should also read the [IP Address Blocking](sandbin-ip-blocking.md) page: it
explains what a wrong key costs from the server side, including the `GET`-is-never-blocked
rule.
