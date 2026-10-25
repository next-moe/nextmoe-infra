package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"gorm.io/gorm"
)

type saveFile struct {
	AssetID   int64  `json:"asset_id"`
	WorkID    int64  `json:"work_id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Filename  string `json:"filename"`
	Label     string `json:"label"`
	Site      string `json:"site"`
	SourceURL string `json:"source_url"`
	GameTitle string `json:"game_title"`
	GameID    int64  `json:"-"`
}

func runSaves(ctx context.Context, src, cat *gorm.DB, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required by the saves stage")
	}
	var rows []saveFile
	if err := src.WithContext(ctx).Raw(`
SELECT a.id AS asset_id, a.local_path AS path, a.sha256, a.bytes, a.filename, COALESCE(a.label, '') AS label,
       a.site, e.source_url, g.title AS game_title, g.id AS game_id
FROM asset a
JOIN entry e ON e.id = a.entry_id
JOIN game g ON g.id = COALESCE(a.game_id, e.game_id)
WHERE a.status = 'saved' AND a.local_path <> '' AND a.sha256 <> ''
ORDER BY a.id`).Scan(&rows).Error; err != nil {
		return err
	}
	gameIDs := map[int64]bool{}
	for _, r := range rows {
		gameIDs[r.GameID] = true
	}
	var games []matchSource
	if err := src.WithContext(ctx).Raw(`
SELECT g.id AS entry_id, g.title, g.brand, g.dlsite_id, g.fanza_id, g.extra::text AS extra
FROM game g WHERE g.id IN ?`, keysOf(gameIDs)).Scan(&games).Error; err != nil {
		return err
	}
	works, err := matchWorks(ctx, cat, games)
	if err != nil {
		return err
	}

	seen := map[[2]string]bool{}
	var files []saveFile
	counts := map[string]int{}
	matchedBySite, totalBySite := map[string]int{}, map[string]int{}
	duplicates := 0
	for _, r := range rows {
		totalBySite[r.Site]++
		m := works[r.GameID]
		counts[m.Method]++
		if m.WorkID == nil {
			continue
		}
		k := [2]string{fmt.Sprint(*m.WorkID), r.SHA256}
		if seen[k] {
			duplicates++
			continue
		}
		seen[k] = true
		matchedBySite[r.Site]++
		r.WorkID = *m.WorkID
		files = append(files, r)
	}

	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	for _, f := range files {
		if err := enc.Encode(f); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}

	fmt.Printf("\n=== import-walkthroughs saves assets=%d games=%d ===\n", len(rows), len(games))
	fmt.Printf("dlsite=%d fanza=%d title=%d title+brand=%d ambiguous=%d none=%d duplicates=%d written=%d\n",
		counts[matchDLsite], counts[matchFanza], counts[matchTitle], counts[matchTitleBrand], counts[matchAmbiguous], counts[matchNone],
		duplicates, len(files))
	sites := make([]string, 0, len(totalBySite))
	for s := range totalBySite {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	for _, s := range sites {
		fmt.Printf("  %-18s %5d of %5d\n", s, matchedBySite[s], totalBySite[s])
	}
	return nil
}
