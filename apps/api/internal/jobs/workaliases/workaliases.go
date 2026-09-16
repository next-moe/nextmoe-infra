package workaliases

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const maxAliasLen = 500

type Opts struct {
	Apply     bool
	DSN       string
	DlsiteDSN string
	Source    string
}

type Stats struct {
	BgmWorks      int
	BgmPlanned    int
	BgmSkippedDup int
	BgmWritten    int
	BgmConflict   int

	KanaWorks   int
	KanaBundle  int
	KanaNoKana  int
	KanaPlanned int
	KanaWritten int

	Errors int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.Source == "" {
		opts.Source = "all"
	}
	if (opts.Source == "dlsite-kana" || opts.Source == "all") && opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("dlsite mirror DSN is required for the dlsite-kana lane (--dlsite-dsn)")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)

	st := &Stats{}
	if opts.Source == "bgm" || opts.Source == "all" {
		if err := runBgm(ctx, db, opts, st); err != nil {
			return nil, err
		}
	}
	if opts.Source == "dlsite-kana" || opts.Source == "all" {
		if err := runKana(ctx, db, opts, st); err != nil {
			return nil, err
		}
	}
	slog.Info("workaliases done", "apply", opts.Apply,
		"bgm_works", st.BgmWorks, "bgm_planned", st.BgmPlanned, "bgm_skipped_dup", st.BgmSkippedDup,
		"bgm_written", st.BgmWritten, "bgm_conflict", st.BgmConflict,
		"kana_works", st.KanaWorks, "kana_no_kana", st.KanaNoKana,
		"kana_planned", st.KanaPlanned, "kana_written", st.KanaWritten, "errors", st.Errors)
	return st, nil
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

func aliasesFrom(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var ib infobox
	if err := json.Unmarshal(raw, &ib); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || len(s) > maxAliasLen {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, f := range ib.Fields {
		if f.Key != "别名" {
			continue
		}
		if f.Array {
			for _, it := range f.Items {
				add(it.Value)
			}
			continue
		}
		add(f.Value)
	}
	return out
}

func runBgm(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		Infobox []byte `gorm:"column:infobox_parsed"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (w.id) w.id AS work_id, s.infobox_parsed
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = 5 AND r.entity_id = w.id
			AND r.source_id = 3 AND r.link_kind = 0
		JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
		WHERE coalesce(w.site,'') = '' AND w.deleted_at IS NULL
		  AND s.infobox_parsed IS NOT NULL
		ORDER BY w.id, r.external_id`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load bgm candidates: %w", err)
	}

	type cand struct {
		workID int64
		alias  string
	}
	var cands []cand
	workIDs := map[int64]struct{}{}
	for _, r := range rows {
		aliases := aliasesFrom(r.Infobox)
		if len(aliases) == 0 {
			continue
		}
		st.BgmWorks++
		workIDs[r.WorkID] = struct{}{}
		for _, a := range aliases {
			cands = append(cands, cand{r.WorkID, a})
		}
	}

	existing := map[string]struct{}{}
	ids := make([]int64, 0, len(workIDs))
	for id := range workIDs {
		ids = append(ids, id)
	}
	for _, chunk := range chunkInt64(ids, 10000) {
		var t []struct {
			WorkID int64  `gorm:"column:work_id"`
			Title  string `gorm:"column:title"`
		}
		if err := db.WithContext(ctx).Raw(`SELECT work_id, title FROM catalog_work_title
			WHERE work_id IN ?`, chunk).Scan(&t).Error; err != nil {
			return fmt.Errorf("load existing titles: %w", err)
		}
		for _, r := range t {
			existing[titleKey(r.WorkID, r.Title)] = struct{}{}
		}
	}

	var touched []int64
	for _, c := range cands {
		if _, dup := existing[titleKey(c.workID, c.alias)]; dup {
			st.BgmSkippedDup++
			continue
		}
		st.BgmPlanned++
		if !opts.Apply {
			continue
		}
		res := db.WithContext(ctx).Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
			VALUES (?, '', ?, 1, 0) ON CONFLICT DO NOTHING`, c.workID, c.alias)
		if res.Error != nil {
			st.Errors++
			slog.Warn("alias insert", "work", c.workID, "err", res.Error)
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

func runKana(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Workno string `gorm:"column:workno"`
		Bundle bool   `gorm:"column:bundle"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (rel.work_id) rel.work_id, r.external_id AS workno,
			` + repository.BundleReleaseSQL("rel") + ` AS bundle
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id
		JOIN catalog_work w ON w.id = rel.work_id AND coalesce(w.site,'') = '' AND w.deleted_at IS NULL
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE r.entity_type = 6 AND r.source_id = 4 AND r.link_kind = 0
		  AND NOT EXISTS (SELECT 1 FROM catalog_work_title t WHERE t.work_id = rel.work_id AND t.kind = 3)
		ORDER BY rel.work_id, bundle, r.external_id`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load kana candidates: %w", err)
	}
	st.KanaWorks = len(rows)
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
		if !r.Bundle {
			worknos = append(worknos, r.Workno)
		}
	}
	kana := map[string]string{}
	for _, chunk := range chunkStr(worknos, 10000) {
		var t []struct {
			Workno string `gorm:"column:workno"`
			Kana   string `gorm:"column:kana"`
		}
		if err := dl.WithContext(ctx).Raw(`SELECT workno,
			coalesce(product_json->>'work_name_kana', info_json->>'work_name_kana', '') AS kana
			FROM works WHERE workno IN ?`, chunk).Scan(&t).Error; err != nil {
			return fmt.Errorf("load mirror kana: %w", err)
		}
		for _, r := range t {
			if k := strings.TrimSpace(r.Kana); k != "" && len(k) <= maxAliasLen {
				kana[r.Workno] = k
			}
		}
	}

	var touched []int64
	for _, r := range rows {
		if r.Bundle {
			st.KanaBundle++
			continue
		}
		k, ok := kana[r.Workno]
		if !ok {
			st.KanaNoKana++
			continue
		}
		st.KanaPlanned++
		if !opts.Apply {
			continue
		}
		res := db.WithContext(ctx).Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
			VALUES (?, 'ja', ?, 3, 0) ON CONFLICT DO NOTHING`, r.WorkID, k)
		if res.Error != nil {
			st.Errors++
			slog.Warn("kana insert", "work", r.WorkID, "err", res.Error)
			continue
		}
		if res.RowsAffected == 1 {
			st.KanaWritten++
			touched = append(touched, r.WorkID)
		}
	}
	return repository.TouchWorks(ctx, db, touched)
}

func titleKey(workID int64, title string) string {
	return fmt.Sprintf("%d\x00%s", workID, title)
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
