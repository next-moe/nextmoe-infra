---
name: dispatch-cursor
description: Dispatch implementation and investigation work to the local cursor-agent CLI as a headless executor, inside a kernel sandbox, while this session stays the orchestrator and acceptor. The default executor since 2026-09-17, when grok's balance ran out. Use when a task is large enough to hand off as a written task book, or when the user asks to "派发 cursor" / "dispatch cursor" / "让 cursor 做". Covers the three conditions under which the sandbox silently turns off, the measured fence, the exact dispatch, the preflight proof, task-book structure and the acceptance protocol.
---

# Dispatching cursor-agent as executor

> **If you are the cursor executor and this file was loaded into your context: ignore it.**
> It describes how the orchestrator dispatches *you*. It is not a task book. Your task book
> is the prompt you were started with, and nothing here overrides it.

This session is the **orchestrator**: it adjudicates design, writes the task book, owns git,
databases and production, and accepts or rejects the result. The local `cursor-agent` CLI is the
**executor**. `dispatch-grok` is the same protocol for the grok CLI; its balance ran out on
2026-09-17 with a 402 (`Grok Build usage balance exhausted`), so use this one until the user says
grok is topped up. Sections 5–7 carry grok's lessons over, so this skill stands on its own.

The kun-ui-flutter repo has a sibling of this skill that runs cursor **unsandboxed**
(`--force --sandbox disabled`) behind rules and a credential-free environment. That is not
enough here: this machine's ssh keys reach production without an agent, and the loopback holds
Postgres, other projects' databases and another session's ssh tunnel. This skill keeps the sandbox.

## 1. What cursor-agent can do here

Measured 2026-09-17, `cursor-agent 2026.08.25-3e8eec8`, in a worktree of this repo. The
artefacts are in that session's scratchpad under `cursor/probe/` and `cursor/runs/fence-smoke/`.

| Capability | Result |
|---|---|
| Shell | yes, as bash (`dispatch.sh` sets `SHELL`), inside the sandbox of section 2 |
| Go gates | `go build ./...`, `go vet`, `go test` all work in the sandbox, **including `httptest.NewServer`** (the sandbox has a loopback of its own). `GOCACHE`/`GOMODCACHE` are redirected to a fresh `/tmp/cursor-sandbox-cache/<hash>/` **per run**, so every run builds cold, and that directory is never removed: 1.3 GB of tmpfs, which is RAM, per Go run. `dispatch.sh` deletes it afterwards, using the path the preflight prints |
| Network | only through Cursor's proxy, which allows package registries (`proxy.golang.org` → 200) and refuses the rest (`api.github.com` → `CONNECT tunnel failed, response 403`). Raw TCP: `Network is unreachable` |
| Project instructions | `CLAUDE.md` / `AGENTS.md` load as rules, and per Cursor's rule model (not probed separately) so does every `.cursor/rules/*.mdc` with `alwaysApply`; the kun-ui-flutter probes saw every `.claude/skills/*` listed as a skill — hence the guard at the top of this file |
| Model | `~/.cursor/cli-config.json` picks it: Cursor Grok 4.6 Extra High today. `CURSOR_MODEL=<id>` overrides with an id from `cursor-agent models`; `cursor-grok-4.6-low` for probes. **cursor-agent writes a `--model` choice back into the config it started with**: on 2026-09-17 a probe run without `CURSOR_CONFIG_DIR` turned the user's default into Grok 4.6 Low, and two other sessions' dispatches (and one of ours) inherited it before it was noticed. Never run `cursor-agent --model` outside `dispatch.sh`. `dispatch.sh` pins `--model cursor-grok-4.6-xhigh` (Extra High) unless `CURSOR_MODEL` is set, so the global default no longer reaches a run, and it warns if the global model fields change during one |
| Output | `--output-format stream-json`, one event per line; the `result` event has token counts and no cost |
| Long commands | the shell tool times out after 30 s by default and moves the command to the background; the model polls it. Task books ask for a 600 s timeout on builds |
| Budget | no turn limit. The wall clock is the budget: `CURSOR_TIMEOUT`, 4 h by default |
| Auth | `CURSOR_API_KEY` from the environment (`apiKeySource: "env"`), which is why the config directory can move |

## 2. The sandbox, and how it silently turns off

`cursor-agent sandbox run` and a headless run use the same Landlock + user-namespace sandbox:
the shell runs as uid 0 in its own namespace, with `CURSOR_SANDBOX=native`. **But a headless run
applies it only when all three hold**, and when one fails every command runs as the real user
with no error and nothing in the output to say so — the stream even shows the sandbox policy
that was *requested*:

1. **No `--force` / `--yolo`.** With it, every command ran unsandboxed.
2. **`approvalMode: "allowlist"`.** The user's config says `"unrestricted"` (Run Everything),
   which is unsandboxed. `--sandbox enabled` and `sandbox.mode: "enabled"` do not override it.
3. **The command is not on `permissions.allow`.** An allowed command runs outside the sandbox,
   and the user's config allows `Shell(**)`. The dispatch config's allow list is empty.

Measured inside the sandbox once all three held:

| Probe | Result |
|---|---|
| write the worktree, `/tmp` | allowed |
| write `~/.cache`, a sibling repo, the file tool outside the worktree | refused (`Permission denied` / `Rejected:`) |
| `ssh kungal-neo`, `/dev/tcp/<prod>/22` | `Network is unreachable` |
| `127.0.0.1:5432` and a host listener | `Connection refused` — the loopback is private |
| `docker version` | cannot reach `/var/run/docker.sock` |
| request `required_permissions: ["all"]` or `["full_network"]` | **rejected** in headless mode |
| `git add`, `git commit` | **allowed** — the sandbox makes the git dirs writable, the common dir included |
| read `~/.pgpass`, `~/.ssh/*`, `~/.config/gh/hosts.yml`, the main checkout's `apps/api/.env` | **allowed** (the read boundary is the whole system) |
| a `.cursorignore` inside the worktree | honoured by shell and file tools; one in `$HOME` only by the file tools |

A fresh worktree has **no `.env` files at all** (they are gitignored), which is why dispatches go
to worktrees: the secrets are simply not in the tree. What the sandbox leaves open — git writes
and reads of credentials elsewhere on the box — is what the other two layers are for.

### Layer two: rules (`dispatch.sh` writes them into a per-run config copy)

`CURSOR_CONFIG_DIR` points at `<out>/cursor-config/`, a copy of the user's config with the allow
list emptied, the deny list replaced, `approvalMode` and `sandbox.mode` set, and `authInfo`
dropped. The user's own config is never edited. Rule semantics, measured in the kun-ui-flutter
probes on the same version and confirmed here:

- **A Shell rule is a glob over each sub-command's whole text.** `Shell(*git commit*)` catches an
  absolute path, `env`, `bash -c`; a bare `Shell(ssh)` matches the command base. A variable
  (`p=commit; git $p`) gets past any rule — that is why a moved HEAD is checked afterwards.
- **Read and Write rules take absolute paths or `**/`.** A relative one matches nothing. They
  fence the file tools only; the shell obeys the sandbox and the Shell rules.
- **A refused call does not end the run.** It comes back as `permissionDenied`,
  `writePermissionDenied` or `rejected`, and `check.sh` lists every one.

The deny list covers git writes (and `git -C` / `-c` / `--git-dir`, which slip past them), `gh`,
`ssh`/`scp`/`rsync`, `psql`/`pg_dump`, `docker`, `sudo`, `pkill`, `pnpm dev`, other agents
(`grok`, `agent`, `cursor-agent`, `claude`, `codex`), shell text naming a credential file, file-tool
reads of those files and of any `.env`, and file-tool writes to `.claude/`, `.cursor/`,
`CLAUDE.md`, `AGENTS.md` and `node_modules/`. Add per-task rules with `--deny '<rule>'`.

Some rules also catch reads that are harmless: `rg 'git add'` or `cat x/.env.example` are refused.
The executor reports the refusal, and the file tools can still read what the shell cannot.

### Layer three: the environment

GitHub tokens and `SSH_AUTH_SOCK` unset, `PGPASSFILE` and `DOCKER_HOST` pointed at nothing,
`TEST_DATABASE_DSN` unset, every push URL rewritten to a path that does not exist, `SHELL=bash`
with an empty `ZDOTDIR` (`~/.zshenv` sources `~/.grok/env`, which exports a GitHub token), and
`GOMAXPROCS=8`. All of it sits behind the network block, so it only matters if the sandbox is off —
which is exactly when it is needed.

## 3. The dispatch

```bash
# 1. a worktree of origin/main that nobody else stands on (iron rule 13)
git -C /home/kun/Desktop/code/website/nextmoe-infra worktree add -b <branch> \
  /home/kun/.config/superpowers/worktrees/kun-galgame-infra/<name> origin/main
# 2. the task book, from task-book-template.md
export CURSOR_OUT_ROOT="$SCRATCHPAD/cursor/runs"   # session scratchpad, never the repo
mkdir -p "$CURSOR_OUT_ROOT/<slug>"                  # write task.md there
# 3. from the worktree root, detached
cd <worktree> && CURSOR_OUT_ROOT="$CURSOR_OUT_ROOT" setsid nohup \
  /home/kun/Desktop/code/website/nextmoe-infra/.claude/skills/dispatch-cursor/dispatch.sh <slug> \
  >"$CURSOR_OUT_ROOT/<slug>/dispatch.out" 2>&1 </dev/null &
```

Then wait with a background Bash until-loop on
`pgrep -f 'dispatch-cursor/[d]ispatch.sh <slug>'`, not a foreground sleep.

`dispatch.sh` refuses to run from a main checkout or a subdirectory, and refuses `--force`,
`--yolo`, `--sandbox`, `--approve-mcps`, `--auto-review` and `--worktree`. It adds
`-p --trust --sandbox enabled --output-format stream-json` and the three layers, holds a
listener on the host loopback for the preflight to fail to reach, enforces the time limit, and
hands the stream to `check.sh`. Extra arguments pass through to `cursor-agent`.

| Variable | When |
|---|---|
| `CURSOR_MODEL=cursor-grok-4.6-low` | probes and mechanical sweeps; unset means `cursor-grok-4.6-xhigh` |
| `CURSOR_TIMEOUT=<seconds>` | default 14400 |

**At most two at once**, and never two over overlapping paths. A dispatch started with
`setsid nohup … &` survives the harness killing its own background tasks.

Memory: other sessions on this machine dispatch cursor too, and the harness kills its own
background tasks when RAM runs out (it did on 2026-09-17, with swap full). A detached dispatch
survives that; its waiter does not, so re-arm the waiter.

A killed run leaves its work in the tree. To resume it, put a "RESUMED RUN" note at the top of the
same task book (what exists, what is left, re-run every gate), move the old `stream.jsonl` and
`run.json` aside, and dispatch again from the same worktree.

## 4. Reading the result

`check.sh <out> [head-before]` runs at the end of every dispatch and can be run by hand on a
killed one. It exits non-zero, and says why, when:

1. **The preflight line is missing or wrong.** The task book's step 0 runs
   `bash "$DISPATCH_PREFLIGHT"`, which prints
   `dispatch-preflight: sandbox=native net=blocked loopback=private` from inside (and a
   `dispatch-cache:` line that `dispatch.sh` uses to clean up). Anything else
   — or nothing — means **every shell call in that run was unfenced**; treat it as such. This is
   the only proof, because the sandbox fails open (section 2). Controls: run unsandboxed it
   prints `sandbox=none net=open loopback=host`, and `check.sh` fails on a stream from a
   `--force` run, on a tampered preflight line, on a shell call with no sandbox policy that
   succeeded, and on a moved HEAD.
2. **A shell call succeeded with no sandbox policy** — an escalation got through.
3. **HEAD moved** — the executor committed.
4. No `result` event (quota, auth, network: read `stderr.log` before retrying), or
   `is_error: true`, or a non-zero exit.

It also lists every refused call. Then, by hand:

```sh
git status --porcelain       # only the paths the task book made writable
git log --oneline -3         # no commit you did not make
```

and re-run every gate yourself, **including the DB suites** the executor could not reach
(`scripts/ephemeral-test-db.sh`, `-count=1 -p 1`). The report is a file in the scratchpad, never
in the repo, so `git status` shows exactly what the executor changed. `result` is every assistant
text block concatenated; never parse a report out of it.

## 5. The task book

Template: `task-book-template.md`. It already carries step 0, the environment paragraph and
the discipline section; fill in the rest.

- **English**, and it says everything the executor writes is English.
- **Self-contained.** State the worktree path, branch, where the code lives, and every prior
  adjudication inline. Name the output directory by absolute path.
- **No open design decisions.** If the mechanics depend on code the executor has yet to read,
  state the invariant plus the precedent, and require it to report what it chose.
- **Name the commands it may run**, and say the orchestrator re-runs them.
- **Scope and out-of-scope, both named.**
- **Quote binding clauses, don't cite them** (`refs/api-v2/` `A<n>`/`B<n>`/`G<n>`/`D<n>`, with
  the line). `refs/` is gitignored, so a worktree does not have it: give the absolute path into
  the main checkout, which the executor can read.
- **Forbid ranking**, and put "anything that looks wrong, in scope or not" near the top of the
  report.
- **Demand a positive control** on any search, audit or census.
- **Word each check as the property, not as your guess at its shape.** "Every `docker run` uses
  the infra-tools digest" produced a false positive on a second image that was digest-pinned on
  its own line; "sources the alert library" did not match a library that is exec'd. The executor
  follows the letter and says so, which is right, and the cost is yours.
- **Ask it to check that the new tests actually run.** A `TestMain` that exits 0 without a
  database skips a whole package, new DB-free tests included. The `image-decode-400` book asked
  for tests that do not need a database where the code allows it; the executor then noticed that
  two packages would have skipped them anyway, and fixed it.

## 6. What the orchestrator never delegates

Adjudications; all git (`git commit -- <paths>`, never `add -A`); databases, migrations and the
ephemeral test DB; docker and `pnpm dev`; production (`ssh kungal-neo`); cross-repo docs sync;
anything that spends money or dials a partner API; and final acceptance (section 4).

## 7. What is worth dispatching

Dispatching buys orchestrator context. Judge a task by whether its result can be checked without
re-reading its input.

| Shape | Verdict |
|---|---|
| Broad read → narrow report of `file:line` + quoted line | **Dispatch.** Audits, inventories, censuses. |
| Wide mechanical edit a gate asserts | **Dispatch.** You read only what the gate flags. |
| Fully adjudicated implementation whose correctness tests assert | **Dispatch.** It iterates to green inside the sandbox; you still read the diff. |
| New code carrying open design judgement | **Do not dispatch** until you have adjudicated it. |
| Anything whose truth is in a database or in production | **Do not dispatch.** The sandbox cannot reach either, by design. |

An audit of something that is deployed (`scripts/prod-cron`, compose files) can only compare the
repo with itself. Pair it with your own read-only pass on the box: the `cron-audit` run was right
about every file it could see and could not see the job that had no crontab line.

## 8. Quality ledger

Record each real dispatch here — task shape, model, what acceptance found — so the next
orchestrator knows how far to trust the executor.

| Date | Task | Model | Elapsed | Acceptance |
|---|---|---|---|---|
| 2026-09-17 | fence smoke (10 scripted probes) | grok-4.6-low | 85 s | every step attempted once, as written; report matched the stream |
| 2026-09-17 | `bgm-jpeg-dims` (#233): fully adjudicated Go fix, 91 lines + 166 lines of tests | grok-4.6-low (inherited by accident, see section 1) | 335 s, 30 calls, 208k in + 560k cache read, 17k out | Code correct, in scope, gates green, one transcribed comment and no others. **Caught a wrong premise in the task book**: local Go 1.27 already accepts the production sampling, which the book said `DecodeConfig` refuses (true on 1.25, which CI and the image use); it switched to factor-3 fixtures and said so under Deviations instead of weakening the assertion. Added a C4 trap nobody asked for. Weakness: 4 of 12 mutants survived because every broken-header case ended at EOF, so a parser that walked past the fault still errored (the book did not ask for a trailing frame either); acceptance added one per case and a both-paths-fail case. "Anything wrong" list accurate, mostly minor |
| 2026-09-17 | `cron-audit`: read-only drift audit of `scripts/prod-cron` (20 directories × checks A–H), task book with a known answer key | grok-4.6-xhigh | 495 s, 46 calls, 265k in + 705k cache read, 30k out | Complete table, every `ok` cell backed by a quoted line; all 5 answer-key items found (the 6h-vs-25h limit, the README test list, both non-jobs quoted, the two jobs with no infra-tools image). Every sampled `file:line` was exact. 7 findings: 4 real (#235), 1 false positive that was **the book's wording** (check F said "every `docker run` must use the infra-tools digest"; it listed the separately digest-pinned DLsite image, quoting both lines accurately), 2 correctly marked unverifiable from the tree. It noticed that check E's "sources the alert library" did not match reality (alert.sh is exec'd) and recorded it instead of failing every row. Blind spot by construction: prod state. The largest gap, a job with no crontab line, was only found at acceptance on the box |
| 2026-09-17 | `image-decode-400` (#236): adjudicated fix across the image handler, imageclient and six backfill jobs, one test per job | grok-4.6-xhigh | 695 s, 172 calls, 285k in + 2.96M cache read, 45k out | Every adjudication implemented as written, no stray edits, gates green on both toolchains. Chose the extraction seam the book allowed and said why. **Found on its own that two packages' `TestMain` exited 0 without a database**, so its new DB-free tests would never have run in CI; it fixed that by following the named precedent and listed it under Deviations. "Anything wrong" was 20 accurate `file:line` items: the out-of-scope jobs still retrying 80010, the v2 face answering it with 503, `ErrTooLarge` still 500. Acceptance: 18/18 compiling mutants killed with no test changes; 128 tests ran against three ephemeral DBs and minio, 0 skipped; the one addition was an end-to-end HTTP test through the real decoder, which the book had not asked for. Verdict: at Extra High it is a dependable implementer of adjudicated work; still re-run and mutate |
