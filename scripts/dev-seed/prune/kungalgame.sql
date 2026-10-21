-- ============================================================================
-- dev-seed prune: kungalgame (forum database)
--
-- Shrinks a full copy of the `kungalgame` forum DB down to a few hundred
-- entities with full FK closure, for the tiny collaborator seed dump.
--
-- Invocation (orchestrator):
--   psql -h localhost -p 5432 -U postgres -v ON_ERROR_STOP=1 -q \
--     -d kungalgame_seedbuild \
--     -v export_dir=<dir> -v seed_works=300 -v seed_topics=300 -v seed_users=400 \
--     -f prune/kungalgame.sql
--
-- Design notes:
--   * FK constraints stay enforced the whole time. Deletion order relies on
--     the schema's declared ON DELETE CASCADE for parent->child fans, and
--     deletes RESTRICT children (galgame_rating) explicitly before parents.
--   * The `user` table has NO incoming FK constraints in this schema: every
--     user reference (user_id / sender_id / author_id / ...) is a soft
--     reference. Kept-user closure is therefore computed here and the big
--     per-user tables are pruned against it; residual soft-ref orphans are
--     counted and reported at the end.
--   * Since G0 a forum galgame id IS the catalog work id, so ids no longer
--     encode recency: galgame roots are the newest published rows by
--     `created`. Topic ids are still forum-local, so topics stay newest-by-id.
-- ============================================================================

-- Fail early if a required variable was not passed.
\if :{?export_dir}
\else
  \echo 'FATAL: -v export_dir=... is required'
  \quit 1
\endif
\if :{?seed_works}
\else
  \echo 'FATAL: -v seed_works=... is required'
  \quit 1
\endif
\if :{?seed_topics}
\else
  \echo 'FATAL: -v seed_topics=... is required'
  \quit 1
\endif
\if :{?seed_users}
\else
  \echo 'FATAL: -v seed_users=... is required'
  \quit 1
\endif

-- ----------------------------------------------------------------------------
-- Section 0: snapshot before-counts of every public table (for the report)
-- ----------------------------------------------------------------------------
CREATE TEMP TABLE _seed_counts (phase text, tbl text, n bigint);

DO $$
DECLARE r record; c bigint;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
    EXECUTE format('SELECT count(*) FROM %I', r.tablename) INTO c;
    INSERT INTO _seed_counts VALUES ('before', r.tablename, c);
  END LOOP;
END $$;

-- ----------------------------------------------------------------------------
-- Section 1: root keep-sets — newest visible topics and newest galgames
-- ----------------------------------------------------------------------------
CREATE TEMP TABLE keep_topic AS
  SELECT id FROM topic
  WHERE status = 0 AND access_scope = 'public'
  ORDER BY id DESC LIMIT :seed_topics;
ALTER TABLE keep_topic ADD PRIMARY KEY (id);

CREATE TEMP TABLE keep_galgame AS
  SELECT id FROM galgame
  WHERE published
  ORDER BY created DESC, id DESC LIMIT :seed_works;
ALTER TABLE keep_galgame ADD PRIMARY KEY (id);

-- Quizzes: linked to galgames only through galgame_quiz_galgame.
-- Keep a quiz if it has no galgame links at all, or at least one link to a
-- kept galgame.
CREATE TEMP TABLE keep_quiz AS
  SELECT q.id FROM galgame_quiz q
  WHERE NOT EXISTS (SELECT 1 FROM galgame_quiz_galgame l WHERE l.quiz_id = q.id)
     OR EXISTS (SELECT 1 FROM galgame_quiz_galgame l
                JOIN keep_galgame kg ON kg.id = l.work_id
                WHERE l.quiz_id = q.id);
ALTER TABLE keep_quiz ADD PRIMARY KEY (id);

-- ----------------------------------------------------------------------------
-- Section 2: kept-user closure
--   participants of kept content
--   UNION staff-ish authors of small site-wide content (docs / update log /
--         todo / permission audit) — the `user` table itself carries no
--         role/status columns in this DB, so these stand in for admins
--   UNION authors of small sections kept wholesale (toolsets, websites, quizzes)
--   UNION the newest :seed_users users by id
-- Everything is intersected with the real user table at the end.
-- ----------------------------------------------------------------------------
CREATE TEMP TABLE keep_user AS
SELECT DISTINCT uid FROM (
  -- topic authors of kept topics
  SELECT t.user_id AS uid FROM topic t JOIN keep_topic k ON k.id = t.id
  UNION ALL
  -- visible reply / comment authors on kept topics
  SELECT r.user_id FROM topic_reply r JOIN keep_topic k ON k.id = r.topic_id
  WHERE r.status = 0
  UNION ALL
  SELECT c.user_id FROM topic_comment c JOIN keep_topic k ON k.id = c.topic_id
  WHERE c.status = 0
  UNION ALL
  -- galgame contributors on kept galgames: creator, wiki-edit actors,
  -- contributors, resource uploaders
  SELECT g.creator_user_id FROM galgame g JOIN keep_galgame k ON k.id = g.id
  WHERE g.creator_user_id IS NOT NULL
  UNION ALL
  SELECT a.user_id FROM galgame_activity a JOIN keep_galgame k ON k.id = a.work_id
  UNION ALL
  SELECT c.user_id FROM galgame_contributor c JOIN keep_galgame k ON k.id = c.work_id
  UNION ALL
  SELECT r.user_id FROM galgame_resource r JOIN keep_galgame k ON k.id = r.work_id
  UNION ALL
  -- staff-ish soft references from site-wide singletons (stand-in for admins)
  SELECT author_id FROM doc_article
  UNION ALL
  SELECT user_id FROM update_log
  UNION ALL
  SELECT user_id FROM todo
  UNION ALL
  SELECT claimed_user_id FROM todo
  UNION ALL
  SELECT updated_by FROM role_permission_override
  UNION ALL
  SELECT operator_id FROM permission_audit_log
  UNION ALL
  -- authors of small sections that are kept wholesale
  SELECT user_id FROM galgame_toolset
  UNION ALL
  SELECT user_id FROM galgame_toolset_resource
  UNION ALL
  SELECT user_id FROM galgame_toolset_contributor
  UNION ALL
  SELECT user_id FROM galgame_website
  UNION ALL
  SELECT q.user_id FROM galgame_quiz q JOIN keep_quiz k ON k.id = q.id
  UNION ALL
  -- newest N users by id
  SELECT id FROM (SELECT id FROM "user" ORDER BY id DESC LIMIT :seed_users) newest
) refs
WHERE uid IS NOT NULL
  AND EXISTS (SELECT 1 FROM "user" u WHERE u.id = refs.uid);
ALTER TABLE keep_user ADD PRIMARY KEY (uid);

-- ----------------------------------------------------------------------------
-- Section 3: private and ephemeral tables ship empty
-- ----------------------------------------------------------------------------
-- Private by nature, whoever they belong to: chats, notifications, drafts,
-- per-topic subscriptions, collections (the same folders catalog ships empty),
-- quiz answers (shown only to the answerer and the quiz author), lottery prize
-- codes, purge / merge archives, moderation bookkeeping.
TRUNCATE chat_message_reaction, chat_message_read_by, chat_message,
         chat_room_admin, chat_room_participant, chat_room;
TRUNCATE message, system_message_read_state, topic_subscription, topic_draft;
TRUNCATE galgame_quiz_answer;
TRUNCATE galgame_collection_item, galgame_collection_viewer, galgame_collection;
TRUNCATE topic_lottery_code;
TRUNCATE user_purge_archive, galgame_merge_discarded, _user_merge_109310_to_1922,
         trust_disposition_applied;
TRUNCATE toolset_upload, dlsite_store_link;

-- 2026-10-21 (forum 213): the merge-fold ledger snapshots resources, comments
-- and user rows as JSON.
DO $$
DECLARE
    present text;
BEGIN
    SELECT string_agg(format('public.%I', t), ', ') INTO present
    FROM unnest(ARRAY['galgame_merge_fold_row', 'galgame_merge_fold']) AS t
    WHERE to_regclass('public.' || t) IS NOT NULL;
    IF present IS NOT NULL THEN
        EXECUTE 'TRUNCATE TABLE ' || present;
    END IF;
END $$;

-- ----------------------------------------------------------------------------
-- Section 4: topic-side prune
-- ----------------------------------------------------------------------------
-- Dropping a topic cascades to: topic_reply (and its likes/dislikes/reactions
-- via reply FK), topic_comment (+likes), topic_like/dislike/favorite/upvote/
-- reaction, topic_poll (+options/votes), topic_lottery (+prizes/entries),
-- topic_section_relation, topic_access_grant.
DELETE FROM topic WHERE id NOT IN (SELECT id FROM keep_topic);

-- Hidden replies / comments are removed content.
DELETE FROM topic_reply   WHERE status <> 0;
DELETE FROM topic_comment WHERE status <> 0;

-- Engagement rows on kept topics: restrict to kept users.
DELETE FROM topic_reaction WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_like     WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_dislike  WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_upvote   WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_favorite WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_reply_like     WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_reply_dislike  WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_reply_reaction WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_comment_like   WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_poll_vote      WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM topic_poll_vote v USING topic_poll p
WHERE p.id = v.poll_id AND p.is_anonymous;
DELETE FROM topic_lottery_entry  WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- ----------------------------------------------------------------------------
-- Section 5: galgame-side prune
-- ----------------------------------------------------------------------------
-- galgame_rating references galgame with ON DELETE RESTRICT: remove ratings
-- of dropped galgames first (cascades rating likes), and restrict ratings on
-- kept galgames to kept users.
DELETE FROM galgame_rating
WHERE work_id NOT IN (SELECT id FROM keep_galgame)
   OR user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_rating_like WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- Dropping a galgame cascades to: galgame_like, galgame_favorite,
-- galgame_resource (+link/provider/like).
DELETE FROM galgame WHERE id NOT IN (SELECT id FROM keep_galgame);

DELETE FROM galgame_like          WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_favorite      WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_resource_like WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- Soft work refs (no FK) of dropped galgames.
DELETE FROM galgame_activity      WHERE work_id NOT IN (SELECT id FROM keep_galgame);
DELETE FROM galgame_contributor   WHERE work_id NOT IN (SELECT id FROM keep_galgame);
DELETE FROM galgame_renumber_2026 WHERE new_id  NOT IN (SELECT id FROM keep_galgame);
DELETE FROM galgame_resource_relocation
WHERE work_id NOT IN (SELECT id FROM keep_galgame);

-- Community-thread migration maps carry soft refs only. Rating threads do not
-- survive the kun_community prune, so their map rows go too.
DELETE FROM galgame_comment_community_map
WHERE work_id NOT IN (SELECT id FROM keep_galgame);
DELETE FROM resource_comment_community_map WHERE source = 'rating';

-- Likes on community posts about galgames (post_id lives in the community DB).
DELETE FROM galgame_post_like WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- ----------------------------------------------------------------------------
-- Section 6: quizzes / toolsets / websites (small sections)
-- ----------------------------------------------------------------------------
-- Quiz-galgame links to dropped galgames, then quizzes that lost every link
-- (cascades favorites / links).
DELETE FROM galgame_quiz_galgame WHERE work_id NOT IN (SELECT id FROM keep_galgame);
DELETE FROM galgame_quiz WHERE id NOT IN (SELECT id FROM keep_quiz);
DELETE FROM galgame_quiz_favorite WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_quiz_view_daily WHERE entity_id NOT IN (SELECT id FROM keep_quiz);

-- Toolsets and websites are kept wholesale (their authors joined keep_user);
-- restrict per-user engagement rows to kept users.
DELETE FROM galgame_toolset_practicality WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_website_like     WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM galgame_website_favorite WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- ----------------------------------------------------------------------------
-- Section 7: feed, per-user state
-- ----------------------------------------------------------------------------
-- feed_activity: rows by kept users about kept content, capped at 5,000
-- newest. work_id and source_id are soft refs whose target depends on
-- `type`; rows about content that did not survive are dropped.
DELETE FROM feed_activity WHERE user_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM feed_activity
WHERE work_id <> 0 AND work_id NOT IN (SELECT id FROM keep_galgame);
DELETE FROM feed_activity
WHERE (type IN ('TOPIC_CREATION', 'TOPIC_UPVOTE')
         AND source_id NOT IN (SELECT id FROM keep_topic))
   OR (type = 'TOPIC_REPLY_CREATION'
         AND source_id NOT IN (SELECT id FROM topic_reply))
   OR (type = 'TOPIC_COMMENT_CREATION'
         AND source_id NOT IN (SELECT id FROM topic_comment))
   OR (type = 'GALGAME_QUIZ_CREATION'
         AND source_id NOT IN (SELECT id FROM keep_quiz));
DELETE FROM feed_activity
WHERE id NOT IN (SELECT id FROM feed_activity ORDER BY id DESC LIMIT 5000);

-- The forum updates kungal_user_state rows in place, so kept users keep
-- theirs; the muted-notification preference is reset to its default.
DELETE FROM kungal_user_state WHERE user_id NOT IN (SELECT uid FROM keep_user);
UPDATE kungal_user_state SET muted_notification_types = '[]'::jsonb;

-- Social graph: both endpoints must be kept.
DELETE FROM user_follow
WHERE follower_id NOT IN (SELECT uid FROM keep_user)
   OR followed_id NOT IN (SELECT uid FROM keep_user);
DELETE FROM user_friend
WHERE user_id   NOT IN (SELECT uid FROM keep_user)
   OR friend_id NOT IN (SELECT uid FROM keep_user);

DELETE FROM user_permission_override WHERE user_id NOT IN (SELECT uid FROM keep_user);

-- ----------------------------------------------------------------------------
-- Section 8: users (no incoming FK constraints — soft refs only)
-- ----------------------------------------------------------------------------
DELETE FROM "user" WHERE id NOT IN (SELECT uid FROM keep_user);

-- ----------------------------------------------------------------------------
-- Section 9: analytics tables and outbox queues
-- ----------------------------------------------------------------------------
TRUNCATE galgame_view_daily;
TRUNCATE topic_view_daily;
-- Last: the feed / anchor triggers enqueue a row for every delete above.
TRUNCATE activity_push_queue, activity_push_sent,
         anchor_presentation_queue, anchor_presentation_sent;

-- ----------------------------------------------------------------------------
-- Section 10: report — before/after counts, hard bar, orphan spot-checks
-- ----------------------------------------------------------------------------
DO $$
DECLARE r record; c bigint;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
    EXECUTE format('SELECT count(*) FROM %I', r.tablename) INTO c;
    INSERT INTO _seed_counts VALUES ('after', r.tablename, c);
  END LOOP;
END $$;

\echo '=== kungalgame prune: before -> after (changed tables) ==='
SELECT b.tbl, b.n AS before, a.n AS after
FROM _seed_counts b
JOIN _seed_counts a ON a.tbl = b.tbl AND a.phase = 'after'
WHERE b.phase = 'before' AND b.n <> a.n
ORDER BY b.n - a.n DESC;

\echo '=== hard bar check: tables still over 20,000 rows (must be empty) ==='
SELECT tbl, n FROM _seed_counts WHERE phase = 'after' AND n > 20000;

-- Fail the run loudly if any table busts the row cap.
DO $$
DECLARE bad int;
BEGIN
  SELECT count(*) INTO bad FROM _seed_counts WHERE phase = 'after' AND n > 20000;
  IF bad > 0 THEN
    RAISE EXCEPTION 'seed prune failed: % table(s) exceed 20,000 rows', bad;
  END IF;
END $$;

\echo '=== soft-reference orphan spot-checks (user refs without FK) ==='
SELECT 'topic_comment.target_user_id' AS ref, count(*) AS orphans
  FROM topic_comment c
  WHERE target_user_id IS NOT NULL AND target_user_id <> 0
    AND NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = c.target_user_id)
UNION ALL
SELECT 'system_message.user_id', count(*)
  FROM system_message m
  WHERE user_id IS NOT NULL AND user_id <> 0
    AND NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = m.user_id)
UNION ALL
SELECT 'topic.best_answer_id (cross-check)', count(*)
  FROM topic t
  WHERE best_answer_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM topic_reply r WHERE r.id = t.best_answer_id);

-- ----------------------------------------------------------------------------
-- Section 11: export kept ids for the downstream platform/community prunes
-- ----------------------------------------------------------------------------
-- NOTE: \copy passes its arguments raw and performs NO psql variable
-- interpolation (verified), so the files are written via \o redirection +
-- server-side COPY TO STDOUT, where :var interpolation does work (verified).
\set f_users     :export_dir '/kungalgame_users.csv'
\set f_topics    :export_dir '/kungalgame_topics.csv'
\set f_galgames  :export_dir '/kungalgame_galgames.csv'
\set f_resources :export_dir '/kungalgame_resources.csv'

\o :f_users
COPY (SELECT id FROM "user" ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\o :f_topics
COPY (SELECT id FROM topic ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\o :f_galgames
COPY (SELECT id FROM galgame ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\o :f_resources
COPY (SELECT id FROM galgame_resource ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o

\echo '=== export done ==='
\echo 'users     -> ' :f_users
\echo 'topics    -> ' :f_topics
\echo 'galgames  -> ' :f_galgames
\echo 'resources -> ' :f_resources

ANALYZE;
