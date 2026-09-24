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
  authenticated client's binding: `oauth_clients.community_site` when it is set,
  and `oauth_clients.catalog_site` otherwise. A client with neither is refused on
  every write and on the site-scoped reads (`403`). This makes a client unable to
  act outside its own site. Trust binds its site by the same rule, so a site's
  comment walls and its report queue are always one tenant.
- **Why community has its own binding.** `catalog_site` names the site a client
  files CATALOG CLAIMS under, and two properties can share that identity while
  being separate communities: moyu and the kungal forum are both
  `catalog_site=kungal`, and moyu's binding is load-bearing for its claims. While
  community read that column, the two sites shared one tenant, where the anchor
  id was the only separation (1,992 of moyu's 2,040 commented page ids already
  existed as a forum anchor, 490 of them carrying forum posts) — and every
  tenant-wide face answered the other site's rows: the post feed, both searches,
  the unread total behind a notification badge, per-author post counts, and
  `POST /authors/{id}/purge`, which would have scrubbed a user's forum comments
  when moyu deleted their account. `community_site` separates them at the
  tenant, which is the only place a filter cannot be forgotten. Set it with SQL
  (`UPDATE oauth_clients SET community_site = '<site>' WHERE id = '<client_id>'`)
  — it has no admin face, like `catalog_site` — and set it **before** the site
  writes its first thread: changing it later strands every row under the old
  tenant.
- **User context is BFF-supplied.** Per-user identities (`author_id`, `user_id`,
  `flagger_id`, `responder_id`, `decided_by`) travel in the request body: the BFF
  has already authenticated the user against the shared session (OIDC), and
  community trusts that assertion. Community never reverse-connects to the main
  DB or the IdP.

## 3. Read faces (embed-first)

- `GET /comments?anchor_kind=&anchor_id=` — an anchor's comment wall: its single
  comments thread (invariant 4) with the first page of posts, keyset by
  `post_number` like every other post page (the embed read-first-screen; Coral
  story model). Until someone comments there is **no thread**, and the face says
  so: `thread` is absent and `posts` is empty. It reads; it never writes.
- `POST /comments/resolve` — **deprecated**, kept only until the consuming sites
  move off it. Same first screen, but it *get-or-creates* the thread, so a page
  view mints a row: 110,918 of kungal's 114,070 comments threads were minted by
  a view and hold nothing at all. Read with `GET /comments`, write with
  `POST /comments` (§4).
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
  one post. **`board_id`** narrows a topic listing to one board (with
  **`subboards=true`**, the board and its sub-boards) and replaces the anchor
  filter for boards — sending both is a `422`, as is a `board_id` on a kind other
  than topic; a board of another site is a `404`. **`pinned`** is `any`
  (default), `only` or `exclude`: on a board both board and site pins count as
  pinned, on the site listing only site pins do, so a site page reads
  `pinned=only` for its banner topics and `pinned=exclude` for the rest, and a
  board page does the same with `board_id`. `only` returns the pinned set as one
  page, newest pin first, and refuses a `cursor` (`400`). A comments thread is
  created by its first *comment*, but the
  deprecated resolve face created one per anchor **view**, so a tenant that has
  been serving pages carries a long tail of empty threads (110,918 of kungal's
  114,070 when the write path changed) and an unfiltered "latest threads" read
  is mostly anchors nobody has spoken about. `GET /threads/{id}` and
  `GET /threads/{id}/posts` — a thread with a page of posts, keyset by
  `post_number` (`after`). Cooked HTML is served for display; the raw markdown is
  included for the editor.
- Every thread view carries **`board_id`** when the thread is a board topic (its
  `anchor_id` as a number) and, while a pin is in force, **`pin_scope`** /
  `pinned_at` / `pinned_until`; a pin whose `pinned_until` has passed reads as
  unpinned everywhere. The post context on feed, search and author rows carries
  the same `board_id`.
- `GET /boards`, `GET /boards/{id}`, `GET /boards/by-slug/{slug}` — see §5.
- `GET /search/posts` and `GET /search/threads` — case-insensitive substring
  search over a post's **markdown source** and over thread titles, 2-100
  characters, optional `kind` filter, newest-first keyset (creation time for
  posts, creation time for threads — a thread-search cursor is a `created`
  cursor and an `activity` one is refused). Posts search the source, not the
  cooked HTML, or `nofollow` would match every post carrying a link. Visible
  posts and live threads only, and a title hit carries the same
  `opening_status` the listing does, so a held opening post does not leak its
  title through search either. `%` and `_` in a query are characters the user
  typed, not wildcards. Both follow the **id-addressed guard**, like `GET /posts`
  and the unread faces: the caller's own site plus catalog-anchored threads.
  Only `GET /authors/*` and `POST /posts/resolve` are strictly `site = ?`, so a
  consumer that renders results as its own pages still has to drop anchors it
  does not own from the other four — and a page can arrive shorter than `limit`.
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

- `GET /authors/{id}/posts`, `POST /posts/resolve`, `GET /authors/stats` — one
  author's visible posts with thread context (keyset by post id, `anchor_kind`
  filter), a batch hydrate of <=100 post ids in request order, and visible-post
  counts for <=100 named authors (`kind` / `anchor_kind` filters, `-1` = every).
  All three are site-scoped.
- `GET /authors/top` — the site's most-posted-in authors, most first, with
  `kind` / `anchor_kind` filters and a `limit`. `GET /authors/stats` answers a
  caller that already knows which authors it means; a leaderboard is the question
  no batch of named ids can ask. Ties break on the lower author id, so a page is
  stable between calls.
- **Every post any face returns carries `reaction_count`**, and
  `viewer_reacted` when the request names a viewer (a `viewer_id` query parameter
  on the GET faces, a body field on `POST /posts/resolve`; a request with no
  viewer gets counts and `false`). That includes the one write face that returns
  a post it did not just create, `PATCH /posts/{id}`, whose viewer is the acting
  user in the body — it shipped without the count and answered 0 on a post with
  likes until moyu reported it. The exception is `POST /threads/{id}/posts`,
  where the post was inserted by that same call and 0 is the true count. Without
  a count a site that wanted to render a like count had
  to keep a mirror table of its own beside every post id and dual-write it on
  each toggle — two writes that are not one transaction, drifting from
  `community_reaction` the first time one fails and from
  `community_trust.likes_given/received` (maintained from the same rows)
  permanently.

## 4. Write faces (embed capability set, invariant 11)

`POST /topics`, `POST /feedback` (each opens a thread with its opening post;
a topic opens on a board, a feedback thread on an entity anchor `1..4`),
`POST /comments` (comment on an anchor), `POST /threads/{id}/posts` (reply),
`PATCH /posts/{id}` (author edit),
`DELETE /posts/{id}` (author self-delete), `POST /posts/{id}/reaction` (toggle),
`PUT` / `DELETE /posts/{id}/reaction` (set / unset, idempotent),
`POST /posts/{id}/flag` (report), `POST /feedback/{id}/status`,
`POST /feedback/{id}/merge`. Capabilities are read/post/reply/edit/delete/react/
report/feedback — NOT a shrunken forum (edit **history** / the version surface,
advanced search, and mod tooling live on the full surface, not here).

#### Open a topic — `POST /topics`

A topic names its board with **`board_id`**. The board must belong to the
caller's site (`404` otherwise) and passes its gates before anything is written:
an archived board takes no topics (`409`), an announcement board takes them only
with **`as_moderator: true`** (`403` otherwise), and `topic_min_trust_level`
refuses authors below it (`403`) unless `as_moderator` is set. The topic's
rating is the higher of the requested one and the board's floor, and the opening
post carries the same. `anchor_id` is the **deprecated** way to name the board —
a board id when it is all digits, a slug otherwise — and exists only so a site
that opened topics on its own string (letmoe's `"main"`) keeps working through
the switch; sending both is a `422`. The key resolves only to a board that
exists: the migration created `main` where topics already hung from it
(production letmoe and letmoe-staging), and anywhere else — a fresh or local
database — a site creates its boards before opening topics, or gets a `404`.

#### Comment on an anchor — `POST /comments`

A comments thread is **born with its first comment**, in the transaction that
writes that comment — the same rule topics and feedback already follow. The body
therefore carries the anchor (`anchor_kind` + `anchor_id`) and the
`content_rating` to stamp on a thread that may not exist yet, not a thread id;
`anchor_kind` must be 1..4, since a board hosts topics rather than a comment
wall. A concurrent first comment loses the insert and appends to the winner's
thread instead (`ON CONFLICT DO NOTHING`, then re-read inside the same
transaction), so an anchor never ends up with two conversations. Everything past
the thread is the ordinary reply path — trust level, sandbox quota, content
check, review enqueue, auto-subscribe — and the response carries the thread as
it stands *after* the write, together with the new post.

#### Retrying a create — `Idempotency-Key` on `POST /comments` and `POST /threads/{id}/posts`

A caller that times out cannot tell whether its write landed, and before this
header a retry after a slow first write posted the comment twice (moyu's BFF
gives up at 8 s). With `Idempotency-Key: <key>` (at most 255 bytes, unique per
site; a UUID per user action is the intended use):

- The first call writes the post and records the key **in the same
  transaction**, so a post and its key are never committed without each other.
- A later call with the same key and the same request answers with **that
  post** — `200`, the same body shape, header `Idempotency-Replayed: true` —
  and writes nothing. The request is the body plus, for a reply, the thread id.
- The same key with a different request is `409`.
- Two calls with the same key that race each other write once: the second to
  reach the write waits for the first to commit, then replays it. If the first
  fails, the second writes.
- A call that failed wrote nothing and recorded no key, so retrying it runs the
  whole write again.
- Keys are scoped to the site (the tenant from §2) and kept for **24 hours**.
- No header means no change: every call writes.

The replayed post is the post as it stands now, so it carries any reactions it
has received since.

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

The response reports the acting user's new state (`added` — the
`viewer_reacted` a read face would report for them), the post's like count after
the toggle (`reaction_count`, read in the same transaction, the same number a
read face reports), **plus the post's context** — `author_id` / `thread_id` /
`anchor_kind` / `anchor_id` — which the reaction flow resolves anyway for the
trust tallies. The context lets the consuming site fan out its like notification
(recipient + jump target) without a second read; the count lets it render the
click's result without one. The count arrived after the read faces did: until
then a site that had dropped its mirror table had to re-read the post after every
click, which is the round trip the mirror had been saving it.

#### Reaction set / unset — `PUT` / `DELETE /posts/{id}/reaction`

A toggle undoes itself when retried, so a caller that retries after a timeout
turned a slow like into no like. `PUT` (body `{user_id, kind}`) adds the
reaction and `DELETE` (`?user_id=&kind=`, body-free like the post delete) removes
it; repeating either changes nothing. Both answer with the toggle's shape, and
**`changed`** says whether this call moved the state: `false` means the reaction
was already as asked, which is what a retry sees. The trust tallies and the
like notification move only on a change, so a repeated `PUT` credits the author
once. The toggle stays for the callers that use it, and reports `changed: true`.

A reply to a board topic (`POST /threads/{id}/posts`) passes the board's gates
too: no replies on an archived board (`409`), and `reply_min_trust_level`
refuses authors below it (`403`). An author can still edit and delete their
posts on an archived board.

#### Author purge — `POST /authors/{id}/purge`, `POST /authors/{id}/purge/restore`

The compliance purge tombstones every post the author wrote on this site and
blanks its content, and deletes their reactions and the rows §6 lists. In the
same transaction it keeps every row it changed or deleted, as it was, in
`community_purge_archive`, and the service prunes those rows 30 days later.
Until then `POST /authors/{id}/purge/restore` undoes this site's purges of the
author. Posts get their status and content back unless their status has changed
since, as when a moderator approved one. Deleted rows are reinserted unless the
user has recreated the same row since. The actor ids and event recipients the
purge cleared are put back. The response counts mirror the purge's. The call
returns `404` when nothing is left to restore: the author was never purged here,
the purge was already undone, or it is older than 30 days.

The purge used to keep nothing. On 2026-09-23 the forum purged two users by
mistake; their forum rows came back from a dump that happened to exist and from
dead tuples, and their comments here could not have come back at all. For 30 days, a purged author's content can now be recovered from this
database, as it can for 28 days from the nightly dumps.

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

## 5. Boards and topic moderation

A **board** is the anchor every topic hangs from (`anchor_kind = 0`, `anchor_id`
= the board id). Boards are per site and nest **one level**: a top-level board
may hold sub-boards, a sub-board holds none. The site owns who may manage them —
community trusts the calling BFF, as it does for every identity (§2), and writes
`actor_id` to the audit log.

| field | meaning |
|---|---|
| `slug` | URL key, unique on the site; lowercase letters, digits and single hyphens, **never all digits** (a numeric key is read as an id) |
| `name`, `description`, `icon`, `color`, `topic_template` | presentation; `icon` is an emoji, icon name or image hash and `color` a palette token, both rendered by the site; `topic_template` is the markdown a composer starts from |
| `parent_id`, `position` | the tree and the order among siblings; a new board goes last |
| `format` | `0` discussion, `1` Q&A (a topic can mark its answer), `2` announcement (only moderators open topics; everyone can reply) |
| `status` | `0` active, `1` archived: everything stays readable, no new topics or replies |
| `content_rating` | the floor for its topics' rating (invariant 12); changing it applies to topics opened afterwards |
| `topic_min_trust_level`, `reply_min_trust_level` | trust level `0..3` needed to open a topic / to reply. Capped at 3 because a staff boost floors staff at TL3 (§7) — a higher gate would lock the site's own moderators out |

**Faces.** `GET /boards` lists the site's boards in display order (each
top-level board followed by its sub-boards); `GET /boards/{id}` and
`GET /boards/by-slug/{slug}` read one. `POST /boards` creates, `PATCH
/boards/{id}` changes only the fields it carries (`parent_id: 0` moves a board
to the top level, an empty text clears an optional field; a board with
sub-boards cannot become a sub-board), `DELETE /boards/{id}?actor_id=` removes a
board only when **no thread names it** — tombstoned topics included — and it has
no sub-boards (`409` otherwise: move the topics, or archive the board). Deleting
a board also removes its anchor subscriptions. `POST /boards/reorder` sets one
parent's order from a list that must name every sibling exactly once (`422`
otherwise), so the result never depends on positions the caller did not see. A
duplicate slug is a `409`; another site's board is a `404` on every face.

**Stats.** Every board read carries `stats`: `topics_count` counts the topics a
reader sees in the listing — live threads whose opening post is visible, so a
held or tombstoned opening drops out — `posts_count` sums those topics'
`posts_count` (numbers allocated, like the thread counter), and
`last_posted_at` / `last_thread_id` / `last_thread_title` name the most recently
active one. A board's stats are its own; a parent does not include its
sub-boards. They are computed from the threads on every read — production held 3
topics when boards shipped, and the aggregate uses a partial index on board
topics. A rollup is the scale trigger, and when it comes it must be filled
by this same aggregate.

**Concurrency.** Opening a topic re-reads its board under a share lock inside
the write, and deleting or re-parenting a board takes an update lock first, so a
topic never lands on a board that was deleted while it was being written.

**Legacy anchors.** Before boards existed a site named its board with a string
of its own. The migration that introduced boards turned each such string into a
board of that slug on its site — letmoe's and letmoe-staging's `"main"`, the
only ones in production — and re-anchored the topics onto its id. The board is
named after its slug until the site renames it.

### Moderation faces

Each takes the acting moderator as `actor_id` and answers with the thread as it
stands afterwards. They reach only threads **the caller's site opened** (`404`
otherwise) — stricter than the id-addressed guard: a catalog-anchored thread
takes replies from every site, but closing it would close it for all of them,
so only its own site moderates it, as with `POST /feedback/{id}/status`.

- `POST /threads/{id}/move` `{board_id}` — moves a board topic to another board
  of the same site. The topic's rating (and every post's) rises to the new
  board's floor and never falls: an R18 topic stays R18 on an all-ages board.
  A board pin stays behind (it was about the old board); a site pin moves with
  the topic.
- `POST /threads/{id}/pin` `{scope, until?}` — `1` pins it on its board, `2` on
  its board and on the site listing, `0` unpins; `until` is a future time after
  which the pin lapses by itself. Pinning again restarts `pinned_at`, which is
  the order pinned topics list in. Board topics only.
- `POST /threads/{id}/close` `{closed}` — closes a thread of any kind to new
  posts (`409 thread is not open` on a reply) or reopens it. A merged feedback
  thread stays closed.
- `POST /threads/{id}/answer` `{post_id, as_moderator?}` — marks the reply that
  answers a topic on a Q&A board, or a feedback thread; `post_id: 0` clears it.
  The thread's author marks it, or a moderator with `as_moderator`. The answer
  must be a visible reply in that thread. It is checked when it is marked and
  not kept in step afterwards: a reply removed later stays named in
  `answer_post_id`, and a reader renders it as removed.

## 6. Unread, subscriptions and notifications

A `(thread, user)` row exists only once that pair has interacted (Discourse's
topic_users model): a thread the user never opened carries no row and reports no
state at all — not "everything unread".

- `POST /threads/{id}/read` — the site reports how far a user has read
  (`last_read_post_number`). The mark is **monotonic** (a late receipt from a
  slower tab cannot un-read what was already read) and clamped to the thread's
  highest post number. Reading is never inferred from a GET: a read face with a
  write side effect cannot be cached, retried or prefetched safely.
- `POST /threads/{id}/notification` — set `0=muted 1=normal 2=tracking
  3=watching`. Community stores the preference; the dispatcher in this service
  applies it when it turns outbox events into inbox rows.
- The **compliance purge** (`POST /authors/{id}/purge`) clears these rows too,
  and reports `read_states_deleted`, `anchor_subscriptions_deleted` and
  `notifications_deleted`: a row records which threads a person opened and how
  far they read, which anchors they watch and what they were told, which is
  exactly the trace the purge exists to remove. A thread row belongs to the
  site it records (the thread's site when that is NULL), the same site its
  notifications are delivered to, so a catalog thread row written through
  another site is that site's to purge.
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

### Anchor subscriptions

A `(site, user, anchor)` row exists only when the user has set a non-normal
level on that anchor. The faces are `POST /anchors/notification`,
`POST /anchors/states`, and `GET /users/{id}/anchor-subscriptions`.

An anchor row accepts `0=muted`, `3=watching`, `4=watching first post`. `1`
normal is the absence of a row: setting it deletes the row and the face answers
level 1. Tracking (`2`) is thread-only and refused here — an anchor-level
"tracking" would have to count threads the user never opened, which the sparse
thread row model does not have.

`site` is the **delivery site**: the site the user subscribed through, not the
anchor's tenant. A catalog work is one conversation for the whole network, and
users of two sites subscribe to it separately and are notified on their own
site. A site-local anchor (kinds 0–2) is interpreted in the caller's id space;
a board anchor (kind 0) must name an existing board of the caller's site
(`404` otherwise).

Effective level for a user on a thread: the thread row's level if a thread row
exists; otherwise the level of that user's anchor row for the thread's anchor,
if one exists; otherwise normal. A site-local anchor only counts rows of the
thread's own site; a catalog anchor counts each site's row separately, one
delivery per site. Muted means nothing is sent. A poster's own thread row
(watching) therefore outranks a muted board.

Because a thread row outranks the anchor, the first row a **read** writes takes
`watching` when the user watches the thread's anchor through the reading site,
and `normal` otherwise — a board watcher keeps hearing about a topic after
opening it. The anchor only seeds a new row: an existing row keeps its level,
and unwatching the anchor later does not rewrite it. A muted anchor is not
copied: it silences the threads a user has never opened, and once they open
one, replies and mentions addressed to them come through (a `normal` row still
gets no `posted`).

`community_thread_user` now records the site of the user's latest interaction.
A catalog-anchored thread can be watched from several sites; the column says
which site that interaction came through. A NULL (the one-off importers still
insert without it) falls back to the thread's site.

### Notifications

Writes that should notify someone (`post_created`, `post_liked`,
`feedback_status_changed`, `answer_accepted`) insert a `community_event` row
**in the same transaction** as the write. A crash after commit cannot lose the
event; the in-memory sink is not the notification path.

A single dispatcher (`NotificationService.Run`) claims a transaction-level
advisory lock, then processes up to 50 pending events with `FOR UPDATE SKIP
LOCKED`. `seq` on `community_notification` comes from
`community_notification_seq` and is assigned only by the dispatcher, on insert
and on every fold update. One writer is what makes `seq` equal commit order, so
a feed reader never skips a row. Marking a row read does **not** move `seq`.

Recipients of one event are keyed by `(delivery site, user)` and keep the
highest-priority kind per key: `replied` > `mentioned` > `thread_created` >
`posted`. Delivery site is the anchor row's `site` when the source is an
anchor subscription; otherwise `COALESCE(thread_row.site, thread.site)` when
the user has a thread row, else the event's `site`. Then:

1. Drop the event's actor.
2. Drop anyone whose **effective level** on the thread (for that delivery
   site) is muted.
3. Drop an **anchor-sourced** candidate when the user has any thread row on
   that thread — the thread row governs.

A `post_created` event notifies the reply target (`replied`), each mentioned
id (`mentioned`), watching thread rows (`posted` when the post is not the
first), and matching anchor rows: `watching` is `thread_created` on the first
post of a non-comments thread and `posted` otherwise; `watching first post` is
`thread_created` only on that first non-comments post (a comment wall's first
comment is not a new thread). A like notifies the post's author (`liked`). A
feedback status change notifies the thread creator (while they still have a
thread row — a purge removes it) and watching thread rows
(`feedback_status`); a call that leaves the status and the response as they
were enqueues nothing. Marking an answer notifies the answer's author
(`answer_accepted`); clearing it or marking the same post again enqueues
nothing, and an answer replaced before its event is dispatched is dropped.

A held (hidden) post with a pending review item is **parked** and retried with
backoff `min(2^attempts minutes, 60 minutes)`. Approve it and the next attempt
delivers; reject it and the next attempt drops. A missing / hidden / deleted
thread, a missing or deleted post, a hidden post with no pending review, or a
like that has already been undone is dropped.

The seven kinds and their folds:

| kind | fold key | counts |
|---|---|---|
| `1` replied | none | one row per event |
| `2` mentioned | none | one row per event |
| `3` posted | `posted:<thread_id>` | `item_count` = visible posts in `[first_post_number, post_number]` not by the recipient; `actor_count` = distinct authors of those |
| `4` thread_created | none | one row per event |
| `5` liked | `like:<post_id>` | `item_count` = `actor_count` = like reactions on the post with `created_at >= since_at` (the earliest like folded in) not by the recipient |
| `6` answer_accepted | none | one row per event |
| `7` feedback_status | `fb:<thread_id>` | on conflict `item_count = item_count + 1` |

A fold points at its latest post and actor. A parked post delivered after the
posts that followed it widens the fold's range back to itself rather than
moving that pointer.

Reading a thread (`POST /threads/{id}/read`) marks that user's unread
`replied` / `mentioned` / `posted` / `thread_created` rows for the thread
(any site) whose `post_number` is at or before the clamped watermark. Posting
in a thread does the same up to the new post, as it already moves the author's
watermark there. That is what resets a fold: the next activity starts a new
row. A fold's counts leave out the recipient's own posts.

Two ways to consume:

- **Inbox** (a site without its own inbox): `GET /users/{id}/notifications`
  (newest `seq` first, optional `unread_only`, `unread_count`) and
  `POST /users/{id}/notifications/read` (`ids` or `all` — exactly one).
- **Feed** (a site that mirrors into its own inbox): `GET /notifications/feed`
  (`seq > after`, ascending). Upsert by `id`, keep `next_after`, and forward
  reads with `POST /users/{id}/notifications/read` so folds reset.

`mention_user_ids` rides `POST /topics`, `POST /feedback`,
`POST /comments`, and `POST /threads/{id}/posts`. The site resolves `@name` to
user ids; community only delivers. More than 20 ids sent is a `422`; ids
`<= 0`, the author, and duplicates are dropped.

Retention: processed events older than 7 days, and notifications read more
than 90 days ago, are pruned. The compliance purge additionally deletes that
site's notifications whose recipient is the user (`notifications_deleted`),
nulls `actor_id` on that site's rows whose actor is the user, deletes that
site's events whose actor is the user, and removes the user as reply target and
mention from that site's pending events (the last three are logged, not
reported). The purge waits for a running dispatch batch, so no batch can
deliver to the user after the purge has cleared their rows.

## 7. Trust engine (doc 11 §6)

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

## 8. Reputation-weighted reporting & the review queue (doc 11 §6 layers 4-5)

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

## 9. Events (doc 11 §7)

The in-memory sink remains for trust forwarding and scanning (`post.created`,
`reply.to_you`, `feedback.status_changed`, `flag.threshold`, review enqueue /
approve / reject). Notifications come from the transactional outbox
(`community_event`) instead; the sink is not the fan-out path.

## 10. Not yet on the wire (deferred, with triggers)

Pre-moderation switch (per-thread/site, opened on a malicious event), Akismet /
external moderation callback (the `external` review source is reserved),
aggregate hot-ranking and materialized jobs (scale-trigger; `sort=posts` is a
live count, not a ranking), and any web/TS type generation (no in-repo consumer
— letmoe reaches over S2S).

Boards leave these out on purpose, each with the condition that brings it in:

- **Read-restricted boards** (staff-only areas). Every read face — listings,
  feed, both searches, thread and post reads, unread, author and resolve faces —
  would have to take the viewer's role from the site and filter on it; a filter
  every face must remember is the failure the tenant split exists to avoid.
  Trigger: the first site that needs a hidden board, designed as a scope on the
  tenant rather than a parameter on each face.
- **Per-board moderators.** Roles live at the site, which already vouches with
  `as_moderator` and can read a topic's board from `board_id`. Trigger: a site
  that wants community to hold the assignment.
- Tags, polls, slow mode and auto-close timers.

Two follow-ups wait on the consuming sites rather than on this service: the
retirement of `POST /comments/resolve` (a declared breaking change), and the
one-off sweep of the empty comments threads it minted. The sweep is safe —
nothing but `community_post`, `community_thread_user` and the notification
tables (`community_event`, `community_notification`, both written only for a
post or a feedback thread) references a thread, and none of the empty rows
carry any of them — but it has to run *after* the sites
stop calling resolve, or the next page view mints them straight back.

Notifications leave these out on purpose, each with the condition that brings
it in:

- **Push delivery** (webhook or SSE red dot). Trigger: a site needs lower
  latency than polling the feed.
- **Mentions added by an edit.** The outbox records mentions at write time of
  a new post; an edit does not enqueue. Trigger: a site needs edit-time
  mentions to notify.
- **Anchor-level tracking.** Tracking (`2`) is thread-only. Trigger: a product
  wants "new posts on this board" without watching every thread.
- **A per-board default level.** New threads inherit nothing from the board
  beyond the explicit anchor row. Trigger: a site wants a board-wide default
  other than normal.
- **Templates and App push** (doc 02 §6 tier 3). Trigger: a first-party app
  that is not a site BFF needs rendered copy or a device push.
