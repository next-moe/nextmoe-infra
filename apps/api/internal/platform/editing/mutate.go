package editing

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreateProposalInput struct {
	EntityType string
	EntityID   int64
	Patch      map[string]any
	Note       string
	Actor      PolicyContext
}

const editAdvisoryClassID = 0x65646974

func lockEntity(etx *gorm.DB, entityType string, entityID int64) error {
	// The id is stringified Go-side: with a `?::text` cast pgx infers the
	// param as TEXT and has no encode plan for an int64 bound to it.
	return etx.Exec(
		`SELECT pg_advisory_xact_lock(?::int4, hashtext(?))`,
		editAdvisoryClassID, entityType+":"+strconv.FormatInt(entityID, 10),
	).Error
}

func (e *Engine) CreateProposal(ctx context.Context, in CreateProposalInput) (*Proposal, *Revision, error) {
	spec, err := e.resolveSpec(in.EntityType)
	if err != nil {
		return nil, nil, err
	}
	if len(in.Patch) == 0 {
		return nil, nil, ErrEmptyPatch
	}
	if err := e.deriveOwnership(ctx, spec, in.EntityID, &in.Actor); err != nil {
		return nil, nil, err
	}
	pols := make(map[string]Policy, len(in.Patch))
	needOwner := false
	for _, key := range sortedKeys(in.Patch) {
		f, err := spec.fieldForWrite(key)
		if err != nil {
			return nil, nil, err
		}
		pol := spec.EffectivePolicy(key)
		if pol.Propose == ProposeLocked {
			return nil, nil, &LockedFieldError{Key: key}
		}
		if !pol.AllowsPropose(in.Actor) {
			return nil, nil, &PermissionError{Key: key, Action: "propose"}
		}
		if err := f.Validate(in.Patch[key]); err != nil {
			return nil, nil, &ValidationError{Key: key, Reason: err.Error()}
		}
		pols[key] = pol
		if pol.Automerge == AutomergeOwner {
			needOwner = true
		}
	}
	owner, err := e.ownerSite(ctx, spec, in.EntityID, needOwner)
	if err != nil {
		return nil, nil, err
	}
	automerge := true
	for _, pol := range pols {
		if !pol.allowsAutomergeWithOwner(in.Actor, owner) {
			automerge = false
			break
		}
	}
	if _, err := spec.LoadSnapshot(ctx, in.EntityID); err != nil {
		return nil, nil, err
	}
	baseSeq, err := maxRevisionSeq(e.db.WithContext(ctx), spec, in.EntityID)
	if err != nil {
		return nil, nil, err
	}
	rawPatch, err := encodeJSON(in.Patch)
	if err != nil {
		return nil, nil, err
	}
	prop := &Proposal{
		EntityFamily: spec.Family, EntityType: spec.Type, EntityID: in.EntityID,
		BaseRevisionSeq: baseSeq, Patch: rawPatch,
		ProposerUID: in.Actor.UserID, Note: in.Note, Site: in.Actor.Site, Status: StatusOpen,
	}
	if !automerge {
		if err := e.db.WithContext(ctx).Create(prop).Error; err != nil {
			return nil, nil, err
		}
		return prop, nil, nil
	}
	var rev *Revision
	err = e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		if err := lockEntity(etx, prop.EntityType, prop.EntityID); err != nil {
			return err
		}
		if err := etx.Create(prop).Error; err != nil {
			return err
		}
		r, err := e.mergeLocked(ctx, etx, spec, prop, in.Patch, ActionDirect, nil, in.Actor.UserID, "")
		if err != nil {
			return err
		}
		rev = r
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	e.afterMerge(ctx, rev)
	return prop, rev, nil
}

type AmendInput struct {
	Set   map[string]any
	Unset []string
	Note  string
	Actor PolicyContext
}

func (e *Engine) AmendProposal(ctx context.Context, proposalID int64, in AmendInput) (*ProposalAmendment, error) {
	if len(in.Set) == 0 && len(in.Unset) == 0 {
		return nil, ErrEmptyDelta
	}
	var amendment *ProposalAmendment
	err := e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		prop, err := lockProposal(etx, proposalID)
		if err != nil {
			return err
		}
		if prop.Status != StatusOpen {
			return ErrNotOpen
		}
		spec, err := e.resolveSpec(prop.EntityType)
		if err != nil {
			return err
		}
		if err := e.deriveOwnership(ctx, spec, prop.EntityID, &in.Actor); err != nil {
			return err
		}
		amendments, err := loadAmendments(etx, proposalID)
		if err != nil {
			return err
		}
		eff, _, err := effectivePatch(prop, amendments)
		if err != nil {
			return err
		}
		unset := make(map[string]struct{}, len(in.Unset))
		for _, key := range in.Unset {
			unset[key] = struct{}{}
		}
		for _, key := range sortedKeys(in.Set) {
			if _, both := unset[key]; both {
				return &ValidationError{Key: key, Reason: "key is both set and unset"}
			}
			f, err := spec.fieldForWrite(key)
			if err != nil {
				return err
			}
			pol := spec.EffectivePolicy(key)
			if pol.Propose == ProposeLocked {
				return &LockedFieldError{Key: key}
			}
			if !pol.AllowsReview(in.Actor) {
				return &PermissionError{Key: key, Action: "review"}
			}
			if err := f.Validate(in.Set[key]); err != nil {
				return &ValidationError{Key: key, Reason: err.Error()}
			}
		}
		for _, key := range in.Unset {
			if _, ok := spec.Field(key); !ok {
				return &UnknownFieldError{Key: key}
			}
			if _, present := eff[key]; !present {
				return &ValidationError{Key: key, Reason: "key is not in the effective patch"}
			}
			if !spec.EffectivePolicy(key).AllowsReview(in.Actor) {
				return &PermissionError{Key: key, Action: "review"}
			}
		}
		rawDelta, err := encodeJSON(Delta{Set: in.Set, Unset: in.Unset})
		if err != nil {
			return err
		}
		amendment = &ProposalAmendment{
			ProposalID: proposalID, Seq: len(amendments) + 1,
			PatchDelta: rawDelta, AmenderUID: in.Actor.UserID, Note: in.Note,
		}
		if err := etx.Create(amendment).Error; err != nil {
			return err
		}
		// An amendment changes what a merge will write but used to touch only
		// the child row, so the proposal's validator — built from
		// edit_proposal.updated_at — never moved: reviewer A's If-Match still
		// matched after B amended, and the merge wrote B's effective patch
		// while A believed they had approved their own read. UpdateColumn, not
		// Update, so GORM does not also stamp the column it is being given.
		return etx.Model(&Proposal{}).Where("id = ?", proposalID).
			UpdateColumn("updated_at", time.Now().UTC()).Error
	})
	if err != nil {
		return nil, err
	}
	return amendment, nil
}

func (e *Engine) MergeProposal(ctx context.Context, proposalID int64, actor PolicyContext, note string) (*Revision, error) {
	var rev *Revision
	err := e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		prop, err := lockProposal(etx, proposalID)
		if err != nil {
			return err
		}
		if prop.Status != StatusOpen {
			return ErrNotOpen
		}
		if err := lockEntity(etx, prop.EntityType, prop.EntityID); err != nil {
			return err
		}
		spec, err := e.resolveSpec(prop.EntityType)
		if err != nil {
			return err
		}
		if err := e.deriveOwnership(ctx, spec, prop.EntityID, &actor); err != nil {
			return err
		}
		amendments, err := loadAmendments(etx, proposalID)
		if err != nil {
			return err
		}
		eff, amended, err := effectivePatch(prop, amendments)
		if err != nil {
			return err
		}
		if len(eff) == 0 {
			return ErrEmptyPatch
		}
		for _, key := range sortedKeys(eff) {
			f, err := spec.fieldForWrite(key)
			if err != nil {
				return err
			}
			if !spec.EffectivePolicy(key).AllowsReview(actor) {
				return &PermissionError{Key: key, Action: "review"}
			}
			if err := f.Validate(eff[key]); err != nil {
				return &ValidationError{Key: key, Reason: err.Error()}
			}
		}
		drifted, err := driftedFields(etx, spec, prop.EntityID, prop.BaseRevisionSeq)
		if err != nil {
			return err
		}
		var conflicts []string
		for _, key := range sortedKeys(eff) {
			if _, moved := drifted[key]; !moved {
				continue
			}
			if _, ok := amended[key]; !ok {
				conflicts = append(conflicts, key)
			}
		}
		if len(conflicts) > 0 {
			return &ConflictError{Keys: conflicts}
		}
		var amender *int64
		if n := len(amendments); n > 0 {
			amender = &amendments[n-1].AmenderUID
		}
		rev, err = e.mergeLocked(ctx, etx, spec, prop, eff, ActionMerged, amender, actor.UserID, note)
		return err
	})
	if err != nil {
		return nil, err
	}
	e.afterMerge(ctx, rev)
	return rev, nil
}

func (e *Engine) DeclineProposal(ctx context.Context, proposalID int64, actor PolicyContext, note string) error {
	return e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		prop, err := lockProposal(etx, proposalID)
		if err != nil {
			return err
		}
		if prop.Status != StatusOpen {
			return ErrNotOpen
		}
		spec, err := e.resolveSpec(prop.EntityType)
		if err != nil {
			return err
		}
		if err := e.deriveOwnership(ctx, spec, prop.EntityID, &actor); err != nil && !errors.Is(err, ErrEntityNotFound) {
			return err
		}
		amendments, err := loadAmendments(etx, proposalID)
		if err != nil {
			return err
		}
		eff, _, err := effectivePatch(prop, amendments)
		if err != nil {
			return err
		}
		keys := sortedKeys(eff)
		if len(keys) == 0 {
			orig, err := decodeObject(prop.Patch)
			if err != nil {
				return err
			}
			keys = sortedKeys(orig)
		}
		for _, key := range keys {
			if !spec.EffectivePolicy(key).AllowsReview(actor) {
				return &PermissionError{Key: key, Action: "review"}
			}
		}
		return closeProposal(etx, prop.ID, StatusDeclined, actor.UserID, note)
	})
}

func (e *Engine) WithdrawProposal(ctx context.Context, proposalID int64, actor PolicyContext) error {
	return e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		prop, err := lockProposal(etx, proposalID)
		if err != nil {
			return err
		}
		if prop.Status != StatusOpen {
			return ErrNotOpen
		}
		if prop.ProposerUID != actor.UserID {
			return ErrNotProposer
		}
		return closeProposal(etx, prop.ID, StatusWithdrawn, actor.UserID, "")
	})
}

func (e *Engine) mergeLocked(
	ctx context.Context, etx *gorm.DB, spec *EntityTypeSpec, prop *Proposal,
	eff map[string]any, action int16, amenderUID *int64, decidedBy int64, decisionNote string,
) (*Revision, error) {
	base, err := spec.LoadSnapshot(ctx, prop.EntityID)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(eff))
	for _, key := range sortedKeys(eff) {
		if !jsonValueEqual(base[key], eff[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return nil, ErrNoEffectiveChanges
	}
	snapshot := make(map[string]any, len(base))
	for k, v := range base {
		snapshot[k] = v
	}
	for _, key := range changed {
		snapshot[key] = eff[key]
	}
	seq, err := maxRevisionSeq(etx, spec, prop.EntityID)
	if err != nil {
		return nil, err
	}
	rawChanged, err := encodeJSON(changed)
	if err != nil {
		return nil, err
	}
	rawSnapshot, err := encodeJSON(snapshot)
	if err != nil {
		return nil, err
	}
	rev := &Revision{
		EntityFamily: spec.Family, EntityType: spec.Type, EntityID: prop.EntityID,
		Seq: seq + 1, Action: action,
		ChangedFields: rawChanged, Snapshot: rawSnapshot,
		ActorUID: prop.ProposerUID, AmenderUID: amenderUID, ProposalID: &prop.ID,
		Site: prop.Site,
	}
	if seq == 0 {
		if rev.BaselineSnapshot, err = encodeJSON(base); err != nil {
			return nil, err
		}
	}
	if err := etx.Create(rev).Error; err != nil {
		return nil, err
	}
	if err := closeProposal(etx, prop.ID, StatusMerged, decidedBy, decisionNote); err != nil {
		return nil, err
	}
	now := time.Now()
	prop.Status = StatusMerged
	prop.DecidedByUID = &decidedBy
	prop.DecidedAt = &now
	prop.DecisionNote = decisionNote
	for _, key := range changed {
		f, _ := spec.Field(key)
		if err := ApplyField(ctx, etx, f, prop.EntityID, eff[key]); err != nil {
			return nil, err
		}
	}
	return rev, nil
}

func lockProposal(etx *gorm.DB, id int64) (*Proposal, error) {
	var p Proposal
	err := etx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProposalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func DeclineOpenProposals(tx *gorm.DB, entityType string, entityID int64, decidedBy *int64, note string) error {
	return tx.Model(&Proposal{}).
		Where("entity_type = ? AND entity_id = ? AND status = ?", entityType, entityID, StatusOpen).
		Updates(map[string]any{
			"status": StatusDeclined, "decided_by_uid": decidedBy, "decided_at": time.Now(), "decision_note": note,
		}).Error
}

func closeProposal(etx *gorm.DB, id int64, status int16, decidedBy int64, note string) error {
	now := time.Now()
	return etx.Model(&Proposal{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "decided_by_uid": decidedBy, "decided_at": now, "decision_note": note,
	}).Error
}

func driftedFields(etx *gorm.DB, spec *EntityTypeSpec, entityID int64, baseSeq int) (map[string]struct{}, error) {
	var revs []Revision
	if err := etx.Select("changed_fields").
		Where("entity_family = ? AND entity_type = ? AND entity_id = ? AND seq > ?",
			spec.Family, spec.Type, entityID, baseSeq).
		Find(&revs).Error; err != nil {
		return nil, err
	}
	drifted := make(map[string]struct{})
	for i := range revs {
		keys, err := decodeKeys(revs[i].ChangedFields)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			drifted[k] = struct{}{}
		}
	}
	return drifted, nil
}
