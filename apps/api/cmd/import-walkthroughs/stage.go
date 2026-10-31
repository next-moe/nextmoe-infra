package main

import (
	"context"

	"gorm.io/gorm"
)

const stageDDL = `
CREATE SCHEMA IF NOT EXISTS wt_stage;
CREATE TABLE IF NOT EXISTS wt_stage.extraction (
  entry_id       bigint      NOT NULL,
  prompt_version text        NOT NULL,
  model          text        NOT NULL,
  input_hash     text        NOT NULL,
  verdict        text        NOT NULL,
  chart          jsonb,
  metrics        jsonb       NOT NULL DEFAULT '{}',
  error          text        NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (entry_id, prompt_version)
);
ALTER TABLE wt_stage.extraction ADD COLUMN IF NOT EXISTS raw jsonb;
CREATE TABLE IF NOT EXISTS wt_stage.translation (
  entry_id       bigint      NOT NULL,
  prompt_version text        NOT NULL,
  model          text        NOT NULL,
  input_hash     text        NOT NULL,
  strings        jsonb,
  error          text        NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (entry_id, prompt_version)
);
CREATE TABLE IF NOT EXISTS wt_stage.match (
  entry_id    bigint      PRIMARY KEY,
  work_id     bigint,
  method      text        NOT NULL,
  candidates  jsonb       NOT NULL DEFAULT '[]',
  characters  jsonb       NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);`

// glm-5.2's v2 run left 965 charts incomplete, and grok-4.7's v3 re-run of
// them is not better everywhere, so each entry reads its best extraction, not
// its newest.
func bestExtractions(where string) string {
	return `(SELECT DISTINCT ON (entry_id) * FROM wt_stage.extraction WHERE ` + where +
		` ORDER BY entry_id, verdict <> 'ok', coalesce((metrics->>'coverage')::float8, 0) DESC,` +
		` substring(prompt_version from '[0-9]+$')::int DESC) AS x`
}

const settledVerdicts = "verdict IN ('ok', 'review', 'no_chart')"

func ensureStage(ctx context.Context, src *gorm.DB) error {
	return src.WithContext(ctx).Exec(stageDDL).Error
}
