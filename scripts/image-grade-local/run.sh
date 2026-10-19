#!/bin/sh
# Image grading, off-host half. The tagger (the infra-wdtagger image) must not
# run on the production host, so a workstation with docker and ssh to prod runs
# it: it asks prod for the ungraded images, fetches them from the public CDN,
# scores them here, and streams the scores back to
# /root/image-grade-nightly/io.sh, which writes the grades and syncs them into
# the catalog. Until the next run an ungraded image keeps the work-level stamp
# its importer wrote, so a missed day delays grades and leaks nothing.
#
# Installed as a systemd user timer (image-grade.timer beside this file); see
# the unit for the schedule. A failed run mails through prod's alert.sh, and a
# run that stops happening is caught by prod's watchdog (image-grade-tag).
#
#   IMAGE_GRADE_NEO       ssh alias of the production host (kungal-neo)
#   IMAGE_GRADE_THREADS   tagger threads (12)
#   IMAGE_GRADE_STATE     logs and work files (~/.local/state/nextmoe-image-grade)
set -eu
NEO=${IMAGE_GRADE_NEO:-kungal-neo}
THREADS=${IMAGE_GRADE_THREADS:-12}
STATE=${IMAGE_GRADE_STATE:-$HOME/.local/state/nextmoe-image-grade}
CDN=https://image.kungal.iloveren.link
PORT=18089
TAGGER=nextmoe-image-grade-wdtagger

mkdir -p "$STATE/logs" "$STATE/work"
LOG="$STATE/logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$STATE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
on_exit() {
  rc=$?
  docker rm -f "$TAGGER" >/dev/null 2>&1 || true
  if [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    tail -n 40 "$LOG" | ssh -o BatchMode=yes "$NEO" \
      "sudo /root/lib/alert.sh '[FAIL] image grade off-host ($(uname -n), exit $rc)' -" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== image grade start $(date -u '+%F %T')Z on $(uname -n) ==="

# A Persistent catch-up fires the moment the workstation wakes, before the
# network is back: on 2026-10-17 ssh said "Network is unreachable" and mailed a
# false [FAIL]. A user manager has no network-online.target to order after.
tries=0
until ssh -o BatchMode=yes -o ConnectTimeout=10 "$NEO" true 2>/dev/null; do
  tries=$((tries + 1))
  [ "$tries" -ge 20 ] && break
  sleep 15
done

WORK="$STATE/work/$(date -u +%Y%m%dT%H%M%S)"
mkdir -p "$WORK"
ssh -o BatchMode=yes "$NEO" 'sudo /root/image-grade-nightly/io.sh pending' > "$WORK/pending.csv"
echo "pending: $(wc -l < "$WORK/pending.csv")"

TAGFAIL=0
if [ -s "$WORK/pending.csv" ]; then
  for t in infra-wdtagger infra-tools; do
    docker pull -q "ghcr.io/next-moe/$t:latest" >/dev/null || echo "WARN: $t pull failed; using the local copy"
  done
  WD_IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/next-moe/infra-wdtagger:latest)
  TOOLS_IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/next-moe/infra-tools:latest)
  echo "tagger: $WD_IMG"
  echo "tools: $TOOLS_IMG"
  docker rm -f "$TAGGER" >/dev/null 2>&1 || true
  docker run -d --rm --name "$TAGGER" -p "127.0.0.1:$PORT:8080" -e "WD_THREADS=$THREADS" "$WD_IMG" >/dev/null
  docker run --rm --network host --user "$(id -u):$(id -g)" -v "$WORK:/w" "$TOOLS_IMG" \
    classify-image-safety -mode tag -in /w/pending.csv -out /w/scores.jsonl \
    -base-url "$CDN" -tagger-url "http://127.0.0.1:$PORT" -concurrency 8 \
    || TAGFAIL=1
  docker rm -f "$TAGGER" >/dev/null 2>&1 || true
fi

# Whatever was scored is written even when some images failed; those stay
# ungraded and are listed again next run.
if [ -s "$WORK/scores.jsonl" ]; then
  ssh -o BatchMode=yes "$NEO" 'sudo /root/image-grade-nightly/io.sh apply' < "$WORK/scores.jsonl"
else
  ssh -o BatchMode=yes "$NEO" 'sudo /root/image-grade-nightly/io.sh apply' < /dev/null
fi

find "$STATE/work" -mindepth 1 -maxdepth 1 -type d -mtime +14 -exec rm -rf {} +
find "$STATE/logs" -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find "$STATE/logs" -name 'run-*.log.gz' -mtime +90 -delete
[ "$TAGFAIL" -eq 0 ] || { echo "=== some images failed to tag ==="; exit 1; }
echo "=== image grade done $(date -u '+%F %T')Z ==="
