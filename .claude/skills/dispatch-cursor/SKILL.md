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
| 2026-09-22 | `forum-stale-gid-fold` (kun-galgame-forum #176): fully adjudicated fix from a design note, new client method + a second pass in the merge sync, 5 fold tests | grok-4.6-xhigh | 986 s, 108 calls, 339k in + 3.67M cache read, 60k out | Every adjudication implemented as written, only the named paths touched, HEAD unmoved, gates green. Both required proofs present and the never-fold-into-an-empty-canonical rule implemented exactly. Acceptance ran 6 mutations: 5 killed (stale-side proof, round-trip proof, the empty-canonical guard, never calling the new pass, dropping `include=refs`); the 6th — removing the `!local[staleGID]` filter — survived and on inspection is genuinely redundant, because `resolved` is built from the local-row set, so it is defence in depth rather than a hole. It introduced a repo interface and a nil-Redis no-op so the five fold tests need no database, and declared both under Deviations with the typed-nil trap handled. It also stated a real limitation the book had not: a work-id-shaped stale gid carrying no anchor ref cannot satisfy the second proof and will not fold — correct, and better said than papered over. 13 accurate `file:line` observations, including two stale comments in files it did not touch. |
| 2026-09-18 | `dlsite-cdn-404` (#248): adjudicated Go flag + a dated shell ledger in a prod cron script + four harness cases | grok-4.6-xhigh | 1001 s, 91 calls, 203k in + 3.66M cache read, 66k out | Every adjudication implemented as written, only the six writable paths, no deviations, gates green including `shellcheck`. Its POSIX awk merge sidestepped the empty-first-file `NR==FNR` trap and said so. The "anything wrong" list (17 flat items) held both real defects acceptance fixed: a line-number assertion on the substring `"1"` (would accept `line 10`) and a `grep -c` under `set -e` that aborts the harness instead of failing the case. It listed two checks as unverifiable without a database that needed none (both return before `openGorm`); acceptance added that test, and it is the only one that kills the `--kind intro` mutant. 19/19 mutants killed after that. Blind spot by construction: prod. A read-only dry run of its binary against the live catalog and the real mirror log reproduced production's counts in the control and cut 257 works to 5. Verdict: dependable at Extra High; its tests still assert a little less than its report implies, so mutate |
| 2026-09-18 | `mt-stray-rows`: adjudicated fix across two MT jobs (comparison-row SQL, two prune paths, a transaction, a derived-row exclusion) + 6 DB tests the sandbox cannot run | grok-4.6-xhigh | 930 s, 103 calls, 183k in + 2.26M cache read, 58k out | Every adjudication implemented as written, only the writable paths, no deviations; tests assert table rows, not counters, and the derived row is compared down to `updated_at`. **Its own report said which mutation its tests could not catch** (the delete moved out of the transaction) and flagged `readMachine`'s missing `ORDER BY`. As delivered, 16 of 19 compiling mutants died; the survivors were the entity prune-only path (skip + dry run: the book asked for the work lane's prune-only test only, and it mirrored the book, not the symmetry), the write path's `WouldPrune` count, and the entity delete's `lang` filter. Acceptance added one test and two assertions (21/21), dropped a no-strays branch so every write takes the transaction, rerouted four guard tests from a test-only `upsert` wrapper to the real write path, and moved a comment that sat on the wrong line. Verdict unchanged: dependable on adjudicated work; name every symmetric case in the book, it will not infer one |
| 2026-09-18 | `mt-answer-in-reasoning` (#254): adjudicated retry logic mirrored in two translator copies (a thinking-off re-send, one length roll, a call cap) + 8 named DB-free tests per package | grok-4.6-xhigh | 690 s, 106 calls, 120k in + 2.08M cache read, 41k out | Both copies identical and as written, the comment verbatim, only the writable paths, no deviations, gates green with every named test `--- PASS`. The out-of-scope census was complete, with file:line for each of 14 other chat clients, and it noticed one client that already disables thinking on every call. Weakness, which was **the book's**: it tested exactly the cases the book named, so 2 of 26 mutants survived: a thinking-off retry that ends on `length` was unchecked, and the returned model was never asserted. The book stated both behaviours but listed neither as a test. Acceptance added one test each. Lesson: every clause of the behaviour, not just the headline paths, needs a named test in the book |
| 2026-09-18 | `reconcile-machinery-audit`: read-only map of every work/ref writer, the probable→exact paths, the candidate lifecycle and every anchor consumer (Q1–Q8), each claim with `file:line` | grok-4.6-xhigh | 860 s, 326 calls, 484k in + 7.35M cache read, 52k out | Every spot-checked claim exact (the `planWorkPair` accept rule, `shouldReleaseQuarantine` counting deferred, deferred never reloaded, `planRef` with no reject). It found what the orchestrator had not: EG credits and music read only `rule:eg-vndb-rosetta` refs, so ~5,800 works never got EG staff. It ran the preflight twice; `check.sh` compared the two joined lines to one and reported FENCE NOT PROVEN on a fenced run (fixed: every line must be the fenced one) |
| 2026-09-18 | `eg-crosslink-anchors` + follow-up `-2`: new lane with an evidence classifier, a primary-per-work order, receipts, 15 named DB tests, importer switch-over, cron ceiling + harness case | grok-4.6-xhigh | 1416 s + 896 s, 181 + 126 calls | Both runs implemented the book to the letter, no stray edits, 62 + 65 DB tests pass on Postgres with 0 skips. The follow-up fixed three gaps **in the orchestrator's first book** (a probable primary did not block a second one; rejections checked on one of three paths; single-family primaries could never be confirmed). One sound deviation (empty title keys never corroborate). Mutation 23/24 killed after acceptance added three tests for filters the orchestrator itself added (soft-deleted releases, dead EG refs); the survivor is equivalent. Acceptance also replaced `make_date` with string formatting: one bad date row would have failed the whole weekly lane, and the fixtures could not show it. Prod read-only dry run matched the census |
| 2026-09-18 | `judge-terminal-dispositions` (#256): judge apply rules gain two corroborators (exclusive loose name, shared identity record), `kept_apart_*` deferrals instead of human queues, quarantine release, ref rejects | grok-4.6-xhigh | 2867 s, 368 calls, 699k in + 21.1M cache read, 169k out | Book implemented as written. Three DB tests passed alone and failed in the full package (leftover refs and proposals under `RESTART IDENTITY`); acceptance widened their TRUNCATE lists. Mutation 19/19 after acceptance added one store-record case (a DMM probable shared record must not corroborate). A prod dry apply over the live queue planned 242 accepts; all 242 were read and are the same game |
| 2026-09-18 | `titlekey` (#257): a pure package of three title-key functions and 22 table tests | grok-4.6-xhigh | 556 s, 34 calls, 78k in + 685k cache read, 33k out | Every row of the book's table passed on the first run; mutations killed. The smallest and cleanest run of the day |
| 2026-09-18 | `eg-only-works`: new lane (pack/port/title-with-corroboration/quarantine/mint, intra-batch fold, receipts, hold-out report), 21 named DB tests, cron block + harness case | grok-4.6-xhigh | 1361 s, 165 calls, 221k in + 5.88M cache read, 89k out | The book was followed to the letter, and the two real defects were **the book's**: an intra-batch fold on title alone (27 of 207 same-title groups on prod spanned brands, some different games: CARAT 1992/1998) and a rejected hit that stranded the game forever; both fixed at acceptance. Its own gaps: two DB tests it could not run compared `int` with `int64`, and an EG id whose exact ref sits on a deleted work would have collided with the unique index and failed a whole mint chunk. Mutation 28/29 (one equivalent) after four tests were added. Prod hold-out: title+date 16,891/13, brand 1,236/10, bgmdate 50/255 (the misses sampled as catalog twins). Its "anything wrong" list flagged both stranding cases |
| 2026-09-18 | `dlsite-only-games`: new importer lane (edition union-find, Bangumi-declared links, title with corroboration, quarantine, mint, fold), a workno tokenizer package, 29 named DB tests | grok-4.6-xhigh | 1611 s, 131 calls, 286k in + 5.72M cache read, 105k out | Logic right, but three defects copied from precedent or invisible without prod: the loader read every fetched staging row with full `product_json` (838k rows, ~6 GB on prod; restricted to 68k game rows and projected fields); `regist_date` stores JST midnight read as UTC+8, so the UTC day is one day early — the precedent `splitDate` had written that day into 13,457 releases, and the fix went into both existing DLsite lanes too; the work revision snapshotted a release (also copied from `egdlsite_create`). The local test Postgres runs in `Asia/Shanghai`, which hid the day bug from a naive fixture: date fixtures must pick an instant that is wrong in both UTC and UTC+8. Mutation 27/29 (two equivalent) after five tests were added; one "survivor" was the orchestrator's own `-run` filter skipping the test |
| 2026-09-18 | `reconcile-watch` (#260): a weekly read-only cron job (five lane dry runs, five queue queries, one alert) plus a ten-case harness | grok-4.6-xhigh | 750 s, 69 calls, 342k in + 2.16M cache read, 50k out | Harness T1–T10 and its own positive control were right the first time, and its "anything wrong" list was the most useful of the day: it flagged that `approved_stale` never counts a null `execute_after`, that the day count was calendar dates, and that both scripts lacked the exec bit (it could not `chmod`). All three fixed at acceptance; a prod run of the installed job parsed every lane |
| 2026-09-18 | `edition-split-sources` (#261): veto exemption, stamp rename with a guarded reopen, merge keeps one EG/Bangumi primary | grok-4.6-xhigh | 970 s, 125 calls, 318k in + 5.34M cache read, 60k out | Mechanics sound. It exempted the two sources for **every** entity type; `work-dedup approve` applies the list to persons and labels too, so acceptance scoped it to works (`IdentityVetoExemptSourceIDsFor`). It noticed on its own that the list's old comment had its two clauses swapped. Mutation 4/6, two equivalent. Prod dry apply planned 73 accepts, all read, all one game |
| 2026-09-18 | `dedup-titlekey` (#262): two judge corroborators and a `seed-keys` mode | grok-4.6-xhigh | 1311 s, 159 calls, 253k in + 6.93M cache read, 83k out | Seven llmsuggest tests failed on a **fresh** database only (it never ensured `src_bangumi`; another package happened to create it) — found by running on a new ephemeral DB, not a reused one. Its seed read every medium; a prod dry run found 21,065 pairs, ~20,000 of them ASMR works sharing titles, so acceptance limited it to galgame (1,088 + 797). Mutation 10/10 after one test was added |
| 2026-09-18 | `getchu-attach` (#263): attach-only lane, three corroborators, hold-out, cron block | grok-4.6-xhigh | 1081 s, 171 calls, 281k in + 4.95M cache read, 65k out | Built exactly what the book asked; the **book** was wrong: the prod hold-out it made possible showed brand (31/108) and Bangumi date (22/247) far weaker than date (7,887/81), so acceptance removed both. The hold-out report is what caught it — ask for one on every matcher |
| 2026-09-18 | `hltb-attach` (#264): attach-only lane, hold-out, one cron line | grok-4.6-xhigh | 881 s, 104 calls, 145k in + 2.60M cache read, 52k out | Clean. It wrote exact refs because the book said exact; its own findings list pointed out that the Steam-bridged precedent writes probable, and acceptance followed the precedent. Prod hold-out 1,173/6 |
| 2026-09-18 | `image-mirror-batch`: batch the DLsite lane of a weekly cron job, four harness cases | grok-4.6-xhigh | 495 s, 53 calls, 283k in + 1.35M cache read, 24k out | Implemented the book (`--limit` on the dry run) and then, in its findings, showed why the book was wrong: `--limit` caps the tool's candidates, and a work whose files the CDN answers 404 for stays a candidate that fetches nothing, so the batch could stall for good. Acceptance moved the cut to the fetch list. The findings list, not the diff, is where this executor earns its keep |
| 2026-09-18 | `getchu-mint`: seven-rung Getchu ladder (two JAN rungs, title cuts, EG brand + date), bundle/goods screens, a gated mint with fold and quarantine, a cron group move, a sixth watch lane | grok-4.6-xhigh | 2061 s, 263 calls, 750k in + 9.61M cache read, 132k out | The largest book yet, implemented as written with one reasoned deviation (peel a trailing `【げっちゅ屋限定版】` before the token walk, or `Bracket Game【…】` becomes `Bracket`) and 25 accurate findings, including brandless items folding together and a dead counter. Its DB tests had never run: three failed on the first real database because **my book contradicted itself** (it offered `特典ボックス` as an edition tail and listed `ボックス` as a bundle marker; the executor picked the tail for its fixture). The prod dry run of its mint list was the real acceptance: 53 of 128 planned mints were EG games listed under another brand (じぃすぽっと's 今日のおかず under モニスタラッシュ), plus CERO Z titles (LEFT4 DEAD, GTA V, Call of Duty), a handheld and a mailer, and 67 items carried Getchu's `0001/01/01` placeholder date. Acceptance added the any-brand EG edition screen, the 31-day EG rung, a whole-title censor match, the 18+ EG brand and action-only screens, strength-ordered candidates and the placeholder date; 48 mutants killed. The book cannot see the data; read every planned mint before trusting a mint lane |
| 2026-09-19 | `release-date-sync` (#272): shared per-source date readers, fill-only lane rewritten as an ownership-chain sync with receipts and optimistic writes, three cron files | grok-4.6-xhigh | 1411 s, 278 calls, 419k in + 8.75M cache read, 130k out | Implemented every adjudication; gates green on its side. One test bug surfaced only on a real database (`relDate` read a soft-deleted release with `First`, so `record not found`). Mutation found 6 survivors, every one a missing fixture rather than wrong code: the candidate SQL and the lane gate both carried the claimed / one-release conditions, so no test reached the second copy; zero date parts and dead refs had no fixture; its T29/T11 script checks grepped for a flag anywhere in the argv, so the dry line satisfied the apply check. Acceptance added `TestDateLaneGates`, a human-stamp-after-read write case, and full-command assertions (24/24 Go and 5/5 script mutants then killed). It could not delete two emptied files (`rm` not on its list) and said so. Lesson for books: when a condition is enforced in both the loader SQL and the Go gate, ask for a fixture that enters through a different lane so the second copy is reachable |
| 2026-09-19 | `field-writers`: read-only census of every catalog field the scheduled jobs write, classified create-only / fill-empty / sync / append-only / set-sync, three calibration answers | grok-4.6-xhigh | 960 s, 407 calls, 405k in + 9.0M cache read, 78k out | All three calibrations right; every sampled `file:line` exact; 53 fields, 21 follow upstream. **Blind by construction to a field with zero writers**: the book asked it to classify writes, so VNDB work tags — 384k rows no code wrote any more — never appeared. Found on prod by listing each source × table's last write time. A census of writers needs that prod pass beside it |
| 2026-09-19 | `vndb-tag-sync` (#275): recompute every anchored work's VNDB tags from the vote mirror, guarded diff writes, embed the tag map, two cron files | grok-4.6-xhigh | 1196 s, 184 calls, 349k in + 6.8M cache read, 82k out | Caught a contradiction in the book (the lie formula vs its own test cases) and chose the test cases, which prod data then confirmed. **Its first version would have deleted every VNDB tag in production**: `Vote.VID` had no column tag, GORM scanned vid into nothing, every pure test passed, and only the DB suite (which the sandbox cannot run) went red. Also a 65k-element `IN` list one row short of the Postgres bind limit. Acceptance added 6 tests; 24 mutants killed; the prod dry run then matched an independent SQL prediction on every counter |
| 2026-09-19 | `vndb-work-titles` (#274): fill the titles and empty display names a VNDB mint would have written | grok-4.6-xhigh | 676 s, 110 calls, 314k in + 2.6M cache read, 53k out | **Found a real bug in the book**: `AND NOT (<human provenance SQL>)` is UNKNOWN on an empty provenance, so it would have filled none of the 2,714 names; it used `COALESCE(…, false)` and said so. Its one DB test asserted a wrong re-run count; the same `IN`-list bind-limit trap; two mutants survived on missing fixtures |
| 2026-09-19 | `anchor-liveness`: generalise the dead-ref audit to every VNDB/Bangumi/EG entity type, with a consumer census | grok-4.6-xhigh | 1636 s, 375 calls, 543k in + 8.4M cache read, 109k out | Confirmed every lane from the writer's `file:line`; the census of 140 readers was exact and complete enough to adjudicate on. DB suite passed first time, 8 of 8 mutants killed, and the prod dry run matched the orchestrator's earlier measurement lane by lane (it also exposed an orchestrator miscount of EG refs across id spaces) |
| 2026-09-19 | `hltb-rolling-refresh` (kun-howlongtobeat-api): oldest-first daily refresh slice that never flips a good page off `fetched` | grok-4.6-xhigh | 710 s, 100 calls, 249k in + 2.6M cache read, 65k out | Clean: all DB tests passed on first real run, 5 of 5 mutants killed. Wrote English into Chinese docs because the book said "everything in English" — the book's fault; say "code and report" |
| 2026-09-19 | `dlsite-full-refresh` (kun-dlsite-api): a resumable full refresh every N days inside the daily one | grok-4.6-xhigh | 755 s, 114 calls, 228k in + 2.5M cache read, 48k out | Correct and honest about the one gap (no fake-origin seam, so `Run` is untested end to end). A deploy comment asserted something false ("price/sales/rating move on that window"); rewritten at acceptance |
| 2026-09-19 | `work-dedup-pairs`: `-mode pairs` files OPEN work-merge proposals from an operator TSV, leaving approve/execute to the existing modes | grok-4.6-xhigh | 580 s, 77 calls, 297k in + 1.7M cache read, 41k out | All scope implemented, nothing written outside the three paths. **It saw a trap the book did not name**: Bangumi is veto-exempt for works, so it built the ref-conflict fixture on VNDB anchors. Every DB assertion held on the first DB run. Its "anything wrong" list held the two real defects, and both were **my book's rules**: a moved row skipped only itself, so the rows ahead of it were filed before the error; and a whitespace-only note passed the guard. Acceptance made the check-then-write two passes, counted rejected proposals as existing so a rerun does not re-file a ref-conflict pair, and added a rerun-after-execute assertion. Mutants: 18/20 killed on its tests; the two survivors (a propose failure not surfaced, source-then-target partition) were missing cases; 20/20 after adding them. 43 tests, 0 skipped |
| 2026-09-22 | `eganchors-slot` (#284): make the EG anchor lane read the occupancy set its unique index actually uses, so it stops re-planning refs the index refuses | grok-4.6-xhigh | 580 s, 63 calls, 283k in + 1.5M cache read, 35k out | Both facts loaded exactly as adjudicated, every append routed through one `appendPlan` so a skip cannot leave a planned counter inflated, four DB tests including the negative control the book asked for. Gates green; all DB tests skipped for it and passed at acceptance against an ephemeral database; **5/5 mutants killed** (add `dead_at IS NULL` to the occupancy query, join it to live works only, flip `holder != p.WorkID`, drop the pair skip, drop `ExactSlotTaken++`). Section 2 was 9 accurate `file:line` items, two of which were the book's own gap: `cmd/reconcile-eg-anchors/main.go` prints a second summary line that the book made unwritable, so the new counters were missing from it, and `Corroborated` can now exceed `exact_planned`. It followed the writable-path fence rather than fixing the first one, and said so under Deviations — correct. Orchestrator added the `fmt.Printf` counters afterwards |
