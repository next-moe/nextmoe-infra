#!/bin/sh
# Nightly LLM adjudication lane: judge the review queues, apply the verdicts,
# and carry the resulting merge proposals all the way to executed.
#
# It exists because the judge had no schedule. src_llm.run showed queue-refs had
# been started exactly once ever (2026-09-03, --limit 100, against 35,680
# probable refs) and every apply was hand-started, which is why three human
# review piles grew without bound while the machinery to drain them was already
# written and passing its tests.
#
# The merge half is four steps, not two, and the last two are the reason this
# file exists: llm-suggest's `accept` only files an OPEN proposal. Approval and
# execution are separate transitions, so a lane that stops after apply produces
# proposals nobody executes — 965 of them had accumulated by 2026-09-14.
#
# Two outcomes, as in work-dedup-nightly: a lane that broke exits non-zero and
# the trap sends [FAIL] with no success stamp; a lane that ran but found the
# proposal backlog outgrowing its -limit sends [ADJ] inline and still finishes
# 0, because the run was healthy and the backlog is the finding — suppressing
# the stamp there would make the deadman report a working job as dead.
#
# crontab (root): 0 21 * * * /root/llm-adjudicate-nightly/run.sh
# 21:00 CST = 13:00 UTC, after work-dedup-nightly (18:30 CST) has finished
# seeding the night's new candidate pairs, so this lane judges them the same
# night instead of a day late.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/llm-adjudicate-nightly/run.sh in
# nextmoe-infra — installing or updating it on the box is a manual scp over
# /root/llm-adjudicate-nightly/run.sh.
set -eu
BASE=/root/llm-adjudicate-nightly
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

# The trap catches a run that failed; state/last-success lets
# /root/lib/watchdog.sh catch a job that stopped running at all — the failure no
# in-script handler can see. The stamp is gated on LOCKED so a run that skipped
# because a previous one still holds the lock never stamps for work it did not do.
on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] llm adjudicate nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== llm adjudicate nightly start $(date -u '+%F %T')Z ==="

CATC=kun-visual-novel-infra-vqvqbc-catalog-1
NOTE='llm:queue-adjudicator'
APPROVE_LIMIT=200

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# Fresh env snapshot from the live catalog container; the EXIT trap shreds it.
# Both tools read KUN_CATALOG_PG_* straight out of the environment, so no DSN is
# ever assembled on a host command line and no password reaches argv.
# /root/env-llm-key.env carries only the gateway credential and is load-bearing
# for other jobs — it is never shredded here.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" | grep -v '^$' > env.tmp
chmod 600 env.tmp

LLM='--llm-base http://ec2.ksm.moe:3000/v1 --model deepseek-v4-flash'

# The log is per-day and appended to, so a second run on the same day would
# otherwise read the FIRST run's counters. Everything past this offset is ours.
MARK=$(wc -c < "$LOG")

echo "--- 1/6 judge work pairs ---"
docker run --rm --name adj-judge-workpair --network dokploy-network \
  --env-file "$BASE/env.tmp" --env-file /root/env-llm-key.env "$IMG" \
  sh -c "exec llm-suggest $LLM --apply --task queue-workpair"

echo "--- 2/6 apply work-pair verdicts (files open proposals) ---"
docker run --rm --name adj-apply-workpair --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec llm-suggest --mode apply --queue workpair --actor 1 --min-confidence 0.9 --apply'

# Screens the RESOLVED endpoints for an exact-ref contradiction from an
# independent registry before approving; a contradiction only from a
# first-party source is our own duplicate and does not veto the merge.
echo "--- 3/6 approve (capped at $APPROVE_LIMIT) ---"
docker run --rm --name adj-approve --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec work-dedup -mode approve -actor 1 -note '$NOTE' -limit $APPROVE_LIMIT -run"

echo "--- 4/6 execute ---"
docker run --rm --name adj-execute --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec work-dedup -mode execute -actor 1 -note '$NOTE' -run"

OURS=$(tail -c "+$((MARK + 1))" "$LOG")

# open=N with N at the cap means more proposals were waiting than this run was
# allowed to approve. The lane itself is healthy, so this alerts and carries on.
if echo "$OURS" | grep -q "\[approve\].*open=$APPROVE_LIMIT "; then
  echo "=== approve hit its -limit; the proposal backlog is growing ==="
  /root/lib/alert.sh "[ADJ] llm adjudicate backlog" "$BASE/$LOG" || echo "alert delivery failed"
fi

# A merge that executed tonight left the search indexes pointing at a
# soft-deleted work, so hand the reindex its trigger. It owns its own flock,
# alerting and success stamp, so a failure here is logged and not re-alerted.
if echo "$OURS" | grep -q '\[execute\].*executed=[1-9]'; then
  echo "--- 5/6 merges executed - triggering catalog reindex ---"
  /root/reindex-catalog/run.sh || echo "reindex-catalog exited $? (its own alerting covers this)"
else
  echo "--- 5/6 no merges executed - skipping reindex ---"
fi

# Refs last: it is the long lane, and unlike the work-pair lane it can only ever
# confirm (planRef has no reject path), so nothing downstream waits on it.
# --families llm only: the chain family needs the erogamescape staging DSN.
echo "--- 6/6 judge and confirm probable refs ---"
docker run --rm --name adj-judge-refs --network dokploy-network \
  --env-file "$BASE/env.tmp" --env-file /root/env-llm-key.env "$IMG" \
  sh -c "exec llm-suggest $LLM --apply --task queue-refs --families llm"
docker run --rm --name adj-apply-refs --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec llm-suggest --mode apply --queue ref --actor 1 --min-confidence 0.9 --apply'

find logs -name 'run-*.log' ! -name "run-$(date +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== llm adjudicate nightly done $(date -u '+%F %T')Z ==="
