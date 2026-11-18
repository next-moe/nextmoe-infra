#!/bin/sh
# Offline harness for alert.sh beside it: a fake docker on PATH records what
# would be mailed, so nothing reaches Docker, a database or an SMTP server.
#
#   sh scripts/prod-cron/lib/alert-test.sh
set -eu

ALERT_SH=${ALERT_SH:-"$(cd "$(dirname "$0")" && pwd)/alert.sh"}
FAILED=0
tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

setup() {
  td=$(mktemp -d)
  mkdir -p "$td/bin" "$td/state" "$td/root/job/logs" "$td/root/job/state"
  echo "it broke" > "$td/root/job/logs/run.log"
  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
CTL=${FAKE_DOCKER_DIR:?refusing to reach a real docker}
case "$1" in
  inspect) printf 'KUN_CONFIG_KEK=k\nKUN_PG_HOST=postgres\nKUN_VISUAL_NOVEL_EMAIL_HOST=h\nJWT_SECRET=leak\n'; exit 0 ;;
  image) echo "ghcr.io/next-moe/infra-tools@sha256:cafe"; exit 0 ;;
  run)
    envf=""; prev=""
    for a in "$@"; do [ "$prev" = --env-file ] && envf=$a; prev=$a; done
    cp "$envf" "$CTL/last.env"
    printf '%s\n' "$*" >> "$CTL/sent"
    [ -f "$CTL/fail" ] && exit 1
    exit 0 ;;
esac
exit 99
FAKE
  printf '#!/bin/sh\n[ "$1" = -u ] && shift\nrm -f "$@"\n' > "$td/bin/shred"
  chmod +x "$td/bin/"*
}

alert() {
  rc=0
  PATH="$td/bin:$PATH" FAKE_DOCKER_DIR="$td" ALERT_STATE_DIR="$td/state" ALERT_JOBS_ROOT="$td/root" \
    sh "$ALERT_SH" "$@" > "$td/out" 2>&1 || rc=$?
}
sends() { if [ -f "$td/sent" ]; then wc -l < "$td/sent" | tr -d ' '; else echo 0; fi; }
age_mark() { for m in "$td/state"/alert-*; do touch -d "$1" "$m"; done; }

tstart 1
setup
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$rc" = 0 ] && [ "$(sends)" = 1 ] || fail "first alert not sent (rc=$rc)"
! grep -q JWT_SECRET "$td/last.env" || fail "env carries more than ops-alert needs"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$rc" = 0 ] && [ "$(sends)" = 1 ] || fail "repeat was not suppressed"
grep -q "repeat 1 suppressed" "$td/out" || fail "no suppression note"
tend

tstart 2
setup
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
age_mark "2 hours ago"
touch "$td/root/job/state/last-success"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$(sends)" = 2 ] || fail "a failure after a success was suppressed"
tend

tstart 3
setup
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
age_mark "73 hours ago"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$(sends)" = 2 ] || fail "no renotify after 72h"
grep -q "1 identical alert(s) suppressed" "$td/sent" || fail "renotify does not count the suppressed repeats"
tend

tstart 4
setup
ALERT_DIGEST=a alert "[SHELF] findings" "$td/root/job/logs/run.log"
age_mark "1 hour ago"
touch "$td/root/job/state/last-success"
ALERT_DIGEST=a alert "[SHELF] findings" "$td/root/job/logs/run.log"
[ "$(sends)" = 1 ] || fail "unchanged findings resent after a successful run"
ALERT_DIGEST=b alert "[SHELF] findings" "$td/root/job/logs/run.log"
[ "$(sends)" = 2 ] || fail "changed findings not sent"
tend

tstart 5
setup
touch "$td/fail"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$rc" != 0 ] || fail "a failed send exited 0"
grep -q "\[FAIL\] job" "$td/state/undelivered" || fail "undelivered alert not queued"
rm "$td/fail"
alert "[FAIL] job (exit 1)" "$td/root/job/logs/run.log"
[ "$(sends)" = 2 ] || fail "an undelivered alert suppressed its retry"
tend

tstart 6
setup
echo "from stdin" | { PATH="$td/bin:$PATH" FAKE_DOCKER_DIR="$td" ALERT_STATE_DIR="$td/state" ALERT_JOBS_ROOT="$td/root" sh "$ALERT_SH" "[STALE] 1 job overdue" - > "$td/out" 2>&1; } || fail "stdin body"
[ "$(sends)" = 1 ] || fail "stdin alert not sent"
[ -z "$(find "$td/state" -name '.alert*')" ] || fail "temp files left behind"
tend

exit "$FAILED"
