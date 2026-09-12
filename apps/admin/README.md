# NextMoe·未萌 管理台 (`apps/admin`)

The internal **admin console** for the NextMoe platform — a Nuxt 4 + Vue 3 app
built on the KunUI component library (`@kungal/ui-*`). Served at
`https://admin.nextmoe.dev`. It is a standard OAuth RP (confidential client
`nextmoe-admin`) against the account-center OP, and the single operator UI in
front of the platform's Go services, covering:

- **Identity & users** — accounts, roles, moemoepoints, sessions, avatars
- **Sites & OAuth clients** — site registry, OAuth client management, app directory
- **Trust & Safety** — review queue, registries, Tier0 word list, dead letters
- **Catalog** — cross-media identity registry review (candidates / proposals / refs)
- **AI gateway** — model routing, tenant metering, moderation
- **Artifacts & images** — large-file and image-hosting admin
- **Dev API & jobs** — developer-platform apps/keys and background job status

The whole app is `noindex`.

## Scripts

- `pnpm dev` — dev server on **http://127.0.0.1:9421**
- `pnpm build` — production build (`nuxt build`)
- `pnpm typecheck` — type-check with `vue-tsc`
- `pnpm lint` / `pnpm lint:fix` — ESLint
- `pnpm gen:types:*` — regenerate `shared/types/generated/*` from the cross-service
  OpenAPI specs in `../../docs/{artifact,catalog,trust,ai}` (never hand-edit the output)

See the repo-root `CLAUDE.md` for platform-wide conventions.
