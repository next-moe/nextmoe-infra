#!/bin/sh
# shellcheck disable=SC2016
# Weekly source-import (catalog EG / DLsite / Getchu / leftover VNDB+Bangumi
# lanes). Canonical copy: scripts/prod-cron/source-import/run.sh in
# nextmoe-infra — edit there and redeploy; the box copy is /root/source-import/run.sh
# and must stay byte-identical.
#
# Catalog rows from EG, DLsite and Getchu stopped updating in July–August 2026
# because nothing scheduled these tools. They are already written, already
# tested, and were last run by hand then. This job is that schedule.
#
# WHY a separate job from vndb-refresh: that job exits early when VNDB has
# published no new dump, and a failure in an unrelated lane must not abort
# the VNDB chain. Crontab: Sunday 18:00 CST (`0 18 * * 0`), after
# vndb-refresh at 17:30, because the Getchu and store-anchor lanes read
# src_vndb which that job reloads.
#
# Every tool runs from the infra-tools image resolved below; nothing is staged
# on this host.
set -eu
BASE=${SOURCE_IMPORT_BASE:-/root/source-import}
ALERT_SH=${SOURCE_IMPORT_ALERT:-/root/lib/alert.sh}
VNDB_LOCK=${SOURCE_IMPORT_VNDB_LOCK:-/root/vndb-refresh/.lock}
cd "$BASE"
mkdir -p logs state
LOG="logs/run-$(date -u +%F).log"
exec >>"$LOG" 2>&1

LOCKED=0
FAIL=0
# Report the outcome. A failing run alerts immediately; a succeeding run
# stamps state/last-success, which is what /root/lib/watchdog.sh reads to
# notice a job that has silently stopped running at all — the failure mode a
# trap inside this script can never see, because it looks exactly like
# silence. An alert that cannot be delivered must not fail the run itself.
#
# The stamp is gated on LOCKED, set only after this job's flock succeeds.
# A run that skipped because a PREVIOUS one is still holding the lock did no
# work, and stamping for it would turn a permanently hung run into a silent
# weekly no-op the watchdog reads as healthy. Groups keep going when one
# fails (they are independent upstreams), but ANY failed group fails the run
# — swallowing that with `|| echo WARN` is how a lane can die weekly without
# a single alert.
#
# The trap is armed before either lock so a vndb-refresh wait timeout still
# alerts, which means it also fires for a run that never owned env.tmp. The
# first harness pass of this script shredded the RUNNING instance's snapshot
# from a lock-skipped one, so the cleanup is gated on LOCKED too.
on_exit() {
  rc=$?
  if [ "${LOCKED:-0}" = 1 ] && [ -f env.tmp ]; then shred -u env.tmp; fi
  [ "$rc" -eq 0 ] && [ "${FAIL:-0}" -ne 0 ] && rc=1
  if [ "$rc" -eq 0 ] && [ "${LOCKED:-0}" = 1 ]; then
    date -u '+%F %T' > "$BASE/state/last-success"
  elif [ "$rc" -ne 0 ]; then
    echo "=== FAILED (exit $rc) - sending alert ==="
    "$ALERT_SH" "[FAIL] source-import (exit $rc)" "$BASE/$LOG" || echo "alert delivery failed"
  fi
}
trap on_exit EXIT
echo "=== source-import start $(date -u '+%F %T')Z ==="

# Wait for vndb-refresh to drop its lock before taking ours. Getchu refs/intros
# and the dmm/dlsite/dlsite-en store-anchor lanes read src_vndb; that job
# reloads those tables by whole-table replacement, so running against a
# half-reloaded mirror would join torn staging. flock -w 7200 on its lock
# file is a wait-until-free, not a hold. Timeout: fail the run (alert, no
# stamp) rather than proceed.
echo "waiting on vndb-refresh lock $VNDB_LOCK (up to 7200s)"
if ! flock -w 7200 "$VNDB_LOCK" true; then
  echo "FATAL: timed out waiting for vndb-refresh ($VNDB_LOCK); refusing to run against a half-reloaded src_vndb"
  exit 1
fi

exec 9>"$BASE/.lock"; flock -n 9 || { echo "another run holds the lock; exit"; exit 0; }
LOCKED=1

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

# Fresh env snapshot from the catalog container (secrets never on command
# lines; the file is shredded by the EXIT trap).
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CATC" > env.tmp
chmod 600 env.tmp
# --user 0:0: the image defaults to appuser (10001), which cannot write the
# root-owned state/ under the /w mount.
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
DSNSH='U="${KUN_CATALOG_PG_USER:-$KUN_PG_USER}"; export PGPASSWORD="${KUN_CATALOG_PG_PASSWORD:-$KUN_PG_PASSWORD}"; B="host=127.0.0.1 port=5432 user=$U sslmode=disable options='"'"'-c max_parallel_workers_per_gather=0'"'"'"; CAT="$B dbname=kun_catalog"; EG="$B dbname=erogamescape"; DL="$B dbname=dlsite"; GC="$B dbname=getchu"'

begin_group() {
  GROUP=$1
  GROUP_FAIL=0
  echo "--- group $GROUP ---"
}

gstep() {
  if [ "$GROUP_FAIL" -ne 0 ]; then
    return 0
  fi
  if ! run "$@"; then
    echo "WARN: group $GROUP failed"
    GROUP_FAIL=1
    FAIL=1
  fi
}

# Last occurrence of <counter>=<digits> in the dry log. Prints
# "<label>: <counter>=<n> (ceiling <max>)" for every counter it is asked
# to check. Unparseable or over-ceiling fails the call; the caller refuses
# the rest of the group.
check_counters() {
  _cc_label=$1
  _cc_log=$2
  shift 2
  _cc_bad=0
  for _cc_spec in "$@"; do
    _cc_counter=${_cc_spec%%=*}
    _cc_max=${_cc_spec#*=}
    _cc_n=$(sed -n "s/.*${_cc_counter}=\\([0-9][0-9]*\\).*/\\1/p" "$_cc_log" | tail -1)
    echo "$_cc_label: $_cc_counter=$_cc_n (ceiling $_cc_max)"
    if [ -z "$_cc_n" ]; then
      echo "FATAL: could not read $_cc_counter from $_cc_label dry run"
      _cc_bad=1
    elif [ "$_cc_n" -gt "$_cc_max" ]; then
      echo "FATAL: $_cc_label $_cc_counter=$_cc_n exceeds ceiling $_cc_max"
      _cc_bad=1
    fi
  done
  return "$_cc_bad"
}

dry_ok() {
  _dry_label=$1
  shift
  last_dry_log="state/${_dry_label}-dry.log"
  if ! run "$@" >"$last_dry_log" 2>&1; then
    echo "FATAL: $_dry_label dry run failed"
    cat "$last_dry_log"
    GROUP_FAIL=1
    FAIL=1
    return 1
  fi
  return 0
}

ceiling_failed() {
  if [ "$GROUP_FAIL" -eq 0 ]; then
    cat "$last_dry_log"
    GROUP_FAIL=1
    FAIL=1
  fi
}

begin_group vndb-extra
gstep sh -c "$DSNSH"'; import-store-anchors --dsn "$CAT" --only dmm --apply'
gstep sh -c "$DSNSH"'; import-store-anchors --dsn "$CAT" --only dlsite --apply'
gstep sh -c "$DSNSH"'; import-store-anchors --dsn "$CAT" --only dlsite-en --apply'
gstep sh -c "$DSNSH"'; import-release-labels --dsn "$CAT" --apply'
gstep sh -c "$DSNSH"'; backfill-character-instances --dsn "$CAT" --apply'
gstep import-work-intro --apply

# Ceilings. The roster lanes only read EXACT anchors, and a merge demotes both
# exacts to probable, so a folded batch comes back as a mass re-mint (the same
# tripwire as vndb-refresh step 5). The two-month backlog these lanes carried
# on 2026-09-16 planned 24 EG characters, 112 EG names, 0 Bangumi characters,
# 295 DLsite series and 975 cross-media works; it was drained by hand before
# this job was armed, so a ceiling tripping here is news, not backlog.
begin_group eg
# Upstream-liveness audit for every EG entity type and link kind. A row the
# daily crawler has not re-synced within 48 hours of the table max is treated
# as deleted upstream (the crawler never DELETEs). Organic EG deletions are a
# few a week; a dry-run ceiling of 500 on marked_total trips a mass event
# (a half-finished crawl is also refused by the 97% freshness guard).
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-liveness sh -c "$DSNSH"'; audit-anchor-liveness --dsn "$CAT" --source erogamescape --eg-dsn "$EG"' \
     && check_counters eg-liveness "$last_dry_log" marked_total=500; then
    gstep sh -c "$DSNSH"'; audit-anchor-liveness --dsn "$CAT" --source erogamescape --eg-dsn "$EG" --apply --receipts /w/state/liveness-erogamescape.jsonl'
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-anchors sh -c "$DSNSH"'; reconcile-eg-anchors --dsn "$CAT" --eg-dsn "$EG"' \
     && check_counters eg-anchors "$last_dry_log" exact_planned=300 probable_planned=600 related_planned=1500; then
    gstep sh -c "$DSNSH"'; reconcile-eg-anchors --dsn "$CAT" --eg-dsn "$EG" --apply'
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-works sh -c "$DSNSH"'; reconcile-eg-works --dsn "$CAT" --eg-dsn "$EG"' \
     && check_counters eg-works "$last_dry_log" attached=300 quarantined=100 minted_live=150; then
    gstep sh -c "$DSNSH"'; reconcile-eg-works --dsn "$CAT" --eg-dsn "$EG" --apply --limit 250'
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-roster import-character-roster --source eg \
     && check_counters eg-roster "$last_dry_log" characters_created=300; then
    gstep import-character-roster --source eg --apply
  else
    ceiling_failed
  fi
fi
# Must follow the roster: voice-actor credits route through its character anchors.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-credits import-galgame-credits --source eg \
     && check_counters eg-credits "$last_dry_log" names_created=1000 characters_created=300; then
    gstep import-galgame-credits --source eg --apply
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-music import-galgame-credits --source eg-music \
     && check_counters eg-music "$last_dry_log" names_created=500; then
    gstep import-galgame-credits --source eg-music --apply
  else
    ceiling_failed
  fi
fi
gstep sh -c "$DSNSH"'; import-store-refs --dsn "$CAT" --eg-dsn "$EG" --apply'
gstep sh -c "$DSNSH"'; backfill-work-playtime --dsn "$CAT" --eg-dsn "$EG" --source eg --apply'

begin_group bangumi
# Mints Bangumi game subjects that no work anchors yet, ahead of the roster so a
# new work is cast in the same run. A title colliding with a live work is
# minted quarantined for the work-pair judge. The dry survey counts the whole
# pool, quarantine backlog included (1,369 on 2026-09-17), so --limit is what
# bounds a run and the ceiling watches the live creates only.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok bgm-type4 sh -c "$DSNSH"'; expand-bgm-type4-gated --dsn "$CAT"' \
     && check_counters bgm-type4 "$last_dry_log" to_create=150; then
    gstep sh -c "$DSNSH"'; expand-bgm-type4-gated --dsn "$CAT" --apply --limit 250'
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok bangumi-roster import-character-roster --source bangumi \
     && check_counters bangumi-roster "$last_dry_log" characters_created=300; then
    gstep import-character-roster --source bangumi --apply
  else
    ceiling_failed
  fi
fi
gstep sh -c "$DSNSH"'; backfill-bgm-zh-names --dsn "$CAT" --lane character --apply'
gstep sh -c "$DSNSH"'; backfill-bgm-zh-names --dsn "$CAT" --lane person --apply'
gstep sh -c "$DSNSH"'; backfill-bgm-zh-names --dsn "$CAT" --lane label --apply'
# --run is the apply flag of import-entity-aliases, person-link-batch and
# import-bangumi-xmedia: Go's flag package treats --apply as undefined and exits
# 2, the same class of invocation break as bgm-refresh's --wiki-dsn on
# 2026-08-13. Each leg is named; with neither flag the tool runs both.
gstep import-entity-aliases --hints --run
# The candidate leg files the credit-name pairs Bangumi declares as aliases, and
# person-link-batch links the ones its rules settle: A3, both names credited on
# one work; A4, each declared as the other's alias. On 2026-09-17 those rules
# linked 786 of a 2,167-pair backlog (342 new people) and left 1,359 pending,
# because the LLM credit-name judge sees nothing but the two names and is not
# scheduled. A link has no undo, so both steps run behind a dry-run ceiling.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok alias-candidates import-entity-aliases --candidates \
     && check_counters alias-candidates "$last_dry_log" candidates=300; then
    gstep import-entity-aliases --candidates --run
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok person-link person-link-batch --rule-set alias --actor 1 \
     && check_counters person-link "$last_dry_log" created=200 attached=300; then
    gstep person-link-batch --rule-set alias --actor 1 --run
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok bangumi-xmedia import-bangumi-xmedia \
     && check_counters bangumi-xmedia "$last_dry_log" registered_anime=200 registered_manga=200 registered_novel=200; then
    gstep import-bangumi-xmedia --run
  else
    ceiling_failed
  fi
fi

begin_group dlsite
# Mints the DLsite works an EG game claims and no work carries yet, ahead of the
# genre, alias, platform and series lanes so they enrich it the same run.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok eg-dlsite import-eg-dlsite-releases \
     && check_counters eg-dlsite "$last_dry_log" minted=150 quarantined=50; then
    gstep import-eg-dlsite-releases --run
  else
    ceiling_failed
  fi
fi
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok dlsite-games sh -c "$DSNSH"'; import-dlsite-games --dlsite-dsn "$DL"' \
     && check_counters dlsite-games "$last_dry_log" declared_groups=300 title_attached_groups=300 quarantined_groups=100 minted_groups=300; then
    gstep sh -c "$DSNSH"'; import-dlsite-games --dlsite-dsn "$DL" --run --limit 250'
  else
    ceiling_failed
  fi
fi
gstep sh -c "$DSNSH"'; backfill-dlsite-genres --dsn "$CAT" --dlsite-dsn "$DL" --apply'
# Held back until 2026-09-18 as possibly unfit (a quarter of store blurbs carry a
# URL line). It fills only works with no ja intro at all, and the 7,455 DLsite
# intros already live carry URLs at a higher rate (24.8%) than its backlog (19.6%);
# intromt strips bare URL lines before translating, so the zh-Hans face gets 0.3%.
# --kind intro only: covers and screenshots need the mirror and belong to image-mirror.
gstep sh -c "$DSNSH"'; backfill-dlsite-media --dsn "$CAT" --dlsite-dsn "$DL" --kind intro --apply'
gstep sh -c "$DSNSH"'; import-work-aliases --dsn "$CAT" --dlsite-dsn "$DL" --source all --apply'
gstep sh -c "$DSNSH"'; import-work-platforms --dsn "$CAT" --dlsite-dsn "$DL" --source all --apply'
# Rebuilds the DLsite series lane and deletes series that lost all members;
# the deletion ceiling is the point.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok work-series sh -c "$DSNSH"'; import-work-series --dsn "$CAT" --dlsite-dsn "$DL"' \
     && check_counters work-series "$last_dry_log" series_created=100 series_deleted=10 series_renamed=50; then
    gstep sh -c "$DSNSH"'; import-work-series --dsn "$CAT" --dlsite-dsn "$DL" --apply'
  else
    ceiling_failed
  fi
fi

begin_group getchu
# After EG and DLsite so a Getchu mint sees the works those lanes just created.
gstep import-getchu-refs --apply
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok getchu-attach sh -c "$DSNSH"'; reconcile-getchu --dsn "$CAT" --getchu-dsn "$GC" --eg-dsn "$EG"' \
     && check_counters getchu-attach "$last_dry_log" attached=300 minted_live=50 minted_quarantined=100; then
    gstep sh -c "$DSNSH"'; reconcile-getchu --dsn "$CAT" --getchu-dsn "$GC" --eg-dsn "$EG" --apply'
  else
    ceiling_failed
  fi
fi
gstep sh -c "$DSNSH"'; import-getchu-intros --dsn "$CAT" --getchu-dsn "$GC" --population all --apply'
gstep sh -c "$DSNSH"'; import-getchu-characters --dsn "$CAT" --getchu-dsn "$GC" --apply'

# Last of the source groups: it reads every source the groups above and
# vndb-refresh just refreshed. Importers only create releases, so a date that
# moved upstream (VNDB postponements, DLsite announcements) stayed put; the
# previous fill-empty job also read works.regist_date, which sat one day late
# on 2,009 DLsite rows against the API string.
begin_group release-dates
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok release-dates sh -c "$DSNSH"'; backfill-release-meta --dsn "$CAT" --dlsite-dsn "$DL" --eg-dsn "$EG" --getchu-dsn "$GC"' \
     && check_counters release-dates "$last_dry_log" all_filled=3000 all_moved=1000 all_cleared=100; then
    gstep sh -c "$DSNSH"'; backfill-release-meta --dsn "$CAT" --dlsite-dsn "$DL" --eg-dsn "$EG" --getchu-dsn "$GC" --apply --receipts /w/state/release-dates.jsonl'
  else
    ceiling_failed
  fi
fi

begin_group labels
# Last, so the works the groups above minted carry their anchors first. vndb and
# erogamescape mint a label for a producer no label answers to; Bangumi only
# anchors. The 2026-09-17 backlog (182 producers, 13 corporate-graph nodes, a
# month of VNDB) was drained by hand after its whole name list was read, so a
# ceiling tripping here is news, not backlog.
if [ "$GROUP_FAIL" -eq 0 ]; then
  if dry_ok org-labels sh -c "$DSNSH"'; reconcile-org-labels --dsn "$CAT" --eg-dsn "$EG" --source all' \
     && check_counters org-labels "$last_dry_log" new_labels=100 minted=30 anchors_exact=300 anchors_probable=300; then
    gstep sh -c "$DSNSH"'; reconcile-org-labels --dsn "$CAT" --eg-dsn "$EG" --source all --apply'
  else
    ceiling_failed
  fi
fi

# DELIBERATELY NOT RUN HERE:
#   import-dlsite-works — imports only ボイス・ASMR works; ASMR is out of catalog scope by decision of 2026-08-21
#   catalog-char-xsrc — the LLM-adjudicated fold of cross-source character twins; it has its own nightly job, char-xsrc-nightly
#   llm-suggest --task queue-creditname — has its own nightly job, llm-adjudicate-nightly
#   enrich-org-labels — its dry counts do not subtract the rows already written, so its first apply is unmeasured
#   backfill-vndb-covers, backfill-character-portraits, backfill-dlsite-media (cover, screenshot),
#   backfill-bangumi-covers, backfill-label-logos (bangumi), backfill-person-photos — image-mirror
#   backfill-getchu-media, backfill-getchu-portraits — Getchu's robots.txt disallows the image paths
#   backfill-label-logos --source cien — kun-dlsite-api cien-avatars reads cien.dlsite.com, which this host has not been probed against
#   mint-catalog-persons — one-shot; requires a wave-152 clusters file
#   reindex-catalog — has its own daily cron at 06:10 CST

find logs -name 'run-*.log' ! -name "run-$(date -u +%F).log" -exec gzip -qf {} \;
find logs -name 'run-*.log.gz' -mtime +90 -delete
[ "$FAIL" -eq 0 ] || { echo "=== source-import had failed group(s) ==="; exit 1; }
echo "=== source-import done $(date -u '+%F %T')Z ==="
