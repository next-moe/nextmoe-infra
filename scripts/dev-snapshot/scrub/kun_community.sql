-- kun_community scrub — server-side scratch DB only.
-- Only HELD posts (status = 1, awaiting moderation) have their body reduced to
-- fidelity, plus moderator flag notes (裁定 1b). Public posts (approved) keep
-- their real content — that is the community graph the snapshot exists to serve.
--
-- Expected psql variables: marker  (e.g. -v marker="[dev-scrubbed]")

\set ON_ERROR_STOP on

BEGIN;

-- Held posts (status = 1): body + rendered HTML → synthetic placeholder.
UPDATE community_post SET
  content_raw  = :'marker' || ' synthetic held-post body.',
  content_html = '<p>' || :'marker' || ' synthetic held-post body.</p>'
  WHERE status = 1;

-- Moderator flag notes (free text about a user/report).
UPDATE community_flag
  SET note = :'marker' || ' synthetic flag note.'
  WHERE note IS NOT NULL AND note <> '';

-- What the author purge took, kept 30 days so a mistaken purge can be undone:
-- content its authors had removed, so none of it leaves production.
TRUNCATE community_purge_archive;

COMMIT;
