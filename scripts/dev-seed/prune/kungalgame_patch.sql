-- =============================================================================
-- dev-seed prune: kungalgame_patch (moyu.moe patch site)
-- =============================================================================
-- Shrinks a full scratch copy (TEMPLATE'd from the desensitised prod snapshot)
-- down to the newest :seed_works published patches plus full FK closure, for
-- a tiny collaborator seed dump.
--
-- Invocation (orchestrator):
--   psql -h localhost -p 5432 -U postgres -v ON_ERROR_STOP=1 -q \
--     -d kungalgame_patch_seedbuild \
--     -v export_dir=<dir> -v seed_works=300 -v seed_topics=300 -v seed_users=400 \
--     -f prune/kungalgame_patch.sql
--
-- Variables used: :seed_works, :seed_users, :export_dir.
-- :seed_topics is accepted but intentionally unused — this database has no
-- topic concept (forum topics live in the platform DB).
--
-- Technique: FK constraints stay enforced. Keep-sets are materialised in TEMP
-- tables, then children are deleted before parents; any closure mistake fails
-- loudly as an FK violation. Fully deterministic (ties broken by id).
--
-- Schema notes:
--   * The desensitised "user" table carries NO role / name / email columns,
--     so "admins & moderators" are approximated by the distinct actors of
--     admin_log.
--   * Since G0 a patch id IS the catalog work id, so ids no longer encode
--     recency: roots are the newest published patches by `created`.
--   * Soft (non-FK) reference columns: patch.creator_id, doc.author_uid,
--     doc.user_id, claim_event_processed.actor_uid / work_id,
--     patch_resource_revision.actor_id, patch_resource_file_history.actor_id.
--     Orphan counts for these are reported at the end.
--   * Left untouched (small config / vocab / ledger tables): doc, cron_state,
--     site_setting, _migrations, wiki_message_processed, claim_event_processed.
-- =============================================================================

BEGIN;

-- -----------------------------------------------------------------------------
-- Section 0: row counts before pruning (for the change report at the end)
-- -----------------------------------------------------------------------------
CREATE TEMP TABLE _rows_before (tbl text PRIMARY KEY, n bigint NOT NULL);

DO $$
DECLARE t text;
BEGIN
  FOR t IN
    SELECT c.relname FROM pg_class c
    JOIN pg_namespace ns ON ns.oid = c.relnamespace
    WHERE ns.nspname = 'public' AND c.relkind = 'r'
  LOOP
    EXECUTE format(
      'INSERT INTO _rows_before SELECT %L, count(*) FROM %I', t, t);
  END LOOP;
END $$;

-- -----------------------------------------------------------------------------
-- Section 1: keep-sets
-- -----------------------------------------------------------------------------

CREATE TEMP TABLE keep_patch (id int PRIMARY KEY);
INSERT INTO keep_patch
SELECT id FROM patch WHERE published ORDER BY created DESC, id DESC LIMIT :seed_works;

-- Visible resources of kept patches (1 = disabled by its owner, 2 = hidden
-- by moderation).
CREATE TEMP TABLE keep_resource (id int PRIMARY KEY);
INSERT INTO keep_resource
SELECT r.id FROM patch_resource r JOIN keep_patch kp ON kp.id = r.galgame_id
WHERE r.status = 0;

-- All comments on kept patches (whole threads: parent chains never cross
-- patches, so filtering by galgame_id keeps every ancestor of a kept comment),
-- minus comments on dropped resources, which cascade away with them.
CREATE TEMP TABLE keep_comment (id int PRIMARY KEY);
INSERT INTO keep_comment
SELECT c.id FROM patch_comment c JOIN keep_patch kp ON kp.id = c.galgame_id
WHERE c.resource_id IS NULL
   OR c.resource_id IN (SELECT id FROM keep_resource);

-- Users to keep:
--   (a) authors of kept patches (patch.user_id, hard FK) and their soft
--       creator_id when it still resolves to a real user;
--   (b) uploaders of kept resources;
--   (c) authors of comments on kept patches;
--   (d) admin_log actors (role columns do not exist in this desensitised
--       snapshot; acting-in-admin_log is the closest admin/moderator signal);
--   (e) the newest :seed_users users by id.
CREATE TEMP TABLE keep_user (id int PRIMARY KEY);
INSERT INTO keep_user
SELECT DISTINCT id FROM (
  SELECT p.user_id       AS id FROM patch p JOIN keep_patch kp ON kp.id = p.id
  UNION
  SELECT p.creator_id    AS id FROM patch p JOIN keep_patch kp ON kp.id = p.id
  WHERE p.creator_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM "user" u WHERE u.id = p.creator_id)
  UNION
  SELECT r.user_id       AS id FROM patch_resource r
  JOIN keep_resource kr ON kr.id = r.id
  UNION
  SELECT c.user_id       AS id FROM patch_comment c
  JOIN keep_comment kc ON kc.id = c.id
  UNION
  SELECT DISTINCT user_id AS id FROM admin_log
  UNION
  -- Parenthesised subselect: a bare ORDER BY/LIMIT here would bind to the
  -- whole UNION and silently cap the entire keep set.
  SELECT id FROM
    (SELECT id FROM "user" ORDER BY id DESC LIMIT :seed_users) newest
) s;

ANALYZE keep_patch; ANALYZE keep_resource; ANALYZE keep_comment;
ANALYZE keep_user;

-- -----------------------------------------------------------------------------
-- Section 2: private and ephemeral tables ship empty
-- -----------------------------------------------------------------------------
-- Chats and user_message (DMs + notifications) are private by nature.
TRUNCATE chat_message_seen, chat_message_reaction, chat_message_edit_history,
         chat_message, chat_member, chat_room;
TRUNCATE user_message;
-- 2026-10-14: admin_log 'delete' entries embed the deleted account as JSON,
-- email and password hash included, and the snapshot scrub did not touch the
-- table; the 2026-08-07 seed shipped 38 such emails in a public release.
TRUNCATE admin_log;
TRUNCATE trust_disposition_applied, trust_subject_watermark, dlsite_store_link;

-- 2026-10-21 (moyu 052): merge records snapshot resources, including
-- moderation-hidden ones with their links, and comments as JSON.
DO $$
DECLARE
    present text;
BEGIN
    SELECT string_agg(format('public.%I', t), ', ') INTO present
    FROM unnest(ARRAY['patch_merge_record_row', 'patch_merge_record']) AS t
    WHERE to_regclass('public.' || t) IS NOT NULL;
    IF present IS NOT NULL THEN
        EXECUTE 'TRUNCATE TABLE ' || present;
    END IF;
END $$;

-- refresh-dev-db clears user.ip, but local logins write it again.
UPDATE "user" SET ip = '' WHERE ip <> '';

-- -----------------------------------------------------------------------------
-- Section 3: deletes, children before parents
-- -----------------------------------------------------------------------------

-- 3.1 comment subtree ---------------------------------------------------------
DELETE FROM user_patch_comment_like_relation l
WHERE NOT EXISTS (SELECT 1 FROM keep_comment kc WHERE kc.id = l.comment_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = l.user_id);

DELETE FROM patch_comment c
WHERE NOT EXISTS (SELECT 1 FROM keep_comment kc WHERE kc.id = c.id);

-- 3.2 resource subtree --------------------------------------------------------
DELETE FROM user_patch_resource_like_relation l
WHERE NOT EXISTS (SELECT 1 FROM keep_resource kr WHERE kr.id = l.resource_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = l.user_id);

DELETE FROM user_patch_resource_favorite_relation f
WHERE NOT EXISTS (SELECT 1 FROM keep_resource kr WHERE kr.id = f.resource_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = f.user_id);

DELETE FROM patch_resource_revision v
WHERE NOT EXISTS (SELECT 1 FROM keep_resource kr WHERE kr.id = v.resource_id);

DELETE FROM patch_resource_file_history h
WHERE NOT EXISTS (SELECT 1 FROM keep_resource kr WHERE kr.id = h.resource_id);

DELETE FROM patch_resource r
WHERE NOT EXISTS (SELECT 1 FROM keep_resource kr WHERE kr.id = r.id);

-- 3.3 remaining patch children ------------------------------------------------
DELETE FROM patch_link l
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = l.galgame_id);

-- Soft refs (no FK): community-thread migration map and id redirects.
DELETE FROM patch_comment_community_map m
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = m.galgame_id);
DELETE FROM patch_redirect r
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = r.new_id);

DELETE FROM user_patch_favorite_relation f
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = f.galgame_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = f.user_id);

DELETE FROM user_patch_contribute_relation c
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = c.galgame_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = c.user_id);

-- 3.4 user children -----------------------------------------------------------
DELETE FROM user_follow_relation f
WHERE NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = f.follower_id)
   OR NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = f.following_id);

-- 3.5 parents -----------------------------------------------------------------
-- patch.user_id is ON DELETE RESTRICT: a mistake in keep_user would abort here.
DELETE FROM patch p
WHERE NOT EXISTS (SELECT 1 FROM keep_patch kp WHERE kp.id = p.id);

DELETE FROM "user" u
WHERE NOT EXISTS (SELECT 1 FROM keep_user ku WHERE ku.id = u.id);

-- -----------------------------------------------------------------------------
-- Section 4: acceptance gate — no table may exceed 10,000 rows
-- -----------------------------------------------------------------------------
DO $$
DECLARE t text; c bigint;
BEGIN
  FOR t IN
    SELECT cl.relname FROM pg_class cl
    JOIN pg_namespace ns ON ns.oid = cl.relnamespace
    WHERE ns.nspname = 'public' AND cl.relkind = 'r'
  LOOP
    EXECUTE format('SELECT count(*) FROM %I', t) INTO c;
    IF c > 10000 THEN
      RAISE EXCEPTION 'acceptance gate failed: table % still has % rows (max 10000)', t, c;
    END IF;
  END LOOP;
END $$;

-- -----------------------------------------------------------------------------
-- Section 5: change report (before -> after, changed tables only)
-- -----------------------------------------------------------------------------
CREATE TEMP TABLE _rows_after (tbl text PRIMARY KEY, n bigint NOT NULL);

DO $$
DECLARE t text;
BEGIN
  FOR t IN
    SELECT cl.relname FROM pg_class cl
    JOIN pg_namespace ns ON ns.oid = cl.relnamespace
    WHERE ns.nspname = 'public' AND cl.relkind = 'r'
  LOOP
    EXECUTE format('INSERT INTO _rows_after SELECT %L, count(*) FROM %I', t, t);
  END LOOP;
END $$;

\echo '=== prune report: rows before -> after (changed tables only) ==='
SELECT b.tbl, b.n AS before, a.n AS after
FROM _rows_before b
JOIN _rows_after a USING (tbl)
WHERE b.n <> a.n
ORDER BY b.tbl;

\echo '=== soft-reference orphan counts (no enforced FK; informational) ==='
SELECT 'patch.creator_id -> user' AS soft_ref, count(*) AS orphans
FROM patch p
WHERE p.creator_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = p.creator_id)
UNION ALL
SELECT 'doc.author_uid -> user', count(*)
FROM doc d
WHERE NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = d.author_uid)
UNION ALL
SELECT 'doc.user_id -> user', count(*)
FROM doc d
WHERE d.user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = d.user_id)
UNION ALL
SELECT 'claim_event_processed.actor_uid -> user', count(*)
FROM claim_event_processed e
WHERE NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = e.actor_uid)
UNION ALL
SELECT 'claim_event_processed.work_id -> patch', count(*)
FROM claim_event_processed e
WHERE NOT EXISTS (SELECT 1 FROM patch p WHERE p.id = e.work_id)
UNION ALL
SELECT 'patch_resource_revision.actor_id -> user', count(*)
FROM patch_resource_revision v
WHERE NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = v.actor_id)
UNION ALL
SELECT 'patch_resource_file_history.actor_id -> user', count(*)
FROM patch_resource_file_history h
WHERE NOT EXISTS (SELECT 1 FROM "user" u WHERE u.id = h.actor_id);

COMMIT;

-- -----------------------------------------------------------------------------
-- Section 6: export kept user ids for the platform DB prune
-- -----------------------------------------------------------------------------
-- One id per line, no header. The platform prune script consumes this file.
-- Note: psql's \copy does NOT interpolate variables in its argument (verified
-- on psql 18), so redirect COPY TO STDOUT through \o instead — \o does
-- interpolate its filename argument.
\set filepath :export_dir '/kungalgame_patch_users.csv'
\o :filepath
COPY (SELECT id FROM "user" ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\echo '=== exported kept user ids ==='
\echo :filepath

-- Kept patch ids too: patch.id IS the catalog work id, so the catalog prune
-- must keep these works or every moyu card's catalog hydration misses in the
-- seed.
\set filepath :export_dir '/kungalgame_patch_patches.csv'
\o :filepath
COPY (SELECT id FROM patch ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\echo '=== exported kept patch ids ==='
\echo :filepath

-- Kept resource ids: moyu's resource comment threads in kun_community anchor
-- on them.
\set filepath :export_dir '/kungalgame_patch_resources.csv'
\o :filepath
COPY (SELECT id FROM patch_resource ORDER BY id) TO STDOUT WITH (FORMAT csv);
\o
\echo '=== exported kept resource ids ==='
\echo :filepath
