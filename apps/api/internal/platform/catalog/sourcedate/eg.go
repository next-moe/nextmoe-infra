package sourcedate

import (
	"strings"
	"time"
)

func EG(sellday string, now time.Time) Verdict {
	sellday = strings.TrimSpace(sellday)
	t, err := time.Parse("2006-01-02", sellday)
	if err != nil {
		return Verdict{State: Unknown}
	}
	// EG writes 2050-01-01 for an unannounced release (176 games in production).
	if t.After(now.AddDate(2, 0, 0)) {
		return Verdict{State: TBA}
	}
	if t.Year() < minYear {
		return Verdict{State: Unknown}
	}
	yy, mm, dd := int16(t.Year()), int16(t.Month()), int16(t.Day())
	return Verdict{State: Dated, Y: yy, M: &mm, D: &dd}
}
