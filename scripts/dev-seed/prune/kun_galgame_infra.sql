-- Prune kun_galgame_infra (platform accounts / OAuth / sites) to the users the
-- other seeds still reference. Runs LAST among the user-bearing DBs: it reads
-- the kept-user id lists exported by prune/kungalgame.sql (forum-local ids),
-- prune/kungalgame_patch.sql (patch-local ids) and prune/kun_community.sql
-- (platform uids), and maps the site-local ones through user_migrations.
--
-- Kept wholesale: sites, roles, first-party and listed oauth_clients, the shop
-- storefront, settings (all tiny, all needed for a working dev login).

-- CSV paths are CWD-relative: \copy does not interpolate psql variables, so
-- build-seed.sh runs this file with CWD = the shared export directory.

BEGIN;

CREATE TEMP TABLE in_kungal (id bigint PRIMARY KEY);
\copy in_kungal FROM 'kungalgame_users.csv'
CREATE TEMP TABLE in_moyu (id bigint PRIMARY KEY);
\copy in_moyu FROM 'kungalgame_patch_users.csv'
CREATE TEMP TABLE in_platform (id bigint PRIMARY KEY);
\copy in_platform FROM 'kun_community_users.csv'

-- Third-party apps nobody listed publicly are their developers' private
-- registrations.
DELETE FROM oauth_clients WHERE owner_user_id IS NOT NULL AND NOT listed;

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
  -- privileged accounts and app owners always ride along (admin console and
  -- developer portal need them)
  SELECT user_id FROM user_roles
  UNION SELECT user_id    FROM user_site_roles
  UNION SELECT granted_by FROM user_site_roles
  UNION SELECT created_by_user_id FROM sites
  UNION SELECT owner_user_id FROM oauth_clients
) s(u) WHERE u IS NOT NULL;

-- Plus a deterministic sample of the newest accounts.
INSERT INTO keep_user
SELECT id FROM users ORDER BY id DESC LIMIT :seed_users
ON CONFLICT DO NOTHING;

-- Credentials, tokens and keys: sessions, codes, resets, signing keys,
-- federated identities (external subject ids), developer API keys and usage.
-- creator_applications: its evidence/message columns are private user
-- submissions. Preferences and notices are private per-user state.
TRUNCATE sessions, password_resets, authorization_codes, signing_keys,
         oauth_accounts, developer_api_keys, developer_api_usage,
         creator_applications, user_preferences, platform_notices;

-- Points history, shop orders and store payouts / coupons / click stats.
-- The ledger ships empty on purpose: cmd/oauth's ledger.ImportLegacy opens
-- an opening_balance for every user whose users.moemoepoint it cannot
-- explain, so dev balances rebuild themselves from the kept users.moemoepoint.
TRUNCATE ledger_entries, ledger_transfers, ledger_accounts, moemoepoint_log;
TRUNCATE shop_orders, shop_entitlements, shop_loadouts, shop_codes,
         shop_restock_alerts;
TRUNCATE store_coupons, store_coupon_shares, store_coupon_batches,
         store_coupon_links, store_link_daily_stats, store_purchase_links;

-- FK children of users first, then users.
DELETE FROM user_follows    WHERE follower_id  NOT IN (SELECT id FROM keep_user)
                               OR following_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_roles      WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_migrations WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM user_site_data  WHERE user_id NOT IN (SELECT id FROM keep_user);
DELETE FROM users WHERE id NOT IN (SELECT id FROM keep_user);

-- refresh-dev-db guarantees these, but local use writes them again (an erased
-- account's address, locally created OAuth clients with their own secrets).
-- Pending account deletions and the content preference are private state.
UPDATE users SET email = 'user' || id || '@' || :'dev_email_domain'
  WHERE email NOT LIKE ('%@' || :'dev_email_domain');
UPDATE users SET original_email = 'user' || id || '@' || :'dev_email_domain'
  WHERE original_email <> '' AND original_email NOT LIKE ('%@' || :'dev_email_domain');
UPDATE users SET ip = '' WHERE ip <> '';
UPDATE users SET deletion_requested_at = NULL, deletion_due_at = NULL
  WHERE deletion_requested_at IS NOT NULL OR deletion_due_at IS NOT NULL;
UPDATE users SET nsfw_display = 'show' WHERE nsfw_display <> 'show';
UPDATE oauth_clients
  SET secret = 'sha256:' || encode(sha256(convert_to(:'dev_secret_prefix' || id, 'UTF8')), 'hex');

DELETE FROM job_run
  WHERE id NOT IN (SELECT id FROM job_run ORDER BY id DESC LIMIT 200);

COMMIT;

-- Kept avatars are public images: the kun_images prune keeps their rows.
\copy (SELECT DISTINCT avatar_image_hash FROM users WHERE avatar_image_hash <> '' ORDER BY 1) TO 'kun_galgame_infra_avatars.csv'
