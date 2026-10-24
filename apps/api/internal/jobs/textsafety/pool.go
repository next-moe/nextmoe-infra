package textsafety

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const galgameLiveSQL = `w.deleted_at IS NULL AND w.status = ?
	AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')`

type workTitles struct {
	ID    int64
	Hash  string
	Names []string
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func titleSetHash(names []string) string { return md5Hex(strings.Join(names, "\n")) }

// loadWorkTitles returns every live galgame work with the names that decide its
// shelf: the display name and its official and alias titles. Release titles are
// judged one by one (loadPendingReleaseTitles) and hidden on their own, because
// folding them in let a bundle or crash-fix release carry other games' names
// onto a work and move it to the nsfw shelf. Search hints are never rendered and
// stay out.
func loadWorkTitles(ctx context.Context, db *gorm.DB) ([]workTitles, error) {
	var rows []struct {
		ID          int64  `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
		Titles      string `gorm:"column:titles"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id, w.display_name,
		       coalesce((SELECT string_agg(t.title, E'\x1f') FROM catalog_work_title t WHERE t.work_id = w.id AND t.kind IN (?, ?)), '') AS titles
		FROM catalog_work w
		WHERE `+galgameLiveSQL+`
		ORDER BY w.id`,
		model.WorkTitleKindOfficial, model.WorkTitleKindAlias, model.WorkStatusLive).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workTitles, 0, len(rows))
	for _, r := range rows {
		seen := map[string]bool{}
		var names []string
		for _, n := range append([]string{r.DisplayName}, strings.Split(r.Titles, "\x1f")...) {
			n = strings.TrimSpace(n)
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		out = append(out, workTitles{ID: r.ID, Hash: titleSetHash(names), Names: names})
	}
	return out, nil
}

// loadPendingReleaseTitles returns each distinct release title of a live galgame
// work that has no verdict, as a one-name set: its hash is md5 of the trimmed
// title, the expression the read face looks it up by.
func loadPendingReleaseTitles(ctx context.Context, db *gorm.DB) ([]workTitles, error) {
	return releaseTitleSets(ctx, db, `AND NOT EXISTS (SELECT 1 FROM catalog_work_title_safety s WHERE s.title_hash = md5(btrim(r.title)))`)
}

func loadReleaseTitleSets(ctx context.Context, db *gorm.DB) ([]workTitles, error) {
	return releaseTitleSets(ctx, db, "")
}

func releaseTitleSets(ctx context.Context, db *gorm.DB, extra string) ([]workTitles, error) {
	var titles []string
	if err := db.WithContext(ctx).Raw(`SELECT DISTINCT btrim(r.title) FROM catalog_release r
		JOIN catalog_work w ON w.id = r.work_id
		WHERE `+galgameLiveSQL+` AND r.deleted_at IS NULL AND btrim(coalesce(r.title, '')) <> '' `+extra,
		model.WorkStatusLive).Scan(&titles).Error; err != nil {
		return nil, err
	}
	out := make([]workTitles, len(titles))
	for i, t := range titles {
		out[i] = workTitles{Hash: titleSetHash([]string{t}), Names: []string{t}}
	}
	return out, nil
}

type IntroInput struct {
	Hash  string
	Lang  string
	Intro string
	Title string
}

// sfwShelfSQL selects the live galgame works an sfw page shows: only their
// intros, and only the intros of characters appearing in them, are ever served
// from the judged face. A work that moves onto the sfw shelf later is picked up
// by the next run. Takes the r18 rating as its one argument.
var sfwShelfSQL = `NOT ` + service.DisplayLimitNSFWSQL

// loadPendingIntros returns the distinct intro texts that have no verdict yet
// and hang on at least one work on the sfw shelf.
func loadPendingIntros(ctx context.Context, db *gorm.DB, limit int) ([]IntroInput, error) {
	var rows []IntroInput
	q := `SELECT DISTINCT ON (md5(i.intro)) md5(i.intro) AS hash, i.lang, i.intro, w.display_name AS title
		FROM catalog_work_intro i
		JOIN catalog_work w ON w.id = i.work_id
		WHERE ` + galgameLiveSQL + ` AND ` + sfwShelfSQL + ` AND btrim(i.intro) <> ''
		  AND NOT EXISTS (SELECT 1 FROM catalog_work_intro_safety s WHERE s.intro_hash = md5(i.intro))
		ORDER BY md5(i.intro), w.id`
	args := []any{model.WorkStatusLive, model.ContentRatingR18}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return rows, db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error
}

// LoadIntroSample returns n distinct intros in a fixed pseudo-random order,
// judged or not, for calibration runs.
func LoadIntroSample(ctx context.Context, db *gorm.DB, n int) ([]IntroInput, error) {
	var rows []IntroInput
	return rows, db.WithContext(ctx).Raw(`SELECT * FROM (
		SELECT DISTINCT ON (md5(i.intro)) md5(i.intro) AS hash, i.lang, i.intro, w.display_name AS title
		FROM catalog_work_intro i
		JOIN catalog_work w ON w.id = i.work_id
		WHERE `+galgameLiveSQL+` AND btrim(i.intro) <> ''
		ORDER BY md5(i.intro), w.id) x
		ORDER BY md5(x.hash || 'sample') LIMIT ?`, model.WorkStatusLive, n).Scan(&rows).Error
}

// loadPendingCharacterIntros is loadPendingIntros for characters: an intro is
// needed when its character appears in at least one work on the sfw shelf.
func loadPendingCharacterIntros(ctx context.Context, db *gorm.DB, limit int) ([]IntroInput, error) {
	var rows []IntroInput
	q := `SELECT DISTINCT ON (md5(ci.intro)) md5(ci.intro) AS hash, ci.lang, ci.intro, c.display_name AS title
		FROM catalog_character_intro ci
		JOIN catalog_character c ON c.id = ci.character_id AND c.deleted_at IS NULL
		WHERE btrim(ci.intro) <> ''
		  AND EXISTS (SELECT 1 FROM catalog_work_character wc JOIN catalog_work w ON w.id = wc.work_id
		              WHERE wc.character_id = ci.character_id AND ` + galgameLiveSQL + ` AND ` + sfwShelfSQL + `)
		  AND NOT EXISTS (SELECT 1 FROM catalog_character_intro_safety s WHERE s.intro_hash = md5(ci.intro))
		ORDER BY md5(ci.intro), ci.character_id`
	args := []any{model.WorkStatusLive, model.ContentRatingR18}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return rows, db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error
}

func LoadPendingCharacterIntros(ctx context.Context, db *gorm.DB, limit int) ([]IntroInput, error) {
	return loadPendingCharacterIntros(ctx, db, limit)
}

// LoadCharacterIntroSample is LoadIntroSample for character intros.
func LoadCharacterIntroSample(ctx context.Context, db *gorm.DB, n int) ([]IntroInput, error) {
	var rows []IntroInput
	return rows, db.WithContext(ctx).Raw(`SELECT * FROM (
		SELECT DISTINCT ON (md5(ci.intro)) md5(ci.intro) AS hash, ci.lang, ci.intro, c.display_name AS title
		FROM catalog_character_intro ci
		JOIN catalog_character c ON c.id = ci.character_id AND c.deleted_at IS NULL
		WHERE btrim(ci.intro) <> ''
		  AND EXISTS (SELECT 1 FROM catalog_work_character wc JOIN catalog_work w ON w.id = wc.work_id
		              WHERE wc.character_id = ci.character_id AND `+galgameLiveSQL+`)
		ORDER BY md5(ci.intro), ci.character_id) x
		ORDER BY md5(x.hash || 'sample') LIMIT ?`, model.WorkStatusLive, n).Scan(&rows).Error
}

func LoadPendingIntros(ctx context.Context, db *gorm.DB, limit int) ([]IntroInput, error) {
	return loadPendingIntros(ctx, db, limit)
}

// parallel runs fn over every batch with at most conc in flight.
func parallel[T any](ctx context.Context, batches [][]T, conc int, fn func(context.Context, []T)) {
	sem := make(chan struct{}, max(conc, 1))
	var wg sync.WaitGroup
	for _, b := range batches {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(b []T) {
			defer func() { <-sem; wg.Done() }()
			fn(ctx, b)
		}(b)
	}
	wg.Wait()
}

// pack groups items so no batch exceeds maxRunes of text or maxItems entries;
// an item larger than maxRunes travels alone.
func pack[T any](items []T, size func(T) int, maxRunes, maxItems int) [][]T {
	var out [][]T
	var cur []T
	runes := 0
	for _, it := range items {
		n := size(it)
		if len(cur) > 0 && (runes+n > maxRunes || len(cur) >= maxItems) {
			out = append(out, cur)
			cur, runes = nil, 0
		}
		cur = append(cur, it)
		runes += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// bisect runs a batch and, when the provider refuses it, halves it until the
// refused items stand alone; those are answered by refused.
func bisect[T, R any](ctx context.Context, batch []T, run func(context.Context, []T) ([]R, error), refused func(T) R) ([]R, error) {
	out, err := run(ctx, batch)
	if !errors.Is(err, ErrBlocked) {
		return out, err
	}
	if len(batch) == 1 {
		return []R{refused(batch[0])}, nil
	}
	mid := len(batch) / 2
	left, err := bisect(ctx, batch[:mid], run, refused)
	if err != nil {
		return nil, err
	}
	right, err := bisect(ctx, batch[mid:], run, refused)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}
