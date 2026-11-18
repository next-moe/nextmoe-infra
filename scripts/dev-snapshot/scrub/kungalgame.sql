-- kungalgame scrub — server-side scratch DB only.
-- The kungalgame `user` table is now just id + timestamps (auth migrated to
-- kun_galgame_infra), so it carries no user PII. Only private chat / DM content
-- is synthesised (裁定 1b). Public content (topics/replies/comments/resources) is
-- preserved verbatim.
--
-- Expected psql variables: marker  (e.g. -v marker="[dev-scrubbed]")

\set ON_ERROR_STOP on

BEGIN;

-- Private one-to-one chat messages.
UPDATE chat_message
  SET content = :'marker' || ' synthetic private chat message.'
  WHERE content IS NOT NULL AND content <> '';

-- Chat-room last-message preview (private snippet).
UPDATE chat_room
  SET last_message_content = :'marker' || ' synthetic'
  WHERE last_message_content IS NOT NULL AND last_message_content <> '';

-- Station-internal direct messages (站内信).
UPDATE message
  SET content = :'marker' || ' synthetic direct message.'
  WHERE content IS NOT NULL AND content <> '';

-- What the user purge took, kept so a mistaken purge can be undone: deleted
-- content, including chat and message rows as JSON the updates above never
-- reach.
TRUNCATE user_purge_archive;

-- The forum's own config store (kungal migration 230_config_store) holds prod
-- secrets sealed with the forum's prod KUN_CONFIG_KEK. Guarded because a
-- snapshot can predate the tables.
DO $$
BEGIN
  IF to_regclass('config_secret') IS NOT NULL THEN
    TRUNCATE config_secret, config_secret_audit;
  END IF;
END $$;

COMMIT;
