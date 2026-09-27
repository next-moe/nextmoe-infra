#!/usr/bin/env bash
#
# refresh-dev-db.sh — refresh the local dev databases from desensitised prod
# artifacts (one command). The desensitisation happens AT SOURCE on the prod
# host (scripts/dev-snapshot/build-snapshot.sh); this script only downloads +
# restores, so raw production PII never reaches the dev box (裁定 1a).
#
# Groups (裁定 4):
#   core     (default) download desensitised artifacts + restore.
#   sources  raw streaming (dlsite/erogamescape): zero PII, zero desensitisation,
#            no artifact — a direct `pg_dump -Fc | pg_restore` pipe.
#
# Usage:
#   ./scripts/refresh-dev-db.sh                       # all core DBs, latest artifact
#   ./scripts/refresh-dev-db.sh --fresh               # rebuild the artifact first
#   ./scripts/refresh-dev-db.sh --db kun_community    # one core DB
#   ./scripts/refresh-dev-db.sh --group sources       # stream dlsite + erogamescape
#   ./scripts/refresh-dev-db.sh --group sources --db erogamescape
#   ./scripts/refresh-dev-db.sh --assert-only --db kun_galgame_infra  # re-check PII
#
# Restore into throwaway DBs without touching the live ones (used by the
# pipeline's own verification):
#   DEVDB_SUFFIX=_devtest ./scripts/refresh-dev-db.sh --group core
#
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SNAPSHOT_SRC="$HERE/dev-snapshot"
# shellcheck source=/dev/null
source "$SNAPSHOT_SRC/config.sh"

# --- args --------------------------------------------------------------------

GROUP="core"
ONLY_DB=""
FRESH=0
ASSERT_ONLY=0
SUBSET=0

usage() { sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --group) GROUP="${2:?}"; shift 2 ;;
    --db) ONLY_DB="${2:?}"; shift 2 ;;
    --fresh) FRESH=1; shift ;;
    --assert-only) ASSERT_ONLY=1; shift ;;
    --subset) SUBSET=1; shift ;;   # reserved; full DB is streamed for now
    -h|--help) usage 0 ;;
    *) echo "unknown arg: $1" >&2; usage 2 ;;
  esac
done

die() { echo "ERROR: $*" >&2; exit 1; }

lpsql() { psql -h "$LOCAL_PG_HOST" -p "$LOCAL_PG_PORT" -U "$LOCAL_PG_USER" "$@"; }

refuse_check() {
  local db="$1"
  if [[ "$db" =~ $REFUSED_DB_REGEX ]]; then
    die "REFUSED: '$db' is a letmoe database — it is never in a snapshot. Use letmoe's own seed system (see docs/dev-environment.md)."
  fi
}

in_list() { local n="$1"; shift; local x; for x in "$@"; do [[ "$x" == "$n" ]] && return 0; done; return 1; }

# --- assertions (deliverable B: the pipeline self-proves it desensitised) -----

MK="$SCRUB_MARKER"
FAILS=0

# assert_zero <label> <db> <count-sql>  — count-sql MUST return 0.
assert_zero() {
  local label="$1" db="$2" sql="$3" n
  n="$(lpsql -Atc "$sql" -d "${db}${DEVDB_SUFFIX}" 2>/dev/null | tr -d '[:space:]')"
  if [[ "$n" != "0" ]]; then
    echo "   ASSERT FAIL [$db] $label: expected 0, got ${n:-<query-error>}" >&2
    FAILS=$((FAILS+1)); return 1
  fi
  echo "   ok  [$db] $label"
}

# content_col_synthetic <db> <table> <col>  — every non-empty value carries MK.
content_col_synthetic() {
  assert_zero "$2.$3 all synthetic" "$1" \
    "SELECT count(*) FROM \"$2\" WHERE \"$3\" IS NOT NULL AND \"$3\" <> '' AND \"$3\" NOT LIKE '%${MK}%'"
}

assert_infra() {
  local db="kun_galgame_infra"
  assert_zero "no real emails"        "$db" "SELECT count(*) FROM users WHERE email IS NOT NULL AND email <> '' AND email NOT LIKE '%@${DEV_EMAIL_DOMAIN}'"
  assert_zero "no real original_email" "$db" "SELECT count(*) FROM users WHERE original_email IS NOT NULL AND original_email <> '' AND original_email NOT LIKE '%@${DEV_EMAIL_DOMAIN}'"
  assert_zero "no real source_email"  "$db" "SELECT count(*) FROM user_migrations WHERE source_email IS NOT NULL AND source_email <> '' AND source_email NOT LIKE '%@${DEV_EMAIL_DOMAIN}'"
  assert_zero "users.ip cleared"      "$db" "SELECT count(*) FROM users WHERE ip IS NOT NULL AND ip <> ''"
  assert_zero "sessions empty"        "$db" "SELECT count(*) FROM sessions"
  assert_zero "authorization_codes empty" "$db" "SELECT count(*) FROM authorization_codes"
  assert_zero "password_resets empty" "$db" "SELECT count(*) FROM password_resets"
  assert_zero "signing_keys empty"    "$db" "SELECT count(*) FROM signing_keys"
  assert_zero "oauth tokens cleared"  "$db" "SELECT count(*) FROM oauth_accounts WHERE (access_token IS NOT NULL AND access_token<>'') OR (refresh_token IS NOT NULL AND refresh_token<>'')"
  # oauth_clients secrets = dev derivation (computed in shell, no pgcrypto needed).
  local bad=0 cid secret expect
  while IFS='|' read -r cid secret; do
    [[ -z "$cid" ]] && continue
    expect="sha256:$(printf '%s%s' "$DEV_SECRET_PREFIX" "$cid" | sha256sum | awk '{print $1}')"
    [[ "$secret" == "$expect" ]] || { echo "   ASSERT FAIL [$db] oauth secret != dev derivation for client $cid" >&2; bad=1; }
  done < <(lpsql -Atc "SELECT id||'|'||secret FROM oauth_clients" -d "${db}${DEVDB_SUFFIX}")
  if [[ "$bad" == "0" ]]; then echo "   ok  [$db] all oauth_clients secrets = dev derivation"; else FAILS=$((FAILS+1)); fi
}

run_assertions() {
  local db="$1"
  echo ">> asserting desensitisation for $db"
  case "$db" in
    kun_galgame_infra) assert_infra ;;
    kungalgame)
      content_col_synthetic kungalgame chat_message content
      content_col_synthetic kungalgame message content
      content_col_synthetic kungalgame chat_room last_message_content ;;
    kungalgame_patch)
      content_col_synthetic kungalgame_patch chat_message content
      content_col_synthetic kungalgame_patch user_message content
      content_col_synthetic kungalgame_patch chat_message_edit_history previous_content
      assert_zero "user.ip cleared" kungalgame_patch "SELECT count(*) FROM \"user\" WHERE ip IS NOT NULL AND ip <> ''" ;;
    kun_community)
      assert_zero "held posts content synthetic" kun_community \
        "SELECT count(*) FROM community_post WHERE status=1 AND (content_raw NOT LIKE '%${MK}%' OR content_html NOT LIKE '%${MK}%')"
      content_col_synthetic kun_community community_flag note ;;
    kun_chat)
      assert_zero "live chat text synthetic" kun_chat \
        "SELECT count(*) FROM chat_message WHERE deleted_at IS NULL AND kind = 'message' AND text NOT LIKE '%${MK}%'"
      assert_zero "drafts cleared" kun_chat "SELECT count(*) FROM chat_member WHERE draft IS NOT NULL" ;;
    kun_images)
      assert_zero "first_uploader_ip cleared" kun_images "SELECT count(*) FROM images WHERE first_uploader_ip IS NOT NULL AND first_uploader_ip <> ''" ;;
    kun_news)
      content_col_synthetic kun_news news_moderation_decision reason ;;
    *) echo "   (no PII assertions defined for $db — passthrough)" ;;
  esac
}

# --- restore primitives ------------------------------------------------------

recreate_db() {   # drop + create the target DB, terminating live backends first.
  local target="$1"
  lpsql -d postgres -q -c \
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='$target' AND pid<>pg_backend_pid();" >/dev/null 2>&1
  lpsql -d postgres -q -c "DROP DATABASE IF EXISTS \"$target\";" \
    || die "could not drop $target (is something still connected?)"
  lpsql -d postgres -q -c "CREATE DATABASE \"$target\";" || die "could not create $target"
}

# --- core group --------------------------------------------------------------

do_core() {
  local sel=()
  if [[ -n "$ONLY_DB" ]]; then
    refuse_check "$ONLY_DB"
    in_list "$ONLY_DB" "${CORE_DBS[@]}" || die "'$ONLY_DB' is not a core DB. core = ${CORE_DBS[*]}"
    sel=("$ONLY_DB")
  else
    sel=("${CORE_DBS[@]}")
  fi

  if [[ "$ASSERT_ONLY" == "1" ]]; then
    for db in "${sel[@]}"; do run_assertions "$db"; done
    finish; return
  fi

  # Resolve the server-side artifact directory.
  local snapshot_dir
  if [[ "$FRESH" == "1" ]]; then
    echo ">> --fresh: rebuilding desensitised artifact on $SSH_HOST ..."
    local remote_tmp="/tmp/dev-snapshot-build.$$"
    scp -q -r "$SNAPSHOT_SRC" "$SSH_HOST:$remote_tmp" || die "could not copy build scripts to $SSH_HOST"
    local logf; logf="$(mktemp)"
    ssh "$SSH_HOST" "sudo bash $remote_tmp/build-snapshot.sh ${ONLY_DB:+--db $ONLY_DB}" 2>&1 | tee "$logf"
    ssh "$SSH_HOST" "rm -rf $remote_tmp" 2>/dev/null
    snapshot_dir="$(sed -n 's/^SNAPSHOT_DIR=//p' "$logf" | tail -1)"
    rm -f "$logf"
    [[ -n "$snapshot_dir" ]] || die "server build did not report a snapshot dir (build failed?)"
  else
    snapshot_dir="$(ssh "$SSH_HOST" "ls -1d $SNAPSHOT_ROOT/*/ 2>/dev/null | sort | tail -1" 2>/dev/null | sed 's:/*$::')"
    [[ -n "$snapshot_dir" ]] || die "no artifact on $SSH_HOST:$SNAPSHOT_ROOT — run again with --fresh"
  fi
  echo ">> using server artifact: $snapshot_dir"

  # Download only what we need, into a self-cleaning temp dir.
  local dl; dl="$(mktemp -d)"
  trap 'rm -rf "$dl"' EXIT
  scp -q "$SSH_HOST:$snapshot_dir/manifest.txt" "$dl/" 2>/dev/null || true
  for db in "${sel[@]}"; do
    scp -q "$SSH_HOST:$snapshot_dir/$db.dump" "$dl/" \
      || die "artifact missing for '$db' at $snapshot_dir (rebuild with --fresh)"
  done

  for db in "${sel[@]}"; do
    local target="${db}${DEVDB_SUFFIX}"
    echo ">> restoring $db -> $target"
    recreate_db "$target"
    if ! pg_restore -h "$LOCAL_PG_HOST" -p "$LOCAL_PG_PORT" -U "$LOCAL_PG_USER" \
        -j4 --no-owner --no-acl -d "$target" "$dl/$db.dump"; then
      echo "   (pg_restore reported non-fatal warnings for $db; assertions below are the gate)" >&2
    fi
    run_assertions "$db"
  done

  if [[ -f "$dl/manifest.txt" ]]; then
    echo ">> manifest summary:"; grep -E '^# (--- summary|[a-z])' "$dl/manifest.txt" | sed 's/^# /   /'
  fi
  if in_list "kun_galgame_infra" "${sel[@]}"; then
    seed_devportal_client
    credential_card
  fi
  finish
}

# The portal is SSO-only. refresh restores the prod oauth_clients set, whose
# NextMoe 开发者平台 row only accepts https://developer.nextmoe.dev/auth/callback.
# Without this upsert, local :9430 authorize answers 无效的客户端.
seed_devportal_client() {
  local db="kun_galgame_infra${DEVDB_SUFFIX}"
  local hash
  hash=$(printf %s 'dev-secret-devportal-dev' | sha256sum | cut -d' ' -f1)
  lpsql -d "$db" -v ON_ERROR_STOP=1 <<SQL
INSERT INTO oauth_clients
  (id, name, secret, redirect_uris, grants, is_public, auto_consent,
   refresh_token_ttl_seconds, allowed_scopes,
   image_enabled, catalog_site,
   dev_enabled, dev_tier, dev_rate_per_min, dev_quota_daily,
   dev_review_status)
VALUES
  ('devportal-dev', 'NextMoe 开发者平台 (dev)', 'sha256:$hash',
   '["http://127.0.0.1:9430/auth/callback"]'::jsonb,
   '["authorization_code","refresh_token"]'::jsonb,
   false, true, 7776000, '["openid","profile","email"]'::jsonb,
   false, '',
   false, '', 0, 0,
   'approved')
ON CONFLICT (id) DO UPDATE SET
  secret = EXCLUDED.secret,
  redirect_uris = EXCLUDED.redirect_uris,
  grants = EXCLUDED.grants,
  auto_consent = EXCLUDED.auto_consent,
  allowed_scopes = EXCLUDED.allowed_scopes;
SQL
  echo "   re-seeded oauth_clients.devportal-dev (SSO for :9430)"
}

# credential_card prints the dev credential contract every time kun_galgame_infra
# is refreshed. The restore REPLACES users and oauth_clients with the (scrubbed)
# prod set, so any consumer still holding pre-refresh credentials breaks in two
# characteristic ways: S2S calls answer 401 "客户端密钥无效" (stale client
# secret in a product repo's .env) and logins answer 邮箱或密码错误 (old dev
# account emails no longer exist). Both are cured by the contract below — never
# by UPDATEing secrets in the DB, which the next refresh would wipe again.
credential_card() {
  local db="kun_galgame_infra${DEVDB_SUFFIX}"
  echo ""
  echo ">> dev credential contract (docs/dev-environment.md — re-derived on EVERY refresh):"
  echo "   logins:        email user<id>@${DEV_EMAIL_DOMAIN}  password kungal-dev   (every account)"
  echo "   client secret: dev-secret-<client_id>  — set this in each product repo's .env, e.g.:"
  lpsql -Atc "SELECT id, name FROM oauth_clients ORDER BY site_id NULLS LAST, name" -d "$db" 2>/dev/null \
    | awk -F'|' '{printf "     %-34s dev-secret-%s  (%s)\n", $1, $1, $2}' \
    || echo "     (could not list oauth_clients — query $db manually)"
  echo "   re-seeded:     oauth_clients.devportal-dev (portal SSO on :9430)"
  echo "   hand-seeded dev-only rows (e.g. letmoe-dev) were WIPED by this refresh — re-seed"
  echo "   them per docs/dev-environment.md if a repo depends on one."
}

# --- sources group -----------------------------------------------------------

do_sources() {
  local sel=()
  if [[ -n "$ONLY_DB" ]]; then
    refuse_check "$ONLY_DB"
    in_list "$ONLY_DB" "${SOURCE_DBS[@]}" || die "'$ONLY_DB' is not a source DB. sources = ${SOURCE_DBS[*]}"
    sel=("$ONLY_DB")
  else
    sel=("${SOURCE_DBS[@]}")
  fi
  [[ "$SUBSET" == "1" ]] && echo ">> note: --subset is reserved; streaming the full DB."

  for db in "${sel[@]}"; do
    local target="${db}${DEVDB_SUFFIX}"
    echo ">> streaming source $db -> $target (raw public scrape data, no desensitisation)"
    recreate_db "$target"
    # Serial restore (no -j4): a parallel pg_restore needs a seekable file, which
    # a piped stream is not. Streaming trades parallelism for zero local staging.
    ssh "$SSH_HOST" "sudo docker exec $PG_CONTAINER pg_dump -Fc -U postgres -d $db" \
      | pg_restore -h "$LOCAL_PG_HOST" -p "$LOCAL_PG_PORT" -U "$LOCAL_PG_USER" --no-owner --no-acl -d "$target"
    local rc=("${PIPESTATUS[@]}")
    [[ "${rc[0]}" == "0" ]] || die "ssh/pg_dump of $db failed (rc=${rc[0]})"
    echo "   ok  streamed $db"
  done
  finish
}

finish() {
  if [[ "$FAILS" -gt 0 ]]; then
    echo ">> FAILED: $FAILS assertion(s) did not pass." >&2
    exit 1
  fi
  echo ">> done."
  exit 0
}

# --- dispatch ----------------------------------------------------------------

case "$GROUP" in
  core) do_core ;;
  sources) do_sources ;;
  *) die "unknown group '$GROUP' (use core|sources)" ;;
esac
