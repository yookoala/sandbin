FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY skill.go ./
COPY skills ./skills
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sandbin ./cmd/sandbin

FROM docker.io/library/alpine:3.22
RUN adduser -D -u 1000 sandbin && mkdir -p /data && chown sandbin:sandbin /data
COPY --from=build /out/sandbin /usr/local/bin/sandbin
USER 1000:1000
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/usr/local/bin/sandbin"]
CMD ["serve", "--db", "/data/sandbin.db"]
# podman drops this line when it builds the default OCI format; deploy/sandbin.container and
# the run command in docs/usages/sandbin-podman.md carry the same probe as --health-cmd,
# which podman always honours.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
