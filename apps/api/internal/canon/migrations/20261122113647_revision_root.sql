-- +goose Up
-- modify "revision" table
ALTER TABLE "revision" ADD COLUMN "root_entity" character varying NOT NULL DEFAULT '', ADD COLUMN "root_id" bigint NOT NULL DEFAULT 0;
-- create index "revision_root_entity_root_id" to table: "revision"
CREATE INDEX "revision_root_entity_root_id" ON "revision" ("root_entity", "root_id");

-- +goose Down
-- reverse: create index "revision_root_entity_root_id" to table: "revision"
DROP INDEX "revision_root_entity_root_id";
-- reverse: modify "revision" table
ALTER TABLE "revision" DROP COLUMN "root_id", DROP COLUMN "root_entity";
