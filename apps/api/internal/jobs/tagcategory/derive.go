package tagcategory

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/vndbtagmap"

	"gorm.io/gorm"
)

const (
	vndbTheme     = "g1"
	vndbCharacter = "g20"
	vndbSetting   = "g22"
	vndbPlot      = "g24"
	vndbStyle     = "g674"
)

// byBranch maps a VNDB root's direct child to a category; a branch not listed
// takes its root's entry in byRoot. Theme has no root entry: every one of its
// branches is listed, and a Theme branch VNDB adds later derives nothing until
// it is added here.
var byBranch = map[string]int16{
	"g96":   model.TagCategoryRelationship, // Romance
	"g23":   model.TagCategoryAdult,        // Sexual Content
	"g360":  model.TagCategoryStory,        // Other Elements
	"g147":  model.TagCategoryStory,        // Drama
	"g2":    model.TagCategoryGenre,        // Fantasy
	"g105":  model.TagCategoryGenre,        // Science Fiction
	"g104":  model.TagCategoryGenre,        // Comedy
	"g19":   model.TagCategoryGenre,        // Mystery
	"g7":    model.TagCategoryGenre,        // Horror
	"g789":  model.TagCategoryGenre,        // Thriller
	"g12":   model.TagCategoryGenre,        // Action
	"g1801": model.TagCategoryGenre,        // Educational
	"g285":  model.TagCategoryStory,        // Plot > Type
	"g399":  model.TagCategoryPresentation, // Style > Presentation
	"g1947": model.TagCategoryFormat,       // Style > Game Jam
	"g1970": model.TagCategoryFormat,       // Style > April Fool's
	"g1526": model.TagCategoryFormat,       // Style > Title Parody
}

var byRoot = map[string]int16{
	vndbCharacter: model.TagCategoryCharacter,
	vndbSetting:   model.TagCategorySetting,
	vndbPlot:      model.TagCategorySystem,
	vndbStyle:     model.TagCategorySystem,
}

var precedence = []int16{
	model.TagCategoryCharacter,
	model.TagCategoryAdult,
	model.TagCategoryRelationship,
	model.TagCategorySetting,
	model.TagCategoryGenre,
	model.TagCategoryStory,
	model.TagCategorySystem,
	model.TagCategoryPresentation,
	model.TagCategoryFormat,
}

type DeriveStats struct {
	Candidates int
	Derived    int
	ByCategory map[int16]int
}

func Derive(ctx context.Context, db *gorm.DB, apply bool) (*DeriveStats, error) {
	var cands []struct {
		ID     int64
		Sexual bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, sexual FROM catalog_tag
		WHERE category_id IS NULL AND NOT COALESCE(` + editspec.HumanFieldProvenanceSQL("field_provenance", "category_id") + `, false)`).
		Scan(&cands).Error; err != nil {
		return nil, fmt.Errorf("load uncategorized tags: %w", err)
	}
	st := &DeriveStats{Candidates: len(cands), ByCategory: map[int16]int{}}
	if len(cands) == 0 {
		return st, nil
	}
	branches, err := vndbBranches(ctx, db)
	if err != nil {
		return nil, err
	}

	pick := map[int16][]int64{}
	for _, c := range cands {
		got := map[int16]bool{}
		for _, b := range branches[c.ID] {
			if cat, ok := byBranch[b.branch]; ok {
				got[cat] = true
			} else if cat, ok := byRoot[b.root]; ok {
				got[cat] = true
			}
		}
		var cat int16
		for _, p := range precedence {
			if got[p] {
				cat = p
				break
			}
		}
		if c.Sexual && cat != model.TagCategoryCharacter {
			cat = model.TagCategoryAdult
		}
		if cat != 0 {
			pick[cat] = append(pick[cat], c.ID)
		}
	}
	for cat, ids := range pick {
		st.ByCategory[cat] = len(ids)
		st.Derived += len(ids)
		if !apply {
			continue
		}
		if err := db.WithContext(ctx).Exec(`UPDATE catalog_tag SET category_id = ? WHERE id IN ? AND category_id IS NULL`,
			cat, ids).Error; err != nil {
			return nil, fmt.Errorf("set category %d: %w", cat, err)
		}
	}
	return st, nil
}

type branch struct{ root, branch string }

func vndbBranches(ctx context.Context, db *gorm.DB) (map[int64][]branch, error) {
	var tags []struct {
		ID    string `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Alias string `gorm:"column:alias"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, name, coalesce(alias, '') AS alias FROM src_vndb.tags`).
		Scan(&tags).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags: %w", err)
	}
	var ups []struct {
		ID     string `gorm:"column:id"`
		Parent string `gorm:"column:parent"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, parent FROM src_vndb.tags_parents`).Scan(&ups).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags_parents: %w", err)
	}
	var maps []struct {
		SourceName string `gorm:"column:source_name"`
		TagID      int64  `gorm:"column:tag_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT m.source_name, m.tag_id FROM catalog_tag_source_map m
		JOIN catalog_source s ON s.id = m.source_id WHERE s.key = 'vndb'`).Scan(&maps).Error; err != nil {
		return nil, fmt.Errorf("load vndb tag map: %w", err)
	}

	byName := make(map[string]int64, len(maps))
	for _, m := range maps {
		byName[m.SourceName] = m.TagID
	}
	parentsOf := make(map[string][]string, len(ups))
	for _, u := range ups {
		parentsOf[u.ID] = append(parentsOf[u.ID], u.Parent)
	}
	roots := map[string]bool{vndbTheme: true, vndbCharacter: true, vndbSetting: true, vndbPlot: true, vndbStyle: true}

	tm := vndbtagmap.Embedded()
	out := map[int64][]branch{}
	for _, t := range tags {
		canon, ok := byName[vndbtagmap.Resolve(tm, t.Name, t.Alias)]
		if !ok {
			continue
		}
		seen := map[string]bool{}
		var walk func(id, below string)
		walk = func(id, below string) {
			if seen[id] {
				return
			}
			seen[id] = true
			if roots[id] {
				out[canon] = append(out[canon], branch{root: id, branch: below})
				return
			}
			for _, p := range parentsOf[id] {
				walk(p, id)
			}
		}
		walk(t.ID, "")
	}
	return out, nil
}
