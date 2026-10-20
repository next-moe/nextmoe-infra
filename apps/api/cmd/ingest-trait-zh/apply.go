package main

import (
	"context"
	"sort"

	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

const (
	provenanceCurated = int16(0)
	provenanceMachine = int16(1)
)

type traitRow struct {
	ID         int64  `gorm:"column:id"`
	VndbTID    string `gorm:"column:vndb_tid"`
	Name       string `gorm:"column:name"`
	NameZh     string `gorm:"column:name_zh"`
	Provenance int16  `gorm:"column:name_zh_provenance"`
	GroupTID   string `gorm:"column:group_tid"`
	Human      bool   `gorm:"column:human"`
}

type decision int

const (
	decWrite decision = iota
	decSame
	decConflict
	decHuman
)

type plannedWrite struct {
	Trait    traitRow
	Zh       string
	Decision decision
}

func plan(proposals []pair, rows []traitRow, prov int16) []plannedWrite {
	byName := map[string]string{}
	for _, p := range proposals {
		if _, ok := byName[p.En]; !ok {
			byName[p.En] = p.Zh
		}
	}
	var out []plannedWrite
	for _, r := range rows {
		zh, ok := byName[r.Name]
		if !ok {
			continue
		}
		out = append(out, plannedWrite{Trait: r, Zh: zh, Decision: decide(r, zh, prov)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Trait.ID < out[j].Trait.ID })
	return out
}

func decide(r traitRow, zh string, prov int16) decision {
	switch {
	case r.Human:
		return decHuman
	case r.NameZh == zh && r.Provenance == prov:
		return decSame
	case r.NameZh == "":
		return decWrite
	case r.Provenance == provenanceMachine:
		return decWrite
	case r.NameZh == zh:
		return decSame
	default:
		return decConflict
	}
}

func loadTraits(ctx context.Context, db *gorm.DB) ([]traitRow, error) {
	var rows []traitRow
	err := db.WithContext(ctx).Raw(
		`SELECT id, vndb_tid, name, name_zh, name_zh_provenance, group_tid,
		        COALESCE(` + editspec.HumanFieldProvenanceSQL("field_provenance", "name_zh") + `, false) AS human
		   FROM catalog_character_trait ORDER BY id`).Scan(&rows).Error
	return rows, err
}

func applyWrites(ctx context.Context, db *gorm.DB, writes []plannedWrite, prov int16) (int, error) {
	written := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, w := range writes {
			if w.Decision != decWrite {
				continue
			}
			res := tx.Exec(`UPDATE catalog_character_trait
				   SET name_zh = ?, name_zh_provenance = ?, updated_at = now()
				 WHERE id = ? AND (name_zh = '' OR name_zh_provenance = ?) AND `+
				editspec.NotHumanFieldSQL("field_provenance", "name_zh"),
				w.Zh, prov, w.Trait.ID, provenanceMachine)
			if res.Error != nil {
				return res.Error
			}
			written += int(res.RowsAffected)
		}
		return nil
	})
	return written, err
}

type counts struct {
	Proposals int
	Matched   int
	Write     int
	Same      int
	Conflict  int
	Human     int
}

func summarise(proposals []pair, writes []plannedWrite) counts {
	c := counts{Proposals: len(proposals), Matched: len(writes)}
	for _, w := range writes {
		switch w.Decision {
		case decWrite:
			c.Write++
		case decSame:
			c.Same++
		case decConflict:
			c.Conflict++
		case decHuman:
			c.Human++
		}
	}
	return c
}
