package llm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrNotFound   = errors.New("llm admin: not found")
	ErrInUse      = errors.New("llm admin: still in use")
	ErrInvalid    = errors.New("llm admin: invalid")
	ErrNoKEK      = errors.New("llm admin: KUN_LLM_KEK is not set, so API keys cannot be stored")
	sceneKeyShape = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)
	modelKeyShape = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,63}$`)
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

type Admin struct {
	db  *gorm.DB
	kek []byte
	gw  *Gateway
	now func() time.Time
}

func NewAdmin(db *gorm.DB, kek []byte, gw *Gateway) *Admin {
	return &Admin{db: db, kek: kek, gw: gw, now: time.Now}
}

func (a *Admin) KEKConfigured() bool { return len(a.kek) > 0 }

func (a *Admin) changed(ctx context.Context, kind, detail string, routeID, providerID int64) {
	_ = a.db.WithContext(ctx).Create(&RouteEvent{RouteID: routeID, ProviderID: providerID, Kind: kind, Detail: detail, CreatedAt: a.now()}).Error
	if a.gw != nil {
		_ = a.gw.Refresh(context.WithoutCancel(ctx))
	}
}

type ProviderView struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	BaseURL            string            `json:"base_url"`
	KeyConfigured      bool              `json:"key_configured"`
	KeyHint            string            `json:"key_hint"`
	Headers            map[string]string `json:"headers"`
	PriceMultiplierPPM int64             `json:"price_multiplier_ppm"`
	Enabled            bool              `json:"enabled"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func providerView(p Provider) ProviderView {
	h := map[string]string{}
	_ = json.Unmarshal(p.Headers, &h)
	return ProviderView{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, KeyConfigured: len(p.KeyCipher) > 0 || p.KeyEnv != "",
		KeyHint: p.KeyHint, Headers: h, PriceMultiplierPPM: p.PriceMultiplierPPM, Enabled: p.Enabled, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

type ProviderInput struct {
	Name               *string            `json:"name,omitempty" maxLength:"64"`
	BaseURL            *string            `json:"base_url,omitempty" maxLength:"512"`
	APIKey             *string            `json:"api_key,omitempty" maxLength:"4096"`
	ClearKey           bool               `json:"clear_key,omitempty"`
	Headers            *map[string]string `json:"headers,omitempty"`
	PriceMultiplierPPM *int64             `json:"price_multiplier_ppm,omitempty" minimum:"0"`
	Enabled            *bool              `json:"enabled,omitempty"`
}

func (a *Admin) Providers(ctx context.Context) ([]ProviderView, error) {
	var rows []Provider
	if err := a.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderView, len(rows))
	for i, p := range rows {
		out[i] = providerView(p)
	}
	return out, nil
}

func (a *Admin) applyProvider(p *Provider, in ProviderInput) error {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.BaseURL != nil {
		p.BaseURL = strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/")
	}
	if p.Name == "" {
		return invalid("name is required")
	}
	if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return invalid("base_url must be an http(s) URL")
	}
	if in.Headers != nil {
		raw, _ := json.Marshal(*in.Headers)
		p.Headers = raw
	}
	if len(p.Headers) == 0 {
		p.Headers = datatypes.JSON("{}")
	}
	if in.PriceMultiplierPPM != nil {
		p.PriceMultiplierPPM = *in.PriceMultiplierPPM
	}
	if p.PriceMultiplierPPM <= 0 {
		p.PriceMultiplierPPM = 1_000_000
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.ClearKey {
		p.KeyCipher, p.KeyEnv, p.KeyHint = nil, "", ""
	}
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		if len(a.kek) == 0 {
			return ErrNoKEK
		}
		key := strings.TrimSpace(*in.APIKey)
		sealed, err := SealKey(a.kek, key)
		if err != nil {
			return err
		}
		p.KeyCipher, p.KeyEnv, p.KeyHint = sealed, "", keyHint(key)
	}
	return nil
}

func keyHint(key string) string {
	if len(key) <= 8 {
		return "…"
	}
	return "…" + key[len(key)-4:]
}

func (a *Admin) CreateProvider(ctx context.Context, in ProviderInput) (ProviderView, error) {
	p := Provider{Kind: KindCompatible, Enabled: true}
	if err := a.applyProvider(&p, in); err != nil {
		return ProviderView{}, err
	}
	if err := a.db.WithContext(ctx).Create(&p).Error; err != nil {
		return ProviderView{}, uniqueOr(err, "a provider with this name exists")
	}
	a.changed(ctx, "provider_created", p.Name, 0, p.ID)
	return providerView(p), nil
}

func (a *Admin) UpdateProvider(ctx context.Context, id int64, in ProviderInput) (ProviderView, error) {
	var p Provider
	if err := a.first(ctx, &p, id); err != nil {
		return ProviderView{}, err
	}
	if err := a.applyProvider(&p, in); err != nil {
		return ProviderView{}, err
	}
	if err := a.db.WithContext(ctx).Save(&p).Error; err != nil {
		return ProviderView{}, uniqueOr(err, "a provider with this name exists")
	}
	detail := p.Name
	if in.APIKey != nil && *in.APIKey != "" {
		detail += ": key replaced"
	} else if in.ClearKey {
		detail += ": key cleared"
	}
	a.changed(ctx, "provider_updated", detail, 0, p.ID)
	return providerView(p), nil
}

func (a *Admin) DeleteProvider(ctx context.Context, id int64) error {
	var n int64
	if err := a.db.WithContext(ctx).Model(&Route{}).Where("provider_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %d routes use this provider", ErrInUse, n)
	}
	res := a.db.WithContext(ctx).Delete(&Provider{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	a.changed(ctx, "provider_deleted", "", 0, id)
	return nil
}

type ModelView struct {
	ID            int64     `json:"id"`
	Key           string    `json:"key"`
	Kind          string    `json:"kind" enum:"chat,embedding,moderation,search"`
	ContextTokens int       `json:"context_tokens"`
	MaxOutput     int       `json:"max_output"`
	ToolCall      bool      `json:"tool_call"`
	Vision        bool      `json:"vision"`
	Reasoning     bool      `json:"reasoning"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func modelView(m Model) ModelView {
	return ModelView{ID: m.ID, Key: m.Key, Kind: m.Kind, ContextTokens: m.ContextTokens, MaxOutput: m.MaxOutput,
		ToolCall: m.ToolCall, Vision: m.Vision, Reasoning: m.Reasoning, Enabled: m.Enabled, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

type ModelInput struct {
	Key           *string `json:"key,omitempty" maxLength:"64"`
	Kind          *string `json:"kind,omitempty" enum:"chat,embedding,moderation,search"`
	ContextTokens *int    `json:"context_tokens,omitempty" minimum:"0"`
	MaxOutput     *int    `json:"max_output,omitempty" minimum:"0"`
	ToolCall      *bool   `json:"tool_call,omitempty"`
	Vision        *bool   `json:"vision,omitempty"`
	Reasoning     *bool   `json:"reasoning,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
}

func (a *Admin) Models(ctx context.Context) ([]ModelView, error) {
	var rows []Model
	if err := a.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ModelView, len(rows))
	for i, m := range rows {
		out[i] = modelView(m)
	}
	return out, nil
}

func applyModel(m *Model, in ModelInput, creating bool) error {
	if in.Key != nil {
		m.Key = strings.TrimSpace(*in.Key)
	}
	if !modelKeyShape.MatchString(m.Key) {
		return invalid("key must be lower-case letters, digits and . _ : -")
	}
	if in.Kind != nil {
		if !creating && *in.Kind != m.Kind {
			return invalid("a model's kind cannot change; create a new model")
		}
		m.Kind = *in.Kind
	}
	if m.Kind == "" {
		m.Kind = ModelChat
	}
	if in.ContextTokens != nil {
		m.ContextTokens = *in.ContextTokens
	}
	if in.MaxOutput != nil {
		m.MaxOutput = *in.MaxOutput
	}
	if in.ToolCall != nil {
		m.ToolCall = *in.ToolCall
	}
	if in.Vision != nil {
		m.Vision = *in.Vision
	}
	if in.Reasoning != nil {
		m.Reasoning = *in.Reasoning
	}
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
	return nil
}

func (a *Admin) CreateModel(ctx context.Context, in ModelInput) (ModelView, error) {
	m := Model{Enabled: true}
	if err := applyModel(&m, in, true); err != nil {
		return ModelView{}, err
	}
	if err := a.db.WithContext(ctx).Create(&m).Error; err != nil {
		return ModelView{}, uniqueOr(err, "a model with this key exists")
	}
	a.changed(ctx, "model_created", m.Key, 0, 0)
	return modelView(m), nil
}

func (a *Admin) UpdateModel(ctx context.Context, id int64, in ModelInput) (ModelView, error) {
	var m Model
	if err := a.first(ctx, &m, id); err != nil {
		return ModelView{}, err
	}
	if err := applyModel(&m, in, false); err != nil {
		return ModelView{}, err
	}
	if err := a.db.WithContext(ctx).Save(&m).Error; err != nil {
		return ModelView{}, uniqueOr(err, "a model with this key exists")
	}
	a.changed(ctx, "model_updated", m.Key, 0, 0)
	return modelView(m), nil
}

func (a *Admin) DeleteModel(ctx context.Context, id int64) error {
	var routes, scenes int64
	db := a.db.WithContext(ctx)
	if err := db.Model(&Route{}).Where("model_id = ?", id).Count(&routes).Error; err != nil {
		return err
	}
	if err := db.Model(&Scene{}).Where("model_id = ? OR fallback_model_ids @> ?::jsonb", id, fmt.Sprintf("[%d]", id)).Count(&scenes).Error; err != nil {
		return err
	}
	if routes+scenes > 0 {
		return fmt.Errorf("%w: %d routes and %d scenes use this model", ErrInUse, routes, scenes)
	}
	res := db.Delete(&Model{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	a.changed(ctx, "model_deleted", "", 0, 0)
	return nil
}

type Price struct {
	InputMicro  int64 `json:"input_micro" minimum:"0"`
	CachedMicro int64 `json:"cached_micro" minimum:"0"`
	OutputMicro int64 `json:"output_micro" minimum:"0"`
}

type RouteStats struct {
	Calls    int64 `json:"calls"`
	Errors   int64 `json:"errors"`
	AvgMs    int64 `json:"avg_ms"`
	CostNano int64 `json:"cost_nano"`
}

type RouteView struct {
	ID           int64      `json:"id"`
	ModelID      int64      `json:"model_id"`
	ProviderID   int64      `json:"provider_id"`
	UpstreamID   string     `json:"upstream_id"`
	Priority     int        `json:"priority"`
	Weight       int        `json:"weight"`
	Status       string     `json:"status" enum:"active,disabled"`
	StatusReason string     `json:"status_reason"`
	Price        *Price     `json:"price"`
	Stats24h     RouteStats `json:"stats_24h"`
}

type RouteInput struct {
	ModelID    *int64  `json:"model_id,omitempty"`
	ProviderID *int64  `json:"provider_id,omitempty"`
	UpstreamID *string `json:"upstream_id,omitempty" maxLength:"128"`
	Priority   *int    `json:"priority,omitempty"`
	Weight     *int    `json:"weight,omitempty" minimum:"1" maximum:"10000"`
	Status     *string `json:"status,omitempty" enum:"active,disabled"`
	Price      *Price  `json:"price,omitempty"`
}

func (a *Admin) Routes(ctx context.Context) ([]RouteView, error) {
	db := a.db.WithContext(ctx)
	var rows []Route
	if err := db.Order("model_id, priority DESC, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	var prices []PriceRevision
	if err := db.Where("id IN (SELECT price_id FROM llm_route WHERE price_id IS NOT NULL)").Find(&prices).Error; err != nil {
		return nil, err
	}
	byPrice := map[int64]PriceRevision{}
	for _, p := range prices {
		byPrice[p.ID] = p
	}
	var stats []struct {
		RouteID  int64
		Calls    int64
		Errors   int64
		AvgMs    float64
		CostNano int64
	}
	if err := db.Raw(`SELECT route_id, count(*) AS calls, count(*) FILTER (WHERE NOT ok) AS errors,
			coalesce(avg(duration_ms), 0) AS avg_ms, coalesce(sum(cost_nano), 0) AS cost_nano
		FROM llm_attempt WHERE created_at > ? GROUP BY route_id`, a.now().Add(-24*time.Hour)).Scan(&stats).Error; err != nil {
		return nil, err
	}
	byRoute := map[int64]RouteStats{}
	for _, s := range stats {
		byRoute[s.RouteID] = RouteStats{Calls: s.Calls, Errors: s.Errors, AvgMs: int64(s.AvgMs), CostNano: s.CostNano}
	}
	out := make([]RouteView, len(rows))
	for i, r := range rows {
		out[i] = RouteView{ID: r.ID, ModelID: r.ModelID, ProviderID: r.ProviderID, UpstreamID: r.UpstreamID, Priority: r.Priority,
			Weight: r.Weight, Status: r.Status, StatusReason: r.StatusReason, Stats24h: byRoute[r.ID]}
		if r.PriceID != nil {
			if p, ok := byPrice[*r.PriceID]; ok {
				out[i].Price = &Price{InputMicro: p.InputMicro, CachedMicro: p.CachedMicro, OutputMicro: p.OutputMicro}
			}
		}
	}
	return out, nil
}

func (a *Admin) route(ctx context.Context, id int64) (RouteView, error) {
	views, err := a.Routes(ctx)
	if err != nil {
		return RouteView{}, err
	}
	for _, v := range views {
		if v.ID == id {
			return v, nil
		}
	}
	return RouteView{}, ErrNotFound
}

func (a *Admin) saveRoute(ctx context.Context, r *Route, in RouteInput, creating bool) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.ModelID != nil && creating {
			r.ModelID = *in.ModelID
		}
		if in.ProviderID != nil && creating {
			r.ProviderID = *in.ProviderID
		}
		if in.UpstreamID != nil {
			r.UpstreamID = strings.TrimSpace(*in.UpstreamID)
		}
		if r.UpstreamID == "" {
			return invalid("upstream_id is required")
		}
		var m Model
		if err := tx.First(&m, r.ModelID).Error; err != nil {
			return invalid("model %d does not exist", r.ModelID)
		}
		var n int64
		if err := tx.Model(&Provider{}).Where("id = ?", r.ProviderID).Count(&n).Error; err != nil || n == 0 {
			return invalid("provider %d does not exist", r.ProviderID)
		}
		r.Protocol = protocolFor(m.Kind)
		if in.Priority != nil {
			r.Priority = *in.Priority
		}
		if in.Weight != nil {
			r.Weight = *in.Weight
		}
		if r.Weight <= 0 {
			r.Weight = 100
		}
		if in.Status != nil {
			r.Status, r.StatusReason = *in.Status, ""
		}
		if r.Status == "" {
			r.Status = RouteActive
		}
		if err := tx.Save(r).Error; err != nil {
			return uniqueOr(err, "this model already has a route to this provider with that upstream id")
		}
		if in.Price != nil {
			p := PriceRevision{RouteID: r.ID, InputMicro: in.Price.InputMicro, CachedMicro: in.Price.CachedMicro, OutputMicro: in.Price.OutputMicro, Source: "manual", CreatedAt: a.now()}
			if err := tx.Create(&p).Error; err != nil {
				return err
			}
			r.PriceID = &p.ID
			if err := tx.Model(r).Update("price_id", p.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func protocolFor(kind string) string {
	switch kind {
	case ModelEmbedding:
		return ProtocolEmbeddings
	case ModelModeration:
		return ProtocolModerations
	case ModelSearch:
		return ProtocolResponses
	}
	return ProtocolChat
}

func (a *Admin) CreateRoute(ctx context.Context, in RouteInput) (RouteView, error) {
	if in.ModelID == nil || in.ProviderID == nil {
		return RouteView{}, invalid("model_id and provider_id are required")
	}
	r := Route{}
	if err := a.saveRoute(ctx, &r, in, true); err != nil {
		return RouteView{}, err
	}
	a.changed(ctx, "route_created", r.UpstreamID, r.ID, r.ProviderID)
	return a.route(ctx, r.ID)
}

func (a *Admin) UpdateRoute(ctx context.Context, id int64, in RouteInput) (RouteView, error) {
	var r Route
	if err := a.first(ctx, &r, id); err != nil {
		return RouteView{}, err
	}
	if err := a.saveRoute(ctx, &r, in, false); err != nil {
		return RouteView{}, err
	}
	kind := "route_updated"
	if in.Status != nil {
		kind = "route_" + *in.Status
		if a.gw != nil && *in.Status == RouteActive {
			a.gw.health.success(r.ID)
		}
	}
	a.changed(ctx, kind, r.UpstreamID, r.ID, r.ProviderID)
	return a.route(ctx, r.ID)
}

func (a *Admin) DeleteRoute(ctx context.Context, id int64) error {
	var r Route
	if err := a.first(ctx, &r, id); err != nil {
		return err
	}
	if err := a.db.WithContext(ctx).Delete(&r).Error; err != nil {
		return err
	}
	a.changed(ctx, "route_deleted", r.UpstreamID, r.ID, r.ProviderID)
	return nil
}

type SceneView struct {
	Key              string    `json:"key"`
	ModelID          int64     `json:"model_id"`
	FallbackModelIDs []int64   `json:"fallback_model_ids" nullable:"false"`
	Class            string    `json:"class" enum:"interactive,search,batch,moderation"`
	Temperature      *float64  `json:"temperature" nullable:"true"`
	MaxOutput        int       `json:"max_output"`
	TTFTMs           int       `json:"ttft_ms"`
	IdleMs           int       `json:"idle_ms"`
	TotalMs          int       `json:"total_ms"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SceneInput struct {
	ModelID          int64    `json:"model_id"`
	FallbackModelIDs []int64  `json:"fallback_model_ids" maxItems:"8" nullable:"false"`
	Class            string   `json:"class" enum:"interactive,search,batch,moderation"`
	Temperature      *float64 `json:"temperature" minimum:"0" maximum:"2" nullable:"true"`
	MaxOutput        int      `json:"max_output" minimum:"0"`
	TTFTMs           int      `json:"ttft_ms" minimum:"0" maximum:"600000"`
	IdleMs           int      `json:"idle_ms" minimum:"0" maximum:"600000"`
	TotalMs          int      `json:"total_ms" minimum:"0" maximum:"1800000"`
}

func sceneView(s Scene) SceneView {
	fb := []int64{}
	_ = json.Unmarshal(s.FallbackModelIDs, &fb)
	return SceneView{Key: s.Key, ModelID: s.ModelID, FallbackModelIDs: fb, Class: s.Class, Temperature: s.Temperature,
		MaxOutput: s.MaxOutput, TTFTMs: s.TTFTMs, IdleMs: s.IdleMs, TotalMs: s.TotalMs, UpdatedAt: s.UpdatedAt}
}

func (a *Admin) Scenes(ctx context.Context) ([]SceneView, error) {
	var rows []Scene
	if err := a.db.WithContext(ctx).Order("key").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SceneView, len(rows))
	for i, s := range rows {
		out[i] = sceneView(s)
	}
	return out, nil
}

func (a *Admin) PutScene(ctx context.Context, key string, in SceneInput) (SceneView, error) {
	if !sceneKeyShape.MatchString(key) {
		return SceneView{}, invalid("scene key must look like agent.assistant")
	}
	db := a.db.WithContext(ctx)
	ids := append([]int64{in.ModelID}, in.FallbackModelIDs...)
	var models []Model
	if err := db.Where("id IN ?", ids).Find(&models).Error; err != nil {
		return SceneView{}, err
	}
	kinds := map[int64]string{}
	for _, m := range models {
		kinds[m.ID] = m.Kind
	}
	for _, id := range ids {
		if kinds[id] == "" {
			return SceneView{}, invalid("model %d does not exist", id)
		}
		if kinds[id] != kinds[in.ModelID] {
			return SceneView{}, invalid("fallback model %d is a %s model; the primary is %s", id, kinds[id], kinds[in.ModelID])
		}
	}
	if slices.Contains(in.FallbackModelIDs, in.ModelID) {
		return SceneView{}, invalid("the primary model cannot also be a fallback")
	}
	fb, _ := json.Marshal(nonNil(in.FallbackModelIDs))
	s := Scene{Key: key, ModelID: in.ModelID, FallbackModelIDs: fb, Class: in.Class, Temperature: in.Temperature,
		MaxOutput: in.MaxOutput, TTFTMs: in.TTFTMs, IdleMs: in.IdleMs, TotalMs: in.TotalMs, UpdatedAt: a.now()}
	if s.Class == "" {
		s.Class = ClassInteractive
	}
	if s.TTFTMs == 0 {
		s.TTFTMs = 20_000
	}
	if s.IdleMs == 0 {
		s.IdleMs = 30_000
	}
	if s.TotalMs == 0 {
		s.TotalMs = 120_000
	}
	if err := db.Save(&s).Error; err != nil {
		return SceneView{}, err
	}
	a.changed(ctx, "scene_saved", key, 0, 0)
	return sceneView(s), nil
}

func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

func (a *Admin) DeleteScene(ctx context.Context, key string) error {
	res := a.db.WithContext(ctx).Delete(&Scene{}, "key = ?", key)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	a.changed(ctx, "scene_deleted", key, 0, 0)
	return nil
}

type AccessKeyView struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	AllowedModels []string   `json:"allowed_models"`
	Enabled       bool       `json:"enabled"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type AccessKeyInput struct {
	Name          *string   `json:"name,omitempty" maxLength:"64"`
	AllowedModels *[]string `json:"allowed_models,omitempty" maxItems:"64"`
	Enabled       *bool     `json:"enabled,omitempty"`
}

func accessKeyView(k AccessKey) AccessKeyView {
	allowed := []string{}
	_ = json.Unmarshal(k.AllowedModels, &allowed)
	return AccessKeyView{ID: k.ID, Name: k.Name, Prefix: k.Prefix, AllowedModels: allowed, Enabled: k.Enabled, LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt}
}

func (a *Admin) AccessKeys(ctx context.Context) ([]AccessKeyView, error) {
	var rows []AccessKey
	if err := a.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AccessKeyView, len(rows))
	for i, k := range rows {
		out[i] = accessKeyView(k)
	}
	return out, nil
}

const accessKeyPrefix = "nmk_"

func (a *Admin) CreateAccessKey(ctx context.Context, in AccessKeyInput) (AccessKeyView, string, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return AccessKeyView{}, "", invalid("name is required")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return AccessKeyView{}, "", err
	}
	secret := accessKeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	allowed := []string{}
	if in.AllowedModels != nil {
		allowed = *in.AllowedModels
	}
	raw, _ := json.Marshal(allowed)
	k := AccessKey{Name: strings.TrimSpace(*in.Name), Prefix: secret[:len(accessKeyPrefix)+4], Hash: HashAccessKey(secret), AllowedModels: raw, Enabled: true}
	if in.Enabled != nil {
		k.Enabled = *in.Enabled
	}
	if err := a.db.WithContext(ctx).Create(&k).Error; err != nil {
		return AccessKeyView{}, "", uniqueOr(err, "an access key with this name exists")
	}
	a.changed(ctx, "access_key_created", k.Name, 0, 0)
	return accessKeyView(k), secret, nil
}

func (a *Admin) UpdateAccessKey(ctx context.Context, id int64, in AccessKeyInput) (AccessKeyView, error) {
	var k AccessKey
	if err := a.first(ctx, &k, id); err != nil {
		return AccessKeyView{}, err
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return AccessKeyView{}, invalid("name is required")
		}
		k.Name = strings.TrimSpace(*in.Name)
	}
	if in.AllowedModels != nil {
		k.AllowedModels, _ = json.Marshal(*in.AllowedModels)
	}
	if in.Enabled != nil {
		k.Enabled = *in.Enabled
	}
	if err := a.db.WithContext(ctx).Save(&k).Error; err != nil {
		return AccessKeyView{}, uniqueOr(err, "an access key with this name exists")
	}
	a.changed(ctx, "access_key_updated", k.Name, 0, 0)
	return accessKeyView(k), nil
}

func (a *Admin) DeleteAccessKey(ctx context.Context, id int64) error {
	var k AccessKey
	if err := a.first(ctx, &k, id); err != nil {
		return err
	}
	if err := a.db.WithContext(ctx).Delete(&k).Error; err != nil {
		return err
	}
	a.changed(ctx, "access_key_deleted", k.Name, 0, 0)
	return nil
}

type EventView struct {
	ID         int64     `json:"id"`
	RouteID    int64     `json:"route_id"`
	ProviderID int64     `json:"provider_id"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"created_at"`
}

func (a *Admin) Events(ctx context.Context, limit int) ([]EventView, error) {
	var rows []RouteEvent
	if err := a.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]EventView, len(rows))
	for i, e := range rows {
		out[i] = EventView{ID: e.ID, RouteID: e.RouteID, ProviderID: e.ProviderID, Kind: e.Kind, Detail: e.Detail, CreatedAt: e.CreatedAt}
	}
	return out, nil
}

func (a *Admin) Probe(ctx context.Context, routeID int64) (ProbeResult, error) {
	if err := a.first(ctx, &Route{}, routeID); err != nil {
		return ProbeResult{}, err
	}
	return a.gw.Probe(ctx, routeID)
}

func (a *Admin) first(ctx context.Context, dst any, id int64) error {
	err := a.db.WithContext(ctx).First(dst, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func uniqueOr(err error, msg string) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "SQLSTATE 23505") {
		return invalid("%s", msg)
	}
	return err
}
