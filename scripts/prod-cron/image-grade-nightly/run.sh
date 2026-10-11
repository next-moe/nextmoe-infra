#!/bin/sh
# image grade nightly: grade the images that arrived since last night, then
# push the per-image grades into the catalog media rows.
#
# Step A (classify-image-safety -mode grade) is naturally incremental — its
# candidate predicate is `review_labels -> 'grade' IS NULL` — so a normal night
# grades a few hundred images and the Monday image-mirror wave ~18,000.
# Step B (sync-image-grades) then refines catalog_work_screenshot /
# catalog_work_cover `sexual` for every machine-ingested source, replacing the
# work-level stamp the importers write on insert. Both steps are idempotent:
# an idle night writes nothing.
#
# The grader is the infra-wdtagger image (docker/wdtagger.Dockerfile), started
# here for the length of step A and removed after. Until 2026-10-11 step A asked
# moondream on Cloudflare Workers AI; the account fell to the free daily
# allocation on 2026-09-18, after which nights graded 0-4,500 images and the
# queue grew every week. The tagger runs on this host's CPU, capped at two cores
# (~2 img/s measured here), and costs nothing per image.
#
# Runs from images resolved to a digest below; nothing is staged on this host.
# The image service env lives in /root/imgsafety/env.img. Canonical copy:
# scripts/prod-cron/image-grade-nightly/run.sh in nextmoe-infra — edit
# there and redeploy.
set -eu
BASE=/root/image-grade-nightly
ENVDIR=/root/imgsafety
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
# Same two-layer reporting as the other maintenance crons: the trap catches a
# run that failed, and state/last-success lets /root/lib/watchdog.sh catch a
# job that stopped running at all. Step B runs even when step A failed (already
# graded images are still worth propagating), but any failed step fails the run.
FAIL=0
TAGGER=image-grade-wdtagger
on_exit() {
  rc=$?
  docker rm -f "$TAGGER" >/dev/null 2>&1 || true
  [ "$rc" -eq 0 ] && [ "$FAIL" -ne 0 ] && rc=1
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] image grade nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== image grade nightly start $(date -u '+%F %T')Z ==="

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"
WD_TAG=ghcr.io/next-moe/infra-wdtagger:latest
docker pull -q "$WD_TAG" >/dev/null 2>&1 || echo "WARN: tagger pull failed; using the local copy"
WD_IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$WD_TAG")
echo "tagger: $WD_IMG"

PGHOST_C=kun-visual-novel-infra-vqvqbc-postgres-1
# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
# The PG credentials are shared across the platform databases; only the
# dbname differs.
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; B="host='"$PGHOST_C"' port=5432 user=$KUN_PG_USER sslmode=disable"; IMGDSN="$B dbname=${KUN_IMAGES_PG_DATABASE:-kun_images}"; CATDSN="$B dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog}"'

# Step A — grade only the ungraded.
echo "--- step A: grade new images ---"
docker rm -f "$TAGGER" >/dev/null 2>&1 || true
docker run -d --rm --name "$TAGGER" --network dokploy-network --cpus 2 --memory 2g \
  -e WD_THREADS=2 "$WD_IMG" >/dev/null
docker run --rm --network dokploy-network \
  --env-file "$ENVDIR/env.img" \
  "$IMG" sh -c "$DSNSH"'; exec classify-image-safety -mode grade -dsn "$IMGDSN" \
    -base-url "$KUN_IMAGE_PUBLIC_BASE_URL" -tagger-url http://'"$TAGGER"':8080 -limit 0 -concurrency 3 --apply' \
  || { echo "WARN: grading exited $?"; FAIL=1; }
docker rm -f "$TAGGER" >/dev/null 2>&1 || true

# Step B — propagate per-image grades into the catalog media rows. Ungraded and
# dangling hashes are counted, not errors; the run fails only on write errors.
echo "--- step B: propagate grades to catalog ---"
docker run --rm --network dokploy-network \
  --env-file "$ENVDIR/env.img" \
  "$IMG" sh -c "$DSNSH"'; exec sync-image-grades --dsn "$CATDSN" --images-dsn "$IMGDSN" --limit 0 --apply' \
  || { echo "WARN: sync-image-grades exited $?"; FAIL=1; }

# Compress finished logs and purge old.
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== image grade nightly had failed step(s) ==="; exit 1; }
echo "=== image grade nightly done $(date -u '+%F %T')Z ==="
