#!/bin/sh
# Weekly restage of the Tokyo-hosted crawler databases from nextmoe-crawler.
#
# Three of the four crawlers moved to the Tokyo box (docs/deploy/19-crawler-box.md)
# because DLsite only answers a Japanese egress. Their consumers did not move:
# the catalog import family inside bgm-refresh and vndb-refresh reads `dlsite`,
# `erogamescape` and `howlongtobeat` as LOCAL databases (--dlsite-dsn / --eg-dsn
# / --hltb-dsn) and does bulk joins against them. This job is that bridge.
#
# `erogamescape` is NOT one of the sources here and must not become one: that
# crawler runs on this host (ErogameScape drops the Tokyo range at the IP
# layer), writing straight into the local database the importers read. Adding it
# would restore a stale weekly copy over a live daily one.
#
# WHY WEEKLY when the crawl is daily: both consumers are weekly (bgm Wed 11:00,
# vndb Sun 17:30). Shipping ~17 GB every day to feed a job that reads it once a
# week buys no freshness, it just pays seven times for the same thing. This runs
# Sunday morning, ahead of the larger of the two.
#
# WHY PROD PULLS: the crawler box is the least trusted machine in the estate —
# it exists to be the thing an upstream might block or ban. It holds no
# credential of ours and opens no port for this; the private key is here, and
# the database password it needs is a root-only file on its own side, so no
# secret reaches a command line on either host.
#
# Canonical copy: scripts/prod-cron/crawler-restage/run.sh in nextmoe-infra —
# edit there and redeploy by scp (there is no other sync mechanism).
#
# One-time setup on this host:
#   ssh-keygen -t ed25519 -f /root/.ssh/nextmoe_crawler -N ''
#   # then append the .pub to root@43.230.161.212:~/.ssh/authorized_keys
#   cat >> /root/.ssh/config <<'EOF'
#   Host nextmoe-crawler
#     HostName 43.230.161.212
#     User root
#     IdentityFile /root/.ssh/nextmoe_crawler
#   EOF
# and on the crawler box, /root/crawler-db.env (chmod 600) holding one line:
#   PGPASSWORD=<the crawler-db POSTGRES_PASSWORD>
set -eu
BASE=/root/crawler-restage
cd "$BASE"
mkdir -p logs state dump
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

PG=kun-visual-novel-infra-vqvqbc-postgres-1
REMOTE=${CRAWLER_HOST:-nextmoe-crawler}
# The crawler box's Postgres publishes no host port, and its container name is
# Dokploy-generated and changes on every rebuild. A throwaway container on the
# shared network reaches it by the alias the compose pins instead, and takes the
# password from a file so it never appears in the remote host's `ps`.
psql() { docker exec "$PG" psql -U postgres -d postgres -tAc "$1"; }
count() { docker exec "$PG" psql -U postgres -d "$1" -tAc "SELECT count(*) FROM $2"; }

REMOTE_DUMP='docker run --rm --network dokploy-network --env-file /root/crawler-db.env postgres:18-alpine pg_dump -h crawler-postgres -U postgres -Fc -Z3'

# Report the outcome. A failing run alerts immediately; a succeeding run stamps
# state/last-success, which is what /root/lib/watchdog.sh reads to notice a job
# that has silently stopped running at all — the failure a trap inside this
# script can never see, because it looks exactly like silence.
#
# The stamp is gated on LOCKED, set only after the flock above succeeds: a run
# that skipped because a PREVIOUS one is still holding the lock did no work, and
# stamping for it would turn a permanently hung run into a silent weekly no-op
# the watchdog reads as healthy.
on_exit() {
  rc=$?
  rm -f dump/*.dump
  # A run that died mid-source leaves a half-restored <db>_next behind. Next
  # week's run drops it before restoring, but dlsite's is 13 GB and this host
  # has ~70 GB free — a week is too long to hold that for nothing.
  for d in dlsite getchu howlongtobeat; do
    psql "DROP DATABASE IF EXISTS ${d}_next" >/dev/null 2>&1 || true
  done
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] crawler restage (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== crawler restage start $(date -u '+%F %T')Z ==="

# Each entry is <database>:<table whose row count is the health of the copy>.
SRCS="dlsite:works getchu:items howlongtobeat:games"

for src in $SRCS; do
  db=${src%%:*}
  tbl=${src##*:}
  echo "--- $db ---"

  # 1. Pull. -Fc so the restore can run in parallel later if it ever needs to,
  #    and so a truncated transfer fails the restore rather than loading half a
  #    table.
  ssh -o BatchMode=yes "$REMOTE" "$REMOTE_DUMP -d $db" > "dump/$db.dump"
  SIZE=$(stat -c%s "dump/$db.dump")

  # 2. Size floor, calibrated against the last good pull rather than a number
  #    invented here: these databases grow, and the only honest statement about
  #    "big enough" is "not much smaller than last week". The first run has no
  #    baseline and only records one.
  if [ -f "state/$db.size" ]; then
    LAST=$(cat "state/$db.size")
    FLOOR=$((LAST * 70 / 100))
    [ "$SIZE" -ge "$FLOOR" ] || { echo "FATAL: $db dump $SIZE < 70% of last good $LAST"; exit 1; }
  else
    echo "  no baseline yet; recording $SIZE"
  fi
  echo "  dump: $SIZE bytes"

  # 3. Restore beside the live copy, never over it. pg_restore --clean into the
  #    live database would leave it empty or half-loaded for the length of a
  #    13 GB restore, and a failure partway would leave it that way until
  #    someone noticed.
  psql "DROP DATABASE IF EXISTS ${db}_next"
  psql "CREATE DATABASE ${db}_next"
  docker exec -i "$PG" pg_restore -U postgres -d "${db}_next" --no-owner --no-privileges < "dump/$db.dump"

  # 4. Row floor on the restored copy. The size check above can pass on a dump
  #    that is intact but stale or emptied upstream; this one compares what
  #    actually landed against what is already serving.
  NEW=$(count "${db}_next" "$tbl")
  OLD=$(count "$db" "$tbl" 2>/dev/null || echo 0)
  echo "  rows in $tbl: live=$OLD new=$NEW"
  if [ "$OLD" -gt 0 ]; then
    MIN=$((OLD * 70 / 100))
    [ "$NEW" -ge "$MIN" ] || { echo "FATAL: $db.$tbl new=$NEW < 70% of live=$OLD"; exit 1; }
  fi

  # 5. Swap. A rename needs the database to have no other sessions; the weekly
  #    consumers connect only while they run, so a leftover session here means
  #    something unexpected is holding it and the swap must not proceed —
  #    terminating a connection whose owner is unknown is not this job's call.
  BUSY=$(psql "SELECT count(*) FROM pg_stat_activity WHERE datname = '$db'")
  [ "$BUSY" -eq 0 ] || { echo "FATAL: $db has $BUSY open session(s); not swapping"; exit 1; }
  psql "ALTER DATABASE $db RENAME TO ${db}_old"
  psql "ALTER DATABASE ${db}_next RENAME TO $db"
  psql "DROP DATABASE ${db}_old"

  # 6. Only a swapped copy updates the baseline, so a run that failed the floors
  #    cannot lower the bar for the next one.
  echo "$SIZE" > "state/$db.size"
  rm -f "dump/$db.dump"
  echo "  swapped"
done

echo "=== crawler restage done $(date -u '+%F %T')Z ==="
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
