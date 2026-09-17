#!/bin/sh
# Monthly report on the tag vocabulary the catalog has NOT judged yet — the
# trigger for the next tag-canon wave (wave 208 residual ③).
#
# WHY a report and not a propose: propose is an LLM pass whose output is only
# useful once a human reviews it, and a shared gateway quota is a production
# dependency (a batch job that empties it takes the live moderation gate down
# with it). So the cron does the free half — counting the unjudged names that
# already survive the junk gate, the mapped set and catalog_tag_rejection — and
# mails the busiest ones. The wave itself stays a human decision:
#
#   tag-canon-pair -mode propose --dsn "$CAT" --prior <last wave's decisions>
#   tag-canon-pair -mode review  --in … --md … --decisions …
#   tag-canon-pair -mode apply   --decisions … --apply
#
# --prior is what keeps a monthly cadence cheap: a name judged in an earlier
# wave is never classified again, so each pass only pays for what is new.
#
# Runs from the infra-tools image resolved below; nothing is staged on this
# host. Canonical copy: scripts/prod-cron/tag-vocab-backlog/run.sh in
# nextmoe-infra — edit there and redeploy.
set -eu
BASE=/root/tag-vocab-backlog
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }

# Floor a name must reach to count as "worth a wave", and how many such names
# it takes before this is worth an email. Both are judgment, not physics: 20 is
# the bangumi admission gate wave 208 settled on, and a handful of new names a
# month is normal churn, not a backlog.
FLOOR=${FLOOR:-20}
ALERT_AT=${ALERT_AT:-25}

on_exit() {
  rc=$?
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] catalog tag-vocab backlog (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== tag-vocab backlog start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp

# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; CAT="host=127.0.0.1 port=5432 user=$U dbname=${KUN_CATALOG_PG_DATABASE:-kun_catalog} sslmode=disable"'

REPORT="$BASE/state/backlog-$(date -u +%F).txt"
docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" "$IMG" \
  sh -c "$DSNSH"'; tag-canon-pair -mode backlog --dsn "$CAT" --backlog-floor '"$FLOOR" \
  | tee "$REPORT"

# "above_floor(20)=57" — the count of names busy enough to be worth judging.
N=$(sed -n 's/.*above_floor([0-9]*)=\([0-9]*\).*/\1/p' "$REPORT" | head -1)
: "${N:=0}"
echo "unjudged names above floor $FLOOR: $N (alert at $ALERT_AT)"
if [ "$N" -ge "$ALERT_AT" ]; then
  /root/lib/alert.sh "[tag vocab] $N unjudged tag names above usage $FLOOR — a wave is due" "$REPORT" \
    || echo "alert delivery failed"
fi

find state -name 'backlog-*.txt' -mtime +400 -delete
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +400 -delete
echo "=== tag-vocab backlog done $(date -u '+%F %T')Z ==="
