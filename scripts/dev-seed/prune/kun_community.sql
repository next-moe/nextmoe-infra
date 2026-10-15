-- Prune kun_community down to the threads whose anchors survive in the forum
-- and moyu seeds. Runs AFTER prune/kungalgame.sql and
-- prune/kungalgame_patch.sql, which export the kept content ids.
--
-- Anchor model: site='kungal' anchor_kind=1 anchors on a forum galgame id;
-- anchor_kind=2 anchors on '<prefix>:<id>' where prefix 'resource' points at
-- forum galgame_resource rows and quiz/toolset/website are site-global
-- features we keep wholesale. 'rating' threads are dropped: their targets are
-- per-user rating rows that do not survive sampling, so keeping them would
-- only produce dangling anchors in dev. site='moyu' anchors on a patch id
-- (anchor_kind=1) or a patch resource id (anchor_kind=2). Other sites' threads
-- are few and kept wholesale. Only open/closed threads and visible posts
-- survive: hidden or deleted content does not ship.

-- CSV paths are CWD-relative: \copy does not interpolate psql variables, so
-- build-seed.sh runs this file with CWD = the shared export directory.

BEGIN;

CREATE TEMP TABLE in_galgame (id bigint PRIMARY KEY);
\copy in_galgame FROM 'kungalgame_galgames.csv'
CREATE TEMP TABLE in_resource (id bigint PRIMARY KEY);
\copy in_resource FROM 'kungalgame_resources.csv'
CREATE TEMP TABLE in_patch (id bigint PRIMARY KEY);
\copy in_patch FROM 'kungalgame_patch_patches.csv'
CREATE TEMP TABLE in_patch_resource (id bigint PRIMARY KEY);
\copy in_patch_resource FROM 'kungalgame_patch_resources.csv'

-- Root keep-set, then close it over merged_into_id chains so the thread
-- self-FK stays satisfied after the delete.
CREATE TEMP TABLE keep_thread (id bigint PRIMARY KEY);
INSERT INTO keep_thread
WITH RECURSIVE roots AS (
  SELECT t.id
  FROM community_thread t
  WHERE t.status IN (0, 1)
    AND (t.site NOT IN ('kungal', 'moyu')
     OR (t.site = 'kungal' AND t.anchor_kind = 1 AND t.anchor_id ~ '^[0-9]+$'
         AND t.anchor_id::bigint IN (SELECT id FROM in_galgame))
     OR (t.site = 'kungal' AND t.anchor_kind = 2
         AND split_part(t.anchor_id, ':', 1) IN ('quiz', 'toolset', 'website'))
     OR (t.site = 'kungal' AND t.anchor_kind = 2
         AND split_part(t.anchor_id, ':', 1) = 'resource'
         AND split_part(t.anchor_id, ':', 2) ~ '^[0-9]+$'
         AND split_part(t.anchor_id, ':', 2)::bigint IN (SELECT id FROM in_resource))
     OR (t.site = 'moyu' AND t.anchor_kind = 1 AND t.anchor_id ~ '^[0-9]+$'
         AND t.anchor_id::bigint IN (SELECT id FROM in_patch))
     OR (t.site = 'moyu' AND t.anchor_kind = 2 AND t.anchor_id ~ '^[0-9]+$'
         AND t.anchor_id::bigint IN (SELECT id FROM in_patch_resource)))
), chain AS (
  SELECT t.id, t.merged_into_id
  FROM community_thread t JOIN roots r ON r.id = t.id
  UNION
  SELECT t.id, t.merged_into_id
  FROM community_thread t JOIN chain c ON t.id = c.merged_into_id
)
SELECT DISTINCT id FROM chain;

CREATE TEMP TABLE keep_post (id bigint PRIMARY KEY);
INSERT INTO keep_post
SELECT id FROM community_post
WHERE thread_id IN (SELECT id FROM keep_thread) AND status = 0;

-- Private, moderation and outbox tables ship empty: notifications, per-user
-- read state and subscriptions, blocks, flags and the review queue, the purge
-- archive, the purge cursor, idempotency keys and undelivered events.
TRUNCATE community_notification, community_thread_user, community_anchor_user,
         community_feed_seen, community_activity_setting, community_user_block,
         community_flag, community_review_item, community_purge_archive,
         account_purge_cursor, community_write_request, community_event;

-- Children of posts first (soft refs, but keep the discipline), then posts,
-- then threads.
DELETE FROM community_reaction WHERE post_id NOT IN (SELECT id FROM keep_post);
DELETE FROM community_post     WHERE id NOT IN (SELECT id FROM keep_post);
DELETE FROM community_thread   WHERE id NOT IN (SELECT id FROM keep_thread);

DELETE FROM community_anchor_presentation p
WHERE NOT EXISTS (SELECT 1 FROM community_thread t
                  WHERE t.site = p.site AND t.anchor_kind = p.anchor_kind
                    AND t.anchor_id = p.anchor_id);

-- Last: the post / thread / anchor triggers project every change above into
-- these.
TRUNCATE community_activity, community_activity_group,
         community_activity_projection;

-- Platform uids still referenced by the kept rows. Exported for the
-- kun_galgame_infra prune; trust rows and follows shrink to the same set.
CREATE TEMP TABLE keep_user (id bigint PRIMARY KEY);
INSERT INTO keep_user
SELECT DISTINCT u FROM (
  SELECT author_id        FROM community_post
  UNION SELECT target_user_id FROM community_post
  UNION SELECT created_by     FROM community_thread
  UNION SELECT fb_responder_id FROM community_thread
  UNION SELECT user_id        FROM community_reaction
) s(u) WHERE u IS NOT NULL;

DELETE FROM community_trust WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM community_user_follow
WHERE follower_id NOT IN (SELECT id FROM keep_user)
   OR followee_id NOT IN (SELECT id FROM keep_user);

COMMIT;

\copy (SELECT id FROM keep_user ORDER BY id) TO 'kun_community_users.csv'
