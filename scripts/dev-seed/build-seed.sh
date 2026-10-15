#!/usr/bin/env bash
#
# build-seed.sh — build the lightweight dev-seed dumps from the local
# desensitised snapshot databases. See config.sh for the model.
#
# For each DB in SEED_DBS:
#   1. drop + recreate <db>_seedbuild
#   2. pipe a plain-SQL copy of the local <db> into it
#   3. run prune/<db>.sql inside it (deletes down to a few hundred entities,
#      FK constraints stay enforced, so closure is guaranteed by construction)
#   4. pg_dump -Fc the result into the output dir
#
# Usage: ./build-seed.sh [--only <db>]   (--only reuses existing export CSVs)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=config.sh
source "${SCRIPT_DIR}/config.sh"

ONLY_DB=""
if [[ "${1:-}" == "--only" ]]; then
  ONLY_DB="${2:?--only requires a database name}"
fi

PSQL=(psql -h "${LOCAL_PG_HOST}" -p "${LOCAL_PG_PORT}" -U "${LOCAL_PG_USER}" -v ON_ERROR_STOP=1 -q)
STAMP="$(date +%Y%m%d-%H%M)"
OUT_DIR="${SEED_OUT_ROOT}/${STAMP}"
# --only refreshes one dump in place inside the newest full build, so the
# output dir stays a complete publishable set.
if [[ -n "${ONLY_DB}" && -d "${SEED_OUT_ROOT}/latest" ]]; then
  OUT_DIR="$(readlink -f "${SEED_OUT_ROOT}/latest")"
fi
EXPORT_DIR="${SEED_OUT_ROOT}/exports"
mkdir -p "${OUT_DIR}" "${EXPORT_DIR}"

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }

for db in "${SEED_DBS[@]}"; do
  [[ -n "${ONLY_DB}" && "${db}" != "${ONLY_DB}" ]] && continue
  build="${db}${SEED_BUILD_SUFFIX}"

  log "${db}: rebuilding scratch ${build}"
  # A leftover connection (e.g. from an aborted run) makes DROP DATABASE fail.
  "${PSQL[@]}" -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${build}' AND pid <> pg_backend_pid()" >/dev/null
  "${PSQL[@]}" -d postgres -c "DROP DATABASE IF EXISTS ${build}"
  "${PSQL[@]}" -d postgres -c "CREATE DATABASE ${build}"

  log "${db}: copying local snapshot into ${build}"
  pg_dump -h "${LOCAL_PG_HOST}" -p "${LOCAL_PG_PORT}" -U "${LOCAL_PG_USER}" \
    --no-owner --no-privileges -d "${db}" \
    | "${PSQL[@]}" -d "${build}" >/dev/null

  # CWD = export dir: psql's \copy does NOT interpolate variables (verified on
  # psql 18), so prune scripts address export CSVs by bare relative filename.
  log "${db}: pruning to seed size"
  (cd "${EXPORT_DIR}" && "${PSQL[@]}" -d "${build}" \
    -v export_dir="${EXPORT_DIR}" \
    -v seed_works="${SEED_WORKS}" \
    -v seed_topics="${SEED_TOPICS}" \
    -v seed_users="${SEED_USERS}" \
    -v dev_email_domain="${DEV_EMAIL_DOMAIN}" \
    -v dev_secret_prefix="${DEV_SECRET_PREFIX}" \
    -f "${SCRIPT_DIR}/prune/${db}.sql")

  log "${db}: dumping seed"
  pg_dump -h "${LOCAL_PG_HOST}" -p "${LOCAL_PG_PORT}" -U "${LOCAL_PG_USER}" \
    --no-owner --no-privileges -Fc -Z6 -d "${build}" \
    -f "${OUT_DIR}/${db}.dump"

  "${PSQL[@]}" -d postgres -c "DROP DATABASE ${build}"
done

log "writing manifest + checksums"
{
  echo "stamp: $(basename "${OUT_DIR}")"
  echo "source: local desensitised snapshot databases (refresh-dev-db)"
  echo "restore: scripts/restore-dev-seed.sh (or pg_restore --no-owner per dump)"
  for f in "${OUT_DIR}"/*.dump; do
    printf '%s  %s\n' "$(du -h "$f" | cut -f1)" "$(basename "$f")"
  done
} > "${OUT_DIR}/MANIFEST.txt"
(cd "${OUT_DIR}" && sha256sum ./*.dump > SHA256SUMS)

ln -sfn "${OUT_DIR}" "${SEED_OUT_ROOT}/latest"
log "done → ${OUT_DIR}"
cat "${OUT_DIR}/MANIFEST.txt"
