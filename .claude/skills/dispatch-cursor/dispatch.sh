#!/usr/bin/env bash
# Dispatch one cursor-agent executor run. See SKILL.md in this directory.
#
#   CURSOR_OUT_ROOT=<dir> dispatch.sh <slug> [--deny '<rule>']... [extra cursor-agent args...]
#
# Run it from the root of a linked worktree (never the main checkout). Expects
# <CURSOR_OUT_ROOT>/<slug>/task.md. Writes stream.jsonl, run.json, stderr.log and cursor-config/
# beside it. `--deny` adds a per-task rule; everything else passes straight through.
#
# Start it detached (SKILL.md section 3): a real dispatch takes an hour or more.
set -euo pipefail

slug="${1:?usage: dispatch.sh <slug> [--deny rule]... [extra cursor-agent args...]}"
shift

root="${CURSOR_OUT_ROOT:?set CURSOR_OUT_ROOT to a writable directory under the scratchpad}"
out="$root/$slug"
skill="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$out/task.md" ] || { echo "dispatch: missing $out/task.md" >&2; exit 2; }
# The task book travels as one argv string, and Linux caps a single argument at 128 KiB.
[ "$(wc -c <"$out/task.md")" -lt 120000 ] || { echo "dispatch: task.md is over 120 KiB" >&2; exit 2; }

tree="$PWD"
[ "$(git rev-parse --show-toplevel 2>/dev/null)" = "$tree" ] ||
  { echo "dispatch: run from the root of a git worktree" >&2; exit 2; }
[ "$(git rev-parse --path-format=absolute --git-dir)" != "$(git rev-parse --path-format=absolute --git-common-dir)" ] ||
  { echo "dispatch: $tree is a main checkout; dispatch into a linked worktree (iron rule 13)" >&2; exit 2; }

extra=()
pass=()
while [ $# -gt 0 ]; do
  case "$1" in
    --deny) extra+=("${2:?--deny needs a rule}"); shift 2 ;;
    # Each of these turns the sandbox off for every shell call (SKILL.md section 2).
    -f | --force | --yolo | --sandbox | --sandbox=* | --approve-mcps | --auto-review | -w | --worktree | --worktree=*)
      echo "dispatch: refusing $1: it defeats the fence or the worktree discipline" >&2; exit 2 ;;
    *) pass+=("$1"); shift ;;
  esac
done

# Layer two: permission rules. A Shell rule is a glob over each sub-command's whole text, so the
# leading `*` is what catches an absolute path, `env` and `bash -c`; a bare name matches the
# command base. The sandbox already blocks the network, other loopback services and docker, so
# these mostly fence what it leaves open: git writes and credential reads.
deny=(
  'Shell(*git commit*)'   'Shell(*git add*)'       'Shell(*git push*)'
  'Shell(*git reset*)'    'Shell(*git checkout*)'  'Shell(*git switch*)'
  'Shell(*git rebase*)'   'Shell(*git merge*)'     'Shell(*git stash*)'
  'Shell(*git restore*)'  'Shell(*git clean*)'     'Shell(*git branch*)'
  'Shell(*git tag*)'      'Shell(*git worktree*)'  'Shell(*git cherry-pick*)'
  'Shell(*git revert*)'   'Shell(*git am *)'       'Shell(*git apply*)'
  'Shell(*git update-ref*)' 'Shell(*git symbolic-ref*)' 'Shell(*git pull*)'
  'Shell(*git fetch*)'    'Shell(*git remote*)'    'Shell(*git config*)'
  'Shell(*git gc*)'       'Shell(*git prune*)'
  # A global option in front of the subcommand slips past every rule above.
  'Shell(*git -C*)'       'Shell(*git -c *)'       'Shell(*git --git-dir*)'
  'Shell(*git --work-tree*)'
  'Shell(gh)'             'Shell(*bin/gh *)'
  'Shell(ssh)'            'Shell(*bin/ssh *)'      'Shell(scp)'     'Shell(rsync)'
  'Shell(psql)'           'Shell(*bin/psql*)'      'Shell(pg_dump)' 'Shell(pg_restore)'
  'Shell(docker)'         'Shell(*bin/docker*)'    'Shell(*docker compose*)'
  'Shell(sudo)'           'Shell(*bin/sudo *)'     'Shell(pkill)'   'Shell(killall)'
  'Shell(systemctl)'      'Shell(crontab)'         'Shell(*pnpm dev*)'
  'Shell(grok)'           'Shell(agent)'           'Shell(*bin/grok*)'
  'Shell(cursor-agent)'   'Shell(*cursor-agent *)' 'Shell(claude)'  'Shell(codex)'
  'Shell(*.ssh/*)'        'Shell(*.pgpass*)'       'Shell(*/.env*)' 'Shell(* .env*)'
  'Shell(*.codex/*)'      'Shell(*.grok/*)'        'Shell(*gh/hosts*)'
  'Shell(*.docker/config*)' 'Shell(*.npmrc*)'      'Shell(*cli-config.json*)'
  'Shell(*env-llm-key*)'
  "Read($HOME/.ssh/**)"   "Read($HOME/.pgpass)"    "Read($HOME/.codex/**)"
  "Read($HOME/.grok/env)" "Read($HOME/.config/gh/**)" "Read($HOME/.docker/config.json)"
  "Read($HOME/.npmrc)"    "Read($HOME/.cursor/cli-config.json)"
  'Read(**/.env)'         'Read(**/.env.local)'    'Read(**/.env.production)'
  # This skill and the agent rules, so an executor that loads them cannot rewrite its own fence.
  "Write($tree/.claude/**)" "Write($tree/.cursor/**)"
  "Write($tree/CLAUDE.md)"  "Write($tree/AGENTS.md)"
  "Write($tree/**/node_modules/**)"
)

# Layer one: the sandbox. It engages only in allowlist mode, without --force, and for commands
# the allow list does not name; an allowed command runs unsandboxed. So the allow list is empty.
# authInfo is identity only (auth rides CURSOR_API_KEY), and the executor has no use for it.
cfg="$out/cursor-config"
mkdir -p "$cfg" "$out/zdotdir"
jq '.permissions.allow = [] | .permissions.deny = $ARGS.positional
    | .approvalMode = "allowlist" | .sandbox.mode = "enabled" | del(.authInfo)' \
  "$HOME/.cursor/cli-config.json" --args "${deny[@]}" "${extra[@]}" >"$cfg/cli-config.json"

# Layer three: the environment, as defence in depth behind the sandbox's network block.
# ~/.zshenv sources ~/.grok/env, which exports a GitHub token, so the shell is bash and a zsh
# started anyway finds no startup files.
env_fence=(
  -u GITHUB_PERSONAL_ACCESS_TOKEN -u GH_TOKEN -u GITHUB_TOKEN -u SSH_AUTH_SOCK
  -u TEST_DATABASE_DSN -u DATABASE_URL -u PGPASSWORD -u PGHOST -u PGUSER
  SHELL=/bin/bash
  ZDOTDIR="$out/zdotdir"
  PGPASSFILE=/nonexistent/pgpass-denied-by-dispatch
  DOCKER_HOST=unix:///nonexistent/docker-denied-by-dispatch.sock
  GOMAXPROCS=8
  DISPATCH_PREFLIGHT="$skill/preflight.sh"
  GIT_CONFIG_COUNT=4
)
i=0
for scheme in 'https://' 'http://' 'ssh://' 'git@'; do
  env_fence+=(
    "GIT_CONFIG_KEY_$i=url./nonexistent/push-denied-by-dispatch/.pushInsteadOf"
    "GIT_CONFIG_VALUE_$i=$scheme"
  )
  i=$((i + 1))
done
env_fence+=(CURSOR_CONFIG_DIR="$cfg")

# Pinned, so a change to the global config's default cannot reach a run.
model=(--model "${CURSOR_MODEL:-cursor-grok-4.6-xhigh}")

# A listener on the host's loopback for the preflight to fail to reach. A socket that is
# listening completes the handshake without accept(), so reaching it proves the loopback is shared.
python3 -c 'import socket, time
s = socket.socket(); s.bind(("127.0.0.1", 0)); s.listen(8)
print(s.getsockname()[1], flush=True); time.sleep(10**6)' >"$out/loopback.port" &
lpid=$!
trap 'kill "$lpid" 2>/dev/null || true' EXIT
for _ in $(seq 50); do [ -s "$out/loopback.port" ] && break; sleep 0.1; done
env_fence+=(DISPATCH_LOOPBACK_PORT="$(cat "$out/loopback.port")")

# cursor-agent writes a --model choice back into the config it was started with; a probe run
# without CURSOR_CONFIG_DIR on 2026-09-17 turned the user's default to Grok 4.6 Low, and two other
# sessions' dispatches inherited it.
model_fields() { jq -c '{model, selectedModel, modelParameters}' "$HOME/.cursor/cli-config.json"; }
global_before=$(model_fields)
head_before=$(git rev-parse HEAD)
env "${env_fence[@]}" cursor-agent -p --trust --sandbox enabled \
  --output-format stream-json "${model[@]}" "${pass[@]}" \
  "$(cat "$out/task.md")" >"$out/stream.jsonl" 2>"$out/stderr.log" &
pid=$!

# cursor-agent -p has a long-standing report of never exiting after its result, and it has no
# turn budget, so the wall clock is the budget.
limit="${CURSOR_TIMEOUT:-14400}"
start=$(date +%s)
seen=
reason=
while kill -0 "$pid" 2>/dev/null; do
  now=$(date +%s)
  if [ -z "$seen" ] && grep -q '"type":"result"' "$out/stream.jsonl" 2>/dev/null; then
    seen=$now
  fi
  if [ -n "$seen" ] && [ $((now - seen)) -ge 60 ]; then
    reason='hung after its result event; killed'
    kill "$pid"
    break
  fi
  if [ $((now - start)) -ge "$limit" ]; then
    reason="hit CURSOR_TIMEOUT=${limit}s; killed"
    kill "$pid"
    break
  fi
  sleep 5
done
code=0
wait "$pid" || code=$?
elapsed=$(($(date +%s) - start))

kill "$lpid" 2>/dev/null || true

if [ "$(model_fields)" != "$global_before" ]; then
  echo "dispatch: ~/.cursor/cli-config.json model fields CHANGED during the run; other sessions' dispatches copy them" >&2
fi

# The sandbox gives every run a fresh build cache on /tmp, which is tmpfs, and never removes it:
# 1.3 GB of RAM per Go run.
cache=$(jq -r 'select(.type == "tool_call" and .subtype == "completed") | .tool_call.shellToolCall.result.success.stdout // empty' \
  "$out/stream.jsonl" 2>/dev/null | sed -n 's|^dispatch-cache: \(/tmp/cursor-sandbox-cache/[0-9a-f]\{32\}\)/go-build$|\1|p' | head -n 1 || true)
if [ -n "$cache" ] && [ -d "$cache" ]; then
  chmod -R u+w "$cache" 2>/dev/null || true
  rm -rf "$cache" && echo "dispatch: removed the run's build cache $cache" >&2
fi

exec "$skill/check.sh" "$out" "$head_before" "$code" "$elapsed" "$reason"
