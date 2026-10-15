-- Prune kun_artifacts to the newest few hundred public artifact records. Like
-- the image seed, the underlying files stay in prod object storage; the rows
-- are enough to exercise listings and the manifest model.

BEGIN;

CREATE TEMP TABLE keep_artifact (id bigint PRIMARY KEY);
INSERT INTO keep_artifact
SELECT id FROM artifacts WHERE public AND deleted_at IS NULL
ORDER BY id DESC LIMIT :seed_works;

-- A composite survives only if every source artifact does (composite_sources
-- cascades from composites and RESTRICTs artifact deletes).
DELETE FROM composites c
WHERE EXISTS (SELECT 1 FROM composite_sources s
              WHERE s.composite_id = c.id
                AND s.artifact_id NOT IN (SELECT id FROM keep_artifact));

DELETE FROM manifests WHERE artifact_id NOT IN (SELECT id FROM keep_artifact);
DELETE FROM artifacts WHERE id NOT IN (SELECT id FROM keep_artifact);

COMMIT;
