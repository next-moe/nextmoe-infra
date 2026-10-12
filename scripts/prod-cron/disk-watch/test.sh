#!/bin/sh
# Offline harness for run.sh beside it: fake df / docker / flock / alert on PATH,
# so it never reaches Docker or reads this machine's disk.
#
#   sh scripts/prod-cron/disk-watch/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

# ctl/free is what df reports; a prune moves ctl/free-after-prune into it.
install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/base" "$td/ctl"
  cat > "$td/bin/df" <<'FAKE'
#!/bin/sh
CTL=${FAKE_CTL:?}
echo "Avail"
[ -s "$CTL/free" ] && echo " $(cat "$CTL/free")G"
exit 0
FAKE
  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
echo "$*" >> "$CTL/docker.log"
case "$*" in
  "image prune -f")
    [ -f "$CTL/free-after-prune" ] && cp "$CTL/free-after-prune" "$CTL/free"
    echo "Deleted Images:"
    echo "deleted: sha256:feed"
    echo
    echo "Total reclaimed space: 2.4GB" ;;
  "system df") echo "TYPE TOTAL ACTIVE SIZE RECLAIMABLE" ;;
  *) echo "fake docker: unexpected $*" >&2; exit 99 ;;
esac
FAKE
  cat > "$td/bin/flock" <<'FAKE'
#!/bin/sh
[ "${FLOCK_HOLD_FAIL:-0}" = 1 ] && exit 1
exit 0
FAKE
  cat > "$td/bin/du" <<'FAKE'
#!/bin/sh
echo "7.9G	/root/image-mirror"
FAKE
  cat > "$td/alert.sh" <<'FAKE'
#!/bin/sh
echo "$1" >> "${FAKE_CTL:?}/alerts"
cp "$2" "$FAKE_CTL/alert-body"
FAKE
  chmod +x "$td/bin/"* "$td/alert.sh"
}

run_job() {
  td=$1
  set +e
  env PATH="$td/bin:$PATH" FAKE_CTL="$td/ctl" \
    DISK_WATCH_BASE="$td/base" DISK_WATCH_ALERT="$td/alert.sh" \
    sh "$RUN_SH" > "$td/ctl/stdout" 2>&1
  echo $? > "$td/ctl/exit"
  set -e
}

exit_is() { [ "$(cat "$1/ctl/exit")" = "$2" ]; }
has_stamp() { [ -f "$1/base/state/last-success" ]; }
alerts() { n=$(grep -c "${2:-.}" "$1/ctl/alerts" 2>/dev/null || true); echo "${n:-0}"; }
prunes() { n=$(grep -c '^image prune -f$' "$1/ctl/docker.log" 2>/dev/null || true); echo "${n:-0}"; }
log_has() { cat "$1"/base/logs/run-*.log | grep -q "$2"; }

# --- T1: plenty of room prunes nothing, mails nothing, and stamps ---
tstart 1
td=$(mktemp -d); install_fakes "$td"; echo 100 > "$td/ctl/free"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
has_stamp "$td" || fail "no stamp"
[ "$(prunes "$td")" = 0 ] || fail "pruned"
[ "$(alerts "$td")" = 0 ] || fail "alerted"
log_has "$td" 'free=100G$' || fail "free space not logged"
tend; rm -rf "$td"

# --- T2: under the prune line but over the alert line prunes once and stays quiet ---
tstart 2
td=$(mktemp -d); install_fakes "$td"; echo 35 > "$td/ctl/free"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ "$(prunes "$td")" = 1 ] || fail "prunes=$(prunes "$td")"
[ "$(alerts "$td")" = 0 ] || fail "alerted"
log_has "$td" 'free=35G pruned=2.4GB now=35G' || fail "prune not logged"
tend; rm -rf "$td"

# --- T3: still short after the prune mails once with the report, and the watch itself succeeded ---
tstart 3
td=$(mktemp -d); install_fakes "$td"; echo 20 > "$td/ctl/free"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
has_stamp "$td" || fail "no stamp"
[ "$(prunes "$td")" = 1 ] || fail "prunes=$(prunes "$td")"
[ "$(alerts "$td" '^\[DISK\] 20G free')" = 1 ] || fail "alerts: $(cat "$td/ctl/alerts" 2>/dev/null)"
grep -q '/root/image-mirror' "$td/ctl/alert-body" || fail "report has no du"
[ -f "$td/base/state/alerted" ] || fail "no alert mark"
tend

# --- T4: the same shortage ten minutes later does not mail again ---
tstart 4
run_job "$td"
[ "$(alerts "$td")" = 1 ] || fail "re-alerted inside the window"
tend

# --- T5: once the window has passed it mails again ---
tstart 5
touch -d '7 hours ago' "$td/base/state/alerted"
run_job "$td"
[ "$(alerts "$td")" = 2 ] || fail "alerts=$(alerts "$td")"
tend

# --- T6: a prune that clears the shortage clears the mark, so the next shortage mails at once ---
tstart 6
echo 60 > "$td/ctl/free-after-prune"
run_job "$td"
[ "$(alerts "$td")" = 2 ] || fail "alerted after the prune fixed it"
[ -f "$td/base/state/alerted" ] && fail "mark kept"
tend; rm -rf "$td"

# --- T7: an unreadable df is a failure, not a pass ---
tstart 7
td=$(mktemp -d); install_fakes "$td"; : > "$td/ctl/free"; run_job "$td"
exit_is "$td" 0 && fail "exit 0"
has_stamp "$td" && fail "stamp written"
[ "$(alerts "$td" '^\[FAIL\] disk-watch')" = 1 ] || fail "no failure alert"
[ "$(prunes "$td")" = 0 ] || fail "pruned blind"
tend; rm -rf "$td"

# --- T8: a run that finds the lock held does nothing ---
tstart 8
td=$(mktemp -d); install_fakes "$td"; echo 20 > "$td/ctl/free"
FLOCK_HOLD_FAIL=1 run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
has_stamp "$td" && fail "stamp written"
[ -f "$td/ctl/docker.log" ] && fail "docker ran"
[ "$(alerts "$td")" = 0 ] || fail "alerted"
tend; rm -rf "$td"

[ "$FAILED" -eq 0 ]
