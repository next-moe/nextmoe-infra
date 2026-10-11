#!/bin/sh
# The production half of image grading. scripts/image-grade-local runs the
# tagger off this host and calls this over ssh, so the only work done here is
# reading and writing the images database:
#
#   io.sh pending                          hash,ext CSV of every ungraded image
#   io.sh apply [--replace-machine] < f    write tag-mode scores, sync them into
#                                          the catalog, stamp state/last-tag
#
# state/last-tag is in /root/lib/watchdog.conf as image-grade-tag, so a
# workstation that stops grading is reported like any job that stops running.
# Canonical copy: scripts/prod-cron/image-grade-nightly/io.sh in nextmoe-infra.
set -eu
BASE=/root/image-grade-nightly
ENVDIR=/root/imgsafety
PGHOST_C=kun-visual-novel-infra-vqvqbc-postgres-1
mkdir -p "$BASE/logs" "$BASE/state" "$BASE/incoming"

case "${1:-}" in
pending)
  exec docker exec "$PGHOST_C" psql -U postgres -d kun_images -X -q -c \
    "\copy (SELECT hash, ext FROM images WHERE review_labels -> 'grade' IS NULL AND deleted_at IS NULL ORDER BY hash) TO STDOUT WITH CSV"
  ;;
apply)
  REPLACE=""
  case "${2:-}" in
  --replace-machine) REPLACE="-replace-machine" ;;
  "") ;;
  *) echo "usage: io.sh apply [--replace-machine] < scores.jsonl" >&2; exit 2 ;;
  esac
  ;;
*)
  echo "usage: io.sh pending | io.sh apply [--replace-machine] < scores.jsonl" >&2
  exit 2
  ;;
esac

IN=$(mktemp "$BASE/incoming/scores-XXXXXX.jsonl")
RUN=$(mktemp "$BASE/incoming/run-XXXXXX.log")
LOG="$BASE/logs/apply-$(date -u +%F).log"
finish() {
  rc=$?
  cat "$RUN" >> "$LOG"
  cat "$RUN"
  rm -f "$IN" "$RUN"
  exit "$rc"
}
trap finish EXIT
cat > "$IN"
chmod 0644 "$IN"
echo "=== apply start $(date -u '+%F %T')Z lines=$(wc -l < "$IN") $REPLACE ===" >> "$RUN"
# The nightly sync holds the same lock; wait for it instead of racing it.
exec 9>"$BASE/.lock"
flock 9

if [ -s "$IN" ]; then
  IMG_TAG=ghcr.io/next-moe/infra-tools:latest
  docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy" >> "$RUN"
  IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
  DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; B="host='"$PGHOST_C"' port=5432 user=$KUN_PG_USER sslmode=disable"; IMGDSN="$B dbname=${KUN_IMAGES_PG_DATABASE:-kun_images}"; CATDSN="$B dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog}"'
  docker run --rm --network dokploy-network --env-file "$ENVDIR/env.img" -v "$IN:/w/scores.jsonl:ro" \
    "$IMG" sh -c "$DSNSH"'; exec classify-image-safety -mode apply -dsn "$IMGDSN" -in /w/scores.jsonl '"$REPLACE"' --apply' \
    >> "$RUN" 2>&1
  docker run --rm --network dokploy-network --env-file "$ENVDIR/env.img" \
    "$IMG" sh -c "$DSNSH"'; exec sync-image-grades --dsn "$CATDSN" --images-dsn "$IMGDSN" --limit 0 --apply' \
    >> "$RUN" 2>&1
fi
date -u '+%F %T' > "$BASE/state/last-tag"
echo "=== apply done $(date -u '+%F %T')Z ===" >> "$RUN"
