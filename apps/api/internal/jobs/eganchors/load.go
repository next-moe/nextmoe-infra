package eganchors

import (
	"context"
	"fmt"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type registryIDs struct {
	vndb   int16
	dlsite int16
	eg     int16
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"vndb": &r.vndb, "dlsite": &r.dlsite, "erogamescape": &r.eg,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if r.vndb == 0 || r.dlsite == 0 || r.eg == 0 {
		return r, fmt.Errorf("registry not seeded (vndb=%d dlsite=%d eg=%d)", r.vndb, r.dlsite, r.eg)
	}
	return r, nil
}

func loadSnapshot(ctx context.Context, db, egDB *gorm.DB, ids registryIDs) (snapshot, error) {
	var snap snapshot
	var err error
	if snap.games, err = loadGames(ctx, egDB); err != nil {
		return snap, err
	}
	if snap.transplant, err = loadTransplantSubjects(ctx, egDB); err != nil {
		return snap, err
	}
	if snap.vndbLinks, err = loadVNDBDeclared(ctx, db); err != nil {
		return snap, err
	}
	if snap.vndbWork, err = loadTargetWorkRefs(ctx, db, ids.vndb, model.EntityTypeWork); err != nil {
		return snap, err
	}
	if snap.dlsiteWork, err = loadDLsiteTargets(ctx, db, ids.dlsite); err != nil {
		return snap, err
	}
	if snap.egHoldings, snap.workHasPrimary, err = loadEGHoldings(ctx, db, ids.eg); err != nil {
		return snap, err
	}
	if snap.rejected, err = loadRejections(ctx, db, ids.eg); err != nil {
		return snap, err
	}
	return snap, nil
}

func loadGames(ctx context.Context, egDB *gorm.DB) ([]egGame, error) {
	var rows []struct {
		ID       int64  `gorm:"column:id"`
		Gamename string `gorm:"column:gamename"`
		VNDB     string `gorm:"column:vndb"`
		DLsiteID string `gorm:"column:dlsite_id"`
		Model    string `gorm:"column:model"`
		Sellday  string `gorm:"column:sellday"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT id,
			coalesce(gamename, '') AS gamename,
			coalesce(vndb, '') AS vndb,
			coalesce(dlsite_id, '') AS dlsite_id,
			coalesce(model, '') AS model,
			coalesce(sellday, '') AS sellday
		FROM games`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load eg games: %w", err)
	}
	out := make([]egGame, len(rows))
	for i, r := range rows {
		out[i] = egGame{ID: r.ID, Gamename: r.Gamename, VNDB: r.VNDB, DLsiteID: r.DLsiteID, Model: r.Model, Sellday: r.Sellday}
	}
	return out, nil
}

func loadTransplantSubjects(ctx context.Context, egDB *gorm.DB) (map[int64]struct{}, error) {
	var ids []int64
	if err := egDB.WithContext(ctx).Raw(`
		SELECT DISTINCT game_subject FROM game_relations WHERE raw->>'kind' = 'transplant'`).
		Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load transplant subjects: %w", err)
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func loadVNDBDeclared(ctx context.Context, db *gorm.DB) (map[int64][]string, error) {
	var rows []struct {
		EgID string `gorm:"column:eg_id"`
		VID  string `gorm:"column:vid"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT e.value AS eg_id, ve.id AS vid
		FROM src_vndb.extlinks e
		JOIN src_vndb.vn_extlinks ve ON ve.link = e.id
		WHERE e.site = 'egs'
		UNION
		SELECT e.value AS eg_id, rv.vid AS vid
		FROM src_vndb.extlinks e
		JOIN src_vndb.releases_extlinks re ON re.link = e.id
		JOIN src_vndb.releases_vn rv ON rv.id = re.id
		WHERE e.site = 'egs'`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load vndb egs links: %w", err)
	}
	out := map[int64][]string{}
	seen := map[[2]string]struct{}{}
	for _, r := range rows {
		egID, err := strconv.ParseInt(r.EgID, 10, 64)
		if err != nil {
			continue
		}
		key := [2]string{r.EgID, r.VID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out[egID] = append(out[egID], r.VID)
	}
	return out, nil
}

func loadTargetWorkRefs(ctx context.Context, db *gorm.DB, source int16, entityType int16) (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS work_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = 0 AND r.dead_at IS NULL
			AND w.deleted_at IS NULL AND w.status IN (?, ?)`,
		entityType, source, model.WorkStatusLive, model.WorkStatusStub).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load target work refs: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ExternalID] = r.WorkID
	}
	return out, nil
}

func loadDLsiteTargets(ctx context.Context, db *gorm.DB, source int16) (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, rel.work_id
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = 0 AND r.dead_at IS NULL
			AND w.deleted_at IS NULL AND w.status IN (?, ?)`,
		model.EntityTypeRelease, source, model.WorkStatusLive, model.WorkStatusStub).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load dlsite targets: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ExternalID] = r.WorkID
	}
	return out, nil
}

func loadEGHoldings(ctx context.Context, db *gorm.DB, source int16) (map[int64][]holding, map[int64]bool, error) {
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
		return nil, nil, fmt.Errorf("load eg holdings: %w", err)
	}
	holds := map[int64][]holding{}
	holdsPrimary := map[int64]bool{}
	for _, r := range rows {
		egID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		holds[egID] = append(holds[egID], holding{WorkID: r.WorkID, LinkKind: r.LinkKind})
		if r.LinkKind == model.LinkKindExact || r.LinkKind == model.LinkKindProbable {
			holdsPrimary[r.WorkID] = true
		}
	}
	return holds, holdsPrimary, nil
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

func loadWorkCorroboration(ctx context.Context, db *gorm.DB, workIDs []int64) (map[int64][]string, map[int64][]string, error) {
	titles := map[int64][]string{}
	dates := map[int64][]string{}
	if len(workIDs) == 0 {
		return titles, dates, nil
	}
	var nameRows []struct {
		ID          int64  `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT id, display_name FROM catalog_work WHERE id IN ?`, workIDs,
	).Scan(&nameRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load work display names: %w", err)
	}
	for _, r := range nameRows {
		if r.DisplayName != "" {
			titles[r.ID] = append(titles[r.ID], r.DisplayName)
		}
	}
	var titleRows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Title  string `gorm:"column:title"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT work_id, title FROM catalog_work_title WHERE work_id IN ? AND kind IN (?, ?, ?)`,
		workIDs, model.WorkTitleKindOfficial, model.WorkTitleKindAlias, model.WorkTitleKindAbbreviation,
	).Scan(&titleRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load work titles: %w", err)
	}
	for _, r := range titleRows {
		titles[r.WorkID] = append(titles[r.WorkID], r.Title)
	}
	var dateRows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Day    string `gorm:"column:day"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT work_id,
		        lpad(released_y::text, 4, '0') || '-' || lpad(released_m::text, 2, '0') || '-' || lpad(released_d::text, 2, '0') AS day
		 FROM catalog_release
		 WHERE work_id IN ? AND deleted_at IS NULL
		   AND released_y IS NOT NULL AND released_m IS NOT NULL AND released_d IS NOT NULL`,
		workIDs,
	).Scan(&dateRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load work release dates: %w", err)
	}
	for _, r := range dateRows {
		dates[r.WorkID] = append(dates[r.WorkID], r.Day)
	}
	return titles, dates, nil
}
