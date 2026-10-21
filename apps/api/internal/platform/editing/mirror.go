package editing

import (
	"context"
	"errors"
	"sort"

	"gorm.io/gorm"
)

type mirrorTarget struct {
	entityID int64
	before   map[string]any
}

func lockMirrors(
	ctx context.Context, etx *gorm.DB, spec *EntityTypeSpec, entityID int64,
	keys []string, before, after map[string]any,
) ([]mirrorTarget, error) {
	byEntity := make(map[int64][]string)
	for _, key := range keys {
		f, ok := spec.Field(key)
		if !ok || f.Mirror == nil {
			continue
		}
		for _, id := range f.Mirror.Others(before[key], after[key]) {
			if id == entityID || containsKey(byEntity[id], key) {
				continue
			}
			byEntity[id] = append(byEntity[id], key)
		}
	}
	ids := make([]int64, 0, len(byEntity))
	for id := range byEntity {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]mirrorTarget, 0, len(ids))
	for _, id := range ids {
		if err := lockEntity(etx, spec.Type, id); err != nil {
			return nil, err
		}
		t := mirrorTarget{entityID: id, before: make(map[string]any, len(byEntity[id]))}
		for _, key := range byEntity[id] {
			f, _ := spec.Field(key)
			v, err := f.Mirror.Load(ctx, etx, id)
			if err != nil {
				return nil, err
			}
			t.before[key] = v
		}
		out = append(out, t)
	}
	return out, nil
}

// writeMirrorRevisions gives every mirrored entity whose value moved a revision
// of its own, attributed to the proposal that moved it. Without one, the other
// end's history misses the change, and a proposal open there merges over it
// unchecked: drift is computed from revisions.
func writeMirrorRevisions(
	ctx context.Context, etx *gorm.DB, spec *EntityTypeSpec, prop *Proposal,
	targets []mirrorTarget, action int16, amenderUID *int64,
) ([]*Revision, error) {
	var out []*Revision
	for _, t := range targets {
		after := make(map[string]any, len(t.before))
		changed := make([]string, 0, len(t.before))
		for _, key := range sortedKeys(t.before) {
			f, _ := spec.Field(key)
			v, err := f.Mirror.Load(ctx, etx, t.entityID)
			if err != nil {
				return nil, err
			}
			if !jsonValueEqual(t.before[key], v) {
				after[key] = v
				changed = append(changed, key)
			}
		}
		if len(changed) == 0 {
			continue
		}
		base, err := spec.LoadSnapshot(ctx, t.entityID)
		if errors.Is(err, ErrEntityNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		snapshot := make(map[string]any, len(base))
		for k, v := range base {
			snapshot[k] = v
		}
		for _, key := range changed {
			snapshot[key] = after[key]
		}
		seq, err := maxRevisionSeq(etx, spec, t.entityID)
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
			EntityFamily: spec.Family, EntityType: spec.Type, EntityID: t.entityID,
			Seq: seq + 1, Action: action,
			ChangedFields: rawChanged, Snapshot: rawSnapshot,
			ActorUID: prop.ProposerUID, AmenderUID: amenderUID, ProposalID: &prop.ID,
			Site: prop.Site, OriginSite: prop.OriginSite,
		}
		if seq == 0 {
			if rev.BaselineSnapshot, err = encodeJSON(base); err != nil {
				return nil, err
			}
		}
		if err := etx.Create(rev).Error; err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, nil
}

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
