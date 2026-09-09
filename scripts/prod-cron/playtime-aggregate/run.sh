#!/bin/sh
# Nightly nextmoe-source aggregates (catalog). Two projections from first-party
# data onto catalog faces the read path already renders by source_id:
#   1. playtime median — folds the per-user reports that downstream Galgame
#      managers write to /v2/me/playtimes (catalog_user_playtime; /v1/playtime
#      was retired 2026-08-27) into catalog_work_playtime, source `nextmoe`.
#   2. forum rating mean — folds kungalgame.galgame_rating into
#      catalog_work_rating, source `nextmoe` (≥3 voters, live kungal claim).
#
# Why these must be jobs and not write-time triggers: the published number is
# an aggregate over a population, so it changes when the POPULATION changes,
# not only when one person reports. A work sitting at two finishers/raters
# publishes nothing; the third makes it eligible, and a report withdrawn below
# the threshold makes it ineligible again — both tools DELETE in that
# direction too. Recomputing the whole eligible set daily is the only cheap
# way to keep those two directions symmetric.
#
# Safe to re-run at any time: writes are change-detected upserts, so a second
# pass on an unchanged corpus writes nothing (unchanged=N, written=0).
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/playtime-aggregate/run.sh in
# nextmoe-infra — edit there and redeploy.
set -eu
BASE=/root/playtime-aggregate
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }

# Report the outcome. A failing run alerts immediately; a succeeding run stamps
# state/last-success, which /root/lib/watchdog.sh reads to notice a job that has
# silently stopped running at all — the failure mode a trap inside this script
# can never see, because it looks exactly like silence. An alert that cannot be
# delivered must not fail the run itself.
on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] playtime aggregation (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== playtime aggregation start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# Fresh env snapshot from the catalog container: secrets never reach a command
# line, and the EXIT trap shreds the file even on failure.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp

run() {
  docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" "$IMG" "$@"
}
# The DSN is assembled INSIDE the container from the env snapshot, so the
# password exists only in that process — never in argv on this host.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; P="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; CAT="host=127.0.0.1 port=5432 user=$U password=$P dbname=kun_catalog sslmode=disable"; FORUM="host=127.0.0.1 port=5432 user=$U password=$P dbname=kungalgame sslmode=disable"'

# A dry pass first, and a ceiling on it. The threshold means a work can only
# appear once at least three different people have finished it, so organic
# daily movement is small. A four-digit eligible set on a routine day means
# something other than people playing games moved the numbers — a client
# replaying its whole history, or a bulk import wearing a user's identity —
# and the median of that is not a fact about how long the game takes.
CEILING=2000
run sh -c "$DSNSH"'; aggregate-user-playtime --dsn "$CAT"' > state/dry.log 2>&1 || {
  echo "FATAL: dry run failed"; cat state/dry.log; exit 1; }
cat state/dry.log
ELIGIBLE=$(sed -n 's/.*eligible=\([0-9]*\).*/\1/p' state/dry.log | tail -1)
[ -n "$ELIGIBLE" ] || { echo "FATAL: could not read eligible= from the dry run"; exit 1; }
[ "$ELIGIBLE" -le "$CEILING" ] || {
  echo "FATAL: $ELIGIBLE works eligible exceeds the ceiling $CEILING — inspect"
  echo "       catalog_user_playtime for a client reporting in bulk before"
  echo "       letting this publish."
  exit 1; }

# Apply. Deletions are part of the contract here: a work that fell back under
# the reporter threshold loses its nextmoe row rather than keeping a stale one.
run sh -c "$DSNSH"'; aggregate-user-playtime --dsn "$CAT" --apply'

# Forum rating mean onto catalog_work_rating / source nextmoe. Same dry-then-
# ceiling-then-apply shape: organic daily movement is small, and a four-digit
# eligible set on a routine day means something other than people rating games
# moved the numbers.
run sh -c "$DSNSH"'; aggregate-forum-ratings --dsn "$CAT" --forum-dsn "$FORUM"' > state/ratings-dry.log 2>&1 || {
  echo "FATAL: ratings dry run failed"; cat state/ratings-dry.log; exit 1; }
cat state/ratings-dry.log
RATINGS_ELIGIBLE=$(sed -n 's/.*eligible=\([0-9]*\).*/\1/p' state/ratings-dry.log | tail -1)
[ -n "$RATINGS_ELIGIBLE" ] || { echo "FATAL: could not read eligible= from the ratings dry run"; exit 1; }
[ "$RATINGS_ELIGIBLE" -le "$CEILING" ] || {
  echo "FATAL: $RATINGS_ELIGIBLE works eligible exceeds the ceiling $CEILING — inspect"
  echo "       kungalgame.galgame_rating for a bulk import before"
  echo "       letting this publish."
  exit 1; }

run sh -c "$DSNSH"'; aggregate-forum-ratings --dsn "$CAT" --forum-dsn "$FORUM" --apply'

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== playtime aggregation done $(date -u '+%F %T')Z ==="
