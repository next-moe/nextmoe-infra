package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

var (
	ErrUnknownKey    = errors.New("settings: unknown key")
	ErrInvalidValue  = errors.New("settings: invalid value")
	ErrNoteTooLong   = errors.New("settings: note exceeds 512 characters")
	ErrNotSiteScoped = errors.New("settings: key does not accept a site override")
)

type OverrideView struct {
	Value           any    `json:"value"`
	Version         int64  `json:"version"`
	UpdatedByUserID uint   `json:"updated_by_user_id"`
	UpdatedByName   string `json:"updated_by_name"`
	Note            string `json:"note"`
	UpdatedAt       string `json:"updated_at"`
}

type KeyView struct {
	Key        string        `json:"key"`
	Kind       Kind          `json:"kind"`
	DescEN     string        `json:"desc_en"`
	DescZH     string        `json:"desc_zh"`
	Default    any           `json:"default"`
	EnvVar     string        `json:"env_var,omitempty"`
	Enum       []string      `json:"enum,omitempty"`
	Min        *float64      `json:"min,omitempty"`
	Max        *float64      `json:"max,omitempty"`
	Pattern    string        `json:"pattern,omitempty"`
	Multiline  bool          `json:"multiline,omitempty" doc:"A long text such as a prompt; edit it in a text area."`
	SiteScoped bool          `json:"site_scoped"`
	Public     bool          `json:"public"`
	Effective  any           `json:"effective"`
	Source     Source        `json:"source"`
	Override   *OverrideView `json:"override"`
	Inherited  any           `json:"inherited,omitempty"`
}

type DomainView struct {
	Name    string    `json:"name"`
	TitleZH string    `json:"title_zh"`
	Keys    []KeyView `json:"keys"`
}

type ScopeView struct {
	Kind   string `json:"kind"`
	SiteID *uint  `json:"site_id,omitempty"`
}

type Overview struct {
	Scope    ScopeView    `json:"scope"`
	Domains  []DomainView `json:"domains"`
	Writable bool         `json:"writable"`
}

type EffectiveView struct {
	SiteID   *uint          `json:"site_id"`
	ETag     string         `json:"etag"`
	Settings map[string]any `json:"settings"`
}

type Service struct {
	reg   *Registry
	store *Store
	dist  *Distributor
}

func NewService(reg *Registry, store *Store, dist *Distributor) *Service {
	return &Service{reg: reg, store: store, dist: dist}
}

func (s *Service) Overview(ctx context.Context, writable bool, scope Scope) (*Overview, error) {
	platformRows, err := s.overrideByKey(ctx, PlatformScope)
	if err != nil {
		return nil, err
	}
	var siteRows map[string]*OverrideRow
	if !scope.IsPlatform() {
		siteRows, err = s.overrideByKey(ctx, scope)
		if err != nil {
			return nil, err
		}
	}
	out := &Overview{Scope: scopeView(scope), Writable: writable}
	for _, d := range s.reg.Domains() {
		view := DomainView{Name: d.Name, TitleZH: d.TitleZH, Keys: make([]KeyView, 0, len(d.Keys))}
		for _, e := range d.Keys {
			if !scope.IsPlatform() && !e.Meta().SiteScoped {
				continue
			}
			v := keyView(e, platformRows[e.Meta().Name])
			if !scope.IsPlatform() {
				v = applySiteOverride(e, v, siteRows[e.Meta().Name])
			}
			view.Keys = append(view.Keys, v)
		}
		if !scope.IsPlatform() && len(view.Keys) == 0 {
			continue
		}
		out.Domains = append(out.Domains, view)
	}
	return out, nil
}

func (s *Service) Set(ctx context.Context, actorID uint, scope Scope, key string, raw json.RawMessage, note string, expectVersion *int64) (*KeyView, error) {
	e, ok := s.reg.Lookup(key)
	if !ok {
		return nil, ErrUnknownKey
	}
	if !scope.IsPlatform() && !e.Meta().SiteScoped {
		return nil, ErrNotSiteScoped
	}
	if utf8.RuneCountInString(note) > 512 {
		return nil, ErrNoteTooLong
	}
	v, err := e.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	if err := e.Validate(v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	if _, err := s.store.Set(ctx, scope, key, raw, note, expectVersion, actorID); err != nil {
		return nil, err
	}
	if err := s.dist.Refresh(ctx); err != nil {
		return nil, err
	}
	s.dist.Announce(ctx)
	return s.viewFor(ctx, e, scope)
}

func (s *Service) Reset(ctx context.Context, actorID uint, scope Scope, key string, note string) (*KeyView, error) {
	e, ok := s.reg.Lookup(key)
	if !ok {
		return nil, ErrUnknownKey
	}
	if !scope.IsPlatform() && !e.Meta().SiteScoped {
		return nil, ErrNotSiteScoped
	}
	if utf8.RuneCountInString(note) > 512 {
		return nil, ErrNoteTooLong
	}
	if err := s.store.Reset(ctx, scope, key, note, actorID); err != nil {
		return nil, err
	}
	if err := s.dist.Refresh(ctx); err != nil {
		return nil, err
	}
	s.dist.Announce(ctx)
	return s.viewFor(ctx, e, scope)
}

func (s *Service) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	return s.store.RecentAudit(ctx, limit)
}

func (s *Service) Effective(ctx context.Context, siteID *uint) (*EffectiveView, error) {
	out := make(map[string]any)
	for _, e := range s.reg.Entries() {
		if !e.Meta().Public {
			continue
		}
		out[e.Meta().Name] = e.Current()
	}
	if siteID != nil {
		rows, err := s.store.Values(ctx, SiteScope(*siteID))
		if err != nil {
			return nil, err
		}
		for _, e := range s.reg.Entries() {
			m := e.Meta()
			if !m.Public || !m.SiteScoped {
				continue
			}
			raw, ok := rows[m.Name]
			if !ok {
				continue
			}
			v, err := e.Decode(raw)
			if err != nil {
				continue
			}
			if err := e.Validate(v); err != nil {
				continue
			}
			out[m.Name] = v
		}
	}
	return &EffectiveView{
		SiteID:   siteID,
		ETag:     etagFor(out),
		Settings: out,
	}, nil
}

func etagFor(settings map[string]any) string {
	b, _ := json.Marshal(settings)
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func (s *Service) overrideByKey(ctx context.Context, scope Scope) (map[string]*OverrideRow, error) {
	rows, err := s.store.Overrides(ctx, scope)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*OverrideRow, len(rows))
	for i := range rows {
		byKey[rows[i].Key] = &rows[i]
	}
	return byKey, nil
}

func (s *Service) viewFor(ctx context.Context, e Entry, scope Scope) (*KeyView, error) {
	platformRows, err := s.overrideByKey(ctx, PlatformScope)
	if err != nil {
		return nil, err
	}
	v := keyView(e, platformRows[e.Meta().Name])
	if scope.IsPlatform() {
		return &v, nil
	}
	siteRows, err := s.overrideByKey(ctx, scope)
	if err != nil {
		return nil, err
	}
	v = applySiteOverride(e, v, siteRows[e.Meta().Name])
	return &v, nil
}

func scopeView(scope Scope) ScopeView {
	if scope.IsPlatform() {
		return ScopeView{Kind: ScopePlatform}
	}
	id, _ := strconv.ParseUint(scope.ID, 10, 64)
	u := uint(id)
	return ScopeView{Kind: ScopeSite, SiteID: &u}
}

func keyView(e Entry, row *OverrideRow) KeyView {
	m := e.Meta()
	v := KeyView{
		Key:        m.Name,
		Kind:       m.Kind,
		DescEN:     m.DescEN,
		DescZH:     m.DescZH,
		Default:    e.Default(),
		EnvVar:     m.EnvVar,
		Enum:       m.Enum,
		Min:        m.Min,
		Max:        m.Max,
		Pattern:    m.Pattern,
		Multiline:  m.Multiline,
		SiteScoped: m.SiteScoped,
		Public:     m.Public,
		Effective:  e.Default(),
		Source:     SourceDefault,
	}
	if row == nil {
		return v
	}
	ov, decoded, ok := overrideFromRow(e, row)
	v.Override = ov
	if ok {
		v.Effective = decoded
		v.Source = SourceDB
	}
	return v
}

func applySiteOverride(e Entry, v KeyView, row *OverrideRow) KeyView {
	v.Inherited = v.Effective
	if row == nil {
		v.Override = nil
		return v
	}
	ov, decoded, ok := overrideFromRow(e, row)
	v.Override = ov
	if ok {
		v.Effective = decoded
		v.Source = SourceSite
	}
	return v
}

func overrideFromRow(e Entry, row *OverrideRow) (*OverrideView, any, bool) {
	ov := &OverrideView{
		Version:         row.Version,
		UpdatedByUserID: row.UpdatedByUserID,
		UpdatedByName:   row.UpdatedByName,
		Note:            row.Note,
		UpdatedAt:       row.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	decoded, err := e.Decode(json.RawMessage(row.Value))
	if err == nil {
		ov.Value = decoded
		if e.Validate(decoded) == nil {
			return ov, decoded, true
		}
		return ov, decoded, false
	}
	if json.Valid(row.Value) {
		var dumped any
		_ = json.Unmarshal(row.Value, &dumped)
		ov.Value = dumped
	}
	return ov, nil, false
}
