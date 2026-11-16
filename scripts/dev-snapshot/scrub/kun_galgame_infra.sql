-- kun_galgame_infra scrub — runs ONLY on the server-side scratch database
-- (dev_snapshot_scratch_kun_galgame_infra), never against the production DB.
--
-- Expected psql variables (build-snapshot.sh passes them with -v):
--   dev_argon2    argon2("kungal-dev")     for users.password — the auth service
--                                          verifies this column with argon2 ONLY
--   dev_bcrypt    bcrypt("kungal-dev")     for the LEGACY kungal_password /
--                                          moyu_password columns (bcrypt verify)
--   email_domain  dev email domain         e.g. -v email_domain=dev.local
--   marker        synthetic-text marker    e.g. -v marker="[dev-scrubbed]"
--
-- OAuth client secrets are NOT derived here — build-snapshot.sh sets them in
-- shell (sha256, no pgcrypto) so the artifact schema stays pristine.

\set ON_ERROR_STOP on

BEGIN;

-- Users: emails, password hashes, last-seen IP. avatar / name / bio are public.
UPDATE users SET
  email          = 'user' || id || '@' || :'email_domain',
  original_email = CASE
                     WHEN original_email IS NOT NULL AND original_email <> ''
                       THEN 'user' || id || '@' || :'email_domain'
                     ELSE original_email
                   END,
  password       = :'dev_argon2',
  kungal_password = CASE
                      WHEN kungal_password IS NOT NULL AND kungal_password <> ''
                        THEN :'dev_bcrypt'
                      ELSE kungal_password
                    END,
  moyu_password  = CASE
                     WHEN moyu_password IS NOT NULL AND moyu_password <> ''
                       THEN :'dev_bcrypt'
                     ELSE moyu_password
                   END,
  ip             = '';

-- Migration provenance carried the source-site email of every migrated user.
UPDATE user_migrations
  SET source_email = 'user' || user_id || '@' || :'email_domain'
  WHERE source_email IS NOT NULL AND source_email <> '';

-- Third-party linked-account OAuth tokens (Google/GitHub etc.). 0 rows today,
-- but the wipe is part of the contract.
UPDATE oauth_accounts SET access_token = '', refresh_token = '';

-- Creator applications: user-submitted free text may carry contact PII.
UPDATE creator_applications SET
  message        = CASE
                     WHEN message IS NOT NULL AND message <> ''
                       THEN :'marker' || ' synthetic creator application message.'
                     ELSE message
                   END,
  evidence       = '[]'::jsonb,
  decline_reason = CASE
                     WHEN decline_reason IS NOT NULL AND decline_reason <> ''
                       THEN :'marker' || ' synthetic decline reason.'
                     ELSE decline_reason
                   END;

-- Token / secret / code tables: emptied wholesale (裁定 1d out-bound residue too).
-- signing_keys: dev runs HS256 (KUN_OIDC_KEY_ENC_KEY empty in docker-compose.dev.yml),
-- and would self-bootstrap fresh ES256/RS256 keys if a KEK were set — so an empty
-- table is both correct and avoids shipping prod key material under a foreign KEK.
TRUNCATE sessions;
TRUNCATE authorization_codes;
TRUNCATE password_resets;
TRUNCATE signing_keys;

-- The config store holds prod secrets sealed with the prod KUN_CONFIG_KEK; the
-- snapshot boot reports name prod hosts. A dev box has neither key nor hosts.
-- Guarded because a snapshot can predate the tables.
DO $$
BEGIN
  IF to_regclass('config_secrets') IS NOT NULL THEN
    TRUNCATE config_secrets, config_secret_audit_logs, config_boot_snapshots;
  END IF;
END $$;

-- OAuth first-party dev callbacks: ensure each product repo's localhost callback
-- is present (idempotent union+dedup). forum/patch/wiki already carry these in
-- prod; this guarantees presence regardless. letmoe's dev client is provisioned
-- by letmoe's own seed (its client_id is not in prod), see docs.
UPDATE oauth_clients c SET redirect_uris = (
  SELECT jsonb_agg(DISTINCT e) FROM (
    SELECT jsonb_array_elements_text(c.redirect_uris) AS e
    UNION SELECT 'http://127.0.0.1:2333/auth/callback'
  ) t
) WHERE c.id = '4ed9bc99ec0a789a4796b83e22bd84c5';

UPDATE oauth_clients c SET redirect_uris = (
  SELECT jsonb_agg(DISTINCT e) FROM (
    SELECT jsonb_array_elements_text(c.redirect_uris) AS e
    UNION SELECT 'http://127.0.0.1:6969/auth/callback'
  ) t
) WHERE c.id = 'df3ff6008d740bfacbe46aa8cf483cf2';


COMMIT;
