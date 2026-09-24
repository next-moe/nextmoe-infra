#!/bin/sh
# Nightly: encrypt pg-backup's newest dumps and copy them off this host, to a
# Cloudflare R2 bucket.
#
# WHY: pg-backup's dumps share the disk with the database. They cover a bad
# write, a purge or a broken migration, not the loss of this disk or this host.
#
# Only the age PUBLIC key lives here (age-recipient.txt), so whoever holds this
# host can write backups but read none; the owner keeps the private key offline.
# The R2 token reaches this one bucket only, and the bucket's lock rule keeps
# every object for 30 days, so the token cannot destroy recent backups either.
# A lifecycle rule on the bucket deletes objects after 35 days; this job never
# deletes anything.
#
# What goes up:
#   daily/<run>/   the newest complete dumps/<day>/, once per day
#   weekly/<run>/  dumps/weekly/ (erogamescape, umami), whenever it changed
# Each <run> prefix is new (UTC time of the run): a locked object cannot be
# overwritten, and age encrypts to a fresh key every time, so a retry after a
# half-finished upload has to start a new prefix. A prefix is complete when it
# holds SHA256SUMS, which is uploaded last.
#
# Restore, on the machine holding the private key:
#   rclone copy r2:<bucket>/daily/<run> ./restore && cd restore
#   sha256sum -c SHA256SUMS
#   age -d -i nextmoe-backup.agekey kun_catalog.dump.age > kun_catalog.dump
#   then pg_restore into a scratch database, as pg-backup/run.sh describes.
#
# Inputs, both root-only, set up once by hand (see scripts/prod-cron/README.md):
#   /root/pg-backup/r2.env              R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY
#                                        R2_ENDPOINT R2_BUCKET
#   /root/pg-backup/age-recipient.txt   the age public key, age1...
#
# crontab (root): 30 4 * * * /root/pg-offsite/run.sh
# Cron on this box runs in Asia/Shanghai: 04:30 CST, two hours after pg-backup.
#
# Canonical copy: scripts/prod-cron/pg-offsite/run.sh in nextmoe-infra —
# installing or updating it on the box is a manual scp over
# /root/pg-offsite/run.sh, so edit here first and copy it out.
set -eu
BASE=${PG_OFFSITE_BASE:-/root/pg-offsite}
DUMPS=${PG_OFFSITE_DUMPS:-/root/pg-backup/dumps}
R2_ENV=${PG_OFFSITE_R2_ENV:-/root/pg-backup/r2.env}
RECIPIENT=${PG_OFFSITE_RECIPIENT:-/root/pg-backup/age-recipient.txt}
AGE=${PG_OFFSITE_AGE:-/root/lib/bin/age}
RCLONE_IMG=${PG_OFFSITE_RCLONE_IMG:-rclone/rclone:1.75.1}
ALERT_SH=${PG_OFFSITE_ALERT:-/root/lib/alert.sh}

cd "$BASE"
umask 077
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }

on_exit() {
  rc=$?
  rm -rf "$BASE/stage"
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] pg-offsite (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== pg-offsite start $(date -u '+%F %T')Z ==="

set -a
# shellcheck source=/dev/null
. "$R2_ENV"
set +a
: "${R2_ACCESS_KEY_ID:?}" "${R2_SECRET_ACCESS_KEY:?}" "${R2_ENDPOINT:?}" "${R2_BUCKET:?}"
recipient=$(cat "$RECIPIENT")
case "$recipient" in
  age1*) ;;
  *) echo "$RECIPIENT holds no age public key"; exit 2 ;;
esac

RCLONE_CONFIG_R2_TYPE=s3
RCLONE_CONFIG_R2_PROVIDER=Cloudflare
RCLONE_CONFIG_R2_ACCESS_KEY_ID=$R2_ACCESS_KEY_ID
RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=$R2_SECRET_ACCESS_KEY
RCLONE_CONFIG_R2_ENDPOINT=$R2_ENDPOINT
RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true
export RCLONE_CONFIG_R2_TYPE RCLONE_CONFIG_R2_PROVIDER RCLONE_CONFIG_R2_ACCESS_KEY_ID \
  RCLONE_CONFIG_R2_SECRET_ACCESS_KEY RCLONE_CONFIG_R2_ENDPOINT RCLONE_CONFIG_R2_NO_CHECK_BUCKET

# The credentials reach the container by name (-e VAR), never on an argv.
rclone() {
  docker run --rm --name pg-offsite-rclone -v "$BASE/stage:/stage:ro" \
    -e RCLONE_CONFIG_R2_TYPE -e RCLONE_CONFIG_R2_PROVIDER -e RCLONE_CONFIG_R2_ACCESS_KEY_ID \
    -e RCLONE_CONFIG_R2_SECRET_ACCESS_KEY -e RCLONE_CONFIG_R2_ENDPOINT -e RCLONE_CONFIG_R2_NO_CHECK_BUCKET \
    "$RCLONE_IMG" "$@"
}

RUN=$(date -u +%Y%m%dT%H%M%SZ)

# ship <label> <dir>: encrypt every file in <dir> and upload the set.
ship() {
  label=$1 src=$2
  rm -rf stage
  mkdir stage
  n=0
  for f in "$src"/*; do
    [ -f "$f" ] || continue
    "$AGE" -r "$recipient" -o "stage/${f##*/}.age" "$f"
    n=$((n + 1))
  done
  [ "$n" -gt 0 ] || { echo "$label: $src is empty"; return 2; }
  (cd stage && sha256sum -- *.age > SHA256SUMS.pending)
  dest="r2:$R2_BUCKET/$label/$RUN"
  rclone copy /stage "$dest" --exclude SHA256SUMS.pending
  rclone check /stage "$dest" --one-way --exclude SHA256SUMS.pending
  mv stage/SHA256SUMS.pending stage/SHA256SUMS
  rclone copyto /stage/SHA256SUMS "$dest/SHA256SUMS"
  echo "$label: $n file(s), $(du -sh stage | cut -f1) -> $label/$RUN"
}

day=$(find "$DUMPS" -mindepth 1 -maxdepth 1 -type d -name '????-??-??' | sort | tail -n 1)
[ -n "$day" ] || { echo "no complete dump day in $DUMPS"; exit 2; }
if [ "$(cat state/daily 2>/dev/null)" = "${day##*/}" ]; then
  echo "daily: ${day##*/} already shipped"
else
  ship daily "$day"
  echo "${day##*/}" > state/daily
fi

if [ -d "$DUMPS/weekly" ]; then
  stamp=$(find "$DUMPS/weekly" -maxdepth 1 -type f -name '*.dump' -printf '%T@ %f\n' | sort | md5sum | cut -c1-32)
  if [ "$(cat state/weekly 2>/dev/null)" = "$stamp" ]; then
    echo "weekly: unchanged"
  else
    ship weekly "$DUMPS/weekly"
    echo "$stamp" > state/weekly
  fi
fi
echo "=== pg-offsite done $(date -u '+%F %T')Z ==="
