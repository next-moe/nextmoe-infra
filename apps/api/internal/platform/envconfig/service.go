package envconfig

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"api/pkg/config"
	"api/pkg/secretbox"
)

const (
	KindBootstrap = "bootstrap"
	KindTopology  = "topology"
	KindPanel     = "panel"
	KindStorable  = "storable"
)

const (
	IssueEnvRedundant = "env_redundant"
	IssueEnvShadows   = "env_shadows"
	IssueRestart      = "restart_pending"
	IssueKEKMissing   = "kek_missing"
)

var extraScopes = []string{"migrate", config.ScopeTools}

type ServiceView struct {
	Name        string   `json:"name"`
	Hostname    string   `json:"hostname"`
	StartedAt   string   `json:"started_at"`
	StoreLoaded bool     `json:"store_loaded"`
	Unread      []string `json:"unread"`
}

type ServiceValue struct {
	Service     string        `json:"service"`
	Source      config.Source `json:"source"`
	Set         bool          `json:"set"`
	Value       string        `json:"value,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"`
	Issue       string        `json:"issue,omitempty"`
}

type StoredView struct {
	Scope         string `json:"scope"`
	Value         string `json:"value,omitempty"`
	Hint          string `json:"hint,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	Version       int64  `json:"version"`
	UpdatedByName string `json:"updated_by_name"`
	Note          string `json:"note"`
	UpdatedAt     string `json:"updated_at"`
}

type VarView struct {
	Key      string         `json:"key"`
	Secret   bool           `json:"secret"`
	Kind     string         `json:"kind"`
	Default  string         `json:"default,omitempty"`
	Services []ServiceValue `json:"services"`
	Stored   []StoredView   `json:"stored"`
	Issues   []string       `json:"issues"`
	Diverged bool           `json:"diverged"`
}

type Overview struct {
	Writable      bool          `json:"writable"`
	KEKConfigured bool          `json:"kek_configured"`
	Scopes        []string      `json:"scopes"`
	Services      []ServiceView `json:"services"`
	Vars          []VarView     `json:"vars"`
}

type AuditEntry struct {
	ID             uint   `json:"id"`
	ActorUserID    uint   `json:"actor_user_id"`
	ActorName      string `json:"actor_name"`
	Action         string `json:"action"`
	Scope          string `json:"scope"`
	Key            string `json:"key"`
	OldFingerprint string `json:"old_fingerprint"`
	NewFingerprint string `json:"new_fingerprint"`
	OldValue       string `json:"old_value"`
	NewValue       string `json:"new_value"`
	Note           string `json:"note"`
	CreatedAt      string `json:"created_at"`
}

type Service struct {
	store *Store
	kek   func() []byte
}

func NewService(store *Store) *Service {
	return &Service{store: store, kek: config.StoreKey}
}

type snapshot struct {
	BootSnapshot
	entries map[string]config.Entry
}

func (s *Service) load(ctx context.Context) ([]snapshot, []SecretRow, error) {
	rows, err := s.store.Snapshots(ctx)
	if err != nil {
		return nil, nil, err
	}
	snaps := make([]snapshot, 0, len(rows))
	for _, r := range rows {
		var list []config.Entry
		if err := json.Unmarshal(r.Entries, &list); err != nil {
			return nil, nil, err
		}
		sn := snapshot{BootSnapshot: r, entries: make(map[string]config.Entry, len(list))}
		for _, e := range list {
			sn.entries[e.Key] = e
		}
		snaps = append(snaps, sn)
	}
	secrets, err := s.store.Secrets(ctx)
	return snaps, secrets, err
}

func (s *Service) Overview(ctx context.Context, writable bool) (*Overview, error) {
	snaps, secrets, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	kek := s.kek()
	out := &Overview{
		Writable:      writable && kek != nil,
		KEKConfigured: kek != nil,
		Scopes:        scopes(snaps),
		Services:      make([]ServiceView, 0, len(snaps)),
	}
	for _, sn := range snaps {
		var unread []string
		_ = json.Unmarshal(sn.Unread, &unread)
		out.Services = append(out.Services, ServiceView{
			Name: sn.Service, Hostname: sn.Hostname, StartedAt: sn.StartedAt.Format(time.RFC3339),
			StoreLoaded: sn.StoreLoaded, Unread: unread,
		})
	}

	stored := map[string][]SecretRow{}
	for _, r := range secrets {
		stored[r.Key] = append(stored[r.Key], r)
	}
	for _, key := range universe(snaps, secrets) {
		out.Vars = append(out.Vars, buildVar(key, snaps, stored[key], kek))
	}
	return out, nil
}

func buildVar(key string, snaps []snapshot, rows []SecretRow, kek []byte) VarView {
	secret := config.IsSecret(key)
	v := VarView{Key: key, Secret: secret, Kind: kindOf(key), Services: []ServiceValue{}, Stored: []StoredView{}, Issues: []string{}}

	plain := map[string]string{}
	byScope := map[string]SecretRow{}
	for _, r := range rows {
		byScope[r.Scope] = r
		sv := StoredView{
			Scope: r.Scope, Hint: r.Hint, Fingerprint: r.Fingerprint, Version: r.Version,
			UpdatedByName: r.UpdatedByName, Note: r.Note, UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
		}
		if !secret && kek != nil {
			if p, err := secretbox.Open(kek, r.Cipher); err == nil {
				sv.Value, plain[r.Scope] = p, p
			}
		}
		v.Stored = append(v.Stored, sv)
	}

	seen := map[string]bool{}
	issues := map[string]bool{}
	for _, sn := range snaps {
		e, ok := sn.entries[key]
		if !ok {
			continue
		}
		if v.Default == "" && !secret {
			v.Default = e.Default
		}
		val := ServiceValue{Service: sn.Service, Source: e.Source, Set: e.Set, Value: e.Value, Fingerprint: e.Fingerprint}
		row, has := byScope[sn.Service]
		if !has {
			row, has = byScope[""]
		}
		if has {
			val.Issue = issueFor(sn, e, row, plain[row.Scope], secret)
			if val.Issue != "" {
				issues[val.Issue] = true
			}
		}
		if e.Set {
			seen[identity(e, secret)] = true
		}
		v.Services = append(v.Services, val)
	}
	v.Diverged = len(seen) > 1
	for i := range issues {
		v.Issues = append(v.Issues, i)
	}
	sort.Strings(v.Issues)
	return v
}

func issueFor(sn snapshot, e config.Entry, row SecretRow, plain string, secret bool) string {
	same := e.Fingerprint == row.Fingerprint
	if !secret {
		same = e.Value == plain
	}
	switch {
	case e.Source == config.SourceEnv && same:
		return IssueEnvRedundant
	case e.Source == config.SourceEnv:
		return IssueEnvShadows
	case !sn.StoreLoaded:
		return IssueKEKMissing
	case row.UpdatedAt.After(sn.StartedAt) || !same:
		return IssueRestart
	}
	return ""
}

func identity(e config.Entry, secret bool) string {
	if secret {
		return e.Fingerprint
	}
	return e.Value
}

func kindOf(key string) string {
	switch {
	case config.IsBootstrap(key):
		return KindBootstrap
	case config.IsTopology(key):
		return KindTopology
	case config.IsPanelOnly(key):
		return KindPanel
	}
	return KindStorable
}

func universe(snaps []snapshot, secrets []SecretRow) []string {
	set := map[string]bool{}
	for _, sn := range snaps {
		for k := range sn.entries {
			set[k] = true
		}
	}
	for _, r := range secrets {
		set[r.Key] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func scopes(snaps []snapshot) []string {
	out := []string{""}
	for _, sn := range snaps {
		out = append(out, sn.Service)
	}
	return append(out, extraScopes...)
}

func (s *Service) Set(ctx context.Context, actorID uint, scope, key, value, note string, version *int64) error {
	if err := s.check(ctx, scope, key); err != nil {
		return err
	}
	_, err := s.store.Put(ctx, s.kek(), PutInput{
		ActorID: actorID, Action: ActionSet, Scope: scope, Key: key, Value: value, Note: note, ExpectVersion: version,
	})
	return err
}

func (s *Service) Reset(ctx context.Context, actorID uint, scope, key, note string) error {
	return s.store.Delete(ctx, s.kek(), actorID, scope, key, note)
}

func (s *Service) check(ctx context.Context, scope, key string) error {
	snaps, secrets, err := s.load(ctx)
	if err != nil {
		return err
	}
	valid := false
	for _, sc := range scopes(snaps) {
		valid = valid || sc == scope
	}
	if !valid {
		return ErrUnknownScope
	}
	for _, k := range universe(snaps, secrets) {
		if k == key {
			return nil
		}
	}
	return ErrUnknownVar
}

func (s *Service) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.store.Audit(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, AuditEntry{
			ID: r.ID, ActorUserID: r.ActorUserID, ActorName: r.ActorName, Action: r.Action,
			Scope: r.Scope, Key: r.Key, OldFingerprint: r.OldFingerprint, NewFingerprint: r.NewFingerprint,
			OldValue: r.OldValue, NewValue: r.NewValue, Note: r.Note, CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}
