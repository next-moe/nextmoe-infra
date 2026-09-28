package storerefs

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type Opts struct {
	Apply bool
	DSN   string
	EGDSN string
	Only  string
}

const (
	LaneEG  = "eg"
	LaneBgm = "bgm"
)

type Stats struct {
	Anchored     int
	SteamPlanned int
	SteamWritten int
	SteamExists  int
	DmmPlanned   int
	DmmWritten   int
	DmmExists    int

	BgmAnchored  int
	BgmStated    int
	BgmAmbiguous int
	BgmHasSteam  int
	BgmPlanned   int
	BgmWritten   int
	BgmExists    int

	Rejected int
	Errors   int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	runEG, runBgm := opts.Only == "" || opts.Only == LaneEG, opts.Only == "" || opts.Only == LaneBgm
	if !runEG && !runBgm {
		return nil, fmt.Errorf("unknown lane %q (want %s, %s, or empty for both)", opts.Only, LaneEG, LaneBgm)
	}
	if opts.DSN == "" || (runEG && opts.EGDSN == "") {
		return nil, fmt.Errorf("--dsn is required, and --eg-dsn for the eg lane")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)

	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	rejected, err := loadRejections(ctx, db, []int16{ids.steamSource, ids.dmmSource})
	if err != nil {
		return nil, err
	}

	st := &Stats{}
	var touched []int64
	if runEG {
		t, err := runEGLane(ctx, db, opts, ids, rejected, st)
		if err != nil {
			return nil, err
		}
		touched = append(touched, t...)
	}
	if runBgm {
		t, err := runBgmLane(ctx, db, opts, ids, rejected, st)
		if err != nil {
			return nil, err
		}
		touched = append(touched, t...)
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}
	slog.Info("storerefs done", "apply", opts.Apply, "lane", opts.Only, "anchored", st.Anchored,
		"steam_planned", st.SteamPlanned, "steam_written", st.SteamWritten, "steam_exists", st.SteamExists,
		"dmm_planned", st.DmmPlanned, "dmm_written", st.DmmWritten, "dmm_exists", st.DmmExists,
		"bgm_anchored", st.BgmAnchored, "bgm_stated", st.BgmStated, "bgm_ambiguous", st.BgmAmbiguous,
		"bgm_has_steam", st.BgmHasSteam, "bgm_planned", st.BgmPlanned,
		"bgm_written", st.BgmWritten, "bgm_exists", st.BgmExists,
		"rejected", st.Rejected, "errors", st.Errors)
	return st, nil
}

func runEGLane(ctx context.Context, db *gorm.DB, opts Opts, ids registryIDs, rejected map[string]struct{}, st *Stats) ([]int64, error) {
	egDB, err := openGorm(opts.EGDSN)
	if err != nil {
		return nil, fmt.Errorf("connect eg mirror: %w", err)
	}
	defer closeGorm(egDB)

	anchors, err := loadAnchors(ctx, db, ids.galgameMedium, ids.egSource)
	if err != nil {
		return nil, fmt.Errorf("load eg anchors: %w", err)
	}
	st.Anchored = len(anchors)

	gameIDs := make([]int64, 0, len(anchors))
	for _, a := range anchors {
		if n, err := strconv.ParseInt(a.ExternalID, 10, 64); err == nil {
			gameIDs = append(gameIDs, n)
		}
	}
	type xref struct {
		Steam *int64
		Dmm   string
	}
	xrefs := map[int64]xref{}
	for _, chunk := range chunkInt64(gameIDs, 10000) {
		var rows []struct {
			ID    int64  `gorm:"column:id"`
			Steam *int64 `gorm:"column:steam"`
			Dmm   string `gorm:"column:dmm"`
		}
		if err := egDB.WithContext(ctx).
			Raw(`SELECT id, steam, coalesce(dmm, '') AS dmm FROM games
				WHERE id IN ? AND (steam IS NOT NULL OR coalesce(dmm, '') <> '')`, chunk).
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("load eg xrefs: %w", err)
		}
		for _, r := range rows {
			xrefs[r.ID] = xref{Steam: r.Steam, Dmm: r.Dmm}
		}
	}

	var touched []int64
	for _, a := range anchors {
		n, err := strconv.ParseInt(a.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		x, ok := xrefs[n]
		if !ok {
			continue
		}
		if x.Steam != nil {
			if writeRef(ctx, db, opts.Apply, a.WorkID, ids.steamSource,
				strconv.FormatInt(*x.Steam, 10), "rule:eg-steam", rejected,
				&st.SteamPlanned, &st.SteamWritten, &st.SteamExists, &st.Rejected, &st.Errors) {
				touched = append(touched, a.WorkID)
			}
		}
		if x.Dmm != "" {
			if writeRef(ctx, db, opts.Apply, a.WorkID, ids.dmmSource,
				x.Dmm, "rule:eg-dmm", rejected,
				&st.DmmPlanned, &st.DmmWritten, &st.DmmExists, &st.Rejected, &st.Errors) {
				touched = append(touched, a.WorkID)
			}
		}
	}
	return touched, nil
}

func writeRef(ctx context.Context, db *gorm.DB, apply bool, workID int64, sourceID int16,
	externalID, rule string, rejected map[string]struct{},
	planned, written, exists, rejectedN, errors *int) bool {
	if _, hit := rejected[rejKey(workID, sourceID, externalID)]; hit {
		*rejectedN++
		return false
	}
	*planned++
	if !apply {
		return false
	}
	wrote, err := repository.InsertRefIfAbsent(db.WithContext(ctx), model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID,
		SourceID: sourceID, ExternalID: externalID,
		LinkKind: model.LinkKindProbable, MatchedBy: rule,
	})
	if err != nil {
		*errors++
		slog.Warn("insert store ref", "work", workID, "source", sourceID, "ext", externalID, "err", err)
		return false
	}
	if wrote {
		*written++
	} else {
		*exists++
	}
	return wrote
}

type registryIDs struct {
	galgameMedium int16
	egSource      int16
	bgmSource     int16
	steamSource   int16
	dmmSource     int16
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"erogamescape": &r.egSource, "bangumi": &r.bgmSource, "steam": &r.steamSource, "dmm": &r.dmmSource,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.galgameMedium == 0 || r.egSource == 0 || r.bgmSource == 0 || r.steamSource == 0 || r.dmmSource == 0 {
		return r, fmt.Errorf("registry not seeded (medium=%d eg=%d bangumi=%d steam=%d dmm=%d)",
			r.galgameMedium, r.egSource, r.bgmSource, r.steamSource, r.dmmSource)
	}
	return r, nil
}

type anchor struct {
	WorkID     int64  `gorm:"column:work_id"`
	ExternalID string `gorm:"column:external_id"`
}

func loadAnchors(ctx context.Context, db *gorm.DB, medium, source int16) ([]anchor, error) {
	var out []anchor
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (w.id) w.id AS work_id, r.external_id
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = 5 AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = 0
		WHERE w.medium_id = ? AND w.deleted_at IS NULL
		ORDER BY w.id, r.external_id`, source, medium).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func loadRejections(ctx context.Context, db *gorm.DB, sources []int16) (map[string]struct{}, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id, source_id, external_id FROM catalog_match_rejection
		WHERE entity_type = 5 AND source_id IN ?`, sources).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load rejections: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[rejKey(r.EntityID, r.SourceID, r.ExternalID)] = struct{}{}
	}
	return out, nil
}

func rejKey(entityID int64, sourceID int16, externalID string) string {
	return fmt.Sprintf("%d\x00%d\x00%s", entityID, sourceID, externalID)
}

func chunkInt64(in []int64, size int) [][]int64 {
	var out [][]int64
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
