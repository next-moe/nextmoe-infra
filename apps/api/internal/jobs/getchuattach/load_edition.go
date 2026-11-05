package getchuattach

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// A row this lane founded is a physical or digital one carrying only Getchu
// refs; it never writes kind 0. Refs alone are not enough: on 2026-11-05 the
// first fold pass joined 74 SKUs to eg-work-import's ref-less kind-0 rows, and
// those rows then read as Getchu-founded, so later SKUs could not join them.
func loadReleaseRows(ctx context.Context, db *gorm.DB, ids registryIDs, snap *snapshot) error {
	var rows []struct {
		ID        int64  `gorm:"column:id"`
		WorkID    int64  `gorm:"column:work_id"`
		Kind      int16  `gorm:"column:kind"`
		Day       string `gorm:"column:day"`
		Title     string `gorm:"column:title"`
		Platforms string `gorm:"column:platforms"`
		Founded   bool   `gorm:"column:founded"`
	}
	if err := db.WithContext(ctx).Raw(`
		WITH vp AS (
			SELECT v.entity_id AS release_id, string_agg(p.platform, ',') AS plats
			FROM catalog_external_ref v
			JOIN src_vndb.releases_platforms p ON p.id = v.external_id
			WHERE v.entity_type = ? AND v.source_id = ? AND v.dead_at IS NULL
			GROUP BY v.entity_id
		), rs AS (
			SELECT entity_id AS release_id,
			       bool_or(source_id = ?) AS has_getchu, bool_or(source_id <> ?) AS has_other
			FROM catalog_external_ref
			WHERE entity_type = ?
			GROUP BY entity_id
		)
		SELECT rel.id, rel.work_id, rel.kind,
		       CASE WHEN rel.released_y IS NOT NULL AND rel.released_m IS NOT NULL AND rel.released_d IS NOT NULL
		            THEN lpad(rel.released_y::text, 4, '0') || '-' || lpad(rel.released_m::text, 2, '0') || '-' || lpad(rel.released_d::text, 2, '0')
		            ELSE '' END AS day,
		       coalesce(rel.title, '') AS title,
		       concat_ws(',', nullif(rel.platform, ''),
		                 (SELECT string_agg(x, ',') FROM jsonb_array_elements_text(
		                     CASE WHEN jsonb_typeof(rel.extra->'platforms') = 'array' THEN rel.extra->'platforms' ELSE '[]'::jsonb END) x),
		                 vp.plats) AS platforms,
		       rel.kind <> ? AND coalesce(rs.has_getchu AND NOT rs.has_other, false) AS founded
		FROM catalog_release rel
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		LEFT JOIN vp ON vp.release_id = rel.id
		LEFT JOIN rs ON rs.release_id = rel.id
		WHERE rel.deleted_at IS NULL AND rel.kind IN (?, ?, ?)`,
		model.EntityTypeRelease, ids.vndb,
		ids.getchu, ids.getchu, model.EntityTypeRelease,
		model.ReleaseKindDefault,
		model.ReleaseKindDefault, model.ReleaseKindDigital, model.ReleaseKindPhysical,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load release rows: %w", err)
	}
	for _, r := range rows {
		snap.workReleases[r.WorkID] = append(snap.workReleases[r.WorkID], relRow{
			ID: r.ID, Kind: r.Kind, Day: r.Day, Title: r.Title,
			Platforms: splitPlatforms(r.Platforms), Founded: r.Founded,
		})
	}
	return nil
}

func splitPlatforms(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func loadVNDBGetchuLinks(ctx context.Context, db *gorm.DB, vndb int16, snap *snapshot) error {
	var rows []struct {
		GetchuID  string `gorm:"column:getchu_id"`
		ReleaseID int64  `gorm:"column:release_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT e.value AS getchu_id, vr.entity_id AS release_id
		FROM src_vndb.extlinks e
		JOIN src_vndb.releases_extlinks rx ON rx.link = e.id
		JOIN catalog_external_ref vr ON vr.entity_type = ? AND vr.source_id = ? AND vr.external_id = rx.id
			AND vr.link_kind = ? AND vr.dead_at IS NULL
		WHERE e.site = 'getchu'`,
		model.EntityTypeRelease, vndb, model.LinkKindExact,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load vndb getchu extlinks: %w", err)
	}
	for _, r := range rows {
		snap.vndbLinked[r.GetchuID] = append(snap.vndbLinked[r.GetchuID], r.ReleaseID)
	}
	return nil
}

// loadParallel names the rows a Getchu attach founded beside an edition the
// catalog already held. A row is folded only while nothing but its Getchu refs
// hangs on it; the NOT EXISTS list is every table that points at a release —
// the three foreign keys TestParallelCheckCoversEveryReleaseReference pins,
// plus the polymorphic external refs and a split request still waiting to move
// the row.
func loadParallel(ctx context.Context, db *gorm.DB, ids registryIDs, snap *snapshot) error {
	var rows []struct {
		GetchuID  string `gorm:"column:getchu_id"`
		ReleaseID int64  `gorm:"column:release_id"`
		WorkID    int64  `gorm:"column:work_id"`
	}
	bare, bareArgs := parallelRowSQL("rel", ids.getchu)
	args := append([]any{model.EntityTypeRelease, ids.getchu, model.LinkKindExact}, bareArgs...)
	if err := db.WithContext(ctx).Raw(`
		SELECT e.external_id AS getchu_id, rel.id AS release_id, rel.work_id
		FROM catalog_external_ref e
		JOIN catalog_release rel ON rel.id = e.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		WHERE e.entity_type = ? AND e.source_id = ? AND e.link_kind = ? AND e.dead_at IS NULL
		  AND `+bare, args...,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load parallel getchu rows: %w", err)
	}
	for _, r := range rows {
		snap.parallel[r.GetchuID] = parallelRow{ReleaseID: r.ReleaseID, WorkID: r.WorkID}
	}
	return nil
}

func parallelRowSQL(alias string, getchu int16) (string, []any) {
	return fmt.Sprintf(`%[1]s.kind <> ? AND %[1]s.extra = '{}'::jsonb AND %[1]s.field_provenance = '{}'::jsonb
		  AND NOT EXISTS (SELECT 1 FROM catalog_external_ref o
		                  WHERE o.entity_type = ? AND o.entity_id = %[1]s.id AND o.source_id <> ?)
		  AND NOT EXISTS (SELECT 1 FROM catalog_credit c WHERE c.release_id = %[1]s.id)
		  AND NOT EXISTS (SELECT 1 FROM catalog_release_label l WHERE l.release_id = %[1]s.id)
		  AND NOT EXISTS (SELECT 1 FROM catalog_walkthrough wt WHERE wt.release_id = %[1]s.id)
		  AND NOT EXISTS (SELECT 1 FROM catalog_split_request s
		                  WHERE s.executed_at IS NULL AND s.release_ids @> jsonb_build_array(%[1]s.id))`, alias),
		[]any{model.ReleaseKindDefault, model.EntityTypeRelease, getchu}
}
