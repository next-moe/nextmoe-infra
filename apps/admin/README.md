# NextMoe Admin Console (`apps/web`)

The internal **admin console** for the NextMoe platform — a Nuxt 4 + Vue 3 app
built on the KunUI component library (`@kungal/ui-*`). It is the single operator
UI in front of the platform's Go services, covering:

- **Identity & users** — accounts, roles, moemoepoints, sessions, avatars
- **Sites & OAuth clients** — site registry, OAuth client management, app directory
- **Trust & Safety** — review queue, registries, Tier0 word list, dead letters
- **Catalog** — cross-media identity registry review (candidates / proposals / refs)
- **AI gateway** — model routing, tenant metering, moderation
- **Artifacts & images** — large-file and image-hosting admin
- **Dev API & jobs** — developer-platform apps/keys and background job status

## Scripts

- `pnpm dev` — dev server on **http://localhost:9420**
- `pnpm build` — production build (`nuxt build`)
- `pnpm typecheck` — type-check with `vue-tsc`
- `pnpm lint` / `pnpm lint:fix` — ESLint
- `pnpm gen:types:*` — regenerate `shared/types/generated/*` from the cross-service
  OpenAPI specs in `../../docs/{artifact,catalog,trust,ai}` (never hand-edit the output)

See the repo-root `CLAUDE.md` for platform-wide conventions.
