#!/bin/sh
# Catalog search reindex + wiki registrar freshness — DAILY (canonical-api
# track: weekly cron installed 2026-07-28; flipped to daily 2026-07-30 per the
# aggregation-track alignment, doc 106 §25, window >= 06:00 CST after their
# 03:20 claim registrar and 05:45 stats writer).
#
# reindex-catalog rebuilds the FIVE search indexes (credit_names /
# characters / labels / works / tags) from kun_catalog into OpenSearch
# (the only search engine since 2026-09-02). Upsert-only, so this
# HEALS drift from bulk waves + merges (they skip the write-through hook).
# Safe to re-run any time.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/reindex-catalog/run.sh in
# nextmoe-infra — edit there and redeploy.
set -eu
BASE=/root/reindex-catalog
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
# Same two-layer reporting as the other maintenance crons: the trap catches a
# run that failed, and state/last-success lets /root/lib/watchdog.sh catch a
# job that stopped running at all — which looks exactly like success to any
# handler living inside this script.
on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] catalog daily reindex (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== catalog daily maintenance start $(date -u '+%F %T')Z ==="

CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# Fresh env snapshot from the catalog container (secrets never on command
# lines; file shredded by the EXIT trap). Joining the catalog container's netns
# gives byte-identical connectivity to BOTH postgres and the search engine.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp

docker run --rm --network "container:$CATC" --env-file "$BASE/env.tmp" \
  "$IMG" reindex-catalog --batch=5000

echo "=== catalog daily maintenance done $(date -u '+%F %T')Z ==="
