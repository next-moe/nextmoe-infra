package chardocs

import (
	"context"
	"math"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"

	"gorm.io/gorm"
)

type Row struct {
	ID          int64  `gorm:"column:id"`
	DisplayName string `gorm:"column:display_name"`
	Lang        string `gorm:"column:lang"`
	Latin       string `gorm:"column:latin"`
	Gender      *int16 `gorm:"column:gender"`
}

type Context struct {
	parents map[int64][]int64
	sexual  map[int64]bool
	workPop map[int64]float64
	ancMemo map[int64][]int64
}

func Load(ctx context.Context, db *gorm.DB) (*Context, error) {
	parents, err := loadParents(ctx, db)
	if err != nil {
		return nil, err
	}
	sexual, err := loadSexualFamily(ctx, db)
	if err != nil {
		return nil, err
	}
	workPop, err := LoadWorkPopularityRaw(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	return &Context{
		parents: parents,
		sexual:  sexual,
		workPop: workPop,
		ancMemo: map[int64][]int64{},
	}, nil
}

func LoadWorkPopularityRaw(db *gorm.DB) (map[int64]float64, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		V      int64 `gorm:"column:v"`
	}
	if err := db.Raw(`SELECT work_id, max(value) AS v FROM catalog_work_popularity
		WHERE metric IN (?, ?) GROUP BY work_id`,
		model.PopularityMetricBgmCollect, model.PopularityMetricDownloads).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]float64, len(rows))
	for _, r := range rows {
		m[r.WorkID] = float64(r.V)
	}
	return m, nil
}

func (c *Context) Build(ctx context.Context, db *gorm.DB, rows []Row) ([]catsearch.EntityDoc, error) {
	docs := make([]catsearch.EntityDoc, len(rows))
	if len(rows) == 0 {
		return docs, nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	links, err := loadLinks(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	roster, err := loadRoster(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		d := catsearch.EntityDoc{
			ID:         catsearch.CharacterDocID(r.ID),
			EntityType: "character",
			Latin:      r.Latin,
			CatalogID:  r.ID,
			Gender:     r.Gender,
			Popularity: characterPopularity(roster[r.ID], c.workPop),
		}
		d.SetName(r.Lang, r.DisplayName)
		d.TraitIDs, d.TraitIDsSFW = c.traitUnions(links[r.ID])
		docs[i] = d
	}
	return docs, nil
}

func (c *Context) traitUnions(traitIDs []int64) (all, sfw []int64) {
	allSet := map[int64]struct{}{}
	sfwSet := map[int64]struct{}{}
	for _, tid := range traitIDs {
		c.addClosure(allSet, tid)
		if !c.sexual[tid] {
			c.addClosure(sfwSet, tid)
		}
	}
	return sortedIDs(allSet), sortedIDs(sfwSet)
}

func (c *Context) addClosure(dst map[int64]struct{}, id int64) {
	dst[id] = struct{}{}
	for _, a := range c.ancestors(id) {
		dst[a] = struct{}{}
	}
}

func (c *Context) ancestors(id int64) []int64 {
	if got, ok := c.ancMemo[id]; ok {
		return got
	}
	seen := map[int64]struct{}{id: {}}
	var out []int64
	var walk func(int64)
	walk = func(tid int64) {
		for _, p := range c.parents[tid] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
			walk(p)
		}
	}
	walk(id)
	c.ancMemo[id] = out
	return out
}

func characterPopularity(edges []rosterEdge, workPop map[int64]float64) float64 {
	var sum float64
	for _, e := range edges {
		p := workPop[e.WorkID]
		if e.Kind == model.WorkCharacterKindMain {
			sum += p
		} else {
			sum += 0.5 * p
		}
	}
	return math.Log1p(sum)
}

func loadParents(ctx context.Context, db *gorm.DB) (map[int64][]int64, error) {
	var rows []struct {
		TraitID  int64 `gorm:"column:trait_id"`
		ParentID int64 `gorm:"column:parent_id"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT trait_id, parent_id FROM catalog_character_trait_parent`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]int64{}
	for _, r := range rows {
		m[r.TraitID] = append(m[r.TraitID], r.ParentID)
	}
	return m, nil
}

func loadSexualFamily(ctx context.Context, db *gorm.DB) (map[int64]bool, error) {
	var rows []struct {
		ID     int64 `gorm:"column:id"`
		Sexual bool  `gorm:"column:sexual_family"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT id, sexual_family FROM catalog_character_trait`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]bool, len(rows))
	for _, r := range rows {
		m[r.ID] = r.Sexual
	}
	return m, nil
}

func loadLinks(ctx context.Context, db *gorm.DB, ids []int64) (map[int64][]int64, error) {
	var rows []struct {
		CharacterID int64 `gorm:"column:character_id"`
		TraitID     int64 `gorm:"column:trait_id"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT character_id, trait_id FROM catalog_character_trait_link
		 WHERE character_id IN ? AND spoiler_level <= ?`,
		ids, model.SpoilerNone).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]int64{}
	for _, r := range rows {
		m[r.CharacterID] = append(m[r.CharacterID], r.TraitID)
	}
	return m, nil
}

type rosterEdge struct {
	WorkID int64
	Kind   int16
}

func loadRoster(ctx context.Context, db *gorm.DB, ids []int64) (map[int64][]rosterEdge, error) {
	var rows []struct {
		CharacterID int64 `gorm:"column:character_id"`
		WorkID      int64 `gorm:"column:work_id"`
		Kind        int16 `gorm:"column:kind"`
	}
	q := `SELECT wc.character_id, wc.work_id, wc.kind FROM catalog_work_character wc
		WHERE wc.character_id IN ? AND ` + editspec.NotSuppressedRosterSQL("wc") +
		` AND ` + editspec.LiveWorkSQL("wc.work_id")
	if err := db.WithContext(ctx).Raw(q, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]rosterEdge{}
	for _, r := range rows {
		m[r.CharacterID] = append(m[r.CharacterID], rosterEdge{WorkID: r.WorkID, Kind: r.Kind})
	}
	return m, nil
}

func sortedIDs(set map[int64]struct{}) []int64 {
	if len(set) == 0 {
		return nil
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
