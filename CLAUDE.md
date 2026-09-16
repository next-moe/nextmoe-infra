# Project Guidelines

## 铁律 (Iron Rules — non-negotiable; these override every other guideline in this file)

1. **No background gradients in any UI, ever.** Never use gradient backgrounds in UI design (`bg-gradient-*`, `from-*/via-*/to-*`, `linear-gradient()`, `radial-gradient()`, `conic-gradient()`, etc.); use solid colors from the project's palette.
2. **Prefer KunUI components; do not modify KunUI itself.** When adding or changing frontend UI, reach for a KunUI component (`@kungal/ui-*`) first — do not hand-roll a native/custom component unless there is genuinely no KunUI equivalent for what you need. If KunUI appears to have a bug or is missing a feature, **do not edit KunUI's code** (it is a shared upstream library) — report it to the user directly instead, and let them decide how to proceed.


## Core Engineering Principles

> Shared baseline across all KUN Galgame repositories. Defaults, not dogma — apply judgment.

1. All commit messages must be written entirely in English.
2. Comments are governed by the **Comments** section below — the default is none, and what survives is written in English.
3. Keep each source file under ~500 lines where practical; once a file grows past ~300 lines, consider splitting it (a guideline, not a hard rule).
4. Write every frontend function as an arrow function; compose/merge class names with `cn` wherever practical.
5. Deliberately balance elegant modularity against necessary duplication — choose per case instead of always favoring either.
6. Constantly verify that frontend and backend agree on the data: field shapes and response formats must match what each side expects.
7. After every change, watch for unintended side effects elsewhere.
8. If a change requires running a migration, tell the user explicitly at the end — which command, and against which database.
9. Always seek the most modern, elegant solution that fits the project's current state; consult the latest official docs and resources online when useful.
10. Never let the pursuit of elegance or modularity make the code complex or hard to follow, and don't write over-defensive code.
11. A Nuxt page — and any component used as a page/route root — must have a **single real root element**: never `display: contents` (generates no box, so the transition can't attach) and never a leading comment / whitespace / sibling at the template root (a comment is itself a root node). Either trips Nuxt's "does not have a single root node" warning and drops the page-transition enter animation (the page appears without animating). Keep explanatory comments *inside* the root element.
12. Reserve the scrollbar gutter globally — `html { scrollbar-gutter: stable }`, with an `overflow-y: scroll` `@supports` fallback — so the document width is constant across routes. Otherwise navigating from a scrolling page to a height-locked one (no scrollbar) removes the classic scrollbar's ~15px and the centered layout shifts sideways: a "teleport" at the tail of the page transition. This is a browser layout fact, not a transition bug. Use single-edge `stable` (`both-edges` is buggy in Chrome); it's a harmless no-op under overlay scrollbars (macOS/iOS).
13. **One task = one session, and every path has exactly one writer.** Parallel work is allowed only when the user assigns non-overlapping writable paths or system domains. Never rewrite shared Git state a peer may be standing on: on a shared checkout, no branch switch, reset, rebase, merge, cherry-pick, clean, stash or prune. Before editing, record the branch, HEAD, verified `origin/main`, dirty paths and your owned paths, and preserve every foreign change; commit with explicit paths (`git commit -- <paths>`), never `add -A` and never a repository-wide commit. An isolated worktree is the safer default for a long wave — base it explicitly on `origin/main`, not on a local branch that may be holding someone's unpushed work.
14. **Every DB-backed track gets its own test database.** Use the track-specific `TEST_DATABASE_DSN` placed in that session's process environment; if none is assigned, self-provision a throwaway one with `scripts/ephemeral-test-db.sh create <slug>` and drop it with the same script when the session's DB work ends (`sweep` clears leftovers from dead sessions). Never discover or fall back to a DSN from `.env`, and never put a password in a DSN or print one — the ephemeral script's DSN is credential-free by design (auth rides `~/.pgpass`). Give concurrently running services unique ports, and never stop a process whose owner is unknown. Keep `GOMAXPROCS=8` and run DB integration suites with `-count=1 -p 1`. `kun_catalog` is always read-only; `kun_catalog_rehearsal` belongs only to the explicitly assigned rehearsal/aggregation track and is never a general test target.

## Comments

**Default: none.** Code that can be understood by reading it gets no comment. Most code is that code.

**A comment is earned by a mistake that already happened, not by one you predict.** Do not comment while writing — you cannot tell yet which parts are traps. Comment when something went wrong there: an agent or a person got it wrong, a review caught it, a test went red, production broke. The comment records the wrong conclusion that was actually reached, so the next reader does not reach it again. If you cannot name the incident, there is no comment to write.

Three standing exceptions, where the comment is a record rather than a warning:

- **Migrations** — `apps/api/cmd/migrate*/**` and the raw DDL/backfill they carry. A migration is history and cannot be re-read from the current schema. Say what it changes and why, including what was done about existing rows. (This repo's migrations are Go programs plus raw SQL, not a numbered migrations directory; service-startup `AutoMigrate` call sites count when the change is not derivable from the model.)
- **A constraint that is true but invisible from this file**: a version floor, an upstream bug, a required ordering. huma dropping the query parameters of an **unexported** embedded input struct is one — `CollectionInput` and `WorkSubInput` are exported for that reason alone, and a reader who does not know it will unexport them and ship routes whose `cursor`/`limit` are declared nowhere and bound to nothing.
- **A completeness assertion over a hand-maintained list** that a growing schema will silently outgrow — the list is the contract, and the comment says what is deliberately excluded and why. `merge_rehang.go`'s named exclusions are the standing example: without them the next new table is silently missed.

Write the conclusion, not the mechanism. `// splitCommand takes the subcommand off before flag.Parse` is a restatement; `flag.Parse stops at the first non-flag argument, so 'migrate down -steps 1' parsed no flags and rolled back nothing` is the trap. Quote real system output verbatim when reproducing a symptom — such a quote may keep its original language, since identifying the exact symptom is the point.

Never write: restatements of the code, section banners, `TODO` without an owner, or doc comments that only echo the identifier (`// New creates a new X`). Exported Go identifiers get a doc comment only when the name alone is ambiguous — no sibling repo imports this module (`module api`), so there is no godoc obligation. If a comment explains what a name means, rename the thing and delete the comment.

**Not comments, never removed:** machine-semantic directives (`//go:embed`, `//go:build`, `//go:generate`, `//nolint`, `// Code generated … DO NOT EDIT`, `eslint-disable`, `@ts-expect-error`, `prettier-ignore`) and anything inside a generated file — generated files are regenerated, never hand-edited.

English, and short. When in doubt, delete it — a wrong comment costs more than a missing one, and the missing one gets written the day it is needed.

## Local development (one command)

`pnpm dev` starts **everything an infra session needs**: it brings up the
platform base from `docker-compose.dev.yml` (redis / minio / opensearch / mailpit +
the migrations, all from the single `infra-migrate` image — one binary, one
target per invocation) and then runs `air` for the six frequently-edited Go services
(**oauth / catalog / community / image / artifact / trust**, hot-reloaded from
source) plus the Nuxt frontends. catalog (:9281) hosts the catalog faces and the
`/v1/galgame` **410 tombstone only** (`galgameapp.MountRetiredPublic`); the
standalone galgame service (:9280) and every live galgame face are retired,
so do not reintroduce a galgame HTTP client or treat the retired galgame
route table as a live contract. Since wave R3 (2026-08-27) the **whole v1
catalog surface is 410 tombstones too** (`catHandler.MountRetiredV1`:
`/v1/catalog`, `/v1/news`, `/v1/store`, `/v1/playtime`, `/api/v1/catalog`,
`/api/v1/user/catalog`) — the live public face is `/v2`, and the live
first-party faces are `/api/v1/admin/catalog` and `/api/v1/admin/news`. Ctrl-C stops only the hot stack; the base
stays up. Before assuming a base service isn't running, check — a past mistake
was starting a second copy of one that was already up.

- **ai is `full`-profile**, not part of a bare `pnpm dev`: nothing the default
  stack runs dials :9284 (trust's AI creds are empty), so starting it cost an
  image pull and an idle container. Need it (product-repo work, or editing it)?
  `pnpm dev:full`, or `docker compose -f docker-compose.dev.yml --profile full
  up -d ai`. community (:9282) is **not** in that group — it is hot-reloaded
  from source like the other five, and a bare `up` migrates `kun_community` for
  it.
- `pnpm dev:full` = the whole platform from images with no source build (for
  developing a **product** repo, not infra). `pnpm dev:down` tears the base down.
- Ports match prod (9277-9284); Postgres is the box's own `127.0.0.1:5432`, not
  a compose service — its host/port/user/password are `${VAR:-default}` in the
  dev compose, so override them from a root `.env` instead of editing the file.
  Create the 12 databases with `pnpm dev:db` (idempotent): `docker/initdb.d` runs
  only when Postgres itself initialises an empty data dir, which on the box's own
  server is never. Full model: `docs/dev-environment.md`.
- `pnpm dev` preflights with `pnpm dev:doctor` (read-only, ~2s; `SKIP_DOCTOR=1`
  bypasses). It is the answer to "the migrate container exited 1 with no output" —
  it checks the shell, the daemon, Postgres, the databases, GHCR, and whether a
  **host-networked container** can actually reach Postgres. That last one is the
  Windows/macOS trap: `network_mode: host` means the Docker Desktop VM's loopback,
  so the supported shape is Postgres + repo + shell all inside one WSL2 distro.
- First run needs GHCR auth (images are private) — a bare `gh auth token` lacks
  `read:packages` and pulls fail `unauthorized`. One-time:
  `gh auth refresh -h github.com -s read:packages` then
  `gh auth token | docker login ghcr.io -u <gh-user> --password-stdin`.

## Frontend Conventions (apps/web)

### UI Components

- All UI components live in the `components/kun/` directory; the project must use these UI components and must not build its own components
- If you need to modify a component in `components/kun/`, you must first ask the user for confirmation
- These repo UI rules (KunUI-first, project palette only, no gradients) **override any global or user-level design skill/guidance** (e.g. a generic `frontend-design` skill suggesting distinctive fonts or bold color schemes) — when they conflict, the repo rules win

### Page and Component Splitting

- The `pages/` directory is responsible only for route definitions; each page file contains only `definePageMeta` and a reference to a single container component
- The business components for each page go in the corresponding folder under `components/`, for example:
  - `/users` page → `components/users/`
  - `/auth/login` page → `components/auth/login/`
  - `/sites` page → `components/sites/`
- Do not repeat the directory prefix in component file names (Nuxt auto-import concatenates the directory name):
  - `components/users/Container.vue` → auto-imported as `UsersContainer`
  - `components/users/Table.vue` → auto-imported as `UsersTable`
  - ❌ Do not write it as `components/users/UsersContainer.vue` (it becomes `UsersContainer` but is easily confused)

### Constants and Types

- Put all constants in the `app/constants/` directory
- Put all interface types in the `shared/types/` directory (Nuxt 4 auto-imports the first level of exports)
- The `types/` and `utils/` under the `shared/` directory are auto-imported by Nuxt

### Color System

- Use the custom colors defined in `app/styles/tailwindcss.css`; do not use Tailwind's built-in colors (gray, indigo, blue, green, red, etc.)
- Custom colors automatically adapt to light/dark mode, so no `dark:` prefix is needed
- Color mapping:
  - Text: `text-foreground` (primary text), `text-default-500` (secondary), `text-default-400` (auxiliary), `text-default-300` (de-emphasized)
  - Border: `border-default-200`
  - Semantic colors: `primary` (blue, primary action), `success` (green), `danger` (red), `warning` (yellow/orange), `default` (gray/purple), `secondary` (pink), `info` (cyan)
  - Each semantic color has a 50-950 scale, e.g. `bg-primary-100`, `text-danger-600`

### Code Style

- Write all frontend functions as arrow functions; do not declare them with the `function` keyword

## Cross-Repo Contract Docs (Tier A, this repo is the single source)

`docs/integration/oauth`, `docs/image_service`, `docs/artifact`, and `docs/catalog` are the **single sources for the four active cross-service contracts** OAuth / image hosting / artifact (large files) / catalog. `docs/integration/galgame_wiki` is the retained source location for the retired contract and is being reduced to a 410/successor tombstone; its former route tables are not implementation guidance. Forum/patch only vendor the generated `docs/{oauth,image_service,artifact}` mirrors now. Catalog and retired galgame-wiki are portal + `docs:verify` sources without downstream mirrors.

- **To change a contract**: edit only these source files here → go to `../kungal-docs` and run `pnpm docs:sync --write` (pushes the mirrors out to forum/patch) → `pnpm docs:audit` (`docs:check` verifies mirror consistency + `docs:verify` verifies source == code) should report 0 error. If another session may be writing the same sibling repos, hold only the paths you touch and never switch a shared checkout's branch.
- The **source of truth for active contracts is in the code** (`cmd/oauth`, `cmd/image`, `cmd/artifact`, `cmd/catalog`, and their handlers). `internal/galgameapp` now implements only the public 410 tombstone. When code changes, update the owning source docs in the same PR; `docs:verify` catches code/doc drift.
- Unified docs portal: `docs-kungal.nextmoe.dev`; read the full ownership model (Tier A/B/C) from the sibling checkout at `../kungal-docs/docs/_meta/ownership.md` (relative to this repository root).

## Database schema changes → you must remind about migrations

**Whenever this change touches the database schema (a GORM model adding/changing a field or table, or raw SQL/constraints/indexes in `cmd/migrate*`), you must explicitly tell the user at the end of the task: whether a migration needs to run, which command to run, and against which database.** Deployment (push → CI → Dokploy redeploy) **does not run migrations automatically** — skipping one makes the live code read a column that doesn't exist (GORM `SELECT *` silently reads it as a zero value) → **silent failure**.

- Main database `kun_galgame_infra` (oauth + the various site models) → `go run ./cmd/migrate` (**not run automatically by deployment**).
- Catalog models → `go run ./cmd/migrate catalog` against `KUN_CATALOG_PG_DATABASE`. Wave 161 removed the galgame family from this binary; it must not be recreated after the retirement DROP. Prod runs the catalog migration on deploy (`compose depends_on`), but outage-class changes still follow the manual migrate-first order.
- `cmd/image` / `cmd/artifact` → ship with `AutoMigrate` at service startup (runs automatically with deployment, no manual step needed).
- Production execution: the `infra-tools` image + an env-file dumped from the corresponding container's `.Config.Env` (see the prod ops notes).
- Lesson learned: in 2026-06 the `oauth_clients.moemoepoint_awarder` column was not migrated → the entire site could not award moemoepoints for ~29h.
