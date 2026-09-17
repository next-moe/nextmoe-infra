#!/bin/sh
# Deadman check for the root-owned maintenance crons.
#
# WHY this exists and a failure trap is not enough: the failure mode that
# actually costs data is the job NOT RUNNING — cron disabled, host rebooted
# into a broken state, a stale lock file, the script deleted. All of those
# produce perfect silence, which is indistinguishable from success if the
# only alerting is an error handler inside the script. This asks the opposite
# question: when did each job last SUCCEED, and is that too long ago?
#
# It matters most for vndb-refresh: VNDB keeps only ~2 days of dumps, so a
# refresh that quietly stops is a window of upstream history that cannot be
# recovered later.
#
# Registry lives in /root/lib/watchdog.conf: <name> <state-file> <max-age-hours>
set -eu

CONF=/root/lib/watchdog.conf
STATE=/root/lib/state
ALERT=/root/lib/alert.sh
# Do not re-send every single day for the same ongoing outage; one reminder
# every three days is enough to stay visible without becoming filtered noise.
RENOTIFY_HOURS=72

mkdir -p "$STATE"
NOW=$(date +%s)
REPORT=$(mktemp); trap 'rm -f "$REPORT"' EXIT
STALE=0

while read -r NAME FILE MAXH; do
  case "$NAME" in ''|\#*) continue ;; esac

  if [ -f "$FILE" ]; then
    AGE=$(( (NOW - $(stat -c %Y "$FILE")) / 3600 ))
  else
    # No stamp at all. Real states: the job has never succeeded since the
    # watchdog was installed, or someone wiped its state directory. Both
    # deserve the same answer as "too old".
    AGE=-1
  fi

  MARK="$STATE/alerted-$NAME"

  if [ "$AGE" -ge 0 ] && [ "$AGE" -le "$MAXH" ]; then
    rm -f "$MARK"
    continue
  fi

  # Overdue. Stay quiet if this same outage was already reported recently.
  if [ -f "$MARK" ] && [ $(( (NOW - $(stat -c %Y "$MARK")) / 3600 )) -lt "$RENOTIFY_HOURS" ]; then
    continue
  fi

  if [ "$AGE" -lt 0 ]; then
    echo "$NAME: NO SUCCESS STAMP at $FILE" >> "$REPORT"
  else
    echo "$NAME: last success ${AGE}h ago (limit ${MAXH}h) — $FILE" >> "$REPORT"
  fi
  : > "$MARK"
  STALE=$((STALE + 1))
done < "$CONF"

[ "$STALE" -gt 0 ] || exit 0

{
  echo "One or more scheduled maintenance jobs have not succeeded recently."
  echo
  cat "$REPORT"
  echo
  echo "Host: $(hostname)   checked: $(date -u '+%F %T')Z"
  echo "Logs: /root/<job>/logs/"
} | "$ALERT" "[STALE] $STALE maintenance job(s) overdue" - || {
  echo "watchdog: alert send failed" >&2; exit 1; }
