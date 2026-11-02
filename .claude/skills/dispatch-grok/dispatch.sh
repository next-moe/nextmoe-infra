#!/usr/bin/env bash
# Dispatch one grok executor run. See SKILL.md in this directory.
#
#   GROK_OUT_ROOT=<dir under /tmp> dispatch.sh <slug> [extra grok args...]
#
# Expects <GROK_OUT_ROOT>/<slug>/task.md to exist. Writes run.json, stderr.log and
# debug.log beside it. Extra arguments are passed straight through to grok — that is
# where the per-task --deny 'Write(<glob>)' rules for out-of-scope paths go.
#
# Knobs: GROK_SANDBOX_PROFILE (default workspace; read-only for audits),
# GROK_MAX_TURNS (default 200).
#
# Run this in the background: a real dispatch takes minutes.
set -euo pipefail

slug="${1:?usage: dispatch.sh <slug> [extra grok args...]}"
shift

root="${GROK_OUT_ROOT:?set GROK_OUT_ROOT to a writable directory under /tmp}"
out="$root/$slug"
[ -f "$out/task.md" ] || { echo "dispatch: missing $out/task.md" >&2; exit 2; }

repo="$(git rev-parse --show-toplevel)"

# --allow is not a closed allowlist, so it is not a fence (SKILL.md §2). Without
# --always-approve a headless refusal prints "permission required:" and can still
# exit 0. --cwd is the repo root: workspace writes the session directory.
DENY_ARGS=()
for f in "$repo/apps/api/.env" "$repo/.env"; do
  DENY_ARGS+=(--deny "Read($f)" --deny "Write($f)")
done

grok --prompt-file "$out/task.md" \
     --cwd "$repo" \
     --always-approve \
     --sandbox "${GROK_SANDBOX_PROFILE:-workspace}" \
     "${DENY_ARGS[@]}" \
     --output-format json \
     --max-turns "${GROK_MAX_TURNS:-200}" \
     --debug-file "$out/debug.log" \
     "$@" \
     >"$out/run.json" 2>"$out/stderr.log" || true

if [ ! -s "$out/run.json" ]; then
  echo "dispatch: grok produced no JSON; see $out/stderr.log" >&2
  tail -c 2000 "$out/stderr.log" >&2 || true
  exit 1
fi

stop=$(jq -r '.stopReason // "?"' "$out/run.json")
turns=$(jq -r '.num_turns // "?"' "$out/run.json")
cost=$(jq -r '.total_cost_usd // "?"' "$out/run.json")
# A refusal comes back to the model as a tool result and the run still ends end_turn,
# and the model need not repeat it in .text; the debug log records each one.
denied=$(grep -c -e 'deny rule matched' -e 'could not be checked against the permission rules' "$out/debug.log" 2>/dev/null || true)
denied=${denied:-0}
printf 'stopReason=%s turns=%s cost_usd=%s refused_tool_calls=%s out=%s\n' "$stop" "$turns" "$cost" "$denied" "$out"

[ -s "$out/stderr.log" ] && { echo '--- stderr ---' >&2; tail -c 2000 "$out/stderr.log" >&2; }

if [ "$denied" != "0" ]; then
  echo "dispatch: $denied tool call(s) refused by a deny rule or as uncheckable — check the report for routed-around steps" >&2
fi

# Headless prints this once and can still exit 0. --always-approve makes it unreachable
# unless a later flag overrides the mode.
if [ -s "$out/stderr.log" ] && grep -q 'permission required:' "$out/stderr.log"; then
  echo 'dispatch: a tool call was refused because headless cannot prompt; the process may still have exited 0' >&2
  exit 1
fi

if [ "$stop" != "end_turn" ]; then
  if [ "$turns" -ge "${GROK_MAX_TURNS:-200}" ] 2>/dev/null; then
    echo 'dispatch: turn budget exhausted — raise GROK_MAX_TURNS or split the task' >&2
  else
    echo "dispatch: run ended as '$stop'; inspect $out/debug.log" >&2
  fi
  exit 1
fi
