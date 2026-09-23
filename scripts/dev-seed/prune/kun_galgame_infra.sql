-- Prune kun_galgame_infra (platform accounts / OAuth / sites) to the users the
-- other seeds still reference. Runs LAST among the user-bearing DBs: it reads
-- the kept-user id lists exported by prune/kungalgame.sql (forum-local ids),
-- prune/kungalgame_patch.sql (patch-local ids) and prune/kun_community.sql
-- (platform uids), and maps the site-local ones through user_migrations.
--
-- Kept wholesale: sites, oauth_clients, roles, signing_keys, developer_api_*
-- (all tiny, all needed for a working dev login). Ephemeral credential tables
-- (sessions / password_resets / authorization_codes) are emptied: a seed has
-- no business carrying tokens, scrubbed or not.

-- CSV paths are CWD-relative: \copy does not interpolate psql variables, so
-- build-seed.sh runs this file with CWD = the shared export directory.

BEGIN;

CREATE TEMP TABLE in_kungal (id bigint PRIMARY KEY);
\copy in_kungal FROM 'kungalgame_users.csv'
CREATE TEMP TABLE in_moyu (id bigint PRIMARY KEY);
\copy in_moyu FROM 'kungalgame_patch_users.csv'
CREATE TEMP TABLE in_platform (id bigint PRIMARY KEY);
\copy in_platform FROM 'kun_community_users.csv'

CREATE TEMP TABLE keep_user (id bigint PRIMARY KEY);
INSERT INTO keep_user
SELECT DISTINCT u FROM (
  -- site-local ids mapped to platform accounts
  SELECT user_id FROM user_migrations
    WHERE source_db = 'kungal' AND source_user_id IN (SELECT id FROM in_kungal)
  UNION
  SELECT user_id FROM user_migrations
    WHERE source_db = 'moyu' AND source_user_id IN (SELECT id FROM in_moyu)
  UNION
  -- post-migration site accounts carry the platform uid directly as their
  -- site-local id and have NO user_migrations row — resolve those verbatim
  SELECT u.id FROM users u
    WHERE u.id IN (SELECT id FROM in_kungal)
      AND NOT EXISTS (SELECT 1 FROM user_migrations um
                      WHERE um.source_db = 'kungal' AND um.source_user_id = u.id)
  UNION
  SELECT u.id FROM users u
    WHERE u.id IN (SELECT id FROM in_moyu)
      AND NOT EXISTS (SELECT 1 FROM user_migrations um
                      WHERE um.source_db = 'moyu' AND um.source_user_id = u.id)
  UNION
  -- community exports platform uids directly; guard against stray ids
  SELECT id FROM users WHERE id IN (SELECT id FROM in_platform)
  UNION
  -- privileged accounts always ride along (admin console needs them)
  SELECT user_id FROM user_roles
  UNION SELECT user_id    FROM user_site_roles
  UNION SELECT granted_by FROM user_site_roles
  UNION SELECT created_by_user_id FROM sites
) s(u) WHERE u IS NOT NULL;

-- Plus a deterministic sample of the newest accounts.
INSERT INTO keep_user
SELECT id FROM users ORDER BY id DESC LIMIT :seed_users
ON CONFLICT DO NOTHING;

-- Ephemeral credential tables: schema only. creator_applications joins them:
-- its evidence/message columns are private user submissions the snapshot
-- scrub does not touch, so a redistributable seed must not carry them.
TRUNCATE sessions, password_resets, authorization_codes, creator_applications;

-- FK children of users first, then users.
DELETE FROM oauth_accounts  WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_follows    WHERE follower_id  NOT IN (SELECT id FROM keep_user)
                               OR following_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_roles      WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_migrations WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_site_data  WHERE user_id NOT IN (SELECT id FROM keep_user);
-- Soft references (no FK) pruned to the same set.
DELETE FROM moemoepoint_log
  WHERE user_id NOT IN (SELECT id FROM keep_user)
     OR (actor_user_id IS NOT NULL AND actor_user_id NOT IN (SELECT id FROM keep_user));
-- The ledger is pruned by whole transfers, never by entries: a transfer that
-- loses one leg no longer balances, and the system accounts are re-derived from
-- what is left. It is not capped like the log above for the same reason.
DO $$
BEGIN
  IF to_regclass('ledger_transfers') IS NULL THEN
    RETURN;
  END IF;
  CREATE TEMP TABLE drop_transfer ON COMMIT DROP AS
    SELECT DISTINCT e.transfer_id AS id FROM ledger_entries e
    JOIN ledger_accounts a ON a.id = e.account_id
    WHERE a.kind = 'user' AND a.user_id NOT IN (SELECT id FROM keep_user)
    UNION
    SELECT id FROM ledger_transfers
    WHERE actor_user_id <> 0 AND actor_user_id NOT IN (SELECT id FROM keep_user);
  DELETE FROM ledger_entries WHERE transfer_id IN (SELECT id FROM drop_transfer);
  DELETE FROM ledger_transfers WHERE id IN (SELECT id FROM drop_transfer);
  DELETE FROM ledger_accounts WHERE kind = 'user' AND user_id NOT IN (SELECT id FROM keep_user);
  UPDATE ledger_accounts a
    SET balance = COALESCE((SELECT SUM(e.amount) FROM ledger_entries e WHERE e.account_id = a.id), 0);
  UPDATE users u SET moemoepoint = a.balance
    FROM ledger_accounts a
    WHERE a.kind = 'user' AND a.asset = 'moemoepoint' AND a.code = '' AND a.user_id = u.id;
END $$;
DO $$
BEGIN
  IF to_regclass('shop_orders') IS NULL THEN
    RETURN;
  END IF;
  DELETE FROM shop_loadouts     WHERE user_id NOT IN (SELECT id FROM keep_user);
  DELETE FROM shop_entitlements WHERE user_id NOT IN (SELECT id FROM keep_user);
  DELETE FROM shop_orders       WHERE user_id NOT IN (SELECT id FROM keep_user);
END $$;
DELETE FROM users WHERE id NOT IN (SELECT id FROM keep_user);

-- Caps on log-ish tables that survive per-user filtering.
DELETE FROM moemoepoint_log
  WHERE id NOT IN (SELECT id FROM moemoepoint_log ORDER BY id DESC LIMIT 5000);
DELETE FROM job_run
  WHERE id NOT IN (SELECT id FROM job_run ORDER BY id DESC LIMIT 200);

COMMIT;
