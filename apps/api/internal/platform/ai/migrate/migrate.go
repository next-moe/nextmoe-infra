// Package migrate owns the kun_ai schema migration: the AutoMigrate table list
// plus the idempotent raw-SQL section for the two usage indexes. It is called
// by cmd/migrate-ai and by the ai integration tests (which provision their
// database with the exact production schema). No seed — the routes are code
// constants (internal/platform/ai/route) and budget rows are inserted by ops.
package migrate

import (
	"fmt"

	"api/internal/platform/ai/model"
	"api/internal/platform/llm"

	"gorm.io/gorm"
)

// Run applies the full kun_ai schema (tables + raw-SQL indexes). Idempotent:
// safe on every deploy and repeatedly against the same database.
func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.AIUsage{},
		&model.AIRouteBudget{},
	); err != nil {
		return fmt.Errorf("ai automigrate: %w", err)
	}
	// 2026-10-28: the LLM gateway tables (provider / model / route / price
	// revision / scene / attempt / event / access key). All new and additive;
	// no existing row changes. Provider API keys are stored sealed with
	// KUN_LLM_KEK and managed from the admin console; on its first start the
	// ai service seeds the providers, models, routes and scenes from the legacy
	// KUN_AI_UPSTREAM_* / KUN_AI_OMNI_* env when llm_provider is empty, so this
	// migration writes no rows.
	//
	// 2026-10-29: llm_provider_model (the per-provider catalog filled by the
	// admin "sync models" action and a daily sync) and
	// llm_price_revision.per_call_micro (per call / per picture, default 0, so
	// every existing revision keeps its token-only price). Additive.
	//
	// Same day, routing: llm_route.rpm (0 = unlimited; max_concurrency is now
	// enforced too), llm_scene.strategy (default 'weighted', today's behaviour),
	// llm_scene.context_fallback / content_fallback (default '[]', none) and
	// llm_provider.catalog_synced_at (null until the first sync). Additive;
	// every existing route and scene keeps behaving as before.
	//
	// Same day, budgets and usage: llm_access_key.daily_budget_micro and
	// llm_scene.daily_budget_micro (0 = unlimited), llm_attempt.access_key_id
	// (0 for in-process callers; older rows read as 0), llm_event.actor, and
	// llm_usage_hourly, which the ai service fills from llm_attempt every ten
	// minutes; raw attempts older than 30 days are then pruned. Additive.
	if err := db.AutoMigrate(llm.Tables()...); err != nil {
		return fmt.Errorf("llm gateway automigrate: %w", err)
	}
	return rawSQL(db)
}

// rawSQL creates the ai_usage access indexes AutoMigrate does not derive from
// the model (doc 20 §5: site-leading tenant view + route-leading scenario
// trend; the budget fuse aggregates the current-day window off the site index).
// Every statement is CREATE INDEX IF NOT EXISTS, so this reruns freely.
func rawSQL(db *gorm.DB) error {
	for _, ix := range []struct{ name, stmt string }{
		{"idx_ai_usage_site_created", `
			CREATE INDEX IF NOT EXISTS idx_ai_usage_site_created
			    ON ai_usage(site, created_at)`},
		{"idx_ai_usage_route_created", `
			CREATE INDEX IF NOT EXISTS idx_ai_usage_route_created
			    ON ai_usage(route, created_at)`},
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}
	return nil
}
