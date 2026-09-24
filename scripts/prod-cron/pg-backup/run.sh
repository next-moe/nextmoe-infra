#!/bin/sh
# Nightly logical backups of the databases that hold first-party data.
#
# WHY: until 2026-09-24 prod had no routine backup at all (archive_mode=off, no
# dump cron). Two accidental forum purges on 2026-09-23 were recovered only from
# a dump that happened to exist (G0 window) and from dead tuples.
#
# Same-disk logical dumps: they cover bad writes, purges and broken migrations,
# not the loss of this disk. Off-host copies are a separate, still-open job.
#
# Every database in the cluster must be named in DBS or SKIP. One in neither is
# left undumped and fails the run, so the alert names it: a new service's
# database cannot go without a backup unnoticed. SKIP, and why each entry needs
# no dump here:
#   dlsite, getchu, howlongtobeat  primary copy on the crawler box; crawler-restage
#                                  pulls them back every Sunday
#   erogamescape                   its crawler on this host re-crawls it from
#                                  ErogameScape
#   kun_galgame_wiki_retired_w1    frozen since the wiki's retirement
#   kun_letmoe_staging, postgres   nothing first-party
#
# Retention: 7 daily, plus Sunday dumps for 28 days.
# Restore into a scratch database, never over the live one:
#   docker exec <pg> createdb -U postgres <db>_restore
#   docker exec -i <pg> pg_restore -U postgres -d <db>_restore < dumps/<day>/<db>.dump
#
# crontab (root): 30 2 * * * /root/pg-backup/run.sh
#
# Canonical copy: scripts/prod-cron/pg-backup/run.sh in nextmoe-infra —
# installing or updating it on the box is a manual scp over
# /root/pg-backup/run.sh, so edit here first and copy it out.
set -eu
BASE=${PG_BACKUP_BASE:-/root/pg-backup}
ALERT_SH=${PG_BACKUP_ALERT:-/root/lib/alert.sh}
MIN_FREE_GB=${PG_BACKUP_MIN_FREE_GB:-25}

cd "$BASE"
umask 077
mkdir -p logs state dumps
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }

on_exit() {
  rc=$?
  if [ "$rc" -eq 0 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] pg-backup (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== pg-backup start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
DBS="kungalgame kun_galgame_infra kun_catalog kun_community kun_trust kun_images kungalgame_patch kun_shortlink kun_letmoe kun_news kun_artifacts kungalgame_sticker kun_blog kun_ai"
SKIP="dlsite getchu howlongtobeat erogamescape kun_galgame_wiki_retired_w1 kun_letmoe_staging postgres"

# The dumps share the disk with the database itself; filling it would take
# postgres down with it.
free_gb=$(df -BG --output=avail "$BASE" | tail -1 | tr -dc 0-9)
if [ "$free_gb" -lt "$MIN_FREE_GB" ]; then
  echo "only ${free_gb}G free, need ${MIN_FREE_GB}G; not dumping"
  exit 2
fi

present=" $(docker exec "$PG" psql -U postgres -d postgres -tAc \
  "SELECT string_agg(datname, ' ' ORDER BY datname) FROM pg_database WHERE datallowconn AND NOT datistemplate") "
unlisted=""
for db in $present; do
  case " $DBS $SKIP " in
    *" $db "*) ;;
    *) unlisted="$unlisted $db" ;;
  esac
done
missing=""
for db in $DBS; do
  case "$present" in
    *" $db "*) ;;
    *) missing="$missing $db" ;;
  esac
done

DAY=$(date +%F)
OUT="dumps/$DAY"
rm -rf dumps/*.partial
mkdir -p "$OUT.partial"

docker exec "$PG" pg_dumpall -U postgres --globals-only > "$OUT.partial/globals.sql"
for db in $DBS; do
  case " $missing " in *" $db "*) continue ;; esac
  start=$(date +%s)
  docker exec "$PG" pg_dump -U postgres -Fc -d "$db" > "$OUT.partial/$db.dump"
  docker exec -i "$PG" pg_restore --list < "$OUT.partial/$db.dump" > /dev/null
  echo "$db: $(du -h "$OUT.partial/$db.dump" | cut -f1) in $(( $(date +%s) - start ))s"
done

rm -rf "$OUT"
mv "$OUT.partial" "$OUT"
echo "total today: $(du -sh "$OUT" | cut -f1)"

now=$(date +%s)
for dir in dumps/????-??-??; do
  [ -d "$dir" ] || continue
  d=${dir#dumps/}
  age=$(( (now - $(date -d "$d" +%s)) / 86400 ))
  dow=$(date -d "$d" +%u)
  if [ "$age" -gt 28 ] || { [ "$age" -gt 7 ] && [ "$dow" -ne 7 ]; }; then
    echo "pruning $dir (age ${age}d)"
    rm -rf "$dir"
  fi
done
find logs -name 'run-*.log' -mtime +60 -delete

if [ -n "$unlisted$missing" ]; then
  [ -z "$unlisted" ] || echo "in the cluster but in neither DBS nor SKIP, not dumped:$unlisted"
  [ -z "$missing" ] || echo "in DBS but not in the cluster:$missing"
  exit 3
fi
echo "=== pg-backup done $(date -u '+%F %T')Z, ${free_gb}G was free ==="
