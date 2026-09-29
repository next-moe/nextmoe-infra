package main

import (
	"context"
	"time"

	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

type pairRow struct {
	A              int64      `gorm:"column:a"`
	B              int64      `gorm:"column:b"`
	SharedNorm     string     `gorm:"column:shared_norm"`
	SharedNorms    int        `gorm:"column:shared_norms"`
	SharedOfficial int        `gorm:"column:shared_official"`
	LaneA          string     `gorm:"column:lane_a"`
	LaneB          string     `gorm:"column:lane_b"`
	AnchorsA       int        `gorm:"column:anchors_a"`
	AnchorsB       int        `gorm:"column:anchors_b"`
	SiteA          *string    `gorm:"column:site_a"`
	SiteB          *string    `gorm:"column:site_b"`
	NameA          string     `gorm:"column:name_a"`
	NameB          string     `gorm:"column:name_b"`
	AnchorConflict bool       `gorm:"column:anchor_conflict"`
	RelConflict    bool       `gorm:"column:release_conflict"`
	RefOverlap     bool       `gorm:"column:ref_overlap"`
	RefOverlapCI   bool       `gorm:"column:ref_overlap_ci"`
	DateA          *time.Time `gorm:"column:date_a"`
	DateB          *time.Time `gorm:"column:date_b"`
	LabelOverlap   bool       `gorm:"column:label_overlap"`
}

// pairQuerySQL is the standing duplicate detector: every pair of live works
// sharing a folded title/display_name norm or a case-insensitively equal
// work-level external ref (identity kinds pair outright; related-page refs
// pair only when no exact anchors prove the works distinct), scored with the
// corroborating facts the verdict needs. Work-level identity anchors drive
// lanes and conflicts; dlsite identity lives on releases (entity_type 6), so
// the dlsite lane and the release-level conflict/overlap read through
// catalog_release — a work-level-only read files the whole dlsite family
// under 'other' and misses every edition split.
//
// Every per-pair fact reads the pair's two wk rows, aggregated once per work.
// It used to be correlated EXISTS against materialized CTEs, which have no
// index to probe; once the 2026-09-18 drain added 39k works, the nightly
// census hit temp_file_limit (20GB) every night.
func pairQuerySQL() string {
	return `
WITH lw AS (
  SELECT id, site, display_name, medium_id FROM catalog_work WHERE deleted_at IS NULL
),
norms AS (
  SELECT work_id, n, bool_or(official) AS official FROM (` + service.WorkDupeCorpusSQL() + `) c GROUP BY work_id, n
),
pairs AS (
  SELECT a.work_id AS a, b.work_id AS b, min(a.n) AS shared_norm, count(*) AS shared_norms,
    count(*) FILTER (WHERE a.official AND b.official) AS shared_official
  FROM norms a JOIN norms b ON a.n = b.n AND a.work_id < b.work_id
  WHERE ` + service.WorkDupeNormEligibleSQL("a.n") + `
  GROUP BY a.work_id, b.work_id
),
wref AS (
  SELECT entity_id AS work_id,
    count(*) FILTER (WHERE link_kind = 0) AS anchors,
    bool_or(link_kind = 0 AND source_id = 2) AS vndb,
    bool_or(link_kind = 0 AND source_id = 3) AS bgm,
    bool_or(link_kind = 0 AND source_id = 5) AS eg,
    array_agg(source_id || ':' || external_id) FILTER (WHERE link_kind = 0) AS anchor_keys,
    array_agg(source_id || ':' || external_id) FILTER (WHERE link_kind IN (0, 1)) AS ref_keys,
    array_agg(source_id || ':' || lower(external_id)) AS ref_keys_ci
  FROM catalog_external_ref
  WHERE entity_type = 5 AND dead_at IS NULL
  GROUP BY entity_id
),
rref AS (
  SELECT rel.work_id,
    bool_or(r.source_id = 4) AS dlsite,
    array_agg(r.source_id || ':' || r.external_id) AS keys,
    array_agg(r.source_id || ':' || lower(r.external_id)) AS keys_ci
  FROM catalog_external_ref r
  JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
  WHERE r.entity_type = 6 AND r.link_kind = 0 AND r.dead_at IS NULL
  GROUP BY rel.work_id
),
wlabel AS (
  SELECT work_id, array_agg(label_id) AS label_ids FROM catalog_work_label GROUP BY work_id
),
wdate AS (
  SELECT work_id, min(make_date(released_y, coalesce(nullif(released_m,0),1), coalesce(nullif(released_d,0),1))) AS d
  FROM catalog_release
  WHERE deleted_at IS NULL AND released_y IS NOT NULL
  GROUP BY work_id
),
bgmdate AS (
  SELECT r.entity_id AS work_id, min(s.date::date) AS d
  FROM catalog_external_ref r
  JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
  WHERE r.entity_type = 5 AND r.link_kind = 0 AND r.dead_at IS NULL AND r.source_id = 3
    AND s.date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
  GROUP BY r.entity_id
),
wk AS (
  SELECT lw.id, lw.site, lw.display_name, lw.medium_id,
    CASE WHEN lw.site IS NOT NULL AND lw.site <> '' THEN 'kungal'
         WHEN w.vndb THEN 'vndb'
         WHEN w.bgm THEN 'bgm'
         WHEN w.eg THEN 'eg'
         WHEN r.dlsite THEN 'dlsite'
         ELSE 'other' END AS lane,
    coalesce(w.anchors, 0) AS anchors,
    coalesce(w.anchor_keys, '{}') AS anchor_keys,
    coalesce(w.ref_keys, '{}') AS ref_keys,
    coalesce(w.ref_keys_ci, '{}') AS ref_keys_ci,
    coalesce(r.keys, '{}') AS rel_keys,
    coalesce(r.keys_ci, '{}') AS rel_keys_ci,
    coalesce(l.label_ids, '{}') AS label_ids,
    coalesce(d.d, b.d) AS released
  FROM lw
  LEFT JOIN wref w ON w.work_id = lw.id
  LEFT JOIN rref r ON r.work_id = lw.id
  LEFT JOIN wlabel l ON l.work_id = lw.id
  LEFT JOIN wdate d ON d.work_id = lw.id
  LEFT JOIN bgmdate b ON b.work_id = lw.id
),
refpairs AS (
  SELECT x.entity_id AS a, y.entity_id AS b
  FROM catalog_external_ref x
  JOIN catalog_external_ref y
    ON y.source_id = x.source_id
   AND lower(y.external_id) = lower(x.external_id)
   AND x.entity_id < y.entity_id
  JOIN lw ON lw.id = x.entity_id
  JOIN lw lw_b ON lw_b.id = y.entity_id
  WHERE x.entity_type = 5 AND y.entity_type = 5
    AND x.dead_at IS NULL AND y.dead_at IS NULL
    AND x.link_kind IN (0, 1) AND y.link_kind IN (0, 1)
  GROUP BY x.entity_id, y.entity_id
  UNION
  -- any-kind pairing generated ~78k brand-hub pairs of which 76,632 had conflicting exact
  -- anchors; the no-conflict residue sampled 15/15 as true duplicates.
  SELECT x.entity_id AS a, y.entity_id AS b
  FROM catalog_external_ref x
  JOIN catalog_external_ref y
    ON y.source_id = x.source_id
   AND lower(y.external_id) = lower(x.external_id)
   AND x.entity_id < y.entity_id
  JOIN wk ka ON ka.id = x.entity_id
  JOIN wk kb ON kb.id = y.entity_id
  WHERE x.entity_type = 5 AND y.entity_type = 5
    AND x.dead_at IS NULL AND y.dead_at IS NULL
    AND x.link_kind = 2 AND y.link_kind = 2
    AND NOT ` + sameSourceOtherIDSQL("ka.anchor_keys", "kb.anchor_keys") + `
  GROUP BY x.entity_id, y.entity_id
),
universe AS (
  SELECT coalesce(p.a, r.a) AS a, coalesce(p.b, r.b) AS b,
    coalesce(p.shared_norm, '') AS shared_norm,
    coalesce(p.shared_norms, 0) AS shared_norms,
    coalesce(p.shared_official, 0) AS shared_official
  FROM pairs p
  FULL OUTER JOIN refpairs r ON r.a = p.a AND r.b = p.b
)
SELECT p.a, p.b, p.shared_norm, p.shared_norms, p.shared_official,
  wa.lane AS lane_a, wb.lane AS lane_b,
  wa.anchors AS anchors_a, wb.anchors AS anchors_b,
  wa.site AS site_a, wb.site AS site_b,
  wa.display_name AS name_a, wb.display_name AS name_b,
  ` + sameSourceOtherIDSQL("wa.anchor_keys", "wb.anchor_keys") + ` AS anchor_conflict,
  (` + sameSourceOtherIDSQL("wa.rel_keys", "wb.rel_keys") + `
   AND NOT (wa.rel_keys && wb.rel_keys)) AS release_conflict,
  (wa.ref_keys && wb.ref_keys OR wa.rel_keys && wb.rel_keys) AS ref_overlap,
  (wa.ref_keys_ci && wb.ref_keys_ci OR wa.rel_keys_ci && wb.rel_keys_ci) AS ref_overlap_ci,
  wa.released AS date_a,
  wb.released AS date_b,
  wa.label_ids && wb.label_ids AS label_overlap
FROM universe p
JOIN wk wa ON wa.id = p.a
JOIN wk wb ON wb.id = p.b AND wb.medium_id = wa.medium_id
ORDER BY p.a, p.b`
}

func sameSourceOtherIDSQL(xs, ys string) string {
	return `EXISTS (SELECT 1 FROM unnest(` + xs + `) u(k) JOIN unnest(` + ys + `) v(k)
    ON split_part(v.k, ':', 1) = split_part(u.k, ':', 1) AND v.k <> u.k)`
}

type census struct {
	rows     []pairRow
	verdicts []bucket
	groups   []mergeGroup
}

func buildCensus(ctx context.Context, db *gorm.DB) (*census, error) {
	var rows []pairRow
	if err := db.WithContext(ctx).Raw(pairQuerySQL()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	verdicts, groups := classify(rows)
	return &census{rows: rows, verdicts: verdicts, groups: groups}, nil
}

func (c *census) verdictByPair() map[[2]int64]bucket {
	out := make(map[[2]int64]bucket, len(c.rows))
	for i := range c.rows {
		out[[2]int64{c.rows[i].A, c.rows[i].B}] = c.verdicts[i]
	}
	return out
}

func (c *census) rowByPair() map[[2]int64]pairRow {
	out := make(map[[2]int64]pairRow, len(c.rows))
	for _, r := range c.rows {
		out[[2]int64{r.A, r.B}] = r
	}
	return out
}
