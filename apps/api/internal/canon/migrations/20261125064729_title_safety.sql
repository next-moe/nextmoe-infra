-- +goose Up
-- modify "work" table
ALTER TABLE "work" ALTER COLUMN "display_safety" SET DEFAULT 'unjudged';
-- create "title_verdict" table
CREATE TABLE "title_verdict" ("id" character varying NOT NULL, "class" character varying NOT NULL, "confidence" double precision NOT NULL, "model" character varying NOT NULL, "prompt_version" character varying NOT NULL, "judged_at" timestamptz NOT NULL, PRIMARY KEY ("id"));

-- +goose Down
-- reverse: create "title_verdict" table
DROP TABLE "title_verdict";
-- reverse: modify "work" table
ALTER TABLE "work" ALTER COLUMN "display_safety" SET DEFAULT 'safe';
