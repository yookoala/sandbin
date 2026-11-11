# Plan: make sandbin installable with podman

Status: draft. Decisions in `docs/decisions/0002-container-defaults.md`. Verified in this
sandbox first: podman 5.8.9 with crun is present, `FROM scratch` builds and runs, pulls from
docker.io work, `docker.io/library/golang:1.26-alpine` exists upstream, and
`CGO_ENABLED=0 go build` of the current code produces a 9.8 MB static binary (modernc sqlite
is pure Go, so no cgo is needed anywhere).

## Scope

Containerfile, not Dockerfile. No compose file, no Dockerfile, no registry push. Four new
files, one README section, and one three-line handler.

```
Containerfile              two stages: golang:1.26-alpine builds, alpine:3.22 runs
.containerignore           keeps .git and .opencode out of the build context
deploy/sandbin.container   podman quadlet unit for running it as a service
deploy/sandbin.env.example template for the pre-shared key
cmd/sandbin/handlers.go    add GET /healthz so the image can have a HEALTHCHECK
```

## Containerfile

```
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags=-s -w -o /out/sandbin ./cmd/sandbin

FROM docker.io/library/alpine:3.22
RUN adduser -D -u 1000 sandbin && mkdir -p /data && chown sandbin:sandbin /data
COPY --from=build /out/sandbin /usr/local/bin/sandbin
USER 1000:1000
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/usr/local/bin/sandbin"]
CMD ["--db", "/data/sandbin.db"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
```

Notes the implementer must keep:
- Module files are copied before `cmd/` so dependency download is a cached layer.
- The app needs no shell, no CA certs and no users other than uid 1000; the runtime stage
  must not install anything.
- `VOLUME /data` plus `CMD ["--db", "/data/sandbin.db"]` is what makes the sqlite file survive
  container removal. Podman copies the ownership of `/data` in the image to a fresh named
  volume, which is why the `chown` matters: without it uid 1000 cannot write the database.

## /healthz

`HEALTHCHECK` needs a URL that answers 200 without credentials; `GET /` answers 405 and a bin
path needs a key. Add, in `ServeHTTP` before `splitBinPath`:

```go
if r.URL.Path == "/healthz" {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok")
	return
}
```

It is unauthenticated on purpose: it says nothing about bins. Bin hashes are 8 hex characters,
so `healthz` can never name a real bin and the branch cannot shadow one.

## deploy/sandbin.container

```
[Unit]
Description=sandbin preview bins

[Container]
Image=localhost/sandbin:latest
ContainerName=sandbin
PublishPort=127.0.0.1:8080:8080
Volume=sandbin-data:/data
EnvironmentFile=/etc/sandbin/sandbin.env

[Service]
Restart=always
TimeoutStopSec=20

[Install]
WantedBy=default.target
```

`Exec=` is deliberately absent: the image `CMD` already passes `--db /data/sandbin.db`.
Loopback-only `PublishPort` matches what a preview tool should be; exposing it to a network is
a documented edit, not a default. The named volume `sandbin-data` is created by podman on first
start, so no `.volume` unit is needed.

For a rootless (user) install the unit goes to `~/.config/containers/systemd/` and
`EnvironmentFile=` must point at a file that user can read, e.g.
`~/.config/sandbin/sandbin.env`; for a system install, `/etc/sandbin/` with mode 0600.

## deploy/sandbin.env.example

```
# Copy to /etc/sandbin/sandbin.env (or ~/.config/sandbin/sandbin.env), then chmod 600.
SANDBIN_API_KEY=replace-me
```

`main.go` already falls back to `$SANDBIN_API_KEY`, so no code change is needed for this.

## Milestones (one atomic commit each)

1. `feat: healthz endpoint for container health checks` — handler branch + test.
2. `build: Containerfile and containerignore` — image builds and runs.
3. `deploy: quadlet service unit and env file example` — unit translates cleanly.
4. `docs: podman build and run instructions` — README section.

## Implementer TODO

- [ ] `handlers.go`: add the `/healthz` branch in `ServeHTTP` before `splitBinPath`.
- [ ] `handlers_test.go`: `GET /healthz` is 200 with body `ok` and no API key; `POST /healthz`
      is 405; a real bin still works after the new branch.
- [ ] `Containerfile` exactly as above; verify both base tags actually pull
      (`podman pull docker.io/library/golang:1.26-alpine`, `.../alpine:3.22`).
- [ ] `.containerignore`: `.git`, `.opencode`, `docs`, `*.db`.
- [ ] `deploy/sandbin.container` and `deploy/sandbin.env.example` as above.
- [ ] README: a `## Run it with podman` block —
      `podman build -t localhost/sandbin:latest .`,
      `podman run -d --name sandbin -p 127.0.0.1:8080:8080 -v sandbin-data:/data --env-file ~/.config/sandbin/sandbin.env localhost/sandbin:latest`,
      the quadlet install (copy the unit, `install -m 600` the env file,
      `systemctl --user daemon-reload && systemctl --user start sandbin`), and a line saying the
      key is visible in `podman inspect` output so the env file needs the same care as a password.
- [ ] `go build ./...`, `gofmt -l cmd/sandbin` empty, `go vet ./...` clean, `go test ./...` and
      `go test -race ./...` green.

## Reviewer TODO

Build and size:
- [ ] `podman build -t localhost/sandbin:test .` succeeds from a clean checkout
      (`git clean -fdx` in a scratch copy, not the working tree).
- [ ] `podman images` shows the runtime image under 25 MB.
- [ ] `podman run --rm localhost/sandbin:test --help` prints the three flags.

Runtime behaviour, all through the published port (use 127.0.0.1:18080 to stay out of the way):
- [ ] Full round trip inside the container: POST a bin, PUT a `.html`, a `.md` and an `.ansi`,
      GET the listing and each file; compare the responses with the host-binary behaviour.
- [ ] Repeat the host's adversarial probes against the container: `..`, `%2e%2e`, `//`,
      oversized body, missing key, bin password. Nothing may behave differently behind podman.
- [ ] `podman exec sandbin id -u` is `1000`, not `0`.
- [ ] `podman inspect --format '{{.State.Health.Status}}'` becomes `healthy` within about 40s,
      and `GET /healthz` is what made it so.
- [ ] Persistence: `podman restart sandbin` then GET the same file — the bin must still be there;
      `podman volume inspect sandbin-data` mountpoint contains `sandbin.db`.
- [ ] `--read-only` rootfs still works (`podman run --rm --read-only -v sandbin-data:/data ...`),
      which proves sqlite writes stay inside the volume.
- [ ] The API key is absent from `podman inspect --format '{{.Config.Cmd}} {{.Args}}'`. It is
      present in `{{.Config.Env}}`: that is accepted, but the README must say so.
- [ ] SELinux: `getenforce` on the target host; if `Enforcing`, prove a bind mount needs `:Z`
      (`-v ./data:/data:Z`) and that the named volume path needs no label at all.
- [ ] Rootless port mapping works with no privileges (8080 is above 1024, so it must).

Service unit:
- [ ] `mkdir -p ~/.config/containers/systemd && cp deploy/sandbin.container ~/.config/containers/systemd/`
      then `/usr/libexec/podman/quadlet -user -dryrun` produces a unit with no unknown-key
      warnings, and the generated `ExecStart` carries `--env-file`, `-v sandbin-data:/data`,
      `-p 127.0.0.1:8080:8080`.
- [ ] In this sandbox systemd has no running user manager (`systemctl --user is-system-running`
      reports `offline`), so the dryrun plus the equivalent `podman run` command is the ceiling
      here. Say plainly that starting the service needs a real host, do not fake it.
- [ ] Env file permissions in the README example are `0600`, and the example file itself contains
      no real secret.

Repo shape:
- [ ] No `Dockerfile`, no compose file, no new Go dependency, no changes outside
      `cmd/sandbin/`, `Containerfile`, `.containerignore`, `deploy/`, `README.md`, `docs/`.

## Skipped on purpose

Dockerfile and docker-compose (podman is the target), pushing to a registry, multi-arch
manifest builds, TLS in the container (put a reverse proxy in front), quadlet `.network` and
`.volume` units, an authenticated or `/healthz`-only metrics endpoint, `podman secret`,
user namespace `--userns=keep-id` tuning for bind mounts, an SELinux policy module, CI workflow
files, and a Makefile.
