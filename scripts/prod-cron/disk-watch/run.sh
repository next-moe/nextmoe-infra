#!/bin/sh
# Root-filesystem headroom watch, every 10 minutes. Every Postgres on the box,
# OpenSearch, Docker's image store and the image-mirror staging dirs share /.
#
# 2026-10-12: a day of a dozen merges left ~35G of superseded images behind
# (each infra-tools build is one 2.4G layer, and the hourly refresh-tag-counts
# pulls every one), and nothing removed them before Dokploy's daily prune at
# 07:50 CST. At 23:40 CST OpenSearch crossed its 95% flood stage and made every
# catalog index read-only; Monday's image-mirror then staged 7.9G of DLsite
# screenshots, and at 06:27 CST the disk was full. Every Postgres on the box
# PANICked in a restart loop for 83 minutes, until that 07:50 prune happened to
# free the space. The only warning was pg-backup's 02:30 mail that it had
# skipped its dump.
#
# Below PRUNE_BELOW_G it removes dangling images: without -a, `docker image
# prune` never takes an image that a container, running or stopped, still uses.
# Still below ALERT_BELOW_G it mails, at most every RENOTIFY_HOURS, because what
# is left is not images and a human has to find it. ALERT_BELOW_G is pg-backup's
# own floor and sits just above OpenSearch's 90% high watermark (23.5G here).
#
# Canonical copy: scripts/prod-cron/disk-watch/run.sh in nextmoe-infra — the
# deployed copy is /root/disk-watch/run.sh and must stay byte-identical.
set -eu
BASE=${DISK_WATCH_BASE:-/root/disk-watch}
ALERT_SH=${DISK_WATCH_ALERT:-/root/lib/alert.sh}
PRUNE_BELOW_G=${DISK_WATCH_PRUNE_BELOW_G:-40}
ALERT_BELOW_G=${DISK_WATCH_ALERT_BELOW_G:-25}
RENOTIFY_HOURS=${DISK_WATCH_RENOTIFY_HOURS:-6}
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
    "$ALERT_SH" "[FAIL] disk-watch (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT

free_g() {
  df -BG --output=avail / | tail -1 | tr -dc 0-9
}

free=$(free_g)
: "${free:?df reported no free space figure}"
line="$(date -u '+%F %T')Z free=${free}G"
if [ "$free" -lt "$PRUNE_BELOW_G" ]; then
  reclaimed=$(docker image prune -f | sed -n 's/^Total reclaimed space: //p')
  free=$(free_g)
  : "${free:?df reported no free space figure}"
  line="$line pruned=${reclaimed:-0B} now=${free}G"
fi
echo "$line"

MARK=state/alerted
if [ "$free" -lt "$ALERT_BELOW_G" ]; then
  if [ ! -f "$MARK" ] || [ $(( ($(date +%s) - $(stat -c %Y "$MARK")) / 3600 )) -ge "$RENOTIFY_HOURS" ]; then
    REPORT=state/report.txt
    {
      echo "$line — alerting below ${ALERT_BELOW_G}G"
      echo
      df -h /
      echo
      docker system df
      echo
      timeout 300 du -xsh /root/* /var/lib/docker/volumes/* 2>/dev/null | sort -rh | head -20
    } > "$REPORT" 2>&1 || true
    "$ALERT_SH" "[DISK] ${free}G free on / (alert below ${ALERT_BELOW_G}G)" "$BASE/$REPORT" \
      || echo "alert delivery failed"
    : > "$MARK"
  fi
else
  rm -f "$MARK"
fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
