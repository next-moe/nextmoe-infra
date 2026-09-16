package workseries

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/jobs/seriesorder"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type seriesInfo struct {
	name    string
	members map[int64]struct{}
}

type Opts struct {
	Apply     bool
	DSN       string
	DlsiteDSN string
}

type Stats struct {
	AnchoredWorks  int
	SkippedBundle  int
	SeriesEligible int
	MembersWanted  int

	SeriesCreated int
	SeriesRenamed int
	SeriesDeleted int
	MembersAdded  int
	MembersStale  int
	OrderChanged  int

	Errors int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("both --dsn and --dlsite-dsn are required")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	dl, err := openGorm(opts.DlsiteDSN)
	if err != nil {
		return nil, fmt.Errorf("connect dlsite mirror: %w", err)
	}
	defer closeGorm(dl)

	var dlsiteSrc int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsiteSrc).Error; err != nil {
		return nil, fmt.Errorf("resolve dlsite source: %w", err)
	}
	if dlsiteSrc == 0 {
		return nil, fmt.Errorf("registry not seeded (dlsite source missing)")
	}

	var anchors []struct {
		ExternalID string `gorm:"column:external_id"`
		WorkID     int64  `gorm:"column:work_id"`
		Bundle     bool   `gorm:"column:bundle"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT r.external_id, rel.work_id, `+repository.BundleReleaseSQL("rel")+` AS bundle
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = 6 AND r.source_id = ? AND r.link_kind = 0`, dlsiteSrc).
		Scan(&anchors).Error; err != nil {
		return nil, fmt.Errorf("load dlsite anchors: %w", err)
	}
	workByWorkno := make(map[string]int64, len(anchors))
	distinctWorks := map[int64]struct{}{}
	skippedBundle := 0
	for _, a := range anchors {
		if a.Bundle {
			skippedBundle++
			continue
		}
		workByWorkno[a.ExternalID] = a.WorkID
		distinctWorks[a.WorkID] = struct{}{}
	}
	st := &Stats{AnchoredWorks: len(distinctWorks), SkippedBundle: skippedBundle}

	worknos := make([]string, 0, len(workByWorkno))
	for wn := range workByWorkno {
		worknos = append(worknos, wn)
	}
	sort.Strings(worknos)
	series := map[string]*seriesInfo{}
	for _, chunk := range chunkStr(worknos, 10000) {
		var rows []struct {
			Workno     string `gorm:"column:workno"`
			SeriesID   string `gorm:"column:series_id"`
			SeriesName string `gorm:"column:series_name"`
		}
		if err := dl.WithContext(ctx).Raw(`
			SELECT workno, product_json->>'series_id' AS series_id,
			       coalesce(product_json->>'series_name','') AS series_name
			FROM works
			WHERE workno IN ? AND coalesce(product_json->>'series_id','') <> ''`, chunk).
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("load mirror series: %w", err)
		}
		for _, r := range rows {
			si := series[r.SeriesID]
			if si == nil {
				si = &seriesInfo{members: map[int64]struct{}{}}
				series[r.SeriesID] = si
			}
			if si.name == "" && strings.TrimSpace(r.SeriesName) != "" {
				si.name = strings.TrimSpace(r.SeriesName)
			}
			si.members[workByWorkno[r.Workno]] = struct{}{}
		}
	}

	want := map[string]*seriesInfo{}
	for sid, si := range series {
		if len(si.members) >= 2 {
			if si.name == "" {
				si.name = sid
			}
			want[sid] = si
			st.MembersWanted += len(si.members)
		}
	}
	st.SeriesEligible = len(want)

	var existing []struct {
		ID          int64  `gorm:"column:id"`
		ExternalID  string `gorm:"column:external_id"`
		DisplayName string `gorm:"column:display_name"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, external_id, display_name FROM catalog_series
		WHERE source_id = ?`, dlsiteSrc).Scan(&existing).Error; err != nil {
		return nil, fmt.Errorf("load existing series: %w", err)
	}
	existingByExt := make(map[string]struct {
		id   int64
		name string
	}, len(existing))
	for _, e := range existing {
		existingByExt[e.ExternalID] = struct {
			id   int64
			name string
		}{e.ID, e.DisplayName}
	}

	for sid, si := range want {
		e, ok := existingByExt[sid]
		switch {
		case !ok:
			st.SeriesCreated++
		case e.name != si.name:
			st.SeriesRenamed++
		}
	}
	for sid := range existingByExt {
		if _, ok := want[sid]; !ok {
			st.SeriesDeleted++
		}
	}

	if !opts.Apply {
		planMembers(ctx, db, want, existingByExt, st)
		dryIDs := make(map[string]int64, len(existingByExt))
		for sid, e := range existingByExt {
			dryIDs[sid] = e.id
		}
		if _, err := reconcileOrder(ctx, db, want, dryIDs, st, false); err != nil {
			return nil, err
		}
		logDone(st, opts.Apply)
		return st, nil
	}

	var touched []int64

	idByExt := make(map[string]int64, len(want))
	for sid, si := range want {
		e, ok := existingByExt[sid]
		if !ok {
			var id int64
			if err := db.WithContext(ctx).Raw(`INSERT INTO catalog_series (display_name, source_id, external_id)
				VALUES (?, ?, ?) RETURNING id`, si.name, dlsiteSrc, sid).Scan(&id).Error; err != nil {
				st.Errors++
				slog.Warn("series insert", "ext", sid, "err", err)
				continue
			}
			idByExt[sid] = id
			continue
		}
		idByExt[sid] = e.id
		if e.name != si.name {
			if err := db.WithContext(ctx).Exec(`UPDATE catalog_series SET display_name = ?, updated_at = now()
				WHERE id = ?`, si.name, e.id).Error; err != nil {
				st.Errors++
				slog.Warn("series rename", "ext", sid, "err", err)
				continue
			}
			for w := range si.members {
				touched = append(touched, w)
			}
		}
	}
	for sid, e := range existingByExt {
		if _, ok := want[sid]; ok {
			continue
		}
		var orphaned []int64
		if err := db.WithContext(ctx).Raw(`DELETE FROM catalog_series_member WHERE series_id = ?
			RETURNING work_id`, e.id).Scan(&orphaned).Error; err != nil {
			st.Errors++
			slog.Warn("series member cascade", "ext", sid, "err", err)
			continue
		}
		touched = append(touched, orphaned...)
		if err := db.WithContext(ctx).Exec(`DELETE FROM catalog_series WHERE id = ?`, e.id).Error; err != nil {
			st.Errors++
			slog.Warn("series delete", "ext", sid, "err", err)
		}
	}

	for sid, si := range want {
		seriesID, ok := idByExt[sid]
		if !ok {
			continue
		}
		var existingMembers []int64
		if err := db.WithContext(ctx).Raw(`SELECT work_id FROM catalog_series_member
			WHERE series_id = ?`, seriesID).Scan(&existingMembers).Error; err != nil {
			st.Errors++
			slog.Warn("load members", "ext", sid, "err", err)
			continue
		}
		have := make(map[int64]struct{}, len(existingMembers))
		for _, w := range existingMembers {
			have[w] = struct{}{}
		}
		for w := range si.members {
			if _, ok := have[w]; ok {
				continue
			}
			res := db.WithContext(ctx).Exec(`INSERT INTO catalog_series_member (series_id, work_id)
				VALUES (?, ?) ON CONFLICT (series_id, work_id) DO NOTHING`, seriesID, w)
			if res.Error != nil {
				st.Errors++
				slog.Warn("member insert", "ext", sid, "work", w, "err", res.Error)
				continue
			}
			if res.RowsAffected == 1 {
				st.MembersAdded++
				touched = append(touched, w)
			}
		}
		for _, w := range existingMembers {
			if _, ok := si.members[w]; ok {
				continue
			}
			if err := db.WithContext(ctx).Exec(`DELETE FROM catalog_series_member
				WHERE series_id = ? AND work_id = ?`, seriesID, w).Error; err != nil {
				st.Errors++
				slog.Warn("member stale delete", "ext", sid, "work", w, "err", err)
				continue
			}
			st.MembersStale++
			touched = append(touched, w)
		}
	}

	orderTouched, err := reconcileOrder(ctx, db, want, idByExt, st, true)
	if err != nil {
		return nil, err
	}
	touched = append(touched, orderTouched...)

	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}
	logDone(st, opts.Apply)
	return st, nil
}

func reconcileOrder(ctx context.Context, db *gorm.DB, want map[string]*seriesInfo,
	idByExt map[string]int64, st *Stats, apply bool) ([]int64, error) {
	var allWorks []int64
	for _, si := range want {
		for w := range si.members {
			allWorks = append(allWorks, w)
		}
	}
	facts, err := seriesorder.LoadFacts(ctx, db, allWorks)
	if err != nil {
		return nil, fmt.Errorf("load ordering facts: %w", err)
	}
	var touched []int64
	for sid, si := range want {
		seriesID, ok := idByExt[sid]
		if !ok {
			continue
		}
		members := make([]int64, 0, len(si.members))
		for w := range si.members {
			members = append(members, w)
		}
		have, err := seriesorder.LoadCurrent(ctx, db, seriesID)
		if err != nil {
			st.Errors++
			slog.Warn("order current", "ext", sid, "err", err)
			continue
		}
		changed, err := seriesorder.Apply(ctx, db, seriesID,
			facts.Assign(members, model.SeriesMemberKindUnknown), have, apply)
		if err != nil {
			st.Errors++
			slog.Warn("order apply", "ext", sid, "err", err)
			continue
		}
		st.OrderChanged += len(changed)
		touched = append(touched, changed...)
	}
	return touched, nil
}

func planMembers(ctx context.Context, db *gorm.DB, want map[string]*seriesInfo, existingByExt map[string]struct {
	id   int64
	name string
}, st *Stats) {
	for sid, si := range want {
		e, ok := existingByExt[sid]
		if !ok {
			st.MembersAdded += len(si.members)
			continue
		}
		var existingMembers []int64
		if err := db.WithContext(ctx).Raw(`SELECT work_id FROM catalog_series_member
			WHERE series_id = ?`, e.id).Scan(&existingMembers).Error; err != nil {
			st.Errors++
			continue
		}
		have := make(map[int64]struct{}, len(existingMembers))
		for _, w := range existingMembers {
			have[w] = struct{}{}
		}
		for w := range si.members {
			if _, ok := have[w]; !ok {
				st.MembersAdded++
			}
		}
		for _, w := range existingMembers {
			if _, ok := si.members[w]; !ok {
				st.MembersStale++
			}
		}
	}
}

func logDone(st *Stats, apply bool) {
	slog.Info("workseries done", "apply", apply,
		"anchored_works", st.AnchoredWorks, "series_eligible", st.SeriesEligible, "members_wanted", st.MembersWanted,
		"skipped_bundle", st.SkippedBundle,
		"series_created", st.SeriesCreated, "series_renamed", st.SeriesRenamed, "series_deleted", st.SeriesDeleted,
		"members_added", st.MembersAdded, "members_stale", st.MembersStale,
		"order_changed", st.OrderChanged, "errors", st.Errors)
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
