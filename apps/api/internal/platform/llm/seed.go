package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	SceneAssistant      = "agent.assistant"
	SceneSearch         = "agent.search"
	SceneTitle          = "agent.title"
	SceneModerationLLM  = "ai.moderation.llm"
	SceneModerationOmni = "ai.moderation.omni"
	SceneCatalogEmbed   = "catalog.embed"
)

type Bootstrap struct {
	UpstreamBaseURL string
	UpstreamKeyEnv  string
	UpstreamModel   string
	OmniBaseURL     string
	OmniKeyEnv      string
	OmniModel       string
}

type seedModel struct {
	model    Model
	upstream string
	price    *PriceRevision
}

// SeedDefaults turns the legacy KUN_AI_UPSTREAM_* / KUN_AI_OMNI_* environment
// into gateway rows on the very first start (llm_provider empty), and never
// again: after that the admin console is the only place keys and routes are
// managed, and the env vars can be deleted.
func SeedDefaults(ctx context.Context, db *gorm.DB, kek []byte, b Bootstrap) error {
	upKey, omniKey := os.Getenv(b.UpstreamKeyEnv), os.Getenv(b.OmniKeyEnv)
	if (b.UpstreamBaseURL == "" || upKey == "") && (b.OmniBaseURL == "" || omniKey == "") {
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
		ids := map[string]int64{}
		if b.UpstreamBaseURL != "" && upKey != "" {
			p, err := seedProvider(tx, kek, "cloudflare", b.UpstreamBaseURL, b.UpstreamKeyEnv, upKey)
			if err != nil {
				return err
			}
			moderation := b.UpstreamModel
			if moderation == "" {
				moderation = "@cf/zai-org/glm-5.2"
			}
			models := []seedModel{
				{Model{Key: "deepseek-v4-flash", Kind: ModelChat, ContextTokens: 131072, MaxOutput: 8192, ToolCall: true, Reasoning: true, Enabled: true},
					"@cf/deepseek-ai/deepseek-v4-flash", &PriceRevision{InputMicro: 440_000, CachedMicro: 14_000, OutputMicro: 1_320_000, Source: "manual"}},
				{Model{Key: "qwen3-30b-a3b", Kind: ModelChat, ContextTokens: 32768, MaxOutput: 4096, ToolCall: true, Reasoning: true, Enabled: true},
					"@cf/qwen/qwen3-30b-a3b-fp8", nil},
				{Model{Key: modelKeyFor(moderation), Kind: ModelChat, ContextTokens: 131072, MaxOutput: 8192, Enabled: true}, moderation, nil},
				{Model{Key: "qwen3-embedding-0.6b", Kind: ModelEmbedding, ContextTokens: 8192, Enabled: true}, "@cf/qwen/qwen3-embedding-0.6b", nil},
			}
			for _, sm := range models {
				if _, dup := ids[sm.model.Key]; dup {
					continue
				}
				id, err := seedRoute(tx, p, sm)
				if err != nil {
					return err
				}
				ids[sm.model.Key] = id
			}
			scenes := []Scene{
				{Key: SceneAssistant, ModelID: ids["deepseek-v4-flash"], FallbackModelIDs: idList(ids, "qwen3-30b-a3b"), Class: ClassInteractive, MaxOutput: 4096, TTFTMs: 20000, IdleMs: 30000, TotalMs: 120000},
				{Key: SceneSearch, ModelID: ids["qwen3-30b-a3b"], FallbackModelIDs: idList(ids, "deepseek-v4-flash"), Class: ClassSearch, MaxOutput: 1024, TTFTMs: 8000, IdleMs: 8000, TotalMs: 15000},
				{Key: SceneTitle, ModelID: ids["qwen3-30b-a3b"], FallbackModelIDs: idList(ids, "deepseek-v4-flash"), Class: ClassBatch, MaxOutput: 256, TTFTMs: 15000, IdleMs: 15000, TotalMs: 30000},
				{Key: SceneModerationLLM, ModelID: ids[modelKeyFor(moderation)], FallbackModelIDs: idList(ids, "deepseek-v4-flash"), Class: ClassModeration, TTFTMs: 60000, IdleMs: 60000, TotalMs: 90000},
				{Key: SceneCatalogEmbed, ModelID: ids["qwen3-embedding-0.6b"], FallbackModelIDs: datatypes.JSON("[]"), Class: ClassSearch, TTFTMs: 30000, IdleMs: 30000, TotalMs: 60000},
			}
			for _, sc := range scenes {
				if err := tx.Create(&sc).Error; err != nil {
					return fmt.Errorf("seed scene %s: %w", sc.Key, err)
				}
			}
		}
		if b.OmniBaseURL != "" && omniKey != "" {
			base := strings.TrimRight(b.OmniBaseURL, "/")
			if !strings.HasSuffix(base, "/v1") {
				base += "/v1"
			}
			p, err := seedProvider(tx, kek, "openai", base, b.OmniKeyEnv, omniKey)
			if err != nil {
				return err
			}
			model := b.OmniModel
			if model == "" {
				model = "omni-moderation-latest"
			}
			id, err := seedRoute(tx, p, seedModel{Model{Key: model, Kind: ModelModeration, Enabled: true}, model, nil})
			if err != nil {
				return err
			}
			sc := Scene{Key: SceneModerationOmni, ModelID: id, FallbackModelIDs: datatypes.JSON("[]"), Class: ClassModeration, TTFTMs: 30000, IdleMs: 30000, TotalMs: 30000}
			if err := tx.Create(&sc).Error; err != nil {
				return fmt.Errorf("seed scene %s: %w", sc.Key, err)
			}
		}
		return nil
	})
}

func seedProvider(tx *gorm.DB, kek []byte, name, baseURL, keyEnv, key string) (*Provider, error) {
	p := Provider{Name: name, Kind: KindCompatible, BaseURL: strings.TrimRight(baseURL, "/"), Enabled: true, PriceMultiplierPPM: 1_000_000, Headers: datatypes.JSON("{}"), KeyHint: keyHint(key)}
	if len(kek) > 0 {
		sealed, err := SealKey(kek, key)
		if err != nil {
			return nil, err
		}
		p.KeyCipher = sealed
	} else {
		slog.Warn("llm seed: KUN_LLM_KEK is not set, so this provider reads its key from the environment until a key is saved in the admin console", "provider", name, "env", keyEnv)
		p.KeyEnv = keyEnv
	}
	if err := tx.Create(&p).Error; err != nil {
		return nil, fmt.Errorf("seed provider %s: %w", name, err)
	}
	return &p, nil
}

func seedRoute(tx *gorm.DB, p *Provider, sm seedModel) (int64, error) {
	m := sm.model
	if err := tx.Create(&m).Error; err != nil {
		return 0, fmt.Errorf("seed model %s: %w", m.Key, err)
	}
	r := Route{ModelID: m.ID, ProviderID: p.ID, UpstreamID: sm.upstream, Protocol: protocolFor(m.Kind), Weight: 100, Status: RouteActive}
	if err := tx.Create(&r).Error; err != nil {
		return 0, fmt.Errorf("seed route %s: %w", m.Key, err)
	}
	if sm.price != nil {
		price := *sm.price
		price.RouteID = r.ID
		if err := tx.Create(&price).Error; err != nil {
			return 0, err
		}
		if err := tx.Model(&r).Update("price_id", price.ID).Error; err != nil {
			return 0, err
		}
	}
	return m.ID, nil
}

func modelKeyFor(upstream string) string {
	k := upstream[strings.LastIndex(upstream, "/")+1:]
	return strings.ToLower(k)
}

func idList(ids map[string]int64, keys ...string) datatypes.JSON {
	out := []int64{}
	for _, k := range keys {
		if id, ok := ids[k]; ok {
			out = append(out, id)
		}
	}
	raw, _ := json.Marshal(out)
	return raw
}
