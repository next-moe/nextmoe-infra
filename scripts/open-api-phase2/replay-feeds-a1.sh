#!/usr/bin/env bash
# 09-open-api-phase2 · 05 wave · A1 feeds byte-compat replay (gate G-a1-1).
#
# A1 mounts the two S2S cron feeds — /galgame/messages/feed and
# /galgame/revisions/recent — onto the internal-tier devapi face as a pure
# addition; the legacy /api Basic-auth registrations are left byte-untouched
# (their retirement is A2). This script proves the internal-face feeds are
# byte-identical to the legacy /api feeds: SAME cmd/catalog process, SAME
# database, SAME handler instance — only the path prefix and the front gate
# (Basic client-secret vs nm_ internal key) differ.
#
#   A = /api/galgame/<feed>       — Authorization: Basic <client:secret>
#   B = /internal/galgame/<feed>  — X-API-Key: <internal-tier nm_ key>
#
# Each feed is replayed at since_id=0 AND at a mid-range cursor auto-derived
# from the since_id=0 page, so the cursor-pagination path is covered on both
# faces (task: ">=2 since_id samples: 0 and a mid value").
#
# Usage:
#   BASE=http://127.0.0.1:9281 \
#   INTERNAL_KEY=nm_test_... BASIC_CLIENT=<client_id> BASIC_SECRET=<secret> \
#     scripts/open-api-phase2/replay-feeds-a1.sh
set -euo pipefail
BASE="${BASE:-http://127.0.0.1:9281}"
INTERNAL_KEY="${INTERNAL_KEY:?set INTERNAL_KEY (an internal-tier devapi key)}"
BASIC_CLIENT="${BASIC_CLIENT:?set BASIC_CLIENT (OAuth client_id for legacy /api Basic auth)}"
BASIC_SECRET="${BASIC_SECRET:?set BASIC_SECRET (the plaintext secret for that client)}"
BASIC=$(printf '%s:%s' "$BASIC_CLIENT" "$BASIC_SECRET" | base64 -w0)
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

geta() { curl -s -m 25 -H "Authorization: Basic $BASIC" -o "$WORK/a.body" -w '%{http_code}' "$BASE/api/galgame/$1"; }
getb() { curl -s -m 25 -H "X-API-Key: $INTERNAL_KEY" -o "$WORK/b.body" -w '%{http_code}' "$BASE/internal/galgame/$1"; }
canon() { jq -S . "$WORK/$1" 2>/dev/null || cat "$WORK/$1"; }

ok=0; diff=0; total=0
compare() { # feed  querystring  label
  local feed="$1" qs="$2" label="$3" sa sb
  total=$((total+1))
  sa=$(geta "$feed?$qs"); sb=$(getb "$feed?$qs")
  if [ "$sa" != "$sb" ]; then echo "DIFF(status $sa!=$sb) $label"; diff=$((diff+1)); return; fi
  if cmp -s <(canon a.body) <(canon b.body); then
    echo "OK   [$sa] $label ($feed?$qs)"; ok=$((ok+1))
  else
    echo "DIFF(body) $label ($feed?$qs)"; diff <(canon a.body) <(canon b.body) | head -20; diff=$((diff+1))
  fi
}

# Derive a mid-range since_id per feed from the since_id=0 page's item ids
# (both feeds return {data:{items:[{id,...}]}}). Median id = a real cursor in
# the middle of the stream; if too few items, falls back to 0 (still a valid
# second sample — the two faces must still agree).
mid_cursor() { # feed
  getb "$1?since_id=0&limit=500" >/dev/null
  jq -r '(.data.items // [] | map(.id) | sort) as $ids
         | if ($ids|length) >= 2 then $ids[($ids|length/2|floor)] else 0 end' "$WORK/b.body" 2>/dev/null || echo 0
}
MID_MSG=$(mid_cursor "messages/feed"); MID_REV=$(mid_cursor "revisions/recent")
echo "BASE=$BASE  A=/api(Basic $BASIC_CLIENT)  B=/internal(X-API-Key)"
echo "derived mid cursors: messages/feed since_id=$MID_MSG  revisions/recent since_id=$MID_REV"
echo "-----------------------------------------------"
compare "messages/feed"    "since_id=0&limit=50"          "messages since_id=0"
compare "messages/feed"    "since_id=$MID_MSG&limit=50"   "messages since_id=mid($MID_MSG)"
compare "revisions/recent" "since_id=0&limit=50"          "revisions since_id=0"
compare "revisions/recent" "since_id=$MID_REV&limit=50"   "revisions since_id=mid($MID_REV)"
echo "-----------------------------------------------"
echo "TOTAL=$total  BYTE_IDENTICAL=$ok  REAL_DIFF=$diff"
[ "$diff" = 0 ] && { echo "RESULT: PASS (internal-face feeds byte-identical to legacy /api feeds)"; exit 0; } || { echo "RESULT: FAIL"; exit 1; }
