#!/usr/bin/env bash
# 09-open-api-phase2 · 01 byte-compat replay (验证矩阵 gate 1). Proves the new
# devapi-gated /internal rich read face is byte-identical to the existing
# internal /api read face — SAME cmd/catalog process, SAME database, only the
# path prefix and the front gate differ.
#
#   A = ...   (default /api      — no credential)
#   B = ...   (default /internal — X-API-Key: <internal key>)
#
# Derived from scripts/wiki-retirement/replay-compare.sh: same jq -S canonical
# + STRIP(.view) normalization and the same two-tier strict/deep comparison
# (a body that matches only after recursive array sorting is a heap TIE, not a
# data diff). Covers all 44 read routes with >=20 sampled gids, PLUS the
# dual-credential samples (/mine, /messages/mine, /search?include_pending=true,
# /batch) which carry the SAME locally-signed HS256 dev-user JWT on both sides
# (B additionally carries the internal key in X-API-Key — the dual-credential
# transport this wave introduced).
#
# content_limit=all routes are included on purpose: the internal face must pass
# content_limit through UNTOUCHED (no sfwGate — 裁定 4/复核修订 2), so an A/B
# match on content_limit=all is itself the proof the passthrough is intact.
#
# Usage:
#   source apps/api/.env
#   INTERNAL_KEY=nm_live_... JWT=<hs256 dev jwt> \
#     scripts/open-api-phase2/replay-internal-face.sh
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/../wiki-retirement/lib.sh" # psql_val + KUN_PG_* connection env

BASE="${BASE:-http://127.0.0.1:9281}"
# Gate 1 (internal vs api): same base, A_PREFIX=/api B_PREFIX=/internal.
# Gate 3 (old-face no-disturbance): set A_BASE=baseline B_BASE=candidate with
# A_PREFIX=B_PREFIX=/api to diff the /api face pre/post the mount refactor.
A_BASE="${A_BASE:-$BASE}"
B_BASE="${B_BASE:-$BASE}"
A_PREFIX="${A_PREFIX:-/api}"
B_PREFIX="${B_PREFIX:-/internal}"
SOURCE_DB="${SOURCE_DB:-kun_galgame_wiki}" # local galgame content DB
INTERNAL_KEY="${INTERNAL_KEY:?set INTERNAL_KEY (an internal-tier devapi key)}"
JWT="${JWT:-}" # optional end-user JWT; required for the dual-credential samples
# STRIP removes non-deterministic fields before comparison:
#   .view                — galgame detail GET async-increments the view counter
#                          (traffic/timing dependent), same as replay-compare.sh.
#   .processing_time_ms  — Meilisearch reports per-query latency in the search
#                          envelope; it varies request-to-request, never data.
STRIP="${STRIP:-walk(if type==\"object\" then del(.view, .processing_time_ms) else . end)}"
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

urlenc() { jq -rn --arg s "$1" '$s|@uri'; }

# --- representative id samples from the source DB ---------------------------
GIDS=$(psql_val "$SOURCE_DB" "SELECT id FROM galgame WHERE status=0 ORDER BY id LIMIT 12")
GIDS+=$'\n'$(psql_val "$SOURCE_DB" "SELECT id FROM galgame WHERE status=0 ORDER BY random() LIMIT 8")
GID_CSV=$(echo "$GIDS" | paste -sd, -)
TAG_ID=$(psql_val    "$SOURCE_DB" "SELECT id FROM galgame_tag ORDER BY id LIMIT 1")
TAG_NAME=$(psql_val  "$SOURCE_DB" "SELECT name FROM galgame_tag WHERE name<>'' ORDER BY id LIMIT 1")
OFF_ID=$(psql_val    "$SOURCE_DB" "SELECT id FROM galgame_official ORDER BY id LIMIT 1")
OFF_NAME=$(psql_val  "$SOURCE_DB" "SELECT name FROM galgame_official WHERE name<>'' ORDER BY id LIMIT 1")
ENG_ID=$(psql_val    "$SOURCE_DB" "SELECT id FROM galgame_engine ORDER BY id LIMIT 1")
ENG_NAME=$(psql_val  "$SOURCE_DB" "SELECT name FROM galgame_engine WHERE name<>'' ORDER BY id LIMIT 1")
SER_ID=$(psql_val    "$SOURCE_DB" "SELECT id FROM galgame_series ORDER BY id LIMIT 1")
MONTH=$(psql_val     "$SOURCE_DB" "SELECT to_char(release_date,'YYYY-MM') FROM galgame WHERE release_date IS NOT NULL AND status=0 ORDER BY random() LIMIT 1")
VNDB=$(psql_val      "$SOURCE_DB" "SELECT vndb_id FROM galgame WHERE vndb_id<>'' AND status=0 ORDER BY id LIMIT 1")
# A real taxonomy revision triple, if any (else fall back to id/rev=1: both
# faces then 404 identically, which still proves the route mirrors).
TAXREV=$(psql_val "$SOURCE_DB" "SELECT entity||' '||target_id||' '||revision FROM taxonomy_revision ORDER BY id LIMIT 1" 2>/dev/null || true)
REV_ENT=$(echo "$TAXREV" | awk '{print $1}'); REV_ID=$(echo "$TAXREV" | awk '{print $2}'); REV_N=$(echo "$TAXREV" | awk '{print $3}')

# --- 44 read routes as prefix-relative paths (A gets /api, B gets /internal) -
Q=$(urlenc "恋"); TAGN=$(urlenc "$TAG_NAME"); OFFN=$(urlenc "$OFF_NAME"); ENGN=$(urlenc "$ENG_NAME"); KW=$(urlenc "夏")
# PUB = deterministic routes → byte-compared (strict/deep). SEARCH = the four
# /search routes → status+shape compared, NOT byte-compared: they are inherently
# non-deterministic across ANY two calls (series SearchGalgames is a SQL
# `LIMIT 20` with NO ORDER BY → Postgres returns an arbitrary window per
# execution; galgame/tag/official are Meilisearch relevance-tie ordered). This
# is a pre-existing property of the endpoints on BOTH faces (proven by an
# A-vs-A control returning different sets), so byte-equality is meaningless for
# them — the correct proof is that /internal routes to the SAME handler and
# returns the SAME envelope+item shape and status. The wiki-retirement
# replay-compare.sh excluded /search for the same reason.
PUB=(
  # galgame list + shape variants (content_limit=all = passthrough proof)
  "/galgame/?page=1&limit=24"
  "/galgame/?page=2&limit=24&sort_field=updated&sort_order=desc"
  "/galgame/?page=1&limit=24&content_limit=all"
  "/galgame/batch?ids=$GID_CSV&view=detail"
  "/galgame/batch?ids=$GID_CSV&view=brief"
  "/galgame/batch?ids=$GID_CSV&view=detail&content_limit=all"
  "/galgame/drafts?page=1&limit=24"
  "/galgame/check?vndb_id=$VNDB"
  "/galgame/user/1/stats"
  "/galgame/user/1/galgames?page=1&limit=24"
  "/galgame/user/1/contributed?page=1&limit=24"
  "/galgame/calendar?month=$MONTH"
  "/galgame/calendar?month=$MONTH&content_limit=all"
  "/galgame/calendar/pending"
  "/galgame/calendar/tba"
  "/galgame/stats"
  "/galgame/officials/$OFF_ID/galgames?page=1&limit=24"
  "/galgame/tags/$TAG_ID/galgames?page=1&limit=24"
  # tag (7 — /search is in SEARCH below)
  "/tag/?page=1&limit=24"
  "/tag/multi?ids=$TAG_ID"
  "/tag/$TAGN"
  "/tag/$TAG_ID/galgame-ids"
  "/tag/$TAG_ID/revisions?page=1&limit=20"
  # official (6 — /search below)
  "/official/?page=1&limit=24"
  "/official/$OFFN"
  "/official/$OFF_ID/galgame-ids"
  "/official/$OFF_ID/revisions?page=1&limit=20"
  # engine (5)
  "/engine/?page=1&limit=24"
  "/engine/$ENGN"
  "/engine/$ENG_ID/galgame-ids"
  "/engine/$ENG_ID/revisions?page=1&limit=20"
  # series (5 — /search below)
  "/series/?page=1&limit=24"
  "/series/$SER_ID"
  "/series/$SER_ID/revisions?page=1&limit=20"
)
SEARCH=(
  "/galgame/search?q=$Q&page=1&limit=12"
  "/tag/search?q=$Q"
  "/official/search?q=$Q"
  "/series/search?keywords=$KW"
)
# revisions/:rev — use a real triple when present (routes the entity's own rev
# handler); otherwise a synthetic id/rev=1 (both faces 404 identically).
if [ -n "${REV_ENT:-}" ]; then
  PUB+=("/$REV_ENT/$REV_ID/revisions/$REV_N")
else
  PUB+=("/tag/$TAG_ID/revisions/1" "/official/$OFF_ID/revisions/1" "/engine/$ENG_ID/revisions/1" "/series/$SER_ID/revisions/1")
fi

# per-gid detail + sub-resources (>=20 gids)
for g in $GIDS; do
  PUB+=("/galgame/$g" "/galgame/$g/links" "/galgame/$g/aliases" "/galgame/$g/contributors" "/galgame/$g/scores")
done

# dual-credential samples (JWT on BOTH sides; B also carries the internal key).
# /mine, /messages/mine, /batch are deterministic → byte-compared. The
# include_pending search is non-deterministic (see SEARCH) → shape-compared.
DUAL=(
  "/galgame/mine"
  "/galgame/messages/mine"
  "/galgame/batch?ids=$GID_CSV&view=detail"
)
DUAL_SEARCH=(
  "/galgame/search?q=$Q&include_pending=true&page=1&limit=12"
)

strict() { jq -S "$STRIP" "$WORK/$1.body" 2>/dev/null || cat "$WORK/$1.body"; }
deep()   { jq -S "$STRIP"' | walk(if type=="array" then sort_by(tojson) else . end)' "$WORK/$1.body" 2>/dev/null || cat "$WORK/$1.body"; }
# shape() = the response ENVELOPE skeleton, independent of which items a
# non-deterministic search returns: the code and the top-level data container
# (its sorted object keys, or "array"). This comes from the handler's response
# wrapper, not the data, so it is stable across calls. Item-level shape is NOT
# asserted here (some galgame fields are omitempty, so the per-item key set
# legitimately varies with which rows a random LIMIT returns) — the full item
# schema is already byte-proven on 100+ deterministic routes (list/batch/detail
# all return the same galgame item type). status + envelope is the correct proof
# that /internal/search routes to the SAME handler as /api/search.
shape() {
  jq -S '{code, data: (.data | if type=="object" then keys elif type=="array" then "array" else type end)}' \
    "$WORK/$1.body" 2>/dev/null || cat "$WORK/$1.body"
}

ok=0; tie=0; diff=0; err=0; total=0; shp=0; tielist=""
compare() { # relpath  use_jwt(0|1)
  local rel="$1" usejwt="$2" sa sb
  local -a ah=() bh=(-H "X-API-Key: $INTERNAL_KEY")
  if [ "$usejwt" = 1 ]; then
    [ -z "$JWT" ] && { echo "SKIP(no JWT) $rel"; return; }
    ah+=(-H "Authorization: Bearer $JWT"); bh+=(-H "Authorization: Bearer $JWT")
  fi
  total=$((total+1))
  sa=$(curl -s -m 25 "${ah[@]}" -o "$WORK/a.body" -w '%{http_code}' "$A_BASE$A_PREFIX$rel") || { echo "ERR curl A $rel"; err=$((err+1)); return; }
  sb=$(curl -s -m 25 "${bh[@]}" -o "$WORK/b.body" -w '%{http_code}' "$B_BASE$B_PREFIX$rel") || { echo "ERR curl B $rel"; err=$((err+1)); return; }
  if [ "$sa" != "$sb" ]; then echo "DIFF(status $sa!=$sb) $rel"; diff=$((diff+1)); return; fi
  if cmp -s <(strict a) <(strict b); then ok=$((ok+1)); return; fi
  if cmp -s <(deep a) <(deep b); then tie=$((tie+1)); tielist+="  TIE(order-only) $rel"$'\n'; return; fi
  echo "DIFF(body) $rel"; diff <(strict a) <(strict b) | head -20; diff=$((diff+1))
}
compare_shape() { # relpath  use_jwt(0|1) — non-deterministic /search: status + shape only
  local rel="$1" usejwt="$2" sa sb
  local -a ah=() bh=(-H "X-API-Key: $INTERNAL_KEY")
  if [ "$usejwt" = 1 ]; then
    [ -z "$JWT" ] && { echo "SKIP(no JWT) $rel"; return; }
    ah+=(-H "Authorization: Bearer $JWT"); bh+=(-H "Authorization: Bearer $JWT")
  fi
  total=$((total+1))
  sa=$(curl -s -m 25 "${ah[@]}" -o "$WORK/a.body" -w '%{http_code}' "$A_BASE$A_PREFIX$rel") || { echo "ERR curl A $rel"; err=$((err+1)); return; }
  sb=$(curl -s -m 25 "${bh[@]}" -o "$WORK/b.body" -w '%{http_code}' "$B_BASE$B_PREFIX$rel") || { echo "ERR curl B $rel"; err=$((err+1)); return; }
  if [ "$sa" != "$sb" ]; then echo "DIFF(status $sa!=$sb) $rel"; diff=$((diff+1)); return; fi
  if cmp -s <(shape a) <(shape b); then shp=$((shp+1)); return; fi
  echo "DIFF(shape) $rel"; diff <(shape a) <(shape b) | head -20; diff=$((diff+1))
}

echo "BASE=$BASE  A=$A_PREFIX  B=$B_PREFIX (X-API-Key)  JWT=$([ -n "$JWT" ] && echo set || echo unset)"
echo "gids=$(echo "$GIDS" | tr '\n' ' ')"
echo "tag=$TAG_ID($TAG_NAME) off=$OFF_ID eng=$ENG_ID series=$SER_ID month=$MONTH vndb=$VNDB taxrev='${TAXREV:-none}'"
echo "replaying ${#PUB[@]} byte + ${#SEARCH[@]} search-shape + ${#DUAL[@]} dual-byte + ${#DUAL_SEARCH[@]} dual-search-shape routes"
for r in "${PUB[@]}";         do compare       "$r" 0; done
for r in "${DUAL[@]}";        do compare       "$r" 1; done
for r in "${SEARCH[@]}";      do compare_shape "$r" 0; done
for r in "${DUAL_SEARCH[@]}"; do compare_shape "$r" 1; done
echo "-----------------------------------------------"
[ -n "$tielist" ] && { echo "order-only ties (same data, heap tie-order):"; printf '%s' "$tielist"; }
echo "TOTAL=$total  BYTE_IDENTICAL=$ok  TIE_ORDER_ONLY=$tie  SEARCH_SHAPE_MATCH=$shp  REAL_DIFF=$diff  CURL_ERR=$err"
[ "$diff" = 0 ] && [ "$err" = 0 ] && { echo "RESULT: PASS (internal face byte-identical to /api read face; /search routes shape+status-identical, non-deterministic by design)"; exit 0; } || { echo "RESULT: FAIL"; exit 1; }
