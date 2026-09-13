package editing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

type Engine struct {
	db  *gorm.DB
	reg *Registry
}

func NewEngine(db *gorm.DB, reg *Registry) *Engine {
	return &Engine{db: db, reg: reg}
}

func (e *Engine) Registry() *Registry { return e.reg }

func (e *Engine) CountProposalsSince(ctx context.Context, proposerUID int64, since time.Time) (int64, error) {
	var n int64
	err := e.db.WithContext(ctx).Model(&Proposal{}).
		Where("proposer_uid = ? AND created_at > ?", proposerUID, since).
		Count(&n).Error
	return n, err
}

func (e *Engine) resolveSpec(entityType string) (*EntityTypeSpec, error) {
	spec, ok := e.reg.Type(entityType)
	if !ok {
		return nil, ErrUnknownEntityType
	}
	return spec, nil
}

func (e *Engine) afterMerge(ctx context.Context, rev *Revision) {
	if rev == nil {
		return
	}
	spec, ok := e.reg.Type(rev.EntityType)
	if !ok || spec.OnMerge == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("editing: OnMerge hook panicked",
				"entity_type", rev.EntityType, "entity_id", rev.EntityID, "panic", r)
		}
	}()
	if err := spec.OnMerge(ctx, MergeEvent{
		EntityID: rev.EntityID, ActorUID: rev.ActorUID, AmenderUID: rev.AmenderUID, Action: rev.Action,
	}); err != nil {
		slog.Warn("editing: OnMerge hook failed",
			"entity_type", rev.EntityType, "entity_id", rev.EntityID, "err", err)
	}
}

func (e *Engine) ownerSite(ctx context.Context, spec *EntityTypeSpec, entityID int64, needed bool) (*string, error) {
	if !needed {
		return nil, nil
	}
	owner, err := spec.OwnerSite(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("editing: owner site for %s/%d: %w", spec.Type, entityID, err)
	}
	return owner, nil
}

func (e *Engine) deriveOwnership(ctx context.Context, spec *EntityTypeSpec, entityID int64, pc *PolicyContext) error {
	if pc.IsEntityOwner || pc.UserID == 0 || entityID == 0 || spec.OwnerUserID == nil {
		return nil
	}
	owner, err := spec.OwnerUserID(ctx, entityID)
	if err != nil {
		return fmt.Errorf("editing: owner user for %s/%d: %w", spec.Type, entityID, err)
	}
	if owner != nil && *owner == pc.UserID {
		pc.IsEntityOwner = true
	}
	return nil
}

func (e *Engine) CurrentSnapshot(ctx context.Context, entityType string, entityID int64) (map[string]any, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return nil, err
	}
	return spec.LoadSnapshot(ctx, entityID)
}

func (e *Engine) GetProposal(ctx context.Context, id int64) (*Proposal, []ProposalAmendment, map[string]any, error) {
	var p Proposal
	if err := e.db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrProposalNotFound
		}
		return nil, nil, nil, err
	}
	amendments, err := loadAmendments(e.db.WithContext(ctx), id)
	if err != nil {
		return nil, nil, nil, err
	}
	eff, _, err := effectivePatch(&p, amendments)
	if err != nil {
		return nil, nil, nil, err
	}
	return &p, amendments, eff, nil
}

type ProposalFilter struct {
	EntityType  string
	EntityID    int64
	Site        string
	ProposerUID int64
	Status      int16
	Limit       int
	BeforeID    int64
}

func (e *Engine) ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error) {
	items, _, err := e.ListProposalsWithTotal(ctx, f)
	return items, err
}

func (e *Engine) ListProposalsWithTotal(ctx context.Context, f ProposalFilter) ([]Proposal, int64, error) {
	q := e.db.WithContext(ctx).Model(&Proposal{})
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != 0 {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if f.Site != "" {
		q = q.Where("site = ?", f.Site)
	}
	if f.ProposerUID != 0 {
		q = q.Where("proposer_uid = ?", f.ProposerUID)
	}
	if f.Status >= 0 {
		q = q.Where("status = ?", f.Status)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// The cursor predicate is added after the count on purpose: total is the
	// size of the filtered collection, not of the page's remainder. Counting
	// the cursor-narrowed query made total shrink on every page, which is what
	// deviation 10 says it must not do.
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := q.Session(&gorm.Session{})
	if f.BeforeID > 0 {
		page = page.Where("id < ?", f.BeforeID)
	}
	var out []Proposal
	if err := page.Order("id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (e *Engine) RevisionByID(ctx context.Context, id int64) (*Revision, error) {
	var rev Revision
	err := e.db.WithContext(ctx).First(&rev, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRevisionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (e *Engine) ListRevisions(ctx context.Context, entityType string, entityID int64, limit int) ([]Revision, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []Revision
	if err := e.db.WithContext(ctx).
		Where("entity_family = ? AND entity_type = ? AND entity_id = ?", spec.Family, spec.Type, entityID).
		Order("seq DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type RevisionFeedFilter struct {
	Since        int64
	EntityFamily string
	EntityType   string
	Site         string
	Limit        int
}

func (e *Engine) RevisionsSince(ctx context.Context, f RevisionFeedFilter) ([]Revision, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := e.db.WithContext(ctx).Model(&Revision{}).Where("id > ?", f.Since)
	if f.EntityFamily != "" {
		q = q.Where("entity_family = ?", f.EntityFamily)
	}
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.Site != "" {
		q = q.Where("site = ?", f.Site)
	}
	var out []Revision
	if err := q.Order("id ASC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type FieldDiff struct {
	Key      string
	Kind     FieldKind
	DiffHint string
	From     any
	To       any
}

func (e *Engine) Diff(ctx context.Context, entityType string, entityID int64, fromSeq, toSeq int) ([]FieldDiff, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return nil, err
	}
	from, err := e.revisionAt(ctx, spec, entityID, fromSeq)
	if err != nil {
		return nil, err
	}
	to, err := e.revisionAt(ctx, spec, entityID, toSeq)
	if err != nil {
		return nil, err
	}
	fromSnap, err := decodeObject(from.Snapshot)
	if err != nil {
		return nil, err
	}
	toSnap, err := decodeObject(to.Snapshot)
	if err != nil {
		return nil, err
	}
	union := make(map[string]struct{}, len(fromSnap)+len(toSnap))
	for k := range fromSnap {
		union[k] = struct{}{}
	}
	for k := range toSnap {
		union[k] = struct{}{}
	}
	var diffs []FieldDiff
	for _, key := range sortedKeys(union) {
		a, b := fromSnap[key], toSnap[key]
		if jsonValueEqual(a, b) {
			continue
		}
		d := FieldDiff{Key: key, From: a, To: b}
		if f, ok := spec.Field(key); ok {
			d.Kind, d.DiffHint = f.Kind, f.DiffHint
		}
		diffs = append(diffs, d)
	}
	return diffs, nil
}

func (e *Engine) revisionAt(ctx context.Context, spec *EntityTypeSpec, entityID int64, seq int) (*Revision, error) {
	var rev Revision
	err := e.db.WithContext(ctx).
		Where("entity_family = ? AND entity_type = ? AND entity_id = ? AND seq = ?",
			spec.Family, spec.Type, entityID, seq).
		First(&rev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRevisionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

type FieldProjection struct {
	Key            string
	Kind           FieldKind
	DiffHint       string
	Deprecated     bool
	Locked         bool
	CanPropose     bool
	CanReview      bool
	WouldAutomerge bool
	MaxSuppressed  int
	MaxElements    int
}

func (e *Engine) SchemaProjection(ctx context.Context, entityType string, entityID int64, pc PolicyContext) ([]FieldProjection, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return nil, err
	}
	if err := e.deriveOwnership(ctx, spec, entityID, &pc); err != nil {
		return nil, err
	}
	needOwner := false
	if entityID != 0 {
		for i := range spec.Fields {
			if spec.EffectivePolicy(spec.Fields[i].Key, pc.Site).Automerge == AutomergeOwner {
				needOwner = true
				break
			}
		}
	}
	owner, err := e.ownerSite(ctx, spec, entityID, needOwner)
	if err != nil {
		return nil, err
	}
	out := make([]FieldProjection, 0, len(spec.Fields))
	for i := range spec.Fields {
		f := &spec.Fields[i]
		pol := spec.EffectivePolicy(f.Key, pc.Site)
		proj := FieldProjection{
			Key: f.Key, Kind: f.Kind, DiffHint: f.DiffHint, Deprecated: f.Deprecated,
			Locked:        pol.Propose == ProposeLocked,
			CanReview:     pol.AllowsReview(pc),
			MaxSuppressed: f.MaxSuppressed,
		}
		if f.Kind == KindList {
			proj.MaxElements = f.MaxElements
			if proj.MaxElements <= 0 {
				proj.MaxElements = DefaultMaxElements
			}
		}
		if !f.Deprecated && !proj.Locked {
			proj.CanPropose = pol.AllowsPropose(pc)
			proj.WouldAutomerge = proj.CanPropose && pol.allowsAutomergeWithOwner(pc, owner)
		}
		out = append(out, proj)
	}
	return out, nil
}

func loadAmendments(db *gorm.DB, proposalID int64) ([]ProposalAmendment, error) {
	var out []ProposalAmendment
	if err := db.Where("proposal_id = ?", proposalID).Order("seq ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func maxRevisionSeq(db *gorm.DB, spec *EntityTypeSpec, entityID int64) (int, error) {
	var seq int
	err := db.Model(&Revision{}).
		Where("entity_family = ? AND entity_type = ? AND entity_id = ?", spec.Family, spec.Type, entityID).
		Select("COALESCE(MAX(seq), 0)").Scan(&seq).Error
	if err != nil {
		return 0, fmt.Errorf("editing: max revision seq: %w", err)
	}
	return seq, nil
}
