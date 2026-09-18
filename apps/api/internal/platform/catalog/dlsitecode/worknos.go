package dlsitecode

import (
	"regexp"
	"strings"
)

var worknoRe = regexp.MustCompile(`(?i)(RJ|VJ|BJ)[0-9]+`)

func Worknos(s string) []string {
	// Digit runs are exactly 6 or 8; tokens are compared whole so RJ012253 does
	// not match inside RJ01225370.
	matches := worknoRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		digits := 0
		for i := len(m) - 1; i >= 0; i-- {
			if m[i] < '0' || m[i] > '9' {
				break
			}
			digits++
		}
		if digits != 6 && digits != 8 {
			continue
		}
		up := strings.ToUpper(m)
		if _, ok := seen[up]; ok {
			continue
		}
		seen[up] = struct{}{}
		out = append(out, up)
	}
	return out
}
