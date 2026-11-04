package docsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/ratingmodel"

	"gorm.io/gorm"
)

var (
	rankModelsMu sync.Mutex
	rankModels   = map[*gorm.DB]*ratingmodel.Cache{}
)

func rankModel(ctx context.Context, db *gorm.DB, s scope) (ratingmodel.Fitted, error) {
	if s == nil {
		return ratingmodel.Latest(ctx, db)
	}
	rankModelsMu.Lock()
	c := rankModels[db]
	if c == nil {
		c = ratingmodel.NewCache(db)
		rankModels[db] = c
	}
	rankModelsMu.Unlock()
	return c.Get(ctx)
}

func loadWorkRanks(db *gorm.DB, s scope) (map[int64]map[string]map[string]float64, error) {
	ctx := context.Background()
	fitted, err := rankModel(ctx, db, s)
	if errors.Is(err, ratingmodel.ErrNoModel) {
		slog.Warn("work ranks skipped: no fitted rating model yet (reindex-catalog fits one)")
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byWork := map[int64][]ratingmodel.Observation{}
	if s == nil {
		obs, err := ratingmodel.LoadAll(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, o := range obs {
			byWork[o.WorkID] = append(byWork[o.WorkID], o)
		}
	} else if byWork, err = ratingmodel.LoadWorks(ctx, db, s); err != nil {
		return nil, err
	}
	out := make(map[int64]map[string]map[string]float64, len(byWork))
	for id, obs := range byWork {
		if doc := ratingmodel.IndexDoc(fitted.Params.Score(obs)); doc != nil {
			out[id] = doc
		}
	}
	return out, nil
}

// The same platform set the registry's platform= filter matches: a release on
// the platform, a release listing it among extra.platforms, or a work-level
// platform row nobody suppressed.
func loadWorkPlatforms(db *gorm.DB, s scope) (map[int64][]string, error) {
	var rows []struct {
		WorkID   int64  `gorm:"column:work_id"`
		Platform string `gorm:"column:platform"`
	}
	q := fmt.Sprintf(`SELECT DISTINCT e.work_id, e.platform FROM (
			SELECT r.work_id, r.platform FROM catalog_release r
			WHERE r.deleted_at IS NULL AND coalesce(r.platform, '') <> ''
			UNION ALL
			SELECT r.work_id, p.platform FROM catalog_release r,
				jsonb_array_elements_text(CASE WHEN jsonb_typeof(r.extra->'platforms') = 'array'
					THEN r.extra->'platforms' ELSE '[]'::jsonb END) AS p(platform)
			WHERE r.deleted_at IS NULL
			UNION ALL
			SELECT wp.work_id, wp.platform FROM catalog_work_platform wp
			WHERE `+editspec.NotSuppressedWorkPlatformSQL("wp.work_id", "wp.platform")+`
		) e `+worksPopulationJoin+` WHERE true`+s.and("e.work_id")+` ORDER BY e.work_id, e.platform`, "e.work_id")
	if err := db.Raw(q, s.args()...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("work platforms: %w", err)
	}
	m := map[int64][]string{}
	for _, r := range rows {
		m[r.WorkID] = append(m[r.WorkID], r.Platform)
	}
	return m, nil
}

const (
	CirculationCommercial = "commercial"
	CirculationDoujin     = "doujin"
)

func loadWorkCirculation(db *gorm.DB, s scope) (map[int64]string, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		Brand  bool  `gorm:"column:brand"`
		Circle bool  `gorm:"column:circle"`
	}
	q := fmt.Sprintf(`SELECT vwl.work_id, bool_or(l.kind = ?) AS brand, bool_or(l.kind = ?) AS circle
		FROM catalog_work_label vwl JOIN catalog_label l ON l.id = vwl.label_id AND l.deleted_at IS NULL
		`+worksPopulationJoin+`
		WHERE vwl.kind IN ? AND `+editspec.NotSuppressedWorkLabelSQL("vwl")+s.and("vwl.work_id")+`
		GROUP BY vwl.work_id`, "vwl.work_id")
	args := s.args(model.LabelKindGameBrand, model.LabelKindDoujinCircle,
		[]int16{model.WorkLabelKindCircle, model.WorkLabelKindDeveloper, model.WorkLabelKindBrand})
	if err := db.Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("work circulation: %w", err)
	}
	m := make(map[int64]string, len(rows))
	for _, r := range rows {
		switch {
		case r.Brand:
			m[r.WorkID] = CirculationCommercial
		case r.Circle:
			m[r.WorkID] = CirculationDoujin
		}
	}
	return m, nil
}

func loadWorkPlaytime(db *gorm.DB, s scope) (map[int64]int, error) {
	var rows []struct {
		WorkID  int64 `gorm:"column:work_id"`
		Minutes int   `gorm:"column:minutes"`
	}
	q := fmt.Sprintf(`SELECT p.work_id, p.minutes FROM catalog_work_playtime p `+worksPopulationJoin+`
		WHERE p.minutes > 0`+s.and("p.work_id"), "p.work_id")
	if err := db.Raw(q, s.args()...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("work playtime: %w", err)
	}
	all := map[int64][]int{}
	for _, r := range rows {
		all[r.WorkID] = append(all[r.WorkID], r.Minutes)
	}
	m := make(map[int64]int, len(all))
	for id, v := range all {
		slices.Sort(v)
		if n := len(v); n%2 == 1 {
			m[id] = v[n/2]
		} else {
			m[id] = (v[n/2-1] + v[n/2]) / 2
		}
	}
	return m, nil
}
