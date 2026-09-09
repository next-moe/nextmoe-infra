# Local dev environment — the one-command platform

`docker-compose.dev.yml` (infra repo root) brings up the **whole nextmoe platform**
on your dev box with one command, so any product repo's `pnpm dev` / `air` can
depend on a single fact: **localhost has a platform**. The platform services
(from prebuilt GHCR images) + all local infrastructure, **zero cloud credentials**.
A bare `up` pulls only what an *infra* session consumes; everything a *product*
repo needs on top of that sits behind the `full` profile.

> This is the dev-environment track's step 01. Step 02 (`refresh-dev-db`) fills the
> databases with desensitised, real-shaped data; step 03 wires each product repo's
> `.env.example` to these ports. This file only stands up the services.

## What runs

| Service | Port (host) | Image / origin | Browser UI |
| --- | --- | --- | --- |
| oauth | 9277 | `ghcr.io/next-moe/infra-oauth` | — |
| image | 9278 | `ghcr.io/next-moe/infra-image` | — |
| artifact | 9279 | `ghcr.io/next-moe/infra-artifact` | — |
| catalog (hosts the galgame surface; :9280 retired) | 9281 | `ghcr.io/next-moe/infra-catalog` | — |
| community *(`full`)* | 9282 | `ghcr.io/next-moe/infra-community` | — |
| trust | 9283 | `ghcr.io/next-moe/infra-trust` | — |
| ai *(`full`)* | 9284 | `ghcr.io/next-moe/infra-ai` | — |
| image-cdn-proxy (Caddy) | 9290 | `caddy:2-alpine` | — |
| MinIO (S3) | 9000 / 9001 | `minio/minio` | http://127.0.0.1:9001 (minioadmin/minioadmin) |
| Mailpit | 1025 / 8025 | `axllent/mailpit` | http://127.0.0.1:8025 |
| OpenSearch | 9200 | `ghcr.io/next-moe/infra-opensearch` | http://127.0.0.1:9200 |
| Redis | 6379 | `redis:8-alpine` | — |
| **Postgres** | **5432** | **your host's own server — NOT in compose** | — |

The ports match production. Frontends (web/wiki/forum/moyu/letmoe) and out-of-band
jobs (image-gc, refping, cron, data-cutover tools) are **deliberately not here** —
frontends run their own `pnpm dev`, jobs run on demand.

## Two ways to run it — hybrid vs all-from-images

**oauth / catalog / image / artifact / trust** carry the compose `full` profile
because `air` rebuilds them from source, and their host ports must stay free.
**community / ai** carry it for a different reason: nothing the default stack
runs dials :9282 or :9284 (community's trust forwarding is off, and trust itself
has empty AI creds), so starting them cost every contributor two image pulls and
two idle containers. A plain `docker compose … up` therefore starts neither
group. This gives two modes:

- **Developing infra itself** → from the infra repo, `pnpm dev` (one command).
  It runs the default compose up (storage + migrations) and then `air`, which
  rebuilds the five hot services from source on every save, plus the Nuxt
  frontends. Ctrl-C stops the hot stack; the base keeps running. `pnpm dev:down`
  stops the base. Need community or ai for what you're working on? Add them:
  `docker compose -f docker-compose.dev.yml --profile full up -d community ai`
  (or hot-reload one with `go run ./cmd/<svc>` — Replace mode, below).
- **Developing a product repo** (letmoe / forum / moyu / …) → you want the WHOLE
  platform from images, no source build. Use `--profile full`:
  `docker compose -f docker-compose.dev.yml --profile full up -d` (or, from the
  infra repo, `pnpm dev:full`). Then run the product repo's own `pnpm dev`.

Everything below (`docker compose … up -d`) is written for the all-from-images
mode; add `--profile full` to include the five hot services.

### Network mode: `host`

Every service uses `network_mode: host`, so:

- inter-service URLs are all `127.0.0.1:<port>` (no service-name DNS), and
- a container's port is bound on the box directly — which is exactly what makes
  **Replace mode** (below) seamless.

### Windows / macOS: run everything inside WSL2 (or a Linux VM)

`network_mode: host` is a **Linux** feature. Under Docker Desktop the containers
live in a Linux VM, so "host" means *that VM's* network namespace — not your
Windows or macOS machine. Two consequences, and they are the usual cause of a
migrate job dying instantly on a fresh Windows checkout:

- a container's `127.0.0.1:5432` reaches the **VM's** loopback, so a Postgres
  installed on Windows is invisible to it; and
- the services' ports are bound in the VM, not on your machine.

There is no `extra_hosts` / `host.docker.internal` fallback in the compose file
— pointing one service at the Windows host would not fix the other direction
(services reaching *each other* on `127.0.0.1`). So the supported path is
**everything inside one WSL2 distro**: Postgres, the repo, and the shell you run
`pnpm dev` from. `pnpm dev` is `bash scripts/dev.sh` and does not run under
`cmd.exe` / PowerShell anyway.

#### Windows setup, start to finish

```powershell
wsl --install -d Ubuntu     # PowerShell, once; reboot if it asks
```

Then, **inside the Ubuntu shell** (everything below is WSL2, never PowerShell):

```sh
sudo apt update && sudo apt install -y postgresql postgresql-client git curl jq
sudo service postgresql start
sudo -u postgres psql -c "ALTER USER postgres PASSWORD 'pick-your-own';"

# Clone into the LINUX filesystem. Not /mnt/c — cross-OS 9p writes are slow
# enough that air's rebuild loop becomes unusable.
cd ~ && git clone <this repo> && cd nextmoe-infra

printf 'KUN_PG_PASSWORD=pick-your-own\n' > .env   # root .env, gitignored
./scripts/create-dev-databases.sh                 # creates the 12 databases
pnpm install && pnpm dev
```

Docker: either Docker Desktop with the WSL2 integration enabled for this distro,
or `apt install docker.io` **inside** the distro. Both work — the requirement is
that the daemon and Postgres agree on what "host" means, which `pnpm dev:doctor`
verifies for real (it runs a host-networked container and has it dial your
Postgres port).

Two Windows-specific failure shapes worth naming:

- **`set: pipefail^M: invalid option name`** (or `$'\r': command not found`) —
  Git for Windows checked the scripts out with CRLF. `.gitattributes` pins `*.sh`
  to LF, so a fresh clone is fine; an older clone is fixed with
  `git rm --cached -r . && git reset --hard`.
- **Windows-side Postgres, WSL2-side stack** — the doctor reports it as "a
  host-networked container CANNOT reach …". Install Postgres in the distro
  instead; that is the supported shape.

macOS has the same VM problem with no WSL2 to fall back on: use a Linux VM, or
run the Go services natively (`go run ./cmd/<svc>`, Replace mode below) against a
native Postgres and skip the containerised services.

### Preflight: `pnpm dev:doctor`

`pnpm dev` runs this first (`SKIP_DOCTOR=1` bypasses it), and it is worth running
on its own whenever something is off. It is read-only — starts nothing, creates
nothing — and checks, in order: the shell is not Git Bash; docker / jq / pnpm and
a reachable daemon; Postgres answering on the configured coordinates; **a
host-networked container reaching that same port** (the check that catches the
Docker Desktop shape); every database present; and GHCR readable. Each failure
prints the fix.

## Prerequisites

1. **A Postgres server you own** — the dev source of truth, and *not* a compose
   service. The compose defaults are `127.0.0.1:5432`, user `postgres`, password
   `191007`; those are just this repo's original dev box, not a convention you
   have to adopt. Override any of them from a **root `.env`** (gitignored, sits
   next to `docker-compose.dev.yml`) — the compose file reads
   `${KUN_PG_HOST:-…}` / `${KUN_PG_PORT:-…}` / `${KUN_PG_USER:-…}` /
   `${KUN_PG_PASSWORD:-…}`:

   ```sh
   # .env  (repo root)
   KUN_PG_PASSWORD=whatever-your-local-postgres-uses
   ```

   Then **create the databases**. `docker/initdb.d/01-create-databases.sh` only
   runs when Postgres itself initialises an empty data dir — and Postgres is not
   a compose service here, so on your own server it never runs automatically:

   ```sh
   ./scripts/create-dev-databases.sh           # or: pnpm dev:db
   ./scripts/create-dev-databases.sh --check   # report only, create nothing
   ```

   It reads your coordinates from the rendered compose config (so the `.env`
   above applies) and the database list from that same initdb hook, and skips
   the ones that already exist. Twelve in total: `kun_galgame_infra`,
   `kun_images`, `kun_artifacts`, `kun_catalog`, `kun_community`, `kun_trust`,
   `kun_ai`, `kun_news`, plus the downstream repos' (`kungalgame`,
   `kungalgame_patch`, `kungalgame_sticker`, `kun_letmoe`).

   A migrate job that exits 1 seconds after start is almost always one of these
   two: wrong password, or a database that doesn't exist. `pnpm dev:doctor` says
   which without reading a single log line.

2. **GHCR access for the platform images** (they are private). The default `gh`
   token does **not** carry `read:packages`, so `docker login` with it fails
   `unauthorized` — add the scope once, then log in:

   ```sh
   gh auth refresh -h github.com -s read:packages   # interactive (device code); one time
   gh auth token | docker login ghcr.io -u <your-gh-user> --password-stdin
   ```

   `docker login` must print `Login Succeeded`; the credential persists in
   `~/.docker/config.json` (no need to repeat it). Alternatively use a classic
   PAT scoped to `read:packages` if you'd rather not widen the `gh` token.

## Bring up

```sh
# 1. Check nothing you care about already holds these ports — and NEVER kill a
#    process that does; stop the matching compose service instead (see below).
ss -tlnp | grep -E ':(9277|9278|9279|9281|9282|9283|9284|9000|9001|7700|1025|8025|9290|6379)\b'

# 2. Pull + start the WHOLE platform (--profile full includes the five hot
#    services; drop it for the hybrid `pnpm dev` mode). migrate-* run first and
#    gate the services.
docker compose -f docker-compose.dev.yml --profile full pull
docker compose -f docker-compose.dev.yml --profile full up -d

# 3. Watch health.
docker compose -f docker-compose.dev.yml ps
```

Pin an image tag with `INFRA_IMAGE_TAG` (a `sha-…` tag or `@sha256:` digest) when
you need reproducibility across a team:

```sh
INFRA_IMAGE_TAG=sha-abc1234 docker compose -f docker-compose.dev.yml up -d
```

### Port already in use?

`docker compose up` does **not** skip a busy port — it fails to bind. If a port is
held by *your own* native process (e.g. you already run `air` in `apps/api`, which
binds 9277-9279 / 9281 / 9283), that is the intended Replace-mode situation: just don't start
that container. Start a subset explicitly, e.g. only the infra + the two services
you need:

```sh
docker compose -f docker-compose.dev.yml up -d minio minio-setup mailpit opensearch image-cdn-proxy catalog community
```

## Verify (healthz checklist)

Every platform service answers `GET /healthz` → `{"status":"ok"}`:

```sh
for p in 9277 9278 9279 9281 9282 9283 9284; do
  printf '%s ' "$p"; curl -fsS "http://127.0.0.1:$p/healthz" && echo || echo DOWN
done
curl -fsS http://127.0.0.1:9000/minio/health/live && echo minio-ok   # MinIO
curl -fsS http://127.0.0.1:9200/_cluster/health && echo               # OpenSearch
curl -fsS http://127.0.0.1:8025/api/v1/messages?limit=1 >/dev/null && echo mailpit-ok
```

- **MinIO console**: http://127.0.0.1:9001 (minioadmin / minioadmin)
- **Mailpit** captures every outbound mail (registration codes, etc.) — read them
  at http://127.0.0.1:8025. Nothing is ever delivered to a real inbox.

## Replace mode (stop a container, `go run` in its place)

Because host mode binds the prod port on the box, you can develop one service
against the live containerised rest of the platform:

```sh
docker compose -f docker-compose.dev.yml stop catalog      # free port 9281
cd apps/api && go run ./cmd/catalog                        # your code now IS the platform's catalog
```

Your local process reads the same host Postgres / MinIO / OpenSearch as the containers,
so the rest of the stack talks to it transparently. Restart the container when done:

```sh
docker compose -f docker-compose.dev.yml start catalog
```

The same works for any of oauth / image / artifact / community / trust / ai —
each is `go run ./cmd/<svc>` (see `apps/api/dev.sh` for the env each expects; the
container env in `docker-compose.dev.yml` is the canonical list).

## Image reads: the read-through CDN proxy

The image service emits content-addressed CDN URLs of the shape
`/<h1>/<h2>/<hash>.<ext>`. In dev its `KUN_IMAGE_PUBLIC_BASE_URL` points at the
Caddy proxy on **:9290**, which:

1. tries the **local MinIO** `kun-images` bucket first (a hit means you uploaded /
   seeded it locally), and
2. on a miss transparently **falls back to the prod public CDN**
   (`https://image.kungal.iloveren.link`), so images that were never seeded locally
   still render.

Uploads (write path) always go to local MinIO — there are **no cloud credentials**
on the dev box. Artifact downloads have **no** such proxy by design: a prod
artifact simply misses locally (upload a fresh one to exercise the flow).

## Data & credentials

- **Client secrets are not this file's concern.** The platform services only need
  DB / S3 / SMTP to boot; OAuth `client_secret`s live in the product repos'
  `.env.example`. Avatar/banner image-upload creds (`KUN_IMAGE_CLIENT_*`) are left
  empty here — set them only if you exercise those upload paths locally.
- **trust/community forwarding envs are empty = fail-closed off**, mirroring prod
  until a real cut-over wires them.
- Realistic data comes from **step 02** (`refresh-dev-db`); a bare bring-up gives
  you empty (freshly migrated) databases.

## Refreshing the databases (step 02 — `refresh-dev-db`)

`docker-compose.dev.yml` gives you empty, freshly-migrated databases. To fill
them with **desensitised, real-shaped production data**, use one command:

```sh
./scripts/refresh-dev-db.sh                    # all core DBs, latest artifact
./scripts/refresh-dev-db.sh --fresh            # rebuild the artifact on the server first
./scripts/refresh-dev-db.sh --db kun_community # just one core DB
./scripts/refresh-dev-db.sh --group sources    # stream the raw scrape DBs (dlsite/erogamescape)
```

Desensitisation happens **at the source** (裁定 1a): the prod host produces
already-clean artifacts; **raw production PII never reaches your dev box.** The
local script only downloads + restores (and deletes the download afterwards).

### Groups

| Group | Databases | How |
| --- | --- | --- |
| `core` (default) | kun_galgame_infra, kungalgame, kungalgame_patch, kun_community, kun_catalog, kun_images, kun_artifacts | download desensitised `*.dump`, `terminate → drop → create → pg_restore -j4` |
| `sources` | dlsite, erogamescape | raw `pg_dump -Fc | pg_restore` stream — **zero PII, zero desensitisation**, no artifact |
| — | **kun_trust** | *not in any group.* Local trust = `go run ./cmd/migrate trust` (re-seed). |
| — | **letmoe (any `*letmoe*`)** | **hard-refused** by the script — letmoe runs its own seed system. |

After a restore the script runs **PII assertions** (no real emails, empty
sessions, every OAuth secret = the dev derivation, …) and exits non-zero if any
fail — the pipeline proves it desensitised. Re-check any time without restoring:
`./scripts/refresh-dev-db.sh --assert-only --db kun_galgame_infra`.

### Desensitisation contract (what changes)

Server-side pipeline: `scripts/dev-snapshot/build-snapshot.sh` +
`scripts/dev-snapshot/scrub/<db>.sql` (one auditable SQL file per scrubbed DB).
It copies each prod DB **read-only** into a throwaway `dev_snapshot_scratch_<db>`
on the server, scrubs *that*, dumps it, and drops it — production is never
mutated.

| Data | Becomes |
| --- | --- |
| `users.email` / `original_email`, `user_migrations.source_email` | `user<id>@dev.local` |
| `users.password` | **argon2** of **`kungal-dev`**(主列走 argon2 校验;一个常量,任意账号用密码 `kungal-dev` 登录,邮箱形如 `user<id>@dev.local`) |
| `kungal_password` / `moyu_password` | **bcrypt** of `kungal-dev`(legacy 列走 bcrypt 校验,算法与主列不同) |
| `users.ip`, `kungalgame_patch."user".ip`, `images.first_uploader_ip` | emptied |
| `oauth_clients.secret` | `sha256:` + hex(sha256(**`dev-secret-<client_id>`**)) — a client presenting the plaintext `dev-secret-<client_id>` authenticates |
| `oauth_clients.redirect_uris` | first-party localhost dev callbacks ensured present (forum :2333, patch :6969, wiki :9421) |
| `sessions`, `authorization_codes`, `password_resets`, `signing_keys`, `oauth_accounts` tokens | emptied (signing_keys: dev runs HS256 / self-bootstraps a fresh KEK) |
| private chat + DM content (`chat_message`, `message`, `user_message`, edit history) | `[dev-scrubbed] …` synthetic text |
| `kun_community` **held** posts (`status=1`) body + `community_flag.note` | `[dev-scrubbed] …` synthetic text |
| public content (topics, replies, comments, resources, catalog, wiki, images) | **preserved verbatim** |

The dev credentials above are **public by design** (裁定 3) — hard-code them in
each product repo's `.env.example` (step 03 consumes this).

### trust: re-seed, don't restore

`kun_trust` is never in a snapshot. Bring it up locally with the migration:

```sh
cd apps/api && go run ./cmd/migrate trust      # creates + seeds kun_trust
```

If you need a dev subject-kind registration (a **dev** callback secret, unrelated
to the prod registry), insert one by hand — e.g.:

```sql
-- kun_trust: register a dev subject kind for local S2S callbacks
INSERT INTO trust_subject_kind (site, key, callback_url, callback_secret, is_deprecated, notify_on_dismiss, created_at)
VALUES ('kungalgame', 'forum_topic', 'http://127.0.0.1:9282/internal/trust/callback', 'dev-trust-callback-secret', false, false, now())
ON CONFLICT DO NOTHING;
```

### ⚠️ Schema truth lives in the migrations, never in a snapshot (裁定 1c)

A snapshot carries whatever schema production had **when it was taken**. If your
local code has a newer migration than the snapshot, **run that repo's migration**
— do not wait for the next snapshot and do not hand-patch columns:

```sh
cd apps/api
go run ./cmd/migrate           # kun_galgame_infra (oauth + site models)
go run ./cmd/migrate catalog   # galgame models + catalog models — one entry point, two pools, both on kun_catalog (dev split retired 2026-07-29)
# cmd/image, cmd/artifact AutoMigrate on boot
```

The snapshot is a **data** fixture and at most a drift *detector* — the
migrations are the single source of truth for structure.

## Lightweight dev-seed (step 02-lite — for collaborators without server access)

The full snapshot pipeline needs SSH to the prod host. Collaborators who only
need *something realistic to develop against* use the **dev-seed**: a few
hundred entities per database (full FK closure, all seven core DBs), sampled
from the desensitised snapshot and published as a rolling GitHub Release
(`dev-seed` tag on this repo, a few MB total). All it needs is an
authenticated `gh`, plus `psql`/`pg_restore`:

```sh
./scripts/restore-dev-seed.sh --yes            # download + verify + drop/create/restore all 7 DBs
DEVSEED_SUFFIX=_seed ./scripts/restore-dev-seed.sh --yes   # keep existing DBs, restore beside them
```

Every seeded account logs in with password `kungal-dev` (same dev-credential
contract as the snapshot). Desensitisation is inherited — the seed is sampled
*from* the scrubbed snapshot databases, never from raw prod. Content-side
image/file hashes may dangle by design: bytes live in prod object storage and
are not part of any seed.

Producing it (maintainer side, this box): `scripts/dev-seed/build-seed.sh`
samples the local snapshot DBs (`prune/<db>.sql` per DB, keep-sets + FK-enforced
deletes) and `publish-seed.sh` replaces the release assets in place. A weekly
systemd user timer (`scripts/dev-seed/systemd/`) keeps the release fresh —
freshness tracks whatever the last `refresh-dev-db` pulled.

## Wiring a product repo (per-repo quickstart index)

Once the stack is up (and optionally refreshed), each product repo needs only its
own `.env` copied from the checked-in `.env.example` — every example already points
at the ports above and carries the **public dev OAuth credentials** (裁定 3). The
universal three steps:

1. `docker compose -f docker-compose.dev.yml --profile full up -d` (this repo, or
   `pnpm dev:full`) — the **whole** platform from images. `--profile full` matters:
   a bare `up` omits the five hot services (oauth/catalog/image/artifact/trust),
   which is what infra's own `pnpm dev` wants (it runs those from source via air).
   A product repo is NOT running air, so it needs them from images → `--profile full`.
2. `./scripts/refresh-dev-db.sh` (optional) — real-shaped, desensitised data.
3. In the product repo: `cp apps/api/.env.example apps/api/.env` (+ the web one),
   then `pnpm dev`.

| Repo | api / web dev ports | env example(s) | OAuth client_id | dev callback |
| --- | --- | --- | --- | --- |
| kun-galgame-forum (kungal) | 2334 / 2333 | `apps/api`, `apps/web` | `4ed9bc99ec0a789a4796b83e22bd84c5` | `http://127.0.0.1:2333/auth/callback` |
| kun-galgame-patch (moyu) | 5214 / 6969 | `apps/api`, `apps/web` | `df3ff6008d740bfacbe46aa8cf483cf2` | `http://127.0.0.1:6969/auth/callback` |
| infra `apps/account` (account center) | — / 9420 | `apps/account` | session-based (n/a) | — |
| kun-letmoe-community | 7001 / 5364 | `apps/api`, `apps/web` | `letmoe-dev` (seed once, below) | `http://127.0.0.1:5364/auth/callback` |

Confidential clients (forum / moyu) present the plaintext `dev-secret-<client_id>`;
the wiki is a public client (PKCE, no secret). **letmoe does NOT take the snapshot**
— its own databases (`kun_letmoe`) are built by its own migrations + seeds, not a
prod snapshot. But its **OAuth client is an infra row** (`oauth_clients` lives in
`kun_galgame_infra`), which a letmoe seed cannot create — so seed it once here,
after `cmd/migrate` has built the table. `letmoe`'s `.env.example` carries the
matching public dev credentials (`letmoe-dev` / `dev-secret-letmoe-dev`):

```sh
# infra: kun_galgame_infra, AFTER `cd apps/api && go run ./cmd/migrate`
HASH=$(printf %s 'dev-secret-letmoe-dev' | sha256sum | cut -d' ' -f1)
psql -h 127.0.0.1 -U postgres -d kun_galgame_infra <<SQL
INSERT INTO oauth_clients
  (id, name, secret, redirect_uris, grants, is_public, auto_consent,
   refresh_token_ttl_seconds, allowed_scopes,
   image_enabled, image_site_key, image_allowed_presets, catalog_site,
   dev_enabled, dev_tier, dev_rate_per_min, dev_quota_daily,
   dev_review_status)
VALUES
  ('letmoe-dev', '一起萌 letmoe (dev)', 'sha256:$HASH',
   '["http://127.0.0.1:5364/auth/callback"]', '["authorization_code","refresh_token"]',
   false, false, 7776000, '["openid","profile","email"]',
   true, 'letmoe', '["topic"]', 'letmoe',
   false, '', 0, 0,
   'approved')
ON CONFLICT (id) DO UPDATE SET secret=EXCLUDED.secret,
  redirect_uris=EXCLUDED.redirect_uris, image_enabled=EXCLUDED.image_enabled,
  image_site_key=EXCLUDED.image_site_key, image_allowed_presets=EXCLUDED.image_allowed_presets,
  catalog_site=EXCLUDED.catalog_site, allowed_scopes=EXCLUDED.allowed_scopes;
SQL
```

`catalog_site='letmoe'` scopes both its community tenant and its catalog reads to
the local `letmoe` site. Each product repo's README repeats the three steps above
and links back to this file.

## Tear down

```sh
docker compose -f docker-compose.dev.yml down       # keep data volumes
docker compose -f docker-compose.dev.yml down -v    # also drop redis/minio/opensearch volumes
```
