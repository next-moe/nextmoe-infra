#
# Build for the Nuxt 4 frontends (Nitro node-server preset), APP-parameterized:
# APP=account (apps/account) and APP=admin (apps/admin) since the NextMoe
# rebrand split apps/web.
#
# Build context MUST be the repo root: the pnpm workspace install needs the
# lockfile + the target app's manifest. (The apps consume @kungal/ui-* from
# npm — no local Nuxt layer to copy.)
#
# Public runtime config (apiBase, image CDN) is read by nuxt.config.ts from
# custom KUN_* env names at BUILD time, so it is passed as build args and
# baked. (Nitro can still override any public key at runtime via the canonical
# NUXT_PUBLIC_* names — see docker/README.md.)
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-trixie-slim AS base
RUN corepack enable
WORKDIR /repo

# ---- deps: copy the workspace manifests, install only the target subgraph ----
FROM base AS deps
ARG APP=account
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/${APP}/package.json apps/${APP}/package.json
COPY apps/api/package.json    apps/api/package.json
# --ignore-scripts: the apps' `postinstall: nuxt prepare` can't run here (app
# source isn't copied yet); the later `nuxt build` runs prepare itself.
# Directory filter (./apps/…), not a name filter: the package `name` field and
# the directory are not guaranteed to match, and a name filter silently
# installs nothing when they drift.
RUN pnpm install --frozen-lockfile --ignore-scripts --filter "./apps/${APP}..."

# ---- build ----
FROM deps AS build
ARG APP=account
# Frontend public config, baked at build. Empty args fall back to the
# in-config defaults (`process.env.X || '<default>'`).
ARG PUBLIC_API_BASE=
ARG PUBLIC_IMAGE_CDN_BASE=
ENV KUN_VISUAL_NOVEL_NUXT_PUBLIC_API_BASE=${PUBLIC_API_BASE} \
    KUN_VISUAL_NOVEL_NUXT_PUBLIC_IMAGE_CDN_BASE=${PUBLIC_IMAGE_CDN_BASE}
COPY apps/${APP} apps/${APP}
RUN pnpm --filter "./apps/${APP}" run build

# ---- run: just Node + the self-contained .output (no pnpm, no sources) ----
FROM node:${NODE_VERSION}-trixie-slim AS run
ARG APP=account
ENV NODE_ENV=production HOST=0.0.0.0 NITRO_PORT=3000
WORKDIR /app
COPY --from=build /repo/apps/${APP}/.output ./.output
USER node
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
