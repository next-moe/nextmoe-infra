#!/bin/sh
# Hourly: alert when a variable that had a value at an earlier start of a
# service has none at its latest start.
#
# Two variables went missing within three days, and both were found only by
# reading logs. #533 (2026-11-16) dropped the KUN_PG_* literals the host crons
# read. The same day's Dokploy panel cut lost the agent's KUN_AGENT_CLIENT_ID,
# and moemoepoint charging stayed off until 2026-11-18. Every Go service records
# what it resolved at start in kun_galgame_infra.config_boot_snapshots, but only
# one row per service, so each start overwrites the one before. This job keeps
# that memory: state/seen-set holds every service|key that has had a value, and
# a pair that is unset now is reported until it has a value again.
#
# To drop a variable on purpose, delete its line from state/seen-set.
#
# Like ymgal-pending-watch it runs no Go tool. The whole check is one query
# through docker exec into the postgres container, with no DSN or password on
# any command line.
#
# Canonical copy: scripts/prod-cron/config-watch/run.sh in nextmoe-infra — the
# deployed copy is /root/config-watch/run.sh and must stay byte-identical.
set -eu
BASE=${CONFIG_WATCH_BASE:-/root/config-watch}
ALERT_SH=${CONFIG_WATCH_ALERT:-/root/lib/alert.sh}
PG=${CONFIG_WATCH_PG:-kun-visual-novel-infra-vqvqbc-postgres-1}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  rm -f "$BASE/state/now" "$BASE/state/next"
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] config watch (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== config watch start $(date -u '+%F %T')Z ==="

NOW=state/now
docker exec "$PG" psql -U postgres -d kun_galgame_infra -XAt -F'|' -v ON_ERROR_STOP=1 -c \
  "SELECT s.service, e->>'key', CASE WHEN (e->>'set')::boolean THEN 'set' ELSE 'unset' END
     FROM config_boot_snapshots s, jsonb_array_elements(s.entries) e ORDER BY 1, 2" > "$NOW"
[ -s "$NOW" ] || { echo "config_boot_snapshots is empty"; exit 1; }

SEEN=state/seen-set
touch "$SEEN"
LOST=state/lost-$(date -u +%F).txt
# FILENAME, not NR==FNR: with an empty seen-set the first run read every
# snapshot line as already seen and remembered nothing.
awk -F'|' 'FILENAME == ARGV[1] { seen[$0] = 1; next } ($1 "|" $2) in seen && $3 == "unset" { print $1 "|" $2 }' "$SEEN" "$NOW" > "$LOST"
awk -F'|' 'FILENAME == ARGV[1] { seen[$0] = 1; next } { k = $1 "|" $2 } (k in seen) || $3 == "set" { print k }' "$SEEN" "$NOW" \
  | sort -u > state/next
mv state/next "$SEEN"

N=$(wc -l < "$LOST" | tr -d ' ')
echo "services=$(cut -d'|' -f1 "$NOW" | sort -u | wc -l | tr -d ' ') remembered=$(wc -l < "$SEEN" | tr -d ' ') lost=$N"
if [ "$N" -gt 0 ]; then
  {
    echo "These variables had a value at an earlier start of the service and have none at its latest start."
    echo "Restore each in admin /settings/env (scope = the service) and restart that service."
    echo "If one was dropped on purpose, delete its line from $BASE/$SEEN."
    echo
    cat "$LOST"
  } > state/report
  ALERT_DIGEST=$(sha256sum < "$LOST" | cut -c1-16) "$ALERT_SH" "[CONFIG] $N variable(s) lost their value" "$BASE/state/report" \
    || echo "alert delivery failed"
fi

find state -name 'lost-*.txt' -mtime +30 -delete
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== config watch done $(date -u '+%F %T')Z ==="
