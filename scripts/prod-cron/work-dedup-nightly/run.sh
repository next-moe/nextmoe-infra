#!/bin/sh
# Nightly duplicate-work lane. `work-dedup -mode nightly` builds the census ONCE
# and chains seed -> propose -> execute over it; running the three modes back to
# back would build that ~1h ACCESS SHARE self-join three times a night.
#
# Exit-code contract of the tool, which this wrapper depends on:
#   0  the lane ran, nothing new landed in needs_manual
#   3  the lane ran, the seed filed new needs_manual pairs -> [DUP] alert, and
#      STILL a success stamp: the run was healthy and the finding is the alert.
#      Suppressing the stamp would make the deadman report a working job as dead.
#   *  the lane broke  -> [FAIL] alert, no stamp
#
# crontab (root): 30 18 * * * /root/work-dedup-nightly/run.sh
# Cron on this box runs in Asia/Shanghai, so that line fires at 18:30 CST, not
# 18:30 UTC: it was written believing it landed at 02:30 CST, the quiet window.
# The Monday watch's `20 4 * * 1` reads the same way, so it is 04:20 CST and the
# two still never hold a census together. Re-aiming this at the quiet window is
# an operator call, not a cleanup: llm-adjudicate-nightly at 21:00 CST is
# deliberately ordered after this one.
#
# The weekly watch is now the independent check on THIS job: after a healthy
# nightly, watch should find fresh=0.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/work-dedup-nightly/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/work-dedup-nightly/run.sh, so edit here first and copy it out (two
# chains once sat on a stale hand-edited copy for a whole cycle).
set -eu
BASE=/root/work-dedup-nightly
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
  if [ -n "${GUARD_PID:-}" ]; then kill "$GUARD_PID" 2>/dev/null || true; fi
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] work-dedup nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== work-dedup nightly start $(date -u '+%F %T')Z ==="

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
#
# Parallel query is disabled per session: the postgres container runs with the
# 64MB docker-default /dev/shm and the pair detector is a self-join over every
# live work title.
#
# work_mem / hash_mem_multiplier / jit are forced down because parallel-off
# alone was not enough: on 2026-08-29 the first prod census ran under prod's
# work_mem=64MB x hash_mem_multiplier=2 and the planner kept the whole pair
# query in memory — the backend grew to 6.4GB anon RSS, the kernel OOM-killed
# it and postgres went through crash recovery (a ~1.3s full-platform DB
# outage). Dev never showed this because its work_mem=4MB made the identical
# query spill ~5GB to temp files instead. These GUCs pin the disk-spill plan;
# do not remove them to make the run faster.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; CAT="host=127.0.0.1 port=5432 user=$U dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog} sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0 -c work_mem=8MB -c hash_mem_multiplier=1 -c jit=off'"'"'"'

# Yield guard, from the 2026-08-29 lock convoy (same day as the OOM above):
# the census holds an ACCESS SHARE on the work tables for ~1h, a deploy's
# migrate-catalog queued its ACCESS EXCLUSIVE behind it, and catalog-1
# (compose depends_on the migrate) sat in Created — the public catalog face
# was down until a human killed the census. ACCESS SHARE conflicts only with
# ACCESS EXCLUSIVE, so a backend queued behind the census wants a lock that,
# normally, only a migration takes: cancel our own query and retry once after
# it clears. "Normally" is why the blocked backends are now named in the log.
# On 2026-09-14 both attempts yielded - the first to that evening's deploy, the
# second 44 minutes later to something this line recorded as a bare count - and
# a count cannot tell a deploy from an autovacuum truncate, which takes the
# same lock and would mean the retry budget is aimed at the wrong thing.
# The RSS cap backstops the OOM in case an image or planner drift ever un-pins
# the disk-spill plan on an unattended night (no retry there — it would just
# blow up again).
#
# 'WITH lw AS' is the first line of pairQuerySQL in cmd/work-dedup/census.go.
# The guard can only see the census while that prefix holds.
(
  set +eu
  while :; do
    sleep 30
    PID=$(docker exec "$PG" psql -U postgres -d kun_catalog -Atc \
      "select pid from pg_stat_activity where state='active' and query like '%WITH lw AS%' and pid <> pg_backend_pid() limit 1" 2>/dev/null)
    [ -n "$PID" ] || continue
    BLOCKED=$(docker exec "$PG" psql -U postgres -Atc \
      "select count(*) from pg_stat_activity where $PID = any(pg_blocking_pids(pid))" 2>/dev/null)
    RSS=$(docker exec "$PG" sh -c "awk '/VmRSS/{print \$2}' /proc/$PID/status" 2>/dev/null)
    if [ -n "$BLOCKED" ] && [ "$BLOCKED" -gt 0 ] 2>/dev/null; then
      WHO=$(docker exec "$PG" psql -U postgres -Atc \
        "select coalesce(nullif(application_name,''), backend_type)||' '||left(regexp_replace(query,'\\s+',' ','g'),100) from pg_stat_activity where $PID = any(pg_blocking_pids(pid))" 2>/dev/null | tr '\n' ';')
      echo "yield: $BLOCKED backend(s) queued behind census pid $PID - cancelling; blocked=[$WHO]"
      : > state/yielded
      docker exec "$PG" psql -U postgres -Atc "select pg_cancel_backend($PID)" >/dev/null 2>&1
    elif [ -n "$RSS" ] && [ "$RSS" -gt 2500000 ] 2>/dev/null; then
      echo "census backend $PID rss ${RSS}kB over 2500000kB - cancelling"
      docker exec "$PG" psql -U postgres -Atc "select pg_cancel_backend($PID)" >/dev/null 2>&1
    fi
  done
) &
GUARD_PID=$!

# The log is per-day and appended to, so a second run on the same day would
# otherwise see the FIRST run's [execute] line. Everything past this offset is
# this run's own output.
MARK=$(wc -c < "$LOG")

attempt=1
while :; do
  rm -f state/yielded
  rc=0
  docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" "$IMG" \
    sh -c "$DSNSH"'; work-dedup -mode nightly -fail-on-new -actor 1 -note '"'"'rule:work-dedup nightly'"'"' -limit 200 -run -dsn "$CAT"' || rc=$?
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 3 ] && [ -f state/yielded ] && [ "$attempt" -eq 1 ]; then
    echo "census yielded to a migration; retrying once"
    attempt=2
    sleep 60
    continue
  fi
  break
done
kill "$GUARD_PID" 2>/dev/null || true
GUARD_PID=
case "$rc" in
  0) echo "no new needs-manual duplicate pairs" ;;
  3) echo "=== new needs-manual duplicate pairs - sending alert ==="
     /root/lib/alert.sh "[DUP] new needs-manual duplicate pairs" "$BASE/$LOG" || echo "alert delivery failed" ;;
  *) echo "FATAL: work-dedup nightly exited $rc"; exit "$rc" ;;
esac

# A merge that executed tonight left the search indexes pointing at a
# soft-deleted work, so hand the reindex its trigger. It owns its own flock,
# alerting and success stamp, so a failure here is logged and not re-alerted.
if tail -c "+$((MARK + 1))" "$LOG" | grep -q '\[execute\].*executed=[1-9]'; then
  echo "=== merges executed - triggering catalog reindex ==="
  /root/reindex-catalog/run.sh || echo "reindex-catalog exited $? (its own alerting covers this)"
fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== work-dedup nightly done $(date -u '+%F %T')Z ==="
