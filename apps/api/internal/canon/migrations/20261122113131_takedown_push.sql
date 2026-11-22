-- +goose Up
-- modify "takedown" table
ALTER TABLE "takedown" ADD COLUMN "exclude_host" character varying NULL, ADD COLUMN "pushed_at" timestamptz NULL, ADD COLUMN "restore_pushed_at" timestamptz NULL;

-- +goose Down
-- reverse: modify "takedown" table
ALTER TABLE "takedown" DROP COLUMN "restore_pushed_at", DROP COLUMN "pushed_at", DROP COLUMN "exclude_host";
