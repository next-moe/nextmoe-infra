package workplatforms

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

type Opts struct {
	Apply     bool
	DSN       string
	DlsiteDSN string
	Source    string
}

type Stats struct {
	DlCandidates int
	DlNoMirror   int
	DlPlanned    int
	DlWritten    int
	DlRaced      int

	BgmWorks    int
	BgmPlanned  int
	BgmWritten  int
	BgmConflict int

	Unmapped map[string]int

	Errors int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.Source == "" {
		opts.Source = "all"
	}
	if (opts.Source == "dlsite" || opts.Source == "all") && opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("dlsite mirror DSN is required for the dlsite/all lanes (--dlsite-dsn)")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)

	registry, err := loadRegistry(ctx, db)
	if err != nil {
		return nil, err
	}

	st := &Stats{Unmapped: map[string]int{}}
	if opts.Source == "dlsite" || opts.Source == "all" {
		if err := runDlsite(ctx, db, opts, st); err != nil {
			return nil, err
		}
	}
	if opts.Source == "bgm" || opts.Source == "all" {
		if err := runBgm(ctx, db, opts, registry, st); err != nil {
			return nil, err
		}
	}
	slog.Info("workplatforms done", "apply", opts.Apply,
		"dl_candidates", st.DlCandidates, "dl_no_mirror", st.DlNoMirror,
		"dl_planned", st.DlPlanned, "dl_written", st.DlWritten, "dl_raced", st.DlRaced,
		"bgm_works", st.BgmWorks, "bgm_planned", st.BgmPlanned,
		"bgm_written", st.BgmWritten, "bgm_conflict", st.BgmConflict,
		"unmapped_kinds", len(st.Unmapped), "errors", st.Errors)
	return st, nil
}

func loadRegistry(ctx context.Context, db *gorm.DB) (map[string]struct{}, error) {
	var keys []string
	if err := db.WithContext(ctx).Raw(
		`SELECT key FROM catalog_platform WHERE NOT is_deprecated`).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("load platform registry: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("catalog_platform registry is empty — run migrate-catalog (seed) first")
	}
	reg := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		reg[k] = struct{}{}
	}
	return reg, nil
}

var aliasMap = map[string]string{
	"pc": "win", "windows": "win", "microsoft windows": "win", "win": "win",
	"android": "and", "web": "web", "browser": "web",
	"nintendo switch": "swi", "switch": "swi", "ns": "swi",
	"nintendo switch 2": "sw2",
	"ios":               "ios", "iphone": "ios", "ipad": "ios",
	"linux": "lin", "mac os": "mac", "macos": "mac", "mac": "mac", "os x": "mac",
	"ps5": "ps5", "playstation 5": "ps5",
	"ps4": "ps4", "playstation 4": "ps4",
	"ps3": "ps3", "playstation 3": "ps3",
	"ps2": "ps2", "playstation 2": "ps2",
	"ps1": "ps1", "playstation": "ps1", "psx": "ps1",
	"psv": "psv", "ps vita": "psv", "playstation vita": "psv", "psvita": "psv",
	"psp": "psp", "playstation portable": "psp",
	"pc-98": "p98", "pc98": "p98", "pc-9801": "p98",
	"pc-88": "p88", "pc88": "p88", "pc-8801": "p88",
	"dos": "dos", "ms-dos": "dos",
	"nds": "nds", "nintendo ds": "nds",
	"3ds": "n3d", "nintendo 3ds": "n3d",
	"xbox one": "xbo", "xbox 360": "xb3", "xbox series x/s": "xxs", "xbox series x|s": "xxs",
	"fm towns": "fmt", "fm-towns": "fmt", "x68000": "x68", "msx": "msx",
	"dreamcast": "drc", "dc": "drc", "sega saturn": "sat", "saturn": "sat", "ss": "sat",
	"wii": "wii", "wii u": "wiu", "gba": "gba", "game boy advance": "gba",
	"gbc": "gbc", "game boy color": "gbc",
	"fc": "nes", "famicom": "nes", "sfc": "sfc", "super famicom": "sfc",
	"md": "smd", "mega drive": "smd", "pc engine": "pce", "pce": "pce",
	"mac os x": "mac", "macosx": "mac", "macos x": "mac", "mac osx": "mac", "macintosh": "mac",
	"pc-fx": "pcf", "snes": "sfc", "ds": "nds", "3do": "tdo",
	"sega mega drive": "smd", "segasaturn": "sat", "sega cd": "scd",
	"wii virtual console": "wii", "dvdpg": "dvd",
	"steamos": "lin", "ubuntu/steamos": "lin",
	"andoroid": "and", "flash": "web", "html5": "web",
	"浏览器": "web", "网页": "web", "在线网页": "web", "安卓": "and",
}

var heuristicRules = []struct {
	re   *regexp.Regexp
	code string
}{
	{regexp.MustCompile(`^(nintendo|nitendo) sw`), "swi"},
	{regexp.MustCompile(`^xbox ?360`), "xb3"},
	{regexp.MustCompile(`^xbox (series|x/s)`), "xxs"},
	{regexp.MustCompile(`^xbox one`), "xbo"},
	{regexp.MustCompile(`^playstation ?vita`), "psv"},
	{regexp.MustCompile(`^playstation ?portable`), "psp"},
	{regexp.MustCompile(`^play ?station ?1\b`), "ps1"},
	{regexp.MustCompile(`^play ?station ?2\b`), "ps2"},
	{regexp.MustCompile(`^play ?station ?3\b`), "ps3"},
	{regexp.MustCompile(`^play ?station ?4\b`), "ps4"},
	{regexp.MustCompile(`^play ?station ?5\b`), "ps5"},
	{regexp.MustCompile(`^pc-?98`), "p98"},
	{regexp.MustCompile(`^pc-?88`), "p88"},
	{regexp.MustCompile(`^pc-engine`), "pce"},
	{regexp.MustCompile(`^msx`), "msx"},
	{regexp.MustCompile(`^fm-?7`), "fm7"},
	{regexp.MustCompile(`^fm-?8\b`), "fm8"},
	{regexp.MustCompile(`^(fm-?towns|towns)`), "fmt"},
	{regexp.MustCompile(`^(sharp ?x1|x1$)`), "x1s"},
	{regexp.MustCompile(`^(sharp ?)?x68`), "x68"},
	{regexp.MustCompile(`^mac`), "mac"},
	{regexp.MustCompile(`^web|^浏览器`), "web"},
	{regexp.MustCompile(`^安卓`), "and"},
	{regexp.MustCompile(`^win`), "win"},
	{regexp.MustCompile(`^pc([^a-z0-9]|$)`), "win"},
	{regexp.MustCompile(`windows`), "win"},
}

func Normalize(raw string, registry map[string]struct{}) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if code, ok := aliasMap[s]; ok {
		return code
	}
	if _, ok := registry[s]; ok {
		return s
	}
	stripped := strings.TrimSpace(strings.NewReplacer("®", "", "™", "").Replace(s))
	if code, ok := aliasMap[stripped]; ok {
		return code
	}
	if strings.Contains(stripped, "phone") {
		return ""
	}
	for _, r := range heuristicRules {
		if r.re.MatchString(stripped) {
			return r.code
		}
	}
	return ""
}

func runDlsite(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	var rows []struct {
		ReleaseID int64  `gorm:"column:release_id"`
		WorkID    int64  `gorm:"column:work_id"`
		Workno    string `gorm:"column:workno"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (rel.id) rel.id AS release_id, rel.work_id, r.external_id AS workno
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = 6 AND r.source_id = 4 AND r.link_kind = 0
		  AND coalesce(rel.platform, '') = ''
		ORDER BY rel.id, r.external_id`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load dlsite candidates: %w", err)
	}
	st.DlCandidates = len(rows)
	if len(rows) == 0 {
		return nil
	}

	dl, err := openGorm(opts.DlsiteDSN)
	if err != nil {
		return fmt.Errorf("connect dlsite mirror: %w", err)
	}
	defer closeGorm(dl)

	worknos := make([]string, 0, len(rows))
	for _, r := range rows {
		worknos = append(worknos, r.Workno)
	}
	mirrorCodes := map[string][]string{}
	for _, chunk := range chunkStr(worknos, 10000) {
		var t []struct {
			Workno   string `gorm:"column:workno"`
			Platform []byte `gorm:"column:platform"`
		}
		if err := dl.WithContext(ctx).Raw(`SELECT workno, product_json->'platform' AS platform
			FROM works WHERE workno IN ?`, chunk).Scan(&t).Error; err != nil {
			return fmt.Errorf("load mirror platforms: %w", err)
		}
		for _, r := range t {
			if codes := mapDlsite(r.Platform); len(codes) > 0 {
				mirrorCodes[r.Workno] = codes
			}
		}
	}

	var touched []int64
	for _, r := range rows {
		codes, ok := mirrorCodes[r.Workno]
		if !ok {
			st.DlNoMirror++
			continue
		}
		st.DlPlanned++
		if !opts.Apply {
			continue
		}
		arr, err := json.Marshal(codes)
		if err != nil {
			st.Errors++
			continue
		}
		res := db.WithContext(ctx).Exec(`UPDATE catalog_release
			SET platform = ?, extra = extra || jsonb_build_object('platforms', ?::jsonb)
			WHERE id = ? AND coalesce(platform, '') = ''
			  AND COALESCE(field_provenance -> 'platform' -> 0 ->> 'source', '') NOT IN ?`,
			codes[0], string(arr), r.ReleaseID, provenance.HumanSources())
		if res.Error != nil {
			st.Errors++
			slog.Warn("dlsite platform update", "release", r.ReleaseID, "err", res.Error)
			continue
		}
		if res.RowsAffected == 1 {
			st.DlWritten++
			touched = append(touched, r.WorkID)
		} else {
			st.DlRaced++
		}
	}
	return repository.TouchWorks(ctx, db, touched)
}

func mapDlsite(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	has := map[string]bool{}
	for _, it := range items {
		switch strings.ToLower(strings.TrimSpace(it)) {
		case "pc":
			has["win"] = true
		case "android":
			has["and"] = true
		case "ios":
			has["ios"] = true
		}
	}
	var out []string
	for _, code := range []string{"win", "and", "ios"} {
		if has[code] {
			out = append(out, code)
		}
	}
	return out
}

type infobox struct {
	Fields []struct {
		Key   string `json:"Key"`
		Array bool   `json:"Array"`
		Value string `json:"Value"`
		Items []struct {
			Value string `json:"Value"`
		} `json:"Items"`
	} `json:"Fields"`
}

func platformsFrom(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var ib infobox
	if err := json.Unmarshal(raw, &ib); err != nil {
		return nil
	}
	var out []string
	for _, f := range ib.Fields {
		if f.Key != "平台" {
			continue
		}
		if f.Array {
			for _, it := range f.Items {
				out = append(out, it.Value)
			}
			continue
		}
		out = append(out, f.Value)
	}
	return out
}

func runBgm(ctx context.Context, db *gorm.DB, opts Opts, registry map[string]struct{}, st *Stats) error {
	var bgmSourceID int16
	if err := db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&bgmSourceID).Error; err != nil || bgmSourceID == 0 {
		return fmt.Errorf("resolve bgm source id: %v", err)
	}

	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		Infobox []byte `gorm:"column:infobox_parsed"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (w.id) w.id AS work_id, s.infobox_parsed
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = 5 AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = 0
		JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
		WHERE coalesce(w.site,'') = '' AND w.deleted_at IS NULL
		  AND s.infobox_parsed IS NOT NULL
		ORDER BY w.id, r.external_id`, bgmSourceID).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load bgm candidates: %w", err)
	}

	type cand struct {
		workID int64
		code   string
	}
	var cands []cand
	for _, r := range rows {
		raws := platformsFrom(r.Infobox)
		if len(raws) == 0 {
			continue
		}
		st.BgmWorks++
		seen := map[string]struct{}{}
		for _, raw := range raws {
			code := Normalize(raw, registry)
			if code == "" {
				if s := strings.TrimSpace(raw); s != "" {
					st.Unmapped[s]++
				}
				continue
			}
			if _, dup := seen[code]; dup {
				continue
			}
			seen[code] = struct{}{}
			cands = append(cands, cand{r.WorkID, code})
		}
	}

	var touched []int64
	for _, c := range cands {
		st.BgmPlanned++
		if !opts.Apply {
			continue
		}
		res := db.WithContext(ctx).Exec(`INSERT INTO catalog_work_platform (work_id, platform, source_id)
			VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, c.workID, c.code, bgmSourceID)
		if res.Error != nil {
			st.Errors++
			slog.Warn("work platform insert", "work", c.workID, "err", res.Error)
			continue
		}
		if res.RowsAffected == 1 {
			st.BgmWritten++
			touched = append(touched, c.workID)
		} else {
			st.BgmConflict++
		}
	}
	return repository.TouchWorks(ctx, db, touched)
}

func (st *Stats) TopUnmapped(n int) []string {
	type kv struct {
		k string
		v int
	}
	all := make([]kv, 0, len(st.Unmapped))
	for k, v := range st.Unmapped {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, fmt.Sprintf("%s×%d", e.k, e.v))
	}
	return out
}

func chunkStr(in []string, size int) [][]string {
	var out [][]string
	for len(in) > size {
		out = append(out, in[:size])
		in = in[size:]
	}
	if len(in) > 0 {
		out = append(out, in)
	}
	return out
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
