#!/bin/sh
# Hourly refresh of catalog_tag_work_count — the precomputed work_count behind
# every canonical-tag chip (wave 201).
#
# WHY this is a job at all: the tag edge is the only taxonomy edge that reaches
# works through a mapping table (catalog_tag_source_map JOIN catalog_work_tag,
# ~1.2M rows), and counting it live cost 200-400ms on EVERY GET /works/{id} —
# 90% of the catalog service's slow-query log. Adding indexes and rewriting the
# join were each measured at ~1.5x; the work is genuine, so it moved off the
# request path instead. Labels, engines and series are still counted live.
#
# WHY hourly: a full recompute is ~0.5s, so frequency is free, and the numbers
# only move when a batch import or a claim wave runs. The read face's promise
# weakens from "exact" to "exact as of computed_at" — this cadence is what keeps
# that sentence honest.
#
# Safe to re-run at any time: it is a full recompute in one transaction (batched
# upsert + delete of everything the pass did not produce), so a second run on an
# unchanged corpus only moves computed_at. If the table is EMPTY the service
# falls back to the live aggregate, so a failed run degrades to "slow", never to
# "wrong" — but it stays slow until this succeeds again.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/refresh-tag-counts/run.sh in
# nextmoe-infra — edit there and redeploy.
set -eu
BASE=/root/refresh-tag-counts
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }

# Same two-layer reporting as the other maintenance crons: the trap catches a
# run that failed, and state/last-success lets /root/lib/watchdog.sh catch a job
# that stopped running at all — which looks exactly like success to any handler
# living inside this script.
on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] catalog tag-count refresh (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== tag-count refresh start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# Fresh env snapshot from the catalog container; the EXIT trap shreds it.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp

# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; CAT="host=127.0.0.1 port=5432 user=$U dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog} sslmode=disable"'

docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "$DSNSH"'; refresh-tag-counts --dsn "$CAT"'

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== tag-count refresh done $(date -u '+%F %T')Z ==="
