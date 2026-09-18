#!/bin/sh
# Offline harness for run.sh beside it: fake docker / rsync / timeout / flock /
# shred / alert on PATH, so it never reaches Docker, VNDB, DLsite or a database.
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
  printf 'RJ01\nVJ02\n' > "$td/ctl/files/dlsite"
  printf '101\n102\n' > "$td/ctl/files/bgmcovers"
  printf '7\n9\n' > "$td/ctl/files/bgmlogos"
  printf '9\n8\n' > "$td/ctl/files/bgmphotos"

  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
{ echo "###"; printf '%s\n' "$@"; echo "###end"; } >> "$CTL/docker.args"
case "$1" in
  pull) exit 0 ;;
  image)
    case "$*" in
      *kun-dlsite-api*) echo "ghcr.io/kunmoe/kun-dlsite-api@sha256:beef" ;;
      *) echo "ghcr.io/next-moe/infra-tools@sha256:cafe" ;;
    esac
    exit 0 ;;
  inspect)
    printf '%s\n' 'KUN_CATALOG_PG_USER=cron' 'KUN_CATALOG_PG_PASSWORD=not-on-argv' 'KUN_PG_PASSWORD=not-on-argv'
    exit 0 ;;
  run) shift ;;
  *) echo "fake docker: unexpected $*" >&2; exit 99 ;;
esac
host_w=""
envfile=""
while [ $# -gt 0 ]; do
  case "$1" in
    --rm) shift ;;
    -v) case "$2" in *:/w) host_w=${2%:/w} ;; esac; shift 2 ;;
    --env-file) envfile=$2; shift 2 ;;
    --network|-e|--user|--name) shift 2 ;;
    -*) echo "fake docker run: unknown option $1" >&2; exit 99 ;;
    *) break ;;
  esac
done
image=$1
shift
if [ "$1" = fetch-bangumi-images ]; then
  kind=$(printf '%s\n' "$@" | sed -n '/^--kind$/{n;p;}')
  printf '%s\n' "$@" > "$CTL/bgm-fetch-$kind.args"
  echo "${envfile:-none}" > "$CTL/bgm-fetch-$kind.envfile"
  echo "bgm-fetch+$kind" >> "$CTL/tools.log"
  list=$(printf '%s\n' "$@" | sed -n '/^--ids-file$/{n;p;}' | sed 's|^/w/||')
  out=$(printf '%s\n' "$@" | sed -n '/^--out$/{n;p;}' | sed 's|^/w/||')
  n=0
  while IFS= read -r id; do
    mkdir -p "$host_w/$out/$id"
    if [ "$kind" = covers ]; then echo jpeg > "$host_w/$out/$id/cover.jpg"; else echo jpeg > "$host_w/$out/$id/logo.jpg"; fi
    n=$((n + 1))
  done < "$host_w/$list"
  if [ -f "$CTL/out/bgmfetch-$kind" ]; then
    cat "$CTL/out/bgmfetch-$kind"
  else
    echo "fetch-bangumi-images: done — kind=$kind ids=$n downloaded=$n skipped_exist=0 no_image=0 not_found=0 errors=0"
  fi
  exit "$(cat "$CTL/rc/bgmfetch-$kind" 2>/dev/null || echo 0)"
fi
if [ "$1" = mirror ]; then
  echo "crawler image=$image" >> "$CTL/tools.log"
  cp "$envfile" "$CTL/crawler.env"
  ls -l "$envfile" | cut -c1-10 > "$CTL/crawler.env.mode"
  printf '%s\n' "$@" > "$CTL/crawler.args"
  list=$(printf '%s\n' "$@" | sed -n '/^--worknos-file$/{n;p;}' | sed 's|^/w/||')
  out=$(printf '%s\n' "$@" | sed -n '/^--out$/{n;p;}' | sed 's|^/w/||')
  n=0
  while IFS= read -r w; do
    mkdir -p "$host_w/$out/$w"
    echo jpeg > "$host_w/$out/$w/${w}_img_main.jpg"
    n=$((n + 1))
  done < "$host_w/$list"
  if [ -f "$CTL/out/crawler" ]; then
    cat "$CTL/out/crawler"
  else
    echo "2026/09/17 mirror: done — worknos=$n downloaded=$n skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=0"
  fi
  exit "$(cat "$CTL/rc/crawler" 2>/dev/null || echo 0)"
fi
[ "$1" = sh ] && [ "$2" = -c ] || { echo "fake docker run: expected sh -c" >&2; exit 99; }
script=$3
case "$script" in
  *backfill-vndb-covers*) tool=covers ;;
  *backfill-character-portraits*) tool=portraits ;;
  *backfill-dlsite-media*) tool=dlsite ;;
  *backfill-bangumi-covers*) tool=bgmcovers ;;
  *backfill-label-logos*) tool=bgmlogos ;;
  *backfill-person-photos*) tool=bgmphotos ;;
  *) echo "fake docker run: unknown tool: $script" >&2; exit 99 ;;
esac
cmd=$(printf '%s\n' "$script" | sed 's/.*\(backfill-[a-z-]*.*\)/\1/')
mode=dry
case "$cmd" in *--apply*) mode=apply ;; esac
echo "$tool+$mode" >> "$CTL/tools.log"
if [ "$mode" = apply ]; then
  echo "$cmd" | sed 's/--dsn "\$CAT" //' >> "$CTL/apply.log"
  ls "$host_w/mirror" >/dev/null 2>&1 || echo "$tool applied without a mirror" >> "$CTL/violations"
  [ -f "$host_w/env.dlsite" ] && echo "env.dlsite still on disk during the $tool upload" >> "$CTL/violations"
else
  if [ "$tool" = dlsite ]; then
    cp "$host_w/state/dlsite-cdn-missing" "$CTL/dlsite-cdn-missing.dry"
  fi
  if [ "$tool" = bgmcovers ] && [ ! -f "$host_w/mirror/bangumi/covers/dims.jsonl" ]; then
    echo "bangumi covers dry run found no dims.jsonl" >&2; exit 1
  fi
  out=$(printf '%s\n' "$cmd" | sed -n 's/.*--\(files\|worknos\|subjects\|ids\)-out \/w\/\([^ ]*\).*/\2/p')
  [ -n "$out" ] || { echo "$tool dry run without a list output" >> "$CTL/violations"; }
  if [ -n "$out" ] && [ -f "$CTL/files/$tool" ]; then cp "$CTL/files/$tool" "$host_w/$out"; fi
fi
key="$tool+$mode"
if [ -f "$CTL/out/$key" ]; then
  cat "$CTL/out/$key"
else
  case "$key" in
    covers+dry) echo 'INFO backfill-vndb-covers summary result="candidates=1168 no_image=1006 portrait=150 landscape=12 planned=162 uploaded=0 dedup=0 rejected=0 errors=0 local=0 quota=false unrated=0 missing=0 to_fetch=1"' ;;
    portraits+dry) echo 'INFO char-portraits DRY forecast candidates=154912 skipped_has_hash=147947 local_present=0 missing_file=2 bad_id=0' ;;
    dlsite+dry) echo 'INFO dlsite-media done summary="map[apply:false candidates:10751 errors:0 unmirrored_works:2]"' ;;
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
applied() { n=$(grep -c "${2:-.}" "$1/ctl/apply.log" 2>/dev/null); echo "${n:-0}"; }
vndb_applied() { applied "$1" 'backfill-vndb-covers\|backfill-character-portraits'; }
dlsite_applied() { applied "$1" 'backfill-dlsite-media'; }
bgm_applied() { applied "$1" 'backfill-bangumi-covers\|backfill-label-logos\|backfill-person-photos'; }
rsynced() { [ -f "$1/ctl/rsync.args" ]; }

expect_blocked() {
  if exit_is "$1" 0; then fail "exit 0"; fi
  if has_stamp "$1"; then fail "stamp written"; fi
  if ! has_alert "$1"; then fail "no alert"; fi
  if rsynced "$1"; then fail "rsync ran"; fi
  if [ "$(vndb_applied "$1")" != 0 ]; then fail "vndb uploads ran"; fi
  if [ "$(dlsite_applied "$1")" != 1 ]; then fail "the dlsite lane did not run on its own"; fi
  if [ "$(bgm_applied "$1")" != 3 ]; then fail "the bangumi lane did not run on its own"; fi
}

expect_dlsite_blocked() {
  if exit_is "$1" 0; then fail "exit 0"; fi
  if has_stamp "$1"; then fail "stamp written"; fi
  if ! has_alert "$1"; then fail "no alert"; fi
  if [ "$(dlsite_applied "$1")" != 0 ]; then fail "dlsite uploads ran"; fi
  if [ "$(vndb_applied "$1")" != 2 ]; then fail "the vndb lane did not run on its own"; fi
  if [ "$(bgm_applied "$1")" != 3 ]; then fail "the bangumi lane did not run on its own"; fi
  if [ -f "$1/base/env.dlsite" ]; then fail "env.dlsite left behind"; fi
}

expect_bgm_blocked() {
  if exit_is "$1" 0; then fail "exit 0"; fi
  if has_stamp "$1"; then fail "stamp written"; fi
  if ! has_alert "$1"; then fail "no alert"; fi
  if [ "$(bgm_applied "$1")" != 0 ]; then fail "bangumi uploads ran"; fi
  if [ "$(vndb_applied "$1")" != 2 ] || [ "$(dlsite_applied "$1")" != 1 ]; then fail "the other lanes did not run"; fi
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
backfill-dlsite-media --dlsite-dsn "$DL" --kind cover,screenshot --mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --upload-gap 100ms --apply
backfill-bangumi-covers --bangumi-mirror /w/mirror/bangumi/covers --allow-landscape --upload-gap 100ms --apply
backfill-label-logos --source bangumi --mirror-dir /w/mirror/bangumi/persons --upload-gap 100ms --apply
backfill-person-photos --mirror-dir /w/mirror/bangumi/persons --upload-gap 100ms --apply
EOF
cmp -s "$td/ctl/want.apply" "$td/ctl/apply.log" || fail "apply lines: $(cat "$td/ctl/apply.log")"
[ -d "$td/base/mirror/vndb" ] && fail "vndb mirror not removed"
[ -d "$td/base/mirror/dlsite" ] && fail "dlsite mirror not removed"
[ -d "$td/base/mirror/bangumi" ] && fail "bangumi mirror not removed"
grep -qx 'crawler image=ghcr.io/kunmoe/kun-dlsite-api@sha256:beef' "$td/ctl/tools.log" || fail "crawler ran from $(grep crawler "$td/ctl/tools.log")"
printf 'mirror\n--worknos-file\n/w/state/dlsite.worknos\n--out\n/w/mirror/dlsite\n--rate\n2\n--concurrency\n3\n' > "$td/ctl/want.crawler"
cmp -s "$td/ctl/want.crawler" "$td/ctl/crawler.args" || fail "crawler args: $(tr '\n' ' ' < "$td/ctl/crawler.args")"
[ -f "$td/base/env.dlsite" ] && fail "env.dlsite left behind"
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
[ "$(vndb_applied "$td")" = 2 ] || fail "uploads after rc 23: $(vndb_applied "$td")"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 10 > "$td/ctl/rc/rsync"
run_job "$td"
exit_is "$td" 0 && fail "rc 10 passed"
has_alert "$td" || fail "no alert on rc 10"
[ "$(vndb_applied "$td")" = 0 ] || fail "uploaded after rc 10"
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
[ "$(vndb_applied "$td")" = 2 ] || fail "uploads: $(vndb_applied "$td")"
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

# --- T12: the crawler reads the password from its env file, never from its DSN ---
tstart 12
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
grep -qx 'DATABASE_URL=host=127.0.0.1 port=5432 user=cron dbname=dlsite sslmode=disable' "$td/ctl/crawler.env" || fail "DATABASE_URL: $(head -1 "$td/ctl/crawler.env")"
grep -qx 'PGPASSWORD=not-on-argv' "$td/ctl/crawler.env" || fail "no PGPASSWORD for the crawler"
grep -q '^-rw-------' "$td/ctl/crawler.env.mode" || fail "env.dlsite mode $(cat "$td/ctl/crawler.env.mode")"
grep -q 'not-on-argv' "$td/ctl/crawler.args" && fail "password on the crawler argv"
tend; rm -rf "$td"

# --- T13: a DLsite list past its ceiling fetches and uploads nothing there, and the VNDB lane still runs ---
tstart 13
td=$(mktemp -d); install_fakes "$td"
i=0; : > "$td/ctl/files/dlsite"
while [ $i -lt 3001 ]; do echo "RJ$i" >> "$td/ctl/files/dlsite"; i=$((i + 1)); done
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
[ "$(wc -l < "$td/base/state/dlsite.worknos")" = 3000 ] || fail "fetch list not cut to the batch: $(wc -l < "$td/base/state/dlsite.worknos")"
grep -qF "dlsite: batch 3000 of 3001 listed (at most 3000)" "$td/base/logs/run-$(date -u +%F).log" || fail "no batch line"
[ "$(dlsite_applied "$td")" = 1 ] || fail "dlsite upload skipped"
tend; rm -rf "$td"

# --- T14: a mirror that downloaded nothing and failed some is a failure, not a quiet week ---
tstart 14
td=$(mktemp -d); install_fakes "$td"
echo '2026/09/17 mirror: done — worknos=2 downloaded=0 skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=9' > "$td/ctl/out/crawler"
run_job "$td"; expect_dlsite_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/crawler"
run_job "$td"; expect_dlsite_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 'mirror: panic before the summary' > "$td/ctl/out/crawler"
run_job "$td"; expect_dlsite_blocked "$td"
tend; rm -rf "$td"

# --- T15: a partial mirror is fine; no DLsite work to fetch skips the crawler and still uploads ---
tstart 15
td=$(mktemp -d); install_fakes "$td"
echo '2026/09/17 mirror: done — worknos=2 downloaded=5 skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=2' > "$td/ctl/out/crawler"
run_job "$td"
exit_is "$td" 0 || fail "partial mirror failed the run"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
: > "$td/ctl/files/dlsite"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ -f "$td/ctl/crawler.args" ] && fail "the crawler ran with nothing to fetch"
[ "$(dlsite_applied "$td")" = 1 ] || fail "dlsite upload skipped"
tend; rm -rf "$td"

# --- T16: a failed DLsite dry run, or one that wrote no list, stops that lane only ---
tstart 16
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/dlsite+dry"
run_job "$td"; expect_dlsite_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
rm -f "$td/ctl/files/dlsite"
run_job "$td"; expect_dlsite_blocked "$td"
tend; rm -rf "$td"

# --- T17: the Bangumi lane fetches covers and one merged person list, then uploads all three ---
tstart 17
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
printf 'fetch-bangumi-images\n--kind\ncovers\n--ids-file\n/w/state/bgm-covers.ids\n--out\n/w/mirror/bangumi/covers\n' > "$td/ctl/want.covers"
cmp -s "$td/ctl/want.covers" "$td/ctl/bgm-fetch-covers.args" || fail "covers fetch args: $(tr '\n' ' ' < "$td/ctl/bgm-fetch-covers.args")"
printf '7\n8\n9\n' > "$td/ctl/want.persons"
cmp -s "$td/ctl/want.persons" "$td/base/state/bgm-persons.ids" || fail "persons list: $(tr '\n' ' ' < "$td/base/state/bgm-persons.ids")"
grep -qx 'none' "$td/ctl/bgm-fetch-persons.envfile" || fail "an env file reached the fetch without bangumi.env"
[ "$(bgm_applied "$td")" = 3 ] || fail "bangumi uploads: $(bgm_applied "$td")"
tend; rm -rf "$td"

# --- T18: bangumi.env reaches the fetch as an env file, and its token reaches no argv ---
tstart 18
td=$(mktemp -d); install_fakes "$td"
echo 'KUN_BANGUMI_TOKEN=tok-not-on-argv' > "$td/base/bangumi.env"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
grep -qx "$td/base/bangumi.env" "$td/ctl/bgm-fetch-covers.envfile" || fail "covers fetch env: $(cat "$td/ctl/bgm-fetch-covers.envfile")"
grep -qx "$td/base/bangumi.env" "$td/ctl/bgm-fetch-persons.envfile" || fail "persons fetch env: $(cat "$td/ctl/bgm-fetch-persons.envfile")"
grep -q 'tok-not-on-argv' "$td/ctl/docker.args" && fail "token on argv"
[ -f "$td/base/bangumi.env" ] || fail "bangumi.env was removed"
tend; rm -rf "$td"

# --- T19: a Bangumi list past its ceiling fetches and uploads nothing there ---
tstart 19
td=$(mktemp -d); install_fakes "$td"
seq 1 1001 > "$td/ctl/files/bgmcovers"
run_job "$td"; expect_bgm_blocked "$td"
[ -f "$td/ctl/bgm-fetch-covers.args" ] && fail "fetched past the ceiling"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
seq 1 1501 > "$td/ctl/files/bgmlogos"
run_job "$td"; expect_bgm_blocked "$td"
tend; rm -rf "$td"

# --- T20: a fetch that got nothing but errors, failed, or printed no summary blocks the lane ---
tstart 20
td=$(mktemp -d); install_fakes "$td"
echo 'fetch-bangumi-images: done — kind=persons ids=3 downloaded=0 skipped_exist=0 no_image=0 not_found=0 errors=3' > "$td/ctl/out/bgmfetch-persons"
run_job "$td"; expect_bgm_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/bgmfetch-covers"
run_job "$td"; expect_bgm_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 'panic' > "$td/ctl/out/bgmfetch-covers"
run_job "$td"; expect_bgm_blocked "$td"
rm -rf "$td"
td=$(mktemp -d); install_fakes "$td"
echo 'fetch-bangumi-images: done — kind=covers ids=2 downloaded=0 skipped_exist=0 no_image=1 not_found=1 errors=0' > "$td/ctl/out/bgmfetch-covers"
run_job "$td"
exit_is "$td" 0 || fail "a quiet fetch with nothing to download failed the run"
tend; rm -rf "$td"

# --- T21: empty lists skip the fetch and still run the uploads ---
tstart 21
td=$(mktemp -d); install_fakes "$td"
: > "$td/ctl/files/bgmcovers"; : > "$td/ctl/files/bgmlogos"; : > "$td/ctl/files/bgmphotos"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ -f "$td/ctl/bgm-fetch-covers.args" ] && fail "covers fetched with nothing to fetch"
[ -f "$td/ctl/bgm-fetch-persons.args" ] && fail "persons fetched with nothing to fetch"
[ "$(bgm_applied "$td")" = 3 ] || fail "uploads: $(bgm_applied "$td")"
tend; rm -rf "$td"

# --- T22: a dry run that writes no list blocks the lane; a failed one does too ---
tstart 22
for tool in bgmcovers bgmlogos bgmphotos; do
  td=$(mktemp -d); install_fakes "$td"
  rm -f "$td/ctl/files/$tool"
  run_job "$td"; expect_bgm_blocked "$td"
  rm -rf "$td"
done
td=$(mktemp -d); install_fakes "$td"
echo 1 > "$td/ctl/rc/bgmphotos+dry"
run_job "$td"; expect_bgm_blocked "$td"
tend; rm -rf "$td"

# --- T23: CDN 404s are recorded; 403 and network errors are not ---
tstart 23
td=$(mktemp -d); install_fakes "$td"
cat > "$td/ctl/out/crawler" <<'EOF'
2026/09/18 04:42:36 mirror: RJ01008586 RJ01008586_img_smp2.jpg: http 404
2026/09/18 04:42:36 mirror: RJ01008586 RJ01008586_img_main.jpg: http 404
2026/09/18 04:42:36 mirror: RJ01008587 RJ01008587_img_main.jpg: http 403
2026/09/18 04:42:36 mirror: RJ01 RJ01_img_smp9.jpg: Get "https://x": dial tcp: i/o timeout
2026/09/18 04:56:54 mirror: done — worknos=2 downloaded=1 skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=3
EOF
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
has_stamp "$td" || fail "no stamp"
today=$(date -u +%F)
printf '%s %s\n' "$today" "RJ01008586/RJ01008586_img_main.jpg" "$today" "RJ01008586/RJ01008586_img_smp2.jpg" > "$td/ctl/want.ledger"
cmp -s "$td/ctl/want.ledger" "$td/base/state/dlsite-cdn-404" || fail "ledger: $(cat "$td/base/state/dlsite-cdn-404" 2>/dev/null)"
printf '%s\n' "RJ01008586/RJ01008586_img_main.jpg" "RJ01008586/RJ01008586_img_smp2.jpg" > "$td/ctl/want.missing"
cmp -s "$td/ctl/want.missing" "$td/base/state/dlsite-cdn-missing" || fail "missing: $(cat "$td/base/state/dlsite-cdn-missing" 2>/dev/null)"
grep -q 'RJ01008587/' "$td/base/state/dlsite-cdn-404" && fail "403 recorded"
grep -q 'RJ01/RJ01_img_smp9.jpg' "$td/base/state/dlsite-cdn-404" && fail "timeout recorded"
grep -q 'RJ01008587/' "$td/base/state/dlsite-cdn-missing" && fail "403 in missing"
grep -q 'RJ01/RJ01_img_smp9.jpg' "$td/base/state/dlsite-cdn-missing" && fail "timeout in missing"
grep -qF -- '--mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --worknos-out' "$td/ctl/docker.args" || fail "dry run missing --cdn-missing"
grep -qF -- '--mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --upload-gap' "$td/ctl/docker.args" || fail "apply missing --cdn-missing"
tend; rm -rf "$td"

# --- T24: expired entries drop before the dry run; a repeat 404 updates the date once ---
tstart 24
td=$(mktemp -d); install_fakes "$td"
mkdir -p "$td/base/state"
x_date=$(date -u -d '91 days ago' +%F)
y_date=$(date -u -d '90 days ago' +%F)
z_date=$(date -u -d '10 days ago' +%F)
{
  printf '%s %s\n' "$x_date" "RJ01/RJ01_img_main.jpg"
  printf '%s %s\n' "$y_date" "RJ02/RJ02_img_main.jpg"
  printf '%s %s\n' "$z_date" "RJ03/RJ03_img_main.jpg"
} | sort -k2,2 > "$td/base/state/dlsite-cdn-404"
cat > "$td/ctl/out/crawler" <<'EOF'
2026/09/18 04:42:36 mirror: RJ03 RJ03_img_main.jpg: http 404
2026/09/18 04:56:54 mirror: done — worknos=2 downloaded=1 skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=1
EOF
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
printf '%s\n' "RJ02/RJ02_img_main.jpg" "RJ03/RJ03_img_main.jpg" > "$td/ctl/want.dry"
cmp -s "$td/ctl/want.dry" "$td/ctl/dlsite-cdn-missing.dry" || fail "dry missing: $(cat "$td/ctl/dlsite-cdn-missing.dry" 2>/dev/null)"
today=$(date -u +%F)
{
  printf '%s %s\n' "$y_date" "RJ02/RJ02_img_main.jpg"
  printf '%s %s\n' "$today" "RJ03/RJ03_img_main.jpg"
} > "$td/ctl/want.ledger"
cmp -s "$td/ctl/want.ledger" "$td/base/state/dlsite-cdn-404" || fail "ledger: $(cat "$td/base/state/dlsite-cdn-404" 2>/dev/null)"
grep -q 'RJ01/RJ01_img_main.jpg' "$td/base/state/dlsite-cdn-404" && fail "expired X kept"
n=$(grep -c 'RJ03/RJ03_img_main.jpg' "$td/base/state/dlsite-cdn-404" || true)
[ "$n" = 1 ] || fail "Z lines: $n"
tend; rm -rf "$td"

# --- T25: 404s are recorded before the downloaded-nothing FATAL ---
tstart 25
td=$(mktemp -d); install_fakes "$td"
cat > "$td/ctl/out/crawler" <<'EOF'
2026/09/18 04:42:36 mirror: RJ01008586 RJ01008586_img_smp2.jpg: http 404
2026/09/18 04:42:36 mirror: RJ01008586 RJ01008586_img_main.jpg: http 404
2026/09/18 04:56:54 mirror: done — worknos=2 downloaded=0 skipped_exist=0 skipped_placeholder=0 skip_no_meta=0 errors=2
EOF
run_job "$td"; expect_dlsite_blocked "$td"
today=$(date -u +%F)
printf '%s %s\n' "$today" "RJ01008586/RJ01008586_img_main.jpg" "$today" "RJ01008586/RJ01008586_img_smp2.jpg" > "$td/ctl/want.ledger"
cmp -s "$td/ctl/want.ledger" "$td/base/state/dlsite-cdn-404" || fail "ledger: $(cat "$td/base/state/dlsite-cdn-404" 2>/dev/null)"
tend; rm -rf "$td"

# --- T26: no ledger at start still writes an empty missing file and passes the flag ---
tstart 26
td=$(mktemp -d); install_fakes "$td"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ -f "$td/base/state/dlsite-cdn-missing" ] || fail "missing file absent"
[ ! -s "$td/base/state/dlsite-cdn-missing" ] || fail "missing file not empty: $(cat "$td/base/state/dlsite-cdn-missing")"
[ -f "$td/base/state/dlsite-cdn-404" ] && [ -s "$td/base/state/dlsite-cdn-404" ] && fail "ledger grew: $(cat "$td/base/state/dlsite-cdn-404")"
grep -qF -- '--mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --worknos-out' "$td/ctl/docker.args" || fail "dry run missing --cdn-missing"
tend; rm -rf "$td"

# --- T27: the fetch list is cut to IMAGE_MIRROR_DLSITE_BATCH, and the dry run is never given --limit ---
tstart 27
td=$(mktemp -d); install_fakes "$td"
i=0; : > "$td/ctl/files/dlsite"
while [ $i -lt 100 ]; do echo "RJ$i" >> "$td/ctl/files/dlsite"; i=$((i + 1)); done
IMAGE_MIRROR_DLSITE_BATCH=42 run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ "$(wc -l < "$td/base/state/dlsite.worknos")" = 42 ] || fail "fetch list not cut to 42"
grep -qF -- '--limit' "$td/ctl/docker.args" && fail "the dry run was given --limit"
tend; rm -rf "$td"

# --- T28: a batch of 3000 listed works passes the guard and the download and apply run ---
tstart 28
td=$(mktemp -d); install_fakes "$td"
i=0; : > "$td/ctl/files/dlsite"
while [ $i -lt 3000 ]; do echo "RJ$i" >> "$td/ctl/files/dlsite"; i=$((i + 1)); done
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td/ctl/stdout")"
[ -f "$td/ctl/crawler.args" ] || fail "the crawler did not run"
[ "$(dlsite_applied "$td")" = 1 ] || fail "dlsite upload skipped"
tend; rm -rf "$td"

# --- T29: IMAGE_MIRROR_DLSITE_BATCH=5000 with 5000 listed works fails because the guard is 3000 ---
tstart 29
td=$(mktemp -d); install_fakes "$td"
i=0; : > "$td/ctl/files/dlsite"
while [ $i -lt 5000 ]; do echo "RJ$i" >> "$td/ctl/files/dlsite"; i=$((i + 1)); done
IMAGE_MIRROR_DLSITE_BATCH=5000 run_job "$td"; expect_dlsite_blocked "$td"
[ -f "$td/ctl/crawler.args" ] && fail "the crawler ran past the ceiling"
tend; rm -rf "$td"

[ "$FAILED" -eq 0 ]
