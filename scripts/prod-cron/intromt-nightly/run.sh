#!/bin/sh
# combined MT nightly (refs/proj/75 resident + refs/proj/173 full-sweep):
# entity intros (characters/persons/labels) then work intros over both
# populations and both source languages, unlimited top. fill-missing +
# src_hash make every run idempotent — an idle night costs zero LLM calls
# (candidate scans only).
#
# Runs intro-mt / entity-intro-mt from the infra-tools image resolved below;
# nothing is staged on this host. LLM + app env live in $BASE/app.env and
# $BASE/llm.env (moved here from /root/wave75 at the 2026-08-13 cutover).
# Canonical copy: scripts/prod-cron/intromt-nightly/run.sh in nextmoe-infra
# — edit there and redeploy.
set -eu
BASE=/root/intromt-nightly
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
# Same two-layer reporting as the other maintenance crons: the trap catches a
# run that failed, and state/last-success lets /root/lib/watchdog.sh catch a
# job that stopped running at all. Lanes keep going when one fails (they are
# independent corpora), but ANY failed lane fails the run — the old shape
# swallowed lane failures with `|| echo WARN`, which is how a lane can die
# nightly for a week without a single alert.
FAIL=0
on_exit() {
  rc=$?
  [ "$rc" -eq 0 ] && [ "$FAIL" -ne 0 ] && rc=1
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] mt nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== mt nightly start $(date -u '+%F %T')Z ==="

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
#
# Parallel query disabled per session: the postgres container
# runs with the 64MB docker-default /dev/shm (SQLSTATE 53100 on big joins).
MTDSN='export PGPASSWORD="$KUN_PG_PASSWORD"; DSN="host=$KUN_PG_HOST port=$KUN_PG_PORT user=$KUN_PG_USER dbname=$KUN_CATALOG_PG_DATABASE sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0'"'"'"'

echo "--- entity lanes (character/person/label) ---"
docker run --rm --name entitymt-nightly --network dokploy-network \
  --env-file "$BASE/app.env" --env-file "$BASE/llm.env" "$IMG" \
  sh -c "$MTDSN"'; exec entity-intro-mt --dsn "$DSN" --apply --max-tokens 24576 --delay-ms 1500 --workers 4' \
  || { echo "WARN: entity lanes exited $?"; FAIL=1; }
for POP in claimed bodyless; do
  for SL in ja en; do
    echo "--- work $POP $SL ---"
    docker run --rm --name "intromt-nightly-$POP-$SL" --network dokploy-network \
      --env-file "$BASE/app.env" --env-file "$BASE/llm.env" "$IMG" \
      sh -c "$MTDSN"'; exec intro-mt --dsn "$DSN" --population '"$POP"' --source-lang '"$SL"' --top 0 --apply --max-tokens 24576 --delay-ms 1500 --workers 4' \
      || { echo "WARN: $POP-$SL exited $?"; FAIL=1; }
  done
done
find logs -name 'run-*.log' -mtime +30 -delete
[ "$FAIL" -eq 0 ] || { echo "=== mt nightly had failed lane(s) ==="; exit 1; }
echo "=== mt nightly done $(date -u '+%F %T')Z ==="
