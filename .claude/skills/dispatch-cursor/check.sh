#!/usr/bin/env bash
# Read one dispatch's stream back and say whether the run can be trusted. dispatch.sh calls it
# when the run ends; call it by hand on a run that was killed.
#
#   check.sh <out-dir> [head-before] [exit-code] [elapsed-seconds] [kill-reason]
#
# Run it from the executor's worktree when passing head-before. Exits non-zero on any failure.
set -uo pipefail

out="${1:?usage: check.sh <out-dir> [head-before] [exit-code] [elapsed] [reason]}"
head_before="${2:-}"
code="${3:-0}"
elapsed="${4:-?}"
reason="${5:-}"
stream="$out/stream.jsonl"
fail=0

jq -c 'select(.type == "result")' "$stream" 2>/dev/null | tail -n 1 >"$out/run.json" || true
calls=$(jq -s '[.[] | select(.type == "tool_call" and .subtype == "completed")] | length' "$stream" 2>/dev/null || echo '?')
model=$(jq -r 'select(.type == "system" and .subtype == "init") | .model' "$stream" 2>/dev/null | head -n 1 || true)

if [ ! -s "$out/run.json" ]; then
  echo "dispatch: no result event (exit=$code elapsed=${elapsed}s calls=$calls ${reason}); see $out/stderr.log" >&2
  tail -c 2000 "$out/stderr.log" >&2 2>/dev/null || true
  fail=1
else
  jq -r --arg e "$elapsed" --arg c "$calls" --arg m "$model" --arg o "$out" \
    '"subtype=\(.subtype) is_error=\(.is_error) elapsed=\($e)s tool_calls=\($c) model=\($m) tokens_in=\(.usage.inputTokens // "?") cache_read=\(.usage.cacheReadTokens // "?") tokens_out=\(.usage.outputTokens // "?") out=\($o)"' \
    "$out/run.json"
  if [ "$(jq -r '.is_error' "$out/run.json")" != false ]; then
    echo "dispatch: the result event reports an error; inspect $stream" >&2
    fail=1
  fi
fi
[ -z "$reason" ] || echo "dispatch: $reason" >&2

shells='select(.type == "tool_call" and .subtype == "completed") | .tool_call.shellToolCall // empty'

# The sandbox fails open: --force, an allow-listed command or approvalMode "unrestricted" each run
# the command unsandboxed, and the stream still shows the sandbox policy that was requested.
# Every preflight line must be the fenced one: an executor that ran step 0 twice printed two
# correct lines, and comparing the joined pair to one line failed a fenced run.
preflight=$(jq -r "$shells | .result.success.stdout // empty" "$stream" 2>/dev/null | grep '^dispatch-preflight:' || true)
unfenced=$(printf '%s\n' "$preflight" | grep -vx 'dispatch-preflight: sandbox=native net=blocked loopback=private' || true)
if [ -z "$preflight" ] || [ -n "$unfenced" ]; then
  echo "dispatch: FENCE NOT PROVEN; treat every shell call in this run as unsandboxed. Preflight said: ${preflight:-nothing}" >&2
  fail=1
fi

escaped=$(jq -r "$shells | select(.result.success and .args.requestedSandboxPolicy == null) | .args.command" "$stream" 2>/dev/null || true)
if [ -n "$escaped" ]; then
  echo "dispatch: $(printf '%s\n' "$escaped" | wc -l) shell call(s) RAN WITHOUT A SANDBOX POLICY:" >&2
  printf '%s\n' "$escaped" | cut -c1-200 >&2
  fail=1
fi

refused=$(jq -r 'select(.type == "tool_call" and .subtype == "completed") | .tool_call | to_entries[]
  | select(.key | endswith("ToolCall")) | .key as $tool | (.value.result // {})
  | if .permissionDenied then "denied   \(.permissionDenied.command // $tool)"
    elif .writePermissionDenied then "denied   \(.writePermissionDenied.error // $tool)"
    elif .rejected then "rejected \(.rejected.command // (.rejected.path | select(. != "")) // $tool)"
    else empty end' "$stream" 2>/dev/null || true)
if [ -n "$refused" ]; then
  echo "dispatch: $(printf '%s\n' "$refused" | wc -l) call(s) were refused; check each rule is right:" >&2
  printf '%s\n' "$refused" | cut -c1-200 | sort | uniq -c >&2
fi

if [ -n "$head_before" ] && [ "$(git rev-parse HEAD)" != "$head_before" ]; then
  echo "dispatch: HEAD MOVED during the run ($head_before -> $(git rev-parse HEAD)); the executor committed" >&2
  fail=1
fi

if [ -s "$out/stderr.log" ]; then
  echo '--- stderr ---' >&2
  tail -c 2000 "$out/stderr.log" >&2
fi
if [ "$code" -ne 0 ] && [ -z "$reason" ]; then
  echo "dispatch: cursor-agent exited $code; inspect $out/stderr.log" >&2
  fail=1
fi
exit "$fail"
