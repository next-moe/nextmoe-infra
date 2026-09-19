// Package storestats pulls the redirector's JST-day click buckets into the
// platform database, so the portal, the /v1/store read face and settlement all
// read one local table instead of fanning out to another service per request.
package storestats

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/store/model"
	"api/internal/platform/store/shortener"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Reader is the shortener leg this job needs.
type Reader interface {
	DailyStats(ctx context.Context, aliases []string, from, to string) (map[string][]shortener.DayStat, error)
}

type Opts struct {
	// WindowDays is how far back a routine run re-reads. Three JST days covers
	// a click written late on the redirector plus the current partial day.
	WindowDays int
	// Batch is the alias count per upstream call; the contract caps it at 500.
	Batch int
	Now   time.Time
	// From, when set, replaces the rolling window with [From, Now], clamped to
	// the earliest mint. The resync job uses it to re-read whole settlement
	// months after the redirector recounts its history.
	From time.Time
}

func DefaultOpts() Opts { return Opts{WindowDays: 3, Batch: shortener.MaxAliasesPerStatsCall} }

// ResyncOpts re-reads everything since the first day of the previous JST
// calendar month: the months a coupon batch can still be split over.
func ResyncOpts(now time.Time) Opts {
	opts := DefaultOpts()
	jst := now.In(model.JST())
	opts.From = time.Date(jst.Year(), jst.Month()-1, 1, 0, 0, 0, 0, model.JST())
	return opts
}

type Result struct {
	Aliases  int
	Calls    int
	Rows     int
	From     string
	To       string
	FullPull bool
}

type aliasRow struct {
	Alias     string
	CreatedAt time.Time
}

func Run(ctx context.Context, db *gorm.DB, reader Reader, opts Opts) (Result, error) {
	if opts.WindowDays < 1 {
		opts.WindowDays = 3
	}
	if opts.Batch < 1 || opts.Batch > shortener.MaxAliasesPerStatsCall {
		opts.Batch = shortener.MaxAliasesPerStatsCall
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	aliases, earliest, err := collectAliases(ctx, db)
	if err != nil {
		return Result{}, err
	}
	res := Result{Aliases: len(aliases)}
	if len(aliases) == 0 {
		return res, nil
	}

	var synced int64
	if err := db.WithContext(ctx).Model(&model.LinkDailyStat{}).Count(&synced).Error; err != nil {
		return res, err
	}

	to := opts.Now
	from := to.AddDate(0, 0, -(opts.WindowDays - 1))
	if !opts.From.IsZero() {
		res.FullPull = true
		from = opts.From
		if from.Before(earliest) {
			from = earliest
		}
	}
	if synced == 0 {
		// Nothing cached yet, so the window would silently start three days ago
		// and lose every click a link collected before this job first ran.
		res.FullPull = true
		from = earliest
	}
	res.From, res.To = model.JSTDay(from), model.JSTDay(to)

	for _, window := range dayWindows(from, to) {
		for _, batch := range chunk(aliases, opts.Batch) {
			stats, err := reader.DailyStats(ctx, batch, window[0], window[1])
			if err != nil {
				return res, fmt.Errorf("daily stats %s..%s: %w", window[0], window[1], err)
			}
			res.Calls++
			n, err := upsert(ctx, db, stats, opts.Now)
			if err != nil {
				return res, err
			}
			res.Rows += n
		}
	}
	return res, nil
}

func collectAliases(ctx context.Context, db *gorm.DB) ([]string, time.Time, error) {
	var rows []aliasRow
	err := db.WithContext(ctx).Raw(`
		SELECT alias, created_at FROM store_purchase_links
		UNION ALL
		SELECT alias, created_at FROM store_coupon_links
		ORDER BY created_at`).Scan(&rows).Error
	if err != nil {
		return nil, time.Time{}, err
	}
	aliases := make([]string, 0, len(rows))
	var earliest time.Time
	for _, r := range rows {
		aliases = append(aliases, r.Alias)
		if earliest.IsZero() || r.CreatedAt.Before(earliest) {
			earliest = r.CreatedAt
		}
	}
	return aliases, earliest, nil
}

// upsert overwrites with the upstream numbers rather than adding to them: the
// redirector holds the authoritative count and every run re-reads whole days,
// so accumulating locally would multiply each day's clicks by the number of
// runs that saw it.
func upsert(ctx context.Context, db *gorm.DB, stats map[string][]shortener.DayStat, now time.Time) (int, error) {
	rows := make([]model.LinkDailyStat, 0, len(stats))
	for alias, series := range stats {
		for _, p := range series {
			rows = append(rows, model.LinkDailyStat{
				Alias: alias, Day: p.Date, Total: p.Total, Uniques: p.Uniques, Bots: p.Bots, SyncedAt: now,
			})
		}
	}
	if len(rows) == 0 {
		return 0, nil
	}
	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "alias"}, {Name: "day"}},
		DoUpdates: clause.AssignmentColumns([]string{"total", "uniques", "bots", "synced_at"}),
	}).CreateInBatches(&rows, 500).Error
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// dayWindows splits a closed JST-day interval into pieces the contract accepts.
func dayWindows(from, to time.Time) [][2]string {
	// Normalise to JST midnight first: from/to carry wall-clock times, and
	// stepping 92 days off 15:00 would drift the window boundaries.
	cur, _ := model.ParseJSTDay(model.JSTDay(from))
	end, _ := model.ParseJSTDay(model.JSTDay(to))
	var out [][2]string
	for !cur.After(end) {
		last := cur.AddDate(0, 0, shortener.MaxStatsSpanDays-1)
		if last.After(end) {
			last = end
		}
		out = append(out, [2]string{model.JSTDay(cur), model.JSTDay(last)})
		cur = last.AddDate(0, 0, 1)
	}
	return out
}

func chunk(all []string, size int) [][]string {
	var out [][]string
	for i := 0; i < len(all); i += size {
		end := min(i+size, len(all))
		out = append(out, all[i:end])
	}
	return out
}
