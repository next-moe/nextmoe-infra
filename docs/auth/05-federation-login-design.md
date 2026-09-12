# 05 — Federated login (Google + GitHub) — Design

> Upstream federation for the self-built OIDC OP (`cmd/oauth`): the OP becomes an
> OAuth/OIDC client of Google (full OIDC) and GitHub (plain OAuth2, no `id_token`).
> Downstream sites are unchanged — a successful federation mints a normal OP
> session (same refresh cookie as password login). A small provider registry plus
> one adapter per provider is the extension point for later providers.

> **Implementation status (2026-09-09)**: v1 implemented on the OP backend —
> Google + GitHub adapters, auto-login/auto-link by verified email, completion
> form that always sets a password, admin/ren refusal, env-only credentials,
> no upstream token storage, no tokens in URLs. Frontend (`apps/web`) is a
> separate change. ⚠️ Prod needs `go run ./cmd/migrate` on `kun_galgame_infra`
> so AutoMigrate can create the two composite unique indexes on `oauth_accounts`
> (deploy does not run that migrate automatically).

## 0. Locked decisions

| # | Decision | Consequence |
|---|----------|-------------|
| 1 | **Auto-login + auto-link by verified email** | If the provider identity is not yet linked but its email is provider-verified and matches an existing user, log that user in and create the link. Unverified or absent email must never auto-link. |
| 2 | **Every new federated user must set a password** | Completion form runs before the account is created. There are no passwordless accounts. |
| 3 | **admin/ren accounts cannot log in or be linked via federation (v1)** | They get an error telling them to use password login. Same idea as the account-switching step-up rule (`SwitchActiveSession`). |
| 4 | **Client credentials live in env only** | House style. The settings center holds one `string_list` on/off key (`auth.federation_providers`). |
| 5 | **Never store upstream access/refresh tokens** | `oauth_accounts.access_token` / `refresh_token` stay NULL. |
| 6 | **Never put tokens in URLs** | The callback sets the normal refresh cookie and redirects; the SPA restores the session via the existing refresh flow. |

## 1. Flows

### 1.1 Start

`GET /api/v1/auth/federation/:provider/start?redirect=`

1. Provider not in `registry.Enabled()` (settings list ∩ env-configured adapters) → 302 `/auth/login?error=federation_disabled`.
2. Generate `state` + `nonce` (`generateSecureToken(32)`), store Redis `federation_state:{state}` = `{provider, nonce, redirect}` for 10 minutes. The stored `redirect` is the raw query value; it is validated only when used.
3. Set httpOnly cookie `nm_fed_state` (Path `/api/v1/auth/federation`, MaxAge 600, SameSite Lax, Secure in production).
4. 302 to the provider AuthorizeURL. Our `redirect_uri` is always `{SiteURL}/api/v1/auth/federation/{provider}/callback`.

### 1.2 Callback

`GET /api/v1/auth/federation/:provider/callback`

Browser navigation. Query `error` (user cancelled at the provider) → 302 `/auth/login?error=federation_denied`. Otherwise:

| | Condition | Result |
|---|-----------|--------|
| A | `state` empty / ≠ `nm_fed_state` cookie, Redis state missing/expired, or stored provider ≠ path | `federation_state`. Redis state row is deleted once read. |
| B | Provider `Exchange` fails | `federation_failed` |
| C | Link hit (`FindByProviderSubject`) | banned → `federation_banned`; roles contain `admin` or `ren` → `federation_stepup`; else mint session, outcome **login** |
| D | No link, email non-empty **and** provider-verified, `FindByEmail` hits | banned / admin / ren as above; same provider already linked to that user under a different subject → `federation_conflict`; else create `oauth_accounts` (tokens NULL), mint session, outcome **login** |
| E | Anything else (no email, unverified email, or verified email with no matching user) | write `federation_pending:{token}` (30 min), outcome **pending**. A verified email that matches nobody still goes here — new users must set a password. |

Login outcome: set the same `refresh_token` cookie as password login, clear `nm_fed_state`, 302 to the **validated** redirect.

Pending outcome: clear `nm_fed_state`, 302 to `{FrontendURL}/auth/federation/complete?token={pending}&redirect={url-escaped raw redirect}`.

Error outcomes: 302 `{FrontendURL}/auth/login?error={code}` plus `&redirect=` only when the raw redirect is non-empty. Codes: `federation_state`, `federation_failed`, `federation_denied`, `federation_banned`, `federation_stepup`, `federation_conflict`, `federation_disabled`.

Session minting copies `Login`: `FindByIDWithRoles`, `generateTokens`, `sessions` row, 7-day TTL.

### 1.3 Complete

`GET /api/v1/auth/federation/pending?token=` returns `{provider, suggested_name, email, email_locked}`.

`EmailLocked` = provider-verified email is non-empty **and** passes `checkEmailDomainAllowed`. A verified email on a domain outside the registration allowlist is not usable — the user must supply an allowlisted email + register code (same mail as password registration).

`POST /api/v1/auth/federation/complete` `{token, name, password, email?, code?}`:

1. Pending Redis miss → `10018` `ErrAuthFederationExpired`.
2. Provider no longer in `Enabled()` → `10017` `ErrAuthFederationDisabled`.
3. If EmailLocked, use the pending email and ignore `email`/`code`. Else `email`+`code` are required and checked exactly like `Register` (`checkEmailDomainAllowed`, Redis `register_code:{email}`, `10011`/`10010`).
4. `ExistsByEmail` → `10006`; `ExistsByName` → `10007`.
5. `FindByProviderSubject` again — if a link appeared → `10019` `ErrAuthFederationConflict`.
6. Create the user like `Register` (hashed password, `NormalizeEmail`). Do not set Avatar from the provider URL.
7. Create the `oauth_accounts` link (tokens NULL). Unique violation → `10019`.
8. Mint session like `Register`. Welcome moemoepoint like `Register` (`oauth:register_gift:%d`), guarded on `moemoepointSvc != nil`.
9. Delete `federation_pending:{token}` and, on the manual-email path, `register_code:{email}`.

Success body is `dto.LoginResponse` (refresh cookie set, no refresh token in JSON).

### 1.4 Providers

`GET /api/v1/auth/federation/providers` (public JSON) returns `{providers: [{name}, ...]}` in settings-key order, filtered to adapters that have env credentials. Default empty list = feature off.

## 2. Data model

**`oauth_accounts`** (existing table; AutoMigrate owns the indexes):

- Unique `(provider, provider_account_id)` — one upstream identity maps to one row.
- Unique `(user_id, provider)` — a user may link a given provider once.
- `access_token` / `refresh_token` columns remain, always written NULL.

**Redis**

| Key | Value | TTL | Written | Consumed |
|-----|-------|-----|---------|----------|
| `federation_state:{state}` | `{provider, nonce, redirect}` | 10 min | Start | Callback (read + delete) |
| `federation_pending:{token}` | `{provider, subject, email, email_verified, name, avatar_url}` | 30 min | Callback outcome E | Pending (read); Complete (delete on success) |

**Cookies (OP host)**

| Cookie | Purpose | Attrs |
|--------|---------|-------|
| `nm_fed_state` | CSRF binding for the provider round-trip | httpOnly, Secure in production, SameSite Lax, Path `/api/v1/auth/federation`, MaxAge 600 |
| `refresh_token` | Same as password login | httpOnly, Secure in production, SameSite Lax, Path `/api/v1/auth`, 7 days |
| `nm_browser` | Existing browser/bag id; set on callback/complete the same way as login | httpOnly, Secure in production, SameSite Lax, Path `/` |

## 3. Security checklist

- [x] **Verified-email gate** — auto-link only when the provider asserts verification; unverified matching email goes to pending and creates no row (account-takeover guard).
- [x] **State cookie binding** — callback requires `state` == `nm_fed_state`; Redis state is one-time.
- [x] **admin/ren refusal** — link-hit and verified-email auto-link both refuse `admin`/`ren` with `federation_stepup`.
- [x] **No tokens in URLs** — access token stays off the query string; refresh is a cookie; pending token is an opaque Redis handle.
- [x] **No upstream token storage** — Google `id_token` / GitHub `access_token` are used in memory during Exchange only.
- [x] **Open-redirect validation** — empty → `{FrontendURL}/profile`; path `/…` (not `//`) → FrontendURL + path; absolute URL only if origin equals FrontendURL or SiteURL; anything else uses the default. Pending/error query `redirect` is the raw value for the SPA to re-validate; the login 302 target is always a validated URL.

## 4. Config reference

**Env (credentials, never the settings center)**

| Variable | Meaning |
|----------|---------|
| `KUN_FEDERATION_GOOGLE_CLIENT_ID` / `KUN_FEDERATION_GOOGLE_CLIENT_SECRET` | Google OIDC client |
| `KUN_FEDERATION_GITHUB_CLIENT_ID` / `KUN_FEDERATION_GITHUB_CLIENT_SECRET` | GitHub OAuth2 client |

Empty credentials → that adapter is not registered. `SiteURL` / `FrontendURL` are the existing server env vars.

**Settings key**

| Key | Kind | Default | Meaning |
|-----|------|---------|---------|
| `auth.federation_providers` | `string_list` | `[]` | Display order of providers on the login page. A name also needs env credentials to appear in `GET /providers`. |

Google / GitHub console redirect URI:

`{SiteURL}/api/v1/auth/federation/{provider}/callback`

with `{provider}` = `google` or `github`.

## 5. Migration & rollout

- **DB**: `go run ./cmd/migrate` against `kun_galgame_infra`. `OAuthAccount` is already in the migrate model list; AutoMigrate creates `idx_oauth_accounts_provider_account` and `idx_oauth_accounts_user_provider` from the GORM tags. Deploy does not run this migrate. If production already has duplicate `(provider, provider_account_id)` rows, the unique index create will fail until those rows are cleaned.
- **Rollout**: ship the OP with empty `auth.federation_providers` (feature off). Set Google/GitHub env on the oauth service, register the redirect URIs, then set the settings key to `["google","github"]` (or a subset). Downstream RPs need no change.
- **Not in v1**: profile bind/unbind, Apple/Microsoft, storing or encrypting upstream tokens, passwordless accounts.
