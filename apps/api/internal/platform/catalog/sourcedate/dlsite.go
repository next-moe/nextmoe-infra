package sourcedate

import "time"

// works.regist_date is not the date. Older rows hold the API's JST midnight
// read as UTC+8, a day early (13,457 of 14,274 releases the 07-08 import wrote);
// newer rows hold the API's time read as UTC, a day late (2,009 releases on
// 2026-09-19); some are months stale. Where VNDB dates the same product it
// agreed with the API string's day 19 times and with the column never. The
// API's own string is the date.
const DLsiteDaySQL = `coalesce(
	CASE WHEN product_json->>'regist_date' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}' THEN left(product_json->>'regist_date', 10) END,
	to_char(regist_date AT TIME ZONE 'Asia/Tokyo', 'YYYY-MM-DD'),
	'')`

func DLsite(ymd string, maxYear int) Verdict {
	if ymd == "" {
		return Verdict{State: Unknown}
	}
	t, err := time.Parse("2006-01-02", ymd)
	if err != nil {
		return Verdict{State: Unknown}
	}
	y := t.Year()
	if y < minYear || y > maxYear {
		return Verdict{State: Unknown}
	}
	yy, mm, dd := int16(y), int16(t.Month()), int16(t.Day())
	return Verdict{State: Dated, Y: yy, M: &mm, D: &dd}
}
