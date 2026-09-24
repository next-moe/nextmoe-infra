#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / alert on PATH, so
# it never reaches Docker or a database. Run it on a dev box before redeploying
# run.sh — this directory has no CI.
#
#   sh scripts/prod-cron/pg-backup/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

LISTED="kungalgame kun_galgame_infra kun_catalog kun_community kun_trust kun_images kungalgame_patch kun_shortlink kun_letmoe kun_news kun_artifacts kungalgame_sticker kun_blog kun_ai"
SKIPPED="dlsite getchu howlongtobeat erogamescape kun_galgame_wiki_retired_w1 kun_letmoe_staging postgres"

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
  mkdir -p "$TD/bin" "$TD/base" "$TD/ctl/rc"
  : > "$TD/ctl/docker.log"
  : > "$TD/ctl/alerts"
  echo "$LISTED $SKIPPED" > "$TD/ctl/dbs"
  echo "dokploy-postgres.1.abc" > "$TD/ctl/ps"

  cat > "$TD/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
echo "$*" >> "$CTL/docker.log"
if [ "$1" = ps ]; then cat "$CTL/ps"; exit 0; fi
[ "$1" = exec ] || { echo "fake docker: unexpected argv: $*" >&2; exit 99; }
shift
[ "$1" = -i ] && shift
shift
case "$1" in
  psql)
    [ -f "$CTL/rc/psql" ] && exit "$(cat "$CTL/rc/psql")"
    cat "$CTL/dbs"
    ;;
  pg_dumpall) echo "-- globals" ;;
  pg_dump)
    for db; do :; done
    [ -f "$CTL/rc/$db" ] && exit "$(cat "$CTL/rc/$db")"
    echo "dump of $db"
    ;;
  pg_restore)
    grep -q '^dump of ' || exit 1
    ;;
  *) echo "fake docker exec: unexpected argv: $*" >&2; exit 99 ;;
esac
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
  chmod +x "$TD/bin/docker" "$TD/bin/flock" "$TD/alert.sh"
}

run_job() {
  JOB_EC=0
  FAKE_CTL="$TD/ctl" PATH="$TD/bin:$PATH" FAKE_FLOCK_HELD="$FLOCK_HELD" \
    PG_BACKUP_BASE="$TD/base" PG_BACKUP_ALERT="$TD/alert.sh" \
    PG_BACKUP_MIN_FREE_GB="$MIN_FREE" PG_BACKUP_WEEKDAY="$WEEKDAY" sh "$RUN_SH" || JOB_EC=$?
  LOGF="$TD/base/logs/run-$(date -u +%F).log"
}

TODAY=$(date +%F)
MIN_FREE=0
FLOCK_HELD=0
WEEKDAY=3

tstart 1
setup
run_job
[ "$JOB_EC" = 0 ] || fail "exit $JOB_EC"
[ -s "$TD/base/state/last-success" ] || fail "no success stamp"
[ ! -s "$TD/ctl/alerts" ] || fail "alerted: $(cat "$TD/ctl/alerts")"
for db in $LISTED; do
  [ "$(cat "$TD/base/dumps/$TODAY/$db.dump" 2>/dev/null)" = "dump of $db" ] || fail "missing $db.dump"
done
[ -s "$TD/base/dumps/$TODAY/globals.sql" ] || fail "no globals.sql"
[ "$(cat "$TD/base/dumps/$TODAY/dokploy.dump" 2>/dev/null)" = "dump of dokploy" ] || fail "missing the panel's dokploy.dump"
[ "$(find "$TD/base/dumps/$TODAY" -name '*.dump' | wc -l)" = 15 ] || fail "dumped a skipped database"
for db in erogamescape umami; do
  [ "$(cat "$TD/base/dumps/weekly/$db.dump" 2>/dev/null)" = "dump of $db" ] || fail "first run made no weekly $db.dump"
done
[ -z "$(find "$TD/base/dumps/weekly" -name '*.partial')" ] || fail "weekly partial left behind"
[ ! -e "$TD/base/dumps/$TODAY.partial" ] || fail "partial left behind"
tend

tstart 2
setup
echo "$LISTED $SKIPPED kun_shop" > "$TD/ctl/dbs"
run_job
[ "$JOB_EC" = 3 ] || fail "exit $JOB_EC, want 3"
[ -f "$TD/base/dumps/$TODAY/kun_catalog.dump" ] || fail "listed databases not dumped"
[ ! -f "$TD/base/dumps/$TODAY/kun_shop.dump" ] || fail "dumped the unlisted database"
grep -q 'neither DBS nor SKIP.* kun_shop' "$LOGF" || fail "log does not name kun_shop"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
[ ! -e "$TD/base/state/last-success" ] || fail "stamped"
tend

tstart 3
setup
echo "$LISTED $SKIPPED" | sed 's/kun_blog //' > "$TD/ctl/dbs"
run_job
[ "$JOB_EC" = 3 ] || fail "exit $JOB_EC, want 3"
[ -f "$TD/base/dumps/$TODAY/kun_ai.dump" ] || fail "databases after the missing one not dumped"
grep -q 'pg_dump.* kun_blog' "$TD/ctl/docker.log" && fail "dumped a database the cluster lacks"
grep -q 'not in the cluster: kun_blog' "$LOGF" || fail "log does not name kun_blog"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 4
setup
mkdir -p "$TD/base/dumps/2026-01-01.partial"
echo 1 > "$TD/ctl/rc/kun_trust"
run_job
[ "$JOB_EC" != 0 ] || fail "a failed pg_dump exited 0"
[ ! -e "$TD/base/dumps/$TODAY" ] || fail "a failed run published its dumps"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
[ ! -e "$TD/base/state/last-success" ] || fail "stamped"
rm "$TD/ctl/rc/kun_trust"
run_job
[ "$JOB_EC" = 0 ] || fail "rerun exit $JOB_EC"
[ -z "$(find "$TD/base/dumps" -name '*.partial')" ] || fail "stale partials kept: $(find "$TD/base/dumps" -name '*.partial')"
tend

tstart 5
setup
age=1
while [ "$age" -le 40 ]; do
  mkdir -p "$TD/base/dumps/$(date -d "-$age days" +%F)"
  age=$((age + 1))
done
run_job
[ "$JOB_EC" = 0 ] || fail "exit $JOB_EC"
age=1
while [ "$age" -le 40 ]; do
  d=$(date -d "-$age days" +%F)
  if [ "$age" -le 7 ] || { [ "$age" -le 28 ] && [ "$(date -d "$d" +%u)" = 7 ]; }; then
    [ -d "$TD/base/dumps/$d" ] || fail "pruned $d (age $age)"
  else
    [ ! -d "$TD/base/dumps/$d" ] || fail "kept $d (age $age)"
  fi
  age=$((age + 1))
done
[ -d "$TD/base/dumps/$TODAY" ] || fail "pruned today"
tend

tstart 6
setup
MIN_FREE=999999
run_job
MIN_FREE=0
[ "$JOB_EC" = 2 ] || fail "exit $JOB_EC, want 2"
[ ! -s "$TD/ctl/docker.log" ] || fail "reached docker with the disk full"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 7
setup
FLOCK_HELD=1
run_job
FLOCK_HELD=0
[ "$JOB_EC" = 0 ] || fail "exit $JOB_EC"
[ ! -s "$TD/ctl/docker.log" ] || fail "ran while another run held the lock"
[ ! -e "$TD/base/state/last-success" ] || fail "stamped a skipped run"
[ ! -s "$TD/ctl/alerts" ] || fail "alerted"
tend

tstart 8
setup
echo 2 > "$TD/ctl/rc/psql"
run_job
[ "$JOB_EC" != 0 ] || fail "an unreadable database list exited 0"
grep -q 'pg_dump' "$TD/ctl/docker.log" && fail "dumped without a database list"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 9
setup
: > "$TD/ctl/ps"
run_job
[ "$JOB_EC" = 3 ] || fail "exit $JOB_EC, want 3"
[ -f "$TD/base/dumps/$TODAY/kun_catalog.dump" ] || fail "the other databases were not dumped"
grep -q 'panel database was not dumped' "$LOGF" || fail "log does not name the missing panel"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

tstart 10
setup
run_job
[ "$JOB_EC" = 0 ] || fail "first run exit $JOB_EC"
: > "$TD/ctl/docker.log"
run_job
[ "$JOB_EC" = 0 ] || fail "weekday rerun exit $JOB_EC"
grep -q 'erogamescape\|umami' "$TD/ctl/docker.log" && fail "a weekday run redid the weekly dumps"
WEEKDAY=7
: > "$TD/ctl/docker.log"
run_job
WEEKDAY=3
[ "$JOB_EC" = 0 ] || fail "sunday exit $JOB_EC"
grep -q 'pg_dump -U umami -Fc -d umami' "$TD/ctl/docker.log" || fail "a Sunday run skipped the weekly dumps"
tend

tstart 11
setup
echo 1 > "$TD/ctl/rc/umami"
run_job
[ "$JOB_EC" != 0 ] || fail "a failed weekly dump exited 0"
[ -d "$TD/base/dumps/$TODAY" ] || fail "a weekly failure unpublished the daily set"
[ ! -f "$TD/base/dumps/weekly/umami.dump" ] || fail "published a failed weekly dump"
grep -q 'FAIL' "$TD/ctl/alerts" || fail "no alert"
tend

if [ "$FAILED" = 0 ]; then echo "ALL PASS"; else echo "SOME FAILED"; exit 1; fi
