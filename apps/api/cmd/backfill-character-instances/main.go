package main

import (
	"flag"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const pairJoin = `
	FROM catalog_external_ref ri
	JOIN catalog_source s ON s.id = ri.source_id AND s.key = 'vndb'
	JOIN src_vndb.chars ch ON ch.id = ri.external_id AND ch.main <> ''
	JOIN catalog_external_ref rm ON rm.source_id = ri.source_id AND rm.entity_type = 4
		AND rm.link_kind = 0 AND rm.external_id = ch.main AND rm.entity_id <> ri.entity_id
	JOIN catalog_character c ON c.id = ri.entity_id AND c.deleted_at IS NULL
	JOIN catalog_character m ON m.id = rm.entity_id AND m.deleted_at IS NULL
	WHERE ri.entity_type = 4 AND ri.link_kind = 0`

// A merged character keeps every VNDB id it absorbed, and those ids can name
// different mains. Writing either one made each run flip the other back:
// character 184644 held c162639 and c172350 and alternated between 124657 and
// 92889 on 2026-09-16, so a re-run never reached zero writes. Such an instance
// is counted and left alone.
const agreedCTE = `WITH p AS (
	SELECT DISTINCT ri.entity_id AS inst_id, rm.entity_id AS main_id` + pairJoin + `
), agreed AS (
	SELECT inst_id, min(main_id) AS main_id FROM p GROUP BY inst_id HAVING count(*) = 1
) `

type counts struct {
	Pairs       int64 `gorm:"column:pairs"`
	Conflicting int64 `gorm:"column:conflicting"`
	WouldChange int64 `gorm:"column:would_change"`
	AlreadyOK   int64 `gorm:"column:already_ok"`
}

type stats struct {
	counts
	LinksTotal int64
	Written    int64
}

func run(db *gorm.DB, apply bool) (stats, error) {
	var st stats
	if err := db.Raw(`SELECT count(*) FROM src_vndb.chars WHERE main <> ''`).Scan(&st.LinksTotal).Error; err != nil {
		return st, err
	}
	if err := db.Raw(agreedCTE + `SELECT
		(SELECT count(DISTINCT inst_id) FROM p) AS pairs,
		(SELECT count(*) FROM (SELECT inst_id FROM p GROUP BY inst_id HAVING count(*) > 1) x) AS conflicting,
		count(*) FILTER (WHERE c.instance_of IS DISTINCT FROM a.main_id) AS would_change,
		count(*) FILTER (WHERE c.instance_of = a.main_id) AS already_ok
		FROM agreed a JOIN catalog_character c ON c.id = a.inst_id`).
		Scan(&st.counts).Error; err != nil {
		return st, err
	}
	if !apply {
		return st, nil
	}
	res := db.Exec(agreedCTE + `UPDATE catalog_character c SET instance_of = a.main_id
		FROM agreed a
		WHERE c.id = a.inst_id AND c.instance_of IS DISTINCT FROM a.main_id`)
	if res.Error != nil {
		return st, res.Error
	}
	st.Written = res.RowsAffected
	return st, nil
}

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (also hosts src_vndb) — REQUIRED")
	apply := flag.Bool("apply", false, "write (default: dry-run counters only)")
	flag.Parse()
	if *dsn == "" {
		slog.Error("--dsn is required; refusing to guess the target database")
		os.Exit(1)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}

	st, err := run(db, *apply)
	if err != nil {
		slog.Error("backfill-character-instances", "apply", *apply, "error", err)
		os.Exit(1)
	}
	slog.Info("backfill-character-instances done", "apply", *apply,
		"vndb_main_links", st.LinksTotal, "instances_resolved", st.Pairs,
		"conflicting_mains", st.Conflicting,
		"would_change", st.WouldChange, "already_ok", st.AlreadyOK, "written", st.Written)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
}
