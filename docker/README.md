# Docker build assets — nextmoe-infra

This repo is the ecosystem **hub** (identity / image / content catalog). This
directory holds the **Dockerfiles + init assets** the images are built from.
The compose files live at the repo root: `docker-compose.dev.yml` (local dev
platform, prebuilt GHCR images — see `docs/dev-environment.md`) and
`docker-compose.prod.yml` (Dokploy production). The old root
`docker-compose.yml` (local-build 15xxx stack) was retired at W5 of the
galgame-wiki retirement.

## Layout

| File | Builds | Base image | Why |
|---|---|---|---|
| `docker/go.Dockerfile` | catalog / artifact / community / chat / trust / ai / mcp / telemetry + `migrate` (pure Go) | `distroless/static` (~25–45 MB) | `CGO_ENABLED=0` static binary |
| `docker/cgo.Dockerfile` | **oauth + image** | `debian:trixie-slim` (~180 MB) | both transitively import `kolesa-team/go-webp` → cgo → **libwebp** at build + runtime |
| `docker/nuxt.Dockerfile` | account + admin (Nitro `node-server`) | `node:24-trixie-slim` (~390 MB) | self-contained `.output`; sharp comes via `@kungal/ui-nuxt`'s `@nuxt/image` |
| `docker/tools.Dockerfile` | every `cmd/*` binary in ONE image (`infra-tools`) | `debian:trixie-slim` | one-off migration / maintenance jobs |
| `docker/initdb.d/` | — | — | `CREATE DATABASE` bootstrap for a fresh Postgres |

Both Go Dockerfiles and the Nuxt one are **parametric** (`--build-arg CMD=…` /
`APP=…`) and require the **repo root** as build context (the pnpm workspace
install needs the lockfile + every workspace manifest).

> Why oauth needs cgo: it embeds the image-admin endpoints, and
> `image/service` imports the WebP `processor`. Extract that (or swap go-webp
> for a pure-Go encoder) and oauth could return to distroless.

## Quick start

- **Local dev**: `pnpm dev` from the repo root — brings up the platform base
  from `docker-compose.dev.yml` (prebuilt GHCR images, host networking,
  prod-matching ports 9277-9284) and hot-reloads the five frequently-edited Go
  services via `air`. Full model: [docs/dev-environment.md](../docs/dev-environment.md).
- **Production**: `docker-compose.prod.yml` via Dokploy (CI builds the images —
  [docs/deploy/13-registry-ci.md](../docs/deploy/13-registry-ci.md)).
- **Build one image locally** (repo root as context):
  `docker build -f docker/go.Dockerfile --build-arg CMD=catalog -t infra-catalog .`

## Configuration

- Backend: 12-factor environment variables. The dev compose inlines literal
  dev constants; prod inlines non-secrets + `${VAR}` from Dokploy's panel.
  `config.validate()` requires `KUN_PG_PASSWORD` + `JWT_SECRET` on every
  service.
- Frontends: public config (`apiBase`, oauth client, image CDN) is **baked at
  build** from the `PUBLIC_*` build args (mapped to the `KUN_*_NUXT_PUBLIC_*`
  names `nuxt.config.ts` reads). To build once and configure at runtime
  instead, set the canonical `NUXT_PUBLIC_*` env on the container — but note
  `oauthClientID`/`oauthRedirectURI` have awkward env-name mappings, which is
  why baking is the default here.

## Health checks

Distroless ships no shell/curl, so each Go service binary self-probes via a
`healthcheck` subcommand (`pkg/health`): the compose healthcheck runs
`/app healthcheck` (or `/app/app healthcheck` for the cgo images), which GETs
its own root `/healthz` and exits 0/1. Frontends use a Node TCP liveness probe.

## Notes / gotchas hit while building

- **No BuildKit/buildx** on this host → Dockerfiles avoid `--mount=type=cache`
  (plain layer caching only). Install buildx to re-enable cache mounts.
- **OpenSearch**: catalog search uses the `infra-opensearch` image
  (`docker/opensearch/Dockerfile`, OS + icu/kuromoji/pinyin). Indexes are
  derived from Postgres; rebuild with `go run ./cmd/reindex-catalog`.
- **sharp arch**: the Nuxt build bundles `sharp` for `linux-x64`; build + run
  both happen in linux-x64 containers, so they match. Don't copy host-built
  `.output` into the image.
- **Migrations**: the `migrate` / `migrate-catalog` / `migrate-*` jobs run on
  every `up` in the dev + prod composes and gate the services
  (`service_completed_successfully`). The one-off cross-repo data-cutover
  pipeline (migrate-galgame-data, migrate-moyu-galgame, …) ships in the
  `infra-tools` image instead (see `tools.Dockerfile`).

## Three-repo orchestration

In production every repo's compose joins the shared external `dokploy-network`;
the backing services (`postgres`/`redis`/`minio`/`opensearch`) are defined **only
here** (the hub). kungal + moyu services connect to `postgres:5432`,
`http://oauth:9277`, `http://catalog:9281`, etc.; Traefik fronts the lot by
domain (routes are compose-owned labels).

## Production hardening (not done here)

- Rotate all secrets; use `docker secret`/a vault, not `env_file`.
- Optional: build the cgo binaries fully static (musl + static libwebp) to put
  oauth/image back on `distroless/static`.
- Pin image digests; add resource limits; ship logs to a collector.
