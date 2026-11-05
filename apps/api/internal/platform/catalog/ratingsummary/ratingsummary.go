package ratingsummary

import (
	"context"
	"encoding/json"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/markdown"

	"gorm.io/gorm"
)

var Columns = []string{"short_summary_html", "short_summary_text", "short_summary_has_spoiler", "short_summary_cooker"}

func Apply(r *model.CatalogUserWorkRating) {
	c := markdown.CookShort(r.ShortSummary)
	r.ShortSummaryHTML, r.ShortSummaryText, r.ShortSummaryHasSpoiler = c.HTML, c.Text, c.HasSpoiler
	r.ShortSummaryCooker = int16(c.Version)
}

type Scope struct {
	WorkIDs  []int64
	EveryRow bool
	DryRun   bool
}

type Stats struct {
	Scanned int
	Cooked  int
}

const batchSize = 500

type cookedRow struct {
	ID         int64  `json:"id"`
	Source     string `json:"src"`
	HTML       string `json:"html"`
	Text       string `json:"text"`
	HasSpoiler bool   `json:"spoiler"`
	Cooker     int16  `json:"cooker"`
}

func Cook(ctx context.Context, db *gorm.DB, sc Scope) (Stats, error) {
	var st Stats
	var after int64
	for {
		q := db.WithContext(ctx).Model(&model.CatalogUserWorkRating{}).
			Select("id", "short_summary", "short_summary_html", "short_summary_text", "short_summary_has_spoiler", "short_summary_cooker").
			Where("id > ?", after).Order("id").Limit(batchSize)
		if !sc.EveryRow {
			q = q.Where("short_summary_cooker <> ?", markdown.ShortVersion)
		}
		if len(sc.WorkIDs) > 0 {
			q = q.Where("work_id IN ?", sc.WorkIDs)
		}
		var rows []model.CatalogUserWorkRating
		if err := q.Find(&rows).Error; err != nil {
			return st, err
		}
		if len(rows) == 0 {
			return st, nil
		}
		after = rows[len(rows)-1].ID
		st.Scanned += len(rows)
		var changed []cookedRow
		for _, r := range rows {
			was := r
			Apply(&r)
			if r.ShortSummaryHTML == was.ShortSummaryHTML && r.ShortSummaryText == was.ShortSummaryText &&
				r.ShortSummaryHasSpoiler == was.ShortSummaryHasSpoiler && r.ShortSummaryCooker == was.ShortSummaryCooker {
				continue
			}
			changed = append(changed, cookedRow{ID: r.ID, Source: r.ShortSummary, HTML: r.ShortSummaryHTML,
				Text: r.ShortSummaryText, HasSpoiler: r.ShortSummaryHasSpoiler, Cooker: r.ShortSummaryCooker})
		}
		st.Cooked += len(changed)
		if len(changed) == 0 || sc.DryRun {
			continue
		}
		payload, err := json.Marshal(changed)
		if err != nil {
			return st, err
		}
		if err := db.WithContext(ctx).Exec(`UPDATE catalog_user_work_rating t
			SET short_summary_html = c.html, short_summary_text = c.text,
			    short_summary_has_spoiler = c.spoiler, short_summary_cooker = c.cooker
			FROM jsonb_to_recordset(?::jsonb) AS c(id bigint, src text, html text, text text, spoiler boolean, cooker smallint)
			WHERE t.id = c.id AND t.short_summary = c.src`, string(payload)).Error; err != nil {
			return st, err
		}
	}
}

type BumpStats struct {
	Bumped  int
	Batches int
}

// Bump stamps changed_at on every rating with a summary, so each site mirror
// pulls it again with its cooked fields. Run it only once every mirror can
// read them: a mirror that cannot consumes the change and never sees it again.
// Each batch commits on its own with clock_timestamp(), well inside the
// change feed's five-second settle.
func Bump(ctx context.Context, db *gorm.DB, batch int, pause time.Duration, dryRun bool) (BumpStats, error) {
	var st BumpStats
	var after int64
	for {
		var ids []int64
		if err := db.WithContext(ctx).Model(&model.CatalogUserWorkRating{}).
			Where("id > ? AND short_summary <> ''", after).Order("id").Limit(batch).Pluck("id", &ids).Error; err != nil {
			return st, err
		}
		if len(ids) == 0 {
			return st, nil
		}
		after = ids[len(ids)-1]
		st.Batches++
		st.Bumped += len(ids)
		if dryRun {
			continue
		}
		if err := db.WithContext(ctx).Exec(`UPDATE catalog_user_work_rating SET changed_at = clock_timestamp() WHERE id IN ?`, ids).Error; err != nil {
			return st, err
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(pause):
		}
	}
}
