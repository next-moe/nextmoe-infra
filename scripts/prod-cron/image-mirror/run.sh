#!/bin/sh
# shellcheck disable=SC2016
# Weekly image mirror (catalog): VNDB covers and character portraits, DLsite
# covers and sample screenshots, Bangumi covers, company logos and person photos.
# Canonical copy: scripts/prod-cron/image-mirror/run.sh in nextmoe-infra — edit
# there and redeploy; the box copy is /root/image-mirror/run.sh and must stay
# byte-identical.
#
# The staged dumps carry image references, not bytes, and the backfill tools
# read a local copy of the bytes. Nothing produced one after the July and
# August backfills, so every work and character added since then stayed without
# its images (6,965 VNDB portraits, 162 VNDB covers, 20,460 DLsite screenshots
# and 257 DLsite covers on 2026-09-17). Each lane asks its tool for the files it
# lacks, fetches exactly those, uploads them, and deletes the copy.
#
#   vndb   — rsync://dl.vndb.org/vndb-img, the path VNDB offers bulk consumers
#   dlsite — kun-dlsite-api `mirror`, reading prod's restaged dlsite database.
#            DLsite answers this host with a redirect on www.dlsite.com, but its
#            image CDN serves it (200 on 2026-09-17).
#   bangumi — fetch-bangumi-images, Bangumi's API then its CDN. The dump
#            carries no image fields. Anonymous requests get 404 for NSFW
#            subjects (143 of the 436 coverless works on 2026-09-17), so a
#            KUN_BANGUMI_TOKEN in $BASE/bangumi.env is used when present.
#
# Crontab: Monday 03:30 CST (`30 3 * * 1`), after Sunday's vndb-refresh and
# crawler-restage, whose loads are what bring new references.
set -eu
BASE=${IMAGE_MIRROR_BASE:-/root/image-mirror}
ALERT_SH=${IMAGE_MIRROR_ALERT:-/root/lib/alert.sh}
VNDB_LOCK=${IMAGE_MIRROR_VNDB_LOCK:-/root/vndb-refresh/.lock}
VNDB_RSYNC=${IMAGE_MIRROR_VNDB_RSYNC:-rsync://dl.vndb.org/vndb-img/}
DLSITE_IMG_TAG=${IMAGE_MIRROR_DLSITE_IMAGE:-ghcr.io/kunmoe/kun-dlsite-api:latest}
COVERS_MAX=${IMAGE_MIRROR_COVERS_MAX:-500}
PORTRAITS_MAX=${IMAGE_MIRROR_PORTRAITS_MAX:-3000}
DLSITE_BATCH=${IMAGE_MIRROR_DLSITE_BATCH:-3000}
DLSITE_WORKS_MAX=${IMAGE_MIRROR_DLSITE_WORKS_MAX:-3000}
DLSITE_404_RETRY_DAYS=${IMAGE_MIRROR_DLSITE_404_RETRY_DAYS:-90}
BANGUMI_COVERS_MAX=${IMAGE_MIRROR_BANGUMI_COVERS_MAX:-1000}
BANGUMI_PERSONS_MAX=${IMAGE_MIRROR_BANGUMI_PERSONS_MAX:-1500}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1

LOCKED=0
FAIL=0
# A failing run alerts; a succeeding run stamps state/last-success for
# /root/lib/watchdog.sh. Both are gated on LOCKED: a run that found the lock
# held did no work, and its cleanup must not shred the running run's env files.
on_exit() {
  rc=$?
  if [ "${LOCKED:-0}" = 1 ]; then
    for f in env.tmp env.dlsite; do
      if [ -f "$f" ]; then shred -u "$f"; fi
    done
  fi
  [ "$rc" -eq 0 ] && [ "${FAIL:-0}" -ne 0 ] && rc=1
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] image-mirror (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== image-mirror start $(date -u '+%F %T')Z ==="

echo "waiting on vndb-refresh lock $VNDB_LOCK (up to 7200s)"
if ! flock -w 7200 "$VNDB_LOCK" true; then
  echo "FATAL: timed out waiting for vndb-refresh ($VNDB_LOCK)"
  exit 1
fi

exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp
# --user 0:0: the mirror under the /w mount is root-owned.
run() {
  docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" \
    -v "$BASE:/w" --user 0:0 "$IMG" "$@"
}
# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; B="host=127.0.0.1 port=5432 user=$U sslmode=disable"; CAT="$B dbname=kun_catalog"; DL="$B dbname=dlsite"'

counter() {
  sed -n "s/.*$1=\\([0-9][0-9]*\\).*/\\1/p" "$2" | tail -1
}

write_dlsite_cdn_missing() {
  if [ -f state/dlsite-cdn-404 ]; then
    awk 'NF >= 2 { print $2 }' state/dlsite-cdn-404 > state/dlsite-cdn-missing.tmp || return 1
    sort state/dlsite-cdn-missing.tmp -o state/dlsite-cdn-missing.tmp || return 1
  else
    : > state/dlsite-cdn-missing.tmp || return 1
  fi
  mv state/dlsite-cdn-missing.tmp state/dlsite-cdn-missing || return 1
}

# `set -e` does not reach inside a function called as a condition, so every
# step in a lane checks itself.
lane_vndb() {
  m=mirror/vndb
  rm -rf "$m" && mkdir -p "$m" || return 1
  run sh -c "$DSNSH"'; backfill-vndb-covers --dsn "$CAT" --from-dump --image-dir /w/mirror/vndb --files-out /w/state/covers.files' \
    > state/covers-dry.log 2>&1 || { echo "FATAL: covers dry run failed"; cat state/covers-dry.log; return 1; }
  run sh -c "$DSNSH"'; backfill-character-portraits --dsn "$CAT" --vndb-image-dir /w/mirror/vndb --files-out /w/state/portraits.files' \
    > state/portraits-dry.log 2>&1 || { echo "FATAL: portraits dry run failed"; cat state/portraits-dry.log; return 1; }
  covers=$(counter to_fetch state/covers-dry.log)
  portraits=$(counter missing_file state/portraits-dry.log)
  echo "vndb: covers to fetch ${covers:-?} (ceiling $COVERS_MAX), portraits ${portraits:-?} (ceiling $PORTRAITS_MAX)"
  if [ -z "$covers" ] || [ -z "$portraits" ]; then
    echo "FATAL: could not read the vndb dry-run counts"; cat state/covers-dry.log state/portraits-dry.log; return 1
  fi
  if [ "$covers" -gt "$COVERS_MAX" ] || [ "$portraits" -gt "$PORTRAITS_MAX" ]; then
    echo "FATAL: vndb fetch list over its ceiling — inspect state/*.files before raising it"; return 1
  fi

  cat state/covers.files state/portraits.files | sort -u > state/vndb.files || return 1
  if [ -s state/vndb.files ]; then
    rc=0
    timeout 3h rsync -a --contimeout=60 --timeout=300 --files-from=state/vndb.files "$VNDB_RSYNC" "$m/" || rc=$?
    # A listed image VNDB has since removed ends the transfer with 23 (the
    # 2026-09-17 probe: 6 of 6,963 portraits). The tools count those as missing.
    case "$rc" in
      0|23|24) ;;
      *) echo "FATAL: rsync exited $rc"; return 1 ;;
    esac
  fi
  echo "vndb: fetched $(find "$m" -type f | wc -l) of $(wc -l < state/vndb.files) files"

  ok=0
  run sh -c "$DSNSH"'; backfill-vndb-covers --dsn "$CAT" --from-dump --image-dir /w/mirror/vndb --mirror-only --workers 2 --upload-gap 200ms --apply' \
    || { echo "WARN: covers upload failed"; ok=1; }
  run sh -c "$DSNSH"'; backfill-character-portraits --dsn "$CAT" --vndb-image-dir /w/mirror/vndb --upload-gap 100ms --apply' \
    || { echo "WARN: portraits upload failed"; ok=1; }
  if [ "$ok" -eq 0 ]; then
    rm -rf "$m"
  fi
  return "$ok"
}

lane_dlsite() {
  m=mirror/dlsite
  rm -rf "$m" && mkdir -p "$m" || return 1
  rm -f state/dlsite.worknos
  # The first run (2026-09-18) re-listed 257 works to fetch 958 files the CDN answers 404 for,
  # and would have done so every week. A 404 is recorded and skips its file for $DLSITE_404_RETRY_DAYS days;
  # other failures are not recorded, since a CDN refusing this host must stay loud.
  cutoff=$(date -u -d "$DLSITE_404_RETRY_DAYS days ago" +%F) || return 1
  expired=0
  if [ -f state/dlsite-cdn-404 ]; then
    before=$(awk 'END { print NR }' state/dlsite-cdn-404)
    awk -v cutoff="$cutoff" 'NF >= 2 && $1 >= cutoff' state/dlsite-cdn-404 > state/dlsite-cdn-404.tmp || return 1
    sort -k2,2 state/dlsite-cdn-404.tmp -o state/dlsite-cdn-404.tmp || return 1
    mv state/dlsite-cdn-404.tmp state/dlsite-cdn-404 || return 1
    after=$(awk 'END { print NR }' state/dlsite-cdn-404)
    expired=$((before - after))
  fi
  write_dlsite_cdn_missing || return 1
  kept=$(awk 'END { print NR }' state/dlsite-cdn-missing)
  echo "dlsite: known CDN 404s $kept ($expired expired, retried after $DLSITE_404_RETRY_DAYS days)"
  run sh -c "$DSNSH"'; backfill-dlsite-media --dsn "$CAT" --dlsite-dsn "$DL" --kind cover,screenshot --mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --worknos-out /w/state/dlsite.worknos' \
    > state/dlsite-dry.log 2>&1 || { echo "FATAL: dlsite dry run failed"; cat state/dlsite-dry.log; return 1; }
  [ -f state/dlsite.worknos ] || { echo "FATAL: the dlsite dry run wrote no worknos file"; return 1; }
  # On 2026-09-18 two new lanes added about 39,000 DLsite products in one night; the full list
  # waits its turn in batches. The cut is on the fetch list, not the tool's --limit: that caps
  # candidates, and a work whose files the CDN answers 404 for stays a candidate that fetches nothing.
  listed=$(wc -l < state/dlsite.worknos)
  head -n "$DLSITE_BATCH" state/dlsite.worknos > state/dlsite.worknos.batch || return 1
  mv state/dlsite.worknos.batch state/dlsite.worknos || return 1
  works=$(wc -l < state/dlsite.worknos)
  echo "dlsite: batch $works of $listed listed (at most $DLSITE_BATCH)"
  if [ "$works" -gt "$DLSITE_WORKS_MAX" ]; then
    echo "FATAL: dlsite fetch list over its ceiling — inspect state/dlsite.worknos before raising it"; return 1
  fi

  if [ "$works" -gt 0 ]; then
    u=$(sed -n 's/^KUN_CATALOG_PG_USER=//p' env.tmp | head -1)
    [ -n "$u" ] || u=$(sed -n 's/^KUN_PG_USER=//p' env.tmp | head -1)
    p=$(sed -n 's/^KUN_CATALOG_PG_PASSWORD=//p' env.tmp | head -1)
    [ -n "$p" ] || p=$(sed -n 's/^KUN_PG_PASSWORD=//p' env.tmp | head -1)
    ( umask 077
      printf 'DATABASE_URL=host=127.0.0.1 port=5432 user=%s dbname=dlsite sslmode=disable\n' "$u"
      printf 'PGPASSWORD=%s\n' "$p" ) > env.dlsite || return 1
    chmod 600 env.dlsite
    docker pull -q "$DLSITE_IMG_TAG" >/dev/null 2>&1 || echo "WARN: dlsite image pull failed; using the local copy"
    dimg=$(docker image inspect --format '{{index .RepoDigests 0}}' "$DLSITE_IMG_TAG") || return 1
    echo "dlsite image: $dimg"
    docker run --rm --network "container:$PG" --env-file "$BASE/env.dlsite" -v "$BASE:/w" --user 0:0 \
      "$dimg" mirror --worknos-file /w/state/dlsite.worknos --out /w/mirror/dlsite --rate 2 --concurrency 3 \
      > state/dlsite-mirror.log 2>&1 || { echo "FATAL: dlsite mirror failed"; tail -20 state/dlsite-mirror.log; return 1; }
    shred -u env.dlsite
    today=$(date -u +%F) || return 1
    sed -n 's/.* mirror: \([A-Z][A-Z][0-9][0-9]*\) \([^ /:]\{1,\}\): http 404$/\1\/\2/p' \
      state/dlsite-mirror.log | sort -u > state/dlsite-cdn-404.found.tmp || return 1
    n404=$(awk 'END { print NR }' state/dlsite-cdn-404.found.tmp)
    if [ -f state/dlsite-cdn-404 ]; then
      ledger=state/dlsite-cdn-404
    else
      ledger=/dev/null
    fi
    awk -v today="$today" -v foundfile="state/dlsite-cdn-404.found.tmp" '
      BEGIN {
        while ((getline p < foundfile) > 0) if (p != "") found[p] = 1
        close(foundfile)
      }
      NF >= 2 {
        if ($2 in found) {
          print today, $2
          delete found[$2]
        } else {
          print $1, $2
        }
      }
      END {
        for (p in found) print today, p
      }
    ' "$ledger" > state/dlsite-cdn-404.tmp || return 1
    sort -k2,2 state/dlsite-cdn-404.tmp -o state/dlsite-cdn-404.tmp || return 1
    mv state/dlsite-cdn-404.tmp state/dlsite-cdn-404 || return 1
    rm -f state/dlsite-cdn-404.found.tmp
    write_dlsite_cdn_missing || return 1
    echo "dlsite: recorded $n404 CDN 404(s)"
    grep 'mirror: done' state/dlsite-mirror.log || true
    got=$(counter downloaded state/dlsite-mirror.log)
    bad=$(counter errors state/dlsite-mirror.log)
    # The mirror counts a failed download and exits 0, so a CDN that stopped
    # answering this host would look like a quiet week.
    if [ -z "$got" ] || { [ "${bad:-0}" -gt 0 ] && [ "$got" -eq 0 ]; }; then
      echo "FATAL: dlsite mirror downloaded nothing (downloaded=${got:-?} errors=${bad:-?})"; return 1
    fi
  fi

  run sh -c "$DSNSH"'; backfill-dlsite-media --dsn "$CAT" --dlsite-dsn "$DL" --kind cover,screenshot --mirror-dir /w/mirror/dlsite --cdn-missing /w/state/dlsite-cdn-missing --upload-gap 100ms --apply' \
    || { echo "WARN: dlsite upload failed"; return 1; }
  rm -rf "$m"
}

lane_bangumi() {
  m=mirror/bangumi
  rm -rf "$m" && mkdir -p "$m/covers" "$m/persons" || return 1
  # backfill-bangumi-covers refuses a mirror without a manifest.
  : > "$m/covers/dims.jsonl" || return 1
  rm -f state/bgm-covers.ids state/bgm-logos.ids state/bgm-photos.ids
  run sh -c "$DSNSH"'; backfill-bangumi-covers --dsn "$CAT" --bangumi-mirror /w/mirror/bangumi/covers --subjects-out /w/state/bgm-covers.ids' \
    > state/bgm-covers-dry.log 2>&1 || { echo "FATAL: bangumi covers dry run failed"; cat state/bgm-covers-dry.log; return 1; }
  run sh -c "$DSNSH"'; backfill-label-logos --source bangumi --dsn "$CAT" --mirror-dir /w/mirror/bangumi/persons --ids-out /w/state/bgm-logos.ids' \
    > state/bgm-logos-dry.log 2>&1 || { echo "FATAL: bangumi logos dry run failed"; cat state/bgm-logos-dry.log; return 1; }
  run sh -c "$DSNSH"'; backfill-person-photos --dsn "$CAT" --mirror-dir /w/mirror/bangumi/persons --ids-out /w/state/bgm-photos.ids' \
    > state/bgm-photos-dry.log 2>&1 || { echo "FATAL: bangumi photos dry run failed"; cat state/bgm-photos-dry.log; return 1; }
  for f in bgm-covers bgm-logos bgm-photos; do
    [ -f "state/$f.ids" ] || { echo "FATAL: the $f dry run wrote no id list"; return 1; }
  done
  sort -u state/bgm-logos.ids state/bgm-photos.ids > state/bgm-persons.ids || return 1
  covers=$(wc -l < state/bgm-covers.ids)
  persons=$(wc -l < state/bgm-persons.ids)
  echo "bangumi: subjects to fetch $covers (ceiling $BANGUMI_COVERS_MAX), persons $persons (ceiling $BANGUMI_PERSONS_MAX)"
  if [ "$covers" -gt "$BANGUMI_COVERS_MAX" ] || [ "$persons" -gt "$BANGUMI_PERSONS_MAX" ]; then
    echo "FATAL: bangumi fetch list over its ceiling — inspect state/bgm-*.ids before raising it"; return 1
  fi

  if [ -f bangumi.env ]; then
    set -- --env-file "$BASE/bangumi.env"
  else
    echo "bangumi: no bangumi.env, fetching anonymously (NSFW subjects will read as not found)"
    set --
  fi
  for kind in covers persons; do
    [ "$kind" = covers ] && list=bgm-covers || list=bgm-persons
    [ -s "state/$list.ids" ] || continue
    docker run --rm --network "container:$PG" "$@" -v "$BASE:/w" --user 0:0 "$IMG" \
      fetch-bangumi-images --kind "$kind" --ids-file "/w/state/$list.ids" --out "/w/mirror/bangumi/$kind" \
      > "state/bgm-$kind-fetch.log" 2>&1 || { echo "FATAL: bangumi $kind fetch failed"; tail -20 "state/bgm-$kind-fetch.log"; return 1; }
    grep 'fetch-bangumi-images: done' "state/bgm-$kind-fetch.log" || true
    got=$(counter downloaded "state/bgm-$kind-fetch.log")
    bad=$(counter errors "state/bgm-$kind-fetch.log")
    if [ -z "$got" ] || { [ "${bad:-0}" -gt 0 ] && [ "$got" -eq 0 ]; }; then
      echo "FATAL: bangumi $kind fetch downloaded nothing (downloaded=${got:-?} errors=${bad:-?})"; return 1
    fi
  done

  ok=0
  # 62% of Bangumi game covers are landscape box art (wave 216); left out, those
  # works stay coverless and are fetched again every week.
  run sh -c "$DSNSH"'; backfill-bangumi-covers --dsn "$CAT" --bangumi-mirror /w/mirror/bangumi/covers --allow-landscape --upload-gap 100ms --apply' \
    || { echo "WARN: bangumi covers upload failed"; ok=1; }
  run sh -c "$DSNSH"'; backfill-label-logos --source bangumi --dsn "$CAT" --mirror-dir /w/mirror/bangumi/persons --upload-gap 100ms --apply' \
    || { echo "WARN: bangumi logos upload failed"; ok=1; }
  run sh -c "$DSNSH"'; backfill-person-photos --dsn "$CAT" --mirror-dir /w/mirror/bangumi/persons --upload-gap 100ms --apply' \
    || { echo "WARN: bangumi photos upload failed"; ok=1; }
  if [ "$ok" -eq 0 ]; then
    rm -rf "$m"
  fi
  return "$ok"
}

# The lanes share nothing but the image service, so one failing never stops
# another; any failing fails the run.
if ! lane_vndb; then echo "WARN: vndb lane failed"; FAIL=1; fi
if ! lane_dlsite; then echo "WARN: dlsite lane failed"; FAIL=1; fi
if ! lane_bangumi; then echo "WARN: bangumi lane failed"; FAIL=1; fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== image-mirror had a failed lane ==="; exit 1; }
echo "=== image-mirror done $(date -u '+%F %T')Z ==="
