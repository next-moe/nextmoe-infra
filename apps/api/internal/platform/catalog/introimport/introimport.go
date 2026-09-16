package introimport

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const introLang = "en"

var (
	reSpoilerSpan = regexp.MustCompile(`(?is)\[spoiler\].*?\[/spoiler\]`)
	reSpoilerOpen = regexp.MustCompile(`(?is)\[spoiler\].*\z`)
	reURLOpen     = regexp.MustCompile(`(?i)\[url=[^\]]*\]`)
	reSimpleTag   = regexp.MustCompile(`(?i)\[/?(?:url|b|i|u|s|quote|raw|code)\]`)
	reBlankRuns   = regexp.MustCompile(`\n{3,}`)
)

// VNDB descriptions are BBCode, and the v2 intro face serves text verbatim.
// The 2026-09-16 backlog held 480 intros, 307 of them carrying [url=…] tags,
// which the character and label lanes already strip; this is the same rule
// as entityintros.stripVNDBMarkup, spoilers removed rather than unwrapped.
func stripVNDBMarkup(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reSpoilerSpan.ReplaceAllString(s, "")
	s = reSpoilerOpen.ReplaceAllString(s, "")
	s = reURLOpen.ReplaceAllString(s, "")
	s = reSimpleTag.ReplaceAllString(s, "")
	s = reBlankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

type Options struct {
	DryRun bool
	Limit  int
}

type Stats struct {
	TotalBodyless    int64
	WithVNDBAnchor   int64
	SkippedNoAnchor  int64
	SkippedEmptyDesc int64
	Already          int64
	IntrosWritten    int64
	WorksCovered     int64
}

type candidate struct {
	WorkID      int64  `gorm:"column:work_id"`
	VNDBID      string `gorm:"column:vndb_id"`
	Description string `gorm:"column:description"`
}

func Run(ctx context.Context, db *gorm.DB, opts Options) (Stats, error) {
	db = db.WithContext(ctx)
	var st Stats

	var mediumID int16
	if err := db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&mediumID).Error; err != nil {
		return st, fmt.Errorf("resolve galgame medium: %w", err)
	}
	var vndbSourceID int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndbSourceID).Error; err != nil {
		return st, fmt.Errorf("resolve vndb source: %w", err)
	}
	if mediumID == 0 || vndbSourceID == 0 {
		return st, fmt.Errorf("registry not seeded (galgame medium=%d, vndb source=%d)", mediumID, vndbSourceID)
	}

	if err := db.Raw(`SELECT count(*) FROM catalog_work
		WHERE medium_id = ? AND (site IS NULL OR site = '') AND deleted_at IS NULL`,
		mediumID).Scan(&st.TotalBodyless).Error; err != nil {
		return st, fmt.Errorf("count bodyless: %w", err)
	}

	q := `SELECT DISTINCT ON (w.id) w.id AS work_id, r.external_id AS vndb_id, COALESCE(v.description, '') AS description
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id AND r.link_kind = ?
		JOIN catalog_source s ON s.id = r.source_id AND s.id = ?
		LEFT JOIN src_vndb.vn v ON v.id = r.external_id
		WHERE w.medium_id = ? AND (w.site IS NULL OR w.site = '') AND w.deleted_at IS NULL
		ORDER BY w.id, r.external_id`
	var cands []candidate
	if err := db.Raw(q, model.EntityTypeWork, model.LinkKindExact, vndbSourceID, mediumID).Scan(&cands).Error; err != nil {
		return st, fmt.Errorf("gather candidates: %w", err)
	}
	if opts.Limit > 0 && len(cands) > opts.Limit {
		cands = cands[:opts.Limit]
	}
	st.WithVNDBAnchor = int64(len(cands))
	st.SkippedNoAnchor = st.TotalBodyless - st.WithVNDBAnchor

	workIDs := make([]int64, 0, len(cands))
	for _, c := range cands {
		workIDs = append(workIDs, c.WorkID)
	}
	already := map[int64]bool{}
	if len(workIDs) > 0 {
		var existing []int64
		if err := db.Raw(`SELECT work_id FROM catalog_work_intro
			WHERE lang = ? AND work_id IN ?`, introLang, workIDs).Scan(&existing).Error; err != nil {
			return st, fmt.Errorf("load existing: %w", err)
		}
		for _, id := range existing {
			already[id] = true
		}
	}

	var toWrite []model.CatalogWorkIntro
	for _, c := range cands {
		text := stripVNDBMarkup(c.Description)
		if text == "" {
			st.SkippedEmptyDesc++
			continue
		}
		if already[c.WorkID] {
			st.Already++
			continue
		}
		toWrite = append(toWrite, model.CatalogWorkIntro{
			WorkID: c.WorkID, Lang: introLang, Intro: text, SourceID: vndbSourceID,
		})
	}

	st.IntrosWritten = int64(len(toWrite))
	st.WorksCovered = st.IntrosWritten
	if opts.DryRun || len(toWrite) == 0 {
		return st, nil
	}

	res := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "lang"}, {Name: "source_id"}},
		DoNothing: true,
	}).CreateInBatches(toWrite, 500)
	if res.Error != nil {
		return st, fmt.Errorf("insert intros: %w", res.Error)
	}
	st.IntrosWritten = res.RowsAffected
	st.WorksCovered = res.RowsAffected
	hosts := make([]int64, 0, len(toWrite))
	for _, row := range toWrite {
		hosts = append(hosts, row.WorkID)
	}
	if err := repository.TouchWorks(ctx, db, hosts); err != nil {
		return st, fmt.Errorf("touch works: %w", err)
	}
	return st, nil
}
