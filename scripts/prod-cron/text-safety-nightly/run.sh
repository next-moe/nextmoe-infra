#!/bin/sh
# Nightly text-safety lane: judge the work titles, work intros and character
# intros that changed since the last run, then fold the title verdicts into the display axis
# (catalog_work.title_nsfw, and display_nsfw on claimed works no human set).
#
# The model is Cursor-hosted and reached through the infra-cursor-llm sidecar,
# an OpenAI-compatible shim over the Cursor SDK that this script starts on
# dokploy-network for the length of the run and removes on exit. Its key lives
# in $BASE/cursor.env (CURSOR_API_KEY=..., mode 600), written by the owner on
# the box.
#
# crontab (root): 30 16 * * * /root/text-safety-nightly/run.sh
# 16:30 CST = 08:30 UTC, after intromt-nightly (13:00 CST) has written the
# day's machine-translated intros, so they are judged the same day.
#
# Runs from the infra-tools and infra-cursor-llm images resolved below; nothing
# is staged on this host. Canonical copy:
# scripts/prod-cron/text-safety-nightly/run.sh in nextmoe-infra — installing or
# updating it on the box is a manual scp over /root/text-safety-nightly/run.sh.
set -eu
BASE=${TS_BASE:-/root/text-safety-nightly}
ALERT_SH=${TS_ALERT:-/root/lib/alert.sh}
REINDEX_SH=${TS_REINDEX:-/root/reindex-catalog/run.sh}
CURSOR_ENV=${TS_CURSOR_ENV:-$BASE/cursor.env}
SIDECAR=text-safety-llm
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  docker rm -f "$SIDECAR" >/dev/null 2>&1 || true
  for f in env.tmp llm.tmp; do
    if [ -f "$f" ]; then shred -u "$f"; fi
  done
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] text safety nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== text safety nightly start $(date -u '+%F %T')Z ==="

resolve() {
  docker pull -q "$1" >/dev/null 2>&1 || echo "WARN: image pull failed for $1; using the local copy" >&2
  docker image inspect --format '{{index .RepoDigests 0}}' "$1"
}
IMG=$(resolve ghcr.io/next-moe/infra-tools:latest)
LLM_IMG=$(resolve ghcr.io/next-moe/infra-cursor-llm:latest)
echo "image: $IMG"
echo "llm image: $LLM_IMG"

CATC=kun-visual-novel-infra-vqvqbc-catalog-1
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" | grep -v '^$' > env.tmp
chmod 600 env.tmp

# The sidecar's bearer token lives only as long as this run.
TOKEN=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
umask 077
printf 'LLM_TOKEN=%s\nKUN_TEXT_SAFETY_LLM_TOKEN=%s\nKUN_TEXT_SAFETY_LLM_BASE=http://%s:8790/v1\n' \
  "$TOKEN" "$TOKEN" "$SIDECAR" > llm.tmp
umask 022

docker rm -f "$SIDECAR" >/dev/null 2>&1 || true
docker run -d --name "$SIDECAR" --network dokploy-network \
  --env-file "$CURSOR_ENV" --env-file "$BASE/llm.tmp" -e LLM_CONCURRENCY=8 "$LLM_IMG" >/dev/null
i=0
until docker exec "$SIDECAR" node -e "fetch('http://127.0.0.1:8790/healthz').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))" 2>/dev/null; do
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    echo "=== sidecar never became healthy ==="
    docker logs --tail 50 "$SIDECAR" || true
    exit 1
  fi
  sleep 2
done

# The password rides PGPASSWORD, never the DSN (lib/dsn-test.sh).
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; DSN="host=$KUN_PG_HOST port=$KUN_PG_PORT user=$KUN_PG_USER dbname=$KUN_CATALOG_PG_DATABASE sslmode=disable"'

step() {
  _name=$1
  shift
  echo "--- $_name ---"
  docker run --rm --name "text-safety-$_name" --network dokploy-network \
    --env-file "$BASE/env.tmp" --env-file "$BASE/llm.tmp" "$IMG" \
    sh -c "$DSNSH"'; exec text-safety -dsn "$DSN" '"$*"
}

MARK=$(wc -c < "$LOG")
# apply before the intros: the intro lanes judge only what the sfw shelf shows,
# so a work the title verdicts moved onto it is judged tonight, not tomorrow.
step titles -mode titles -model grok-4.7:low:fast -concurrency 8
step apply -mode apply -run -prune
step intros -mode intros -model grok-4.6:low:fast -concurrency 8
step char-intros -mode char-intros -model grok-4.6:low:fast -concurrency 8
docker logs --tail 5 "$SIDECAR" || true

# A title verdict that moved a work's shelf changes the content_limit the search
# index serves; the reindex owns its own lock, alerting and stamp.
if tail -c "+$((MARK + 1))" "$LOG" | grep -Eq 'apply run=true .*(title_written|display_written)=[1-9]'; then
  echo "--- shelf moved - triggering catalog reindex ---"
  "$REINDEX_SH" || echo "reindex-catalog exited $? (its own alerting covers this)"
fi

find logs -name 'run-*.log' ! -name "run-$(date +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== text safety nightly done $(date -u '+%F %T')Z ==="
