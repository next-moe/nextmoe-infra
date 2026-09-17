#!/bin/sh
# Offline harness for run.sh beside it: fake docker / rsync / timeout / flock /
# shred / alert on PATH, so it never reaches Docker, VNDB or a database.
#
#   sh scripts/prod-cron/image-mirror/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/base" "$td/vndb" "$td/ctl/out" "$td/ctl/rc" "$td/ctl/files"
  : > "$td/vndb/.lock"
  printf 'cv/50/150.jpg\n' > "$td/ctl/files/covers"
  printf 'ch/50/250.jpg\nch/12/12.jpg\n' > "$td/ctl/files/portraits"

  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
{ echo "###"; printf '%s\n' "$@"; echo "###end"; } >> "$CTL/docker.args"
case "$1" in
  pull) exit 0 ;;
  image) echo "ghcr.io/next-moe/infra-tools@sha256:cafe"; exit 0 ;;
  inspect)
    printf '%s\n' 'KUN_CATALOG_PG_USER=cron' 'KUN_CATALOG_PG_PASSWORD=not-on-argv' 'KUN_PG_PASSWORD=not-on-argv'
    exit 0 ;;
  run) shift ;;
  *) echo "fake docker: unexpected $*" >&2; exit 99 ;;
esac
host_w=""
while [ $# -gt 0 ]; do
  case "$1" in
    --rm) shift ;;
    -v) case "$2" in *:/w) host_w=${2%:/w} ;; esac; shift 2 ;;
    --network|--env-file|-e|--user|--name) shift 2 ;;
    -*) echo "fake docker run: unknown option $1" >&2; exit 99 ;;
    *) break ;;
  esac
done
shift
[ "$1" = sh ] && [ "$2" = -c ] || { echo "fake docker run: expected sh -c" >&2; exit 99; }
script=$3
case "$script" in
  *backfill-vndb-covers*) tool=covers ;;
  *backfill-character-portraits*) tool=portraits ;;
  *) echo "fake docker run: unknown tool: $script" >&2; exit 99 ;;
esac
cmd=$(printf '%s\n' "$script" | sed 's/.*\(backfill-[a-z-]*.*\)/\1/')
mode=dry
case "$cmd" in *--apply*) mode=apply ;; esac
echo "$tool+$mode" >> "$CTL/tools.log"
if [ "$mode" = apply ]; then
  echo "$cmd" | sed 's/--dsn "\$CAT" //' >> "$CTL/apply.log"
  ls "$host_w/mirror/vndb" >/dev/null 2>&1 || echo "$tool applied without a mirror" >> "$CTL/violations"
else
  out=$(printf '%s\n' "$cmd" | sed -n 's/.*--files-out \/w\/\([^ ]*\).*/\1/p')
  [ -n "$out" ] || { echo "$tool dry run without --files-out" >> "$CTL/violations"; }
  [ -n "$out" ] && cp "$CTL/files/$tool" "$host_w/$out"
fi
key="$tool+$mode"
if [ -f "$CTL/out/$key" ]; then
  cat "$CTL/out/$key"
else
  case "$key" in
    covers+dry) echo 'INFO backfill-vndb-covers summary result="candidates=1168 no_image=1006 portrait=150 landscape=12 planned=162 uploaded=0 dedup=0 rejected=0 errors=0 local=0 quota=false unrated=0 missing=0 to_fetch=1"' ;;
    portraits+dry) echo 'INFO char-portraits DRY forecast candidates=154912 skipped_has_hash=147947 local_present=0 missing_file=2 bad_id=0' ;;
    *) echo "fake-ok $key" ;;
  esac
fi
exit "$(cat "$CTL/rc/$key" 2>/dev/null || echo 0)"
FAKE

  cat > "$td/bin/rsync" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?}
{ echo "###"; printf '%s\n' "$@"; echo "###end"; } >> "$CTL/rsync.args"
list=""
for a in "$@"; do
  case "$a" in --files-from=*) list=${a#--files-from=} ;; esac
  dest=$a
done
cp "$list" "$CTL/rsync.list"
rc=0
while IFS= read -r rel; do
  if grep -qxF "$rel" "$CTL/rsync-gone" 2>/dev/null; then rc=23; continue; fi
  mkdir -p "$dest$(dirname "$rel")"
  echo jpeg > "$dest$rel"
done < "$list"
[ -f "$CTL/rc/rsync" ] && rc=$(cat "$CTL/rc/rsync")
exit "$rc"
FAKE

  cat > "$td/bin/timeout" <<'FAKE'
#!/bin/sh
shift
exec "$@"
FAKE

  cat > "$td/bin/flock" <<'FAKE'
#!/bin/sh
set -eu
case "$1" in
  -w) [ "${FLOCK_WAIT_FAIL:-0}" = 1 ] && exit 1; shift 3; [ $# -eq 0 ] && exit 0; exec "$@" ;;
  -n) [ "${FLOCK_HOLD_FAIL:-0}" = 1 ] && exit 1; exit 0 ;;
esac
exit 99
FAKE

  cat > "$td/bin/shred" <<'FAKE'
#!/bin/sh
for a in "$@"; do case "$a" in -*) ;; *) rm -f "$a" ;; esac; done
FAKE

  cat > "$td/alert.sh" <<'FAKE'
#!/bin/sh
echo "$1" >> "${FAKE_CTL:?}/alerts"
FAKE
  chmod +x "$td/bin/"* "$td/alert.sh"
}

run_job() {
  td=$1
  set +e
  env PATH="$td/bin:$PATH" FAKE_CTL="$td/ctl" \
    IMAGE_MIRROR_BASE="$td/base" IMAGE_MIRROR_ALERT="$td/alert.sh" \
    IMAGE_MIRROR_VNDB_LOCK="$td/vndb/.lock" IMAGE_MIRROR_VNDB_RSYNC="rsync://fake/vndb-img/" \
    sh "$RUN_SH" > "$td/ctl/stdout" 2>&1
  echo $? > "$td/ctl/exit"
  set -e
}

exit_is() { [ "$(cat "$1/ctl/exit")" = "$2" ]; }
has_stamp() { [ -f "$1/base/state/last-success" ]; }
has_alert() { [ -s "$1/ctl/alerts" ]; }
applied() { grep -c . "$1/ctl/apply.log" 2>/dev/null || echo 0; }
rsynced() { [ -f "$1/ctl/rsync.args" ]; }

expect_blocked() {
  if exit_is "$1" 0; then fail "exit 0"; fi
  if has_stamp "$1"; then fail "stamp written"; fi
  if ! has_alert "$1"; then fail "no alert"; fi
  if rsynced "$1"; then fail "rsync ran"; fi
  if [ "$(applied "$1")" != 0 ]; then fail "uploads ran"; fi
}

# --- T1: the happy path fetches exactly the listed files, uploads mirror-only, and cleans up ---
tstart 1
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
has_stamp "$td" || fail "no stamp"
has_alert "$td" && fail "alerted"
printf 'ch/12/12.jpg\nch/50/250.jpg\ncv/50/150.jpg\n' > "$td/ctl/want.list"
cmp -s "$td/ctl/want.list" "$td/ctl/rsync.list" || fail "fetch list: $(cat "$td/ctl/rsync.list" 2>/dev/null)"
grep -qx 'rsync://fake/vndb-img/' "$td/ctl/rsync.args" || fail "rsync source"
grep -qx 'mirror/vndb/' "$td/ctl/rsync.args" || fail "rsync destination"
cat > "$td/ctl/want.apply" <<'EOF'
backfill-vndb-covers --from-dump --image-dir /w/mirror/vndb --mirror-only --workers 2 --upload-gap 200ms --apply
backfill-character-portraits --vndb-image-dir /w/mirror/vndb --upload-gap 100ms --apply
EOF
cmp -s "$td/ctl/want.apply" "$td/ctl/apply.log" || fail "apply lines: $(cat "$td/ctl/apply.log")"
[ -d "$td/base/mirror/vndb" ] && fail "mirror not removed"
[ -f "$td/base/env.tmp" ] && fail "env.tmp left behind"
[ -s "$td/ctl/violations" ] && fail "$(cat "$td/ctl/violations")"
tend; rm -rf "$td"

# --- T2: a covers list past its ceiling fetches and uploads nothing ---
tstart 2
td=$(mktemp -d); install_fakes "$td"
echo 'INFO backfill-vndb-covers summary result="candidates=9 planned=501 errors=0 to_fetch=501"' > "$td/ctl/out/covers+dry"
run_job "$td"; expect_blocked "$td"
tend; rm -rf "$td"

# --- T3: so does a portraits list past its ceiling ---
tstart 3
td=$(mktemp -d); install_fakes "$td"
echo 'INFO char-portraits DRY forecast candidates=154912 skipped_has_hash=0 local_present=0 missing_file=3001 bad_id=0' > "$td/ctl/out/portraits+dry"
run_job "$td"; expect_blocked "$td"
tend; rm -rf "$td"

# --- T4: an unreadable count is a failure, not a pass ---
tstart 4
td=$(mktemp -d); install_fakes "$td"
echo 'INFO backfill-vndb-covers summary result="candidates=9 planned=1"' > "$td/ctl/out/covers+dry"
run_job "$td"; expect_blocked "$td"
tend; rm -rf "$td"

# --- T5: a failed dry run stops the run ---
tstart 5
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/portraits+dry"
run_job "$td"; expect_blocked "$td"
tend; rm -rf "$td"

# --- T6: an image VNDB removed (rsync 23) still uploads the rest; a network failure uploads nothing ---
tstart 6
td=$(mktemp -d); install_fakes "$td"
echo 'ch/50/250.jpg' > "$td/ctl/rsync-gone"
run_job "$td"
exit_is "$td" 0 || fail "rc 23 failed the run"
[ "$(applied "$td")" = 2 ] || fail "uploads after rc 23: $(applied "$td")"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 10 > "$td/ctl/rc/rsync"
run_job "$td"
exit_is "$td" 0 && fail "rc 10 passed"
has_alert "$td" || fail "no alert on rc 10"
[ "$(applied "$td")" = 0 ] || fail "uploaded after rc 10"
tend; rm -rf "$td"

# --- T7: nothing to fetch skips rsync and still runs the (no-op) uploads ---
tstart 7
td=$(mktemp -d); install_fakes "$td"
: > "$td/ctl/files/covers"; : > "$td/ctl/files/portraits"
echo 'INFO backfill-vndb-covers summary result="planned=0 to_fetch=0"' > "$td/ctl/out/covers+dry"
echo 'INFO char-portraits DRY forecast missing_file=0' > "$td/ctl/out/portraits+dry"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
rsynced "$td" && fail "rsync ran with an empty list"
[ "$(applied "$td")" = 2 ] || fail "uploads: $(applied "$td")"
tend; rm -rf "$td"

# --- T8: one lane failing still runs the other, fails the run, and keeps the copy ---
tstart 8
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/covers+apply"
run_job "$td"
exit_is "$td" 0 && fail "exit 0"
has_alert "$td" || fail "no alert"
has_stamp "$td" && fail "stamp written"
grep -q '^portraits+apply$' "$td/ctl/tools.log" || fail "portraits skipped after the covers failure"
[ -f "$td/base/mirror/vndb/cv/50/150.jpg" ] || fail "mirror removed after a failure"
tend; rm -rf "$td"

# --- T9: a run that finds the lock held does nothing and leaves the running run's snapshot ---
tstart 9
td=$(mktemp -d); install_fakes "$td"
echo 'KUN_PG_PASSWORD=held' > "$td/base/env.tmp"
FLOCK_HOLD_FAIL=1 run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
has_stamp "$td" && fail "stamp written"
[ -f "$td/base/env.tmp" ] || fail "shredded the running run's env.tmp"
[ -f "$td/ctl/tools.log" ] && fail "tools ran"
tend; rm -rf "$td"

# --- T10: a vndb-refresh lock timeout alerts and shreds nothing ---
tstart 10
td=$(mktemp -d); install_fakes "$td"
echo 'KUN_PG_PASSWORD=held' > "$td/base/env.tmp"
FLOCK_WAIT_FAIL=1 run_job "$td"
exit_is "$td" 0 && fail "exit 0"
has_alert "$td" || fail "no alert"
[ -f "$td/base/env.tmp" ] || fail "shredded the running run's env.tmp"
tend; rm -rf "$td"

# --- T11: no password on any container's argv ---
tstart 11
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
grep -q 'not-on-argv' "$td/ctl/docker.args" && fail "password on argv"
grep -q ' password=' "$td/ctl/docker.args" && fail "password= in a DSN"
tend; rm -rf "$td"

[ "$FAILED" -eq 0 ]
