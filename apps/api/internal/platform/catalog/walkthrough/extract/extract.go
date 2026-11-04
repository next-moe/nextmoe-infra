package extract

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"api/internal/platform/catalog/walkthrough"
)

const (
	VerdictOK       = "ok"
	VerdictNoChart  = "no_chart"
	VerdictReview   = "review"
	VerdictRejected = "rejected"
	VerdictError    = "error"
)

type Metrics struct {
	Seconds           float64  `json:"seconds"`
	CompletionTokens  int      `json:"completion_tokens"`
	Coverage          float64  `json:"coverage"`
	Missed            []string `json:"missed"`
	EndMarkers        int      `json:"end_markers"`
	EndingsReached    int      `json:"endings_reached"`
	NotVerbatim       []string `json:"not_verbatim"`
	DroppedConditions []string `json:"dropped_conditions"`
	Problems          []string `json:"problems"`
}

var (
	endMarker    = regexp.MustCompile(`(END|ＥＮＤ|エンド)\s*[0-9０-９]*$`)
	chartStart   = regexp.MustCompile(`(START|【セーブ|◆セーブ|はじめから)`)
	markerLine   = regexp.MustCompile(`(セーブ|(?i:start)|開始|はじめから|初めから|最初から|からスタート|END|ＥＮＤ|回収|終了|━)`)
	numberedMark = regexp.MustCompile(`^[0-9０-９]*\s*■`)
	orWord       = regexp.MustCompile(`(?i)\bor\b|選択肢`)
	siteChrome   = regexp.MustCompile(`拡大リンク|クリックで.*表示|参考画像`)
	whitespaces  = regexp.MustCompile(`\s+`)
)

func squash(s string) string { return whitespaces.ReplaceAllString(s, "") }

func Settle(text string, got Extracted, m *Metrics) (string, *walkthrough.Chart) {
	if got.NoChart {
		return VerdictNoChart, nil
	}
	chart := got.Chart()
	walkthrough.Normalize(&chart)
	m.DroppedConditions = dropUnquotedConditions(text, &chart)
	quoteCharacterNames(text, &chart)
	RepairFromSource(text, &chart)
	walkthrough.Normalize(&chart)
	return Gate(text, chart, m), &chart
}

func Gate(text string, c walkthrough.Chart, m *Metrics) string {
	m.Problems = walkthrough.Validate(c)
	m.NotVerbatim = notVerbatim(text, c)
	m.Coverage, m.Missed = coverage(text, c)
	m.EndMarkers = countEndMarkers(text)
	for _, s := range c.Segments {
		if s.To.NodeType == walkthrough.NodeEnding {
			m.EndingsReached++
		}
	}
	switch {
	case len(m.Problems) > 0 || len(m.NotVerbatim) > 0:
		return VerdictRejected
	case m.Coverage < 0.9 || m.EndingsReached < m.EndMarkers:
		return VerdictReview
	default:
		return VerdictOK
	}
}

func notVerbatim(text string, c walkthrough.Chart) []string {
	src := squash(text)
	var bad []string
	check := func(s string) {
		if t := squash(s); t != "" && !strings.Contains(src, t) {
			bad = append(bad, s)
		}
	}
	for _, s := range c.Segments {
		for _, st := range s.Steps {
			if st.Text != nil {
				check(*st.Text)
			}
			for _, a := range st.Alt {
				check(a)
			}
			if st.Remark != nil {
				check(*st.Remark)
			}
		}
	}
	for _, l := range c.Legend {
		check(l.Text)
	}
	for _, e := range c.Endings {
		check(e.Name)
	}
	for _, s := range c.Sections {
		check(s.Title)
	}
	for _, ch := range c.Characters {
		check(ch.Name)
	}
	return bad
}

func dropUnquotedConditions(text string, c *walkthrough.Chart) []string {
	src := squash(text)
	var dropped []string
	for i := range c.Segments {
		if cond := c.Segments[i].Condition; cond != nil && !strings.Contains(src, squash(*cond)) {
			dropped = append(dropped, *cond)
			c.Segments[i].Condition = nil
		}
	}
	return dropped
}

func quoteCharacterNames(text string, c *walkthrough.Chart) {
	src := squash(text)
	for i := range c.Characters {
		ch := &c.Characters[i]
		if strings.Contains(src, squash(ch.Name)) {
			continue
		}
		for j, a := range ch.Aliases {
			if strings.Contains(src, squash(a)) {
				ch.Aliases[j] = ch.Name
				ch.Name = a
				break
			}
		}
	}
}

func chartRegion(text string) []string {
	lines := strings.Split(text, "\n")
	first, last := -1, -1
	for i, l := range lines {
		if first < 0 && chartStart.MatchString(l) {
			first = i
		}
		if endMarker.MatchString(strings.TrimSpace(l)) {
			last = i
		}
	}
	if first < 0 || last < first {
		return nil
	}
	return lines[first : last+1]
}

func countEndMarkers(text string) int {
	n := 0
	for _, l := range chartRegion(text) {
		if endMarker.MatchString(strings.TrimSpace(l)) {
			n++
		}
	}
	return n
}

func coverage(text string, c walkthrough.Chart) (float64, []string) {
	var known []string
	add := func(s string) {
		if t := squash(s); t != "" {
			known = append(known, t)
		}
	}
	for _, s := range c.Segments {
		for _, st := range s.Steps {
			if st.Text != nil {
				add(*st.Text)
				if st.Remark != nil {
					add(*st.Text + *st.Remark)
				}
			}
			for _, a := range st.Alt {
				add(a)
			}
		}
		if s.Condition != nil {
			add(*s.Condition)
		}
	}
	for _, s := range c.Sections {
		add(s.Title)
	}
	for _, ch := range c.Characters {
		add(ch.Name)
	}
	for _, e := range c.Endings {
		add(e.Name)
	}
	for _, e := range c.Entries {
		add(e.Label)
	}
	for _, l := range c.Legend {
		add(l.Text)
	}
	slices.SortFunc(known, func(a, b string) int { return len(b) - len(a) })
	var total int
	var missed []string
	for _, raw := range chartRegion(text) {
		l := strings.TrimSpace(raw)
		if l == "" || markerLine.MatchString(l) || strings.Contains(l, "│") || strings.HasPrefix(l, "※") ||
			numberedMark.MatchString(l) || strings.HasPrefix(l, "[") || isHeadingLine(l) || siteChrome.MatchString(l) {
			continue
		}
		total++
		if !covered(squash(l), known) {
			missed = append(missed, l)
		}
	}
	if total == 0 {
		return 1, nil
	}
	ratio := 1 - float64(len(missed))/float64(total)
	if len(missed) > 8 {
		missed = missed[:8]
	}
	return ratio, missed
}

func covered(line string, known []string) bool {
	rest := line
	for _, k := range known {
		if strings.Contains(k, line) {
			return true
		}
		rest = strings.ReplaceAll(rest, k, "")
	}
	rest = orWord.ReplaceAllString(rest, "")
	n := 0
	for _, r := range rest {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if n++; n >= 2 {
				return false
			}
		}
	}
	return true
}
