#!/bin/sh
# Offline harness for run.sh beside it: fake docker / age / flock / alert on
# PATH, so it never reaches Docker, R2 or a real key. Run it on a dev box before
# redeploying run.sh — this directory has no CI.
#
#   sh scripts/prod-cron/pg-offsite/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0
SECRET=s3cr3t-value-that-must-not-leak
RECIPIENT_KEY=age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  rm -rf "$TD"
  if [ "$T_OK" = 1 ]; then
    echo "T$T_ID PASS"
  else
    echo "T$T_ID FAIL:$T_MSG"
    FAILED=1
  fi
}

setup() {
  TD=$(mktemp -d)
  mkdir -p "$TD/bin" "$TD/base" "$TD/ctl/rc" "$TD/dumps/2026-09-23" "$TD/dumps/2026-09-24" "$TD/dumps/weekly"
  : > "$TD/ctl/docker.log"
  : > "$TD/ctl/age.log"
  : > "$TD/ctl/alerts"
  echo "old" > "$TD/dumps/2026-09-23/kun_catalog.dump"
  echo "catalog" > "$TD/dumps/2026-09-24/kun_catalog.dump"
  echo "globals" > "$TD/dumps/2026-09-24/globals.sql"
  echo "eg" > "$TD/dumps/weekly/erogamescape.dump"
  printf 'R2_ACCESS_KEY_ID=AKID\nR2_SECRET_ACCESS_KEY=%s\nR2_ENDPOINT=https://acct.r2.cloudflarestorage.com\nR2_BUCKET=nextmoe-pg-backup\n' \
    "$SECRET" > "$TD/r2.env"
  echo "$RECIPIENT_KEY" > "$TD/recipient"

  cat > "$TD/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
echo "$*" >> "$CTL/docker.log"
[ "$1" = run ] || { echo "fake docker: unexpected argv: $*" >&2; exit 99; }
[ "${RCLONE_CONFIG_R2_SECRET_ACCESS_KEY:-}" = "${FAKE_SECRET:?}" ] || { echo "credentials not passed by name" >&2; exit 98; }
while [ $# -gt 0 ]; do
  case "$1" in
    copy|check|copyto) op=$1; break ;;
  esac
  shift
done
[ -f "$CTL/rc/$op" ] && exit "$(cat "$CTL/rc/$op")"
exit 0
FAKE

  cat > "$TD/bin/age" <<'FAKE'
#!/bin/sh
set -eu
echo "$*" >> "$FAKE_CTL/age.log"
[ -f "$FAKE_CTL/rc/age" ] && exit "$(cat "$FAKE_CTL/rc/age")"
[ "$1" = -r ] && [ "$3" = -o ] || exit 97
printf 'enc(%s)' "$(cat "$5")" > "$4"
FAKE

  cat > "$TD/bin/flock" <<'FAKE'
#!/bin/sh
[ "${FAKE_FLOCK_HELD:-0}" = 1 ] && exit 1
exit 0
FAKE

  cat > "$TD/alert.sh" <<'FAKE'
#!/bin/sh
echo "$1" >> "$FAKE_CTL/alerts"
FAKE
  chmod +x "$TD/bin/docker" "$TD/bin/age" "$TD/bin/flock" "$TD/alert.sh"
}

run_job() {
  JOB_EC=0
  FAKE_CTL="$TD/ctl" FAKE_SECRET="$SECRET" PATH="$TD/bin:$PATH" FAKE_FLOCK_HELD="$FLOCK_HELD" \
    PG_OFFSITE_BASE="$TD/base" PG_OFFSITE_DUMPS="$TD/dumps" PG_OFFSITE_R2_ENV="$TD/r2.env" \
    PG_OFFSITE_RECIPIENT="$TD/recipient" PG_OFFSITE_AGE="$TD/bin/age" PG_OFFSITE_ALERT="$TD/alert.sh" \
    sh "$RUN_SH" || JOB_EC=$?
  LOGF="$TD/base/logs/run-$(date -u +%F).log"
}

FLOCK_HELD=0

tstart 1
setup
run_job
[ "$JOB_EC" = 0 ] || fail "exit $JOB_EC: $(tail -3 "$LOGF")"
[ -s "$TD/base/state/last-success" ] || fail "no success stamp"
[ ! -s "$TD/ctl/alerts" ] || fail "alerted"
[ "$(cat "$TD/base/state/daily" 2>/dev/null)" = 2026-09-24 ] || fail "did not ship the newest day"
grep -q "2026-09-23" "$TD/ctl/age.log" && fail "encrypted an older day"
[ "$(grep -c -- "-r $RECIPIENT_KEY" "$TD/ctl/age.log")" = 3 ] || fail "want 3 files encrypted to the recipient, got: $(cat "$TD/ctl/age.log")"
grep -q 'copy /stage r2:nextmoe-pg-backup/daily/' "$TD/ctl/docker.log" || fail "no daily copy"
grep -q 'check /stage r2:nextmoe-pg-backup/daily/' "$TD/ctl/docker.log" || fail "daily upload not checked"
grep -q 'copyto /stage/SHA256SUMS r2:nextmoe-pg-backup/daily/.*/SHA256SUMS' "$TD/ctl/docker.log" || fail "no SHA256SUMS"
grep -q 'copy /stage r2:nextmoe-pg-backup/weekly/' "$TD/ctl/docker.log" || fail "no weekly copy"
grep -q "$SECRET" "$TD/ctl/docker.log" && fail "the R2 secret reached an argv"
[ ! -e "$TD/base/stage" ] || fail "stage left behind"
tend

tstart 2
setup
run_job
: > "$TD/ctl/docker.log"
: > "$TD/ctl/age.log"
run_job
[ "$JOB_EC" = 0 ] || fail "rerun exit $JOB_EC"
[ ! -s "$TD/ctl/docker.log" ] || fail "reshipped an unchanged day: $(cat "$TD/ctl/docker.log")"
grep -q 'already shipped' "$LOGF" || fail "log does not say so"
echo "eg v2" > "$TD/dumps/weekly/erogamescape.dump"
touch -d '+1 minute' "$TD/dumps/weekly/erogamescape.dump"
run_job
grep -q 'copy /stage r2:nextmoe-pg-backup/weekly/' "$TD/ctl/docker.log" || fail "a changed weekly set was not shipped"
grep -q 'daily/' "$TD/ctl/docker.log" && fail "reshipped the daily set with the weekly one"
tend

tstart 3
setup
echo "ssh-ed25519 AAAA not-an-age-key" > "$TD/recipient"
run_job
[ "$JOB_EC" = 2 ] || fail "exit $JOB_EC, want 2"
[ ! -s "$TD/ctl/docker.log" ] || fail "uploaded without an age recipient"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 4
setup
echo 1 > "$TD/ctl/rc/check"
run_job
[ "$JOB_EC" != 0 ] || fail "a failed check exited 0"
grep -q 'copyto' "$TD/ctl/docker.log" && fail "marked an unverified upload complete"
[ ! -e "$TD/base/state/daily" ] || fail "recorded an unverified day as shipped"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
[ ! -e "$TD/base/stage" ] || fail "stage left behind"
rm "$TD/ctl/rc/check"
run_job
[ "$JOB_EC" = 0 ] || fail "retry exit $JOB_EC"
[ "$(cat "$TD/base/state/daily" 2>/dev/null)" = 2026-09-24 ] || fail "retry did not ship"
tend

tstart 5
setup
echo 1 > "$TD/ctl/rc/age"
run_job
[ "$JOB_EC" != 0 ] || fail "a failed encryption exited 0"
[ ! -s "$TD/ctl/docker.log" ] || fail "uploaded after a failed encryption"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 6
setup
mkdir -p "$TD/dumps/2026-09-25.partial"
echo "half" > "$TD/dumps/2026-09-25.partial/kun_catalog.dump"
run_job
[ "$(cat "$TD/base/state/daily" 2>/dev/null)" = 2026-09-24 ] || fail "picked a partial day"
grep -q half "$TD/ctl/age.log" "$TD/ctl/docker.log" && fail "touched a partial dump"
tend

tstart 7
setup
FLOCK_HELD=1
run_job
FLOCK_HELD=0
[ "$JOB_EC" = 0 ] || fail "exit $JOB_EC"
[ ! -s "$TD/ctl/docker.log" ] || fail "ran while another run held the lock"
[ ! -e "$TD/base/state/last-success" ] || fail "stamped a skipped run"
tend

tstart 8
setup
echo 1 > "$TD/ctl/rc/copy"
run_job
[ "$JOB_EC" != 0 ] || fail "a failed upload exited 0"
grep -q 'check\|copyto' "$TD/ctl/docker.log" && fail "went on after a failed upload"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

if [ "$FAILED" = 0 ]; then echo "ALL PASS"; else echo "SOME FAILED"; exit 1; fi
