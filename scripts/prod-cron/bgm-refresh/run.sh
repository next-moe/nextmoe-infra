#!/bin/sh
# BGM weekly refresh (refs/proj ops, 2026-07-21 拍板 ⑦):
# pull the newest Bangumi Archive dump, re-ingest src_bangumi (whole-table
# replacement per file), then idempotently re-run the BGM-sourced catalog
# family tools. Safe to re-run any time; skips when no new dump is published.
# Every tool runs from the infra-tools image resolved below; nothing is staged
# on this host. Canonical copy: scripts/prod-cron/bgm-refresh/run.sh in
# nextmoe-infra — edit there and redeploy.
set -eu
BASE=/root/bgm-refresh
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1
exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1
# Report the outcome. A failing run alerts immediately; a succeeding run
# stamps state/last-success, which is what /root/lib/watchdog.sh reads to
# notice a job that has silently stopped running at all — the failure mode a
# trap inside this script can never see, because it looks exactly like
# silence. An alert that cannot be delivered must not fail the run itself.
#
# The stamp is also gated on LOCKED, set only after the flock above succeeds.
# A run that skipped because a PREVIOUS one is still holding the lock did no
# work, and stamping for it would turn a permanently hung run into a silent
# weekly no-op the watchdog reads as healthy — the one failure the watchdog
# exists to catch. Today the lock-skip also exits above this trap, so the gate
# is redundant; it stops being redundant the moment someone moves the trap up
# to "define cleanup first", which reads like a tidy-up and is not one. The
# "no new dump" skip below DOES stamp: that path ran and found nothing to do.
on_exit() {
  rc=$?
  rm -f dump.zip
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] bgm weekly refresh (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== BGM weekly refresh start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

# Resolve the tools image to a digest once per run: every step below runs
# identical code, and the log records exactly what ran. CI builds this image
# in lockstep with apps/api, so pulling here is what keeps the tools current
# with the deployed schema — the hand-staged binaries this replaced fell two
# commits behind a NOT NULL migration and wrote nothing for 15 days (2026-08-05).
# A failed pull falls back to the local copy loudly rather than dying offline.
IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# 1. Resolve the newest dump asset (assets accumulate under the rolling
#    "archive" tag; names embed the date so max_by(.name) is the newest).
URL=$(curl -s --retry 3 https://api.github.com/repos/bangumi/Archive/releases/tags/archive \
  | jq -r '[.assets[] | select(.name|test("^dump-.*\\.zip$"))] | max_by(.name) | .browser_download_url')
NAME=$(basename "$URL")
[ -n "$NAME" ] && [ "$NAME" != null ] || { echo "FATAL: dump asset resolve failed"; exit 1; }
if [ -f state/last-dump ] && [ "$(cat state/last-dump)" = "$NAME" ]; then
  echo "no new dump ($NAME); nothing to do"; exit 0
fi
echo "new dump: $NAME"

# 2. Download + integrity guards — the ingest is truncate+reload per file,
#    so never feed it a truncated/corrupt dump.
curl -sL --retry 3 -o dump.zip "$URL"
SIZE=$(stat -c%s dump.zip)
[ "$SIZE" -gt 250000000 ] || { echo "FATAL: dump.zip too small ($SIZE bytes)"; exit 1; }
rm -rf dump && mkdir dump && unzip -qo dump.zip -d dump
LINES=$(wc -l < dump/subject.jsonlines)
[ "$LINES" -gt 600000 ] || { echo "FATAL: subject.jsonlines too few lines ($LINES)"; exit 1; }
echo "dump ok: $SIZE bytes, subject lines $LINES"

# 3. Fresh env snapshot from the catalog container (secrets never on command
#    lines; file is shredded by the EXIT trap).
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp
# --user 0:0: the image defaults to appuser (10001), which cannot write the
# root-owned state/ under the /w mount (build-derived-series receipts).
run() {
  docker run --rm --network "container:$PG" --env-file "$BASE/env.tmp" \
    -e KUN_CATALOG_PG_HOST=127.0.0.1 -e KUN_CATALOG_PG_DATABASE=kun_catalog \
    -v "$BASE:/w" --user 0:0 "$IMG" "$@"
}
# The password rides PGPASSWORD, never the DSN: a container's processes sit in
# this host's process table with their argv, and on 2026-09-02 `ps` printed the
# assembled DSN with its password. pgx reads PGPASSWORD when the DSN names none.
#
# Parallel query is disabled per session: the postgres container runs with the
# 64MB docker-default /dev/shm, and parallel hash joins on the big staging
# tables exhaust it (SQLSTATE 53100, seen 2026-07-22 in backfill-entity-intros).
# Serial plans spill to disk instead — slower but bounded. Remove once the
# compose sets shm_size on the postgres service.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; B="host=127.0.0.1 port=5432 user=$U sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0'"'"'"; CAT="$B dbname=kun_catalog"; EG="$B dbname=erogamescape"; DL="$B dbname=dlsite"; HL="$B dbname=howlongtobeat"'

# 4. Ingest (env-config tool).
run ingest-bangumi --dump-dir /w/dump

# 5. Family re-run, anchors first (all idempotent fill/upsert; a failure
#    aborts the run and leaves state/last-dump unset so next week retries).
run sh -c "$DSNSH"'; reconcile-doujin-bangumi --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; enrich-bgm-summaries --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; backfill-work-tags --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; backfill-work-ratings --dsn "$CAT" --eg-dsn "$EG" --dlsite-dsn "$DL" --hltb-dsn "$HL" --apply'
run sh -c "$DSNSH"'; backfill-entity-intros --dsn "$CAT" --eg-dsn "$EG" --apply'
# --wiki-dsn was dropped from the tool with the galgame-wiki retirement. Go's
# flag package treats an undefined flag as a usage error and exits 2, so this
# line killed the 2026-08-13 run the moment the binary was current again: the
# image tracking main can break the INVOCATION, not just the binary. When a
# tool's flags change in infra, fix this file in the same PR.
run sh -c "$DSNSH"'; backfill-release-meta --dsn "$CAT" --dlsite-dsn "$DL" --eg-dsn "$EG" --apply'
run sh -c "$DSNSH"'; backfill-bgm-work-meta --dsn "$CAT" --apply'
# Chinese source titles from the subject.name_cn the ingest above just
# replaced — work-level metadata, so it belongs beside the meta step rather
# than with the edge tools. Fill-missing only: a title a human published is
# never overwritten, and a machine title is superseded rather than duplicated.
# Prod-proven idempotent (wave 210: a second pass wrote zero).
run sh -c "$DSNSH"'; backfill-work-zh-titles --dsn "$CAT" --mode source --source bgm --apply'
run import-galgame-credits --source bangumi --apply
run import-work-relations --source all --run
# Derived series (wave 184): re-cluster the grown relation graph. Reaper
# semantics — an unchanged graph writes nothing; worklist captures refusals.
run sh -c "$DSNSH"'; build-derived-series --dsn "$CAT" --apply --receipts /w/state/derived-receipts.jsonl --worklist /w/state/derived-worklist.jsonl'
run sh -c "$DSNSH"'; backfill-character-attrs --dsn "$CAT" --apply'

# 6. Finalize.
echo "$NAME" > state/last-dump
# Compress finished logs (GORM slow-SQL dumps make raw logs ~40MB) and purge old.
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== BGM weekly refresh done $(date -u '+%F %T')Z ==="
