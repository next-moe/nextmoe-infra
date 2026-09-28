package workengines

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/srcbangumi"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const bgmEngineField = "游戏引擎"

var (
	engineSeparators = regexp.MustCompile(`[,，、/／;；&＆+＋|｜]|\s+and\s+|\s+[-－]\s+`)
	engineSuffixes   = []string{"engine", "引擎", "エンジン"}
)

func (v *vocab) resolveName(ctx context.Context, name string, byPrefix bool) (int64, bool, error) {
	g := groupKey(name)
	if id, ok, err := v.resolveGroup(ctx, g); err != nil || ok {
		return id, ok, err
	}
	k := engineKey(name)
	for _, suffix := range engineSuffixes {
		if trimmed := strings.TrimSuffix(k, suffix); trimmed != k && trimmed != "" {
			if t, ok := aliasKeys[trimmed]; ok {
				trimmed = t
			}
			if id, ok, err := v.resolveGroup(ctx, trimmed); err != nil || ok {
				return id, ok, err
			}
		}
	}
	if !byPrefix {
		return 0, false, nil
	}
	if g, ok := prefixGroup(k); ok {
		return v.resolveGroup(ctx, g)
	}
	return 0, false, nil
}

func (v *vocab) resolveValue(ctx context.Context, value string, unmapped map[string]int) ([]int64, error) {
	if id, ok, err := v.resolveName(ctx, value, false); err != nil {
		return nil, err
	} else if ok {
		return []int64{id}, nil
	}
	var out []int64
	for _, part := range engineSeparators.Split(value, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, ok, err := v.resolveName(ctx, part, true)
		if err != nil {
			return nil, err
		}
		if !ok {
			unmapped[part]++
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

type workEngine struct{ work, engine int64 }

func runBgm(ctx context.Context, db *gorm.DB, opts Opts, v *vocab, bgmSource int16, forecast map[int64]struct{}, st *Stats) error {
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		Infobox []byte `gorm:"column:infobox_parsed"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id, s.infobox_parsed
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
		WHERE w.deleted_at IS NULL AND s.infobox_parsed IS NOT NULL
		  AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')
		ORDER BY w.id, s.id`,
		model.EntityTypeWork, bgmSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load bangumi anchors: %w", err)
	}

	covered, err := worksWithOtherEngines(ctx, db, bgmSource)
	if err != nil {
		return err
	}
	for w := range forecast {
		covered[w] = struct{}{}
	}

	want := map[workEngine]struct{}{}
	stated := map[int64]struct{}{}
	for _, r := range rows {
		values := srcbangumi.InfoboxValues(r.Infobox, bgmEngineField)
		if len(values) == 0 {
			continue
		}
		stated[r.WorkID] = struct{}{}
		if _, ok := covered[r.WorkID]; ok {
			continue
		}
		for _, value := range values {
			ids, err := v.resolveValue(ctx, value, st.Unmapped)
			if err != nil {
				return err
			}
			for _, id := range ids {
				want[workEngine{r.WorkID, id}] = struct{}{}
			}
		}
	}
	st.BgmStated = len(stated)
	for w := range stated {
		if _, ok := covered[w]; ok {
			st.BgmCovered++
		}
	}

	var have []struct {
		WorkID   int64 `gorm:"column:work_id"`
		EngineID int64 `gorm:"column:engine_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT work_id, engine_id FROM catalog_work_engine WHERE source_id = ?`,
		bgmSource).Scan(&have).Error; err != nil {
		return fmt.Errorf("load bangumi engine rows: %w", err)
	}
	haveSet := make(map[workEngine]struct{}, len(have))
	for _, h := range have {
		haveSet[workEngine{h.WorkID, h.EngineID}] = struct{}{}
	}

	var add, drop []workEngine
	for e := range want {
		if _, ok := haveSet[e]; !ok {
			add = append(add, e)
		}
	}
	for e := range haveSet {
		if _, ok := want[e]; !ok {
			drop = append(drop, e)
		}
	}
	sortEdges(add)
	sortEdges(drop)
	st.BgmAdd, st.BgmDrop = len(add), len(drop)
	worksWithBgm := map[int64]struct{}{}
	for e := range want {
		worksWithBgm[e.work] = struct{}{}
	}
	st.BgmWorks = len(worksWithBgm)
	if !opts.Apply {
		return nil
	}

	for start := 0; start < len(add); start += writeBatch {
		batch := add[start:min(start+writeBatch, len(add))]
		rows := make([]model.CatalogWorkEngine, 0, len(batch))
		works := make([]int64, 0, len(batch))
		for _, e := range batch {
			rows = append(rows, model.CatalogWorkEngine{WorkID: e.work, EngineID: e.engine, SourceID: bgmSource})
			works = append(works, e.work)
		}
		if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return fmt.Errorf("insert bangumi engine rows: %w", err)
		}
		if err := repository.TouchWorks(ctx, db, works); err != nil {
			return fmt.Errorf("touch works: %w", err)
		}
	}
	for _, e := range drop {
		if err := db.WithContext(ctx).Where("work_id = ? AND engine_id = ? AND source_id = ?", e.work, e.engine, bgmSource).
			Delete(&model.CatalogWorkEngine{}).Error; err != nil {
			return fmt.Errorf("delete bangumi engine row: %w", err)
		}
		if err := repository.TouchWorks(ctx, db, []int64{e.work}); err != nil {
			return fmt.Errorf("touch works: %w", err)
		}
	}
	return nil
}

func worksWithOtherEngines(ctx context.Context, db *gorm.DB, bgmSource int16) (map[int64]struct{}, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`
		SELECT work_id FROM catalog_work_engine WHERE source_id <> ?
		UNION
		SELECT work_id FROM (`+model.ReleaseEnginesSQL+`) re`, bgmSource).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load works with engines: %w", err)
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func sortEdges(es []workEngine) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].work != es[j].work {
			return es[i].work < es[j].work
		}
		return es[i].engine < es[j].engine
	})
}
