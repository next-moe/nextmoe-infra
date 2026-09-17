#!/bin/sh
# shellcheck disable=SC2016
# Weekly VNDB image mirror (catalog covers and character portraits).
# Canonical copy: scripts/prod-cron/image-mirror/run.sh in nextmoe-infra — edit
# there and redeploy; the box copy is /root/image-mirror/run.sh and must stay
# byte-identical.
#
# The VNDB dump carries image ids, not bytes. VNDB serves the bytes to bulk
# consumers over rsync (rsync://dl.vndb.org/vndb-img), and the two backfill
# tools read a local copy of that tree. Nothing produced one after the July
# backfill, so every VNDB character and cover added since then stayed without
# an image (6,965 portraits on 2026-09-17). This job asks each tool for the
# files it lacks, fetches exactly those, uploads them, and deletes the copy.
#
# Crontab: Monday 03:30 CST (`30 3 * * 1`), after Sunday's vndb-refresh, whose
# ingest is what brings new ids; it also waits on that job's lock.
set -eu
BASE=${IMAGE_MIRROR_BASE:-/root/image-mirror}
ALERT_SH=${IMAGE_MIRROR_ALERT:-/root/lib/alert.sh}
VNDB_LOCK=${IMAGE_MIRROR_VNDB_LOCK:-/root/vndb-refresh/.lock}
VNDB_RSYNC=${IMAGE_MIRROR_VNDB_RSYNC:-rsync://dl.vndb.org/vndb-img/}
COVERS_MAX=${IMAGE_MIRROR_COVERS_MAX:-500}
PORTRAITS_MAX=${IMAGE_MIRROR_PORTRAITS_MAX:-3000}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1

LOCKED=0
FAIL=0
# A failing run alerts; a succeeding run stamps state/last-success for
# /root/lib/watchdog.sh. Both are gated on LOCKED: a run that found the lock
# held did no work, and its cleanup must not shred the running run's env.tmp.
on_exit() {
  rc=$?
  if [ "${LOCKED:-0}" = 1 ] && [ -f env.tmp ]; then shred -u env.tmp; fi
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
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; CAT="host=127.0.0.1 port=5432 user=$U dbname=kun_catalog sslmode=disable"'

counter() {
  sed -n "s/.*$1=\\([0-9][0-9]*\\).*/\\1/p" "$2" | tail -1
}

MIRROR=mirror/vndb
rm -rf "$MIRROR"
mkdir -p "$MIRROR"

# 1. The files each tool would upload and the mirror lacks.
if ! run sh -c "$DSNSH"'; backfill-vndb-covers --dsn "$CAT" --from-dump --image-dir /w/mirror/vndb --files-out /w/state/covers.files' \
     > state/covers-dry.log 2>&1; then
  echo "FATAL: covers dry run failed"; cat state/covers-dry.log; exit 1
fi
if ! run sh -c "$DSNSH"'; backfill-character-portraits --dsn "$CAT" --vndb-image-dir /w/mirror/vndb --files-out /w/state/portraits.files' \
     > state/portraits-dry.log 2>&1; then
  echo "FATAL: portraits dry run failed"; cat state/portraits-dry.log; exit 1
fi
COVERS=$(counter to_fetch state/covers-dry.log)
PORTRAITS=$(counter missing_file state/portraits-dry.log)
echo "covers to fetch: ${COVERS:-?} (ceiling $COVERS_MAX); portraits to fetch: ${PORTRAITS:-?} (ceiling $PORTRAITS_MAX)"
if [ -z "$COVERS" ] || [ -z "$PORTRAITS" ]; then
  echo "FATAL: could not read the dry-run counts"; cat state/covers-dry.log state/portraits-dry.log; exit 1
fi
if [ "$COVERS" -gt "$COVERS_MAX" ] || [ "$PORTRAITS" -gt "$PORTRAITS_MAX" ]; then
  echo "FATAL: fetch list over its ceiling — inspect state/*.files before raising it"; exit 1
fi

# 2. Fetch exactly those files.
cat state/covers.files state/portraits.files | sort -u > state/fetch.files
if [ -s state/fetch.files ]; then
  rc=0
  timeout 3h rsync -a --contimeout=60 --timeout=300 --files-from=state/fetch.files "$VNDB_RSYNC" "$MIRROR/" || rc=$?
  # A listed image VNDB has since removed ends the transfer with 23 (the
  # 2026-09-17 probe: 6 of 6,963 portraits). The tools count those as missing.
  case "$rc" in
    0|23|24) ;;
    *) echo "FATAL: rsync exited $rc"; exit 1 ;;
  esac
fi
echo "fetched $(find "$MIRROR" -type f | wc -l) of $(wc -l < state/fetch.files) files"

# 3. Upload. The two lanes are independent; either failing fails the run.
run sh -c "$DSNSH"'; backfill-vndb-covers --dsn "$CAT" --from-dump --image-dir /w/mirror/vndb --mirror-only --workers 2 --upload-gap 200ms --apply' \
  || { echo "WARN: covers upload failed"; FAIL=1; }
run sh -c "$DSNSH"'; backfill-character-portraits --dsn "$CAT" --vndb-image-dir /w/mirror/vndb --upload-gap 100ms --apply' \
  || { echo "WARN: portraits upload failed"; FAIL=1; }

# 4. The bytes live in the image service now. A failed run keeps its copy for
#    the operator; the next run starts clean either way.
if [ "$FAIL" -eq 0 ]; then
  rm -rf "$MIRROR"
fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== image-mirror had a failed upload ==="; exit 1; }
echo "=== image-mirror done $(date -u '+%F %T')Z ==="
