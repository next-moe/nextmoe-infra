// Package ratingimport copies the forum's galgame_rating and LetMoe's
// game_rating into catalog_user_work_rating. Once a site writes to the catalog
// its own table becomes a mirror of the central one, and importing a mirror
// would file other sites' ratings under that site: run it against a site only
// before that site's switch.
package ratingimport

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"
	"unicode/utf8"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const (
	SiteKungal = "kungal"
	SiteLetmoe = "letmoe"
)

type Source struct {
	Site     string
	ClientID string
	DB       *gorm.DB
}

type Opts struct {
	Catalog *gorm.DB
	Sources []Source
	Apply   bool
}

type Stats struct {
	Site         string
	Read         int
	Inserted     int
	Updated      int
	Unchanged    int
	Adopted      int
	SkippedGone  int
	SkippedWork  int
	Redirected   int
	Invalid      int
	Withdrawn    int
	StatesSeeded int
	StatesKept   int
}

type Result struct {
	Sites      []Stats
	Aggregates service.RatingAggregateStats
}

type sourceRow struct {
	ID           int64
	UserID       int64
	WorkID       int64
	Overall      int16
	Aspects      [8]*int16
	Recommend    int16
	SpoilerLevel int16
	ShortSummary string
	State        int16
	Completion   *int16
	Created      time.Time
	Updated      time.Time
}

var errDryRun = stderrors.New("dry run")

func Run(ctx context.Context, o Opts) (Result, error) {
	read := make([][]sourceRow, len(o.Sources))
	for i, src := range o.Sources {
		rows, err := readSource(ctx, src)
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", src.Site, err)
		}
		read[i] = rows
	}
	var out Result
	var started time.Time
	err := o.Catalog.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT now()`).Scan(&started).Error; err != nil {
			return err
		}
		for i, src := range o.Sources {
			st, err := importSite(tx, src, read[i])
			if err != nil {
				return fmt.Errorf("import %s: %w", src.Site, err)
			}
			out.Sites = append(out.Sites, st)
		}
		var err error
		if out.Aggregates, err = service.RefreshRatingAggregatesTx(tx); err != nil {
			return err
		}
		if !o.Apply {
			return errDryRun
		}
		return nil
	})
	if err == errDryRun {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, restamp(ctx, o.Catalog, started)
}

// Every row the import wrote carries the time its transaction began, and the
// import runs long enough for a mirror polling a feed to move past that time
// before the commit; stamping again in a short transaction afterwards puts the
// rows back in front of every cursor.
func restamp(ctx context.Context, db *gorm.DB, since time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{
			`UPDATE catalog_user_work_rating SET changed_at = now() WHERE changed_at >= ?`,
			`UPDATE catalog_user_work_rating_tombstone SET deleted_at = now() WHERE deleted_at >= ?`,
			`UPDATE catalog_user_work_state SET updated_at = now() WHERE updated_at >= ?`,
			`UPDATE catalog_work SET updated_at = now() WHERE updated_at >= ?`,
		} {
			if err := tx.Exec(stmt, since).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type central struct {
	ID       int64
	ActorUID int64
	WorkID   int64
	Updated  time.Time `gorm:"column:updated_at"`
}

func importSite(tx *gorm.DB, src Source, rows []sourceRow) (Stats, error) {
	st := Stats{Site: src.Site, Read: len(rows)}
	works, err := resolveWorks(tx, rows)
	if err != nil {
		return st, err
	}
	ledger := map[int64]model.CatalogUserWorkRatingImport{}
	var led []model.CatalogUserWorkRatingImport
	if err := tx.Where("site = ?", src.Site).Find(&led).Error; err != nil {
		return st, err
	}
	for _, l := range led {
		ledger[l.SourceID] = l
	}
	seen := map[int64]bool{}
	for _, r := range rows {
		seen[r.ID] = true
		if !validRow(r) {
			st.Invalid++
			continue
		}
		workID, redirected, ok := works.resolve(r.WorkID)
		if !ok {
			st.SkippedWork++
			continue
		}
		if redirected {
			st.Redirected++
		}
		r.WorkID = workID
		if l, ok := ledger[r.ID]; ok {
			var c central
			err := tx.Raw(`SELECT id, actor_uid, work_id, updated_at FROM catalog_user_work_rating WHERE id = ?`, l.RatingID).Scan(&c).Error
			if err != nil {
				return st, err
			}
			if c.ID == 0 {
				st.SkippedGone++
				continue
			}
			if err := refreshFromSource(tx, src, r, c, &st); err != nil {
				return st, err
			}
		} else {
			var c central
			if err := tx.Raw(`SELECT id, actor_uid, work_id, updated_at FROM catalog_user_work_rating
				WHERE actor_uid = ? AND work_id = ?`, r.UserID, r.WorkID).Scan(&c).Error; err != nil {
				return st, err
			}
			ratingID := c.ID
			if ratingID != 0 {
				st.Adopted++
				if err := refreshFromSource(tx, src, r, c, &st); err != nil {
					return st, err
				}
			} else {
				if ratingID, err = insertRating(tx, src, r); err != nil {
					return st, err
				}
				if ratingID == 0 {
					st.SkippedGone++
					continue
				}
				st.Inserted++
			}
			if err := tx.Create(&model.CatalogUserWorkRatingImport{
				Site: src.Site, SourceID: r.ID, RatingID: ratingID, SyncedAt: time.Now(),
			}).Error; err != nil {
				return st, err
			}
		}
		seeded, err := seedState(tx, r)
		if err != nil {
			return st, err
		}
		if seeded {
			st.StatesSeeded++
		} else if r.State != 0 {
			st.StatesKept++
		}
	}
	for sourceID, l := range ledger {
		if seen[sourceID] {
			continue
		}
		res := tx.Exec(`WITH gone AS (
			    DELETE FROM catalog_user_work_rating r WHERE r.id = ? AND r.updated_at <= ?
			       AND NOT EXISTS (SELECT 1 FROM catalog_user_work_rating_import o
			                        WHERE o.rating_id = r.id AND NOT (o.site = ? AND o.source_id = ?))
			    RETURNING r.id, r.work_id)
			INSERT INTO catalog_user_work_rating_tombstone (id, work_id, deleted_at)
			SELECT id, work_id, now() FROM gone`, l.RatingID, l.SyncedAt, src.Site, sourceID)
		if res.Error != nil {
			return st, res.Error
		}
		st.Withdrawn += int(res.RowsAffected)
	}
	return st, nil
}

func refreshFromSource(tx *gorm.DB, src Source, r sourceRow, c central, st *Stats) error {
	if !r.Updated.After(c.Updated) {
		st.Unchanged++
	} else {
		a := r.Aspects
		if err := tx.Exec(`UPDATE catalog_user_work_rating
			   SET overall = ?, art = ?, story = ?, music = ?, "character" = ?, route = ?, "system" = ?,
			       voice = ?, replay_value = ?, recommend = ?, spoiler_level = ?, short_summary = ?,
			       updated_at = ?, changed_at = now()
			 WHERE id = ?`,
			r.Overall, a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7],
			r.Recommend, r.SpoilerLevel, r.ShortSummary, r.Updated, c.ID).Error; err != nil {
			return err
		}
		st.Updated++
	}
	return tx.Exec(`UPDATE catalog_user_work_rating_import SET synced_at = now() WHERE site = ? AND source_id = ?`,
		src.Site, r.ID).Error
}

// The forum keeps its ids: likes and wall threads on the forum hang on them.
// They sit below the catalog's own ids, which the migration starts at
// 1,000,001. An id the catalog already tombstoned stays deleted.
func insertRating(tx *gorm.DB, src Source, r sourceRow) (int64, error) {
	a := r.Aspects
	cols := `actor_uid, work_id, overall, art, story, music, "character", route, "system", voice, replay_value,
		recommend, spoiler_level, short_summary, client_id, site, created_at, updated_at`
	args := []any{r.UserID, r.WorkID, r.Overall, a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7],
		r.Recommend, r.SpoilerLevel, r.ShortSummary, src.ClientID, src.Site, r.Created, r.Updated}
	var id int64
	if src.Site == SiteKungal {
		var gone int64
		if err := tx.Raw(`SELECT count(*) FROM catalog_user_work_rating_tombstone WHERE id = ?`, r.ID).Scan(&gone).Error; err != nil {
			return 0, err
		}
		if gone > 0 {
			return 0, nil
		}
		err := tx.Raw(`INSERT INTO catalog_user_work_rating (id, `+cols+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			append([]any{r.ID}, args...)...).Scan(&id).Error
		return id, err
	}
	err := tx.Raw(`INSERT INTO catalog_user_work_rating (`+cols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, args...).Scan(&id).Error
	return id, err
}

// An existing work state is newer than the rating row it would come from.
// updated_at is the work-state sync cursor, so a seeded state takes the time
// it reached the catalog, not the rating's.
func seedState(tx *gorm.DB, r sourceRow) (bool, error) {
	if r.State == 0 {
		return false, nil
	}
	res := tx.Exec(`INSERT INTO catalog_user_work_state (actor_uid, work_id, state, completion, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, now()) ON CONFLICT (actor_uid, work_id) DO NOTHING`,
		r.UserID, r.WorkID, r.State, r.Completion, r.Created)
	if res.Error != nil || res.RowsAffected == 0 {
		return false, res.Error
	}
	return true, tx.Exec(`UPDATE catalog_user_work_rating SET changed_at = now() WHERE actor_uid = ? AND work_id = ?`,
		r.UserID, r.WorkID).Error
}

func validRow(r sourceRow) bool {
	if r.UserID <= 0 || r.Overall < model.RatingScoreMin || r.Overall > model.RatingScoreMax {
		return false
	}
	for _, v := range r.Aspects {
		if v != nil && (*v < model.RatingScoreMin || *v > model.RatingScoreMax) {
			return false
		}
	}
	return r.Recommend != 0 && r.SpoilerLevel >= 0 && utf8.ValidString(r.ShortSummary) &&
		utf8.RuneCountInString(r.ShortSummary) <= model.RatingSummaryMaxRunes
}

type workResolver struct {
	live     map[int64]bool
	redirect map[int64]int64
}

func resolveWorks(tx *gorm.DB, rows []sourceRow) (*workResolver, error) {
	w := &workResolver{live: map[int64]bool{}, redirect: map[int64]int64{}}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.WorkID)
	}
	if len(ids) == 0 {
		return w, nil
	}
	var reds []struct {
		OldID     int64
		CurrentID int64
	}
	if err := tx.Raw(`SELECT old_id, current_id FROM catalog_redirect WHERE entity_type = ? AND old_id IN ?`,
		model.EntityTypeWork, ids).Scan(&reds).Error; err != nil {
		return nil, err
	}
	for _, r := range reds {
		w.redirect[r.OldID] = r.CurrentID
		ids = append(ids, r.CurrentID)
	}
	var live []int64
	if err := tx.Raw(`SELECT id FROM catalog_work WHERE id IN ? AND status = ? AND deleted_at IS NULL`,
		ids, model.WorkStatusLive).Scan(&live).Error; err != nil {
		return nil, err
	}
	for _, id := range live {
		w.live[id] = true
	}
	return w, nil
}

func (w *workResolver) resolve(id int64) (int64, bool, bool) {
	if w.live[id] {
		return id, false, true
	}
	if next, ok := w.redirect[id]; ok && w.live[next] {
		return next, true, true
	}
	return 0, false, false
}
