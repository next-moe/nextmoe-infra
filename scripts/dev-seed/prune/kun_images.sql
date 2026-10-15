-- Prune kun_images to the metadata of public images the other seeds show:
-- kept catalog covers and kept users' avatars (hash lists exported by
-- prune/kun_catalog.sql and prune/kun_galgame_infra.sql). Image BYTES live in
-- prod MinIO/CDN and are not part of any seed. A hash is also its CDN URL, so
-- sampling arbitrary uploads would publish links to images posted in private
-- chats; content-side hash references dangle in dev by design.

-- CSV paths are CWD-relative: \copy does not interpolate psql variables, so
-- build-seed.sh runs this file with CWD = the shared export directory.

BEGIN;

TRUNCATE image_moderation_queue;

CREATE TEMP TABLE public_hash (hash text);
\copy public_hash FROM 'kun_catalog_covers.csv'
\copy public_hash FROM 'kun_galgame_infra_avatars.csv'

CREATE TEMP TABLE keep_image (id bigint PRIMARY KEY, hash text);
INSERT INTO keep_image
SELECT id, hash FROM images
WHERE deleted_at IS NULL AND hash IN (SELECT hash FROM public_hash);

DELETE FROM image_site_usage WHERE hash NOT IN (SELECT hash FROM keep_image);
DELETE FROM images           WHERE id   NOT IN (SELECT id   FROM keep_image);

-- refresh-dev-db clears it, but local uploads write it again.
UPDATE images SET first_uploader_ip = '' WHERE first_uploader_ip <> '';

COMMIT;
