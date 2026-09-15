# Community Service — S2S contract

> The community primitive (`cmd/community`, database `kun_community`) is a
> multi-tenant discussion service whose only unit is a **thread**; an anchor
> decides the three shapes (board topic / entity-resource comments / feedback
> with a status flow). Design truth: `refs/docs/nextmoe-draft/11-community-primitive-design.md`.
> The machine-readable contract is `openapi.yaml` (code-first, exported by
> `cmd/gen-openapi -community`); this page is the human companion — authentication,
> tenancy, user context, and the trust/moderation semantics the spec cannot state.

## 1. Surface & transport

- Code-first OpenAPI 3.1 via Huma on Fiber v3. House envelope on every response:
  `{ "code": 0, "message": "成功", "data": … }`; errors use the same envelope
  with a non-zero `code` and the appropriate HTTP status.
- Base path `/api/v1/community`. Bind port **9282** (`KUN_COMMUNITY_PORT`).
- `GET /openapi.json` (unauthenticated) serves the live spec; `GET /healthz`.
- Migrations are NOT run at startup — `cmd/migrate community` is the single entry
  point against `kun_community`.

## 2. Authentication & tenancy

- **S2S Basic auth**: `Authorization: Basic base64(client_id:client_secret)`
  against the OAuth client registry. Any valid first-party client authenticates;
  the caller is a site BFF, not a browser.
- **Tenant `site` is derived, never on the wire.** It comes from the
  authenticated client's binding `oauth_clients.catalog_site` (the shared
  per-client site key, reused as the community tenant). A client with no binding
  is refused on every write and on the site-scoped reads (`403`). This makes a
  client unable to act outside its own site.
- **User context is BFF-supplied.** Per-user identities (`author_id`, `user_id`,
  `flagger_id`, `responder_id`, `decided_by`) travel in the request body: the BFF
  has already authenticated the user against the shared session (OIDC), and
  community trusts that assertion. Community never reverse-connects to the main
  DB or the IdP.

## 3. Read faces (embed-first)

- `POST /comments/resolve` — get-or-create the single comments thread for an
  anchor and return its first page of posts (the embed read-first-screen; Coral
  story model). Idempotent per anchor (invariant 4).
- `GET /threads` — the site's threads of a `kind`, newest-activity first, keyset
  (`cursor` opaque). An **optional anchor filter** (`anchor_kind` + `anchor_id`)
  narrows the page to a single anchor within the tenant — the resource-detail
  feedback-wall read path. It is generic across kinds (a per-anchor comments or
  topic listing benefits too), site-scoped, and keyset-paginated like the
  unfiltered listing. A real anchor id is never empty, so an empty `anchor_id` is
  the "no filter, whole site" sentinel. Each listed thread also carries its
  **opening post's** `opening_status` + `opening_author_id` (the post_number=1
  status/author; absent for an empty comments thread) so an embed can hide an
  opening post that must not leak its title — a **held** (TL0 first-post) opening
  post is visible only to its author, a **self-deleted** (tombstoned) one is
  gone. The thread's own `status` stays `open` in both cases (the moderation
  state lives on the post), so the list cannot filter on `status` alone; the
  fields are populated only on this list read, not on a thread detail (which
  already carries the opening post in its posts page). The listing takes a
  **`sort`** — `activity` (default: last activity), `created` (newest thread) or
  `posts` (most replies) — and a cursor is bound to the sort that minted it
  (replaying one under another is a `400`). An `activity` cursor keeps its
  original two-part shape, so cursors held by callers that predate `sort` still
  work; `posts` orders on a mutable key, so a row can move between pages while a
  caller pages through it. **`has_posts`** keeps only threads holding at least
  one post — a comments thread is created by the first *view* of its anchor, so
  on a busy tenant nearly all of them are empty (~110,000 of kungal's 113,000 at
  the time of writing) and an unfiltered "latest threads" read is mostly anchors
  nobody has spoken about. `GET /threads/{id}` and
  `GET /threads/{id}/posts` — a thread with a page of posts, keyset by
  `post_number` (`after`). Cooked HTML is served for display; the raw markdown is
  included for the editor.
- `GET /search/posts` and `GET /search/threads` — case-insensitive substring
  search over a post's **markdown source** and over thread titles, 2-100
  characters, optional `kind` filter, newest-first keyset (creation time for
  posts, creation time for threads — a thread-search cursor is a `created`
  cursor and an `activity` one is refused). Posts search the source, not the
  cooked HTML, or `nofollow` would match every post carrying a link. Visible
  posts and live threads only, and a title hit carries the same
  `opening_status` the listing does, so a held opening post does not leak its
  title through search either. `%` and `_` in a query are characters the user
  typed, not wildcards.
  **Why Postgres and not a search engine**: `pg_trgm` is the only CJK-capable
  index on a stock Postgres here (`zhparser`/`pg_jieba` are not installed and
  `to_tsvector` has no Chinese tokenizer). It accelerates queries of three
  characters or more; a two-character query — very common in Chinese — extracts
  no full trigram and falls back to a scan, which the corpus absorbs: measured
  on production, `ILIKE '%汉化%'` over the live 11k posts / 4.5 MB of text is a
  sequential scan returning 511 hits in **37 ms**, and the corpus grows by a few
  hundred posts a month. An external engine is the scale trigger, not the
  starting point.
- `GET /posts` — the site's newest posts across every thread (the "latest
  replies" face), each carrying its thread context, keyset by **creation time**,
  not id. Filters: `kind`, `anchor_kind` + `anchor_id`, `replies_only` (drop
  opening posts); visible posts only. The id would be the cheaper key and is the
  wrong one — the kungal import gave historical comments fresh ids, so id order
  is import order (measured `corr(id, created_at)` = 0.72). Its tenancy follows
  the **id-addressed guard** rather than the thread listing: the caller's own
  site plus catalog-anchored threads, which are one network-wide conversation by
  design (invariant 1).

## 4. Write faces (embed capability set, invariant 11)

`POST /topics`, `POST /feedback` (each opens a thread with its opening post),
`POST /threads/{id}/posts` (reply), `PATCH /posts/{id}` (author edit),
`DELETE /posts/{id}` (author self-delete), `POST /posts/{id}/reaction` (toggle),
`POST /posts/{id}/flag` (report), `POST /feedback/{id}/status`,
`POST /feedback/{id}/merge`. Capabilities are read/post/reply/edit/delete/react/
report/feedback — NOT a shrunken forum (edit **history** / the version surface,
advanced search, and mod tooling live on the full surface, not here).

#### Post edit — `PATCH /posts/{id}`

Author-only by default: `author_id` (in the body) must match the post's author,
else `403`. The **mod-actor variant** (`as_moderator: true`) skips the author
match: the calling site declares that `author_id` is one of ITS moderators —
community trusts the assertion like every other BFF-supplied identity (§2; the
role tables live at the site) and leaves a structured **audit log** of the action
(post/thread/author/moderator — the minimal audit surface, no table, matching the
review queue's `decided_by` precedent). Either way the body is re-cooked +
re-sanitized at the current `sanitizer_version` and `edited_at` is stamped;
`post_number`, `status`, and `author_id` never move. The post additionally
carries an **`edited_by_moderator`** bookkeeping bit describing the LATEST
edit's actor — a cross-author mod-actor edit sets it, an author self-edit
(including a moderator editing their own post) clears it — so a consuming site
can label "edited (moderation)" distinctly from a plain author edit. Only a
**visible** post is
editable — a held/hidden or tombstoned post returns `409` (a removed post stays
removed; a held post is released via the review queue, not an edit). The TL0
sandbox **per-post content caps apply to the edited body too** (editing is not an
escape hatch out of the newcomer sandbox); the daily create-rate caps do not (an
edit is not a new post).

#### Post delete — `DELETE /posts/{id}`

Author self-delete by default (`author_id` is a **query param** — the request
stays body-free, matching the artifact service's `DELETE`; it is a non-secret
scalar the BFF already holds); the **mod-actor variant** (`?as_moderator=true`)
skips the author match with the same site-vouched semantics and audit log as the
edit. The post is **tombstoned** (`status=deleted`) with its `post_number`
PRESERVED, so the thread numbering never collapses (invariant 13). This is the
**same terminal state** a moderator `reject` produces — only the actor differs —
and the paths coexist: a post already tombstoned by one is an idempotent no-op
for the others. `posts_count` is **not** decremented: the tombstone still
occupies its number, so the counter (numbers allocated, not live posts) stays
consistent with `highest_post_number`, matching the mod-reject path.

#### Reaction toggle — `POST /posts/{id}/reaction`

The response reports the new state (`added`) **plus the post's context** —
`author_id` / `thread_id` / `anchor_kind` / `anchor_id` — which the reaction flow
resolves anyway for the trust tallies. The context lets the consuming site fan
out its like notification (recipient + jump target) without a second read.

### Write-time content pipeline (invariant 6)

Every post body is Markdown. On write it is rendered (goldmark, GFM, raw HTML
escaped) then sanitized against a shared whitelist (bluemonday UGC: scripts /
event handlers / dangerous schemes stripped, links `rel=nofollow`). Both the raw
markdown (`content_raw`) and the cooked HTML (`content_html`) are stored with the
`sanitizer_version` that produced them; a version bump re-cooks stale posts.

### TL0 sandbox (day-1, doc 11 §6 layer 2)

A TL0 newcomer is limited to ≤2 links / 1 image / 2 mentions per post, ≤3 topics
and ≤10 replies per rolling 24h, and their first 2 posts are **held** (created
hidden + enqueued for review). TL≥1 is exempt from the content and daily caps.
Exceeding a cap returns `429`.

## 5. Unread & subscription (the sparse thread_user row)

A `(thread, user)` row exists only once that pair has interacted (Discourse's
topic_users model): a thread the user never opened carries no row and reports no
state at all — not "everything unread".

- `POST /threads/{id}/read` — the site reports how far a user has read
  (`last_read_post_number`). The mark is **monotonic** (a late receipt from a
  slower tab cannot un-read what was already read) and clamped to the thread's
  highest post number. Reading is never inferred from a GET: a read face with a
  write side effect cannot be cached, retried or prefetched safely.
- `POST /threads/{id}/notification` — set `0=muted 1=normal 2=tracking
  3=watching`. Community stores the preference and emits events (§8); delivery
  is the notification layer's job, so the level is a contract with that layer
  rather than a switch inside this service.
- The **compliance purge** (`POST /authors/{id}/purge`) clears these rows too,
  and reports `read_states_deleted`: a row records which threads a person opened
  and how far they read, which is exactly the trace the purge exists to remove.
- **Posting subscribes you**: opening a thread or replying upserts the author's
  own row at `watching` and marks their own post read. An existing row keeps its
  level — someone who muted a thread and then replies stays muted, because the
  mute was deliberate and a reply is not a request to undo it.
- `POST /threads/states` — batch state for one user over ≤100 threads, so a list
  screen gets every unread badge in one round trip. Threads with no row are
  absent from the response rather than reported as unread.
- `GET /users/{id}/unread` — the user's threads carrying unread posts (muted
  excluded), newest-activity keyset, plus a `total`: the red-dot number.
  `unread_count = highest_post_number − last_read_post_number`; a tombstone
  keeps its number (invariant 13), so a thread whose only new post was then
  deleted still reads as one unread. Scope follows the id-addressed guard, so a
  catalog-anchored thread — one conversation network-wide — is listed for every
  tenant the user reaches it from.

## 6. Trust engine (doc 11 §6)

- **Metering** — `POST /trust/activity` is the site BFF's batch receipt of a
  user's reading behavior (deltas: topics entered / posts read / read seconds /
  days visited). Likes are counted in place by the reaction flow, not here. A
  trust row is lazily created (TL0) on first contact.
- **Promotion** (Discourse-derived numbers, centralized & tunable):
  TL0→1 = 5 topics / 30 posts / 10 min; TL1→2 = 15 days / gave+received a like /
  100 posts. TL2→3 uses a **rolling-100-day** activity gate that community does
  not store: the receipt carries `window_active_days` (site-computed "active days
  in the last 100"), and ≥50 promotes to TL3. TL3 is the only demotable level — a
  later receipt whose window falls below the bar drops the user to their earned
  cumulative level. TL4 is human-granted only. Evaluation is in place after each
  receipt (no cron at single-site scale); the level layer is idempotent (a stale
  receipt never over-promotes).
- **Starter boost** — `POST /trust/boost` records a declaration made **at the
  consuming site** (which holds the IdP claims: account age / creator / staff).
  Community only records `granted_boost` and applies it as a floor:
  veteran/creator → TL1, staff → TL3. A boost never demotes; the staff floor also
  shields TL3 from a rolling-window demotion. A **staff** boost additionally
  **zeroes the first-post hold budget** (staff content is exempt from review
  outright — the hold counter is spent per-account, not per-level, so the TL3
  floor alone would not clear it); veteran/creator boosts leave the budget
  untouched.

## 7. Reputation-weighted reporting & the review queue (doc 11 §6 layers 4-5)

- A report's **weight** = the reporter's per-TL base (TL0 1.0 … TL4 2.5) × their
  historical accuracy `agreed/(agreed+disagreed)` (1.0 with no history). A post
  whose accumulated **pending** weight reaches the threshold (≈3 ordinary reports)
  auto-hides, is enqueued (`source=flags`), and emits `flag.threshold`. A **TL3+**
  reporter against a **TL0** author hides on a single vote. Reporting an
  already-hidden/tombstoned post records the flag but never re-enqueues.
- **Centralized queue** — `GET /review` lists the site's pending items (optional
  `source` filter; the cross-site super-view is a future NextMoe concern). Each
  item is joined to its subject post's **`thread_id` + `author_id`** so the
  consuming site's queue UI can deep-link the thread (where an S2S read serves
  the held content) and resolve the author, without a per-item round trip. A
  decision is about the CONTENT: `approve` keeps it (post restored to visible),
  `reject` removes it (post tombstoned). A decision on a flags item **backfills
  every reporter's accuracy** (approve → the reports were wrong → `flags_disagreed`;
  reject → right → `flags_agreed`), which feeds their future weight — the
  reputation loop. Releasing a `first_post_hold` item does not refund the hold
  counter (one-way consumption).
- **No automatic bans** (invariant 9): cross-site signals only soft-hold into the
  queue; hard bans come only from humans and IdP-level suspension.

## 8. Events (doc 11 §7)

The service only EMITS domain events (`post.created`, `reply.to_you`, `mention`,
`feedback.status_changed`, `flag.threshold`); delivery and aggregation belong to
the notification layer. v0 delivery is a no-op sink.

## 9. Not yet on the wire (deferred, with triggers)

Pre-moderation switch (per-thread/site, opened on a malicious event), Akismet /
external moderation callback (the `external` review source is reserved), board
management (`community_board` is still an empty table — every tenant anchors its
topics on an id it mints itself), aggregate hot-ranking and materialized jobs
(scale-trigger; `sort=posts` is a live count, not a ranking), and any web/TS type
generation (no in-repo consumer — letmoe reaches over S2S).
