package getchuattach

import (
	"context"
	"fmt"
	"strconv"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
)

const (
	ruleEGXlinkTwin  = "rule:eg-xlink-twin"
	ruleEGXlinkMulti = "rule:eg-xlink-multi"
)

func loadJanVNDB(ctx context.Context, db *gorm.DB, vndb int16, snap *snapshot) error {
	var rows []struct {
		GTIN      int64 `gorm:"column:gtin"`
		ReleaseID int64 `gorm:"column:release_id"`
		WorkID    int64 `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT vr.gtin, rel.id AS release_id, rel.work_id
		FROM src_vndb.releases vr
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.external_id = vr.id
			AND r.link_kind = ? AND r.dead_at IS NULL
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL AND w.status IN (?, ?)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE vr.gtin <> 0`,
		model.EntityTypeRelease, vndb, model.LinkKindExact,
		model.WorkStatusLive, model.WorkStatusStub,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load vndb jan: %w", err)
	}
	for _, r := range rows {
		snap.janVNDB[r.GTIN] = append(snap.janVNDB[r.GTIN], releaseHit{ReleaseID: r.ReleaseID, WorkID: r.WorkID})
	}
	return nil
}

func loadBrandLabels(ctx context.Context, db *gorm.DB, eg int16, snap *snapshot) error {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		LabelID    int64  `gorm:"column:label_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS label_id
		FROM catalog_external_ref r
		JOIN catalog_label l ON l.id = r.entity_id AND l.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeLabel, eg, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg label anchors: %w", err)
	}
	for _, r := range rows {
		brandID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil || brandID == 0 {
			continue
		}
		snap.brandLabel[brandID] = r.LabelID
	}
	return nil
}

func loadEGStaging(ctx context.Context, db, egDB *gorm.DB, ids registryIDs, snap *snapshot) error {
	if err := loadEGBrands(ctx, egDB, snap); err != nil {
		return err
	}
	if err := loadEGGames(ctx, egDB, snap); err != nil {
		return err
	}
	if err := loadEGWorks(ctx, db, ids.eg, snap); err != nil {
		return err
	}
	return loadEGJan(ctx, egDB, snap)
}

func loadEGBrands(ctx context.Context, egDB *gorm.DB, snap *snapshot) error {
	var rows []struct {
		ID       int64  `gorm:"column:id"`
		Name     string `gorm:"column:brandname"`
		Furigana string `gorm:"column:brandfurigana"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT id,
			coalesce(raw->>'brandname', '') AS brandname,
			coalesce(raw->>'brandfurigana', '') AS brandfurigana
		FROM brands`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg brands: %w", err)
	}
	for _, r := range rows {
		keys := egBrandKeys(r.Name, r.Furigana)
		snap.egBrandKeys[r.ID] = keys
		for k := range keys {
			snap.egBrandByKey[k] = append(snap.egBrandByKey[k], r.ID)
		}
	}
	return nil
}

func loadEGGames(ctx context.Context, egDB *gorm.DB, snap *snapshot) error {
	var rows []egGame
	if err := egDB.WithContext(ctx).Raw(`
		SELECT id,
			coalesce(gamename, '') AS gamename,
			coalesce(sellday, '') AS sellday,
			coalesce(brand_id, 0) AS brand_id,
			coalesce(erogame, false) AS erogame
		FROM games`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg games: %w", err)
	}
	snap.egGames = rows
	for _, g := range rows {
		if g.Sellday != "" {
			snap.egByDay[g.Sellday] = append(snap.egByDay[g.Sellday], g)
		}
		if g.BrandID != 0 {
			snap.egByBrand[g.BrandID] = append(snap.egByBrand[g.BrandID], g)
		}
		if k := titlekey.Loose(g.Gamename); k != "" {
			snap.egByLoose[k] = append(snap.egByLoose[k], g)
		}
	}
	return nil
}

func loadEGWorks(ctx context.Context, db *gorm.DB, eg int16, snap *snapshot) error {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
		MatchedBy  string `gorm:"column:matched_by"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS work_id, r.link_kind, r.matched_by
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL AND w.status IN (?, ?)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = ? AND r.source_id = ? AND r.dead_at IS NULL`,
		model.WorkStatusLive, model.WorkStatusStub,
		model.EntityTypeWork, eg,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg works: %w", err)
	}
	for _, r := range rows {
		if r.LinkKind != model.LinkKindExact &&
			!(r.LinkKind == model.LinkKindRelated && r.MatchedBy != ruleEGXlinkTwin && r.MatchedBy != ruleEGXlinkMulti) {
			continue
		}
		egID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		snap.egWorks[egID] = append(snap.egWorks[egID], r.WorkID)
	}
	return nil
}

func loadEGJan(ctx context.Context, egDB *gorm.DB, snap *snapshot) error {
	var items []struct {
		ID  int64  `gorm:"column:id"`
		JAN string `gorm:"column:jan"`
	}
	if err := egDB.WithContext(ctx).Raw(`
		SELECT id, regexp_replace(coalesce(raw->>'jan', ''), '[^0-9]', '', 'g') AS jan
		FROM items`).Scan(&items).Error; err != nil {
		return fmt.Errorf("load eg items: %w", err)
	}
	janOf := map[int64]int64{}
	for _, r := range items {
		if n := parseJAN(r.JAN); n != 0 {
			janOf[r.ID] = n
		}
	}
	var links []struct {
		Item int64 `gorm:"column:item"`
		Game int64 `gorm:"column:game"`
	}
	if err := egDB.WithContext(ctx).Raw(`SELECT item, game FROM item_games`).Scan(&links).Error; err != nil {
		return fmt.Errorf("load eg item_games: %w", err)
	}
	seen := map[[2]int64]struct{}{}
	for _, l := range links {
		jan, ok := janOf[l.Item]
		if !ok {
			continue
		}
		for _, w := range uniqueIDs(snap.egWorks[l.Game]) {
			key := [2]int64{jan, w}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			snap.janEG[jan] = append(snap.janEG[jan], w)
		}
	}
	return nil
}
