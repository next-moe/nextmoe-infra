#
# Build for the CGO binaries: oauth + image. Both transitively import
# github.com/kolesa-team/go-webp (cgo → libwebp) — image encodes WebP directly,
# oauth via the image-admin endpoints it embeds (image/service imports the
# processor). So neither can be a CGO_ENABLED=0 static/distroless binary; both
# need libwebp at build (-lwebp) and at runtime (libwebp.so). The pure-Go
# services + tools (galgame, migrate-*, workers) use go.Dockerfile instead.
#
#   docker build -f docker/cgo.Dockerfile --build-arg CMD=oauth -t kun-oauth .
#   docker build -f docker/cgo.Dockerfile --build-arg CMD=image -t kun-image .
ARG GO_VERSION=1.27

# ---- build (needs libwebp headers + pkg-config for the cgo link) ----
FROM golang:${GO_VERSION}-trixie AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
        libwebp-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
ARG CMD=image
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/app ./cmd/${CMD}

# ---- run (debian-slim + just the libwebp runtime libs, nonroot) ----
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        libwebp7 libsharpyuv0 ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /usr/sbin/nologin appuser
WORKDIR /app
COPY --from=build /out/app /app/app
# The image service loads image_presets.yaml at runtime from a repo-relative
# default path; ship it + point at the absolute path. oauth ignores it.
COPY apps/api/configs/image_presets.yaml /app/configs/image_presets.yaml
ENV KUN_IMAGE_PRESETS_PATH=/app/configs/image_presets.yaml
# oauth runs the in-process job scheduler; the sync-vndb / sync-vndb-enrich
# jobs parse docs/tagMap.ts at runtime (relative to WORKDIR /app). Ship it so
# the scheduled VNDB jobs don't fail with "open docs/tagMap.ts: no such file"
# (image doesn't use it — harmless extra ~200KB).
COPY docs/tagMap.ts /app/docs/tagMap.ts
USER appuser
ENTRYPOINT ["/app/app"]
