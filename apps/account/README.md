# NextMoe·未萌 账号 (`apps/account`)

The public **account center** for the NextMoe platform — a Nuxt 4 + Vue 3 app
built on the KunUI component library (`@kungal/ui-*`). Served at
`https://account.nextmoe.com` (same-origin with the oauth service). Covers:

- **Sign-in / sign-up** — password login, register, forgot/reset password, logout
- **Federation** — Google / GitHub complete-registration page
- **OAuth consent** — `/oauth/authorize`
- **Profile** — signed-in user's own profile, avatar, email, password, moemoepoints

This app talks **only** to the oauth service. It keeps the first-party cookie
session model (`access_token` + httpOnly refresh cookie).

## Scripts

- `pnpm dev` — dev server on **http://127.0.0.1:9420**
- `pnpm build` — production build (`nuxt build`)
- `pnpm typecheck` — type-check with `vue-tsc`
- `pnpm lint` / `pnpm lint:fix` — ESLint
- `pnpm icons` — regenerate `app/assets/kun-icons.ts`
- `pnpm sitemap` — regenerate `public/sitemap.xml`

See the repo-root `CLAUDE.md` for platform-wide conventions.
