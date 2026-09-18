package egworks

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
	eg, vndb, bangumi int16
	galgame           int16
}

type game struct {
	ID         int64
	Gamename   string
	Furigana   string
	Sellday    string
	Model      string
	BrandID    int64
	Erogame    bool
	Transplant bool
}

type holding struct {
	WorkID   int64
	LinkKind int16
}

type snapshot struct {
	games          []game
	holdings       map[int64][]holding
	exactAnywhere  map[int64]struct{}
	workHasPrimary map[int64]bool
	packSubjects   map[int64][]int64
	transplantObj  map[int64][]int64
	holders        map[int64][]int64
	rejected       map[string]struct{}
	titleIndex     map[string][]int64
	workDates      map[int64][]string
	workLabels     map[int64]map[int64]struct{}
	brandLabel     map[int64]int64
	workBgmDates   map[int64][]string
	vndbWorks      map[int64]struct{}
	registry       map[string]struct{}
	now            time.Time
	ids            registryIDs
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"erogamescape": &r.eg, "vndb": &r.vndb, "bangumi": &r.bangumi,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgame).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.eg == 0 || r.vndb == 0 || r.bangumi == 0 || r.galgame == 0 {
		return r, fmt.Errorf("registry not seeded (eg=%d vndb=%d bangumi=%d galgame=%d)", r.eg, r.vndb, r.bangumi, r.galgame)
	}
	return r, nil
}

func loadSnapshot(ctx context.Context, db, egDB *gorm.DB, ids registryIDs, now time.Time) (snapshot, error) {
	snap := snapshot{
		now: now, ids: ids,
		holdings:       map[int64][]holding{},
		exactAnywhere:  map[int64]struct{}{},
		workHasPrimary: map[int64]bool{},
		packSubjects:   map[int64][]int64{},
		transplantObj:  map[int64][]int64{},
		holders:        map[int64][]int64{},
		rejected:       map[string]struct{}{},
		titleIndex:     map[string][]int64{},
		workDates:      map[int64][]string{},
		workLabels:     map[int64]map[int64]struct{}{},
		brandLabel:     map[int64]int64{},
		workBgmDates:   map[int64][]string{},
		vndbWorks:      map[int64]struct{}{},
		registry:       map[string]struct{}{},
	}
	var err error
	if snap.games, err = loadGames(ctx, egDB); err != nil {
		return snap, err
	}
	if err = loadRelations(ctx, egDB, &snap); err != nil {
		return snap, err
	}
	if err = loadHoldings(ctx, db, ids.eg, &snap); err != nil {
		return snap, err
	}
	if snap.rejected, err = loadRejections(ctx, db, ids.eg); err != nil {
		return snap, err
	}
	if err = loadCorpus(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadDates(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadLabels(ctx, db, ids, &snap); err != nil {
		return snap, err
	}
	if err = loadBgmDates(ctx, db, ids.bangumi, &snap); err != nil {
		return snap, err
	}
	if err = loadVNDBWorks(ctx, db, ids.vndb, &snap); err != nil {
		return snap, err
	}
	if snap.registry, err = loadPlatformRegistry(ctx, db); err != nil {
		return snap, err
	}
	transplant := map[int64]struct{}{}
	for sub := range snap.transplantObj {
		transplant[sub] = struct{}{}
	}
	for i := range snap.games {
		_, snap.games[i].Transplant = transplant[snap.games[i].ID]
	}
	return snap, nil
}

func loadGames(ctx context.Context, egDB *gorm.DB) ([]game, error) {
	var rows []struct {
		ID       int64  `gorm:"column:id"`
		Gamename string `gorm:"column:gamename"`
		Furigana string `gorm:"column:furigana"`
		Sellday  string `gorm:"column:sellday"`
		Model    string `gorm:"column:model"`
		BrandID  int64  `gorm:"column:brand_id"`
		Erogame  bool   `gorm:"column:erogame"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT id,
			coalesce(gamename, '') AS gamename,
			coalesce(furigana, '') AS furigana,
			coalesce(sellday, '') AS sellday,
			coalesce(model, '') AS model,
			coalesce(brand_id, 0) AS brand_id,
			coalesce(erogame, false) AS erogame
		FROM games ORDER BY id`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load eg games: %w", err)
	}
	out := make([]game, len(rows))
	for i, r := range rows {
		out[i] = game{
			ID: r.ID, Gamename: r.Gamename, Furigana: r.Furigana,
			Sellday: r.Sellday, Model: r.Model, BrandID: r.BrandID, Erogame: r.Erogame,
		}
	}
	return out, nil
}

func loadRelations(ctx context.Context, egDB *gorm.DB, snap *snapshot) error {
	var packs []struct {
		Object  int64 `gorm:"column:game_object"`
		Subject int64 `gorm:"column:game_subject"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT game_object, game_subject FROM game_relations WHERE raw->>'kind' = 'bundling'`).
		Scan(&packs).Error; err != nil {
		return fmt.Errorf("load bundling: %w", err)
	}
	seenSub := map[[2]int64]struct{}{}
	for _, r := range packs {
		key := [2]int64{r.Object, r.Subject}
		if _, dup := seenSub[key]; dup {
			continue
		}
		seenSub[key] = struct{}{}
		snap.packSubjects[r.Object] = append(snap.packSubjects[r.Object], r.Subject)
	}
	var ports []struct {
		Subject int64 `gorm:"column:game_subject"`
		Object  int64 `gorm:"column:game_object"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT game_subject, game_object FROM game_relations WHERE raw->>'kind' = 'transplant'`).
		Scan(&ports).Error; err != nil {
		return fmt.Errorf("load transplant: %w", err)
	}
	seenPort := map[[2]int64]struct{}{}
	for _, r := range ports {
		key := [2]int64{r.Subject, r.Object}
		if _, dup := seenPort[key]; dup {
			continue
		}
		seenPort[key] = struct{}{}
		snap.transplantObj[r.Subject] = append(snap.transplantObj[r.Subject], r.Object)
	}
	return nil
}

func loadHoldings(ctx context.Context, db *gorm.DB, source int16, snap *snapshot) error {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS work_id, r.link_kind
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.dead_at IS NULL`,
		model.EntityTypeWork, source).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg holdings: %w", err)
	}
	// uq_catalog_external_ref_exact holds one exact row per EG id whatever became
	// of its work, so an id whose exact ref sits on a deleted work can never take
	// a second one: minting it would abort the whole mint chunk.
	var exact []string
	if err := db.WithContext(ctx).Raw(`
		SELECT external_id FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND link_kind = ?`,
		model.EntityTypeWork, source, model.LinkKindExact).Scan(&exact).Error; err != nil {
		return fmt.Errorf("load eg exact ids: %w", err)
	}
	for _, id := range exact {
		if egID, err := strconv.ParseInt(id, 10, 64); err == nil {
			snap.exactAnywhere[egID] = struct{}{}
		}
	}
	for _, r := range rows {
		egID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		snap.holdings[egID] = append(snap.holdings[egID], holding{WorkID: r.WorkID, LinkKind: r.LinkKind})
		if r.LinkKind == model.LinkKindExact || r.LinkKind == model.LinkKindProbable {
			snap.workHasPrimary[r.WorkID] = true
			snap.holders[egID] = append(snap.holders[egID], r.WorkID)
		}
	}
	return nil
}

func loadRejections(ctx context.Context, db *gorm.DB, source int16) (map[string]struct{}, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id, external_id FROM catalog_match_rejection
		WHERE entity_type = ? AND source_id = ?`, model.EntityTypeWork, source).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load rejections: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		egID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		out[rejKey(r.EntityID, egID)] = struct{}{}
	}
	return out, nil
}

func loadCorpus(ctx context.Context, db *gorm.DB, snap *snapshot) error {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Name   string `gorm:"column:name"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id, w.display_name AS name
		FROM catalog_work w
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE w.deleted_at IS NULL AND w.status IN (?, ?) AND w.display_name <> ''
		UNION ALL
		SELECT t.work_id, t.title
		FROM catalog_work_title t
		JOIN catalog_work w ON w.id = t.work_id AND w.deleted_at IS NULL AND w.status IN (?, ?)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE t.kind IN (?, ?, ?) AND t.title <> '' AND `+editspec.NotSuppressedWorkTitleSQL("t"),
		model.WorkStatusLive, model.WorkStatusStub,
		model.WorkStatusLive, model.WorkStatusStub,
		model.WorkTitleKindOfficial, model.WorkTitleKindAlias, model.WorkTitleKindAbbreviation,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load title corpus: %w", err)
	}
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
	for k, set := range idx {
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		snap.titleIndex[k] = ids
	}
	return nil
}

func loadDates(ctx context.Context, db *gorm.DB, snap *snapshot) error {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Day    string `gorm:"column:day"`
	}
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

func loadLabels(ctx context.Context, db *gorm.DB, ids registryIDs, snap *snapshot) error {
	var anchors []struct {
		ExternalID string `gorm:"column:external_id"`
		LabelID    int64  `gorm:"column:label_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS label_id
		FROM catalog_external_ref r
		JOIN catalog_label l ON l.id = r.entity_id AND l.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeLabel, ids.eg, model.LinkKindExact).Scan(&anchors).Error; err != nil {
		return fmt.Errorf("load eg label anchors: %w", err)
	}
	for _, r := range anchors {
		brandID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil || brandID == 0 {
			continue
		}
		snap.brandLabel[brandID] = r.LabelID
	}
	var edges []struct {
		WorkID  int64 `gorm:"column:work_id"`
		LabelID int64 `gorm:"column:label_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT work_id, label_id FROM catalog_work_label`).
		Scan(&edges).Error; err != nil {
		return fmt.Errorf("load work labels: %w", err)
	}
	for _, r := range edges {
		set := snap.workLabels[r.WorkID]
		if set == nil {
			set = map[int64]struct{}{}
			snap.workLabels[r.WorkID] = set
		}
		set[r.LabelID] = struct{}{}
	}
	return nil
}

func loadBgmDates(ctx context.Context, db *gorm.DB, bangumi int16, snap *snapshot) error {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Date   string `gorm:"column:date"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.entity_id AS work_id, s.date
		FROM catalog_external_ref r
		JOIN src_bangumi.subject s ON s.id::text = r.external_id
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		  AND s.date <> ''`,
		model.EntityTypeWork, bangumi, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load bangumi dates: %w", err)
	}
	for _, r := range rows {
		snap.workBgmDates[r.WorkID] = append(snap.workBgmDates[r.WorkID], r.Date)
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

func loadPlatformRegistry(ctx context.Context, db *gorm.DB) (map[string]struct{}, error) {
	var keys []string
	if err := db.WithContext(ctx).Raw(
		`SELECT key FROM catalog_platform WHERE NOT is_deprecated`).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("load platform registry: %w", err)
	}
	reg := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		reg[k] = struct{}{}
	}
	return reg, nil
}

func rejKey(workID, egID int64) string {
	return strconv.FormatInt(workID, 10) + "\x00" + strconv.FormatInt(egID, 10)
}
