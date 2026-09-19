package sourcedate

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const getchuMinYear = 1970

var (
	getchuYMD   = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})$`)
	getchuFuzzy = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(上旬|中旬|下旬|予定)$`)
)

func Getchu(s string, maxYear int) Verdict {
	s = strings.TrimSpace(s)
	if s == "未定" {
		return Verdict{State: TBA}
	}
	if m := getchuYMD.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
			return Verdict{State: Unknown}
		}
		// 0001/01/01 is Getchu's placeholder and a real calendar date; year 1 is below 1970.
		if y < getchuMinYear || y > maxYear {
			return Verdict{State: Unknown}
		}
		yy, mm, dd := int16(y), int16(mo), int16(d)
		return Verdict{State: Dated, Y: yy, M: &mm, D: &dd}
	}
	if m := getchuFuzzy.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if y < getchuMinYear || y > maxYear || mo < 1 || mo > 12 {
			return Verdict{State: Unknown}
		}
		yy, mm := int16(y), int16(mo)
		return Verdict{State: Dated, Y: yy, M: &mm}
	}
	return Verdict{State: Unknown}
}
