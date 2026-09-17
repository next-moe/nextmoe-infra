#!/bin/sh
# image grade nightly: grade the images that arrived since last night, then
# push the per-image grades into the catalog media rows.
#
# Step A (classify-image-safety -mode grade) is naturally incremental — its
# candidate predicate is `review_labels -> 'grade' IS NULL` — so a normal night
# grades a handful of images in seconds; the 2026-08-13 full-corpus run
# (639k images, 17h25m, $874) is what it will never do again.
# Step B (sync-image-grades) then refines catalog_work_screenshot /
# catalog_work_cover `sexual` for every machine-ingested source, replacing the
# work-level stamp the importers write on insert. Both steps are idempotent:
# an idle night writes nothing.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Image + Cloudflare env live in /root/imgsafety/env.img and env.cf
# (env.cf carries CLOUDFLARE_ACCOUNT_ID / CLOUDFLARE_API_TOKEN, which the tool
# reads straight from the environment). Canonical copy:
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
DSNSH='export PGPASSWORD="$KUN_PG_PASSWORD"; B="host='"$PGHOST_C"' port=5432 user=$KUN_PG_USER sslmode=disable"; IMGDSN="$B dbname=${KUN_IMAGES_PG_DATABASE:-kun_images}"; AIDSN="$B dbname=${KUN_AI_PG_DATABASE:-kun_ai}"; CATDSN="$B dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog}"'

# Step A — grade only the ungraded. -guard-dsn watches the LIVE ai_usage failure
# share: Workers AI limits are account-scoped, and in 2026-08 a batch on this
# shared token drove the production text gate to 58% fail-open. The 10M neuron
# cap (~$110) bounds a surprise image wave; the tool exits non-zero when the
# cap left images ungraded, so a capped night alerts on its own.
echo "--- step A: grade new images ---"
docker run --rm --network dokploy-network \
  --env-file "$ENVDIR/env.img" --env-file "$ENVDIR/env.cf" \
  "$IMG" sh -c "$DSNSH"'; exec classify-image-safety -mode grade -dsn "$IMGDSN" -guard-dsn "$AIDSN" \
    -base-url "$KUN_IMAGE_PUBLIC_BASE_URL" -limit 0 -concurrency 16 -max-neurons 10000000 --apply' \
  || { echo "WARN: grading exited $?"; FAIL=1; }

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
