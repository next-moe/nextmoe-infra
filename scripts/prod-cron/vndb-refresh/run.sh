#!/bin/sh
# VNDB weekly refresh (wave 198) — the missing twin of /root/bgm-refresh/run.sh.
#
# Pull the newest VNDB database dump (vndb.org/d14), re-stage the src_vndb
# schema (whole-table replacement per file) and idempotently re-run the
# VNDB-sourced catalog family. Safe to re-run any time; skips when the dump
# behind the "latest" alias has not moved since the last successful run.
#
# Every tool runs from the infra-tools image resolved below; nothing is staged
# on this host. Canonical copy: scripts/prod-cron/vndb-refresh/run.sh in
# nextmoe-infra — edit there and redeploy.
#
# WHY a separate job from bgm-refresh: the two upstreams publish on different
# clocks (Bangumi weekly on Tue, VNDB daily ~08:05Z) and the two family lists
# barely overlap, so folding them into one runner would make each wait on the
# other's failures. The three tools they share (import-work-relations,
# build-derived-series, backfill-character-attrs) are all idempotent, so
# running them from both jobs is a no-op on the second pass.
set -eu
BASE=/root/vndb-refresh
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
  rm -rf dump.tar.zst dump votes.gz
  if [ -f env.tmp ]; then shred -u env.tmp; fi
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  else
    echo "=== FAILED (exit $rc) - sending alert ==="
    /root/lib/alert.sh "[FAIL] vndb weekly refresh (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== VNDB weekly refresh start $(date -u '+%F %T')Z ==="

PG=kun-visual-novel-infra-vqvqbc-postgres-1
CATC=kun-visual-novel-infra-vqvqbc-catalog-1

# Resolve the tools image to a digest once per run: every step below runs
# identical code, and the log records exactly what ran. CI builds this image
# in lockstep with apps/api, so pulling here is what keeps the tools current
# with the deployed schema. A failed pull falls back to the local copy loudly
# rather than dying offline.
IMG_TAG=ghcr.io/next-moe/infra-tools:latest
docker pull -q "$IMG_TAG" >/dev/null 2>&1 || echo "WARN: image pull failed; using the local copy"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMG_TAG")
echo "image: $IMG"

# The archive is zstd; the extract runs on the host, so the host needs the
# zstd binary. Fail loudly rather than half-extracting.
command -v zstd >/dev/null || { echo "FATAL: zstd not installed (apt-get install -y zstd)"; exit 1; }

# 1. Resolve which dated dump the rolling "latest" alias currently points at.
#    dl.vndb.org answers the alias with a 307 to vndb-db-<YYYY-MM-DD>.tar.zst,
#    so the redirect target IS the version stamp — no download needed to decide
#    whether there is anything new. VNDB keeps only ~2 days of history, which is
#    exactly why this must be a scheduled job and not a manual chore.
URL=https://dl.vndb.org/dump/vndb-db-latest.tar.zst
NAME=$(curl -sI --retry 3 "$URL" | tr -d '\r' | sed -n 's/^[Ll]ocation: .*\///p')
case "$NAME" in
  vndb-db-*.tar.zst) ;;
  *) echo "FATAL: dump version resolve failed (got '$NAME')"; exit 1 ;;
esac
if [ -f state/last-dump ] && [ "$(cat state/last-dump)" = "$NAME" ]; then
  echo "no new dump ($NAME); nothing to do"; exit 0
fi
echo "new dump: $NAME"

# 2. Download + integrity guards. The ingest TRUNCATEs and reloads each table,
#    so a truncated or partial archive would silently empty the staging schema
#    and every downstream tool would then read a catalog that "lost" its VNDB
#    facts. Floors are ~10% under the 2026-08-09 dump (see NOTES.md) — loose
#    enough for organic growth, tight enough to catch a broken transfer.
curl -sL --retry 3 -o dump.tar.zst "$URL"
SIZE=$(stat -c%s dump.tar.zst)
[ "$SIZE" -gt 150000000 ] || { echo "FATAL: dump.tar.zst too small ($SIZE bytes)"; exit 1; }
# The archive unpacks flat (db/, TIMESTAMP, licenses) with no top-level
# directory of its own, hence -C into a dedicated dir.
rm -rf dump && mkdir dump && zstd -dc dump.tar.zst | tar -x -C dump
[ -f dump/db/vn ] || { echo "FATAL: dump/db/vn missing after extract"; exit 1; }
echo "dump timestamp: $(cat dump/TIMESTAMP 2>/dev/null || echo unknown)"

# Every staged table must be present BEFORE the first TRUNCATE: each file is
# its own transaction, so a file missing halfway through would leave earlier
# tables replaced and later ones stale — a torn staging schema.
FILES="vn vn_titles chars chars_names chars_vns images vn_relations staff staff_alias
vn_staff vn_seiyuu traits traits_parents chars_traits tags tags_parents tags_vn
producers releases releases_vn releases_producers releases_platforms
releases_titles extlinks releases_extlinks vn_extlinks producers_extlinks
staff_extlinks producers_relations"
for f in $FILES; do
  [ -s "dump/db/$f" ] && [ -s "dump/db/$f.header" ] || { echo "FATAL: dump/db/$f (or .header) missing/empty"; exit 1; }
done

# Row-count floors on three unrelated tables: one shrinking file is a bad
# transfer, three at once would be an upstream event worth stopping for.
guard() {
  n=$(wc -l < "dump/db/$1")
  [ "$n" -gt "$2" ] || { echo "FATAL: dump/db/$1 too few lines ($n <= $2)"; exit 1; }
  echo "  $1: $n rows"
}
guard vn 60000
guard producers 26000
guard chars 150000
echo "dump ok: $SIZE bytes"

# 2b. The votes dump is a SEPARATE daily publication, not part of the archive
#     above: the database dump carries only vn.c_rating / c_average / c_votecount,
#     and this file is the only public source of a per-vn score histogram. It is
#     staged by whole-table replacement like everything else, so a truncated
#     transfer would silently shrink every histogram on the site and a skipped
#     download would leave last week's bars beside this week's scores — hence
#     FATAL, never a soft skip. gzip -t verifies the CRC over the whole file,
#     which is what actually catches a half-finished transfer; the line floor is
#     ~10% under the 2026-08-14 file (1,988,563 lines).
curl -sL --retry 3 -o votes.gz https://dl.vndb.org/dump/vndb-votes-latest.gz
gzip -t votes.gz || { echo "FATAL: votes.gz failed the gzip integrity check"; exit 1; }
VLINES=$(gzip -dc votes.gz | wc -l)
[ "$VLINES" -gt 1780000 ] || { echo "FATAL: votes.gz too few lines ($VLINES <= 1780000)"; exit 1; }
echo "votes ok: $VLINES lines"

# 3. Fresh env snapshot from the catalog container (secrets never on command
#    lines; the file is shredded by the EXIT trap).
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp
# --user 0:0: the image defaults to appuser (10001), which cannot write the
# root-owned state/ under the /w mount (build-derived-series receipts,
# stale-anchors.tsv).
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
# tables exhaust it (SQLSTATE 53100). Serial plans spill to disk instead —
# slower but bounded. Remove once the compose sets shm_size on postgres.
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; B="host=127.0.0.1 port=5432 user=$U sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0'"'"'"; CAT="$B dbname=kun_catalog"; EG="$B dbname=erogamescape"; DL="$B dbname=dlsite"; HL="$B dbname=howlongtobeat"'

# 4. Re-stage the 29 src_vndb tables plus vn_vote_stats (env-config tool, no --dsn).
run ingest-vndb --dump-dir /w/dump/db --votes-file /w/votes.gz

# 5. Resurrection tripwire, BEFORE any Gold write.
#    A merge that folds two VNDB characters into one demotes BOTH exact vndb
#    anchors to probable (service/merge_execute.go: "two silent exacts must
#    never survive a merge"). import-character-roster only reads EXACT anchors,
#    so those ids look unanchored again and it re-mints the characters the
#    merge just folded away. A dry pass first: organic weekly growth is a few
#    hundred new characters, so a four-digit plan means something folded a
#    batch and a human must look before the mint lands.
#    This catches a mass event, NOT a handful of merges — see NOTES.md §risks.
ROSTER_CEILING=5000
run import-character-roster --source vndb > state/roster-dry.log 2>&1 || {
  echo "FATAL: roster dry run failed"; cat state/roster-dry.log; exit 1; }
PLANNED=$(sed -n 's/.*characters_created=\([0-9]*\).*/\1/p' state/roster-dry.log | tail -1)
[ -n "$PLANNED" ] || { echo "FATAL: could not read characters_created from the roster dry run"; cat state/roster-dry.log; exit 1; }
echo "roster plans $PLANNED new characters (ceiling $ROSTER_CEILING)"
[ "$PLANNED" -le "$ROSTER_CEILING" ] || {
  echo "FATAL: roster mint plan $PLANNED exceeds the ceiling — a merge batch may have"
  echo "       demoted anchors. Inspect state/roster-dry.log, then either raise the"
  echo "       ceiling deliberately or fix the anchors before re-arming."
  exit 1; }

# 5b. Upstream-liveness audit — maintain catalog_external_ref.dead_at for the
#     work-level exact VNDB anchors, against the mirror step 4 just reloaded.
#     VNDB deletes ~20 entries a week and we anchor ~98.7% of it, so without
#     this the set of anchors pointing at a vndb.org page that now 404s grows
#     forever; the public faces render those anchors as links. Marking (never
#     deleting, never re-pointing) is deliberate: the dump carries no
#     redirect/tombstone table, so a deleted entry has no derivable successor,
#     and a deleted ROW would just be re-asserted by the wiki-vndb-id rule.
#     The tool is bidirectional and self-healing — an entry VNDB restores gets
#     its dead_at cleared on the next pass.
#
#     WHY HERE, the earliest legal slot: it only needs a fresh src_vndb (step 4)
#     and interacts with nothing the family below writes (that lane touches
#     character and release anchors, never work-level vndb ones). It must come
#     after the step-5 tripwire because dead_at is a Gold write, and running it
#     before the family rather than after means it still lands on the weeks a
#     later family step aborts the run — the audit is the cheap half.
#
#     Its own --min-mirror-rows guard (default 50,000) refuses to write against
#     a partially-loaded mirror, which would otherwise mark all ~64k anchors
#     dead in one transaction and strip every VNDB link from the site.
run sh -c "$DSNSH"'; audit-vndb-anchors --dsn "$CAT" --apply'

# 5c. Mint stage — admit VNs that have appeared upstream since the last run as
#     new catalog works. Until wave 211 nothing did this: every VNDB consumer
#     below is gated on works that ALREADY carry an exact vndb anchor, so a VN
#     that no work pointed at could never become one and the mirror grew
#     without the catalog following. It runs here, after the tripwire and the
#     audit, because the tripwire's charter is "before any Gold write" and this
#     step is one.
#
#     Admission is finished + in-development only; cancelled VNs stay out
#     (the ~1,500 cancelled entries already carrying anchors are historic
#     accidents, not a precedent). A vid is taken as claimed by an anchor of
#     ANY link_kind, not just exact — a merge demotes both surviving exacts to
#     probable, and minting a probably-linked vid would resurrect exactly what
#     the merge folded away.
#
#     WHY the ceiling: organic weekly growth is a few dozen VNs. A plan past
#     the ceiling means either the anchor table lost rows or step 4 staged a
#     partial vn table, and either way the run must stop before it mints
#     thousands of duplicate works — a mint is far more expensive to undo than
#     to skip.
#     NOTE for the first armed run: the historic backlog is ~480 unanchored
#     finished/in-dev VNs accumulated since the anchors were first built, so
#     the first pass WILL trip this ceiling. Drain it by hand in canary slices
#     (`import-vndb-works --limit 100 --apply`, inspect, repeat) before letting
#     the weekly job own it; do not just raise the number.
VNDB_WORKS_CEILING=300
run import-vndb-works > state/vndb-works-dry.log 2>&1 || {
  echo "FATAL: vndb works dry run failed"; cat state/vndb-works-dry.log; exit 1; }
WPLANNED=$(sed -n 's/.*works_created=\([0-9]*\).*/\1/p' state/vndb-works-dry.log | tail -1)
[ -n "$WPLANNED" ] || { echo "FATAL: could not read works_created from the vndb works dry run"; cat state/vndb-works-dry.log; exit 1; }
echo "vndb-works plans $WPLANNED new works (ceiling $VNDB_WORKS_CEILING)"
[ "$WPLANNED" -le "$VNDB_WORKS_CEILING" ] || {
  echo "FATAL: vndb work mint plan $WPLANNED exceeds the ceiling — the anchor table or"
  echo "       the staged vn table may be short. Inspect state/vndb-works-dry.log, then"
  echo "       drain the backlog in canary slices before re-arming."
  exit 1; }
run import-vndb-works --apply

# 6. Family re-run: identity/anchors first, then edges, then derived.
#    All steps are idempotent (upsert / ON CONFLICT DO NOTHING / change-detected
#    or a whole-lane rebuild). A failure aborts the run and leaves
#    state/last-dump unset, so next week retries the whole thing.

# 6a. Identity — new characters + their exact anchors. Everything below that
#     is character-shaped depends on these anchors existing.
run import-character-roster --source vndb --apply
# Persons and credit edges route their VA credits through the roster anchors,
# so this must follow 6a within the same run, not lead it.
run import-galgame-credits --source vndb --apply

# 6a2. Releases. Entity-shaped like 6a, so it belongs with identity rather than
#      with the edge steps: the exact release anchors it mints are what the
#      release-grain store-id work reads.
#      Wave 202 split the ROW decision (keyed work_id + r-id, no unique governs
#      it) from the ANCHOR decision (keyed by the r-id alone, the shape of
#      uq_catalog_external_ref_exact). A r-id whose exact slot another release
#      already holds now gets a row and a PROBABLE ref instead of a colliding
#      mint, minting goes through one chokepoint that reports a taken slot
#      rather than raising 23505, and the writes sit behind per-batch savepoints
#      so one bad row cannot roll back the wave. That is what let this step come
#      back into the weekly run after the 2026-08-09 abort.
#      The stale-anchor TSV is a human-review artifact, overwritten each run:
#      r-ids whose anchor sits under a work upstream no longer maps them to.
#      It blocks nothing; a non-empty file means there is adjudication waiting.
run import-vndb-releases --apply --stale-anchors-out /w/stale-anchors.tsv

# 6a3. Chinese source titles. It reads the release titles 6a2 just refreshed and
#      fills the zh slot of any work that has no SOURCE Chinese title yet, which
#      is every work 5c minted a few minutes ago. Fill-missing only: a title a
#      human published is never overwritten, and a machine title is superseded
#      rather than duplicated. Prod-proven idempotent (wave 210: a second pass
#      wrote zero).
run sh -c "$DSNSH"'; backfill-work-zh-titles --dsn "$CAT" --mode source --source vndb --apply'

# 6a4. Steam release anchors from the extlinks step 4 just reloaded. Until this
#      line the steam lane only ever ran by hand (store wave, 2026-08-26), so
#      works minted after that day had no steam anchor and the whole HLTB layer
#      below would silently never cover them. --only steam on purpose: the dmm /
#      dlsite lanes stay manual, this run changes nothing about them.
run sh -c "$DSNSH"'; import-store-anchors --dsn "$CAT" --only steam --apply'

# 6a5. HLTB work refs ride the steam anchors 6a4 just minted (HLTB carries no
#      vndb ids; the Steam appid is the only deterministic bridge). Probable,
#      rule:hltb-steam, rejection-guarded — a pair a human rejected stays dead.
run sh -c "$DSNSH"'; import-hltb-refs --dsn "$CAT" --hltb-dsn "$HL" --apply'

# 6b. Character facets for the characters 6a just created.
run sh -c "$DSNSH"'; import-character-traits --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; backfill-character-attrs --dsn "$CAT" --apply'

# 6c. Edges. Each writes only where BOTH endpoints already carry an exact
#     anchor, so a producer/label that reconcile-org-labels has not anchored
#     yet is counted as skipped_unanchored and comes in on a later pass — it is
#     never minted here. reconcile-org-labels runs in source-import half an
#     hour later, so a new label gets its edges the following week.
run sh -c "$DSNSH"'; import-work-producers --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; import-label-relations --dsn "$CAT" --apply'
run sh -c "$DSNSH"'; import-vndb-links --dsn "$CAT" --apply'
run import-work-relations --source vndb --run
run sh -c "$DSNSH"'; backfill-work-playtime --dsn "$CAT" --eg-dsn "$EG" --source vndb --apply'
run sh -c "$DSNSH"'; backfill-work-playtime --dsn "$CAT" --hltb-dsn "$HL" --source hltb --apply'

# 6d. Derived: re-cluster the relation graph the vndb lane just grew. Reaper
#     semantics — an unchanged graph writes nothing; the worklist captures the
#     components it refused (dlsite/curated overlap, oversized clusters).
run sh -c "$DSNSH"'; build-derived-series --dsn "$CAT" --apply --receipts /w/state/derived-receipts.jsonl --worklist /w/state/derived-worklist.jsonl'

# 6e. Ratings. Its vndb lane reads the src_vndb tables step 4 just replaced —
#     score and vote_count from vn, the histogram from vn_vote_stats — so this
#     is the run that makes those numbers current; nothing else refreshes them.
#     bgm-refresh issues the identical command on its own clock and that is
#     fine: every lane is a change-detected upsert, so whichever job runs second
#     reports `unchanged` and writes nothing.
run sh -c "$DSNSH"'; backfill-work-ratings --dsn "$CAT" --eg-dsn "$EG" --dlsite-dsn "$DL" --hltb-dsn "$HL" --apply'

# DELIBERATELY NOT RUN HERE:
#   reconcile-org-labels  — runs in source-import, behind a dry-run ceiling
#   enrich-org-labels     — see source-import
#   backfill-vndb-covers / backfill-character-portraits — image-mirror, Monday 03:30
#   reindex-catalog       — already has its own daily cron at 06:10

# 7. Finalize.
echo "$NAME" > state/last-dump
find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
echo "=== VNDB weekly refresh done $(date -u '+%F %T')Z ==="
