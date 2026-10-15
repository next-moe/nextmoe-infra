#!/usr/bin/env bash
#
# restore-dev-seed.sh — collaborator-facing: download the rolling dev-seed
# release and restore it into a local Postgres. Needs only `gh` (authenticated
# with read access to the repo), `psql` and `pg_restore`.
#
#   ./scripts/restore-dev-seed.sh --yes
#
# Target coordinates (override via env):
#   SEED_PG_HOST=localhost SEED_PG_PORT=5432 SEED_PG_USER=postgres
#   PGPASSWORD=...            (password for that user, if any)
#   DEVSEED_SUFFIX=           (appended to every database name; use e.g.
#                              _seed if the plain names already hold data
#                              you care about — restore DROPS the targets)
#   DEVSEED_DIR=              (restore a local build-seed.sh output dir
#                              instead of downloading the release)

set -euo pipefail

SEED_REPO="${SEED_REPO:-next-moe/nextmoe-infra}"
SEED_TAG="${SEED_TAG:-dev-seed}"
SEED_PG_HOST="${SEED_PG_HOST:-localhost}"
SEED_PG_PORT="${SEED_PG_PORT:-5432}"
SEED_PG_USER="${SEED_PG_USER:-postgres}"
DEVSEED_SUFFIX="${DEVSEED_SUFFIX:-}"
DEVSEED_DIR="${DEVSEED_DIR:-}"

if [[ "${1:-}" != "--yes" ]]; then
  cat >&2 <<EOF
This DROPS and recreates the seed databases (suffix: '${DEVSEED_SUFFIX}') on
${SEED_PG_HOST}:${SEED_PG_PORT}. If any of those names already hold data you
care about, set DEVSEED_SUFFIX or abort. Re-run with --yes to proceed.
EOF
  exit 1
fi

PSQL=(psql -h "${SEED_PG_HOST}" -p "${SEED_PG_PORT}" -U "${SEED_PG_USER}" -v ON_ERROR_STOP=1 -q -d postgres)
if [[ -n "${DEVSEED_DIR}" ]]; then
  WORK="$(readlink -f "${DEVSEED_DIR}")"
  echo "using local build ${WORK}"
else
  WORK="$(mktemp -d)"
  trap 'rm -rf "${WORK}"' EXIT
  echo "downloading ${SEED_REPO}@${SEED_TAG} ..."
  gh release download "${SEED_TAG}" -R "${SEED_REPO}" -D "${WORK}"
fi
(cd "${WORK}" && sha256sum -c SHA256SUMS)

for dump in "${WORK}"/*.dump; do
  db="$(basename "${dump}" .dump)${DEVSEED_SUFFIX}"
  echo "restoring ${db}"
  "${PSQL[@]}" -c "DROP DATABASE IF EXISTS ${db}"
  "${PSQL[@]}" -c "CREATE DATABASE ${db}"
  pg_restore -h "${SEED_PG_HOST}" -p "${SEED_PG_PORT}" -U "${SEED_PG_USER}" \
    --no-owner --no-privileges -d "${db}" "${dump}"
done

echo "done. Every seeded user's password is: kungal-dev"
