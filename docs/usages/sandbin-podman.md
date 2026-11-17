# Running with podman

A Sandbin instance runs under podman one of two ways: pull the image the releases publish to
the GitHub container registry, or build it from a source checkout. The published image is the
short way; the source build is what `make container` and the quadlet unit use.

## Running the Published Image

Every release publishes this repository's `Containerfile` as `ghcr.io/yookoala/sandbin`: each
release tag becomes an image tag of the same name (`:v0.4.3`), and the working `main` branch
publishes `:latest`. A machine with podman needs no source and no Go for this way:

```sh
# pull the published image
podman pull ghcr.io/yookoala/sandbin:latest

# run it: named volume for the sqlite file, API key from a 0600 env file, health probe;
# the image's own CMD is serve --db /data/sandbin.db, so no command is needed
podman run -d \
  --name sandbin \
  -p 127.0.0.1:8080:8080 \
  -v sandbin-data:/data \
  --env-file ~/.config/sandbin/sandbin.env \
  --health-cmd "wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1" \
  --health-interval=30s --health-start-period=5s --health-retries=3 \
  ghcr.io/yookoala/sandbin:latest
```

The env file is the `SANDBIN_API_KEY` setting [Running the server](sandbin-serve.md) describes,
kept mode `0600` like a password. Pull `:v0.4.3` instead of `:latest` to pin the container to a
named release. The quadlet unit under *Running as Systemd Service* looks for
`localhost/sandbin:latest`; point its `Image=` line at `ghcr.io/yookoala/sandbin:latest` to run
the published image as a service instead.

## Building & Running from Source

### Download the Source

```sh
# fetch the source; the Containerfile sits at the repository root
git clone https://github.com/yookoala/sandbin

# enter the checkout
cd sandbin
```

The image build is hermetic two stages: `golang:1.26-alpine` builds, `alpine:3.22` runs, so the
host needs nothing but podman — unlike the source build of the binaries, this does not need Go
installed (see [Running the server](sandbin-serve.md)).

### Running

Build the image under exactly this tag, because the quadlet unit below looks for it and podman
tries to pull a registry image otherwise:

```sh
podman build -t localhost/sandbin:latest .
```

`make container` runs that same build through podman with the version string from
`scripts/version.sh` passed in, which is what the container's `sandbin --version` then prints;
to do it by hand instead of through the Makefile:

```sh
# the same build by hand, stamping the checkout's version string
podman build -t localhost/sandbin:latest \
  --build-arg SANDBIN_VERSION="$(scripts/version.sh)" \
  .
```

Without `--build-arg` the stamp is empty and the image falls back to what the copied tree can
honestly say: the tag `internal/version/release.txt` carries at a release, otherwise plain `dev`.

The ad-hoc run against the image you just built is the published image's run with the other
name, and it needs no command of its own: the image's CMD is `serve --db /data/sandbin.db`.

```sh
# run detached: named volume for the sqlite file, API key from a 0600 env file, health probe
podman run -d \
  --name sandbin \
  -p 127.0.0.1:8080:8080 \
  -v sandbin-data:/data \
  --env-file ~/.config/sandbin/sandbin.env \
  --health-cmd "wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1" \
  --health-interval=30s --health-start-period=5s --health-retries=3 \
  localhost/sandbin:latest
```

### Health Check

Podman warns `HEALTHCHECK is not supported for OCI image format` on every build: its default
image format drops that line. The probe below is the podman-native form, and podman runs it.
Add `--format docker` to the build instead if you want the image to carry it.

```sh
# print the container's health status: probed every 30s, healthy on a healthy server
podman inspect --format '{{`{{`}}.State.Health.Status}}' sandbin
```

The doubled braces in the `--format` above are an escape for the hosted rendering: this page
runs through Go's template pass when served, and the doubled form is what delivers the exact
single pair of braces podman expects. In a plain terminal you type podman's own braces around
`State.Health.Status`.

### Running as Systemd Service

As a systemd user service through podman quadlet, starting from the checkout:

```sh
# create the quadlet drop-in folder and the config folder
mkdir -p ~/.config/containers/systemd ~/.config/sandbin

# install the shipped unit
cp deploy/sandbin.container ~/.config/containers/systemd/

# install the env file the unit will read, mode 0600 — then edit replace-me
install -m 600 deploy/sandbin.env.example ~/.config/sandbin/sandbin.env

# point the unit's EnvironmentFile= at that file
sed -i "s#^EnvironmentFile=.*#EnvironmentFile=$HOME/.config/sandbin/sandbin.env#" ~/.config/containers/systemd/sandbin.container

# let systemd pick up the unit
systemctl --user daemon-reload

# start the service
systemctl --user start sandbin
```

The `sed` is not optional. `deploy/sandbin.container` points `EnvironmentFile=` at
`/etc/sandbin/sandbin.env`, meant for a system install, and quadlet resolves a relative or
`~`-prefixed path against the unit's own directory, so an unedited `~/.config/...` value
restart-loops. For a system install, keep the path as shipped and create `/etc/sandbin/sandbin.env`
with mode 0600 instead.

The published port is loopback-only (`PublishPort=127.0.0.1:8080:8080`) because this is a
preview tool; edit the unit to `8080:8080` to expose it on the network. The API key never
appears in the container command line, but it is visible in `podman inspect` output
(`Config.Env`), so treat the env file like a password.
