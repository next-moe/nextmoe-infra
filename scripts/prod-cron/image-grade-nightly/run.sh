#!/bin/sh
# image grade nightly: push the per-image grades into the catalog media rows.
#
# Grading itself does not run here. Since 2026-10-11 new images are scored off
# this host by scripts/image-grade-local (the infra-wdtagger image on a
# workstation) and written back through apply.sh beside this file, which also
# stamps state/last-tag for the watchdog. On 2026-10-11 the tagger ran on this
# host through the evening peak (1-minute load 9-10 on eight cores) and the
# owner ruled that compute stays off it.
#
# What is left is sync-image-grades: it refines catalog_work_screenshot /
# catalog_work_cover `sexual` for every machine-ingested source, replacing the
# work-level stamp the importers write on insert. apply.sh runs it too, right
# after each write; this daily pass is the safety net. Idempotent: an idle
# night writes nothing.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. The image service env lives in /root/imgsafety/env.img. Canonical copy:
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
# job that stopped running at all.
FAIL=0
on_exit() {
  rc=$?
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

PGHOST_C=kun-visual-novel-infra-vqvqbc-postgres-1
# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
# The PG credentials are shared across the platform databases; only the
# dbname differs.
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; B="host='"$PGHOST_C"' port=5432 user=$KUN_PG_USER sslmode=disable"; IMGDSN="$B dbname=${KUN_IMAGES_PG_DATABASE:-kun_images}"; CATDSN="$B dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog}"'

# Propagate per-image grades into the catalog media rows. Ungraded and
# dangling hashes are counted, not errors; the run fails only on write errors.
docker run --rm --network dokploy-network \
  --env-file "$ENVDIR/env.img" \
  "$IMG" sh -c "$DSNSH"'; exec sync-image-grades --dsn "$CATDSN" --images-dsn "$IMGDSN" --limit 0 --apply' \
  || { echo "WARN: sync-image-grades exited $?"; FAIL=1; }

# Compress finished logs and purge old.
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== image grade nightly had failed step(s) ==="; exit 1; }
echo "=== image grade nightly done $(date -u '+%F %T')Z ==="
