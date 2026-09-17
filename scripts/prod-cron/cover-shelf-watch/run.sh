#!/bin/sh
# Daily cover-shelf audit. Until 2026-09-14 this job was the only thing keeping
# a work off the SFW shelf when it owned no safe cover to elect: the election
# fell through to the blurred 'censored' stand-in, so every viewer — in BOTH
# modes — got the ghost instead of the real cover. Production carried 961 of
# them on 2026-09-13 and four more had arrived by 2026-09-14, before this cron
# had run once on schedule. The write that produced them was the CLAIM: an
# unclaimed work takes its shelf from content_rating, and claiming it swaps that
# for display_nsfw, which defaults to false. No revision, no cover write.
#
# That state is now unreachable. catalog_work.cover_art_all_explicit is derived
# from the cover rows by a database trigger and the display axis reads it, so a
# work whose cover art is entirely explicit leaves the SFW shelf the moment its
# last safe row goes — no cron latency, and no writer left to forget.
#
# What is left for a daily run is the pair of things a derived column cannot
# answer for itself:
#
#   drift     the column disagrees with the cover rows, i.e. the trigger did not
#             fire. Mechanical; -fix repairs it in both directions.
#   misrated  every cover the work owns is explicit, yet it is not rated r18.
#             Mis-rated or mis-graded, both editorial — reported, never written,
#             and it is what raises exit 3.
#
# Runs at 17:00 CST, two hours after image-grade-nightly, so a re-grade is seen
# the same day; the 06:10 CST reindex then carries any repair to the search face
# (content_limit is computed only by reindex-catalog, so a repair is live on the
# SQL list faces at once and invisible to search until that runs).
#
# Exit-code contract of the tool, which this wrapper depends on:
#   0  audit ran, nothing left outstanding
#   3  audit ran, findings a human must rule on -> [SHELF] alert, and STILL a
#      success stamp: the run was healthy and the finding is the alert.
#      Suppressing the stamp would make the deadman report a working job dead.
#   *  the audit broke                          -> [FAIL] alert, no stamp
#
# -max-fix is the runaway guard: drift wider than that is a missing trigger, not
# a missed statement, and repairing the column would hide that.
#
# crontab (root): 0 17 * * * /root/cover-shelf-watch/run.sh
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/cover-shelf-watch/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/cover-shelf-watch/run.sh, so edit here first and copy it out.
set -eu
BASE=/root/cover-shelf-watch
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

# Two-layer reporting, as in the other maintenance crons: the trap catches a run
# that failed, and state/last-success lets /root/lib/watchdog.sh catch a job that
# stopped running at all — the failure no in-script handler can see. The stamp is
# gated on LOCKED so a run that skipped because a previous one still holds the
# lock never stamps for work it did not do.
on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if { [ "$rc" -eq 0 ] || [ "$rc" -eq 3 ]; } && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] cover-shelf daily audit (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== cover-shelf audit start $(date -u '+%F %T')Z ==="

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

rc=0
docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "$DSNSH"'; audit-cover-shelf -dsn "$CAT" -fix -fail-on-findings' || rc=$?

case "$rc" in
  0) echo "no findings outstanding" ;;
  3) echo "=== findings need a human - sending alert ==="
     /root/lib/alert.sh "[SHELF] cover-shelf findings need a human" "$BASE/$LOG" || echo "alert delivery failed" ;;
  *) echo "FATAL: cover-shelf audit exited $rc"; exit "$rc" ;;
esac

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== cover-shelf audit done $(date -u '+%F %T')Z ==="
