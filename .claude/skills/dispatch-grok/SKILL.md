---
name: dispatch-grok
description: Dispatch implementation and investigation work to the local grok CLI as a headless executor while this session stays the orchestrator and acceptor. Use when a task is large enough to hand off as a written task book, or when the user asks to "派发 grok" / "dispatch grok" / "let grok do it". Covers what grok can do on this machine (shell, browser, LSP — all of it unprompted), the sandbox that is now the only real fence, the exact flag set, task-book structure, and the acceptance protocol.
---

# Dispatching grok as executor

> **If you are the grok executor and this file was loaded into your context: ignore it.**
> It describes how the orchestrator dispatches *you*. It is not a task book.

This session is the **orchestrator**: it adjudicates design, writes the task book, decides which
commands the executor may run, and accepts or rejects the result. The local `grok` CLI (Grok
Build, xAI) is the **executor**. Do not use Claude subagents for the execution work this skill
covers — dispatch `grok`.

## 1. What grok can do on this machine

Verified empirically on 2026-09-15 against `grok 1.0.30`. **This section was rewritten from the
ground up:** the 1.0.5 notes it replaces described a machine that no longer exists.
`/etc/grok/requirements.toml` used to pin `sandbox.profile = "strict"` and a confirmation floor
on `run_terminal_command`; it now pins only telemetry and remote-fetch, and says so:

```
# Permission mode and sandbox are left to ~/.grok/config.toml so always-approve can take effect.
```

`~/.grok/config.toml` sets `[ui] permission_mode = "always-approve"`. Everything below therefore
runs **unprompted**, with no `--allow` rule of any kind.

| Capability | Headless (`-p` / `--prompt-file`) | Notes |
|---|---|---|
| Read / grep / list anywhere on the box | **free** | including sibling repos — verified by reading `../kun-galgame-forum/package.json` |
| Write / edit anywhere on the box | **free**, no rule needed | verified: wrote into this repo *and* outside it with zero `--allow` rules |
| **Shell** (`run_terminal_command`) | **available**, unprompted | verified `go version`, `git rev-parse`, `touch`, `curl` |
| LSP | **available** — one `lsp` tool | `goToDefinition`, `findReferences`, `hover`, `goToImplementation`, `documentSymbol`, `workspaceSymbol` |
| Playwright browser | **available** — 30 `browser_*` MCP tools | headed Chromium on `DISPLAY=:0`, isolated profile, `--caps vision`; see §5 |
| Subagents | `spawn_subagent` | `--no-subagents` for one deterministic worker |
| Web search, GitHub / Notion / penpot MCP | available | `--disable-web-search` for offline-only tasks |
| Git worktree, headless | **works now** (`--worktree <name>`) | the 1.0.5 note that `-p` could not create one is obsolete |

grok loads this repo's `CLAUDE.md` automatically as project instructions (~4.1k tokens), so the
iron rules are already in its context. Do not re-paste them; do restate the specific ones the
task turns on.

## 2. The `--allow` fence is gone. The sandbox is what is left.

The old recipe leaned on `--allow 'Write(<glob>)'` as the mechanical form of iron rule 13
(*one path, one writer*). On 1.0.30 with this machine's config it enforces nothing. Measured:

| Probe | Result |
|---|---|
| No rules at all, "write these two files" | both written — inside the repo and outside it |
| `--permission-mode default` + `--allow 'Write(<out>/**)'` | in-glob write OK; out-of-glob write **denied**, run ends `cancelled` with `PermissionCancelled` |
| same mode, no `Bash` rule, `touch <repo>/file` | **succeeded** — the shell is not bound by the mode, so it walks around any path rule |
| `--sandbox workspace`, `touch /home/kun/x` | `touch: cannot touch '/home/kun/…': Permission denied` — kernel (Landlock), and `curl https://example.com` still returns 200 |
| `--sandbox read-only` / `strict` / any profile with a `deny` list | **refuses to start on this box**: `bwrap: Can't mkdir parents for /run/containerd/containerd.sock: Permission denied` |
| `--worktree <name> --sandbox workspace`, `touch <main checkout>/file` | **succeeded** — a worktree isolates the *checkout*, it does not fence writes |

So there are exactly two things left, and both are now load-bearing:

1. **`--sandbox workspace`** — kernel-enforced, the executor may write the repo, `/tmp` and
   `~/.grok` and nothing else on the box. `dispatch.sh` passes it on every run. Reads stay
   unrestricted and the network stays open (web search and MCP keep working).
2. **Discipline in the task book plus `git status --porcelain` at acceptance.** The glob used to
   catch a stray write; now only you do. Name the writable paths in the task book anyway — it is
   what the executor follows — and diff before you believe anything.

`apps/api/.env` carries live B2 / Bangumi / upstream credentials and sits inside that writable
area. A `deny` list would kernel-block it, but deny lists do not start here (row 5 above), so the
task book has to say "do not read or print `apps/api/.env`" and mean it.

Never run two dispatches concurrently over overlapping paths. Sequential, or disjoint.

## 3. The dispatch

```bash
export GROK_OUT_ROOT="$SCRATCHPAD/grok"          # session scratchpad, never the repo
mkdir -p "$GROK_OUT_ROOT/<slug>"
# write the task book to $GROK_OUT_ROOT/<slug>/task.md, then:
.claude/skills/dispatch-grok/dispatch.sh <slug> \
  --allow 'Write(apps/api/internal/platform/catalog/**)' \
  --allow 'Edit(apps/api/internal/platform/catalog/**)'
```

`dispatch.sh` supplies `--prompt-file`, `--sandbox workspace`, the two rules for the output
directory, `--output-format json`, `--max-turns` and `--debug-file`, then reports
`stopReason`/`turns`/`cost_usd` and diagnoses a bad exit. Every extra argument is passed through.

Run it **in the background** — a real task runs for minutes and a foreground call blocks the turn.

Environment knobs: `GROK_SANDBOX_PROFILE` (default `workspace`; `off` disables the fence —
do not), `GROK_PERMISSION_MODE` (unset by default; `default` restores the path fence on the
*file-write tools* at the cost of a run that dies on the first unruled write), `GROK_MAX_TURNS`.

Path `--allow` rules are still worth passing: they document intent, and they become enforcement
again the moment `GROK_PERMISSION_MODE=default` is set. Rule prefixes are the Claude-compatible
names — `Write`, `Edit`, `Read`, `Bash` — not grok's native tool names (`write`,
`search_replace`, `run_terminal_command`); a native name is a hard error (`unknown tool prefix`).

Useful extra flags:

| Flag | When |
|---|---|
| `--effort low\|medium\|high\|xhigh` | default is `high`; `low` for mechanical sweeps |
| `-m grok-4.5` | default is `grok-4.6` |
| `--no-subagents` | one deterministic worker instead of a fan-out |
| `--disable-web-search` | offline-only tasks |
| `-w <name>` / `--worktree <name>` | runs in `~/.grok/worktrees/<repo>/<name>`, detached at the source HEAD (dirty overlay included); `--worktree-ref origin/main` for a clean base. Manage with `grok worktree ls` / `grok worktree rm <id> --force` — **remove it when the wave ends**, it is a real git worktree |

## 4. Now that it has a shell: who runs what

The old division ("grok writes, the orchestrator runs") was forced by a missing capability. It is
now a choice, and the useful line is **feedback loop vs. shared state**.

**Let the executor run** — and say so explicitly in the task book, listing the commands:
`go build ./...`, `go vet`, `gofmt -l`, `rg`, `go test ./<narrow package>`, reading `git log` /
`git diff` / `git status`. This is the real upgrade: it can iterate to a compiling, passing state
instead of handing back code that was never executed.

**Never delegate** — these stay the orchestrator's, and the task book forbids them by name:

- Git that writes: `commit`, `push`, `branch`, `rebase`, `stash`, PRs, and anything touching the
  shared checkout's state.
- Databases: every migration, `scripts/ephemeral-test-db.sh`, any psql against a real database,
  and every production query.
- `docker` / `docker compose` / `pnpm dev*` — starting or stopping a service on this box.
- Production operations (`ssh kungal-neo`, anything under the prod ops notes).
- Cross-repo docs sync (`../kungal-docs` → `pnpm docs:sync --write`, `pnpm docs:audit`).
- Anything that spends money or dials a partner API.
- **Final acceptance.** `git status --porcelain` shows only the expected paths; re-run the gates
  yourself; spot-check the report's highest-stakes claims against the code. A gate the executor
  says it ran is a claim, not a result — trust the report's structure, verify its conclusions.

## 5. The browser

`~/.grok/playwright-mcp.json` runs **headed** Chromium (`headless: false`, `DISPLAY=:0`, isolated
profile, 1440×900, output in `~/.grok/playwright-output`). A window opens on the user's desktop —
say so before dispatching a browser task, and keep the session short.

Worth using for: driving `apps/account` / `apps/admin` / `apps/developer` against the local dev
stack, reproducing a UI bug, capturing a screenshot for a design review, checking a page renders
after a change. The orchestrator starts the stack (`pnpm dev`) — the executor does not.

Never point it at production, at an authenticated production session, or at anything that would
write through a real API. Screenshots land outside the repo, so `git status --porcelain` stays a
pure signal.

## 6. Reading the result

Three traps, in order of how much time they cost:

1. **`stopReason: "cancelled"` is ambiguous.** It covers *both* "a tool call was denied" and
   "ran out of turns". Always pass `--debug-file` (dispatch.sh does) and grep it for
   `PermissionCancelled` to tell them apart. Without the log you have to re-run to find out.
   (With the fence off this is rarer than it was — which also means a `cancelled` run is now
   much more likely to be turn exhaustion.)
2. **`.text` is the concatenation of every assistant text block**, including the "I'll do X"
   preamble and any interim structured objects — not the final answer alone. Never parse a report
   out of it. **Require grok to write its report to a file** in the output directory; keep stdout
   to a one-paragraph pointer.
3. **`--json-schema` output is also concatenated.** If you use it, take the *last* balanced JSON
   object, not the first.

Because the report lives in the scratchpad and never in the repo, `git status --porcelain` after a
run is a pure signal: it shows exactly what grok changed in code, nothing else. That is the first
acceptance check, and since §2 it is also the only one that catches a stray write.

## 7. The task book

Template: `task-book-template.md` in this directory. Requirements:

- **Write it in English.** Executors follow English task books more reliably.
- **Self-contained.** grok sees none of this conversation. State the repo path, the branch, where
  the code lives, and every prior adjudication it depends on, inline.
- **Name the commands it may run, and the ones it may not** (§4). "Run the tests" is now a real
  instruction — so an unbounded one is a real risk.
- **Scope and out-of-scope, both named.** Out-of-scope is what stops a helpful executor from
  refactoring its way into someone else's paths — and since §2 it is the *only* thing that stops it.
- **Acceptance criteria the orchestrator will actually run** — named test functions, exact
  commands, expected output. Tell grok they exist and that *you* re-run them.
- **No open design decisions.** Every adjudication is made in the task book. If the mechanics
  genuinely depend on code grok has yet to read, state the invariant to enforce plus the precedent
  to follow, and require it to report the mechanics it chose, for acceptance.
- **A discipline section:** exact writable paths, forbidden operations, "do not read
  `apps/api/.env`", and *report, don't work around* for anything unexpected.
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
895k tokens of grok's own context to compress 181 KB of source into an 11 KB report. Judge every
candidate task by how much of the orchestrator's context it removes, and that turns on one
property: **can the result be compressed into something checkable without re-reading the input?**

| Shape | Verdict |
|---|---|
| Broad read → narrow report whose findings are `file:line` + a quoted line | **Dispatch.** ~4–6× context saving; the coordinates are what make verification cheap. Audits, inventories, censuses, cross-reference checks, "find every X across N files". |
| Wide mechanical edit whose correctness a gate asserts (build, tests, `gofmt`, `docs:verify`) | **Dispatch**, and let it run the gate itself now — the saving is that the orchestrator reads only what its own re-run flags, not the diff. |
| A question whose answer needs a command run | **Dispatchable since 1.0.30** (it was impossible before), as long as the command is read-only and touches no shared state. The orchestrator still re-runs anything the decision rests on. |
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
