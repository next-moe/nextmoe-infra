#!/usr/bin/env bash
# One-command local dev for this repo. Two halves, one command:
#
#   1. PLATFORM BASE (docker-compose.dev.yml, default profile) — redis / minio /
#      opensearch / mailpit / the read-through image proxy / and the migrations an
#      infra session actually needs, from ONE prebuilt GHCR image
#      (infra-migrate, invoked once per target DB). Brought up once, then LEFT
#      RUNNING.
#   2. HOT-RELOAD STACK — `air` rebuilds the five frequently-edited Go services
#      (oauth / catalog / image / artifact / trust) from source on every save,
#      plus the Nuxt frontends (account / admin / developer) via their own dev servers.
#
# The base and the hot stack never collide: the five hot services carry the
# `full` compose profile, so the default `up` below deliberately does NOT start
# them — their host ports (9277-9279, 9281, 9283) stay free for air.
#
# community / ai are `full`-profile too: nothing a bare `pnpm dev` runs dials
# them, and starting them cost every contributor two image pulls and two idle
# containers. Their consumers are the PRODUCT repos → `pnpm dev:full`.
#
# Ctrl-C stops ONLY the hot stack; the base keeps running (that's the point —
# "localhost has a platform"). Tear the base down with `pnpm dev:down`.
#
#   --full   bring the WHOLE platform up from images (incl. the five hot
#            services + community / ai), do NOT run air/frontends — for
#            developing a PRODUCT repo (letmoe / forum / …), not infra.
#            `pnpm dev:full`.
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE=(docker compose -f docker-compose.dev.yml)
PROFILE_ARGS=()
FULL=0
[[ "${1:-}" == "--full" ]] && { FULL=1; PROFILE_ARGS=(--profile full); }

# Preflight. ~2s against a several-minute first run, and it is what stands
# between a contributor and a migrate container that exits 1 with no explanation
# (missing database / wrong password / Docker Desktop's VM loopback). Run
# unconditionally rather than only on a fresh checkout: the half-succeeded retry
# is exactly when it is needed. SKIP_DOCTOR=1 to bypass.
[[ "${SKIP_DOCTOR:-0}" == 1 ]] || ./scripts/dev-doctor.sh || exit 1

# The box may already provide a backing service on its shared port (a native
# redis on :6379 is common). The compose copy uses host networking, so it would
# fail to bind — and the shared env points every service at 127.0.0.1:<port>
# regardless. So: if a port is already answering, scale that compose service to
# zero and use what's already there. (This is why the dev compose does not
# start-gate on redis.)
declare -A SHAREABLE=([redis]=6379 [mailpit]=1025)
port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && exec 3>&-; }

SCALE_ARGS=()
for svc in "${!SHAREABLE[@]}"; do
  if port_busy "${SHAREABLE[$svc]}"; then
    echo "  · :${SHAREABLE[$svc]} already served — using it, not starting compose '$svc'"
    SCALE_ARGS+=(--scale "$svc=0")
  fi
done

# Phase 0 — refresh every image this profile is about to run (one manifest
# check each; offline is fine, keep going on a pull failure). Not optional
# politeness: a stale infra-migrate predates the subcommand CLI, ignores the
# target argument (flag.Parse skips positional args) and migrates the PLATFORM
# database instead of the one the service is named after — a silent no-op on
# the schema you actually wanted. Service images rot the same way (an infra-
# catalog left over from 07-30 served a two-week-old contract on :9281).
"${COMPOSE[@]}" "${PROFILE_ARGS[@]}" pull --quiet --ignore-pull-failures 2>/dev/null || true

# Phase 1 — start the base. NO --wait here: the run-once jobs (migrate* /
# minio-setup) exit 0 when done, and `up --wait` treats a top-level service that
# exits as a failure. So start everything first, then wait only on the
# long-running services below (there the run-once jobs are depends_on
# `service_completed_successfully`, which --wait handles correctly).
echo "▶ platform base: storage + migrations…"
"${COMPOSE[@]}" "${PROFILE_ARGS[@]}" up -d "${SCALE_ARGS[@]}"

# Phase 2 — block until every long-running service (restart != "no") is healthy,
# minus the ones we scaled to zero. This is what gates air on a migrated DB.
mapfile -t WAIT_SVCS < <(
  "${COMPOSE[@]}" "${PROFILE_ARGS[@]}" config --format json \
    | jq -r '.services | to_entries[]
             | select((.value.restart // "") != "no")
             | select((.value.scale // 1) != 0)
             | .key'
)
# Drop the scaled-to-zero backing services from the wait set.
for svc in "${!SHAREABLE[@]}"; do
  port_busy "${SHAREABLE[$svc]}" && WAIT_SVCS=("${WAIT_SVCS[@]/$svc}")
done
# shellcheck disable=SC2206
WAIT_SVCS=(${WAIT_SVCS[@]})   # re-pack after removals
echo "  waiting for: ${WAIT_SVCS[*]}"
"${COMPOSE[@]}" "${PROFILE_ARGS[@]}" up -d --wait "${WAIT_SVCS[@]}"

if ((FULL)); then
  echo "✔ whole platform up from images. Now run your product repo's \`pnpm dev\`."
  exit 0
fi

# Phase 3 — re-run the migrations FROM THE WORKING TREE. The image above
# migrates to main-as-of-its-last-CI-build; air serves the working tree, and
# those clocks differ whenever a branch is ahead of main. 2026-08-14: wave
# 205's rating columns were in the tree but not yet on main, so the image
# migrate ran green while every air work-detail request failed on
# `column "distribution" does not exist`. A newer image cannot close that
# window — only migrating from the same tree air serves can. Same targets as
# the bare-profile migrate-* jobs (which stay: they pair with the image
# services they gate); idempotent, so the double run costs nothing.
echo "▶ migrations from the working tree…"
(cd apps/api
  go run ./cmd/migrate
  go run ./cmd/migrate catalog
  go run ./cmd/migrate trust
  go run ./cmd/migrate news)

echo "▶ hot-reload stack: air (oauth/catalog/image/artifact/trust) + frontends."
echo "  Ctrl-C stops these; the base above stays up (pnpm dev:down to stop it)."
exec pnpm -F "./apps/**" --parallel --stream run dev
