-- kungalgame_patch scrub — server-side scratch DB only.
-- The `user` table (moyu) keeps only a last-seen IP as PII; auth lives in
-- kun_galgame_infra. Private chat / DM content is synthesised (裁定 1b). Public
-- resource listings (patch_resource: description/password/code) are preserved —
-- those are site content shown to logged-in users, not private user data.
--
-- Expected psql variables: marker  (e.g. -v marker="[dev-scrubbed]")

\set ON_ERROR_STOP on

BEGIN;

-- Last-seen IP ("user" is a reserved word → quoted).
UPDATE "user" SET ip = '' WHERE ip IS NOT NULL AND ip <> '';

-- Private one-to-one chat messages + their edit history.
UPDATE chat_message
  SET content = :'marker' || ' synthetic private chat message.'
  WHERE content IS NOT NULL AND content <> '';

UPDATE chat_message_edit_history
  SET previous_content = :'marker' || ' synthetic'
  WHERE previous_content IS NOT NULL AND previous_content <> '';

-- Station-internal direct messages (站内信).
UPDATE user_message
  SET content = :'marker' || ' synthetic direct message.'
  WHERE content IS NOT NULL AND content <> '';

-- Admin log: 'delete' entries embed the deleted account as JSON, email and
-- password hash included (found 2026-10-14 after the dev seed had shipped 38
-- such emails). The admin log page shows non-JSON content verbatim.
UPDATE admin_log
  SET content = :'marker' || ' synthetic admin log entry.'
  WHERE content IS NOT NULL AND content <> '';

COMMIT;
