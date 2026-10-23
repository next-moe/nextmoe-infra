package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"api/internal/platform/settings/keys"
	"api/internal/platform/trust/actrie"
	"api/internal/platform/trust/model"
	"api/internal/platform/trust/norm"

	"gorm.io/gorm"
)

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionHold  = "hold"
)

type TermService struct {
	db        *gorm.DB
	allowlist map[string]bool
	now       func() time.Time

	mu       sync.Mutex
	cache    *termSnapshot
	loaded   bool
	loadedAt time.Time
}

type activeTerm struct {
	norm   string
	site   string
	banned bool
	lead   bool
	trail  bool
}

func isASCIIAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// matchesWithBoundary re-checks an automaton hit for the boundary its ends
// demand. Terms with no ASCII-alnum end demand nothing and short-circuit.
//
// Substring-only matching measured at 1.58% aggregate precision on 2026-08-09:
// `ice` fired on "service", `master` on "mastercard", `info` on "information",
// and the numeric terms landed inside longer IDs. Those terms read as bad terms
// on the precision report; they were a bad matcher.
func (t *activeTerm) matchesWithBoundary(text string) bool {
	if !t.lead && !t.trail {
		return true
	}
	for i := 0; i <= len(text)-len(t.norm); {
		j := strings.Index(text[i:], t.norm)
		if j < 0 {
			return false
		}
		s, e := i+j, i+j+len(t.norm)
		leadOK := !t.lead || s == 0 || !isASCIIAlnum(text[s-1])
		trailOK := !t.trail || e == len(text) || !isASCIIAlnum(text[e])
		if leadOK && trailOK {
			return true
		}
		i = s + 1
	}
	return false
}

type termSnapshot struct {
	terms   []activeTerm
	matcher *actrie.Matcher
}

func buildSnapshot(terms []activeTerm) *termSnapshot {
	patterns := make([][]byte, len(terms))
	for i := range terms {
		t := &terms[i]
		if t.norm != "" {
			t.lead = isASCIIAlnum(t.norm[0])
			t.trail = isASCIIAlnum(t.norm[len(t.norm)-1])
		}
		patterns[i] = []byte(t.norm)
	}
	return &termSnapshot{terms: terms, matcher: actrie.Build(patterns)}
}

func NewTermService(db *gorm.DB, allowlist map[string]bool) *TermService {
	return &TermService{db: db, allowlist: allowlist, now: time.Now}
}

func (s *TermService) allowed(clientID string) bool {
	return clientID != "" && s.allowlist[clientID]
}

type CheckParams struct {
	CallerClientID string
	Site           string
	WireSite       string
	Text           string
	AuthorID       *int64
}

type CheckResult struct {
	Decision string
	Matched  []string
}

func (s *TermService) Check(ctx context.Context, p CheckParams) (CheckResult, error) {
	site := p.Site
	if p.WireSite != "" {
		if !s.allowed(p.CallerClientID) {
			return CheckResult{}, ErrForwarderNotAllowed
		}
		site = p.WireSite
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	decision, matched := snap.match(site, norm.Normalize(p.Text))
	return CheckResult{Decision: decision, Matched: matched}, nil
}

func (s *TermService) Tier0Matches(ctx context.Context, site, text string) (string, []string, error) {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return "", nil, err
	}
	decision, matched := snap.match(site, norm.Normalize(text))
	return decision, matched, nil
}

func (snap *termSnapshot) match(site, normText string) (string, []string) {
	matched := []string{}
	deny, hold := false, false
	for _, i := range snap.matcher.Match([]byte(normText)) {
		t := &snap.terms[i]
		if t.site != "" && t.site != site {
			continue
		}
		if !t.matchesWithBoundary(normText) {
			continue
		}
		matched = append(matched, t.norm)
		if t.banned {
			deny = true
		} else {
			hold = true
		}
	}
	switch {
	case deny:
		return DecisionDeny, matched
	case hold:
		return DecisionHold, matched
	default:
		return DecisionAllow, matched
	}
}

func (s *TermService) snapshot(ctx context.Context) (*termSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded && s.now().Sub(s.loadedAt) < time.Duration(keys.TrustTermCacheTTLSeconds.Get())*time.Second {
		return s.cache, nil
	}
	var rows []model.TrustTerm
	if err := s.db.WithContext(ctx).Where("is_deprecated = false").Find(&rows).Error; err != nil {
		return nil, err
	}
	terms := make([]activeTerm, 0, len(rows))
	for _, r := range rows {
		site := ""
		if r.Site != nil {
			site = *r.Site
		}
		terms = append(terms, activeTerm{norm: r.TermNorm, site: site, banned: r.Kind == model.TermKindBanned})
	}
	snap := buildSnapshot(terms)
	s.cache = snap
	s.loaded = true
	s.loadedAt = s.now()
	return snap, nil
}

func (s *TermService) invalidate() {
	s.mu.Lock()
	s.loaded = false
	s.mu.Unlock()
}

type TermFilters struct {
	Site              string
	Kind              *int16
	Purpose           *int16
	IncludeDeprecated bool
	Query             string
	Page              int
	Limit             int
}

func (s *TermService) List(ctx context.Context, f TermFilters) ([]model.TrustTerm, int64, error) {
	q := s.db.WithContext(ctx).Model(&model.TrustTerm{})
	if f.Site != "" {
		q = q.Where("site = ?", f.Site)
	}
	if f.Kind != nil {
		q = q.Where("kind = ?", *f.Kind)
	}
	if f.Purpose != nil {
		q = q.Where("purpose = ?", *f.Purpose)
	}
	if !f.IncludeDeprecated {
		q = q.Where("is_deprecated = false")
	}
	if needle := norm.Normalize(f.Query); needle != "" {
		q = q.Where("term_norm LIKE ?", "%"+escapeLike(needle)+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := clampLimit(f.Limit)
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}
	var terms []model.TrustTerm
	if err := q.Order("site NULLS FIRST").Order("term_norm ASC").Order("id ASC").
		Limit(limit).Offset(offset).Find(&terms).Error; err != nil {
		return nil, 0, err
	}
	return terms, total, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

type CreateTermParams struct {
	ActorID int64
	Site    *string
	Term    string
	Kind    int16
	Purpose int16
	Note    *string
}

func (s *TermService) Create(ctx context.Context, p CreateTermParams) (*model.TrustTerm, error) {
	if p.Kind != model.TermKindSuspect && p.Kind != model.TermKindBanned {
		return nil, ErrTermInvalidKind
	}
	if p.Purpose != model.TermPurposeAbuse && p.Purpose != model.TermPurposeCompliance {
		return nil, ErrTermInvalidPurpose
	}
	site := p.Site
	if site != nil && strings.TrimSpace(*site) == "" {
		site = nil
	}
	normTerm := norm.Normalize(p.Term)
	if normTerm == "" {
		return nil, ErrTermEmpty
	}
	term := model.TrustTerm{
		Site: site, TermNorm: normTerm, Kind: p.Kind, Purpose: p.Purpose,
		Note: p.Note, IsDeprecated: false,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.TrustTerm{}).
			Where("COALESCE(site, '') = COALESCE(?, '') AND term_norm = ? AND is_deprecated = false", site, normTerm).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrTermExists
		}
		if err := tx.Create(&term).Error; err != nil {
			if isTermDuplicate(err) {
				return ErrTermExists
			}
			return err
		}
		return AppendAudit(tx, AuditEntry{ActorID: &p.ActorID, Action: "term_created", Site: site})
	})
	if err != nil {
		return nil, err
	}
	s.invalidate()
	return &term, nil
}

func (s *TermService) Deprecate(ctx context.Context, actorID, id int64) (*model.TrustTerm, error) {
	var term model.TrustTerm
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lerr := tx.Take(&term, id).Error
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return ErrTermNotFound
		}
		if lerr != nil {
			return lerr
		}
		if !term.IsDeprecated {
			if err := tx.Model(&model.TrustTerm{}).Where("id = ?", id).Update("is_deprecated", true).Error; err != nil {
				return err
			}
		}
		if err := tx.Take(&term, id).Error; err != nil {
			return err
		}
		return AppendAudit(tx, AuditEntry{ActorID: &actorID, Action: "term_deprecated", Site: term.Site})
	})
	if err != nil {
		return nil, err
	}
	s.invalidate()
	return &term, nil
}

func isTermDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "uq_trust_term_active") ||
		strings.Contains(msg, "duplicate key value")
}
