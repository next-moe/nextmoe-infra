package textsafety

import (
	"context"
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

const applyBatch = 1000

type ApplyOpts struct {
	Run   bool
	Prune bool
}

type ApplyStats struct {
	Works, Judged                   int
	TitleChanged, DisplayChanged    int
	ShelfNSFWBefore, ShelfNSFWAfter int
	ToSFW, ToNSFW                   int
	TitleWritten, DisplayWritten    int
	PrunedTitles, PrunedIntros      int64
	run                             bool
}

func (s *ApplyStats) String() string {
	return fmt.Sprintf("apply run=%v works=%d judged=%d title_changed=%d display_changed=%d shelf_nsfw_before=%d after=%d to_sfw=%d to_nsfw=%d title_written=%d display_written=%d pruned_titles=%d pruned_intros=%d",
		s.run, s.Works, s.Judged, s.TitleChanged, s.DisplayChanged, s.ShelfNSFWBefore, s.ShelfNSFWAfter, s.ToSFW, s.ToNSFW,
		s.TitleWritten, s.DisplayWritten, s.PrunedTitles, s.PrunedIntros)
}

type shelfRow struct {
	ID                  int64   `gorm:"column:id"`
	Site                *string `gorm:"column:site"`
	ProductWorkID       *int64  `gorm:"column:product_work_id"`
	DisplayNSFW         bool    `gorm:"column:display_nsfw"`
	DisplayOwner        string  `gorm:"column:display_owner"`
	ContentRating       int16   `gorm:"column:content_rating"`
	CoverArtAllExplicit bool    `gorm:"column:cover_art_all_explicit"`
	TitleNSFW           *bool   `gorm:"column:title_nsfw"`
}

func (r shelfRow) claimed() bool { return r.Site != nil && *r.Site != "" && r.ProductWorkID != nil }

func (r shelfRow) nsfw() bool {
	return model.WorkShelf{
		Site: r.Site, ProductWorkID: r.ProductWorkID, DisplayNSFW: r.DisplayNSFW, ContentRating: r.ContentRating,
		CoverArtAllExplicit: r.CoverArtAllExplicit, TitleNSFW: r.TitleNSFW,
	}.NSFW()
}

type workChange struct {
	before, after shelfRow
	titles        workTitles
	verdict       model.CatalogWorkTitleSafety
	title         bool
	display       bool
}

type applyPlan struct {
	works   map[int64]shelfRow
	changes []workChange
	stats   ApplyStats
}

func planApply(ctx context.Context, db *gorm.DB) (*applyPlan, error) {
	var rows []shelfRow
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id, w.site, w.product_work_id, w.display_nsfw,
		       coalesce(w.field_provenance -> 'display_nsfw' -> 0 ->> 'source', '') AS display_owner,
		       w.content_rating, w.cover_art_all_explicit, w.title_nsfw
		FROM catalog_work w WHERE `+galgameLiveSQL, model.WorkStatusLive).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load works: %w", err)
	}
	titles, err := loadWorkTitles(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load titles: %w", err)
	}
	var verdicts []model.CatalogWorkTitleSafety
	if err := db.WithContext(ctx).Find(&verdicts).Error; err != nil {
		return nil, fmt.Errorf("load title verdicts: %w", err)
	}
	byHash := make(map[string]model.CatalogWorkTitleSafety, len(verdicts))
	for _, v := range verdicts {
		byHash[v.TitleHash] = v
	}

	p := &applyPlan{works: make(map[int64]shelfRow, len(rows))}
	for _, r := range rows {
		p.works[r.ID] = r
		if r.nsfw() {
			p.stats.ShelfNSFWBefore++
		}
	}
	p.stats.Works = len(rows)
	for _, t := range titles {
		before, ok := p.works[t.ID]
		v, judged := byHash[t.Hash]
		if !ok || !judged {
			continue
		}
		p.stats.Judged++
		want := !(v.Verdict == model.TitleSafetySafe && v.Confidence >= model.SafeTitleConfidence)
		after := before
		after.TitleNSFW = &want
		c := workChange{before: before, titles: t, verdict: v}
		c.title = before.TitleNSFW == nil || *before.TitleNSFW != want
		if before.claimed() && !provenance.IsHuman(before.DisplayOwner) && before.DisplayNSFW != want {
			after.DisplayNSFW = want
			c.display = true
		}
		c.after = after
		p.works[t.ID] = after
		if c.title || c.display {
			p.changes = append(p.changes, c)
			if c.title {
				p.stats.TitleChanged++
			}
			if c.display {
				p.stats.DisplayChanged++
			}
		}
		switch {
		case before.nsfw() && !after.nsfw():
			p.stats.ToSFW++
		case !before.nsfw() && after.nsfw():
			p.stats.ToNSFW++
		}
	}
	for _, r := range p.works {
		if r.nsfw() {
			p.stats.ShelfNSFWAfter++
		}
	}
	return p, nil
}

func Apply(ctx context.Context, db *gorm.DB, opts ApplyOpts) (*ApplyStats, error) {
	p, err := planApply(ctx, db)
	if err != nil {
		return nil, err
	}
	st := p.stats
	st.run = opts.Run
	if !opts.Run {
		return &st, nil
	}
	for start := 0; start < len(p.changes); start += applyBatch {
		batch := p.changes[start:min(start+applyBatch, len(p.changes))]
		touched, titleN, displayN, err := writeChanges(ctx, db, batch)
		if err != nil {
			return nil, err
		}
		st.TitleWritten += titleN
		st.DisplayWritten += displayN
		if err := repository.TouchWorks(ctx, db, touched); err != nil {
			return nil, fmt.Errorf("touch works: %w", err)
		}
	}
	if opts.Prune {
		if st.PrunedTitles, st.PrunedIntros, err = prune(ctx, db); err != nil {
			return nil, err
		}
	}
	return &st, nil
}

func writeChanges(ctx context.Context, db *gorm.DB, batch []workChange) ([]int64, int, int, error) {
	var titleVals, displayVals []string
	var titleArgs, displayArgs []any
	for _, c := range batch {
		if c.title {
			titleVals = append(titleVals, "(?::bigint, ?::boolean)")
			titleArgs = append(titleArgs, c.after.ID, *c.after.TitleNSFW)
		}
		if c.display {
			displayVals = append(displayVals, "(?::bigint, ?::boolean, ?::boolean)")
			displayArgs = append(displayArgs, c.after.ID, c.before.DisplayNSFW, c.after.DisplayNSFW)
		}
	}
	seen := map[int64]bool{}
	var touched []int64
	collect := func(ids []int64) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				touched = append(touched, id)
			}
		}
	}
	var titleIDs, displayIDs []int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(titleVals) > 0 {
			if err := tx.Raw(`UPDATE catalog_work w SET title_nsfw = v.t
				FROM (VALUES `+strings.Join(titleVals, ", ")+`) AS v(id, t)
				WHERE w.id = v.id AND w.title_nsfw IS DISTINCT FROM v.t
				RETURNING w.id`, titleArgs...).Scan(&titleIDs).Error; err != nil {
				return fmt.Errorf("write title_nsfw: %w", err)
			}
		}
		if len(displayVals) > 0 {
			args := append(displayArgs, provenance.HumanSources())
			if err := tx.Raw(`UPDATE catalog_work w SET display_nsfw = v.want,
					field_provenance = jsonb_set(w.field_provenance, '{display_nsfw}',
						jsonb_build_array(jsonb_build_object('source', 'derived', 'at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')))
						|| coalesce(w.field_provenance -> 'display_nsfw', '[]'::jsonb))
				FROM (VALUES `+strings.Join(displayVals, ", ")+`) AS v(id, was, want)
				WHERE w.id = v.id AND w.display_nsfw = v.was
				  AND coalesce(w.field_provenance -> 'display_nsfw' -> 0 ->> 'source', '') NOT IN ?
				RETURNING w.id`, args...).Scan(&displayIDs).Error; err != nil {
				return fmt.Errorf("write display_nsfw: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	collect(titleIDs)
	collect(displayIDs)
	return touched, len(titleIDs), len(displayIDs), nil
}

func prune(ctx context.Context, db *gorm.DB) (int64, int64, error) {
	titles, err := loadWorkTitles(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	live := make([]string, 0, len(titles))
	for _, t := range titles {
		live = append(live, t.Hash)
	}
	tr := db.WithContext(ctx).Exec(`DELETE FROM catalog_work_title_safety s WHERE s.title_hash <> ALL(?::text[])
		AND NOT EXISTS (SELECT 1 FROM catalog_release r WHERE r.deleted_at IS NULL AND md5(btrim(r.title)) = s.title_hash)`,
		"{"+strings.Join(live, ",")+"}")
	if tr.Error != nil {
		return 0, 0, fmt.Errorf("prune title verdicts: %w", tr.Error)
	}
	ir := db.WithContext(ctx).Exec(`DELETE FROM catalog_work_intro_safety s
		WHERE NOT EXISTS (SELECT 1 FROM catalog_work_intro i WHERE md5(i.intro) = s.intro_hash)`)
	if ir.Error != nil {
		return 0, 0, fmt.Errorf("prune intro verdicts: %w", ir.Error)
	}
	cr := db.WithContext(ctx).Exec(`DELETE FROM catalog_character_intro_safety s
		WHERE NOT EXISTS (SELECT 1 FROM catalog_character_intro i WHERE md5(i.intro) = s.intro_hash)`)
	if cr.Error != nil {
		return 0, 0, fmt.Errorf("prune character intro verdicts: %w", cr.Error)
	}
	return tr.RowsAffected, ir.RowsAffected + cr.RowsAffected, nil
}
