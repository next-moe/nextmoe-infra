#!/bin/sh
# Offline harness for watchdog.sh beside it: fake curl and alert, temp state.
#
#   sh scripts/prod-cron/lib/watchdog-test.sh
set -eu

WATCHDOG_SH=${WATCHDOG_SH:-"$(cd "$(dirname "$0")" && pwd)/watchdog.sh"}
FAILED=0
tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

setup() {
  td=$(mktemp -d)
  mkdir -p "$td/bin" "$td/state"
  : > "$td/fresh"
  : > "$td/old"
  touch -d "100 hours ago" "$td/old"
  printf 'fresh %s 48\n' "$td/fresh" > "$td/conf"
  cat > "$td/bin/curl" <<'FAKE'
#!/bin/sh
url=""; data=""; prev=""
for a in "$@"; do
  [ "$prev" = --data-binary ] && data=${a#@}
  case "$a" in http*) url=$a ;; esac
  prev=$a
done
echo "$url" >> "$CTL/pings"
[ -n "$data" ] && cat "$data" >> "$CTL/ping-body"
exit 0
FAKE
  printf '#!/bin/sh\ncat > /dev/null\necho "$1" >> "%s/alerts"\n' "$td" > "$td/alert"
  chmod +x "$td/bin/curl" "$td/alert"
}

watch() {
  PATH="$td/bin:$PATH" CTL="$td" WATCHDOG_CONF="$td/conf" WATCHDOG_STATE="$td/state" \
    WATCHDOG_ALERT="$td/alert" WATCHDOG_DEADMAN_ENV="$td/deadman.env" sh "$WATCHDOG_SH"
}

tstart 1
setup
printf 'old %s 48\n' "$td/old" >> "$td/conf"
watch
grep -q STALE "$td/alerts" || fail "an overdue job was not mailed"
[ ! -f "$td/pings" ] || fail "pinged without a deadman configured"
tend

tstart 2
setup
echo "DEADMAN_URL=https://hc.example/abc" > "$td/deadman.env"
watch
[ "$(cat "$td/pings")" = "https://hc.example/abc" ] || fail "no success ping: $(cat "$td/pings" 2>/dev/null)"
[ ! -f "$td/alerts" ] || fail "mailed with nothing overdue"
tend

tstart 3
setup
echo "DEADMAN_URL=https://hc.example/abc" > "$td/deadman.env"
printf 'old %s 48\n' "$td/old" >> "$td/conf"
: > "$td/state/alerted-old"
watch
[ ! -f "$td/alerts" ] || fail "re-mailed within the renotify window"
[ "$(cat "$td/pings")" = "https://hc.example/abc/fail" ] || fail "an outage already mailed pinged success"
grep -q '^old:' "$td/ping-body" || fail "fail ping does not name the job"
tend

tstart 4
setup
echo "DEADMAN_URL=https://hc.example/abc" > "$td/deadman.env"
printf '2026-11-16 08:30:12Z\t[FAIL] text safety\tops-alert failed\n' > "$td/state/undelivered"
watch
[ "$(cat "$td/pings")" = "https://hc.example/abc/fail" ] || fail "undelivered alerts did not fail the check"
grep -q 'text safety' "$td/ping-body" || fail "fail ping does not carry the undelivered alert"
[ ! -s "$td/state/undelivered" ] || fail "reported alerts not cleared"
grep -q 'text safety' "$td/state/undelivered.reported" || fail "reported alerts not kept"
tend

tstart 5
setup
printf 'old %s 48\n' "$td/old" >> "$td/conf"
printf '#!/bin/sh\ncat > /dev/null\nexit 1\n' > "$td/alert"
watch 2>/dev/null || true
[ ! -f "$td/state/alerted-old" ] || fail "marked an overdue job whose alert was never sent"
printf '#!/bin/sh\ncat > /dev/null\necho "$1" >> "%s/alerts"\n' "$td" > "$td/alert"
watch
grep -q STALE "$td/alerts" || fail "the next run stayed silent after a failed send"
[ -f "$td/state/alerted-old" ] || fail "not marked after a delivered alert"
tend

exit "$FAILED"
