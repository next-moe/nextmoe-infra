package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

var calendarJST = time.FixedZone("Asia/Tokyo", 9*60*60)

var calendarPrecision = []string{"day", "month", "year"}
var calendarStatus = []string{"released", "dated", "announced", "cancelled", "unknown"}

type calendarParams struct {
	Month, Year, Precision, Status string
	ContentLimit, OLang            string
}

func (c *Catalog) ListCalendar(ctx context.Context, q collect.Query, p calendarParams) (repr.CalendarList, error) {
	if c == nil || c.Public == nil {
		return repr.CalendarList{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	if q.Batch {
		return repr.CalendarList{}, feedNoBatch("calendar")
	}
	limits, err := closedCSV(p.ContentLimit, "content_limit", []string{
		catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
	})
	if err != nil {
		return repr.CalendarList{}, err
	}
	win, werr := calendarWindow(p.Month, p.Year, p.Precision, p.Status, time.Now())
	if werr != nil {
		return repr.CalendarList{}, werr
	}
	f := catsvc.CalendarFilter{
		NSFW: q.NSFW, Include: listWorksInclude(q.Include),
		DisplayLimits: limits, OLang: calendarOLang(p.OLang),
	}
	meta := &repr.CalendarMeta{Today: time.Now().In(calendarJST).Format("2006-01-02")}
	if win.empty {
		return repr.CalendarList{List: finishList([]repr.Work{}, nil, 0, q, nil), Meta: *meta}, nil
	}
	if win.bucket.Kind == catsvc.CalendarMonthBucket {
		if merr := calendarMonthMeta(ctx, c, f, win.bucket, meta); merr != nil {
			return repr.CalendarList{}, merr
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	data, lerr := c.Public.CalendarPage(ctx, win.bucket, f, q.Cursor, limit)
	if lerr != nil {
		return repr.CalendarList{}, listCursorErr(lerr)
	}
	items := make([]repr.Work, 0, len(data.Items))
	for _, it := range data.Items {
		items = append(items, workFromListItem(it, q.Include, c.imageURL))
	}
	var total int64
	if q.IncludeTotal {
		n, _, merr := c.Public.CalendarMeta(ctx, win.bucket, f)
		if merr != nil {
			return repr.CalendarList{}, merr
		}
		total = n
	}
	return repr.CalendarList{List: finishList(items, data.NextCursor, total, q, nil), Meta: *meta}, nil
}

func calendarMonthMeta(ctx context.Context, c *Catalog, f catsvc.CalendarFilter, b catsvc.CalendarBucket, meta *repr.CalendarMeta) error {
	minOrd, maxOrd, found, err := c.Public.CalendarBounds(ctx, f)
	if err != nil {
		return err
	}
	if !found {
		no := false
		meta.HasPrev, meta.HasNext = &no, &no
		return nil
	}
	mn, mx := calendarMonthOfOrdinal(minOrd), calendarMonthOfOrdinal(maxOrd)
	meta.MinMonth, meta.MaxMonth = &mn, &mx
	cur := int64(b.Year)*10000 + int64(b.Month)*100
	prev, next := cur > minOrd, cur < maxOrd
	meta.HasPrev, meta.HasNext = &prev, &next
	return nil
}

func calendarMonthOfOrdinal(ord int64) string {
	return fmt.Sprintf("%04d-%02d", ord/10000, (ord/100)%100)
}

// Not parseOLang: the calendar's home population is ja+zh (the zero
// PublicOLang), matching v1, while the works lane defaults to all languages.
func calendarOLang(raw string) catsvc.PublicOLang {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "":
		return catsvc.PublicOLang{}
	case "all":
		return catsvc.PublicOLang{All: true}
	}
	var vals []string
	seen := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		vals = append(vals, tok)
	}
	if len(vals) == 0 {
		return catsvc.PublicOLang{}
	}
	return catsvc.PublicOLang{Values: vals}
}

type calendarWin struct {
	bucket catsvc.CalendarBucket
	empty  bool
}

func calendarWindow(month, year, precision, status string, now time.Time) (calendarWin, *problem.Problem) {
	if precision != "" {
		if _, err := parse.Enum(precision, "precision", calendarPrecision); err != nil {
			return calendarWin{}, err
		}
	}
	if status != "" {
		if _, err := parse.Enum(status, "status", calendarStatus); err != nil {
			return calendarWin{}, err
		}
	}
	if status == "cancelled" {
		return calendarWin{empty: true}, nil
	}

	jst := now.In(calendarJST)
	month = strings.TrimSpace(month)
	year = strings.TrimSpace(year)

	undated := status == "announced" || status == "unknown"
	if undated && month == "" && precision != "day" && precision != "month" {
		return calendarWin{bucket: catsvc.CalendarBucket{Kind: catsvc.CalendarTBABucket}}, nil
	}
	if undated && month != "" {
		return calendarWin{empty: true}, nil
	}

	if precision == "year" || (year != "" && month == "") {
		y := jst.Year()
		if year != "" {
			t, perr := time.Parse("2006", year)
			if perr != nil || len(year) != 4 {
				p := problem.New(problem.CodeInvalidParameter, "", "", "year must be YYYY.")
				p.Errors = []problem.FieldError{{Parameter: "year", Reason: problem.ReasonInvalidFormat, Detail: "expected YYYY"}}
				return calendarWin{}, p
			}
			y = t.Year()
		}
		return calendarWin{bucket: catsvc.CalendarBucket{Kind: catsvc.CalendarPendingBucket, Year: y}}, nil
	}

	m := jst.Format("2006-01")
	if month != "" {
		m = month
	}
	t, perr := time.Parse("2006-01", m)
	if perr != nil || len(m) != 7 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "month must be YYYY-MM.")
		p.Errors = []problem.FieldError{{Parameter: "month", Reason: problem.ReasonInvalidFormat, Detail: "expected YYYY-MM"}}
		return calendarWin{}, p
	}
	return calendarWin{bucket: catsvc.CalendarBucket{
		Kind: catsvc.CalendarMonthBucket, Year: t.Year(), Month: int(t.Month()),
	}}, nil
}
