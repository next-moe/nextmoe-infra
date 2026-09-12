# 02 — Account Switching (multi-account) — Design

> The definitive design for Gmail/Microsoft-style **account switching** across the
> KUN Galgame OAuth ecosystem: a central OAuth2 IdP (`cmd/oauth`) + first-party
> SPAs on **different top-level domains** (kungal.com, moyu.moe, …). Grounded in
> OIDC Core, OIDC Back-Channel Logout, RFC 9700 (BCP 240, 2025) and RFC 6819.
> See also [01-creator-role-design](./01-creator-role-design.md).

> **Implementation status (2026-06-24)**: Phase 1 shipped — session-bag data layer
> (`sessions.browser_id/auth_time/last_used_at`), `/auth/sessions` (+ switch/logout/
> logout-all, Bearer + confused-deputy guard), `/oauth/authorize` `prompt=select_account|none`
> + `login_hint`, admin step-up (`10016`); in-place switcher in apps/web + the redirect/
> local-cache switcher in wiki. Downstream contract: `docs/integration/oauth/09`. **Not yet**:
> forum/moyu switchers, `prompt=none` focus-reconcile, short access-TTL + audit log.
> ⚠️ Prod needs `go run ./cmd/migrate` on `kun_galgame_infra` (deploy doesn't auto-run it).

## 0. Locked decisions

| # | Decision | Consequence |
|---|----------|-------------|
| 1 | **Global active account per browser** (one active account across all tabs/apps) | The IdP is the single source of truth for "who is active"; cross-TLD apps **reconcile to it** (see §5). True instant-global is impossible across different TLDs — apps converge on load/focus. |
| 2 | **Revocation-based logout** (short-lived access tokens + server-revocable sessions) | No back-channel endpoints, no third-party-cookie iframes. Logout is propagated by **revoking the DB session**; other apps notice on their next refresh (bounded by the access-token TTL). Simplest, fewest pitfalls, cross-TLD-robust. |
| 3 | **Switching INTO an admin account requires re-authentication** | The switch flow forces a fresh credential check (step-up) when the target account holds the `admin`/`ren` role. |

## 1. Mental model

Account switching is **not** "log out + log in". It is:

- The **IdP holds a bag of concurrent sessions** — one per signed-in account, all keyed to one browser.
- Each downstream app holds tokens for **exactly one** account at a time (the active one).
- "**Switch**" = the app re-runs the **authorization-code redirect through the IdP**; because the IdP's browser cookie already knows every signed-in account, it issues fresh tokens for the chosen account **without re-entering credentials** (except the admin step-up, decision #3).

This is the `accounts.google.com` model: the IdP owns the sessions; each product redirects there to pick/confirm an account. Because our apps are on **different TLDs**, switching **must** go through redirects — cookies cannot be shared across kungal.com ↔ moyu.moe (Auth0's same-TLD guidance; cross-TLD third-party cookies are an anti-pattern).

## 2. Spec primitives (build on these, don't invent)

- **`prompt=select_account`** on `/authorize` → render the account chooser (bag has ≥1 account). (OIDC Core §3.1.2.1)
- **`prompt=none`** → silent "who is active / is my session still valid?" check; render **no** UI; on failure return an error. (OIDC Core §3.1.2.1)
- **Error codes** `account_selection_required` / `login_required` / `consent_required` / `interaction_required` — the negotiation when `prompt=none` can't finish silently → fall back to the chooser. (OIDC Core §3.1.2.6)
- **`prompt=login` / `max_age=0`** → force re-authentication (used for the admin step-up). (OIDC Core)
- **`sid` vs `sub`** logout granularity — `sid` logs out one session/account, `sub` (no `sid`) logs out all of a user's sessions. (OIDC Back-Channel Logout 1.0)

> We run a **custom Go OAuth server**, so we implement these ourselves — an advantage (off-the-shelf IdPs like FusionAuth *ignore* `prompt=select_account`).

## 3. Data model — the session bag

All multi-session state lives **server-side** in the IdP DB, keyed by an opaque browser id. The SPA never holds more than the active account's short-lived access token.

**Cookies (IdP domain only, httpOnly, Secure):**

| Cookie | Purpose | Attrs |
|--------|---------|-------|
| `nm_browser` | Opaque **browser/bag id** — groups all sessions signed in on this browser. The single long-lived anchor. | httpOnly, Secure, SameSite=Lax, IdP host, long-lived, rotated on privilege change |
| `nm_active` | The **active** account pointer (session id) for this browser. | httpOnly, Secure, SameSite=Lax, IdP host |

**`sessions` table (extend the existing client-bound session row):**

- `id` (session id = OIDC `sid`)
- `browser_id` → `nm_browser` (the bag key; **this is the new column** that turns N independent sessions into one switchable bag)
- `user_id`, `client_id` (already client-bound — keep)
- `refresh_token_hash` (stored server-side — **do not** ship N refresh cookies; the bag holds them)
- `created_at`, `last_used_at`, `revoked_at` (revocation = set `revoked_at`; powers decision #2)
- `auth_time`, `acr` (for the admin step-up / `max_age`)

**Why server-side bag (not N cookies):** one opaque `nm_browser` cookie + DB rows avoids cookie bloat, keeps refresh tokens off the client, and makes revocation a single `UPDATE`. (Connect2id's per-session-cookie schema is *one* valid pattern, not a mandate — server-side is cleaner for us.)

## 4. Flows

### 4.1 Add an account
1. App → IdP `/authorize?prompt=login&state=…&code_challenge=…` (or the chooser's "use another account").
2. IdP authenticates (email/pw or social), creates a new `sessions` row with the **same `browser_id`** (append to the bag), sets it active (`nm_active`).
3. Auth-code → app → exchange for tokens.

### 4.2 Switch (cross-TLD — the crux)
1. App "switch account" → redirect to IdP `/authorize?prompt=select_account&state=…&code_challenge=…`.
2. IdP reads `nm_browser` → loads the bag → renders the **chooser** (name/avatar/email per session). *(If the app passes a target via `login_hint`, the IdP may skip the UI when unambiguous.)*
3. User picks account B → IdP sets `nm_active`=B → **if B is admin, force re-auth first (§6)** → issues auth-code for B.
4. App exchanges the code → swaps to B's tokens. No credential entry (unless step-up).

### 4.3 Reconcile to global (decision #1, cross-TLD)
Because the active pointer lives on the IdP domain and apps are cross-TLD, an app learns the active account by **asking the IdP**:
- On **load / tab focus / route change**, the app does a silent `/authorize?prompt=none` (hidden redirect or background check).
- If the IdP's active account ≠ the app's current account → the app silently re-authorizes for the active account and swaps tokens.
- **Honest limitation:** a background tab does **not** switch instantly; it reconciles when focused/reloaded. This is the best achievable "global" across different TLDs. (Apps that happen to share a TLD — e.g. *.kungal.com — can additionally share the cookie for instant global within that family.)

### 4.4 Logout (decision #2)
- **Log out this account**: IdP sets `revoked_at` on that session, removes it from the bag, picks a new active (or none). The app whose tokens are for that account fails its next refresh → logs out. Other apps unaffected.
- **Log out all**: revoke every session for the `browser_id`, clear `nm_browser`/`nm_active`.
- **Propagation = revocation + short access TTL** (no iframes/back-channel): set the **access-token TTL to ~10–15 min**; every app refreshes via the IdP, and a revoked session makes refresh fail → the app logs out within one TTL. Tighten the TTL if you want faster cross-app logout; that's the only knob.

## 5. The "global active account" reality (decision #1)

- **Source of truth:** `nm_active` on the IdP. There is no instant cross-TLD push.
- **Convergence:** each app reconciles via `prompt=none` on load/focus (§4.3).
- Set expectations in code review: "global" here = *eventually consistent on focus*, not *instant in every background tab*. Make the **active account unmistakable in every app's UI** (avatar + name in the header) so a stale background tab can't cause confusion.

## 6. Admin step-up (decision #3)

When the chosen account holds `admin`/`ren`:
- The switch flow injects `prompt=login` (or `max_age=0`) → the IdP **forces a fresh credential check** before activating that session and issuing the code.
- Record `auth_time` on the session; gate admin endpoints on a recent `auth_time` (re-prompt if stale).
- Rationale: prevents "I left an admin account in the bag and a shoulder-surfer one-click-switched into it" and privilege-confusion (acting as admin via a stale active pointer).

## 7. Security checklist (MUSTs — RFC 9700 / RFC 6819)

- [ ] **CSRF on every switch/add/reconcile redirect**: one-time, user-agent-bound `state` **+ PKCE** (SPAs are public clients). A forged callback must not silently switch the victim into an attacker's account. (RFC 9700 §2.1)
- [ ] **Open-redirect**: allowlist all return/`redirect_uri` targets (already registered per client — extend to any post-switch `return` param). Never forward to an arbitrary query-param URL. (RFC 9700 §2.1/§4.11)
- [ ] **Audience-restrict access tokens** (`aud` per resource server) so moyu's JWT can't be replayed against kungal's API. (RFC 9700 §2.3)
- [ ] **Refresh tokens**: rotation (detect replay → revoke the family) **and** bind each to its `client_id` (we already client-bind sessions). (RFC 9700 §2.2.2; RFC 6819 §5.2.2.2)
- [ ] **Account-confusion guard**: the server derives the acting account from the **token**, never from a client-supplied account hint; every mutating action is attributed to the token's `sub`.
- [ ] **Short access-token TTL** (~10–15 min) — the lever for revocation-based logout (§4.4).
- [ ] **Audit log**: every add / switch / step-up / logout-one / logout-all with `browser_id`, `sub`, `client_id`, ip/ua.
- [ ] **Email is case-insensitive** for the "is this account already in the bag" check (see [[email-case-insensitive-login]] — reuse `NormalizeEmail`).

## 8. Anti-patterns — do NOT do these

- ❌ Sharing a session cookie across kungal.com ↔ moyu.moe (cross-TLD third-party cookie). Use the redirect relay.
- ❌ Putting N refresh tokens in N client-readable places. Keep refresh server-side in the bag.
- ❌ Trusting a client-sent "active account id" for authorization. Always from the token.
- ❌ Assuming back-channel logout is turnkey for SPAs — our SPAs hold no server-side RP session to correlate `sid`; that's exactly why we chose revocation-based logout.
- ❌ Instant-global expectations across TLDs — it's focus-reconcile (§5).
- ❌ One-click switch into an admin account without step-up (§6).

## 9. Migration & rollout

- **DB migration** (main DB `kun_galgame_infra`): add `browser_id`, `last_used_at`, `auth_time`/`acr` to `sessions`; index `(browser_id)`. Backfill existing sessions with a per-session `browser_id` (each becomes a singleton bag). → `go run ./cmd/migrate` (not auto-run on deploy).
- **Phases:**
  1. IdP: session bag + `prompt` handling + chooser page + `/sessions` API (list/add/switch/logout-one/logout-all) + step-up. No app changes yet (single-account still works).
  2. Account center (apps/web): switcher UI + the redirect/reconcile flow — dogfood on the IdP-family domain first.
  3. Roll the switcher into each SPA (forum, moyu, wiki) one at a time; each adds the `prompt=none` reconcile-on-focus.
  4. Tighten access-token TTL; enable audit logging.

## 10. Open implementation points (resolve at build time)

- Exact cross-TLD refresh transport (the current refresh cookie is `SameSite=Lax`, which is **not** sent on cross-site `fetch`) — confirm per app whether refresh happens via redirect or a `SameSite=None` cookie, and align the bag's refresh path with it.
- Whether the `prompt=none` reconcile uses a hidden redirect vs a background JSON check (depends on each SPA's session transport).
