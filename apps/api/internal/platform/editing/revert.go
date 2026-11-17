package editing

import (
	"context"
	"errors"
	"sort"

	"api/internal/platform/editorigin"

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
		if in.ToSeq == 0 && errors.Is(err, ErrRevisionNotFound) {
			return nil, nil, ErrNoBaseline
		}
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
	patch, err := e.restorePatch(ctx, spec, in.EntityID, sortedKeys(targetSnap), targetSnap, current)
	if err != nil {
		return nil, nil, err
	}
	return e.commitRevert(ctx, spec, in.EntityID, patch, in.Note, in.Actor)
}

func (e *Engine) restorePatch(
	ctx context.Context, spec *EntityTypeSpec, entityID int64, keys []string, values, current map[string]any,
) (map[string]any, error) {
	patch := make(map[string]any)
	for _, key := range keys {
		f, err := spec.fieldForWrite(key)
		if err != nil {
			continue
		}
		v, err := f.restoreValue(ctx, e.db.WithContext(ctx), entityID, values[key])
		if err != nil {
			return nil, err
		}
		if !jsonValueEqual(current[key], v) {
			patch[key] = v
		}
	}
	return patch, nil
}

type UndoInput struct {
	EntityType string
	EntityID   int64
	Seq        int
	Force      bool
	Note       string
	Actor      PolicyContext
}

// Undo puts back, for only the fields revision Seq changed, the values they held
// just before it. Revert restores a whole snapshot; Undo leaves every other
// field — and every later change to other fields — alone.
func (e *Engine) Undo(ctx context.Context, in UndoInput) (*Proposal, *Revision, error) {
	spec, err := e.resolveSpec(in.EntityType)
	if err != nil {
		return nil, nil, err
	}
	if err := e.deriveOwnership(ctx, spec, in.EntityID, &in.Actor); err != nil {
		return nil, nil, err
	}
	target, err := e.revisionAt(ctx, spec, in.EntityID, in.Seq)
	if err != nil {
		return nil, nil, err
	}
	priorVals, err := e.valuesBefore(ctx, spec, target)
	if err != nil {
		return nil, nil, err
	}
	keys, err := decodeKeys(target.ChangedFields)
	if err != nil {
		return nil, nil, err
	}
	if !in.Force {
		conflicts, err := e.keysChangedAfter(ctx, spec, in.EntityID, in.Seq, keys)
		if err != nil {
			return nil, nil, err
		}
		if len(conflicts) > 0 {
			return nil, nil, &UndoConflictError{Keys: conflicts}
		}
	}
	current, err := spec.LoadSnapshot(ctx, in.EntityID)
	if err != nil {
		return nil, nil, err
	}
	patch, err := e.restorePatch(ctx, spec, in.EntityID, keys, priorVals, current)
	if err != nil {
		return nil, nil, err
	}
	return e.commitRevert(ctx, spec, in.EntityID, patch, in.Note, in.Actor)
}

func (e *Engine) valuesBefore(ctx context.Context, spec *EntityTypeSpec, target *Revision) (map[string]any, error) {
	if len(target.PriorValues) > 0 {
		return decodeObject(target.PriorValues)
	}
	prior, err := e.revisionAt(ctx, spec, target.EntityID, target.Seq-1)
	if err != nil {
		if target.Seq == 1 && errors.Is(err, ErrRevisionNotFound) {
			return nil, ErrNoBaseline
		}
		return nil, err
	}
	return decodeObject(prior.Snapshot)
}

func (e *Engine) keysChangedAfter(ctx context.Context, spec *EntityTypeSpec, entityID int64, seq int, keys []string) ([]string, error) {
	drifted, err := driftedFields(e.db.WithContext(ctx), spec, entityID, seq)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		if _, ok := drifted[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (e *Engine) commitRevert(ctx context.Context, spec *EntityTypeSpec, entityID int64, patch map[string]any, note string, actor PolicyContext) (*Proposal, *Revision, error) {
	if len(patch) == 0 {
		return nil, nil, ErrNoEffectiveChanges
	}
	for _, key := range sortedKeys(patch) {
		if !spec.EffectivePolicy(key).AllowsReview(actor) {
			return nil, nil, &PermissionError{Key: key, Action: "review"}
		}
	}
	baseSeq, err := maxRevisionSeq(e.db.WithContext(ctx), spec, entityID)
	if err != nil {
		return nil, nil, err
	}
	rawPatch, err := encodeJSON(patch)
	if err != nil {
		return nil, nil, err
	}
	prop := &Proposal{
		EntityFamily: spec.Family, EntityType: spec.Type, EntityID: entityID,
		BaseRevisionSeq: baseSeq, Patch: rawPatch,
		ProposerUID: actor.UserID, Note: note, Site: actor.Site, Status: StatusOpen,
		OriginSite: editorigin.Ptr(ctx),
	}
	var rev *Revision
	err = e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		if err := lockEntity(etx, prop.EntityType, prop.EntityID); err != nil {
			return err
		}
		if err := etx.Create(prop).Error; err != nil {
			return err
		}
		r, err := e.mergeLocked(ctx, etx, spec, prop, patch, ActionReverted, nil, actor.UserID, note)
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
