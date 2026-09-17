#
# Parametric build for the PURE-GO binaries (no cgo): the galgame HTTP service
# and every one-off cmd/migrate-* / worker tool.
#
#   docker build -f docker/go.Dockerfile --build-arg CMD=galgame -t kun-galgame .
#   docker build -f docker/go.Dockerfile --build-arg CMD=migrate -t kun-migrate .
#
# Build context MUST be the repo root. oauth + image transitively import
# go-webp (cgo) and can't be built CGO_ENABLED=0 — they use cgo.Dockerfile.
ARG GO_VERSION=1.27

# ---- build ----
FROM golang:${GO_VERSION}-trixie AS build
WORKDIR /src
# Manifests first → module download layer is cached until go.mod/sum change.
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
ARG CMD=oauth
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/app ./cmd/${CMD}

# ---- run ----
# distroless/static: ~2MB base, no shell, nonroot. Bundles ca-certificates
# (outbound HTTPS: SMTP TLS, image_service, VNDB sync) + tzdata (Asia/Shanghai).
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
