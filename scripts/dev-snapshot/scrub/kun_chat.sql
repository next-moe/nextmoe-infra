-- kun_chat scrub — server-side scratch DB only.
-- Every conversation is private: message text, formatting, quotes, drafts,
-- cross-site context cards and report snapshots are synthesised (裁定 1b).
-- Structure (who talked to whom, seq, read positions, reactions) is kept so
-- the dev copy exercises the same shapes as production.
--
-- Expected psql variables: marker  (e.g. -v marker="[dev-scrubbed]")

\set ON_ERROR_STOP on

BEGIN;

-- Live messages only: a deleted message is already empty, and the schema
-- refuses text on it.
UPDATE chat_message
  SET text = :'marker' || ' synthetic chat message.', entities = NULL,
      reply_quote = NULL, context = NULL
  WHERE deleted_at IS NULL AND kind = 'message';

UPDATE chat_member SET draft = NULL WHERE draft IS NOT NULL;

UPDATE chat_report
  SET snapshot = jsonb_build_object('messages', '[]'::jsonb, 'scrubbed', true),
      note = NULL, forward_error = NULL;

COMMIT;
