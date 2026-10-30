package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const configVersion = 1

type ConfigProvider struct {
	Name               string            `json:"name"`
	BaseURL            string            `json:"base_url"`
	Headers            map[string]string `json:"headers"`
	PriceMultiplierPPM int64             `json:"price_multiplier_ppm"`
	Enabled            bool              `json:"enabled"`
}

type ConfigModel struct {
	Key           string `json:"key"`
	Kind          string `json:"kind"`
	ContextTokens int    `json:"context_tokens"`
	MaxOutput     int    `json:"max_output"`
	ToolCall      bool   `json:"tool_call"`
	Vision        bool   `json:"vision"`
	Reasoning     bool   `json:"reasoning"`
	Enabled       bool   `json:"enabled"`
}

type ConfigRoute struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	UpstreamID  string `json:"upstream_id"`
	Priority    int    `json:"priority"`
	Weight      int    `json:"weight"`
	Status      string `json:"status"`
	MaxInFlight int    `json:"max_in_flight"`
	RPM         int    `json:"rpm"`
	Price       *Price `json:"price"`
}

type ConfigScene struct {
	Key              string   `json:"key"`
	Model            string   `json:"model"`
	Fallbacks        []string `json:"fallbacks"`
	ContextFallback  []string `json:"context_fallback"`
	ContentFallback  []string `json:"content_fallback"`
	Strategy         string   `json:"strategy"`
	DailyBudgetMicro int64    `json:"daily_budget_micro"`
	Class            string   `json:"class"`
	Temperature      *float64 `json:"temperature"`
	MaxOutput        int      `json:"max_output"`
	TTFTMs           int      `json:"ttft_ms"`
	IdleMs           int      `json:"idle_ms"`
	TotalMs          int      `json:"total_ms"`
}

type ConfigAccessKey struct {
	Name             string   `json:"name"`
	AllowedModels    []string `json:"allowed_models"`
	Enabled          bool     `json:"enabled"`
	DailyBudgetMicro int64    `json:"daily_budget_micro"`
}

// GatewayConfig refers to everything by name, never by database id, and
// carries no provider key and no access key secret.
type GatewayConfig struct {
	Version    int               `json:"version"`
	ExportedAt time.Time         `json:"exported_at"`
	Providers  []ConfigProvider  `json:"providers"`
	Models     []ConfigModel     `json:"models"`
	Routes     []ConfigRoute     `json:"routes"`
	Scenes     []ConfigScene     `json:"scenes"`
	AccessKeys []ConfigAccessKey `json:"access_keys" doc:"Names, scopes and budgets only; an imported key that does not exist is created without a secret and must be re-issued."`
}

func (a *Admin) ExportConfig(ctx context.Context) (GatewayConfig, error) {
	db := a.db.WithContext(ctx)
	var (
		providers []Provider
		models    []Model
		routes    []Route
		scenes    []Scene
		keys      []AccessKey
		prices    []PriceRevision
	)
	for _, q := range []struct {
		dst   any
		order string
	}{{&providers, "name"}, {&models, "key"}, {&routes, "id"}, {&scenes, "key"}, {&keys, "name"}, {&prices, "id"}} {
		if err := db.Order(q.order).Find(q.dst).Error; err != nil {
			return GatewayConfig{}, err
		}
	}
	pName := map[int64]string{}
	mKey := map[int64]string{}
	price := map[int64]*PriceRevision{}
	for _, p := range providers {
		pName[p.ID] = p.Name
	}
	for _, m := range models {
		mKey[m.ID] = m.Key
	}
	for i := range prices {
		price[prices[i].ID] = &prices[i]
	}
	keysOf := func(raw datatypes.JSON) []string {
		out := []string{}
		for _, id := range modelIDs(raw) {
			if k, ok := mKey[id]; ok {
				out = append(out, k)
			}
		}
		return out
	}
	cfg := GatewayConfig{Version: configVersion, ExportedAt: a.now(), Providers: []ConfigProvider{}, Models: []ConfigModel{},
		Routes: []ConfigRoute{}, Scenes: []ConfigScene{}, AccessKeys: []ConfigAccessKey{}}
	for _, p := range providers {
		h := map[string]string{}
		_ = json.Unmarshal(p.Headers, &h)
		cfg.Providers = append(cfg.Providers, ConfigProvider{Name: p.Name, BaseURL: p.BaseURL, Headers: h, PriceMultiplierPPM: p.PriceMultiplierPPM, Enabled: p.Enabled})
	}
	for _, m := range models {
		cfg.Models = append(cfg.Models, ConfigModel{Key: m.Key, Kind: m.kind(), ContextTokens: m.ContextTokens, MaxOutput: m.MaxOutput,
			ToolCall: m.ToolCall, Vision: m.Vision, Reasoning: m.Reasoning, Enabled: m.Enabled})
	}
	for _, r := range routes {
		cr := ConfigRoute{Model: mKey[r.ModelID], Provider: pName[r.ProviderID], UpstreamID: r.UpstreamID, Priority: r.Priority,
			Weight: r.Weight, Status: r.Status, MaxInFlight: r.MaxConcurrency, RPM: r.RPM}
		if r.PriceID != nil && price[*r.PriceID] != nil {
			p := price[*r.PriceID]
			cr.Price = &Price{InputMicro: p.InputMicro, CachedMicro: p.CachedMicro, OutputMicro: p.OutputMicro, PerCallMicro: p.PerCallMicro}
		}
		cfg.Routes = append(cfg.Routes, cr)
	}
	for _, s := range scenes {
		cfg.Scenes = append(cfg.Scenes, ConfigScene{Key: s.Key, Model: mKey[s.ModelID], Fallbacks: keysOf(s.FallbackModelIDs),
			ContextFallback: keysOf(s.ContextFallback), ContentFallback: keysOf(s.ContentFallback), Strategy: s.Strategy,
			DailyBudgetMicro: s.DailyBudgetMicro, Class: s.Class, Temperature: s.Temperature, MaxOutput: s.MaxOutput,
			TTFTMs: s.TTFTMs, IdleMs: s.IdleMs, TotalMs: s.TotalMs})
	}
	for _, k := range keys {
		allowed := []string{}
		_ = json.Unmarshal(k.AllowedModels, &allowed)
		cfg.AccessKeys = append(cfg.AccessKeys, ConfigAccessKey{Name: k.Name, AllowedModels: allowed, Enabled: k.Enabled, DailyBudgetMicro: k.DailyBudgetMicro})
	}
	return cfg, nil
}

type ImportChange struct {
	Kind   string `json:"kind" enum:"provider,model,route,scene,access_key"`
	Name   string `json:"name"`
	Action string `json:"action" enum:"create,update,unchanged"`
}

type ImportResult struct {
	Applied  bool           `json:"applied"`
	Changes  []ImportChange `json:"changes"`
	Warnings []string       `json:"warnings"`
}

// ImportConfig upserts by name and never deletes: whatever exists only in the
// target stays as it is, so an import cannot take a route away mid-traffic.
func (a *Admin) ImportConfig(ctx context.Context, cfg GatewayConfig, apply bool) (ImportResult, error) {
	if cfg.Version != configVersion {
		return ImportResult{}, invalid("config version %d is not %d", cfg.Version, configVersion)
	}
	out := ImportResult{Applied: apply, Changes: []ImportChange{}, Warnings: []string{}}
	errRollback := errors.New("dry run")
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		note := func(kind, name, action string) {
			out.Changes = append(out.Changes, ImportChange{Kind: kind, Name: name, Action: action})
		}
		pID := map[string]int64{}
		for _, cp := range cfg.Providers {
			headers, _ := json.Marshal(cp.Headers)
			var p Provider
			err := tx.Where("name = ?", cp.Name).Take(&p).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				p = Provider{Name: cp.Name, Kind: KindCompatible, BaseURL: cp.BaseURL, Headers: headers, PriceMultiplierPPM: cp.PriceMultiplierPPM, Enabled: cp.Enabled}
				if err := tx.Create(&p).Error; err != nil {
					return err
				}
				note("provider", cp.Name, "create")
				out.Warnings = append(out.Warnings, fmt.Sprintf("provider %s was created without an API key; set it in 供应商", cp.Name))
			case err != nil:
				return err
			default:
				same := p.BaseURL == cp.BaseURL && p.PriceMultiplierPPM == cp.PriceMultiplierPPM && p.Enabled == cp.Enabled && jsonEqual(p.Headers, headers)
				if !same {
					if err := tx.Model(&p).Updates(map[string]any{"base_url": cp.BaseURL, "headers": datatypes.JSON(headers),
						"price_multiplier_ppm": cp.PriceMultiplierPPM, "enabled": cp.Enabled}).Error; err != nil {
						return err
					}
				}
				note("provider", cp.Name, map[bool]string{true: "unchanged", false: "update"}[same])
			}
			pID[cp.Name] = p.ID
		}
		mID := map[string]int64{}
		var existingModels []Model
		if err := tx.Find(&existingModels).Error; err != nil {
			return err
		}
		for _, m := range existingModels {
			mID[m.Key] = m.ID
		}
		for _, cm := range cfg.Models {
			want := Model{Key: cm.Key, Kind: cm.Kind, ContextTokens: cm.ContextTokens, MaxOutput: cm.MaxOutput, ToolCall: cm.ToolCall,
				Vision: cm.Vision, Reasoning: cm.Reasoning, Enabled: cm.Enabled}
			id, ok := mID[cm.Key]
			if !ok {
				if err := tx.Create(&want).Error; err != nil {
					return err
				}
				mID[cm.Key] = want.ID
				note("model", cm.Key, "create")
				continue
			}
			var cur Model
			if err := tx.First(&cur, id).Error; err != nil {
				return err
			}
			want.ID, want.CreatedAt = cur.ID, cur.CreatedAt
			want.UpdatedAt = cur.UpdatedAt
			if cur.kind() != want.kind() {
				return invalid("model %s is %s here and %s in the config", cm.Key, cur.kind(), want.kind())
			}
			if reflect.DeepEqual(cur, want) {
				note("model", cm.Key, "unchanged")
				continue
			}
			if err := tx.Model(&cur).Updates(map[string]any{"context_tokens": cm.ContextTokens, "max_output": cm.MaxOutput,
				"tool_call": cm.ToolCall, "vision": cm.Vision, "reasoning": cm.Reasoning, "enabled": cm.Enabled}).Error; err != nil {
				return err
			}
			note("model", cm.Key, "update")
		}
		for _, cr := range cfg.Routes {
			name := fmt.Sprintf("%s → %s:%s", cr.Model, cr.Provider, cr.UpstreamID)
			mid, pid := mID[cr.Model], pID[cr.Provider]
			if mid == 0 || pid == 0 {
				var p Provider
				if pid == 0 && tx.Where("name = ?", cr.Provider).Take(&p).Error == nil {
					pid = p.ID
				}
				if mid == 0 || pid == 0 {
					out.Warnings = append(out.Warnings, "skipped route "+name+": its model or provider is missing")
					continue
				}
			}
			var cur Route
			err := tx.Where("model_id = ? AND provider_id = ? AND upstream_id = ?", mid, pid, cr.UpstreamID).Take(&cur).Error
			action := "unchanged"
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				var m Model
				if err := tx.First(&m, mid).Error; err != nil {
					return err
				}
				cur = Route{ModelID: mid, ProviderID: pid, UpstreamID: cr.UpstreamID, Protocol: protocolFor(m.Kind), Priority: cr.Priority,
					Weight: max(cr.Weight, 1), Status: cr.Status, MaxConcurrency: cr.MaxInFlight, RPM: cr.RPM}
				if cur.Status == "" {
					cur.Status = RouteActive
				}
				if err := tx.Create(&cur).Error; err != nil {
					return err
				}
				action = "create"
			case err != nil:
				return err
			default:
				if cur.Priority != cr.Priority || cur.Weight != cr.Weight || cur.Status != cr.Status || cur.MaxConcurrency != cr.MaxInFlight || cur.RPM != cr.RPM {
					if err := tx.Model(&cur).Updates(map[string]any{"priority": cr.Priority, "weight": max(cr.Weight, 1), "status": cr.Status,
						"max_concurrency": cr.MaxInFlight, "rpm": cr.RPM}).Error; err != nil {
						return err
					}
					action = "update"
				}
			}
			if cr.Price != nil {
				var p PriceRevision
				samePrice := cur.PriceID != nil && tx.First(&p, *cur.PriceID).Error == nil &&
					p.InputMicro == cr.Price.InputMicro && p.CachedMicro == cr.Price.CachedMicro && p.OutputMicro == cr.Price.OutputMicro && p.PerCallMicro == cr.Price.PerCallMicro
				if !samePrice {
					rev := PriceRevision{RouteID: cur.ID, InputMicro: cr.Price.InputMicro, CachedMicro: cr.Price.CachedMicro, OutputMicro: cr.Price.OutputMicro,
						PerCallMicro: cr.Price.PerCallMicro, Source: "import", CreatedAt: a.now()}
					if err := tx.Create(&rev).Error; err != nil {
						return err
					}
					if err := tx.Model(&cur).Update("price_id", rev.ID).Error; err != nil {
						return err
					}
					if action == "unchanged" {
						action = "update"
					}
				}
			}
			note("route", name, action)
		}
		ids := func(keys []string) ([]int64, bool) {
			out := []int64{}
			for _, k := range keys {
				id, ok := mID[k]
				if !ok {
					return nil, false
				}
				out = append(out, id)
			}
			return out, true
		}
		for _, cs := range cfg.Scenes {
			fb, ok1 := ids(cs.Fallbacks)
			cf, ok2 := ids(cs.ContextFallback)
			nf, ok3 := ids(cs.ContentFallback)
			if mID[cs.Model] == 0 || !ok1 || !ok2 || !ok3 {
				out.Warnings = append(out.Warnings, "skipped scene "+cs.Key+": a model it names is missing")
				continue
			}
			var cur Scene
			exists := tx.Where("key = ?", cs.Key).Take(&cur).Error == nil
			raw := func(v []int64) datatypes.JSON { b, _ := json.Marshal(v); return b }
			want := Scene{Key: cs.Key, ModelID: mID[cs.Model], FallbackModelIDs: raw(fb), ContextFallback: raw(cf), ContentFallback: raw(nf),
				Strategy: cs.Strategy, DailyBudgetMicro: cs.DailyBudgetMicro, Class: cs.Class, Temperature: cs.Temperature,
				MaxOutput: cs.MaxOutput, TTFTMs: cs.TTFTMs, IdleMs: cs.IdleMs, TotalMs: cs.TotalMs}
			if exists {
				want.UpdatedAt = cur.UpdatedAt
				if sceneEqual(cur, want) {
					note("scene", cs.Key, "unchanged")
					continue
				}
			}
			want.UpdatedAt = a.now()
			if err := tx.Save(&want).Error; err != nil {
				return err
			}
			note("scene", cs.Key, map[bool]string{true: "update", false: "create"}[exists])
		}
		for _, ck := range cfg.AccessKeys {
			allowed, _ := json.Marshal(ck.AllowedModels)
			var cur AccessKey
			if tx.Where("name = ?", ck.Name).Take(&cur).Error != nil {
				out.Warnings = append(out.Warnings, "access key "+ck.Name+" does not exist here; create it in 访问密钥 to get a secret")
				continue
			}
			if jsonEqual(cur.AllowedModels, allowed) && cur.Enabled == ck.Enabled && cur.DailyBudgetMicro == ck.DailyBudgetMicro {
				note("access_key", ck.Name, "unchanged")
				continue
			}
			if err := tx.Model(&cur).Updates(map[string]any{"allowed_models": datatypes.JSON(allowed), "enabled": ck.Enabled,
				"daily_budget_micro": ck.DailyBudgetMicro}).Error; err != nil {
				return err
			}
			note("access_key", ck.Name, "update")
		}
		if !apply {
			return errRollback
		}
		return nil
	})
	if err != nil && !errors.Is(err, errRollback) {
		return ImportResult{}, err
	}
	if apply {
		a.changed(ctx, "config_imported", fmt.Sprintf("%d changes", countChanged(out.Changes)), 0, 0)
	}
	return out, nil
}

func countChanged(cs []ImportChange) int {
	n := 0
	for _, c := range cs {
		if c.Action != "unchanged" {
			n++
		}
	}
	return n
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return reflect.DeepEqual(x, y)
}

func sceneEqual(a, b Scene) bool {
	return a.ModelID == b.ModelID && jsonEqual(a.FallbackModelIDs, b.FallbackModelIDs) && jsonEqual(a.ContextFallback, b.ContextFallback) &&
		jsonEqual(a.ContentFallback, b.ContentFallback) && a.Strategy == b.Strategy && a.DailyBudgetMicro == b.DailyBudgetMicro &&
		a.Class == b.Class && reflect.DeepEqual(a.Temperature, b.Temperature) && a.MaxOutput == b.MaxOutput &&
		a.TTFTMs == b.TTFTMs && a.IdleMs == b.IdleMs && a.TotalMs == b.TotalMs
}
