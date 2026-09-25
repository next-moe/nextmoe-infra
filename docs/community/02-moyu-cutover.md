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
   `status = 0`, no dangling parents. They become 2,128 walls — 1,969 game
   walls and 159 resource walls, fewer game walls than commented games because
   73 games were only ever commented on through a resource. The ledger
   (`patch_comment_community_map`, written back into moyu's database) is the
   idempotency key: a re-run writes nothing. The likes land in
   `community_reaction` **and** move `community_trust.likes_given/received`,
   which describe those same rows. Existing trust rows are never updated by the
   seed — 1,199 of these authors are kungal forum users whose level and
   held-post budget are theirs, earned on another site.

   Every author of an imported wall also gets the `community_thread_user` row
   the write path would have written for them — watching, and caught up to the
   thread's last post. Without it a legacy commenter reads "not subscribed" on
   a thread they started; caught up rather than at their own post because moyu
   had no unread feature before the cutover, and a watermark at their own post
   would have opened the new badge on replies they had already read. An
   existing row is left exactly as it is, so a re-run after moyu is live cannot
   mark anyone caught up on a reply they have not seen.

   `patch_comment.edit` holds two formats: `Date.now()` milliseconds before
   2026-06-03 and RFC3339 from 2026-06-06 on. Both become `edited_at`; the
   dry run's `unparsable-edit` counter must read 0, and a non-zero one means
   moyu started writing a third thing.

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

### Undoing an import

Everything the import writes is either tagged `site = 'moyu'` or in a table it
created, so an undo is a delete, not a restore — as long as it runs **before**
moyu's deploy, after which the rows are no longer only the import's. Run it in
this order inside one transaction; the trust adjustment has to come first,
because it reads the reactions it is reversing:

```sql
BEGIN;
WITH mine AS (
  SELECT p.id, p.author_id FROM community_post p
    JOIN community_thread t ON t.id = p.thread_id WHERE t.site = 'moyu'
), given AS (
  SELECT r.user_id, count(*) n FROM community_reaction r
    JOIN mine m ON m.id = r.post_id GROUP BY 1
), received AS (
  SELECT m.author_id user_id, count(*) n FROM community_reaction r
    JOIN mine m ON m.id = r.post_id GROUP BY 1
)
UPDATE community_trust c SET
  likes_given    = GREATEST(0, COALESCE(c.likes_given,0)    - COALESCE(g.n,0)),
  likes_received = GREATEST(0, COALESCE(c.likes_received,0) - COALESCE(r.n,0))
  FROM (SELECT user_id FROM given UNION SELECT user_id FROM received) u
  LEFT JOIN given g USING (user_id) LEFT JOIN received r USING (user_id)
 WHERE c.user_id = u.user_id;

DELETE FROM community_reaction WHERE post_id IN
  (SELECT p.id FROM community_post p JOIN community_thread t ON t.id = p.thread_id WHERE t.site='moyu');
DELETE FROM community_thread_user WHERE thread_id IN (SELECT id FROM community_thread WHERE site='moyu');
DELETE FROM community_post   WHERE thread_id IN (SELECT id FROM community_thread WHERE site='moyu');
DELETE FROM community_thread WHERE site = 'moyu';
COMMIT;
```

The 127 seeded `community_trust` rows are deliberately left behind: they are
keyed on the user alone, carry no site, and cost nothing. `DROP TABLE
patch_comment_community_map` in moyu's database finishes it — but only if
moyu's migration 040 has not run, since after that the table is theirs.

## 3. What the moyu session changes

- **Anchors lose their prefix.** `moyu:<patch.id>` → `<patch.id>` (anchor_kind
  1), `moyu-resource:<rid>` → `<rid>` (anchor_kind 2). `internal/community/anchor`
  keeps minting and resolving. `IsMoyu` does **not** become trivially true — see
  the feed bullet below.
- **Delete the like mirror.** `patch_post_like` and its dual write go away; read
  `reaction_count` / `viewer_reacted` off the post, and pass the signed-in user
  as `viewer_id`. `PATCH /posts/{id}` fills both too (its viewer is the acting
  user); until the fix it answered 0, which moyu worked around by carrying the
  count over from the resolve it did before the edit. `POST /posts/{id}/reaction`
  answers `reaction_count` after the toggle, and its `added` is the clicker's
  new `viewer_reacted` — render the click from the response, no re-read. Keep the moemoepoint award keyed on something stable — the
  post id is now a fine key, since it no longer shares a keyspace with anything
  of moyu's.
- **Keep the feed filter; expect fewer dropped rows, not none.** An earlier
  version of this page said the feeds now answer moyu's rows only. They do not:
  `GET /posts`, `GET /search/posts`, `GET /search/threads` and the unread faces
  follow the id-addressed guard — **the caller's site plus every
  catalog-anchored thread** (anchor kinds 3/4), which is one network-wide
  conversation by design and contract invariant 1. What the tenant removed is
  the forum's *site-local* walls, which were all of the rows moyu was dropping.
  Another site's catalog-anchored thread still arrives, so `renderFeed` and the
  ref-ping's `IsMoyu` stay, and a page can still be shorter than `limit`. There
  are no catalog-anchored threads in production yet (0 on 2026-09-16), so today
  the pages arrive full; the filter is for the day there are.
  **Strictly `site = ?`**: `GET /authors/{id}/posts`, `GET /authors/stats`,
  `GET /authors/top` and `POST /posts/resolve`. Those need no filter.
  The unread `total` counts the same rows as the unread list, so a catalog
  thread moyu drops from the list would still light the badge — again, not
  reachable until catalog-anchored threads exist.
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
  in a flag would be a silent wrong sweep. Since 2026-09-24 the redirect row is
  taken as proof on such a site and the conversation moves to the survivor's
  page rather than being retired. letmoe joined that code table on 2026-09-25
  (its game id has been the catalog work id since its migration 028).
