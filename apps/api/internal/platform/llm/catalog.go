package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	CatalogVideo         = "video"
	CatalogSpeech        = "speech"
	CatalogTranscription = "transcription"
	CatalogRerank        = "rerank"

	kindFromMetadata = "metadata"
	kindFromName     = "name"
	kindFromAdmin    = "admin"
	kindFromProbe    = "probe"

	KindAuto = "auto"
)

var gatewayKinds = []string{ModelChat, ModelEmbedding, ModelModeration, ModelSearch, ModelImage}

var catalogKinds = append(slices.Clone(gatewayKinds), CatalogVideo, CatalogSpeech, CatalogTranscription, CatalogRerank)

// ErrCatalogUnsupported: the provider has no OpenAI-style model list. Workers
// AI's OpenAI-compatible base is one; its catalog lives in a separate API.
var ErrCatalogUnsupported = errors.New("llm catalog: the provider does not list its models at /models")

var nameRules = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`embed|(^|[/-])(bge|e5|gte)-`), ModelEmbedding},
	{regexp.MustCompile(`moderation`), ModelModeration},
	{regexp.MustCompile(`rerank`), CatalogRerank},
	{regexp.MustCompile(`whisper|transcri|(^|[/-])asr`), CatalogTranscription},
	{regexp.MustCompile(`(^|[/-])tts|speech`), CatalogSpeech},
	{regexp.MustCompile(`video`), CatalogVideo},
	{regexp.MustCompile(`imagine|image|dall-e|flux|stable-diffusion|sdxl|imagen|seedream`), ModelImage},
}

func guessKind(id string, out []string) (string, string) {
	switch {
	case slices.Contains(out, "embeddings"):
		return ModelEmbedding, kindFromMetadata
	case slices.Contains(out, "video"):
		return CatalogVideo, kindFromMetadata
	case slices.Contains(out, "image"):
		return ModelImage, kindFromMetadata
	case slices.Contains(out, "audio"):
		return CatalogSpeech, kindFromMetadata
	}
	lower := strings.ToLower(id)
	for _, r := range nameRules {
		if r.re.MatchString(lower) {
			return r.kind, kindFromName
		}
	}
	if len(out) > 0 {
		return ModelChat, kindFromMetadata
	}
	return ModelChat, kindFromName
}

type listedModel struct {
	ID           string
	OwnedBy      string
	In, Out      []string
	ContextLen   int
	Raw          json.RawMessage
	MetadataKind string
}

func parseModelList(body []byte) ([]listedModel, error) {
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	items := env.Data
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, ErrCatalogUnsupported
		}
	} else {
		items = env.Data
	}
	out := make([]listedModel, 0, len(items))
	for _, raw := range items {
		var m struct {
			ID           string `json:"id"`
			OwnedBy      string `json:"owned_by"`
			Type         string `json:"type"`
			Architecture struct {
				In  []string `json:"input_modalities"`
				Out []string `json:"output_modalities"`
			} `json:"architecture"`
			In             []string `json:"input_modalities"`
			Out            []string `json:"output_modalities"`
			ContextLength  int      `json:"context_length"`
			ContextWindow  int      `json:"context_window"`
			MaxInputTokens int      `json:"max_input_tokens"`
		}
		if json.Unmarshal(raw, &m) != nil || strings.TrimSpace(m.ID) == "" {
			continue
		}
		lm := listedModel{ID: m.ID, OwnedBy: m.OwnedBy, In: m.Architecture.In, Out: m.Architecture.Out, Raw: raw,
			ContextLen: max(m.ContextLength, m.ContextWindow, m.MaxInputTokens)}
		if len(lm.In) == 0 {
			lm.In = m.In
		}
		if len(lm.Out) == 0 {
			lm.Out = m.Out
		}
		if m.Type == "embedding" || m.Type == "embeddings" {
			lm.MetadataKind = ModelEmbedding
		}
		out = append(out, lm)
	}
	return out, nil
}

type CatalogProbe struct {
	Kind      string    `json:"kind"`
	OK        bool      `json:"ok"`
	LatencyMs int       `json:"latency_ms"`
	ErrorKind string    `json:"error_kind"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

type CatalogRoute struct {
	RouteID  int64  `json:"route_id"`
	ModelKey string `json:"model_key"`
	Status   string `json:"status" enum:"active,disabled"`
}

type ProviderModelView struct {
	ID               int64          `json:"id"`
	ProviderID       int64          `json:"provider_id"`
	UpstreamID       string         `json:"upstream_id"`
	Kind             string         `json:"kind" enum:"chat,embedding,moderation,search,image,video,speech,transcription,rerank"`
	KindSource       string         `json:"kind_source" enum:"metadata,name,admin,probe" doc:"Where the kind came from: the provider's metadata, a guess from the name, an admin, or a successful probe."`
	Supported        bool           `json:"supported" doc:"Whether the gateway can call this kind yet."`
	InputModalities  []string       `json:"input_modalities"`
	OutputModalities []string       `json:"output_modalities"`
	ContextTokens    int            `json:"context_tokens"`
	OwnedBy          string         `json:"owned_by"`
	Listed           bool           `json:"listed" doc:"False once the provider stopped listing it."`
	FirstSeenAt      time.Time      `json:"first_seen_at"`
	LastSeenAt       time.Time      `json:"last_seen_at"`
	DelistedAt       *time.Time     `json:"delisted_at"`
	Probe            *CatalogProbe  `json:"probe"`
	Routes           []CatalogRoute `json:"routes" doc:"Gateway routes already using this upstream id on this provider."`
}

func providerModelView(m ProviderModel, routes []CatalogRoute) ProviderModelView {
	v := ProviderModelView{ID: m.ID, ProviderID: m.ProviderID, UpstreamID: m.UpstreamID, Kind: m.Kind, KindSource: m.KindSource,
		Supported: slices.Contains(gatewayKinds, m.Kind), ContextTokens: m.ContextTokens, OwnedBy: m.OwnedBy, Listed: m.Listed,
		FirstSeenAt: m.FirstSeenAt, LastSeenAt: m.LastSeenAt, DelistedAt: m.DelistedAt, Routes: routes}
	_ = json.Unmarshal(m.InputModalities, &v.InputModalities)
	_ = json.Unmarshal(m.OutputModalities, &v.OutputModalities)
	if v.InputModalities == nil {
		v.InputModalities = []string{}
	}
	if v.OutputModalities == nil {
		v.OutputModalities = []string{}
	}
	if v.Routes == nil {
		v.Routes = []CatalogRoute{}
	}
	if len(m.Probe) > 0 {
		var p CatalogProbe
		if json.Unmarshal(m.Probe, &p) == nil {
			v.Probe = &p
		}
	}
	return v
}

func (a *Admin) ProviderModels(ctx context.Context, providerID int64) ([]ProviderModelView, error) {
	db := a.db.WithContext(ctx)
	var rows []ProviderModel
	if err := db.Where("provider_id = ?", providerID).Order("listed DESC, upstream_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	routes, err := a.catalogRoutes(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderModelView, len(rows))
	for i, r := range rows {
		out[i] = providerModelView(r, routes[r.UpstreamID])
	}
	return out, nil
}

func (a *Admin) catalogRoutes(ctx context.Context, providerID int64) (map[string][]CatalogRoute, error) {
	var rows []struct {
		ID         int64
		UpstreamID string
		Status     string
		Key        string
	}
	err := a.db.WithContext(ctx).Raw(`SELECT r.id, r.upstream_id, r.status, m.key FROM llm_route r JOIN llm_model m ON m.id = r.model_id
		WHERE r.provider_id = ? ORDER BY r.id`, providerID).Scan(&rows).Error
	out := map[string][]CatalogRoute{}
	for _, r := range rows {
		out[r.UpstreamID] = append(out[r.UpstreamID], CatalogRoute{RouteID: r.ID, ModelKey: r.Key, Status: r.Status})
	}
	return out, err
}

type SyncResult struct {
	Listed   int     `json:"listed"`
	Added    int     `json:"added"`
	Delisted int     `json:"delisted"`
	Relisted int     `json:"relisted"`
	Orphaned []int64 `json:"orphaned_routes" doc:"Routes whose upstream id the provider no longer lists."`
}

func (a *Admin) SyncProviderModels(ctx context.Context, providerID int64) (SyncResult, error) {
	var p Provider
	if err := a.db.WithContext(ctx).First(&p, providerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SyncResult{}, ErrNotFound
		}
		return SyncResult{}, err
	}
	listed, err := a.fetchModelList(ctx, &p)
	if err != nil {
		return SyncResult{}, err
	}
	now := a.now()
	res := SyncResult{Listed: len(listed), Orphaned: []int64{}}
	seen := map[string]bool{}
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []ProviderModel
		if err := tx.Where("provider_id = ?", p.ID).Find(&existing).Error; err != nil {
			return err
		}
		byID := map[string]*ProviderModel{}
		for i := range existing {
			byID[existing[i].UpstreamID] = &existing[i]
		}
		for _, lm := range listed {
			if seen[lm.ID] {
				continue
			}
			seen[lm.ID] = true
			kind, source := guessKind(lm.ID, lm.Out)
			if lm.MetadataKind != "" {
				kind, source = lm.MetadataKind, kindFromMetadata
			}
			in, _ := json.Marshal(stringsOrEmpty(lm.In))
			out, _ := json.Marshal(stringsOrEmpty(lm.Out))
			row := byID[lm.ID]
			if row == nil {
				res.Added++
				if err := tx.Create(&ProviderModel{ProviderID: p.ID, UpstreamID: lm.ID, Kind: kind, KindSource: source,
					InputModalities: in, OutputModalities: out, ContextTokens: lm.ContextLen, OwnedBy: lm.OwnedBy,
					Raw: datatypes.JSON(lm.Raw), Listed: true, FirstSeenAt: now, LastSeenAt: now}).Error; err != nil {
					return err
				}
				continue
			}
			updates := map[string]any{"input_modalities": datatypes.JSON(in), "output_modalities": datatypes.JSON(out),
				"context_tokens": lm.ContextLen, "owned_by": lm.OwnedBy, "raw": datatypes.JSON(lm.Raw), "listed": true,
				"last_seen_at": now, "delisted_at": nil}
			if row.KindSource != kindFromAdmin && row.KindSource != kindFromProbe {
				updates["kind"], updates["kind_source"] = kind, source
			}
			if !row.Listed {
				res.Relisted++
			}
			if err := tx.Model(row).Updates(updates).Error; err != nil {
				return err
			}
		}
		for _, row := range existing {
			if seen[row.UpstreamID] || !row.Listed {
				continue
			}
			res.Delisted++
			if err := tx.Model(&row).Updates(map[string]any{"listed": false, "delisted_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SyncResult{}, err
	}
	_ = a.db.WithContext(ctx).Model(&p).Update("catalog_synced_at", now).Error
	routes, err := a.catalogRoutes(ctx, p.ID)
	if err != nil {
		return res, err
	}
	for upstream, rs := range routes {
		if seen[upstream] {
			continue
		}
		for _, r := range rs {
			res.Orphaned = append(res.Orphaned, r.RouteID)
			if r.Status == RouteActive {
				_ = a.db.WithContext(ctx).Create(&RouteEvent{RouteID: r.RouteID, ProviderID: p.ID, Kind: "upstream_delisted",
					Detail: fmt.Sprintf("%s no longer appears in %s's model list", upstream, p.Name), CreatedAt: now}).Error
			}
		}
	}
	slices.Sort(res.Orphaned)
	_ = a.db.WithContext(ctx).Create(&RouteEvent{ProviderID: p.ID, Kind: "catalog_synced",
		Detail: fmt.Sprintf("%s: %d listed, %d new, %d gone, %d back", p.Name, res.Listed, res.Added, res.Delisted, res.Relisted), CreatedAt: now}).Error
	return res, nil
}

func stringsOrEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (a *Admin) fetchModelList(ctx context.Context, p *Provider) ([]listedModel, error) {
	key, err := providerKey(p, a.kek)
	if err != nil {
		return nil, invalid("provider key: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, invalid("base url: %v", err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	var headers map[string]string
	_ = json.Unmarshal(p.Headers, &headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := http.DefaultClient
	if a.gw != nil && a.gw.client != nil {
		client = a.gw.client
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models of %s: %w", p.Name, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
		return nil, ErrCatalogUnsupported
	case resp.StatusCode != http.StatusOK:
		return nil, classifyHTTP(resp.StatusCode, truncate(string(body), 300))
	}
	return parseModelList(body)
}

func (a *Admin) providerModel(ctx context.Context, id int64) (*ProviderModel, error) {
	var m ProviderModel
	err := a.db.WithContext(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &m, err
}

func (a *Admin) providerModelOut(ctx context.Context, m *ProviderModel) (ProviderModelView, error) {
	routes, err := a.catalogRoutes(ctx, m.ProviderID)
	if err != nil {
		return ProviderModelView{}, err
	}
	return providerModelView(*m, routes[m.UpstreamID]), nil
}

func (a *Admin) SetProviderModelKind(ctx context.Context, id int64, kind string) (ProviderModelView, error) {
	if kind != KindAuto && !slices.Contains(catalogKinds, kind) {
		return ProviderModelView{}, invalid("unknown kind %q", kind)
	}
	m, err := a.providerModel(ctx, id)
	if err != nil {
		return ProviderModelView{}, err
	}
	source := kindFromAdmin
	if kind == KindAuto {
		var out []string
		_ = json.Unmarshal(m.OutputModalities, &out)
		kind, source = guessKind(m.UpstreamID, out)
	}
	if err := a.db.WithContext(ctx).Model(m).Updates(map[string]any{"kind": kind, "kind_source": source}).Error; err != nil {
		return ProviderModelView{}, err
	}
	m.Kind, m.KindSource = kind, source
	return a.providerModelOut(ctx, m)
}

func (a *Admin) ProbeProviderModel(ctx context.Context, id int64, kind string) (ProviderModelView, error) {
	m, err := a.providerModel(ctx, id)
	if err != nil {
		return ProviderModelView{}, err
	}
	if kind == "" {
		kind = m.Kind
	}
	if !slices.Contains(gatewayKinds, kind) {
		return ProviderModelView{}, invalid("the gateway cannot call %s models yet", kind)
	}
	if a.gw == nil {
		return ProviderModelView{}, invalid("no gateway")
	}
	var p Provider
	if err := a.db.WithContext(ctx).First(&p, m.ProviderID).Error; err != nil {
		return ProviderModelView{}, err
	}
	res := a.gw.probeUpstream(ctx, &p, &Route{UpstreamID: m.UpstreamID, ProviderID: p.ID, Protocol: protocolFor(kind)}, &Model{Key: "catalog:" + m.UpstreamID, Kind: kind})
	probe, _ := json.Marshal(CatalogProbe{Kind: kind, OK: res.OK, LatencyMs: res.LatencyMs, ErrorKind: res.ErrorKind, Message: truncate(res.Message, 300), At: a.now()})
	updates := map[string]any{"probe": datatypes.JSON(probe)}
	if res.OK && m.KindSource != kindFromAdmin {
		updates["kind"], updates["kind_source"] = kind, kindFromProbe
		m.Kind, m.KindSource = kind, kindFromProbe
	}
	if err := a.db.WithContext(ctx).Model(m).Updates(updates).Error; err != nil {
		return ProviderModelView{}, err
	}
	m.Probe = probe
	return a.providerModelOut(ctx, m)
}

type AdoptInput struct {
	ModelKey *string `json:"model_key,omitempty" maxLength:"64" doc:"Gateway model to attach the route to; created when missing. Defaults to the upstream id, lower-cased with / turned into -."`
	Kind     *string `json:"kind,omitempty" enum:"chat,embedding,moderation,search,image" doc:"Defaults to the catalog kind."`
	Priority *int    `json:"priority,omitempty"`
	Weight   *int    `json:"weight,omitempty" minimum:"1" maximum:"10000"`
	Price    *Price  `json:"price,omitempty"`
}

type AdoptResult struct {
	Model        ModelView `json:"model"`
	Route        RouteView `json:"route"`
	ModelCreated bool      `json:"model_created"`
}

var adoptKeyStrip = regexp.MustCompile(`[^a-z0-9_.:-]+`)

func (a *Admin) AdoptProviderModel(ctx context.Context, id int64, in AdoptInput) (AdoptResult, error) {
	m, err := a.providerModel(ctx, id)
	if err != nil {
		return AdoptResult{}, err
	}
	kind := m.Kind
	if in.Kind != nil {
		kind = *in.Kind
	}
	if !slices.Contains(gatewayKinds, kind) {
		return AdoptResult{}, invalid("the gateway cannot call %s models yet", kind)
	}
	key := strings.Trim(adoptKeyStrip.ReplaceAllString(strings.ReplaceAll(strings.ToLower(m.UpstreamID), "/", "-"), "-"), "-")
	if in.ModelKey != nil {
		key = strings.TrimSpace(*in.ModelKey)
	}
	var existing Model
	err = a.db.WithContext(ctx).Where("key = ?", key).Take(&existing).Error
	out := AdoptResult{}
	switch {
	case err == nil:
		if existing.kind() != kind {
			return AdoptResult{}, invalid("model %s is a %s model, not %s", key, existing.kind(), kind)
		}
		out.Model = modelView(existing)
	case errors.Is(err, gorm.ErrRecordNotFound):
		var in []string
		_ = json.Unmarshal(m.InputModalities, &in)
		vision := slices.Contains(in, "image") && kind == ModelChat
		enabled := true
		mv, err := a.CreateModel(ctx, ModelInput{Key: &key, Kind: &kind, ContextTokens: &m.ContextTokens, Vision: &vision, Enabled: &enabled})
		if err != nil {
			return AdoptResult{}, err
		}
		out.Model, out.ModelCreated = mv, true
	default:
		return AdoptResult{}, err
	}
	rv, err := a.CreateRoute(ctx, RouteInput{ModelID: &out.Model.ID, ProviderID: &m.ProviderID, UpstreamID: &m.UpstreamID,
		Priority: in.Priority, Weight: in.Weight, Price: in.Price})
	if err != nil {
		return AdoptResult{}, err
	}
	out.Route = rv
	return out, nil
}

func (a *Admin) SyncAllProviders(ctx context.Context) {
	var ps []Provider
	if err := a.db.WithContext(ctx).Where("enabled").Order("id").Find(&ps).Error; err != nil {
		slog.Warn("llm catalog: list providers", "err", err)
		return
	}
	for _, p := range ps {
		res, err := a.SyncProviderModels(ctx, p.ID)
		switch {
		case errors.Is(err, ErrCatalogUnsupported):
		case err != nil:
			slog.Warn("llm catalog: sync", "provider", p.Name, "err", err)
		default:
			slog.Info("llm catalog: synced", "provider", p.Name, "listed", res.Listed, "new", res.Added, "gone", res.Delisted, "orphaned_routes", len(res.Orphaned))
		}
	}
}
