package importer

import (
	"encoding/json"
	"sort"
	"strings"

	"api/internal/platform/catalog/sourcedate"

	"gorm.io/datatypes"
)

type relMeta struct {
	olang    string
	released int
	minage   *int16
	freeware bool
	official bool
	patch    bool
}

type relTitle struct {
	lang  string
	mtl   bool
	title string
	latin string
}

func originalTitle(rows []relTitle, olang string) (string, bool) {
	for _, t := range rows {
		if t.lang != olang || t.mtl {
			continue
		}
		if v := strings.TrimSpace(t.title); v != "" {
			return t.title, true
		}
		if v := strings.TrimSpace(t.latin); v != "" {
			return t.latin, true
		}
		return "", false
	}
	return "", false
}

func langSet(rows []relTitle) []string {
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, t := range rows {
		if seen[t.lang] {
			continue
		}
		seen[t.lang] = true
		out = append(out, t.lang)
	}
	return out
}

func buildReleaseExtra(rid string, m relMeta, langs, plats []string) datatypes.JSON {
	extra := map[string]any{
		"vndb_id":  rid,
		"freeware": m.freeware,
		"official": m.official,
	}
	if m.minage != nil {
		extra["minage"] = *m.minage
	}
	if len(langs) > 0 {
		extra["languages"] = langs
	}
	if len(plats) > 0 {
		extra["platforms"] = plats
	}
	b, _ := json.Marshal(extra)
	return b
}

func parseVNDBReleased(released, maxYear int) (y int16, m, d *int16, ok bool) {
	v := sourcedate.VNDB(int64(released), maxYear)
	return v.Y, v.M, v.D, v.State == sourcedate.Dated
}

func (im *Importer) loadReleaseMeta() (map[string]relMeta, error) {
	var rows []struct {
		ID       string `gorm:"column:id"`
		OLang    string `gorm:"column:olang"`
		Released int    `gorm:"column:released"`
		Minage   *int16 `gorm:"column:minage"`
		Freeware bool   `gorm:"column:freeware"`
		Official bool   `gorm:"column:official"`
		Patch    bool   `gorm:"column:patch"`
	}
	if err := im.catalog.Raw(`SELECT id, olang, released, minage, freeware, official, patch FROM src_vndb.releases`).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]relMeta, len(rows))
	for _, r := range rows {
		m[r.ID] = relMeta{olang: r.OLang, released: r.Released, minage: r.Minage, freeware: r.Freeware, official: r.Official, patch: r.Patch}
	}
	return m, nil
}

func (im *Importer) loadReleasePlatforms() (map[string][]string, error) {
	var rows []struct {
		ID       string `gorm:"column:id"`
		Platform string `gorm:"column:platform"`
	}
	if err := im.catalog.Raw(`SELECT id, platform FROM src_vndb.releases_platforms ORDER BY id, platform`).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[string][]string{}
	for _, r := range rows {
		m[r.ID] = append(m[r.ID], r.Platform)
	}
	return m, nil
}

func (im *Importer) loadReleaseTitles() (map[string][]relTitle, error) {
	var rows []struct {
		ID    string `gorm:"column:id"`
		Lang  string `gorm:"column:lang"`
		MTL   bool   `gorm:"column:mtl"`
		Title string `gorm:"column:title"`
		Latin string `gorm:"column:latin"`
	}
	if err := im.catalog.Raw(`SELECT id, lang, mtl, title, latin FROM src_vndb.releases_titles ORDER BY id, lang`).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[string][]relTitle{}
	for _, r := range rows {
		m[r.ID] = append(m[r.ID], relTitle{lang: r.Lang, mtl: r.MTL, title: r.Title, latin: r.Latin})
	}
	return m, nil
}

func (im *Importer) loadReleaseVNCounts() (map[string]int, error) {
	var rows []struct {
		ID string `gorm:"column:id"`
		C  int    `gorm:"column:c"`
	}
	if err := im.catalog.Raw(`SELECT id, count(*) AS c FROM src_vndb.releases_vn GROUP BY id`).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.ID] = r.C
	}
	return m, nil
}

func capReleaseGatesByWork(gates []releaseGateRow, n int) []releaseGateRow {
	works := map[int64]bool{}
	for _, g := range gates {
		works[g.WorkID] = true
	}
	keys := make([]int64, 0, len(works))
	for k := range works {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if n < len(keys) {
		keys = keys[:n]
	}
	keep := make(map[int64]bool, len(keys))
	for _, k := range keys {
		keep[k] = true
	}
	out := gates[:0:0]
	for _, g := range gates {
		if keep[g.WorkID] {
			out = append(out, g)
		}
	}
	return out
}
