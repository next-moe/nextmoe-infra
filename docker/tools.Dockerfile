#
# Tools image: EVERY apps/api/cmd/* binary in one image, for the one-off
# migration / maintenance jobs the per-service images don't carry — e.g.
# reindex-catalog, migrate-moemoepoint, the catalog backfills
# (see docs/deploy/03-bootstrap.md §B). Wave 161 retired the galgame-family
# binaries this comment used to name (migrate-galgame-data, dedup-galgame-alias,
# reindex-search, sync-vndb, ...) together with the tables they operated on.
#
# The per-service Dockerfiles build ONE binary (ARG CMD) so you can't override
# the entrypoint of infra-catalog to run reindex-catalog — that binary isn't in it.
# This image bundles them all and invokes a job by name (env-file = a dump of
# the target service container's .Config.Env, see docs/deploy/16-data-cutover.md):
#
#   docker run --rm --network dokploy-network \
#     --env-file /root/infra.env ghcr.io/next-moe/infra-tools reindex-catalog
#
# Built CGO_ENABLED=1 + libwebp so the cgo cmds (image*, oauth) compile too; the
# rest are pure Go. Build context MUST be the repo root.
ARG GO_VERSION=1.27

# ---- build (libwebp headers for the cgo cmds; pure-Go cmds build the same) ----
FROM golang:${GO_VERSION}-trixie AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
        libwebp-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
# -o <dir>/ writes one binary per cmd package, named after its directory.
RUN mkdir -p /out && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/ ./cmd/...

# ---- run (debian-slim + libwebp runtime; binaries on PATH, invoked by name) ----
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        libwebp7 libsharpyuv0 ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /usr/sbin/nologin appuser
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
# image* cmds read image_presets.yaml from this absolute path.
COPY apps/api/configs /app/configs
ENV KUN_IMAGE_PRESETS_PATH=/app/configs/image_presets.yaml
USER appuser
# No ENTRYPOINT: run a job by name, e.g. `docker run ... infra-tools reindex-catalog`.
