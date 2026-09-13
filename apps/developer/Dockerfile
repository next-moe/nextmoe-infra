#
# NextMoe developer portal (developer.nextmoe.dev) — Nuxt 4, Nitro node-server.
# Modelled on docker/nuxt.Dockerfile but self-contained for apps/developer.
#
# Build context MUST be the repo root: the pnpm workspace install needs the
# lockfile + every workspace manifest. (The app consumes @kungal/ui-* + Scalar
# from npm — no local Nuxt layer to copy.)
#
# No browser-facing build args: the portal exposes NO public runtime config —
# its only backend base (NUXT_OAUTH_API_BASE) is SERVER-side and read at RUNTIME
# by the /api/** Nitro relay. The bundled public specs (app/generated/specs.ts)
# are committed, so no docs/ copy or build-time sync is needed.
#
# Do NOT add `sync:specs` to the build script: this stage copies apps/developer
# only, and that script reads docs/catalog/v2-openapi.yaml from the REPO ROOT.
# It runs in `dev` and in the docs-model CI gate, which is what keeps
# app/generated (docs model + compiled guides) from going stale.
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-trixie-slim AS base
RUN corepack enable
WORKDIR /repo

# ---- deps: copy the needed workspace manifests, install the developer subgraph -
# Copy only the manifests this install actually resolves (like nuxt.Dockerfile:
# APP + api). A COPY of another app's manifest breaks the whole build-and-push
# run the moment that app is removed — and only on push to main, because the
# image workflow does not run on PRs (the 2026-09-13 cutover deploy died on a
# stale apps/web line here).
FROM base AS deps
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/api/package.json        apps/api/package.json
COPY apps/developer/package.json  apps/developer/package.json
# --ignore-scripts: the app's `postinstall: nuxt prepare` can't run here (app
# source isn't copied yet); the later `nuxt build` runs prepare itself.
RUN pnpm install --frozen-lockfile --ignore-scripts --filter "developer..."

# ---- build ----
FROM deps AS build
COPY apps/developer apps/developer
RUN pnpm --filter developer run build

# ---- run: just Node + the self-contained .output (no pnpm, no sources) ----
FROM node:${NODE_VERSION}-trixie-slim AS run
ENV NODE_ENV=production HOST=0.0.0.0 NITRO_PORT=3000
WORKDIR /app
COPY --from=build /repo/apps/developer/.output ./.output
USER node
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
