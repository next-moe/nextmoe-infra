# 07 — SSO session lifetime and the editor platform client

Status: shipped 2026-10-19 (wave E0 of the editor platform).

## 1. Why

The editor platform (`editor.nextmoe.com`) is login-only and must never show a
login button to someone already signed in on another NextMoe site: it redirects
straight to `/oauth/authorize`, and the account center answers silently when it
still holds a sign-in for that browser.

Before this change the account center's own sign-in (the `sessions` row with
`client_id = ''`, carried by the `refresh_token` cookie on
`account.nextmoe.com`) lived 7 days and was only renewed when the user visited
the account center. Downstream client sessions slide for 90 days. A user active
on the forum every day but not on the account center for a week therefore had a
live forum session and a dead account-center session, and the editor would have
bounced them to a login page.

## 2. The session model

An account-center sign-in now has two limits, both in the config center:

| Key | Default | Range | Meaning |
|---|---|---|---|
| `auth.sso_session_idle_days` | 30 | 1–365 | expires after this long with no activity |
| `auth.sso_session_max_days` | 365 | 1–730 | hard limit from `auth_time`; nothing extends past it |

`expires_at = min(now + idle, auth_time + max)` on login, registration,
federation login and every account-center refresh.

**Activity anywhere counts.** At consent the account center records the
browser (`nm_browser`) on the authorization code (`authorization_codes.browser_id`),
and the code exchange copies it onto the client session
(`sessions.sso_browser_id`). Every successful refresh of that client session
extends the same user's live account-center sessions in that browser to
`min(now + idle, auth_time + max)`, never backwards. Rules:

- another browser's sign-in is never extended;
- an expired sign-in is never brought back;
- logout deletes the row, so a later client refresh has nothing to extend.

`sso_browser_id` is deliberately separate from `browser_id`: `browser_id` marks
a row as one of that browser's signed-in accounts, which the account switcher
and "log out of this browser" enumerate. Client sessions must not appear there.

The `refresh_token` cookie's `Max-Age` is the time left to `auth_time + max`,
so the cookie outlives every server-side extension; the server-side
`expires_at` is the real limit.

Rows that existed before the deploy have `sso_browser_id = ''`: those client
sessions do not extend anything until the user signs in to that site again.

## 3. `union_site_roles`

`oauth_clients.union_site_roles` (default false; ren-only to enable in the
admin console, like `auto_consent`). When true, tokens minted for the client by
code exchange and by refresh carry the user's active site roles from **every**
site (deduplicated, sorted) instead of the client's own site. It exists for a
first-party surface that acts on every site's content: on the editor platform a
forum or moyu site moderator must be able to review catalog edits filed from
anywhere.

`/oauth/userinfo` still answers the token's own site.

## 4. The `nextmoe-editor` client

Seeded by `cmd/migrate` when `KUN_EDITOR_PUBLIC_URL` is set (create-only; a
console edit is never undone by a later deploy):

| Field | Value | Why |
|---|---|---|
| `id` | `nextmoe-editor` | |
| site | `editor.nextmoe.com` (seeded site row) | token `aud` and site binding |
| `is_public` | true | PKCE; the editor's BFF holds tokens in httpOnly cookies and has no secret to keep, so nothing needs to be pasted into the deploy panel |
| `auto_consent` | true | first-party; no consent screen, so silent sign-in works |
| `catalog_site` | `editor` | the central editing site |
| `union_site_roles` | true | §3 |
| `allowed_scopes` | `openid profile catalog:read catalog:edit` | `catalog:edit` gates `/v2/me/proposals`, `/v2/me/edit-*`, `/v2/moderation/*`; `catalog:read` for the user-token reads of `/v2/catalog/*` |
| `redirect_uris` | `<KUN_EDITOR_PUBLIC_URL>/auth/callback` | post-logout redirects to the same host are accepted automatically |
| `refresh_token_ttl_seconds` | 7776000 (90 days) | |

## 5. The `editor` role

A sixth global role, assignable by admin and ren (and editable in the
permission overlay by admins). It is granted by approving an editor
application (`POST /api/v1/editor/applications`, admin review under
`/api/v1/admin/editor/applications`, approve/decline need
`oauth.roles.grant_basic`). Its catalog permissions live in `catalog/perm`
(wave E1). Re-apply cooldown: `auth.editor_application_cooldown_hours`
(default 24). The creator flow's cooldown moved from a hardcoded 24 h to
`auth.creator_application_cooldown_hours` (default 24) at the same time.
