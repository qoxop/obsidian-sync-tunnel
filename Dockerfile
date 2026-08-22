# syntax=docker/dockerfile:1.7

FROM golang:1.26.6-bookworm AS builder

ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/obsidian-sync-server ./cmd/obsidian-sync-server && \
    mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="Obsidian Sync Tunnel" \
      org.opencontainers.image.description="Self-hosted Obsidian synchronization server" \
      org.opencontainers.image.licenses="MIT"

WORKDIR /app
COPY --from=builder --chown=65532:65532 /out/obsidian-sync-server /app/obsidian-sync-server
COPY --from=builder --chown=65532:65532 /out/data /data

USER 65532:65532
EXPOSE 8787 8788
VOLUME ["/data"]
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/app/obsidian-sync-server", "healthcheck", "--url", "http://127.0.0.1:8787/healthz", "--timeout", "3s"]

ENTRYPOINT ["/app/obsidian-sync-server"]
CMD ["serve", "--listen", "0.0.0.0:8787", "--allow-non-loopback", "--admin-listen", "0.0.0.0:8788", "--allow-admin-non-loopback", "--database", "/data/sync.db", "--admin-token-file", "/run/secrets/sync_admin_token", "--log", "/data/server.jsonl"]
