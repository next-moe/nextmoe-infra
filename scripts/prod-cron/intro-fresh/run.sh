#!/bin/sh
# Every 20 minutes: machine-translate and safety-judge the intros written since
# the nightlies ran, so an editor save reaches readers the same hour.
#
# Before it, a saved intro waited for intromt-nightly (13:00 CST) and then
# text-safety-nightly (16:30 CST), and the sfw face serves no intro without a
# verdict. On 2026-11-21 a Japanese intro saved at 21:37 CST for work 279247
# would have stayed invisible to sfw readers until 16:30 the next day, while
# the stale machine row kept showing the model's answer to a VNDB voting prompt.
#
# The MT lanes read only intro rows written within WINDOW (--changed-since), so
# an idle run is a few scans of the intro tables and no model call.
# The text-safety lanes judge only intros with no verdict yet.
#
# It never runs beside the nightlies: their locks are probed (not held) first,
# so a nightly that starts while this runs still gets its lock-free start.
#
# Canonical copy: scripts/prod-cron/intro-fresh/run.sh in nextmoe-infra — the
# deployed copy is /root/intro-fresh/run.sh and must stay byte-identical.
set -eu
BASE=${FRESH_BASE:-/root/intro-fresh}
ALERT_SH=${FRESH_ALERT:-/root/lib/alert.sh}
MT_BASE=${FRESH_MT_BASE:-/root/intromt-nightly}
TS_BASE=${FRESH_TS_BASE:-/root/text-safety-nightly}
TS_LLM_ENV=${FRESH_TS_LLM_ENV:-/root/env-llm-key.env}
WINDOW=${FRESH_WINDOW:-2h}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  if [ -f "$BASE/env.tmp" ]; then shred -u "$BASE/env.tmp"; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] intro fresh (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT

for lock in "$MT_BASE/.lock" "$TS_BASE/.lock"; do
  if ! (flock -n 8) 8>"$lock"; then
    echo "$(date -u '+%F %T')Z a nightly holds $lock; skip"
    exit 0
  fi
done
echo "=== intro fresh start $(date -u '+%F %T')Z window=$WINDOW ==="

docker pull -q ghcr.io/next-moe/infra-tools:latest >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/next-moe/infra-tools:latest)

# The password rides PGPASSWORD, never the DSN (lib/dsn-test.sh).
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; DSN="host=$KUN_PG_HOST port=$KUN_PG_PORT user=$KUN_PG_USER dbname=$KUN_CATALOG_PG_DATABASE sslmode=disable"'

FAILED=""
mt() {
  _name=$1
  shift
  echo "--- $_name ---"
  if ! docker run --rm --name "intro-fresh-$_name" --network dokploy-network \
    --env-file "$MT_BASE/app.env" --env-file "$MT_BASE/llm.env" "$IMG" \
    sh -c "$DSNSH"'; exec '"$*"' --dsn "$DSN" --changed-since '"$WINDOW"' --apply --max-tokens 24576 --workers 2'; then
    echo "!!! $_name failed"
    FAILED="$FAILED $_name"
  fi
}

mt entity entity-intro-mt
for POP in claimed bodyless; do
  for SL in ja en; do
    mt "work-$POP-$SL" intro-mt --population "$POP" --source-lang "$SL" --top 0
  done
done

umask 077
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' kun-visual-novel-infra-vqvqbc-catalog-1 | grep -v '^$' > "$BASE/env.tmp"
umask 022
for MODE in intros char-intros; do
  echo "--- safety $MODE ---"
  if ! docker run --rm --name "intro-fresh-safety-$MODE" --network dokploy-network \
    --env-file "$BASE/env.tmp" --env-file "$TS_LLM_ENV" "$IMG" \
    sh -c "$DSNSH"'; exec text-safety -dsn "$DSN" -mode '"$MODE"' -model catalog.text-safety.intro -concurrency 3'; then
    echo "!!! safety $MODE failed"
    FAILED="$FAILED safety-$MODE"
  fi
done

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +14 -delete
if [ -n "$FAILED" ]; then
  echo "=== intro fresh had failed lane(s):$FAILED ==="
  exit 1
fi
echo "=== intro fresh done $(date -u '+%F %T')Z ==="
