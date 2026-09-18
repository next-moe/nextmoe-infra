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
# The merge half is five steps, not two, and approve/execute are the reason this
# file exists: llm-suggest's `accept` only files an OPEN proposal. Approval and
# execution are separate transitions, so a lane that stops after apply produces
# proposals nobody executes — 965 of them had accumulated by 2026-09-14. Release
# then lets go of any quarantined work nothing still holds.
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
BASE=${ADJ_BASE:-/root/llm-adjudicate-nightly}
ALERT_SH=${ADJ_ALERT:-/root/lib/alert.sh}
REINDEX_SH=${ADJ_REINDEX:-/root/reindex-catalog/run.sh}
LLM_ENV=${ADJ_LLM_ENV:-/root/env-llm-key.env}
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
    "$ALERT_SH" "[FAIL] llm adjudicate nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== llm adjudicate nightly start $(date -u '+%F %T')Z ==="

CATC=kun-visual-novel-infra-vqvqbc-catalog-1
NOTE='llm:queue-adjudicator'
APPROVE_LIMIT=200
CREDITNAME_LIMIT=300
CREDITNAME_ACCEPT=0.9

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

# --concurrency 2, not the default 4. Measured 2026-09-14 on the ref lane at 4:
# judged=1426 errors=852, every failure an upstream 429. The retry loop already
# paces five attempts from 500ms to 8s with jitter, which cut the earlier wave's
# failure rate from 52% to 37% but cannot outlast a gateway that stays saturated
# longer than its ~15s window - the only lever left is asking for less at once.
# A failure is stored on the verdict row and the next night's run retries it.
LLM='--llm-base http://ec2.ksm.moe:3000/v1 --model deepseek-v4-flash --concurrency 2'

# llm-suggest exits 1 when ANY judgement failed, and a handful of upstream 429s
# is the steady state on that gateway. On 2026-09-14 the ref judge finished
# "queue-refs done judged=836 errors=27" and `set -e` killed the lane on that
# exit code, so step 6's apply -- the entire point of the ref half -- has never
# run on any night, and state/last-success was never written either. A failure
# is stored on the verdict row and retried tomorrow, so a partial judge is not
# a lane failure. Exit 1 is ambiguous though (it is also "could not reach the
# database"), so the tolerance is granted only to a step that printed its own
# "<task> done" line.
judge_step() {
  _task=$1
  shift
  _mark=$(wc -c < "$LOG")
  _rc=0
  "$@" || _rc=$?
  _counts=$(tail -c "+$((_mark + 1))" "$LOG" \
    | sed -n "s/.*$_task done judged=\([0-9]*\) errors=\([0-9]*\).*/\1 \2/p" | tail -1)
  if [ -z "$_counts" ]; then
    echo "=== $_task did not reach its done line (exit $_rc) ==="
    return "$_rc"
  fi
  _judged=${_counts% *}
  _errs=${_counts#* }
  [ "$_errs" -eq 0 ] || echo "note: $_task stored $_errs failed judgements; tomorrow retries them"
  # A dead gateway otherwise looks exactly like a quiet night: every call fails,
  # the lane finishes clean, stamps its success, and the deadman stays quiet.
  if [ "$_errs" -gt "$_judged" ]; then
    "$ALERT_SH" "[ADJ] $_task: $_errs failures against $_judged judgements" "$BASE/$LOG" \
      || echo "alert delivery failed"
  fi
  return 0
}

# The log is per-day and appended to, so a second run on the same day would
# otherwise read the FIRST run's counters. Everything past this offset is ours.
MARK=$(wc -c < "$LOG")

echo "--- 1/10 seed-keys ---"
docker run --rm --name adj-seed-keys --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec work-dedup -mode seed-keys -actor 1 -limit 500 -run'

echo "--- 2/10 judge work pairs ---"
judge_step queue-workpair \
  docker run --rm --name adj-judge-workpair --network dokploy-network \
  --env-file "$BASE/env.tmp" --env-file "$LLM_ENV" "$IMG" \
  sh -c "exec llm-suggest $LLM --apply --task queue-workpair"

# Both bars are spelled out because they are not the same bar. An accept files a
# merge and MergeService.Unmerge has no route and no CLI in production; a reject
# only parks a pair, and the pair stays readable in catalog_match_candidate.
# Holding both to 0.9 is what left 811 judged-different pairs stuck in
# needs_manual with no action that could ever clear them.
echo "--- 3/10 apply work-pair verdicts (files open proposals) ---"
docker run --rm --name adj-apply-workpair --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec llm-suggest --mode apply --queue workpair --actor 1 --min-confidence 0.9 --min-confidence-reject 0.7 --apply'

# Screens the RESOLVED endpoints for an exact-ref contradiction from an
# independent registry before approving; a contradiction only from a
# first-party source is our own duplicate and does not veto the merge.
echo "--- 4/10 approve (capped at $APPROVE_LIMIT) ---"
docker run --rm --name adj-approve --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec work-dedup -mode approve -actor 1 -note '$NOTE' -limit $APPROVE_LIMIT -run"

echo "--- 5/10 execute ---"
docker run --rm --name adj-execute --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec work-dedup -mode execute -actor 1 -note '$NOTE' -run"

echo "--- 6/10 release unheld quarantine ---"
docker run --rm --name adj-release --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec work-dedup -mode release -actor 1 -note '$NOTE' -run"

OURS=$(tail -c "+$((MARK + 1))" "$LOG")

# open=N with N at the cap means more proposals were waiting than this run was
# allowed to approve. The lane itself is healthy, so this alerts and carries on.
if echo "$OURS" | grep -q "\[approve\].*open=$APPROVE_LIMIT "; then
  echo "=== approve hit its -limit; the proposal backlog is growing ==="
  "$ALERT_SH" "[ADJ] llm adjudicate backlog" "$BASE/$LOG" || echo "alert delivery failed"
fi

# A merge that executed tonight left the search indexes pointing at a
# soft-deleted work, so hand the reindex its trigger. It owns its own flock,
# alerting and success stamp, so a failure here is logged and not re-alerted.
if echo "$OURS" | grep -q '\[execute\].*executed=[1-9]'; then
  echo "--- 7/10 merges executed - triggering catalog reindex ---"
  "$REINDEX_SH" || echo "reindex-catalog exited $? (its own alerting covers this)"
else
  echo "--- 7/10 no merges executed - skipping reindex ---"
fi

# An accepted credit-name pair creates or joins a person, and production has no
# unmerge for either, so the judge is capped per night. The apply is not: it
# leaves held and below-bar rows unstamped, and a row cap would fill with them.
echo "--- 8/10 judge credit-name pairs ---"
judge_step queue-creditname \
  docker run --rm --name adj-judge-creditname --network dokploy-network \
  --env-file "$BASE/env.tmp" --env-file "$LLM_ENV" "$IMG" \
  sh -c "exec llm-suggest $LLM --apply --task queue-creditname --limit $CREDITNAME_LIMIT"

echo "--- 9/10 apply credit-name verdicts (links persons) ---"
docker run --rm --name adj-apply-creditname --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "exec llm-suggest --mode apply --queue creditname --actor 1 --min-confidence $CREDITNAME_ACCEPT --min-confidence-reject 0.8 --apply"

# Refs last: it is the long lane. different at --min-confidence-reject 0.8 is
# rejected through RejectRef; below that the row is stamped held_probable_disputed
# and stays probable until the dossier changes.
#
# --families all, not llm: the chain family resolves its evidence with a join
# against the erogamescape database, which lives on this same postgres server,
# so llm-suggest reaches it by swapping the dbname on the catalog credentials
# and it costs no model calls at all. Probed 2026-09-14: eg-steam and eg-dmm
# both return chain-verified, over 3,728 rows that the llm-only lane skipped.
echo "--- 10/10 judge and confirm probable refs ---"
judge_step queue-refs \
  docker run --rm --name adj-judge-refs --network dokploy-network \
  --env-file "$BASE/env.tmp" --env-file "$LLM_ENV" "$IMG" \
  sh -c "exec llm-suggest $LLM --apply --task queue-refs --families all"
docker run --rm --name adj-apply-refs --network dokploy-network \
  --env-file "$BASE/env.tmp" "$IMG" \
  sh -c 'exec llm-suggest --mode apply --queue ref --actor 1 --min-confidence 0.9 --min-confidence-reject 0.8 --apply'

find logs -name 'run-*.log' ! -name "run-$(date +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== llm adjudicate nightly done $(date -u '+%F %T')Z ==="
