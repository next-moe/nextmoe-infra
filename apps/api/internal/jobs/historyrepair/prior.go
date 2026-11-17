package historyrepair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type revRow struct {
	ID               int64
	EntityFamily     string
	EntityType       string
	EntityID         int64
	Seq              int
	Action           int16
	ChangedFields    datatypes.JSON
	Snapshot         datatypes.JSON
	BaselineSnapshot datatypes.JSON
	CreatedAt        time.Time
}

// laneProjected are the catalog.work list fields whose galgame-era value named
// rows of every lane. labels is not here: no curated work_label row existed
// before Cut, so the curated lane a first catalog edit found was empty.
var laneProjected = []string{
	editspec.FieldWorkCovers, editspec.FieldWorkScreenshots, editspec.FieldWorkLinks,
}

// BackfillPrior fills edit_revision.prior_values for catalog-engine revisions
// written before the engine recorded it. The previous revision's snapshot is
// the prior value, except where that revision is galgame-era (see Cut).
func BackfillPrior(ctx context.Context, db *gorm.DB, reg *editing.Registry, apply bool) (*Stats, error) {
	st := newStats("prior", apply)
	var revs []revRow
	if err := db.WithContext(ctx).Table("edit_revision").
		Select("id, entity_family, entity_type, entity_id, seq, action, changed_fields, baseline_snapshot, created_at").
		Where("prior_values IS NULL AND created_at >= ? AND legacy_id IS NULL", Cut).
		Order("id").Scan(&revs).Error; err != nil {
		return nil, err
	}
	work, ok := reg.Type(editspec.TypeWork)
	if !ok {
		return nil, fmt.Errorf("registry has no %s", editspec.TypeWork)
	}
	for i := range revs {
		r := &revs[i]
		st.Scanned++
		var changed []string
		if err := json.Unmarshal(r.ChangedFields, &changed); err != nil {
			return nil, fmt.Errorf("revision %d changed_fields: %w", r.ID, err)
		}
		before, galgameEra, err := previousState(ctx, db, r)
		if errors.Is(err, errNoPrior) {
			st.Skipped++
			continue
		}
		if err != nil {
			return nil, err
		}
		prior := make(map[string]any, len(changed))
		for _, key := range changed {
			prior[key] = before[key]
		}
		if galgameEra && r.EntityType == editspec.TypeWork {
			if err := correctGalgameEra(ctx, db, work, r, prior, st); err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(prior)
		if err != nil {
			return nil, err
		}
		if apply {
			if err := db.WithContext(ctx).Exec(
				`UPDATE edit_revision SET prior_values = ? WHERE id = ? AND prior_values IS NULL`,
				string(raw), r.ID).Error; err != nil {
				return nil, fmt.Errorf("revision %d: %w", r.ID, err)
			}
		}
		st.Written++
	}
	return st, nil
}

var errNoPrior = errors.New("no recorded state before this revision")

func previousState(ctx context.Context, db *gorm.DB, r *revRow) (map[string]any, bool, error) {
	if r.Seq == 1 {
		if r.Action == editing.ActionCreated || len(r.BaselineSnapshot) == 0 {
			return nil, false, errNoPrior
		}
		var base map[string]any
		return base, false, json.Unmarshal(r.BaselineSnapshot, &base)
	}
	var prev revRow
	err := db.WithContext(ctx).Table("edit_revision").Select("snapshot, created_at").
		Where("entity_family = ? AND entity_type = ? AND entity_id = ? AND seq = ?",
			r.EntityFamily, r.EntityType, r.EntityID, r.Seq-1).
		Take(&prev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errNoPrior
	}
	if err != nil {
		return nil, false, err
	}
	var snap map[string]any
	if err := json.Unmarshal(prev.Snapshot, &snap); err != nil {
		return nil, false, err
	}
	return snap, prev.CreatedAt.Before(Cut), nil
}

func correctGalgameEra(ctx context.Context, db *gorm.DB, work *editing.EntityTypeSpec, r *revRow, prior map[string]any, st *Stats) error {
	if v, ok := prior[editspec.FieldWorkLabels]; ok && v != nil {
		if arr, _ := v.([]any); len(arr) > 0 {
			st.ByField[editspec.FieldWorkLabels]++
			st.sample("work %d seq %d: labels prior %d items -> [] (galgame-era snapshot)", r.EntityID, r.Seq, len(arr))
		}
		prior[editspec.FieldWorkLabels] = []any{}
	}
	for _, key := range laneProjected {
		v, ok := prior[key]
		if !ok || v == nil {
			continue
		}
		f, ok := work.Field(key)
		if !ok || f.Restore == nil {
			return fmt.Errorf("%s has no lane projection", key)
		}
		projected, err := f.Restore(ctx, db, r.EntityID, v)
		if err != nil {
			return fmt.Errorf("work %d %s: %w", r.EntityID, key, err)
		}
		before, _ := v.([]any)
		after, _ := projected.([]any)
		if len(after) != len(before) {
			st.ByField[key]++
			st.sample("work %d seq %d: %s prior %d -> %d items (upstream-held dropped)", r.EntityID, r.Seq, key, len(before), len(after))
		}
		prior[key] = projected
	}
	return nil
}
