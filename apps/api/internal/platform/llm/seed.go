package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	SceneAssistant = "agent.assistant"
	SceneSearch    = "agent.search"
	SceneTitle     = "agent.title"
)

type seedModel struct {
	model    Model
	upstream string
	price    *PriceRevision
}

func SeedFromEnv(ctx context.Context, db *gorm.DB, baseURL, keyEnv string) error {
	if baseURL == "" {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&Provider{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		p := Provider{Name: "cloudflare", Kind: KindCompatible, BaseURL: baseURL, KeyEnv: keyEnv, Enabled: true, PriceMultiplierPPM: 1_000_000}
		if err := tx.Create(&p).Error; err != nil {
			return fmt.Errorf("seed provider: %w", err)
		}
		models := []seedModel{
			{Model{Key: "deepseek-v4-flash", ContextTokens: 131072, MaxOutput: 8192, ToolCall: true, Reasoning: true, Enabled: true},
				"@cf/deepseek-ai/deepseek-v4-flash", &PriceRevision{InputMicro: 440_000, CachedMicro: 14_000, OutputMicro: 1_320_000, Source: "manual"}},
			{Model{Key: "qwen3-30b-a3b", ContextTokens: 32768, MaxOutput: 4096, ToolCall: true, Reasoning: true, Enabled: true},
				"@cf/qwen/qwen3-30b-a3b-fp8", nil},
		}
		ids := map[string]int64{}
		for _, sm := range models {
			m := sm.model
			if err := tx.Create(&m).Error; err != nil {
				return fmt.Errorf("seed model %s: %w", m.Key, err)
			}
			ids[m.Key] = m.ID
			r := Route{ModelID: m.ID, ProviderID: p.ID, UpstreamID: sm.upstream, Protocol: ProtocolChat, Weight: 100, Status: RouteActive}
			if err := tx.Create(&r).Error; err != nil {
				return fmt.Errorf("seed route %s: %w", m.Key, err)
			}
			if sm.price != nil {
				price := *sm.price
				price.RouteID = r.ID
				if err := tx.Create(&price).Error; err != nil {
					return err
				}
				if err := tx.Model(&r).Update("price_id", price.ID).Error; err != nil {
					return err
				}
			}
		}
		fallback := func(keys ...string) datatypes.JSON {
			var out []int64
			for _, k := range keys {
				out = append(out, ids[k])
			}
			raw, _ := json.Marshal(out)
			return raw
		}
		scenes := []Scene{
			{Key: SceneAssistant, ModelID: ids["deepseek-v4-flash"], FallbackModelIDs: fallback("qwen3-30b-a3b"), Class: ClassInteractive, MaxOutput: 4096, TTFTMs: 20000, IdleMs: 30000, TotalMs: 120000},
			{Key: SceneSearch, ModelID: ids["qwen3-30b-a3b"], FallbackModelIDs: fallback("deepseek-v4-flash"), Class: ClassSearch, MaxOutput: 1024, TTFTMs: 8000, IdleMs: 8000, TotalMs: 15000},
			{Key: SceneTitle, ModelID: ids["qwen3-30b-a3b"], FallbackModelIDs: fallback("deepseek-v4-flash"), Class: ClassBatch, MaxOutput: 256, TTFTMs: 15000, IdleMs: 15000, TotalMs: 30000},
		}
		for _, sc := range scenes {
			if err := tx.Create(&sc).Error; err != nil {
				return fmt.Errorf("seed scene %s: %w", sc.Key, err)
			}
		}
		return nil
	})
}
