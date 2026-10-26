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

func (e *Engine) CountDirectEditsSince(ctx context.Context, actorUID int64, since time.Time) (int64, error) {
	var n int64
	err := e.db.WithContext(ctx).Model(&Revision{}).
		Where("actor_uid = ? AND action = ? AND created_at > ?", actorUID, ActionDirect, since).
		Count(&n).Error
	return n, err
}

func (e *Engine) CountOpenProposals(ctx context.Context, proposerUID int64) (int64, error) {
	var n int64
	err := e.db.WithContext(ctx).Model(&Proposal{}).
		Where("proposer_uid = ? AND status = ?", proposerUID, StatusOpen).
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
	for _, m := range rev.mirrored {
		e.afterMerge(ctx, m)
	}
	ev := MergeEvent{
		EntityID: rev.EntityID, ActorUID: rev.ActorUID, AmenderUID: rev.AmenderUID, Action: rev.Action,
	}
	if spec, ok := e.reg.Type(rev.EntityType); ok && spec.OnMerge != nil {
		guardHook(rev, func() {
			if err := spec.OnMerge(ctx, ev); err != nil {
				slog.Warn("editing: OnMerge hook failed",
					"entity_type", rev.EntityType, "entity_id", rev.EntityID, "err", err)
			}
		})
	}
	for _, observe := range e.reg.observers {
		guardHook(rev, func() { observe(ctx, rev.EntityType, ev) })
	}
}

func guardHook(rev *Revision, hook func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("editing: OnMerge hook panicked",
				"entity_type", rev.EntityType, "entity_id", rev.EntityID, "panic", r)
		}
	}()
	hook()
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

func (e *Engine) EffectivePatches(ctx context.Context, props []Proposal) (map[int64]map[string]any, error) {
	if len(props) == 0 {
		return map[int64]map[string]any{}, nil
	}
	ids := make([]int64, 0, len(props))
	for i := range props {
		ids = append(ids, props[i].ID)
	}
	var rows []ProposalAmendment
	if err := e.db.WithContext(ctx).Where("proposal_id IN ?", ids).
		Order("proposal_id ASC, seq ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	byProposal := make(map[int64][]ProposalAmendment, len(props))
	for _, r := range rows {
		byProposal[r.ProposalID] = append(byProposal[r.ProposalID], r)
	}
	out := make(map[int64]map[string]any, len(props))
	for i := range props {
		eff, _, err := effectivePatch(&props[i], byProposal[props[i].ID])
		if err != nil {
			return nil, err
		}
		out[props[i].ID] = eff
	}
	return out, nil
}

const (
	ProposalOrderFiledDesc   = ""
	ProposalOrderFiledAsc    = "filed_asc"
	ProposalOrderUpdatedDesc = "updated_desc"
	ProposalOrderUpdatedAsc  = "updated_asc"
)

type ProposalFilter struct {
	EntityType   string
	EntityID     int64
	Site         string
	OriginSite   string
	ProposerUID  int64
	Status       int16
	FieldKey     string
	HasConflict  *bool
	UpdatedSince *time.Time
	Order        string
	Limit        int
	BeforeID     int64
	Cursor       ProposalCursor
}

// ConflictFields answers, for each open proposal, the keys of its effective
// patch that a revision recorded after its base revision also changed.
func (e *Engine) ConflictFields(ctx context.Context, props []Proposal, effective map[int64]map[string]any) (map[int64][]string, error) {
	out := make(map[int64][]string, len(props))
	type entityKey struct {
		typ string
		id  int64
	}
	minBase := map[entityKey]int{}
	var pairs [][]any
	for i := range props {
		if props[i].Status != StatusOpen {
			continue
		}
		k := entityKey{props[i].EntityType, props[i].EntityID}
		if base, seen := minBase[k]; !seen {
			minBase[k] = props[i].BaseRevisionSeq
			pairs = append(pairs, []any{k.typ, k.id})
		} else if props[i].BaseRevisionSeq < base {
			minBase[k] = props[i].BaseRevisionSeq
		}
	}
	if len(pairs) == 0 {
		return out, nil
	}
	var revs []Revision
	if err := e.db.WithContext(ctx).Select("entity_type", "entity_id", "seq", "changed_fields").
		Where("(entity_type, entity_id) IN ?", pairs).Find(&revs).Error; err != nil {
		return nil, err
	}
	byEntity := map[entityKey][]Revision{}
	for i := range revs {
		k := entityKey{revs[i].EntityType, revs[i].EntityID}
		if revs[i].Seq > minBase[k] {
			byEntity[k] = append(byEntity[k], revs[i])
		}
	}
	for i := range props {
		p := &props[i]
		if p.Status != StatusOpen {
			continue
		}
		drifted := map[string]struct{}{}
		for _, r := range byEntity[entityKey{p.EntityType, p.EntityID}] {
			if r.Seq <= p.BaseRevisionSeq {
				continue
			}
			keys, err := decodeKeys(r.ChangedFields)
			if err != nil {
				return nil, err
			}
			for _, k := range keys {
				drifted[k] = struct{}{}
			}
		}
		hit := []string{}
		for _, k := range sortedKeys(effective[p.ID]) {
			if _, ok := drifted[k]; ok {
				hit = append(hit, k)
			}
		}
		out[p.ID] = hit
	}
	return out, nil
}

type ProposalCursor struct {
	ID        int64
	UpdatedAt time.Time
}

// An amendment that unsets a key still counts as touching it: a reviewer
// reading the queue for one field wants the proposals that argued about it.
const touchedFieldSQL = `(jsonb_exists(edit_proposal.patch, ?) OR EXISTS (
	SELECT 1 FROM edit_proposal_amendment a WHERE a.proposal_id = edit_proposal.id
	  AND (jsonb_exists(COALESCE(a.patch_delta->'set', '{}'::jsonb), ?)
	       OR jsonb_exists(COALESCE(a.patch_delta->'unset', '[]'::jsonb), ?))))`

const conflictSQL = `(edit_proposal.status = 0 AND EXISTS (
	SELECT 1 FROM edit_revision r
	WHERE r.entity_family = edit_proposal.entity_family AND r.entity_type = edit_proposal.entity_type
	  AND r.entity_id = edit_proposal.entity_id AND r.seq > edit_proposal.base_revision_seq
	  AND jsonb_exists_any(r.changed_fields, ARRAY(
	      SELECT jsonb_object_keys(edit_proposal.patch)
	      UNION SELECT jsonb_object_keys(COALESCE(a.patch_delta->'set', '{}'::jsonb))
	      FROM edit_proposal_amendment a WHERE a.proposal_id = edit_proposal.id))))`

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
	if f.OriginSite != "" {
		q = q.Where("origin_site = ?", f.OriginSite)
	}
	if f.ProposerUID != 0 {
		q = q.Where("proposer_uid = ?", f.ProposerUID)
	}
	if f.Status >= 0 {
		q = q.Where("status = ?", f.Status)
	}
	if f.FieldKey != "" {
		q = q.Where(touchedFieldSQL, f.FieldKey, f.FieldKey, f.FieldKey)
	}
	if f.HasConflict != nil {
		if *f.HasConflict {
			q = q.Where(conflictSQL)
		} else {
			q = q.Where("NOT " + conflictSQL)
		}
	}
	if f.UpdatedSince != nil {
		q = q.Where("updated_at > ?", *f.UpdatedSince)
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
	order := "id DESC"
	switch f.Order {
	case ProposalOrderFiledAsc:
		order = "id ASC"
		if f.Cursor.ID > 0 {
			page = page.Where("id > ?", f.Cursor.ID)
		}
	case ProposalOrderUpdatedDesc:
		order = "updated_at DESC, id DESC"
		if f.Cursor.ID > 0 {
			page = page.Where("(updated_at < ? OR (updated_at = ? AND id < ?))",
				f.Cursor.UpdatedAt, f.Cursor.UpdatedAt, f.Cursor.ID)
		}
	case ProposalOrderUpdatedAsc:
		order = "updated_at ASC, id ASC"
		if f.Cursor.ID > 0 {
			page = page.Where("(updated_at > ? OR (updated_at = ? AND id > ?))",
				f.Cursor.UpdatedAt, f.Cursor.UpdatedAt, f.Cursor.ID)
		}
	default:
		if f.BeforeID > 0 {
			page = page.Where("id < ?", f.BeforeID)
		}
	}
	var out []Proposal
	if err := page.Order(order).Limit(limit).Find(&out).Error; err != nil {
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
	if seq == 0 {
		return e.baselineRevision(ctx, spec, entityID)
	}
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

func (e *Engine) baselineRevision(ctx context.Context, spec *EntityTypeSpec, entityID int64) (*Revision, error) {
	first, err := e.revisionAt(ctx, spec, entityID, 1)
	if err != nil {
		return nil, err
	}
	if len(first.BaselineSnapshot) == 0 {
		return nil, ErrRevisionNotFound
	}
	return &Revision{
		EntityFamily: first.EntityFamily, EntityType: first.EntityType, EntityID: first.EntityID,
		Seq: 0, Snapshot: first.BaselineSnapshot, CreatedAt: first.CreatedAt,
	}, nil
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
			if spec.EffectivePolicy(spec.Fields[i].Key).Automerge == AutomergeOwner {
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
		pol := spec.EffectivePolicy(f.Key)
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
