#!/bin/sh
# shellcheck disable=SC2016
# Weekly reconcile watch. After Sunday's source-import apply, a Monday dry run
# of each reconcile lane should plan nothing beyond what arrived since, and
# the nightly judge should have drained its queues. This job is the independent
# check that both are doing that. READ-ONLY: no --apply, --run or -run.
#
# Exit-code contract, matching work-dedup-watch:
#   0  every lane's dry run was readable; findings over threshold send one
#      [RECON] alert, and STILL a success stamp: the run was healthy and the
#      finding is the alert. Suppressing the stamp would make the deadman
#      report a working job as dead every week (work-dedup-watch's exit 3).
#   *  a dry run broke or a counter could not be read -> [FAIL] alert, no stamp
#
# crontab (root): 30 5 * * 1 /root/reconcile-watch/run.sh
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/reconcile-watch/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/reconcile-watch/run.sh, so edit here first and copy it out.
set -eu
BASE=${RECONCILE_WATCH_BASE:-/root/reconcile-watch}
ALERT_SH=${RECONCILE_WATCH_ALERT:-/root/lib/alert.sh}

LANE_BACKLOG_MAX=${LANE_BACKLOG_MAX:-0}
QUARANTINE_OLDEST_DAYS_MAX=${QUARANTINE_OLDEST_DAYS_MAX:-14}
NEEDS_MANUAL_MAX=${NEEDS_MANUAL_MAX:-0}
PENDING_STALE_MAX=${PENDING_STALE_MAX:-0}
APPROVED_STALE_MAX=${APPROVED_STALE_MAX:-0}

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
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] reconcile watch (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== reconcile watch start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# Fresh env snapshot from the catalog container; the EXIT trap shreds it.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp

run() {
  docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" \
    -e KUN_CATALOG_PG_HOST=127.0.0.1 -e KUN_CATALOG_PG_DATABASE=kun_catalog \
    -v "$BASE:/w" --user 0:0 "$IMG" "$@"
}
# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
#
# Parallel query is disabled per session: the postgres container runs with the
# 64MB docker-default /dev/shm, and parallel hash joins on the big staging
# tables exhaust it (SQLSTATE 53100). Serial plans spill to disk instead —
# slower but bounded. Remove once the compose sets shm_size on postgres.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; B="host=127.0.0.1 port=5432 user=$U sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0'"'"'"; CAT="$B dbname=kun_catalog"; EG="$B dbname=erogamescape"; DL="$B dbname=dlsite"; GC="$B dbname=getchu"'

FINDINGS=""
last_dry_log=""
last_queue_n=""

add_finding() {
  if [ -n "$FINDINGS" ]; then
    FINDINGS="$FINDINGS, $1"
  else
    FINDINGS=$1
  fi
}

read_counter() {
  sed -n "s/.*${1}=\\([0-9][0-9]*\\).*/\\1/p" "$2" | tail -1
}

dry_lane() {
  _dry_lane=$1
  shift
  last_dry_log="state/${_dry_lane}-dry.log"
  if ! run "$@" >"$last_dry_log" 2>&1; then
    echo "FATAL: $_dry_lane dry run failed"
    cat "$last_dry_log"
    exit 1
  fi
}

lane_backlog() {
  _lb_lane=$1
  shift
  _lb_sum=0
  for _lb_c in "$@"; do
    _lb_n=$(read_counter "$_lb_c" "$last_dry_log")
    if [ -z "$_lb_n" ]; then
      echo "FATAL: could not read $_lb_c from $_lb_lane dry run"
      exit 1
    fi
    _lb_sum=$((_lb_sum + _lb_n))
  done
  echo "recon lane=$_lb_lane backlog=$_lb_sum"
  if [ "$_lb_sum" -gt "$LANE_BACKLOG_MAX" ]; then
    add_finding "$_lb_lane"
  fi
}

read_queue() {
  _q_name=$1
  _q_sql=$2
  _q_raw=$(
    printf '%s\n' "$_q_sql" | docker exec -i "$PG" psql -U postgres -d kun_catalog -X -A -t
  ) || {
    echo "FATAL: could not read queue $_q_name"
    exit 1
  }
  last_queue_n=$(printf '%s\n' "$_q_raw" | sed -n 's/^[[:space:]]*\([0-9][0-9]*\)[[:space:]]*$/\1/p' | tail -1)
  if [ -z "$last_queue_n" ]; then
    echo "FATAL: could not parse queue $_q_name (got ${_q_raw:-empty})"
    exit 1
  fi
  echo "recon queue=$_q_name value=$last_queue_n"
}

dry_lane eg-anchors sh -c "$DSNSH"'; reconcile-eg-anchors --dsn "$CAT" --eg-dsn "$EG"'
lane_backlog eg-anchors exact_planned probable_planned related_planned

dry_lane eg-works sh -c "$DSNSH"'; reconcile-eg-works --dsn "$CAT" --eg-dsn "$EG"'
lane_backlog eg-works attached quarantined minted_live limited

dry_lane eg-dlsite import-eg-dlsite-releases
lane_backlog eg-dlsite minted quarantined

dry_lane dlsite-games sh -c "$DSNSH"'; import-dlsite-games --dlsite-dsn "$DL"'
lane_backlog dlsite-games edition_groups declared_groups title_attached_groups minted_groups limited_groups

dry_lane bgm-type4 sh -c "$DSNSH"'; expand-bgm-type4-gated --dsn "$CAT"'
lane_backlog bgm-type4 to_create

dry_lane getchu sh -c "$DSNSH"'; reconcile-getchu --dsn "$CAT" --getchu-dsn "$GC" --eg-dsn "$EG"'
lane_backlog getchu attached minted_live minted_quarantined

dry_lane release-dates sh -c "$DSNSH"'; backfill-release-meta --dsn "$CAT" --dlsite-dsn "$DL" --eg-dsn "$EG" --getchu-dsn "$GC"'
lane_backlog release-dates all_filled all_moved all_cleared

read_queue quarantined_works \
  "SELECT count(*) FROM catalog_work WHERE status = 3 AND deleted_at IS NULL"

read_queue quarantine_oldest_days \
  "SELECT COALESCE(floor(extract(epoch FROM now() - min(created_at)) / 86400)::int, 0) FROM catalog_work WHERE status = 3 AND deleted_at IS NULL"
if [ "$last_queue_n" -gt "$QUARANTINE_OLDEST_DAYS_MAX" ]; then
  add_finding quarantine_oldest_days
fi

read_queue needs_manual \
  "SELECT count(*) FROM catalog_match_candidate WHERE entity_type = 5 AND status = 4"
if [ "$last_queue_n" -gt "$NEEDS_MANUAL_MAX" ]; then
  add_finding needs_manual
fi

read_queue pending_stale \
  "SELECT count(*) FROM catalog_match_candidate WHERE entity_type = 5 AND status = 0 AND created_at < now() - interval '3 days'"
if [ "$last_queue_n" -gt "$PENDING_STALE_MAX" ]; then
  add_finding pending_stale
fi

read_queue approved_stale \
  "SELECT count(*) FROM catalog_merge_proposal WHERE status = 1 AND COALESCE(execute_after, proposed_at) < now() - interval '3 days'"
if [ "$last_queue_n" -gt "$APPROVED_STALE_MAX" ]; then
  add_finding approved_stale
fi

if [ -n "$FINDINGS" ]; then
  echo "recon verdict=not-converged"
  echo "=== not converged - sending alert ==="
  "$ALERT_SH" "[RECON] not converged: $FINDINGS" "$BASE/$LOG" || echo "alert delivery failed"
else
  echo "recon verdict=converged"
fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== reconcile watch done $(date -u '+%F %T')Z ==="
