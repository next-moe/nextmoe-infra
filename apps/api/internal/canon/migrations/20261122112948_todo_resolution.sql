-- +goose Up
-- modify "todo" table
ALTER TABLE "todo" ADD COLUMN "answered" boolean NOT NULL DEFAULT false, ADD COLUMN "resolution" jsonb NULL;

-- +goose Down
-- reverse: modify "todo" table
ALTER TABLE "todo" DROP COLUMN "resolution", DROP COLUMN "answered";
