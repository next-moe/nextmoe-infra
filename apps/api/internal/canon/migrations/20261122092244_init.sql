-- +goose Up
-- create "work" table
CREATE TABLE "work" ("id" bigint NOT NULL DEFAULT nextval('canon_ids.work'::regclass), "medium" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));

-- +goose Down
-- reverse: create "work" table
DROP TABLE "work";
