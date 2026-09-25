package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var descCSVHeader = []string{"trait_id", "vndb_tid", "name", "source_hash", "source_text", "description_zh"}

type descCSVRow struct {
	TraitID       int64
	VndbTID       string
	Name          string
	SourceHash    string
	SourceText    string
	DescriptionZh string
}

type descWrite struct {
	ID   int64
	Zh   string
	Hash string
}

type descCounts struct {
	Rows     int
	Write    int
	Same     int
	Stale    int
	Empty    int
	StaleIDs []int64
}

func writeDescCSV(path string, rows []descCSVRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(descCSVHeader); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			strconv.FormatInt(r.TraitID, 10), r.VndbTID, r.Name, r.SourceHash, r.SourceText, r.DescriptionZh,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func readDescCSV(path string) ([]descCSVRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%s: empty CSV", path)
	}
	idCol := indexOf(recs[0], "trait_id")
	tidCol := indexOf(recs[0], "vndb_tid")
	nameCol := indexOf(recs[0], "name")
	hashCol := indexOf(recs[0], "source_hash")
	textCol := indexOf(recs[0], "source_text")
	zhCol := indexOf(recs[0], "description_zh")
	if idCol < 0 || tidCol < 0 || nameCol < 0 || hashCol < 0 || textCol < 0 || zhCol < 0 {
		return nil, fmt.Errorf("%s: header must contain trait_id,vndb_tid,name,source_hash,source_text,description_zh", path)
	}
	var out []descCSVRow
	for i, rec := range recs[1:] {
		need := idCol
		for _, c := range []int{tidCol, nameCol, hashCol, textCol, zhCol} {
			if c > need {
				need = c
			}
		}
		if need >= len(rec) {
			return nil, fmt.Errorf("%s:%d: short row", path, i+2)
		}
		id, err := strconv.ParseInt(strings.TrimSpace(rec[idCol]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid trait_id", path, i+2)
		}
		out = append(out, descCSVRow{
			TraitID:       id,
			VndbTID:       strings.TrimSpace(rec[tidCol]),
			Name:          rec[nameCol],
			SourceHash:    strings.TrimSpace(rec[hashCol]),
			SourceText:    rec[textCol],
			DescriptionZh: rec[zhCol],
		})
	}
	return out, nil
}

func planDescWrites(ctx context.Context, db *gorm.DB, rows []descCSVRow) ([]descWrite, descCounts, error) {
	c := descCounts{Rows: len(rows)}
	idSet := map[int64]struct{}{}
	var ids []int64
	for _, r := range rows {
		if _, ok := idSet[r.TraitID]; ok {
			continue
		}
		idSet[r.TraitID] = struct{}{}
		ids = append(ids, r.TraitID)
	}
	type curRow struct {
		ID          int64  `gorm:"column:id"`
		Description string `gorm:"column:description"`
		Zh          string `gorm:"column:description_zh"`
	}
	current := map[int64]curRow{}
	if len(ids) > 0 {
		var found []curRow
		if err := db.WithContext(ctx).Raw(
			`SELECT id, description, description_zh FROM catalog_character_trait WHERE id IN ?`, ids).
			Scan(&found).Error; err != nil {
			return nil, c, err
		}
		for _, r := range found {
			current[r.ID] = r
		}
	}
	var writes []descWrite
	for _, r := range rows {
		zh := strings.TrimSpace(r.DescriptionZh)
		if zh == "" {
			c.Empty++
			continue
		}
		cur, ok := current[r.TraitID]
		if !ok || sourceHash(cur.Description) != r.SourceHash {
			c.Stale++
			c.StaleIDs = append(c.StaleIDs, r.TraitID)
			continue
		}
		if cur.Zh == zh {
			c.Same++
			continue
		}
		c.Write++
		writes = append(writes, descWrite{ID: r.TraitID, Zh: zh, Hash: r.SourceHash})
	}
	return writes, c, nil
}

func applyDescWrites(ctx context.Context, db *gorm.DB, writes []descWrite) (int, error) {
	written := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, w := range writes {
			res := tx.Exec(`UPDATE catalog_character_trait
				   SET description_zh = ?, description_zh_source_hash = ?, updated_at = now()
				 WHERE id = ? AND `+descriptionHashSQL+` = ? AND description_zh IS DISTINCT FROM ?`,
				w.Zh, w.Hash, w.ID, w.Hash, w.Zh)
			if res.Error != nil {
				return res.Error
			}
			written += int(res.RowsAffected)
		}
		return nil
	})
	return written, err
}

func printDescCounts(c descCounts) {
	fmt.Printf("rows=%d write=%d same=%d stale=%d empty=%d\n", c.Rows, c.Write, c.Same, c.Stale, c.Empty)
	if len(c.StaleIDs) == 0 {
		return
	}
	n := len(c.StaleIDs)
	if n > 20 {
		n = 20
	}
	parts := make([]string, n)
	for i, id := range c.StaleIDs[:n] {
		parts[i] = strconv.FormatInt(id, 10)
	}
	fmt.Printf("stale ids: %s\n", strings.Join(parts, ","))
}

func runApplyDescCSV(ctx context.Context, db *gorm.DB, path string, apply bool) error {
	rows, err := readDescCSV(path)
	if err != nil {
		return err
	}
	writes, c, err := planDescWrites(ctx, db, rows)
	if err != nil {
		return err
	}
	fmt.Printf("\n=== ingest-trait-zh %s (description CSV %s) ===\n", mode(apply), path)
	printDescCounts(c)
	if !apply {
		fmt.Println("\n[dry run] nothing written — re-run with --apply")
		return nil
	}
	n, err := applyDescWrites(ctx, db, writes)
	if err != nil {
		return err
	}
	fmt.Printf("\nwritten=%d\n", n)
	return nil
}
