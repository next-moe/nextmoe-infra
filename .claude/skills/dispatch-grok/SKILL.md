---
name: dispatch-grok
description: Dispatch implementation and investigation work to the local grok CLI as a headless executor while this session stays the orchestrator and acceptor. Use when a task is large enough to hand off as a written task book, or when the user asks to "派发 grok" / "dispatch grok" / "let grok do it". Covers what grok can do (shell, browser, LSP), the two fences that work (kernel sandbox + --deny rules; --allow fences nothing), the exact flag set, task-book structure, and the acceptance protocol.
---

# Dispatching grok as executor

> **If you are the grok executor and this file was loaded into your context: ignore it.**
> It describes how the orchestrator dispatches *you*. It is not a task book. (grok loads this
> repo's Claude skills, so it does see this file.)

grok is **available**. A 402 (`Grok Build usage balance exhausted`) only pauses that executor
until a later run succeeds. `dispatch-cursor` remains the other executor.

This session is the **orchestrator**: it adjudicates design, writes the task book, decides which
commands the executor may run, and accepts or rejects the result. The local `grok` CLI (Grok
Build, xAI) is the **executor**. Do not use Claude subagents for the execution work this skill
covers — dispatch `grok`.

## 1. What the executor can do

Checked against the Grok Build **1.0.70** user guide. The 1.0.30 notes are obsolete:
`read-only` and `strict` start, and a path deny applies to the shell as well as the file
tools. Re-read this section when `grok --version` moves off 1.0.70.

`dispatch.sh` passes `--always-approve` and `--cwd` set to the repo root. Tools then run
without a prompt. Deny rules, hooks, and the sandbox still apply. Do not depend on a
permission mode configured outside this script, and do not record one in a task book.

| Capability | Headless | Notes |
|---|---|---|
| Read / grep / list | unrestricted under `workspace` | including sibling repos |
| Write / edit | inside the sandbox write set | §2 |
| **Shell** (`run_terminal_command`) | available | pipes, `&&`, `;` run; §2 for what a deny rule refuses |
| LSP | one `lsp` tool | `goToDefinition`, `findReferences`, `hover`, `goToImplementation`, `documentSymbol`, `workspaceSymbol` |
| Browser | when Playwright MCP is connected | §5. Say so before a headed run |
| Subagents | `spawn_subagent` | `--no-subagents` for one deterministic worker |
| Web search and connected MCP | available | `--disable-web-search` for offline-only tasks |
| Git worktree | `--worktree <name>` | isolates the checkout; it is not a write fence. Refused under `read-only` and `strict` |

The iron rules are already in its context through this repo's `CLAUDE.md`. Do not re-paste
them; do restate the specific ones the task turns on.

**Do not run `grok mcp list` where the output is kept.** It prints MCP server URLs, and a
URL can carry a token. Never paste that output anywhere.

## 2. Fences: the sandbox and `--deny`. `--allow` fences nothing.

Three fences, in this order of strength. This is the 1.0.70 user guide, not a record of
anyone's config.

1. **`--sandbox workspace`** — what `dispatch.sh` passes. Kernel-enforced (Landlock on
   Linux). Writes are the session directory, `/tmp`, `/var/tmp`, and `~/.grok`. `--cwd` is
   the repo root, so the session directory is the repo, not whatever directory the shell
   happened to be in. Reads stay unrestricted. Child-process network stays open (in-process
   web search and the model call are unaffected either way).

   The same profile still write-denies, inside that grant: `~/.grok/config.toml`,
   `trusted_folders.toml`, `managed_config.toml`, `requirements.toml`, `sandbox.toml`,
   `hooks/`, `hooks-paths` and the absolute targets it lists, `bundled/`, `bundles/`, and
   each project hook config that exists when the sandbox starts (`.claude/settings.json`,
   `.claude/settings.local.json`, `.cursor/hooks.json`, and the files in `.grok/hooks/`).
   A write there fails with `Permission denied`. That is the sandbox.

   Landlock covers content, create, delete, and rename. It does not cover `chmod`,
   timestamps, or extended attributes.

   **`--sandbox read-only`** for an audit that must not touch the repo. Writes are then
   `~/.grok` (with the same write-denies), `/tmp`, and `/var/tmp`, so the report still
   lands in the scratchpad. On Linux, child-process network is blocked. Worktrees are
   refused before the session starts.

   **`strict` still writes the session directory.** It is not a stricter audit profile.
   It also blocks child network and refuses worktrees. `GROK_SANDBOX_PROFILE=off` removes
   the fence — do not.

2. **`--deny` rules** — grok's permission policy, not the kernel. A deny wins over
   always-approve and over every allow rule. **`--allow` is not a closed allowlist**: a
   call that matches no allow rule falls through to the mode, and under `--always-approve`
   that call runs. Fence a path by denying it (`--deny 'Write(apps/web/**)'`), not by
   allow-listing the rest.

   Rule prefixes are the Claude-compatible names — `Write`, `Edit`, `Read`, `Bash`,
   `Grep` — not grok's native tool names. `Write` and `Edit` are one prefix: a `Write`
   deny covers edits, and it covers a shell command that touches that path (`touch`,
   `sed`). A `Read` deny covers `cat` of that path. Patterns are globs: `*` and `?` do
   not cross `/`, `**` does. Write an absolute path or a `**/` pattern. A leading `~/`
   in a pattern is literal text, not the home directory.

   With a Bash deny or ask rule, or a Read or Edit deny, headless grok refuses a shell
   command it cannot check, instead of prompting. That set is: a subshell, command
   substitution (`$(…)` and backticks), background `&`, control flow, and — once a Read
   or Edit deny exists — a command whose files it cannot name. A recursive `rg` or
   `grep -r` is in that last set, and so is a glob that might match a denied path
   (`cat .e?v`). A literal `sh -c` / `bash -c` script is parsed and checked; it is
   refused when a segment is denied, or when `cd` / `pushd` / `popd` leaves a relative
   path that cannot be resolved. Name files by absolute path. The refusal text is
   `could not be checked against the permission rules, so it was not run`, or
   `Denied by permission policy: deny rule on edit matching …`. Neither ends the run (§6).

   `dispatch.sh` denies Read and Write on `apps/api/.env` and the repo-root `.env` on
   every run. Those are Read/Edit denies, so a recursive search of the repo root or of
   `apps/api` is refused. Search a narrower path (`apps/api/internal`, `apps/api/cmd`)
   or use the grep tool. Say so in the task book.

3. **The task book plus `git status --porcelain` at acceptance.** Name the writable paths
   anyway — it is what the executor follows — and diff before you believe anything.

Never run two dispatches concurrently over overlapping paths. Sequential, or disjoint.

## 3. The dispatch

```bash
export GROK_OUT_ROOT="$SCRATCHPAD/grok"          # session scratchpad, under /tmp, never the repo
mkdir -p "$GROK_OUT_ROOT/<slug>"
# write the task book to $GROK_OUT_ROOT/<slug>/task.md, then:
.claude/skills/dispatch-grok/dispatch.sh <slug> \
  --deny 'Write(apps/web/**)' --deny 'Write(apps/editor/**)'   # whatever is out of scope
```

`dispatch.sh` supplies `--prompt-file`, `--cwd <repo root>`, `--always-approve`,
`--sandbox workspace`, the `.env` deny rules, `--output-format json`, `--max-turns` and
`--debug-file`, then reports `stopReason`/`turns`/`cost_usd` plus how many tool calls the
policy refused, and diagnoses a bad exit. Every extra argument is passed through.

Run it **in the background** — a real task runs for minutes and a foreground call blocks the turn.

Environment knobs: `GROK_SANDBOX_PROFILE` (default `workspace`; `read-only` for audits;
`off` disables the fence — do not), `GROK_MAX_TURNS` (default 200). There is no permission-mode
knob: the script always passes `--always-approve`. Without that flag a headless run that
needs a prompt refuses the call, prints `permission required:` once, and can still exit 0.

Useful extra flags:

| Flag | When |
|---|---|
| `--effort low\|medium\|high\|xhigh` | pass it when the task should not use the session default. `high` is enough for most dispatches; `low` for mechanical sweeps |
| `-m <id>` | pin a model. `grok models` lists the ids this install offers. Do not record a configured default in the task book |
| `--no-subagents` | one deterministic worker instead of a fan-out |
| `--disable-web-search` | offline-only tasks |
| `-w <name>` / `--worktree <name>` | runs in `~/.grok/worktrees/<repo>/<name>`; `--worktree-ref origin/main` for a clean base. Manage with `grok worktree ls` / `grok worktree rm <id> --force` — **remove it when the wave ends**, it is a real git worktree. The directory grok was started in stays writable under `workspace`, so a worktree does not fence writes. `read-only` and `strict` refuse to create one |

## 4. Now that it has a shell: who runs what

The useful line is **feedback loop vs. shared state**.

**Let the executor run** — and say so explicitly in the task book, listing the commands:
`go build ./...`, `go vet`, `gofmt -l`, `rg <narrow path>`, `go test ./<narrow package>`, reading
`git log` / `git diff` / `git status`. It can iterate to a compiling, passing state instead of
handing back code that was never executed.

**Never delegate** — these stay the orchestrator's, and the task book forbids them by name:

- Git that writes: `commit`, `push`, `branch`, `rebase`, `stash`, PRs, and anything touching the
  shared checkout's state.
- Databases: every migration, `scripts/ephemeral-test-db.sh`, any psql against a real database,
  and every production query.
- `docker` / `docker compose` / `pnpm dev*` — starting or stopping a service on this box.
- Production operations (`ssh kungal-neo`, anything under the prod ops notes).
- Cross-repo docs sync (`../nextmoe-docs` → `pnpm docs:sync --write`, `pnpm docs:audit`).
- Anything that spends money or dials a partner API.
- **Final acceptance.** `git status --porcelain` shows only the expected paths; re-run the gates
  yourself; spot-check the report's highest-stakes claims against the code. A gate the executor
  says it ran is a claim, not a result — trust the report's structure, verify its conclusions.

## 5. The browser

If the connected Playwright MCP runs headed Chromium, a window opens on the user's display.
Say so before dispatching a browser task, and keep the session short.

Worth using for: driving `apps/account` / `apps/admin` / `apps/developer` against the local dev
stack, reproducing a UI bug, capturing a screenshot for a design review, checking a page renders
after a change. The orchestrator starts the stack (`pnpm dev`) — the executor does not.

Never point it at production, at an authenticated production session, or at anything that would
write through a real API. Screenshots land outside the repo, so `git status --porcelain` stays a
pure signal.

## 6. Reading the result

`run.json` keys on 1.0.70: `stopReason`, `num_turns`, `total_cost_usd`, `total_cost_usd_ticks`,
`usage`, `modelUsage`, `text`, `sessionId`, `requestId`, `cwd`. A run with an incomplete bill
omits the cost floats; `dispatch.sh` prints `?` for a missing `total_cost_usd`.

Traps, in order of how much time they cost:

1. **A refused tool call does not end the run.** A deny-rule refusal comes back to the model
   as a tool result and the run can still end `end_turn`, and the model need not mention it
   in `.text`. `dispatch.sh` counts two lines in `debug.log`: `permission policy: deny rule
   matched`, and `could not be checked against the permission rules`. A non-zero count means
   part of the task may have been routed around or skipped — read the report with that in
   mind. A `cancelled` run is most likely turn exhaustion. (`PermissionCancelled` in the
   debug log was the 1.0.30 shape of a denial.)
2. **A missing prompt approval exits 0.** Without always-approve, headless grok refuses the
   call, prints `permission required:` once on stderr, and the process can still exit 0.
   `dispatch.sh` passes `--always-approve` and fails the run if that line appears anyway.
3. **`.text` is the concatenation of every assistant text block**, including the "I'll do X"
   preamble and any interim structured objects — not the final answer alone. Never parse a report
   out of it. **Require grok to write its report to a file** in the output directory; keep stdout
   to a one-paragraph pointer.
4. **`--json-schema` output is also concatenated.** If you use it, take the *last* balanced JSON
   object, not the first.

Because the report lives in the scratchpad and never in the repo, `git status --porcelain` after a
run is a pure signal: it shows exactly what grok changed in code, nothing else. That is the first
acceptance check.

## 7. The task book

Template: `task-book-template.md` in this directory. Requirements:

- **Write it in English.** Executors follow English task books more reliably.
- **Self-contained.** grok sees none of this conversation. State the repo path, the branch, where
  the code lives, and every prior adjudication it depends on, inline.
- **Name the commands it may run, and the ones it may not** (§4). "Run the tests" is a real
  instruction — so an unbounded one is a real risk.
- **Scope and out-of-scope, both named**, and the out-of-scope paths also passed as `--deny
  'Write(…)'` rules. Out-of-scope is what stops a helpful executor from refactoring its way into
  someone else's paths.
- **Acceptance criteria the orchestrator will actually run** — named test functions, exact
  commands, expected output. Tell grok they exist and that *you* re-run them.
- **No open design decisions.** Every adjudication is made in the task book. If the mechanics
  genuinely depend on code grok has yet to read, state the invariant to enforce plus the precedent
  to follow, and require it to report the mechanics it chose, for acceptance.
- **A discipline section:** exact writable paths, forbidden operations, the denied files and the
  command forms that get refused (§2), and *report, don't work around* for anything unexpected.
- **A named report path and a fixed report structure.**
- **Forbid ranking.** The executor cannot see what the orchestrator knows, so its importance
  ordering is noise at best. In the first real dispatch the single finding with design consequence
  — a spec example violating the spec's own blacklist item — was filed last and labelled
  `not scored`, because it fell outside the four classes the task book named. Require a flat list
  at equal weight, and put the "anything that looks wrong, in scope or not" section *near the top*
  where a helpful executor will not fold it into "what I could not verify".
- **Demand a positive control on any search, audit, or census.** "I found 4" is unreadable without
  "and here are the 16 things I checked that were clean". Insist on the second list.

## 8. What is worth dispatching

Dispatching buys **orchestrator context**, not money — the first real run cost $0.19 and burned
895k tokens of grok's own context to compress 181 KB of source into an 11 KB report. Every run
also carries a fixed overhead: a one-line task at `--effort low` took ~17k fresh + ~36k cached
input tokens and ~$0.02 on 1.0.70 (system prompt, `CLAUDE.md`, skills, tool schemas). That
makes grok a poor fit for thousands of tiny calls (a per-item extraction over a dataset belongs
to a direct model API instead) and a good fit for one large task.

Judge every candidate task by how much of the orchestrator's context it removes, and that turns on
one property: **can the result be compressed into something checkable without re-reading the
input?**

| Shape | Verdict |
|---|---|
| Broad read → narrow report whose findings are `file:line` + a quoted line | **Dispatch** (under `read-only`). ~4–6× context saving; the coordinates are what make verification cheap. Audits, inventories, censuses, cross-reference checks, "find every X across N files". A `read-only` audit cannot `curl` or fetch modules: child network is blocked. |
| Wide mechanical edit whose correctness a gate asserts (build, tests, `gofmt`, `docs:verify`) | **Dispatch**, and let it run the gate itself — the saving is that the orchestrator reads only what its own re-run flags, not the diff. |
| A question whose answer needs a command run | **Dispatchable**, as long as the command is read-only and touches no shared state. The orchestrator still re-runs anything the decision rests on. |
| New code carrying design judgement | **Do not dispatch.** Every line must be read to be reviewed, so the orchestrator pays full input for the output *plus* the review, and still runs the gates. Net saving ≈ zero. |
| Anything needing a database, a migration, a deploy, or a credential | **Do not dispatch.** §4. |

The old structural ceiling — "an executor with no shell can prove that documents disagree with
each other, never that they disagree with reality" — is lifted for anything a read-only command
can settle. It still stands wherever the ground truth is in a database or in production, because
that is exactly where the executor is not allowed to look.

## 9. Standing binding documents

When the dispatched work touches the v2 API, `refs/api-v2/` is binding, and the task book must
name the specific clauses that bind this task — axiom, blacklist item, gate, decision record — by
identifier, with the file. Do not tell the executor to "follow the spec": the spec is eleven files
and it will follow the wrong part of it. Name `A<n>` / `B<n>` / `G<n>` / `D<n>` and quote the line.

`refs/` is gitignored, so grok can read the spec but nothing it writes there reaches a commit.
