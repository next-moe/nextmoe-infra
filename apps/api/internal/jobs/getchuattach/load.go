package getchuattach

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
)

type registryIDs struct {
	getchu, vndb, bangumi, eg int16
	galgame                   int16
}

type item struct {
	GetchuID    string
	Title       string
	Brand       string
	ReleaseDate string
	JAN         int64
	Adult       bool
	BrandID     int64
	Genre       string
	Subgenre    string
	Media       string
}

type egGame struct {
	ID       int64
	Gamename string
	Sellday  string
	BrandID  int64
	Erogame  bool
}

type releaseHit struct {
	ReleaseID int64
	WorkID    int64
}

type snapshot struct {
	items         []item
	exactAnywhere map[string]struct{}
	exactWork     map[string]int64
	rejected      map[string]struct{}
	titleIndex    map[string][]int64
	workDates     map[int64][]string
	vndbWorks     map[int64]struct{}
	janVNDB       map[int64][]releaseHit
	janEG         map[int64][]int64
	egGames       []egGame
	egByDay       map[string][]egGame
	egByBrand     map[int64][]egGame
	egByLoose     map[string][]egGame
	egWorks       map[int64][]int64
	egBrandKeys   map[int64]map[string]struct{}
	egBrandByKey  map[string][]int64
	brandLabel    map[int64]int64
	relTitleIndex map[string][]int64
	relLooseIndex map[string][]int64
	now           time.Time
	ids           registryIDs
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"getchu": &r.getchu, "vndb": &r.vndb, "bangumi": &r.bangumi, "erogamescape": &r.eg,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgame).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.getchu == 0 || r.vndb == 0 || r.bangumi == 0 || r.eg == 0 || r.galgame == 0 {
		return r, fmt.Errorf("registry not seeded (getchu=%d vndb=%d bangumi=%d eg=%d galgame=%d)", r.getchu, r.vndb, r.bangumi, r.eg, r.galgame)
	}
	return r, nil
}

func loadSnapshot(ctx context.Context, db, gcDB, egDB *gorm.DB, ids registryIDs, now time.Time) (snapshot, error) {
	snap := snapshot{
		now: now, ids: ids,
		exactAnywhere: map[string]struct{}{},
		exactWork:     map[string]int64{},
		rejected:      map[string]struct{}{},
		titleIndex:    map[string][]int64{},
		workDates:     map[int64][]string{},
		vndbWorks:     map[int64]struct{}{},
		janVNDB:       map[int64][]releaseHit{},
		janEG:         map[int64][]int64{},
		egByDay:       map[string][]egGame{},
		egByBrand:     map[int64][]egGame{},
		egByLoose:     map[string][]egGame{},
		egWorks:       map[int64][]int64{},
		egBrandKeys:   map[int64]map[string]struct{}{},
		egBrandByKey:  map[string][]int64{},
		brandLabel:    map[int64]int64{},
		relTitleIndex: map[string][]int64{},
		relLooseIndex: map[string][]int64{},
	}
	var err error
	if snap.items, err = loadItems(ctx, gcDB); err != nil {
		return snap, err
	}
	if err = loadExactRefs(ctx, db, ids.getchu, &snap); err != nil {
		return snap, err
	}
	if snap.rejected, err = loadRejections(ctx, db, ids.getchu); err != nil {
		return snap, err
	}
	if err = loadCorpus(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadRelationCorpus(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadDates(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadVNDBWorks(ctx, db, ids.vndb, &snap); err != nil {
		return snap, err
	}
	if err = loadJanVNDB(ctx, db, ids.vndb, &snap); err != nil {
		return snap, err
	}
	if err = loadBrandLabels(ctx, db, ids.eg, &snap); err != nil {
		return snap, err
	}
	if err = loadEGStaging(ctx, db, egDB, ids, &snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func loadItems(ctx context.Context, gcDB *gorm.DB) ([]item, error) {
	var rows []struct {
		GetchuID    string `gorm:"column:getchu_id"`
		Title       string `gorm:"column:title"`
		Brand       string `gorm:"column:brand"`
		ReleaseDate string `gorm:"column:release_date"`
		JAN         string `gorm:"column:jan"`
		Adult       bool   `gorm:"column:adult"`
		BrandID     string `gorm:"column:brand_id"`
		Genre       string `gorm:"column:genre"`
		Subgenre    string `gorm:"column:subgenre"`
		Media       string `gorm:"column:media"`
	}
	if err := gcDB.WithContext(ctx).Raw(`
		SELECT getchu_id,
			coalesce(title, '') AS title,
			coalesce(brand, '') AS brand,
			coalesce(release_date, '') AS release_date,
			regexp_replace(coalesce(parsed_json->'Info'->>'JANコード', ''), '[^0-9]', '', 'g') AS jan,
			(position('18歳未満の方は購入できません' in coalesce(raw_html, '')) > 0) AS adult,
			coalesce(parsed_json->>'BrandID', '') AS brand_id,
			coalesce(parsed_json->'Info'->>'ジャンル', '') AS genre,
			coalesce(parsed_json->'Info'->>'サブジャンル', '') AS subgenre,
			coalesce(parsed_json->'Info'->>'メディア', '') AS media
		FROM items
		WHERE status = 'fetched' AND btrim(coalesce(title, '')) <> ''
		ORDER BY getchu_id`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load getchu items: %w", err)
	}
	out := make([]item, len(rows))
	for i, r := range rows {
		out[i] = item{
			GetchuID: r.GetchuID, Title: r.Title, Brand: r.Brand, ReleaseDate: r.ReleaseDate,
			JAN: parseJAN(r.JAN), Adult: r.Adult, BrandID: parseInt64(r.BrandID),
			Genre: r.Genre, Subgenre: r.Subgenre, Media: r.Media,
		}
	}
	return out, nil
}

func loadExactRefs(ctx context.Context, db *gorm.DB, source int16, snap *snapshot) error {
	// uq_catalog_external_ref_exact holds one exact row per Getchu id whatever
	// the state of its release or work, so an id anchored on a deleted release
	// can never take a second.
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, coalesce(rel.work_id, 0) AS work_id
		FROM catalog_external_ref r
		LEFT JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeRelease, source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load getchu exact refs: %w", err)
	}
	for _, r := range rows {
		snap.exactAnywhere[r.ExternalID] = struct{}{}
		if r.WorkID != 0 {
			snap.exactWork[r.ExternalID] = r.WorkID
		}
	}
	return nil
}

func loadRejections(ctx context.Context, db *gorm.DB, source int16) (map[string]struct{}, error) {
	var rows []struct {
		EntityType int16  `gorm:"column:entity_type"`
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT rej.entity_type, rej.entity_id, rej.external_id, coalesce(rel.work_id, 0) AS work_id
		FROM catalog_match_rejection rej
		LEFT JOIN catalog_release rel ON rel.id = rej.entity_id
		WHERE rej.source_id = ? AND rej.entity_type IN (?, ?)`,
		source, model.EntityTypeWork, model.EntityTypeRelease).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load rejections: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		workID := r.EntityID
		if r.EntityType == model.EntityTypeRelease {
			if r.WorkID == 0 {
				continue
			}
			workID = r.WorkID
		}
		out[rejKey(workID, r.ExternalID)] = struct{}{}
	}
	return out, nil
}

func loadCorpus(ctx context.Context, db *gorm.DB, snap *snapshot) error {
	rows, err := loadWorkNames(ctx, db, model.WorkStatusLive, model.WorkStatusStub)
	if err != nil {
		return fmt.Errorf("load title corpus: %w", err)
	}
	snap.titleIndex = indexKeys(rows)
	return nil
}

func loadRelationCorpus(ctx context.Context, db *gorm.DB, snap *snapshot) error {
	rows, err := loadWorkNames(ctx, db, model.WorkStatusLive, model.WorkStatusStub, model.WorkStatusQuarantine)
	if err != nil {
		return fmt.Errorf("load relation corpus: %w", err)
	}
	snap.relTitleIndex = indexKeys(rows)
	loose := map[string]map[int64]struct{}{}
	for _, r := range rows {
		k := titlekey.Loose(r.Name)
		if k == "" {
			continue
		}
		set := loose[k]
		if set == nil {
			set = map[int64]struct{}{}
			loose[k] = set
		}
		set[r.WorkID] = struct{}{}
	}
	snap.relLooseIndex = flattenIDSet(loose)
	return nil
}

type nameRow struct {
	WorkID int64  `gorm:"column:work_id"`
	Name   string `gorm:"column:name"`
}

func loadWorkNames(ctx context.Context, db *gorm.DB, statuses ...int16) ([]nameRow, error) {
	var rows []nameRow
	args := make([]any, 0, len(statuses)*2+3)
	for _, s := range statuses {
		args = append(args, s)
	}
	for _, s := range statuses {
		args = append(args, s)
	}
	args = append(args, model.WorkTitleKindOfficial, model.WorkTitleKindAlias, model.WorkTitleKindAbbreviation)
	statusSQL := placeholders(len(statuses))
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id, w.display_name AS name
		FROM catalog_work w
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE w.deleted_at IS NULL AND w.status IN (`+statusSQL+`) AND w.display_name <> ''
		UNION ALL
		SELECT t.work_id, t.title
		FROM catalog_work_title t
		JOIN catalog_work w ON w.id = t.work_id AND w.deleted_at IS NULL AND w.status IN (`+statusSQL+`)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE t.kind IN (?, ?, ?) AND t.title <> '' AND `+editspec.NotSuppressedWorkTitleSQL("t"),
		args...,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func loadDates(ctx context.Context, db *gorm.DB, snap *snapshot) error {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Day    string `gorm:"column:day"`
	}
	// Compared as a zero-padded string built with lpad — not make_date; one bad
	// row would abort the query.
	if err := db.WithContext(ctx).Raw(`
		SELECT rel.work_id,
		       lpad(rel.released_y::text, 4, '0') || '-' || lpad(rel.released_m::text, 2, '0') || '-' || lpad(rel.released_d::text, 2, '0') AS day
		FROM catalog_release rel
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE rel.deleted_at IS NULL
		  AND rel.released_y IS NOT NULL AND rel.released_m IS NOT NULL AND rel.released_d IS NOT NULL`).
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("load release dates: %w", err)
	}
	for _, r := range rows {
		snap.workDates[r.WorkID] = append(snap.workDates[r.WorkID], r.Day)
	}
	return nil
}

func loadVNDBWorks(ctx context.Context, db *gorm.DB, vndb int16, snap *snapshot) error {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`
		SELECT r.entity_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeWork, vndb, model.LinkKindExact).Scan(&ids).Error; err != nil {
		return fmt.Errorf("load vndb works: %w", err)
	}
	for _, id := range ids {
		snap.vndbWorks[id] = struct{}{}
	}
	return nil
}

func rejKey(workID int64, getchuID string) string {
	return fmt.Sprintf("%d\x00%s", workID, getchuID)
}

func parseJAN(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n == 0 {
		return 0
	}
	return n
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func placeholders(n int) string {
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

func indexKeys(rows []nameRow) map[string][]int64 {
	idx := map[string]map[int64]struct{}{}
	for _, r := range rows {
		for _, k := range titlekey.Keys(r.Name) {
			set := idx[k]
			if set == nil {
				set = map[int64]struct{}{}
				idx[k] = set
			}
			set[r.WorkID] = struct{}{}
		}
	}
	return flattenIDSet(idx)
}

func flattenIDSet(idx map[string]map[int64]struct{}) map[string][]int64 {
	out := make(map[string][]int64, len(idx))
	for k, set := range idx {
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		out[k] = ids
	}
	return out
}
