# moyu → community primitive: the cutover

> Status: infra side done, waiting to be run. This page is the order of
> operations and the brief for the moyu session.
>
> Every number here was measured on production on 2026-09-16.

## 1. What changed on the infra side, and why

moyu's OAuth client and the kungal forum's are both `catalog_site = kungal`, and
moyu's binding is load-bearing for its catalog claims. Community used to take
its tenant from that same column, which put the two sites' comment walls in one
tenant. That is not a naming inconvenience; it is five defects, and downstream
could only paper over one of them:

| face | what one tenant did |
|---|---|
| `POST /authors/{id}/purge` | scrubbed the user's **forum** comments when moyu deleted their account — content blanked, not recoverable. 4,037 authors hold posts in that tenant and **1,199 of them have a moyu account** |
| `GET /authors/stats` | answered with the user's forum post count; moyu's profile would have printed it as a moyu comment count (today that number is 100% forum) |
| `GET /posts`, `GET /search/posts` | answered with the forum's rows, so moyu filtered them out **after** the server applied `limit` — pages arriving short or empty with a non-empty cursor. The newest 200 comment posts in that tenant are 100% the forum's |
| `GET /users/{id}/unread` | counted the user's forum subscriptions into moyu's notification badge — a red dot whose list renders empty |
| `as_moderator` edit/delete | reached the other site's posts; the tenant guard is a string compare and could not tell them apart |

`oauth_clients.community_site` now carries the community tenant, falling back to
`catalog_site` when empty (so the forum, letmoe and sticker are untouched). moyu
becomes its own tenant, and **the anchor id goes back to moyu's own bare id** —
no prefix, because there is no longer another site in the tenant to collide with.

Two more faces landed with it:

- every post a read face returns carries `reaction_count`, plus `viewer_reacted`
  when the request names a `viewer_id`. A consumer no longer needs a local like
  mirror.
- `GET /authors/top` ranks a site's authors by visible posts.

## 2. Order of operations

1. **Deploy infra.** The `migrate` job runs first and gates every service
   (`depends_on: service_completed_successfully`), so one deploy both adds
   `oauth_clients.community_site` (GORM AutoMigrate) and binds moyu to its own
   tenant — the seed sets `community_site = 'moyu'` for every client whose
   parent site is `www.moyu.moe`, leaves a value already set by hand alone, and
   is a no-op on re-runs. Nothing about it is manual, deliberately: the binding
   is stamped into every thread row a site writes, so a site that starts posting
   under the wrong tenant has to be migrated rather than re-bound.

   Verify before going further:

   ```sql
   SELECT c.name, c.catalog_site, c.community_site
     FROM oauth_clients c JOIN sites s ON s.id = c.site_id
    WHERE s.domain = 'www.moyu.moe';
   -- 鲲 Galgame 补丁 | kungal | moyu
   ```

2. **Import** — `cmd/import-moyu-comments`, dry-run first:

   ```
   MOYU_SOURCE_DSN=… go run ./cmd/import-moyu-comments            # report only
   MOYU_SOURCE_DSN=… go run ./cmd/import-moyu-comments --apply
   ```

   Source `kungalgame_patch` on 2026-09-16: 7,060 comments / 2,042 games / 427
   resource comments / 3,303 replies / 1,470 authors / 653 likes, all
   `status = 0`, no dangling parents. The ledger
   (`patch_comment_community_map`, written back into moyu's database) is the
   idempotency key: a re-run writes nothing. The likes land in
   `community_reaction` **and** move `community_trust.likes_given/received`,
   which describe those same rows. Existing trust rows are never updated by the
   seed — 1,199 of these authors are kungal forum users whose level and
   held-post budget are theirs, earned on another site.

   Check the wall count landed:

   ```sql
   SELECT anchor_kind, count(*) AS threads, sum(posts_count) AS posts
     FROM community_thread WHERE site = 'moyu' GROUP BY 1;
   ```

3. **moyu deploys** its integration (§3).

   > The import and the deploy are ordered, and the order is not the risky part
   > — the anchor shape is. moyu's pre-cutover build mints `moyu:<id>` anchors;
   > deployed after an import that wrote `<id>`, every wall would exist twice,
   > with the conversation in the one nobody is reading. Ship §3 or ship
   > nothing.

4. **Smoke**: comment, reply, like, flag, subscribe on both walls; `/comment`,
   `/user/:id/comment`, `/search?type=comment`, `/admin/comment`,
   `/message/comment`; a legacy `#comment-<id>` deep link resolving through the
   ledger.

`patch_comment` and `user_patch_comment_like_relation` stay frozen, not dropped:
they are the import's source and the rollback's evidence.

## 3. What the moyu session changes

- **Anchors lose their prefix.** `moyu:<patch.id>` → `<patch.id>` (anchor_kind
  1), `moyu-resource:<rid>` → `<rid>` (anchor_kind 2). `internal/community/anchor`
  keeps minting and resolving; `IsMoyu` becomes trivially true, because every
  thread in the tenant is moyu's now.
- **Delete the like mirror.** `patch_post_like` and its dual write go away; read
  `reaction_count` / `viewer_reacted` off the post, and pass the signed-in user
  as `viewer_id`. Keep the moemoepoint award keyed on something stable — the
  post id is now a fine key, since it no longer shares a keyspace with anything
  of moyu's.
- **Stop filtering feeds.** `GET /posts`, `GET /search/posts`,
  `GET /authors/{id}/posts` and `GET /users/{id}/unread` now answer moyu's rows
  only, so `renderFeed` drops nothing and a page of `limit` arrives full. The
  unread `total` and the red dot finally agree with the list.
- **`GET /authors/top`** restores the user board's "sort by comment count".
- **Keep resolving before acting anyway.** A post id is still global; the tenant
  guard refuses another site's post with a 404, but moyu's own resolve-first
  discipline is what turns that into a clean error instead of a surprise.
- The import runs **before** moyu's deploy, so its cutover SQL for the like
  mirror is no longer needed — the likes are upstream.

## 4. What was deliberately not done

- **No `anchor_prefix` filter on the read faces.** It would have fixed four of
  the five defects and none of the purge, and it puts the separation in a
  parameter every future caller can forget. The tenant is the place where it
  cannot be.
- **The forum's own prefixes stay.** `resource:` / `rating:` / `quiz:` /
  `toolset:` / `website:` distinguish wall *types* within one site, which is
  what an anchor id is for.
- **`cmd/retire-merged-comments` treats moyu's site_game anchors as catalog
  ids** (铁律 3: moyu's page id IS the catalog work id), so the claim exclusion
  the forum needs is off for that site. It is a code table, not a flag — a typo
  in a flag would be a silent wrong sweep.
