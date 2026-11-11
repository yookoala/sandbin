# Decisions: container defaults

Date: 2026-11-11. Confirmed with the user before the plan in
`docs/plans/0002-podman-container.md`.

| Topic | Decision | Why |
|---|---|---|
| Runtime base | `docker.io/library/alpine:3.22` | Gives `wget` for a HEALTHCHECK and a shell for debugging. Costs about 9 MB and a base to keep patched, which `scratch` would avoid. |
| Build | hermetic multi-stage with `docker.io/library/golang:1.26-alpine` | `podman build .` works on any machine with podman; the host does not need Go. First build pulls about 70 MB. |
| Service install | podman quadlet unit `deploy/sandbin.container` | systemd starts it at boot with no generated scripts; `podman run` stays the ad-hoc path. |
| API key | `--env-file` / quadlet `EnvironmentFile=` pointing at a 0600 file holding `SANDBIN_API_KEY` | Keeps the key out of shell history and out of the container command line. `main.go` already reads that variable. |
| Health probe | new unauthenticated `GET /healthz` returning `ok`, asked for by quadlet `HealthCmd=` and `podman run --health-cmd` | `GET /` answers 405 and bin paths need credentials, so nothing existing answers 200. The Containerfile `HEALTHCHECK` alone does not work: podman drops it from its default OCI image format. |
