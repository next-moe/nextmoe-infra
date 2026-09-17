#!/bin/sh
# shellcheck disable=SC2016
# Nightly cross-source character fold (catalog-char-xsrc). Canonical copy:
# scripts/prod-cron/char-xsrc-nightly/run.sh in nextmoe-infra; the box copy is
# /root/char-xsrc-nightly/run.sh and must stay byte-identical.
#
# A Bangumi or EG character is its own row until this fold merges it into the
# VNDB character it duplicates. The fold was run by hand once (2026-08-06, wave
# 177) and never scheduled, so by 2026-09-17 1,982 co-resident pairs had never
# been judged, and the roster lanes in source-import had to be fenced off every
# work another source already cast (11,375 same-work twins on 2026-09-16).
#
# Pipeline, all idempotent: build pairs → judge round one → build the
# three-vote panel for the uncertain tail → judge the panel → one worklist →
# propose+approve (48h cooling) → execute what cooled. Verdicts accumulate in
# state/ and a judged key is never asked again, so a quiet night costs no
# model calls.
#
# crontab (root): 30 22 * * * /root/char-xsrc-nightly/run.sh
# 22:30 CST, after llm-adjudicate-nightly (21:00) — the gateway throttles by
# concurrency, so this waits for that job to release its lock first.
set -eu
BASE=${XSRC_BASE:-/root/char-xsrc-nightly}
ALERT_SH=${XSRC_ALERT:-/root/lib/alert.sh}
ADJ_LOCK=${XSRC_ADJ_LOCK:-/root/llm-adjudicate-nightly/.lock}
MT_LOCK=${XSRC_MT_LOCK:-/root/intromt-nightly/.lock}
REINDEX_SH=${XSRC_REINDEX:-/root/reindex-catalog/run.sh}
LLM_ENV=${XSRC_LLM_ENV:-/root/env-llm-key.env}
cd "$BASE"
mkdir -p logs state work
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1

LOCKED=0
FAIL=0
on_exit() {
  rc=$?
  if [ "${LOCKED:-0}" = 1 ] && [ -f env.tmp ]; then shred -u env.tmp; fi
  [ "$rc" -eq 0 ] && [ "${FAIL:-0}" -ne 0 ] && rc=1
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] char-xsrc nightly (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== char-xsrc nightly start $(date -u '+%F %T')Z ==="

echo "waiting on llm-adjudicate lock $ADJ_LOCK (up to 10800s)"
if ! flock -w 10800 "$ADJ_LOCK" true; then
  echo "FATAL: llm-adjudicate still holds $ADJ_LOCK; refusing to share the gateway with it"
  exit 1
fi

exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

CATC=kun-visual-novel-infra-vqvqbc-catalog-1
NOTE='rule:catalog-dedup char-xsrc-nightly'
# Per-night bounds. A judgement takes 3-9s on the gateway, so 600 fits in a
# round's deadline; the deadline is what ends a round whose calls hang.
# PROPOSE_MAX is a tripwire, not a quota: a quiet week proposes a handful, and
# a night past it means a lane minted a batch of twins — the proposals are
# withheld and the run alerts.
JUDGE_LIMIT=${XSRC_JUDGE_LIMIT:-600}
JUDGE_DEADLINE=${XSRC_JUDGE_DEADLINE:-100m}
PROPOSE_MAX=${XSRC_PROPOSE_MAX:-200}

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" | grep -v '^$' > env.tmp
chmod 600 env.tmp

# --user 0:0: the image defaults to appuser (10001), which cannot write the
# root-owned state/ and work/ under the /w mount.
catalog() {
  docker run --rm --network dokploy-network --env-file "$BASE/env.tmp" \
    -v "$BASE:/w" --user 0:0 "$IMG" "$@"
}

# The judge gets the gateway key and nothing from the catalog container. Its
# token is read inside the container, so the key never reaches a host argv.
# catalog-adjudicate exits 1 when any single call failed, and a few upstream
# 429s are the steady state on this gateway; a failed key has no verdict line
# and is retried on the next run. Exit 1 is also "could not start", so the
# tolerance applies only to a run that printed its own done line.
judge() {
  _label=$1
  _mark=$(wc -c < "$LOG")
  _rc=0
  docker run --rm --name "xsrc-judge-$_label" --network dokploy-network \
    --env-file "$LLM_ENV" -e KUN_AI_UPSTREAM_BASE_URL=http://ec2.ksm.moe:3000/v1 \
    -v "$BASE:/w" --user 0:0 "$IMG" \
    sh -c 'KUN_AI_UPSTREAM_TOKEN="$KUN_LLM_API_KEY" exec catalog-adjudicate "$@"' judge \
    -packets "/w/work/$2" -verdicts "/w/state/$3" -errors "/w/state/$4" \
    -model deepseek-v4-flash -workers 1 -rpm 30 -chunk 1 -limit "$JUDGE_LIMIT" \
    -request-timeout 120s -deadline "$JUDGE_DEADLINE" || _rc=$?
  _counts=$(tail -c "+$((_mark + 1))" "$LOG" \
    | sed -n 's/.*adjudicate done .* judged=\([0-9]*\) .* errors=\([0-9]*\).*/\1 \2/p' | tail -1)
  if [ -z "$_counts" ]; then
    echo "=== judge $_label did not reach its done line (exit $_rc) ==="
    return 1
  fi
  _judged=${_counts% *}
  _errs=${_counts#* }
  [ "$_errs" -eq 0 ] || echo "note: judge $_label left $_errs keys unjudged; the next run retries them"
  if [ "$_errs" -gt "$_judged" ]; then
    "$ALERT_SH" "[XSRC] judge $_label: $_errs failures against $_judged judgements" "$BASE/$LOG" \
      || echo "alert delivery failed"
  fi
  return 0
}

echo "--- 1/7 build round-one pairs ---"
catalog catalog-char-xsrc -mode packets -pairs /w/work/pairs.jsonl -packets /w/work/packets.jsonl

echo "--- 2/7 judge round one ---"
judge r1 packets.jsonl verdicts.jsonl errors.jsonl

echo "--- 3/7 build the panel for the uncertain tail ---"
catalog catalog-char-xsrc -mode panel-packets -pairs /w/work/pairs.jsonl \
  -verdicts /w/state/verdicts.jsonl -pairs2 /w/work/pairs2.jsonl -packets2 /w/work/packets2.jsonl

echo "--- 4/7 judge the panel ---"
judge panel packets2.jsonl verdicts2.jsonl errors2.jsonl

echo "--- 5/7 emit one worklist ---"
catalog catalog-char-xsrc -mode emit-all -pairs /w/work/pairs.jsonl -verdicts /w/state/verdicts.jsonl \
  -pairs2 /w/work/pairs2.jsonl -verdicts2 /w/state/verdicts2.jsonl \
  -worklist /w/work/worklist.jsonl -residual /w/work/residual.txt

echo "--- 6/7 propose ---"
if [ -s work/worklist.jsonl ]; then
  catalog catalog-dedup-batch -actor 1 -mode propose -worklist /w/work/worklist.jsonl \
    -note "$NOTE" > work/propose-dry.log 2>&1 || { cat work/propose-dry.log; exit 1; }
  cat work/propose-dry.log
  WOULD=$(sed -n 's/.*\[propose\].* proposals=\([0-9]*\) .*/\1/p' work/propose-dry.log | tail -1)
  if [ -z "$WOULD" ]; then
    echo "FATAL: could not read proposals= from the dry run"
    exit 1
  fi
  if [ "$WOULD" -gt "$PROPOSE_MAX" ]; then
    echo "FATAL: $WOULD proposals exceed the nightly ceiling $PROPOSE_MAX; nothing proposed"
    FAIL=1
  elif [ "$WOULD" -gt 0 ]; then
    catalog catalog-dedup-batch -actor 1 -mode propose -worklist /w/work/worklist.jsonl \
      -note "$NOTE" -run
  fi
else
  echo "empty worklist; nothing to propose"
fi

# The merge moves a character's intro rows to the survivor, but entity-intro-mt
# does not recheck liveness when it writes: on 2026-08-06 a translation run
# overlapping the wave-177 merges wrote 7 zh-Hans rows onto retired ids. Its
# lock is held for the length of the execute only.
echo "--- 7/7 execute cooled proposals ---"
if ! flock -w 7200 "$MT_LOCK" \
    docker run --rm --network dokploy-network --env-file "$BASE/env.tmp" "$IMG" \
    catalog-dedup-batch -actor 1 -mode execute -note "$NOTE" -run > work/execute.log 2>&1; then
  cat work/execute.log
  echo "FATAL: execute failed or intromt-nightly held its lock past 7200s"
  exit 1
fi
cat work/execute.log
if grep -q '\[execute\].* executed=[1-9]' work/execute.log; then
  echo "--- merges executed - triggering catalog reindex ---"
  "$REINDEX_SH" || echo "reindex-catalog exited $? (its own alerting covers this)"
fi

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== char-xsrc nightly withheld its proposals ==="; exit 1; }
echo "=== char-xsrc nightly done $(date -u '+%F %T')Z ==="
