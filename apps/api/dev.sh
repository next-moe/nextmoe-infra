#!/usr/bin/env bash
# Process runner: starts all pre-built Go services concurrently.
# Called by air after a successful build. Air handles file watching and rebuild.

cd "$(dirname "$0")" || exit 1

# Colors
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BRIGHT_BLUE='\033[0;94m'
GRAY='\033[0;90m'
NC='\033[0m'

# Tracked binary PIDs (killed on exit). core_pids is the subset whose exit tears
# the whole stack down so air rebuilds; artifact is deliberately NOT in it — it
# needs B2/S3 (often unconfigured in local dev) and must never stop the rest.
pids=()
core_pids=()

cleanup() {
    # Clear traps first to avoid re-entry if kill propagates a signal.
    trap - SIGINT SIGTERM
    for pid in "${pids[@]}"; do
        kill "$pid" 2>/dev/null
    done
    wait 2>/dev/null
}
# Only INT/TERM — not EXIT. An EXIT trap combined with killing children
# would race with air's process-group handling and could kill air itself.
trap cleanup SIGINT SIGTERM

label() {
    local color=$1 name=$2
    while IFS= read -r line; do
        # Use %b for colors, %s for all dynamic content — prevents printf
        # from interpreting stray % in log lines (e.g. "100%", "%s").
        printf '%b%s %b|%b %s\n' "$color" "$name" "$GRAY" "$NC" "$line"
    done
}

start() {
    local color=$1 name=$2 bin=$3 core=$4
    if [[ ! -x $bin ]]; then
        printf '%b%s %b|%b binary not found or not executable: %s\n' \
            "$color" "$name" "$GRAY" "$NC" "$bin" >&2
        return 1
    fi
    # Process substitution (not pipe) so $! captures the binary's PID,
    # not the label subshell's. This lets us kill and wait on the right thing.
    "$bin" > >(label "$color" "$name") 2>&1 &
    local pid=$!
    pids+=("$pid")
    [[ $core == core ]] && core_pids+=("$pid")
    return 0
}

printf '\n'
printf '%boauth%b      :9277\n'    "$BLUE"    "$NC"
printf '%bcatalog%b    :9281\n'    "$GREEN"   "$NC"
printf '%bcommunity%b  :9282\n'    "$BRIGHT_BLUE" "$NC"
printf '%bartifact%b   :9279\n'    "$YELLOW"  "$NC"
printf '%btrust%b      :9283\n'    "$MAGENTA" "$NC"
printf '%bimage%b      :9278\n'    "$CYAN"    "$NC"
printf '\n'

start "$BLUE"    "oauth     " ./tmp/oauth      core || { cleanup; exit 1; }
start "$GREEN"   "catalog   " ./tmp/catalog    core || { cleanup; exit 1; }
start "$BRIGHT_BLUE" "community " ./tmp/community  core || { cleanup; exit 1; }
start "$MAGENTA" "trust     " ./tmp/trust      core || { cleanup; exit 1; }
start "$CYAN"    "image     " ./tmp/image      core || { cleanup; exit 1; }

# artifact is OPTIONAL in local dev: it needs B2/S3 (KUN_ARTIFACT_S3_*). Start it
# only when configured, and never as "core" — so an unconfigured or broken B2
# never tears down oauth/catalog/trust/image. (cmd/artifact still fail-fasts
# in prod, where it runs as its own service.)
if grep -qE '^[[:space:]]*KUN_ARTIFACT_S3_ACCESS_KEY=[^[:space:]#]' .env 2>/dev/null; then
    start "$YELLOW" "artifact  " ./tmp/artifact \
        || printf '%bartifact  %b|%b failed to start (optional) — continuing without it\n' "$YELLOW" "$GRAY" "$NC"
else
    printf '%bartifact  %b|%b skipped — KUN_ARTIFACT_S3_* not set in .env (configure B2 to enable)\n' \
        "$YELLOW" "$GRAY" "$NC"
fi

# Fail fast on CORE services only: if one exits, tear the rest down so air
# rebuilds. artifact (optional) exiting is ignored here so it can't stop the stack.
wait -n "${core_pids[@]}"
cleanup
exit 1
