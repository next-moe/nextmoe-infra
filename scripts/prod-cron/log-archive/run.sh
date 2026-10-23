#!/bin/sh
# Every 5 minutes: append each running container's new log lines to
# archive/<container>/<UTC date>.log.gz, kept KEEP_DAYS days.
#
# Docker deletes a container's log together with the container, and Dokploy
# recreates every service it deploys: the 2026-10-23 deploy took the whole
# 2026-10-22 oauth/forum/moyu log with it while that day's outage was still
# being investigated. json-file rotation and logrotate's copytruncate drop
# older lines the same way. A recreated container loses at most one interval.
#
# Canonical copy: scripts/prod-cron/log-archive/run.sh in nextmoe-infra — the
# deployed copy is /root/log-archive/run.sh and must stay byte-identical.
set -eu
BASE=${LOG_ARCHIVE_BASE:-/root/log-archive}
ALERT_SH=${LOG_ARCHIVE_ALERT:-/root/lib/alert.sh}
KEEP_DAYS=${LOG_ARCHIVE_KEEP_DAYS:-30}
cd "$BASE"
mkdir -p logs state since archive
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  rm -f "$BASE/state/chunk"
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] log-archive (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT

until=$(date -u +%Y-%m-%dT%H:%M:%SZ)
day=${until%%T*}
chunk="$BASE/state/chunk"
done_n=0
skipped=0
names=$(docker ps --format '{{.Names}}')
for name in $names; do
  started=$(docker inspect -f '{{.State.StartedAt}}' "$name" 2>/dev/null | cut -c1-19) || started=""
  since=${started:+${started}Z}
  if [ -s "since/$name" ]; then
    mark=$(cat "since/$name")
    if [ -z "$since" ] || [ "$mark" \> "$since" ]; then since=$mark; fi
  fi
  if ! docker logs --timestamps ${since:+--since "$since"} --until "$until" "$name" > "$chunk" 2>&1; then
    echo "skip $name: docker logs failed"
    skipped=$((skipped + 1))
    continue
  fi
  if [ -s "$chunk" ]; then
    mkdir -p "archive/$name"
    gzip -c "$chunk" >> "archive/$name/$day.log.gz"
  fi
  echo "$until" > "since/$name"
  done_n=$((done_n + 1))
done
echo "$(date -u '+%F %T')Z archived=$done_n skipped=$skipped until=$until"

find archive -name '*.log.gz' -mtime +"$KEEP_DAYS" -delete
find archive -mindepth 1 -type d -empty -delete
find since -type f -mtime +"$KEEP_DAYS" -delete
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
