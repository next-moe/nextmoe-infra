#!/bin/sh
# Nightly text-safety lane: judge the work titles, work intros and character
# intros that changed since the last run, then fold the title verdicts into the display axis
# (catalog_work.title_nsfw, and display_nsfw on claimed works no human set).
#
# Every lane reaches its model through the ai gateway with the cron access key
# in /root/env-llm-key.env (the file llm-adjudicate-nightly reads), by scene:
# the gateway console decides which model and fallback each scene uses.
#
# crontab (root): 30 16 * * * /root/text-safety-nightly/run.sh
# 16:30 CST = 08:30 UTC, after intromt-nightly (13:00 CST) has written the
# day's machine-translated intros, so they are judged the same day.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/text-safety-nightly/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/text-safety-nightly/run.sh.
set -eu
BASE=${TS_BASE:-/root/text-safety-nightly}
ALERT_SH=${TS_ALERT:-/root/lib/alert.sh}
REINDEX_SH=${TS_REINDEX:-/root/reindex-catalog/run.sh}
LLM_ENV=${TS_LLM_ENV:-/root/env-llm-key.env}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] text safety nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== text safety nightly start $(date -u '+%F %T')Z ==="

docker pull -q ghcr.io/next-moe/infra-tools:latest >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/next-moe/infra-tools:latest)
echo "image: $IMG"

CATC=kun-visual-novel-infra-vqvqbc-catalog-1
umask 077
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" | grep -v '^$' > env.tmp
umask 022

# The password rides PGPASSWORD, never the DSN (lib/dsn-test.sh).
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; DSN="host=$KUN_PG_HOST port=$KUN_PG_PORT user=$KUN_PG_USER dbname=$KUN_CATALOG_PG_DATABASE sslmode=disable"'

# A lane that fails is recorded and the run goes on: on 2026-11-18 the intro
# lane failed under `set -e` and took char-intros and the shelf reindex with it.
FAILED=""
step() {
  _name=$1
  shift
  echo "--- $_name ---"
  if ! docker run --rm --name "text-safety-$_name" --network dokploy-network \
    --env-file "$BASE/env.tmp" --env-file "$LLM_ENV" "$IMG" \
    sh -c "$DSNSH"'; exec text-safety -dsn "$DSN" '"$*"; then
    echo "!!! $_name failed"
    FAILED="$FAILED $_name"
  fi
}

MARK=$(wc -c < "$LOG")
# apply before the intros: the intro lanes judge only what the sfw shelf shows,
# so a work the title verdicts moved onto it is judged tonight, not tomorrow.
step titles -mode titles -model catalog.text-safety.title -concurrency 8
step release-titles -mode release-titles -model catalog.text-safety.title -concurrency 8
step title-review -mode title-review -model catalog.text-safety.review -concurrency 4
step apply -mode apply -run -prune
# Workers AI rate-limits per minute; 24 calls in flight answered 429 on most
# requests during the backfill, and llm-adjudicate saw it at 4.
step intros -mode intros -model catalog.text-safety.intro -concurrency 3
step char-intros -mode char-intros -model catalog.text-safety.intro -concurrency 3

# A title verdict that moved a work's shelf changes the content_limit the search
# index serves; the reindex owns its own lock, alerting and stamp.
if tail -c "+$((MARK + 1))" "$LOG" | grep -Eq 'apply run=true .*(title_written|display_written)=[1-9]'; then
  echo "--- shelf moved - triggering catalog reindex ---"
  "$REINDEX_SH" || echo "reindex-catalog exited $? (its own alerting covers this)"
fi

find logs -name 'run-*.log' ! -name "run-$(date +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
if [ -n "$FAILED" ]; then
  echo "=== text safety nightly had failed lane(s):$FAILED ==="
  exit 1
fi
echo "=== text safety nightly done $(date -u '+%F %T')Z ==="
