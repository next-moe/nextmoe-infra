#!/bin/sh
# Two-month retention for the kungal umami, set by the owner on 2026-10-12.
# Umami keeps every event forever: website_event had reached 41G (9.7G of rows,
# 31G of indexes) and grew ~2.7G a week — the steady drain on the root disk
# that filled that morning (see disk-watch).
#
# Events go one day per transaction, oldest first, so a run that finds months of
# backlog never holds one 20M-row transaction. A session goes once none of its
# events is left. Umami declares no foreign keys; event_data, session_data and
# revenue go by their own created_at.
#
# The freed space is reused by new rows once autovacuum has passed; the files do
# not shrink. Returning the first backlog's space took a REINDEX CONCURRENTLY.
#
# Canonical copy: scripts/prod-cron/umami-retention/run.sh in nextmoe-infra —
# the deployed copy is /root/umami-retention/run.sh and must stay byte-identical.
set -eu
BASE=${UMAMI_RETENTION_BASE:-/root/umami-retention}
ALERT_SH=${UMAMI_RETENTION_ALERT:-/root/lib/alert.sh}
PG=${UMAMI_RETENTION_PG:-kun-visual-novel-umami-fm1njw-db-1}
KEEP=${UMAMI_RETENTION_KEEP:-2 months}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] umami-retention (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== umami retention start $(date -u '+%F %T')Z, keeping $KEEP ==="

docker exec -i "$PG" psql -U umami -d umami -X -q -v ON_ERROR_STOP=1 -v keep="$KEEP" <<'SQL'
SET statement_timeout = 0;
SELECT set_config('retention.keep', :'keep', false) \g /dev/null
DO $$
DECLARE
  cutoff timestamptz := now() - current_setting('retention.keep')::interval;
  lo timestamptz;
  hi timestamptz;
  n bigint;
  events bigint := 0;
BEGIN
  SELECT min(created_at) INTO lo FROM website_event;
  WHILE lo < cutoff LOOP
    hi := least(lo + interval '1 day', cutoff);
    DELETE FROM website_event WHERE created_at >= lo AND created_at < hi;
    GET DIAGNOSTICS n = ROW_COUNT;
    events := events + n;
    COMMIT;
    lo := hi;
  END LOOP;
  RAISE NOTICE 'cutoff % website_event %', cutoff, events;

  DELETE FROM event_data WHERE created_at < cutoff;
  GET DIAGNOSTICS n = ROW_COUNT;
  RAISE NOTICE 'event_data %', n;
  DELETE FROM revenue WHERE created_at < cutoff;
  GET DIAGNOSTICS n = ROW_COUNT;
  RAISE NOTICE 'revenue %', n;
  DELETE FROM session_data WHERE created_at < cutoff;
  GET DIAGNOSTICS n = ROW_COUNT;
  RAISE NOTICE 'session_data %', n;
  DELETE FROM session s
   WHERE s.created_at < cutoff
     AND NOT EXISTS (SELECT 1 FROM website_event e WHERE e.session_id = s.session_id);
  GET DIAGNOSTICS n = ROW_COUNT;
  RAISE NOTICE 'session %', n;
END $$;
SQL

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== umami retention done $(date -u '+%F %T')Z ==="
