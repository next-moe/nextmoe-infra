#!/bin/sh
# Daily watch on the 月幕 (ymgal) review queue.
#
# WHY this job exists: every other news lane drains itself. 批评 is a standing
# blanket release, so its items go straight to published and a backlog cannot
# form. ymgal is the opposite by promise — 苍麟 was told it would never be swept
# with the rest, so every item waits for a human in the console. Nothing told
# that human the queue had items, which made "nobody looked for a fortnight" a
# silent state rather than a reported one.
#
# It runs no Go tool, so it is the one job here outside the infra-tools
# invariant: the whole check is a count, and `docker exec` into the postgres
# container reads it without a DSN or a password on any command line.
#
# Thresholds are judgment, not physics. ymgal publishes roughly one item a day,
# so a queue that is a week old means the review stopped, and twenty waiting
# means it stopped a while ago. Either one is worth an email; neither is worth
# one on the day it happens.
#
# Canonical copy: scripts/prod-cron/ymgal-pending-watch/run.sh in nextmoe-infra
# — the deployed copy is /root/ymgal-pending-watch/run.sh and must stay
# byte-identical; scp is the only sync mechanism.
set -eu
BASE=/root/ymgal-pending-watch
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

AGE_DAYS=${AGE_DAYS:-7}
ALERT_AT=${ALERT_AT:-20}

on_exit() {
  rc=$?
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] ymgal pending watch (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== ymgal pending watch start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
REPORT="$BASE/state/pending-$(date -u +%F).txt"

# status 0 is StatusPending (internal/platform/news/model/news.go).
docker exec "$PG" psql -U postgres -d kun_news -At -F'|' -c \
  "SELECT count(*), COALESCE(max(now()::date - created_at::date), 0)
     FROM news_item WHERE source_key = 'ymgal' AND status = 0" > "$REPORT"

N=$(cut -d'|' -f1 "$REPORT")
AGE=$(cut -d'|' -f2 "$REPORT")
: "${N:=0}"; : "${AGE:=0}"
echo "ymgal pending: $N item(s), oldest $AGE day(s) (alert at count>=$ALERT_AT or age>=$AGE_DAYS)"

if [ "$N" -gt 0 ] && { [ "$N" -ge "$ALERT_AT" ] || [ "$AGE" -ge "$AGE_DAYS" ]; }; then
  docker exec "$PG" psql -U postgres -d kun_news -c \
    "SELECT id, external_id, left(title, 60) AS title, created_at
       FROM news_item WHERE source_key = 'ymgal' AND status = 0
       ORDER BY created_at LIMIT 30" >> "$REPORT" || true
  /root/lib/alert.sh "[ymgal] $N item(s) awaiting review, oldest $AGE day(s)" "$REPORT" \
    || echo "alert delivery failed"
fi

find state -name 'pending-*.txt' -mtime +90 -delete
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== ymgal pending watch done $(date -u '+%F %T')Z ==="
