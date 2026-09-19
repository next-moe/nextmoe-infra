package sourcedate

import (
	"strconv"
	"strings"
)

func Bangumi(s string, maxYear int) Verdict {
	y, m, d, ok := parseFuzzyDate(s, maxYear)
	if !ok {
		return Verdict{State: Unknown}
	}
	return Verdict{State: Dated, Y: y, M: m, D: d}
}

func parseFuzzyDate(s string, maxYear int) (y int16, m, d *int16, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0, nil, nil, false
	}
	yy, err := strconv.Atoi(s[:4])
	if err != nil || yy < minYear || yy > maxYear {
		return 0, nil, nil, false
	}
	y = int16(yy)
	if len(s) >= 7 && s[4] == '-' {
		if mm, err := strconv.Atoi(s[5:7]); err == nil && mm >= 1 && mm <= 12 {
			mv := int16(mm)
			m = &mv
			if len(s) >= 10 && s[7] == '-' {
				if dd, err := strconv.Atoi(s[8:10]); err == nil && dd >= 1 && dd <= 31 {
					dv := int16(dd)
					d = &dv
				}
			}
		}
	}
	return y, m, d, true
}
