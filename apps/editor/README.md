# 未萌编辑台 (apps/editor)

The NextMoe catalog editor at `editor.nextmoe.com`: one place to find catalog
entries, propose and review edits, and read revision history. Login-only; a
visitor who is signed in anywhere on NextMoe enters without ever seeing a login
button.

## Shape

- Nuxt 4 with `ssr: false` — a Vue SPA. Nitro is only a thin BFF.
- `server/middleware/session.ts` — every page navigation without a session is
  answered with a 302 to `account.nextmoe.com`'s authorize endpoint (PKCE S256,
  state, `login_hint` forwarded), before any HTML is served. The OP returns
  silently when its own session is alive and the client is auto-consent.
- `server/routes/auth/callback.get.ts` — code exchange; tokens land in httpOnly
  cookies (`ne_at`, `ne_rt`) and never reach the browser's JavaScript.
- `server/routes/api/v2/[...path].ts` — allowlisted relay to the catalog `/v2`
  faces with the bearer added server-side; writes need the `x-editor-csrf`
  header and a same-origin `Origin`.
- `server/routes/api/session.get.ts` — who the SPA is talking to (name, avatar,
  roles, whether any work field is reviewable).
- `server/routes/auth/logout.post.ts` — revokes the refresh token, clears the
  cookies, hands back the OP end-session URL.

The OAuth client is `nextmoe-editor`: public, auto-consent, `catalog_site =
editor`, scope `openid profile catalog:read catalog:edit`.

## Develop

```sh
pnpm dev            # from the repo root: base stack + Go services + all frontends
pnpm -F ./apps/editor dev   # just this app on http://127.0.0.1:9422
pnpm -F ./apps/editor test  # vitest: PKCE, return paths, proxy allowlist, CSRF
pnpm -F ./apps/editor icons # after adding a lucide:* icon
pnpm -F ./apps/editor gen:types:catalog-v2  # after docs/catalog/v2-openapi.yaml changes
```

Defaults in `nuxt.config.ts` target the local dev stack; see `.env.example`.
Production values come from `docker-compose.prod.yml` (`NUXT_*` runtime env).
