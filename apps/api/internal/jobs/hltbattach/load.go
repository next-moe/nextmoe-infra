package hltbattach

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
)

type registryIDs struct {
	hltb, vndb int16
	galgame    int16
}

type game struct {
	ID    int64
	Name  string
	Alias string
	World string
	Japan string
}

type snapshot struct {
	games         []game
	claimed       map[int64]struct{}
	exactHoldings map[int64][]int64
	rejected      map[string]struct{}
	titleIndex    map[string][]int64
	workDates     map[int64][]string
	vndbWorks     map[int64]struct{}
	ids           registryIDs
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"howlongtobeat": &r.hltb, "vndb": &r.vndb,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgame).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.hltb == 0 || r.vndb == 0 || r.galgame == 0 {
		return r, fmt.Errorf("registry not seeded (howlongtobeat=%d vndb=%d galgame=%d)", r.hltb, r.vndb, r.galgame)
	}
	return r, nil
}

func loadSnapshot(ctx context.Context, db, hltbDB *gorm.DB, ids registryIDs) (snapshot, error) {
	snap := snapshot{
		ids:           ids,
		claimed:       map[int64]struct{}{},
		exactHoldings: map[int64][]int64{},
		rejected:      map[string]struct{}{},
		titleIndex:    map[string][]int64{},
		workDates:     map[int64][]string{},
		vndbWorks:     map[int64]struct{}{},
	}
	var err error
	if snap.games, err = loadGames(ctx, hltbDB); err != nil {
		return snap, err
	}
	if err = loadClaimed(ctx, db, ids.hltb, &snap); err != nil {
		return snap, err
	}
	if snap.rejected, err = loadRejections(ctx, db, ids.hltb); err != nil {
		return snap, err
	}
	if err = loadCorpus(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadDates(ctx, db, &snap); err != nil {
		return snap, err
	}
	if err = loadVNDBWorks(ctx, db, ids.vndb, &snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func loadGames(ctx context.Context, hltbDB *gorm.DB) ([]game, error) {
	var rows []struct {
		ID    int64  `gorm:"column:hltb_id"`
		Name  string `gorm:"column:game_name"`
		Alias string `gorm:"column:game_alias"`
		World string `gorm:"column:release_world"`
		Japan string `gorm:"column:release_jp"`
	}
	if err := hltbDB.WithContext(ctx).Raw(`
		SELECT hltb_id,
			coalesce(raw #> '{data,game,0}' ->> 'game_name', '') AS game_name,
			coalesce(raw #> '{data,game,0}' ->> 'game_alias', '') AS game_alias,
			coalesce(raw #> '{data,game,0}' ->> 'release_world', '') AS release_world,
			coalesce(raw #> '{data,game,0}' ->> 'release_jp', '') AS release_jp
		FROM games
		WHERE status = 'fetched'
		ORDER BY hltb_id`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load hltb games: %w", err)
	}
	out := make([]game, len(rows))
	for i, r := range rows {
		out[i] = game{ID: r.ID, Name: r.Name, Alias: r.Alias, World: r.World, Japan: r.Japan}
	}
	return out, nil
}

func loadClaimed(ctx context.Context, db *gorm.DB, source int16, snap *snapshot) error {
	var ids []string
	if err := db.WithContext(ctx).Raw(`
		SELECT external_id FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ?`,
		model.EntityTypeWork, source).Scan(&ids).Error; err != nil {
		return fmt.Errorf("load hltb claimed ids: %w", err)
	}
	for _, id := range ids {
		hltbID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			continue
		}
		snap.claimed[hltbID] = struct{}{}
	}
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id AS work_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeWork, source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load hltb exact holdings: %w", err)
	}
	for _, r := range rows {
		hltbID, err := strconv.ParseInt(r.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		snap.exactHoldings[hltbID] = append(snap.exactHoldings[hltbID], r.WorkID)
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
		out[rejKey(r.EntityID, r.ExternalID)] = struct{}{}
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

func splitAliases(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func rejKey(workID int64, externalID string) string {
	return strconv.FormatInt(workID, 10) + "\x00" + externalID
}
