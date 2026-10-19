package getchuportraits

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"api/internal/jobs/getchuchars"
	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

type candidate struct {
	CharacterID int64
	GetchuID    string
	File        string
}

type nameplateKey struct {
	GetchuID string
	Ordinal  int
}

func selectCandidates(ctx context.Context, db, gdb *gorm.DB, slot Slot, matched []getchuchars.Candidate, st *Stats) ([]candidate, error) {
	plates, err := loadPlates(ctx, gdb, slot)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(matched))
	for _, m := range matched {
		ids = append(ids, m.CharacterID)
	}
	needs, err := loadUnfilled(ctx, db, slot, ids)
	if err != nil {
		return nil, err
	}

	out := make([]candidate, 0, len(matched))
	for _, m := range matched {
		if !needs[m.CharacterID] {
			st.SkipHasImage++
			continue
		}
		file, ok := pickPlate(m, plates)
		if !ok {
			st.NoImage++
			continue
		}
		out = append(out, candidate{CharacterID: m.CharacterID, GetchuID: file.GetchuID, File: file.File})
	}
	return out, nil
}

type plateFile struct {
	GetchuID string
	File     string
}

func pickPlate(c getchuchars.Candidate, plates map[nameplateKey]string) (plateFile, bool) {
	eds := c.Editions
	if len(eds) == 0 {
		eds = []getchuchars.Edition{{GetchuID: c.GetchuID, Ordinal: c.Ordinal}}
	}
	for _, e := range eds {
		if url, ok := plates[nameplateKey{GetchuID: e.GetchuID, Ordinal: e.Ordinal}]; ok {
			return plateFile{GetchuID: e.GetchuID, File: path.Base(url)}, true
		}
	}
	return plateFile{}, false
}

func loadPlates(ctx context.Context, gdb *gorm.DB, slot Slot) (map[nameplateKey]string, error) {
	var rows []struct {
		GetchuID string `gorm:"column:getchu_id"`
		Ordinal  int    `gorm:"column:ordinal"`
		URL      string `gorm:"column:url"`
	}
	q := fmt.Sprintf(`
		SELECT c.getchu_id, c.ordinal, c.%[1]s AS url
		FROM item_characters c
		JOIN item_images i ON i.getchu_id = c.getchu_id AND i.kind = '%[2]s'
			AND i.url = c.%[1]s
		WHERE c.%[1]s IS NOT NULL`, slot.StagingColumn, slot.ImageKind)
	err := gdb.WithContext(ctx).Raw(q).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read staging %s images: %w", slot.Name, err)
	}
	out := make(map[nameplateKey]string, len(rows))
	for _, r := range rows {
		out[nameplateKey{GetchuID: r.GetchuID, Ordinal: r.Ordinal}] = r.URL
	}
	return out, nil
}

func loadUnfilled(ctx context.Context, db *gorm.DB, slot Slot, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	q := fmt.Sprintf(`SELECT id FROM catalog_character
		 WHERE id IN ? AND deleted_at IS NULL AND %s IS NULL`, slot.TargetColumn) + " AND " + editspec.NotHumanFieldSQL("field_provenance", slot.TargetColumn)
	for i := 0; i < len(ids); i += 5000 {
		batch := ids[i:min(i+5000, len(ids))]
		var got []int64
		if err := db.WithContext(ctx).Raw(q, batch).Scan(&got).Error; err != nil {
			return nil, fmt.Errorf("load characters with no %s: %w", slot.Name, err)
		}
		for _, id := range got {
			out[id] = true
		}
	}
	return out, nil
}

func writeIDs(path string, cands []candidate) error {
	seen := map[string]bool{}
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		if !seen[c.GetchuID] {
			seen[c.GetchuID] = true
			ids = append(ids, c.GetchuID)
		}
	}
	sort.Strings(ids)
	return os.WriteFile(path, []byte(strings.Join(ids, "\n")+"\n"), 0o644)
}

func mirrorPath(root, getchuID, file string) string {
	return strings.Join([]string{strings.TrimRight(root, "/"), getchuID, file}, "/")
}
