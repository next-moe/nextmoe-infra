# 03 — OIDC Standardization ("rebirth" of the OAuth server) — Design

> The definitive design for turning the self-built KUN Galgame OAuth 2.0 server
> (`cmd/oauth`) into a **standards-compliant, minimal OpenID Connect Provider (OP)**
> — WITHOUT replacing it. We keep our hardened Authorization Code + PKCE server and
> hand-roll only the missing standard pieces: an `id_token`, asymmetric signing
> (ES256 active + RS256 supported), a JWKS endpoint, discovery documents, and a
> spec-compliant wire format. Grounded in OIDC Core 1.0, OIDC Discovery 1.0,
> RFC 8414, RFC 7517 (JWK), RFC 9068 (JWT access tokens), RFC 9700 (BCP 240, 2025),
> OIDC RP-Initiated Logout, and OIDC Back-Channel Logout.
> See also [01-creator-role-design](./01-creator-role-design.md) and
> [02-account-switching-design](./02-account-switching-design.md).

> **Implementation status (2026-07-01)**: **Phase 0 implemented** — key infrastructure:
> `signing_keys` table (three-state, `kid`-tagged), `pkg/oidckeys` (ES256/RS256 keygen,
> RFC 7638 thumbprint kid, JWK encoding, AES-256-GCM at-rest), startup key bootstrap,
> and the public `GET /oauth/jwks` + `/.well-known/{openid-configuration,oauth-authorization-server}`
> endpoints — all gated on `KUN_OIDC_KEY_ENC_KEY`, world-readable (ACAO *), standard JSON.
> Verified end-to-end (crypto unit tests + live discovery/JWKS smoke test).
> **Phase 1 CODE COMPLETE** — token core (`pkg/oidctoken`: ES256/HS256 signer + accept-both
> verifier + caching JWKS resolver; JWK→pubkey decode in `pkg/oidckeys`); **all four
> verifiers accept-both** (oauth via the local key set; galgame/image/artifact/moderation
> via the OP's JWKS at `KUN_OIDC_JWKS_URL`); oauth signs ES256 when `KUN_OIDC_SIGN_ASYMMETRIC`
> is on (default off → HS256); refresh tokens are opaque random strings. The flip is a
> **staged ops rollout** (§10 Phase 1): (1) deploy these builds to ALL services with
> `KUN_OIDC_JWKS_URL` set on the resource servers + `KUN_OIDC_KEY_ENC_KEY` on oauth; (2)
> set `KUN_OIDC_SIGN_ASYMMETRIC=true` on oauth; (3) later drop HS256 acceptance.
> **Phase 2 done** — minimal `id_token` (**RS256** — the OIDC registration default
> `id_token_signed_response_alg`, so standard RP libraries verify it with zero per-client
> config; iss/sub/aud/exp/iat + nonce) issued on the `openid` scope for the code grant;
> `nonce` captured at `/authorize`→consent page→code→id_token (new
> `authorization_codes.nonce` column → needs `cmd/migrate` **BEFORE deploy** — the column
> is written on every consent, flag-independent); `end_session_endpoint` now GET+POST with
> the RP-Initiated Logout params (`post_logout_redirect_uri`, `state`, `id_token_hint`).
> **Phase 3 done, and the flag is gone** — `/oauth/token`·`/oauth/userinfo`·`/oauth/revoke`
> emit spec-compliant top-level JSON + RFC 6749 / RFC 6750 error objects **unconditionally**.
> `KUN_OIDC_STANDARD_WIRE` was deleted along with the envelope branch: the line is drawn at the
> **protocol boundary**, not behind a switch. The house `{code,message,data}` envelope stays
> correct for every non-protocol endpoint (`/auth/me`, `/users/*`, …) — ~780 call sites that were
> never in scope. Also landed with it: RFC 7235 case-insensitive `Bearer`, RFC 6750
> `WWW-Authenticate` on every 401, RFC 6749 §4.1.3 client-authentication-before-code ordering,
> `response_modes_supported`, and id_token-on-refresh. **Phase 4 not started** (deferred: DCR,
> DPoP, RFC 7662, back-channel logout).
> This is the keystone of the developer platform (todos #6), deliberately sequenced
> **before** any Huma-ification of the auth surface, because an IdP's canonical
> machine-readable contract is the **OIDC discovery document, not a hand-authored
> OpenAPI spec**. Governing premise: **every current consumer is first-party**
> (kungal / moyu / wiki, one team) → the ecosystem can afford a **coordinated clean
> cutover**; we optimize for the cleanest correct end state, not for preserving the
> legacy wire format forever.
> ⚠️ Phase 0 added a new `signing_keys` table on `kun_galgame_infra` → run
> `go run ./cmd/migrate` (deploy does NOT auto-run it — see the project migration rule).
> Also set a strong `KUN_OIDC_KEY_ENC_KEY` (the KEK that encrypts signing keys) on the
> oauth service, or key bootstrap + jwks/discovery stay disabled.

## 0. Locked decisions

| # | Decision | Consequence |
|---|----------|-------------|
| 1 | **Keep self-built; hand-roll only the missing standard bits.** NOT adopting an OIDC library or migrating to an off-the-shelf IdP. | The hard, easy-to-get-wrong security core is **already correct and already RFC 9700-compliant** (PKCE-everywhere, refresh rotation, reuse detection, CAS anti-race, `alg` allow-listing). The remaining surface (id_token, JWKS, discovery, key lifecycle) is **small and built on stable specs**. Self-built auth stays a first-party asset; no dependency introduced into the identity core. |
| 2 | **Asymmetric signing: ES256 active + RS256 supported.** | OIDC Core §15.1 and RFC 9068 make RS256 **mandatory-to-implement**; `alg=none` is forbidden. ES256 is the *active* signer (smaller keys/signatures, cheaper than RSA); RS256 keys are published too so any conformant verifier works. Verification switches from a shared symmetric secret to public keys. |
| 3 | **Three-state, `kid`-tagged signing keys published via JWKS.** | Keys move `pending` (pre-published) → single `active` signer → `retired` (still published for verification) → deleted after retention. Rotation is routine, not an incident. Private keys are encrypted at rest. |
| 4 | **Minimal `id_token`; roles stay OUT of it.** | `id_token` carries only `iss, sub, aud, exp, iat` (+ `nonce`/`auth_time` when applicable) — it authenticates the *user to the client*, nothing more. Authorization data (`roles`, `site_id`, `scope`) lives in the **access token + `/userinfo`**, never in the `id_token`. Our current server already puts roles there — this decision keeps it that way. |
| 5 | **Access tokens become RFC 9068 JWTs; `/userinfo` stays; revocation model unchanged.** | Access token = `typ=at+jwt`, audience-restricted. This ENABLES stateless local verification via JWKS, but we **do not drop `/userinfo` introspection**. Ban immediacy — a moat — is preserved: bans are enforced at `/authorize` + refresh (already), and access-token TTL stays short (15 min), so a banned user's local-verifiable token dies within one TTL. RFC 7662 introspection is deferred until a resource server needs sub-TTL revocation. |
| 6 | **Standardize the wire format on the OAuth/OIDC protocol endpoints only; coordinated first-party cutover.** | Drop the private `{code,message,data}` envelope on `/oauth/token`, `/oauth/userinfo`, `/oauth/revoke`, discovery, and JWKS → spec-compliant top-level JSON + standard OAuth error objects (`{"error","error_description"}`, RFC 6749 §5.2). The house envelope **stays** on `/auth/*` self-service and `/api` business endpoints. Migrate via expand→contract across repos (§7), then delete the old shape — no dual-shape-forever. |
| 7 | **Code + PKCE only. Everything else deferred.** | `response_type=code` + PKCE S256 is the whole flow surface. Implicit `SHOULD NOT`, ROPC `MUST NOT` (RFC 9700 / OAuth 2.1). Hybrid, dynamic client registration (DCR), request objects, pairwise `sub`, and sender-constraining (mTLS/DPoP) are **out of scope now** — added only when a real third party needs them. |

## 1. Mental model

Today `cmd/oauth` is a **complete, hardened OAuth 2.0 Authorization Server** wearing a
private wire format. OIDC is, by its own definition, *"a simple identity layer on top
of the OAuth 2.0 protocol"* — it **reuses** the same `/authorize` and `/token`
endpoints and adds only the `openid` scope + an `id_token`. So this is **not a rewrite**.
We are:

- **Adding** three genuinely-required OIDC pieces the server lacks: an `id_token`,
  asymmetric signing (with RS256 support), and a published JWKS.
- **Publishing** the machine-readable contract (discovery) so any OIDC library can
  integrate by configuration, not by reading our code.
- **Standardizing** the wire format so an off-the-shelf OIDC client can parse our
  responses (today it cannot — the envelope breaks it).
- **Changing nothing** about the security core (PKCE, refresh rotation, reuse
  detection, CAS, ban enforcement) — it already meets the current BCP.

The result is a **minimal but spec-correct OP**: everything a first-party SSO needs,
and exactly the subset a third party will later discover and consume.

## 2. Spec primitives (build on these, don't invent)

- **`id_token`** — a JWT authenticating the end-user; REQUIRED in the code-flow token
  response when `openid` scope is present. Required claims: `iss, sub, aud, exp, iat`
  (+ `nonce` if the auth request carried one). (OIDC Core §2, §3.1.3.6)
- **RS256 mandatory-to-implement** — an OP MUST support signing tokens with RS256;
  `alg=none` is forbidden. ES256 may be the active algorithm. (OIDC Core §15.1; RFC 9068 §2.1)
- **JWKS + `jwks_uri`** — the OP publishes its public keys as a JWK Set over HTTPS;
  `jwks_uri` is REQUIRED in OIDC discovery. Each key carries a `kid`. (RFC 7517; OIDC Discovery)
- **Discovery** — `/.well-known/openid-configuration` (identity specialization) and
  `/.well-known/oauth-authorization-server` (RFC 8414, the OAuth-native generalization).
  Serve both; they share a body. **This document is the IdP's canonical machine-readable
  contract.** (OIDC Discovery 1.0; RFC 8414)
- **RFC 9068 JWT access token** — `typ=at+jwt` header (keeps it distinct from `id_token`
  per RFC 8725), `iss/sub/aud/exp/iat/jti` claims, audience-restricted.
- **RP-Initiated Logout** — `end_session_endpoint` (support GET + POST); any
  `post_logout_redirect_uri` MUST be pre-registered and matched **exactly**; `id_token_hint`
  identifies the session. (OIDC RP-Initiated Logout 1.0)
- **Back-Channel Logout** — OP → RP server-to-server logout token; browser-independent,
  **the cross-TLD-reliable channel** (front-channel breaks under third-party-cookie
  blocking across different TLDs). Optional, for confidential RPs only. (OIDC Back-Channel Logout 1.0)
- **RFC 9700 (BCP 240)** — the security baseline we already satisfy (§7).

## 3. What we already have vs what we add (gap map)

| Concern | Today (verified in code) | Target |
|---|---|---|
| Flow | `/authorize` code + PKCE S256, `/token`, `/userinfo`, `/revoke` (RFC 7009), consent, custom logout (`cmd/oauth/main.go:196-219`) | Same flow, unchanged |
| Signing | **HS256, single `cfg.JWT.Secret`** (`pkg/utils/jwt.go` `GenerateAccessToken:32`) | **ES256 active + RS256** via `signing_keys` (§4) |
| Access token | HS256 JWT, claims `sub/id/email/name/roles/scope/site_id` (`TokenClaims`); `email` gated on the `email` scope since 2026-07-24 (`service.EmailForScope` — the same gate as `/oauth/userinfo` + `/auth/me`, closing the "decode the token to bypass the filter" hole) | RFC 9068 `at+jwt`, same claims + proper `aud`, ES256-signed |
| Refresh token | HS256 JWT **but looked up by DB value, signature never verified** (`FindByRefreshTokenOrPrev`; `ParseRefreshToken` has **no callers**) | **Opaque random string** (drop the vestigial JWT signing) |
| `id_token` | **none** | Minimal, ES256-signed, issued on `openid` scope |
| JWKS | **none** | `GET /oauth/jwks` (`jwks_uri`) |
| Discovery | **none** | `/.well-known/{openid-configuration,oauth-authorization-server}` |
| Wire format | private `{code,message,data}` envelope on protocol endpoints (handler-added; DTOs `dto.TokenResponse`/`dto.UserInfoResponse` are already the standard shape) | standard top-level JSON + standard OAuth errors |
| Logout | custom `/oauth/logout` top-level bounce (`docs/integration/oauth/07`) | formalized `end_session_endpoint` semantics; back-channel optional |
| Scopes | `openid`/`profile`/`email` already recognized + enforced (`CreateAuthorizationCode:179`, `GetUserInfo:406`) | unchanged |

**Key blast-radius fact (why this is low-risk).** The access-token signature is verified in
exactly **four first-party services, all in the infra repo**: the OP itself (`middleware.Auth`
→ `internal/platform/auth/service/auth_service.go:807`), the galgame service
(`internal/middleware/jwt_auth.go:33,68`), image (`internal/platform/image/middleware/auth.go:138`),
and artifact (`internal/platform/artifact/middleware/auth.go:127`). **Downstream (forum/moyu/wiki)
verify nothing** — they call `GET /oauth/userinfo` to introspect (forum
`internal/user/oauth/client.go` `FetchUserInfo`). So switching to asymmetric signing is **purely
additive externally** and a **coordinated change across these four internal verifiers**. Bonus:
today all four services hold `cfg.JWT.Secret` (each can *forge* tokens); under asymmetric only the
OP holds the private key and galgame/image/artifact need only the **public key** — a real reduction
of the signing-secret footprint. (Note: `middleware.Auth` additionally does a per-request live ban
check — see §6.)

## 4. Data model — signing keys (`signing_keys`)

New table on `kun_galgame_infra`. Private keys **encrypted at rest**; JWKS publishes only
public material.

| Column | Meaning |
|--------|---------|
| `kid` (PK) | Key ID; appears in every JWS header and the JWK entry |
| `alg` | `ES256` (default) or `RS256` |
| `use` | `sig` |
| `public_jwk` (jsonb) | Public JWK — served verbatim in the JWKS |
| `private_key_enc` (bytea) | Private key, AES-256-GCM encrypted under a KEK from env (`KUN_OIDC_KEY_ENC_KEY`) — never served, never logged |
| `state` | `pending` → `active` → `retired` |
| `created_at` / `activated_at` / `retired_at` | Lifecycle timestamps |

**Key lifecycle (three states + overlap):**

1. **pending** — generated, its public JWK **already published** in the JWKS, but not yet
   signing. Lets verifiers pre-fetch the key before it's ever used.
2. **active** — the single signer. Activating a new key demotes the previous active to
   `retired` (there is always exactly one active per `alg`).
3. **retired** — no longer signs, but its public JWK **stays in the JWKS** until every
   token it signed has expired (≥ max access-token TTL), then it is deleted.

**Signer** picks the `active` key for the chosen `alg` and sets the JWS `kid`. **Verifiers**
read `kid` from the header and select the matching public key from the local key set
(cached from the DB/JWKS). Rotation cadence and overlap windows are config knobs (sensible
defaults: rotate ~90d, publish-before-activate ~a few min–hours, keep-retired ≥ access TTL).

**Key storage decision:** DB table with app-level AES-GCM encryption (KEK in env). Rationale:
no external KMS dependency, survives our single-Postgres deployment model, and the KEK is one
env var to protect — consistent with how the rest of infra handles secrets. (KMS is a future
option if/when the threat model warrants it.)

## 5. Token & endpoint design

### 5.1 Access token (RFC 9068)
- Header: `typ: at+jwt` (stamped on BOTH the HS256 and ES256 paths — the flag flips only
  the signature), `alg: ES256` (post-flip; `kid` set), HS256 pre-flip.
- Claims (per RFC 9068 §2.2): `iss` (the OP issuer URL), `sub` (user UUID), `aud` = the
  client's **site domain** (the resource-server identifier, resolved from the client's Site;
  omitted for clients not bound to a site), `client_id` (the requesting client), `exp`
  (15 min), `iat`, `jti`, plus `scope`, `roles`, `site_id`. The resource servers keep
  checking `site_id` (no lockstep change); `aud` serves standard validators.
- Verified by the four infra sites (§3) via the `active`/`retired` public keys.

### 5.2 id_token (minimal)
- Header: `typ: JWT`, `alg: RS256`, `kid`. **RS256, not ES256**: it is the OIDC
  registration default for `id_token_signed_response_alg`, and this OP has no per-client
  alg registration — standard RP libraries (openid-client etc.) verify against their
  configured alg, which defaults to RS256, so RS256 is the only choice that makes
  "point a standard library at discovery, zero extra config" true. (Also OIDC Core §15.1
  mandatory-to-implement.) ES256 stays the access-token algorithm.
- Claims: `iss`, `sub`, `aud` = **client_id** (the RP, distinct from the access token's `aud`),
  `exp`, `iat`, `nonce` (only if the auth request carried one), `auth_time` (when `max_age`/
  the account-switch step-up applies — ties into [02](./02-account-switching-design.md) §6).
- **No `roles`, no `scope`, no `site_id`.** Issued in the `/token` response only when
  `openid` scope was granted.

### 5.3 Refresh token
- Becomes an **opaque, high-entropy random string** (it is already DB-looked-up and its JWT
  signature is never verified — see §3). Storage, rotation, reuse-detection, grace window, and
  CAS in `RefreshWithClient` / `AuthService.RefreshToken` are **unchanged**; only the token's
  *format* changes from a signed JWT to random bytes.

### 5.4 New / changed endpoints
| Endpoint | Change |
|---|---|
| `GET /.well-known/openid-configuration` | **new** — discovery (issuer, endpoints, `jwks_uri`, `id_token_signing_alg_values_supported: [RS256]` (what the OP actually signs id_tokens with — see §5.2), `response_types_supported: [code]`, `response_modes_supported: [query]`, `grant_types_supported: [authorization_code, refresh_token]`, `scopes_supported`, `subject_types_supported: [public]`, `code_challenge_methods_supported: [S256]`, `end_session_endpoint`) |
| `GET /.well-known/oauth-authorization-server` | **new** — RFC 8414 body (same content) |
| `GET /oauth/jwks` | **new** — JWK Set of all `pending`/`active`/`retired` public keys |
| `POST /oauth/token` | issue `id_token` on `openid` scope; **standard top-level JSON**; standard OAuth error objects |
| `GET /oauth/userinfo` | **standard top-level JSON** (drop envelope) — DTO is already the right shape |
| `GET /oauth/logout` → `end_session_endpoint` | formalize to RP-Initiated Logout (GET+POST, `id_token_hint`, exact-match `post_logout_redirect_uri` — the allow-list already exists in `ValidatePostLogoutRedirect`) |
| (optional, deferred) back-channel logout | server-to-server logout token to confidential RPs (forum/moyu) — **complements**, does not replace, doc 02's revocation-based logout for pure SPAs |

## 6. Where `roles` live & the revocation trade-off (decision #4 + #5)

- `id_token` = identity only. `access token` + `/userinfo` = authorization (`roles`).
  This is already how the code behaves; we lock it in and must resist stuffing `roles` into
  the `id_token` when we add it.
- **Stateless vs call-home** for the access token: RFC 9068 JWTs let a resource server verify
  locally via JWKS (scalable, no per-request call-home). We **keep `/userinfo`** so callers who
  want live state still have it. The revocation moat — centralized IdP ban taking effect
  immediately — is **preserved, and stronger than pure TTL-bounding**: the OP's `middleware.Auth`
  already does a **per-request live ban check** (`GetCurrentUser` + `IsBanned()`, `middleware/auth.go:58-72`),
  so `/userinfo` and the OP's own protected endpoints reflect a ban **instantly**; bans are also
  enforced at `/authorize` and every refresh (`user.IsBanned()` at `ExchangeCode:321` and
  `RefreshWithClient:593`). Only the separate resource servers that verify the JWT locally
  (galgame / image / artifact) and downstream RPs are TTL-bounded — and access-token TTL is 15 min,
  so any locally-verified token is stale within one TTL. **This is already true today with HS256, so
  nothing regresses.** If a future resource server needs sub-TTL revocation, add RFC 7662
  introspection then — not now.

## 7. Wire-format cutover (decision #6) — expand → contract across repos

The DTOs are already standard; only the handler's `{code,message,data}` wrapper and the
error objects change. Because all clients are first-party, do a **coordinated expand→contract**,
never a dual-shape-forever:

1. **Expand consumers (tolerant readers).** Update each first-party OAuth client
   (forum `internal/user/oauth/client.go`, moyu, wiki) to parse **either** the standard
   top-level JSON **or** the legacy envelope. Ship + deploy these first. Nothing breaks —
   the OP still emits the old shape.
2. **Flip the producer.** Change the OP's protocol-endpoint handlers to emit standard
   top-level JSON + standard OAuth errors. Tolerant consumers keep working through the flip.
3. **Contract.** Once all RPs run tolerant readers against the new shape, remove the
   legacy-envelope tolerance from consumers and any old-shape code from the OP.

Sequencing note: because downstream *introspects* and mints its own server session at login,
the only consumers of these responses are the RP OAuth-callback code paths — a small, known,
first-party set. No content-negotiation or versioned endpoints needed; a coordinated cutover
is the right call here precisely because the fleet is mono-owned.

## 8. Security checklist (MUSTs — RFC 9700 / RFC 9068 / OIDC Core)

- [x] **Already met:** PKCE-everywhere (public MUST, confidential done), refresh rotation +
  reuse detection, `alg` allow-listing, atomic code consumption, open-redirect allow-list,
  refresh bound to `client_id`, short access TTL. (RFC 9700 §2.1/§2.2)
- [ ] **Asymmetric verify only:** verifiers accept **only** the OP's published `alg`s
  (ES256/RS256) and reject HS* and `none` — extend the current HMAC-only guard to an
  asymmetric allow-list. (RFC 9068 §4)
- [ ] **JWKS over HTTPS**, public keys only, `kid` on every key + every JWS header. (RFC 7517)
- [x] **Audience-restrict** access tokens (`aud` = the client's site domain) so a moyu token
  can't be replayed against kungal's API. (RFC 9700 §2.3) — `aud`/`client_id`/`iss`/`typ:at+jwt`
  are stamped on every access token; the enforcing check in the resource servers remains
  `site_id` (equivalent restriction, no lockstep change).
- [ ] **`id_token` minimal** — no `roles`/authz data; validate `nonce` when present. (OIDC Core)
- [ ] **Private keys encrypted at rest**, KEK never logged, never in the JWKS.
- [ ] **Discovery leaks nothing** beyond endpoints/capabilities (no secrets, no internal hosts).
- [ ] **Issuer consistency** — the `iss` claim in every token == the discovery `issuer` == the base
  the `.well-known` documents are served from (a classic OIDC footgun; verifiers reject on mismatch).
- [ ] **Retired keys stay published** until their tokens expire (no premature deletion → no
  spurious verification failures).

## 9. Anti-patterns — do NOT do these

- ❌ Put `roles`/authz data in the `id_token` (it's for authenticating the user, not authorizing).
- ❌ Keep HS256 for signing once asymmetric is in — verifiers must reject HS* and `none`.
- ❌ Hand-roll JWS crypto — use `golang-jwt/jwt/v5` ES256/RS256 (already the dependency); only
  the *key lifecycle* is ours to build.
- ❌ Serve, log, or embed private key material anywhere near the JWKS.
- ❌ Maintain the old envelope and the standard shape forever — expand→contract, then delete (§7).
- ❌ Add implicit/hybrid/DCR/request-objects/pairwise `sub` "while we're in here" — deferred (§0.7).
- ❌ Delete a retired signing key before its longest-lived token has expired.
- ❌ Forget the `signing_keys` migration on `kun_galgame_infra` (deploy won't run it).

## 10. Migration & rollout (phases)

Each phase is independently deployable; earlier phases are additive.

0. **Key infrastructure.** ✅ **DONE** — `signing_keys` table + generation/encryption + `GET /oauth/jwks`
   + both discovery documents (advertising ES256/RS256, code+PKCE, endpoints). Nothing
   consumes them yet → **zero break**. → `go run ./cmd/migrate` on `kun_galgame_infra`.
   (Files: `pkg/oidckeys/`, `internal/platform/auth/{model/signing_key.go,repository/signing_key_repository.go,service/signing_key_service.go,handler/oidc_handler.go}`, wired in `cmd/oauth/main.go`, gated on `KUN_OIDC_KEY_ENC_KEY`.)
1. **Asymmetric signing.** ✅ **CODE COMPLETE** (oauth signer+verifier + all four resource-server
   verifiers accept-both via `pkg/oidctoken`, flag-gated, refresh→opaque). Remaining = the staged
   ops flip (deploy all → set `KUN_OIDC_JWKS_URL`/`KUN_OIDC_KEY_ENC_KEY` → `KUN_OIDC_SIGN_ASYMMETRIC=true`
   → later drop HS256). Sign access tokens
   (and, next phase, id_tokens) with the `active`
   ES256 key; switch the **four** infra verifiers (§3) to public-key verification by `kid`
   (fetched from local JWKS / the key set); drop `cfg.JWT.Secret` from galgame/image/artifact
   verification (public key only). Make refresh tokens opaque random strings. **Externally
   additive** (downstream introspects, verifies nothing). Internally, run a **one-access-TTL
   accept-both verify window**: the four verifiers accept HS256 (old secret) OR ES256 (new keys)
   so in-flight ≤15-min HS256 tokens issued just before the flip don't 401; after the OP has
   signed ES256 for one access-TTL, drop HS256 acceptance. No dual-*sign* overlap is needed
   (nothing external verifies our signature) — only this brief dual-*verify* window for tokens
   already in the wild. In-flight refresh tokens are unaffected: they're matched by DB value, not
   signature, so old JWT and new opaque refresh tokens coexist naturally.
2. **id_token + logout.** ✅ **DONE** — minimal `id_token` on `openid` scope (code grant),
   **RS256** (§5.2), `nonce` captured at `/authorize` → consent page → `authorization_codes.nonce`
   → id_token; the `end_session_endpoint` (`/oauth/logout`) accepts GET+POST with the
   RP-Initiated Logout params (`post_logout_redirect_uri` + `state` echo + `id_token_hint`,
   whose `aud` substitutes a missing `client_id`). ⚠️ needs `cmd/migrate` (new `nonce`
   column) **before deploy** — see §12.0. Deferred: id_token on refresh, `at_hash`.
3. **Wire-format cutover.** ✅ **DONE, unconditionally.** `/oauth/{token,userinfo,revoke}` emit
   standard top-level JSON + RFC 6749 / RFC 6750 error objects; the `KUN_OIDC_STANDARD_WIRE`
   flag and the envelope branch behind it were deleted rather than left switched-on, so there
   is no second shape to drift back to. Remaining = strip the now-dead dual-read branches from
   the RPs (§12.2 stage 3).
4. **Third parties arrived (2026-07) — the real need is OIDC compliance, not the grab-bag.**
   The registered third parties (hikarinagi, letmoe, LyCorisGal, 紫缘社, …) are all standard
   OIDC-library **SSO relying parties** (confidential code+refresh, scopes `openid profile email`,
   generic-OAuth callback paths) — **not resource servers**. So they need the OP to behave like a
   standard OP for standard client libraries, NOT DCR/DPoP/introspection.
   - ✅ **DONE — token-endpoint request compliance**: `/oauth/token` + `/oauth/revoke` now accept
     `application/x-www-form-urlencoded` (RFC 6749 §4.1.3 mandatory — what standard libs send) in
     addition to JSON, and `client_secret_basic` (HTTP Basic) in addition to `client_secret_post`;
     discovery advertises both auth methods. This was the hard blocker (standard libs couldn't even
     exchange the code).
   - ✅ **DONE — response compliance**: top-level JSON on token/userinfo, and `id_token`-on-refresh
     (OIDC Core §12.2; omitted `nonce`, which the session does not carry). Together with the
     request compliance above, a standard OIDC client now works against discovery with no
     per-client configuration.
   - ⚠️ **This was a BREAKING change for the third parties that were already live.** The envelope
     had made standard libraries unusable, so every third party with working logins had written
     custom unwrapping code — that code broke at the cutover. See §12.3.
   - **Genuinely deferred** (no current third party needs them — all are RPs, none host APIs that
     validate our access tokens): RFC 7662 introspection, DCR (RFC 7591), mTLS/DPoP (RFC 9449),
     RFC 8707 resource indicators. Back-channel logout is the one SSO-relevant deferred item.

**Contract-doc impact (Tier-A):** implementing this changes the source contracts under
`docs/integration/oauth/` (`01-oauth-endpoints`, `04-tokens-and-errors`, `07-logout`) and adds
discovery/JWKS pages. Update the **source** docs in this repo in the same PR, then run
`pnpm docs:sync --write` + `docs:audit` from `kungal-docs` to regenerate the forum/moyu mirrors
(never hand-edit the downstream copies).

## 11. Open implementation points (resolve at build time)

- **KEK provisioning & rotation** (`KUN_OIDC_KEY_ENC_KEY`): where it lives, how it's rotated
  without re-encrypting all private keys at once (envelope-encrypt each private key under a
  per-key DEK wrapped by the KEK).
- ~~**Exact `aud` values**~~ **RESOLVED**: access token `aud` = the client's **site domain**
  (from the client's Site; omitted when unbound), `client_id` claim = the requesting client;
  id_token `aud` = client_id. The access token **keeps** its `site_id` claim (§5.1), so
  galgame/image/artifact need **no lockstep logic change** — migrating those checks from
  `site_id` to `aud` is optional and can happen later.
- **Verifier key-fetch mechanism** for the separate `image`/`artifact` binaries: pull the JWK
  Set from `/oauth/jwks` (cache + refresh on unknown `kid`) vs read the `signing_keys` table
  directly (same DB is reachable). JWKS-over-HTTP is the more decoupled, standard choice.
- **Back-channel logout vs revocation-based (doc 02):** confirm whether forum/moyu want
  immediate server-to-server logout (they DO hold server-side Redis sessions, unlike pure SPAs);
  if so, back-channel is viable for them as an enhancement — reconcile with [02](./02-account-switching-design.md) §0 decision #2.
- **`/auth/refresh` self-service path** shares the refresh model — confirm the opaque-refresh
  change covers it (`AuthService.RefreshToken:579`) as well as the OAuth path.
- **Rotation cadence & overlap defaults** and whether rotation is a scheduled job (fits the
  existing in-process job registry / scheduler) or manual.

## 12. Cutover runbook (ops)

Two cutovers were designed here and they are **independent**. Their status now differs:

- **Asymmetric signing** (§12.1) is still gated by `KUN_OIDC_SIGN_ASYMMETRIC`, default off — the
  operator sequence below is how to turn it on.
- **Wire format** (§12.2) shipped on **2026-07-25 with no flag at all**. Its runbook is kept
  below as the record of what ran, in what order, and what it broke.

Within each, the order is load-bearing.

### 12.0 Prerequisites — ⚠️ ordered against the DEPLOY, not the flips
- [x] **Migrate `kun_galgame_infra` BEFORE pushing these commits** — ✅ **RUN ON PROD
  2026-07-02** (`signing_keys` + `authorization_codes.nonce` verified present; executed via
  a cross-compiled `cmd/migrate` on the stack network, ahead of any push). Why the ordering
  matters: this is **flag-independent** — the new oauth binary INSERTs
  `authorization_codes.nonce` on **every consent**, so an unmigrated DB 500s ALL SSO logins
  the moment the new build serves traffic (Postgres 42703). The prod compose DOES auto-run
  `migrate` on every deploy since `85ea4ab` (depends_on gate, sha-lockstep images), but that
  only helps when the deploy actually pulls the fresh migrate image (the known
  stale-`:latest` redeploy gap) — so the reliable order stays **migrate → push → verify**.
- [ ] Set a strong random **`KUN_OIDC_KEY_ENC_KEY`** on the **oauth** service (the KEK that encrypts
  signing keys). **Immutable once set** — changing it makes existing encrypted keys undecryptable.
- [ ] Redeploy oauth → verify `{issuer}/.well-known/openid-configuration` and `{issuer}/oauth/jwks`
  return 200 with the two published keys. (`issuer` = `KUN_SITE_URL`, e.g. `https://account.nextmoe.com`.)

### 12.1 Asymmetric signing — flip `KUN_OIDC_SIGN_ASYMMETRIC`
Makes access tokens ES256 (JWKS-verifiable) instead of HS256. Verifiers are already accept-both
(Phase 1), so this is safe once they can reach the JWKS.
- [ ] Set **`KUN_OIDC_JWKS_URL=http://oauth:9277/oauth/jwks`** (the **internal** service address) on
  **galgame, image, artifact, moderation**; redeploy them.
- [ ] Verify each resource server can fetch the JWK Set (a token minted after the flip must validate
  on each).
- [ ] Set **`KUN_OIDC_SIGN_ASYMMETRIC=true`** on oauth; redeploy oauth.
- [ ] Verify: a fresh login's access token header is `alg:ES256` + `kid`; calls to galgame/image/
  artifact with it succeed; in-flight HS256 tokens (≤15 min) still verify (accept-both window).
- [ ] **(Stage 3, later)** once no HS256 tokens remain in the wild, drop HS256 acceptance from the
  verifiers and remove `cfg.JWT.Secret` from galgame/image/artifact.

### 12.2 Standard wire format — no flag; **LIVE in production 2026-07-25 12:02Z**
`/oauth/{token,userinfo,revoke}` will emit spec-compliant top-level JSON unconditionally. There is
no switch: the envelope branch is deleted, not turned off — a second shape that still exists is a
second shape that drifts back. Rollout is expand → cut → contract, and stage 3 is scheduled rather
than deferred; "(Stage 3, later)" is how the previous attempt left a flag and two shapes behind.

- [x] **Stage 1a (first-party expand)** — wire-tolerant readers shipped and DEPLOYED before the OP
  changes: letmoe `8352e4b`, stickers `dce8637`, domain-monitor `1803796`, gpt-image2 `8b63677`,
  blog `9a62150`, forum `5f13f4b2`. Already tolerant beforehand: moyu, developer portal.
  Retired, so not an RP any more: infra/wiki.
- [x] **Stage 1b (third-party expand)** — all six live third parties shipped their own readers
  against the partner guide
  [`docs/integration/oauth/13-standard-wire-migration.md`](../integration/oauth/13-standard-wire-migration.md),
  and confirmed before the cut. See §12.3 for how they were identified.
- [x] **Stage 2 (cut)** — `e74faa7` + `b21567b`, live 2026-07-25 12:02Z. `KUN_OIDC_STANDARD_WIRE`,
  `okJSON` and the `protoErr` fallback deleted; `/oauth/userinfo` moved to
  `middleware.BearerAuth` (RFC 6750).
  - ⚠️ **Pushing infra IS deploying** — `build.yml` path-filters on `apps/api/**`. A docs-only
    commit touches no filter and is safe to push.
  - ⚠️ **The webhook redeploy ran on the STALE image** (it fires ~8s after push; the build takes
    ~3.5min). The cut only went live after a manual `docker pull` + `--force-recreate`. Always
    compare the running container's image digest against the registry — see
    [[dokploy-redeploy-no-pull-gap]].
  - ⚠️ **Caught by the post-deploy smoke, not by CI**: `/oauth/userinfo` advertised the new
    challenge header while still emitting the house envelope body. Cause: `oauth.Group("", mw)`
    attaches the guard to the `/oauth` prefix, so it silently applied to every route registered
    after it — including the `/userinfo` line that named `BearerAuth`. Fixed in `b21567b` by
    giving each route its own middleware, so no future route can inherit a guard by position.
- [x] **Stage 3 (contract)** — envelope branch stripped from all eight first-party RPs. Note the
  readers in **forum** and **gpt-image2** were SPLIT rather than simplified: they serve
  `/auth/me*` too, which keeps the house envelope permanently. `decodeProtocol` vs `decodeHouse`
  now makes the two faces impossible to confuse.

**Two traps this cutover walked into — check them on any similar change:**
1. An internal fault answered with a 4xx tells every RP the credential is permanently dead. RPs
   classify 5xx as transient and 4xx as a verdict, so a mis-typed error would log the whole
   userbase out over a blip. Internal faults MUST be `500 server_error`.
2. `invalid_token` must be mapped by each RP. Left unmapped it falls into "unknown → transient",
   and an RP then retries a permanently dead session forever instead of re-authenticating.

### 12.3 Third-party impact — **breaking, which is why the cut waits**
The original assumption ("standard-library RPs need no change") is **wrong**, and production data
disproves it. The envelope made standard libraries unusable — a conforming client reads nothing at
the top level and gives up. So **every third party with working logins has, by construction,
written custom unwrapping code**, and that code breaks at the cut.

Classification evidence (`sessions` joined to `oauth_clients`, 2026-07-25):

| client | sessions | users | sess/user | refreshed | verdict |
|---|---|---|---|---|---|
| Umbra | 3 | 3 | 1.00 | 0 | working → will break |
| Hikarinagi | 498 | 437 | 1.14 | 0 | working → will break |
| YukiHub Android | 26 | 21 | 1.24 | 0 | working → will break |
| LyCorisGal | 63 | 47 | 1.34 | 0 | working → will break |
| AniBT | 15 | 10 | 1.50 | 0 | working → will break |
| 紫缘社 | 123 | 76 | 1.62 | **4** | **proven** envelope reader |
| Singureo | 33 | 4 | **8.25** | 0 | already broken → the cut FIXES it |

How to read it: `rotated_at IS NOT NULL` proves the RP parsed `refresh_token` out of the envelope.
Absent that, **sessions-per-user** separates a working integration (~1.0–1.6, one login per user)
from a retry loop (Singureo's 8.25 — an RP that never managed to read anything). A working
integration necessarily read `access_token`, so it necessarily unwrapped the envelope.

**Post-cut watch signal:** re-run the same query. Any client whose sess/user climbs toward
Singureo's range has fallen into a retry loop — that is the rollback trigger, and it is observable
without waiting for them to report it.

- Communication template: *"The IdP is now a full standards OIDC provider (discovery / JWKS /
  id_token / spec-compliant token & userinfo responses). Configure a standard OIDC client against
  `{issuer}/.well-known/openid-configuration`; if you did anything custom to handle the old
  `{code,message,data}` wrapper, remove it."*

### 12.4 Rollback
- `SIGN_ASYMMETRIC=false` → back to HS256 signing; verifiers still accept both, so **no first-party
  breakage**.
- **The wire format has no flag to revert.** Rolling it back means reverting the commit and
  redeploying oauth. Note that a rollback would now break the third parties that migrated TO the
  standard — after the cutover, standard is the compatible direction, not the risky one.

### 12.5 Push order
⚠️ **One hard precondition before ANY infra push: run the §12.0 migration** — the infra build
writes `authorization_codes.nonce` on every consent regardless of the flags, so an unmigrated
prod DB breaks all SSO logins the moment CI redeploys oauth.
The wire cutover has **no flag**, so its push order is not a preference — it is the whole safety
mechanism. Push and let each RP finish deploying, THEN push infra:

1. **RPs first** — letmoe, stickers, domain-monitor, gpt-image2, blog, forum (moyu and the
   developer portal were already tolerant). Each must be **deployed**, not merely pushed.
2. **infra last** — the moment oauth redeploys, the envelope is gone. Any RP still on the old
   reader breaks at its next login or refresh.

Reversing this order breaks first-party login on every site that has not yet deployed.
