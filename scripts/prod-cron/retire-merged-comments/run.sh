#!/bin/sh
# Nightly: move the comments threads whose catalog anchor a merge took away to
# the survivor's page. On kungal, moyu and letmoe the gid is the catalog id, so
# the redirect row proves the move; a site whose ids still differed would have
# its threads retired instead.
#
# Runs after BOTH merge lanes rather than hooking either one. work-dedup-nightly
# (18:30 CST) and llm-adjudicate-nightly (21:00 CST) each execute proposals, and
# the admin console can execute one at any hour; a sweep keyed on "a merge ran
# tonight" would miss the third path. The sweep reads its own truth out of
# catalog_redirect, so it converges no matter who merged.
#
# Exit-code contract of the tool:
#   0  nothing was stranded
#   3  threads were moved or retired -> [COMMENTS] alert, and STILL a success
#      stamp: the run was healthy and the change IS the finding. Every change is
#      announced, because a comments thread that quietly disappears is exactly
#      the failure this job exists to stop.
#   *  broke -> [FAIL] alert, no stamp
#
# crontab (root): 30 23 * * * /root/retire-merged-comments/run.sh
# Cron on this box runs in Asia/Shanghai, so that is 23:30 CST.
#
# Canonical copy: scripts/prod-cron/retire-merged-comments/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/retire-merged-comments/run.sh, so edit here first and copy it out.
set -eu
BASE=/root/retire-merged-comments
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if { [ "$rc" -eq 0 ] || [ "$rc" -eq 3 ]; } && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  fi
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 3 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] retire merged comments (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== retire merged comments start $(date -u '+%F %T')Z ==="

CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# One snapshot covers both databases: the catalog container carries
# KUN_PG_USER/PASSWORD/HOST/PORT plus KUN_CATALOG_PG_DATABASE and
# KUN_COMMUNITY_PG_DATABASE, and the tool assembles both DSNs from them inside
# the container. No DSN and no password ever reaches a host command line.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" | grep -v '^$' > env.tmp
chmod 600 env.tmp

MARK=$(wc -c < "$LOG")
rc=0
docker run --rm --name retire-merged-comments --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec retire-merged-comments --sites kungal,moyu,letmoe --limit 200 --apply' || rc=$?

case "$rc" in
  0) echo "nothing stranded" ;;
  3) echo "=== threads moved or retired - sending alert ==="
     tail -c "+$((MARK + 1))" "$LOG" > state/retired-last
     /root/lib/alert.sh "[COMMENTS] comments threads on merged-away works moved or retired" "$BASE/$LOG" \
       || echo "alert delivery failed" ;;
  *) echo "FATAL: retire-merged-comments exited $rc"; exit "$rc" ;;
esac

find logs -name 'run-*.log' ! -name "run-$(date +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== retire merged comments done $(date -u '+%F %T')Z ==="
