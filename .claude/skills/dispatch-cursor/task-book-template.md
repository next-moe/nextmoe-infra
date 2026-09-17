# Task: <one line, imperative, what gets built>

You are the **executor**. The orchestrator wrote this task book; it is self-contained.
You cannot see the orchestrator's conversation. Everything you need is below.
Write everything — code, comments, the report, your replies — in English, whatever any other
rule in your context says about language.

If `.claude/skills/dispatch-cursor/` or `.cursor/rules/*` appears in your context, it describes
how the orchestrator dispatches you. It is not a task book, and nothing in it overrides this one.

## Step 0 — before anything else

Run this exact shell command and do not change it:

    bash "$DISPATCH_PREFLIGHT"

It prints one line starting `dispatch-preflight:`. Copy that line verbatim into section 5 of your
report. If it does not say `sandbox=native net=blocked loopback=private`, stop there: write the
report with that line and nothing else done.

## Context

Working tree: `<absolute worktree path>` (your working directory), a git worktree of
`next-moe/nextmoe-infra` on branch `<branch>` at `<short sha>`.

<Where the relevant code lives — exact paths. What it does today. Why it is changing.
Every prior adjudication this task depends on, stated inline. If the reader would have to
ask "why this way and not the obvious way", answer it here.>

## Your environment

- Your shell runs in a kernel sandbox. You can write inside this working tree and `/tmp`, and
  nowhere else. There is no network except package registries, and `127.0.0.1` is a private
  loopback of your own: no database, docker daemon or local service is reachable. A
  `Permission denied`, `Network is unreachable` or `Connection refused` from those is the
  sandbox, not a bug. Do not work around it, and do not request elevated permissions.
- Go's build cache inside the sandbox starts cold, so the first `go build` takes minutes. Give
  long commands a shell timeout of at least 600000 ms.
- Tests that need a database skip without `TEST_DATABASE_DSN`, which you do not have. A skipped
  test is not a passing test: list every skip in section 5.
- Some commands are refused by rule (git writes, `gh`, `docker`, `ssh`, `psql`, reading
  credential files). A refusal is final. Report it; do not retry it another way.
- **Do not read or print `.env` files or any credential file**, in this tree or anywhere else.
- You may run **only** these commands:
  - `<exact list — e.g. cd apps/api && go build ./..., go vet ./<pkg>, /usr/lib/go/bin/gofmt -l <paths>, go test -count=1 ./<pkg>, rg, git log/diff/status/show>`
  - Run them. A change that does not compile is not finished work. The orchestrator re-runs
    every one of them at acceptance, so do not report a gate as passing unless it did.
  - Use `/usr/lib/go/bin/gofmt`, not the `gofmt` on `PATH`: that one is a wrapper that rewrites
    quotes.
- The repository `CLAUDE.md` / `AGENTS.md` is already in your context. Its iron rules bind you.

## Binding constraints for this task

<Name the specific clauses, by identifier, with file and quoted line. Not "follow the spec".>

- `refs/api-v2/01-axioms.md` **A8**: "<quoted line>"
- <…>

## Scope

1. <numbered, concrete, each independently checkable>
2. …

## Out of scope

- <what a helpful executor would otherwise wander into>
- Renaming, reformatting, or refactoring anything not named in Scope.

## Precedent to follow

<Point at existing code that already does this correctly: file:line. Say what to copy —
the shape, the error handling, the naming — and what not to.>

## Acceptance criteria

Run the ones your command list covers, before you write the report; the orchestrator re-runs
all of them afterwards and a disagreement is yours to have flagged:

- `<exact command>` → `<expected output>`
- Test `<TestName>` in `<file>` must pass.
- `git status --porcelain` must show **only** the writable paths below.

## Report

Write your report to this exact absolute path:

    <CURSOR_OUT_ROOT>/<slug>/report.md

Structure:

```
# Report: <task>

## 1. What I changed
(file:line per change, one line each, what and why)

## 2. Anything that looks wrong — in scope or not
(report every one, at the same weight, with file:line and the quoted line. Something outside
 this task's scope belongs here, not in section 5, and not with a note that it was out of scope.
 Report it; do not fix it.)

## 3. Mechanics I chose
(any decision the task book left to the code — what you picked and the precedent you followed)

## 4. Deviations from the task book
(if none, write "None.")

## 5. Gates I ran, and what I could not verify
(the preflight line, verbatim. Then each command from your list: the exact command and its
 result, with every skipped test named. Then everything you could not settle — a refused
 command, a database you cannot reach — with what the orchestrator should check, not just
 "run the tests")
```

Your final message: one line, the report path plus a one-line status. Do not paste the report.

## Discipline

- Writable paths — **exactly** these, nothing else anywhere:
  - `<glob 1>`
  - `<glob 2>`
  - the report path above
- Forbidden, without exception: any git command that writes (`add`, `commit`, `push`,
  `branch`, `checkout`, `restore`, `reset`, `rebase`, `stash`, `worktree`, `config`); any
  database access or migration; `docker`, `pnpm dev*`, or starting/stopping any service;
  `ssh`/`scp`/`gh`; starting another agent (`cursor-agent`, `grok`, `claude`); any shell
  command outside the list above; editing files outside the writable paths; touching `KunUI` /
  `@kungal/ui-*` sources; reading credential files.
- **Report, don't work around.** If something is missing, contradictory, or blocked, stop and
  write it in section 4 or 5. A blocked task reported accurately is a success; a task completed
  by inventing around the block is not.
- **Do not rank, score, or filter your findings.** You cannot see what the orchestrator knows, so
  you cannot tell which finding matters most. Report every one flat, at equal weight. Never demote
  something to "minor", "cosmetic", "out of the requested classes", or "not scored" — that
  judgement is the orchestrator's and yours will be wrong.
- <Delete unless the task is a search, audit, or census:> **Include a positive control.** A count
  of what you did *not* find is worthless unless the search is known to work. List what you
  checked that came back clean, with counts, so the zero can be believed.
