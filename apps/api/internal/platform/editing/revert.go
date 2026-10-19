package editing

import (
	"context"

	"gorm.io/gorm"
)

type RevertInput struct {
	EntityType string
	EntityID   int64
	ToSeq      int
	Note       string
	Actor      PolicyContext
}

func (e *Engine) Revert(ctx context.Context, in RevertInput) (*Proposal, *Revision, error) {
	spec, err := e.resolveSpec(in.EntityType)
	if err != nil {
		return nil, nil, err
	}
	if err := e.deriveOwnership(ctx, spec, in.EntityID, &in.Actor); err != nil {
		return nil, nil, err
	}
	target, err := e.revisionAt(ctx, spec, in.EntityID, in.ToSeq)
	if err != nil {
		return nil, nil, err
	}
	targetSnap, err := decodeObject(target.Snapshot)
	if err != nil {
		return nil, nil, err
	}
	current, err := spec.LoadSnapshot(ctx, in.EntityID)
	if err != nil {
		return nil, nil, err
	}
	patch := make(map[string]any)
	for _, key := range sortedKeys(targetSnap) {
		if _, err := spec.fieldForWrite(key); err != nil {
			continue
		}
		if !jsonValueEqual(current[key], targetSnap[key]) {
			patch[key] = targetSnap[key]
		}
	}
	if len(patch) == 0 {
		return nil, nil, ErrNoEffectiveChanges
	}
	for _, key := range sortedKeys(patch) {
		if !spec.EffectivePolicy(key).AllowsReview(in.Actor) {
			return nil, nil, &PermissionError{Key: key, Action: "review"}
		}
	}
	baseSeq, err := maxRevisionSeq(e.db.WithContext(ctx), spec, in.EntityID)
	if err != nil {
		return nil, nil, err
	}
	rawPatch, err := encodeJSON(patch)
	if err != nil {
		return nil, nil, err
	}
	prop := &Proposal{
		EntityFamily: spec.Family, EntityType: spec.Type, EntityID: in.EntityID,
		BaseRevisionSeq: baseSeq, Patch: rawPatch,
		ProposerUID: in.Actor.UserID, Note: in.Note, Site: in.Actor.Site, Status: StatusOpen,
	}
	var rev *Revision
	err = e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		if err := lockEntity(etx, prop.EntityType, prop.EntityID); err != nil {
			return err
		}
		if err := etx.Create(prop).Error; err != nil {
			return err
		}
		r, err := e.mergeLocked(ctx, etx, spec, prop, patch, ActionReverted, nil, in.Actor.UserID, in.Note)
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
