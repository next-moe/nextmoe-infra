package tagcategory

import (
	"context"
	"errors"
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

// Category "" clears the tag's category: a reviewed "其他" that Derive must
// not fill again.
type CuratedRec struct {
	TagID    int64  `json:"tag_id"`
	Tag      string `json:"tag"`
	Category string `json:"category"`
}

type CuratedStats struct {
	Records   int
	Changed   int
	Stamped   int
	Unchanged int
}

type tagState struct {
	Name     string
	Category *int16
	Human    bool
}

// ApplyCurated validates every record before it writes any, then sets each
// tag's category and stamps it curated in one transaction.
func ApplyCurated(ctx context.Context, db *gorm.DB, recs []CuratedRec, apply bool) (*CuratedStats, error) {
	var cats []struct {
		ID   int16
		Slug string
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, slug FROM catalog_tag_category`).Scan(&cats).Error; err != nil {
		return nil, err
	}
	bySlug := make(map[string]int16, len(cats))
	for _, c := range cats {
		bySlug[c.Slug] = c.ID
	}
	var rows []struct {
		ID         int64
		Name       string
		CategoryID *int16
		Human      bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, name, category_id,
		COALESCE(` + editspec.HumanFieldProvenanceSQL("field_provenance", "category_id") + `, false) AS human
		FROM catalog_tag`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	tags := make(map[int64]tagState, len(rows))
	for _, r := range rows {
		tags[r.ID] = tagState{Name: r.Name, Category: r.CategoryID, Human: r.Human}
	}

	st := &CuratedStats{Records: len(recs)}
	type write struct {
		id  int64
		cat *int16
	}
	var writes []write
	seen := map[int64]int{}
	var errs []error
	for i, r := range recs {
		line := i + 1
		t, ok := tags[r.TagID]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("record %d: tag %d does not exist", line, r.TagID))
			continue
		case t.Name != r.Tag:
			errs = append(errs, fmt.Errorf("record %d: tag %d is named %q, the record says %q", line, r.TagID, t.Name, r.Tag))
			continue
		case seen[r.TagID] > 0:
			errs = append(errs, fmt.Errorf("record %d: tag %d already appears in record %d", line, r.TagID, seen[r.TagID]))
			continue
		}
		seen[r.TagID] = line
		var want *int16
		if r.Category != "" {
			id, ok := bySlug[r.Category]
			if !ok {
				errs = append(errs, fmt.Errorf("record %d: unknown category %q", line, r.Category))
				continue
			}
			want = &id
		}
		same := (want == nil) == (t.Category == nil) && (want == nil || *want == *t.Category)
		switch {
		case same && t.Human:
			st.Unchanged++
			continue
		case same:
			st.Stamped++
		default:
			st.Changed++
		}
		writes = append(writes, write{r.TagID, want})
	}
	if len(errs) > 0 {
		return st, fmt.Errorf("%d invalid records, nothing written:\n%w", len(errs), errors.Join(errs...))
	}
	if !apply || len(writes) == 0 {
		return st, nil
	}

	stamp, err := editing.StampJSON(provenance.SourceCurated)
	if err != nil {
		return nil, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, w := range writes {
			if err := tx.Exec(`UPDATE catalog_tag SET category_id = ?, updated_at = now(),
				field_provenance = jsonb_set(field_provenance, '{category_id}',
					?::jsonb || COALESCE(field_provenance -> 'category_id', '[]'::jsonb))
				WHERE id = ?`, w.cat, string(stamp), w.id).Error; err != nil {
				return fmt.Errorf("tag %d: %w", w.id, err)
			}
		}
		return nil
	})
	return st, err
}
