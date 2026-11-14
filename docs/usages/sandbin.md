# sandbin — usage documentation

`sandbin` is the PUT/GET preview server itself: build it, run `serve`, and drive its HTTP API
with curl or any client. These pages cover the server side; the companion client lives in
[sandbin-client.md](sandbin-client.md). A running server hosts this whole set under `/docs`: the
hub at `/docs/`, this summary at `/docs/sandbin.html`, and each page below at its own
`/docs/<name>.html`.

| Page | What it covers |
|---|---|
| [Running the server](sandbin-serve.md) | building, the verb family, `serve`'s core settings and their environment variables, startup refusals |
| [The HTTP API](sandbin-api.md) | bins and curl semantics, the 301 bin-base redirect, bin passwords, title/description metadata, `DELETE`, the `404.html` fallback, status codes, archive uploads |
| [API key lockout](sandbin-lockout.md) | which requests are key-checked, the 30-minute escalation, `X-Forwarded-For` trust, [Trusted proxies](sandbin-lockout.md#trusted-proxies) |
| [Run it with podman](sandbin-podman.md) | image build, ad-hoc run with volume/env file/health probe, the quadlet systemd service |
| [Managing blocked addresses](sandbin-ip.md) | the control socket, the `ip list|show|unblock|clear` family, the socket's own HTTP shape |

Every client user should also read the [API key lockout](sandbin-lockout.md) page: it explains
what a wrong key costs from the server side, including the `GET`-is-never-blocked rule.
