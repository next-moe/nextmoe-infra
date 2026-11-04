package vndbshots

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"api/internal/jobs/vndbcovers"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type registry struct {
	vndbSource    int16
	galgameMedium int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&r.vndbSource).Error; err != nil {
		return r, fmt.Errorf("resolve vndb source: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.vndbSource == 0 || r.galgameMedium == 0 {
		return r, fmt.Errorf("registry not seeded (vndb source=%d, galgame medium=%d)", r.vndbSource, r.galgameMedium)
	}
	return r, nil
}

type candidate struct {
	WorkID int64  `gorm:"column:work_id"`
	VNDBID string `gorm:"column:vndb_id"`
}

// Newest first: the gap people notice is on works added since the last import.
func loadCandidates(ctx context.Context, db *gorm.DB, reg registry, ids []int64) ([]candidate, error) {
	sql := `
		SELECT DISTINCT ON (w.id) w.id AS work_id, r.external_id AS vndb_id
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE w.medium_id = ? AND w.deleted_at IS NULL AND w.status = ?
			AND NOT EXISTS (SELECT 1 FROM catalog_work_screenshot s WHERE s.work_id = w.id)`
	args := []any{model.EntityTypeWork, reg.vndbSource, model.LinkKindExact, reg.galgameMedium, model.WorkStatusLive}
	if len(ids) > 0 {
		sql += "\n\t\t\tAND w.id IN ?"
		args = append(args, ids)
	}
	sql += "\n\t\tORDER BY w.id DESC, r.external_id"

	var out []candidate
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type shot struct {
	Scr      string
	Rel      string
	Sexual   int16
	Violence int16
}

type workPlan struct {
	WorkID int64
	VNDBID string
	Shots  []shot
}

// An unrated screenshot is left out rather than guessed at: its grade decides
// which shelf sees it.
func buildPlan(ctx context.Context, db *gorm.DB, cands []candidate, perWork, limit int, stats *Stats) ([]workPlan, error) {
	byVN := make(map[string][]shot, len(cands))
	vids := make([]string, 0, len(cands))
	for _, c := range cands {
		vids = append(vids, c.VNDBID)
	}
	for chunk := range slices.Chunk(vids, 5000) {
		var rows []struct {
			VID      string `gorm:"column:vid"`
			Scr      string `gorm:"column:scr"`
			Votes    int    `gorm:"column:votes"`
			Sexual   int    `gorm:"column:sexual"`
			Violence int    `gorm:"column:violence"`
		}
		if err := db.WithContext(ctx).Raw(`
			SELECT s.id AS vid, s.scr, coalesce(i.c_votecount, 0) AS votes,
			       coalesce(i.c_sexual_avg, 0) AS sexual, coalesce(i.c_violence_avg, 0) AS violence
			FROM src_vndb.vn_screenshots s
			LEFT JOIN src_vndb.images i ON i.id = s.scr
			WHERE s.id IN ?
			ORDER BY s.id, length(s.scr), s.scr`, chunk).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("read vndb dump screenshots: %w", err)
		}
		for _, r := range rows {
			rel, ok := vndbcovers.ImageRel(r.Scr)
			if !ok {
				continue
			}
			if r.Votes == 0 {
				stats.Unrated++
				continue
			}
			if len(byVN[r.VID]) >= perWork {
				continue
			}
			byVN[r.VID] = append(byVN[r.VID], shot{
				Scr: r.Scr, Rel: rel,
				Sexual:   vndbcovers.RatingLevel(float64(r.Sexual) / 100),
				Violence: vndbcovers.RatingLevel(float64(r.Violence) / 100),
			})
		}
	}

	var plan []workPlan
	for _, c := range cands {
		shots := byVN[c.VNDBID]
		if len(shots) == 0 {
			stats.NoShots++
			continue
		}
		if limit > 0 && len(plan) >= limit {
			break
		}
		plan = append(plan, workPlan{WorkID: c.WorkID, VNDBID: c.VNDBID, Shots: shots})
		stats.Planned += len(shots)
	}
	stats.Works = len(plan)
	return plan, nil
}

func mirrored(imageDir, rel string) bool {
	st, err := os.Stat(filepath.Join(imageDir, filepath.FromSlash(rel)))
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func writeFilesOut(path, imageDir string, plan []workPlan) (int, error) {
	var missing []string
	for _, w := range plan {
		for _, s := range w.Shots {
			if !mirrored(imageDir, s.Rel) {
				missing = append(missing, s.Rel)
			}
		}
	}
	slices.Sort(missing)
	missing = slices.Compact(missing)
	var b strings.Builder
	for _, rel := range missing {
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return 0, err
	}
	return len(missing), nil
}
