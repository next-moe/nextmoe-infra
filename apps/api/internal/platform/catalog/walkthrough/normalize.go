package walkthrough

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	collectName = regexp.MustCompile(`(回収|終了)$`)
	entryDecor  = regexp.MustCompile(`^[【\[]+|[】\]]+$|(から開始|からスタート|から)$`)
	parenthesed = regexp.MustCompile(`^[(（](.*)[)）]$`)
	badName     = regexp.MustCompile(`(?i)(BAD|バッド|ＢＡＤ)`)
	trueName    = regexp.MustCompile(`(?i)(TRUE|トゥルー|ＴＲＵＥ)`)
	normalName  = regexp.MustCompile(`(?i)(NORMAL|ノーマル|ＮＯＲＭＡＬ)`)
	headingLine = regexp.MustCompile(`^[■□◆◇]?\s*(?:\d{1,2}月\d{1,2}日(?:\s*\(?[月火水木金土日](?:曜日?)?\)?)?|\d{1,2}/\d{1,2}(?:\s*\([月火水木金土日]\))?|第?[\d一二三四五六七八九十]+日目(?:\s*(?:朝|昼|夕方?|夜|午前|午後|深夜|放課後))?|第[\d一二三四五六七八九十百]+[章話部幕]|B\d+(?:-\d+)?F?)\s*[━─]*$`)
	noteLine    = regexp.MustCompile(`^[※*]`)
	routeLine   = regexp.MustCompile(`^\d+(?:\s*[→⇒]\s*\d+)+`)
	bareEnding  = regexp.MustCompile(`^(?:END|エンド|ED)$`)
	eitherOf    = regexp.MustCompile(`(?:^|[^A-Z])OR(?:[^A-Z]|$)|または|いずれか|どちらか`)
	looseJoin   = regexp.MustCompile("[^\x00][&と・、]\x00|\x00[&と・、][^\x00]")
)

func Normalize(c *Chart) {
	FillNil(c)

	for i := range c.Entries {
		c.Entries[i].Label = stripRepeated(entryDecor, c.Entries[i].Label)
	}

	collectKeys := map[string]bool{}
	for _, e := range c.Endings {
		if e.EndingType == "other" && collectName.MatchString(strings.TrimSpace(e.Name)) {
			collectKeys[e.Key] = true
		}
	}
	for i := range c.Segments {
		s := &c.Segments[i]
		if s.To.NodeType == NodeEnding && s.To.Key != nil && collectKeys[*s.To.Key] {
			s.To = Node{NodeType: NodeCollect}
		}
		if s.To.NodeType == NodeCollect || s.To.NodeType == NodeOpen {
			s.To.Key = nil
		}
		for j := range s.Steps {
			st := &s.Steps[j]
			if st.Text != nil {
				t := strings.TrimSpace(*st.Text)
				if rest, ok := strings.CutPrefix(t, "☆"); ok {
					t = strings.TrimSpace(rest)
					st.Achievement = true
				}
				for _, a := range st.Alt {
					a = strings.TrimSpace(a)
					for _, suffix := range []string{"(" + a + ")", "（" + a + "）"} {
						if head, ok := strings.CutSuffix(t, suffix); ok && strings.TrimSpace(head) != "" {
							t = strings.TrimSpace(head)
						}
					}
				}
				st.Text = &t
				bare := parenthesed.ReplaceAllString(t, "$1")
				st.Alt = slices.DeleteFunc(st.Alt, func(a string) bool {
					return strings.TrimSpace(a) == t || strings.TrimSpace(a) == bare
				})
				if st.StepType == StepChoice && len(st.Alt) == 0 {
					st.StepType = choiceKind(t)
				}
			}
			st.AltZh = st.AltZh[:min(len(st.AltZh), len(st.Alt))]
			for len(st.AltZh) < len(st.Alt) {
				st.AltZh = append(st.AltZh, nil)
			}
		}
	}

	for i := range c.Endings {
		e := &c.Endings[i]
		if e.EndingType != "normal" && e.EndingType != "other" {
			continue
		}
		switch {
		case badName.MatchString(e.Name):
			e.EndingType = "bad"
		case trueName.MatchString(e.Name):
			e.EndingType = "true"
		case normalName.MatchString(e.Name):
			e.EndingType = "normal"
		case len(e.Characters) == 1:
			e.EndingType = "heroine"
		case len(e.Characters) > 1:
			e.EndingType = "harem"
		}
	}
	fillBadEndingCharacters(c)

	reached := map[string]bool{}
	for _, s := range c.Segments {
		if s.To.NodeType == NodeEnding && s.To.Key != nil {
			reached[*s.To.Key] = true
		}
		if s.From.NodeType == NodeEnding && s.From.Key != nil {
			reached[*s.From.Key] = true
		}
	}
	for _, e := range c.Entries {
		if e.UnlockedBy != nil {
			reached[*e.UnlockedBy] = true
		}
	}
	c.Endings = slices.DeleteFunc(c.Endings, func(e Ending) bool { return !reached[e.Key] })
	resolveRequiredEndings(c)
}

func choiceKind(text string) string {
	t := norm.NFKC.String(text)
	switch {
	case headingLine.MatchString(t):
		return StepHeading
	case noteLine.MatchString(t):
		return StepNote
	case routeLine.MatchString(t):
		return StepAction
	}
	return StepChoice
}

func fillBadEndingCharacters(c *Chart) {
	sections := map[string]Section{}
	for _, s := range c.Sections {
		sections[s.Key] = s
	}
	sectionOf := map[string]string{}
	for _, s := range c.Segments {
		if s.To.NodeType == NodeEnding && s.To.Key != nil && s.Section != nil {
			sectionOf[*s.To.Key] = *s.Section
		}
	}
	for i := range c.Endings {
		e := &c.Endings[i]
		if e.EndingType != "bad" || len(e.Characters) > 0 {
			continue
		}
		if sec, ok := sections[sectionOf[e.Key]]; ok && len(sec.Characters) == 1 {
			e.Characters = []string{sec.Characters[0]}
		}
	}
}

func resolveRequiredEndings(c *Chart) {
	type named struct{ key, name string }
	var names []named
	seen := map[string]bool{}
	for _, e := range c.Endings {
		n := foldName(e.Name)
		if n == "" || seen[n] || bareEnding.MatchString(n) {
			continue
		}
		seen[n] = true
		names = append(names, named{e.Key, n})
	}
	slices.SortStableFunc(names, func(a, b named) int { return len([]rune(b.name)) - len([]rune(a.name)) })
	for i := range c.Segments {
		s := &c.Segments[i]
		if s.Condition == nil || len(s.RequiresEndings) > 0 {
			continue
		}
		cond := foldName(*s.Condition)
		if eitherOf.MatchString(cond) {
			continue
		}
		var keys []string
		for _, n := range names {
			if !strings.Contains(cond, n.name) || s.To.Key != nil && *s.To.Key == n.key {
				continue
			}
			keys = append(keys, n.key)
			cond = strings.ReplaceAll(cond, n.name, "\x00")
		}
		if !looseJoin.MatchString(cond) {
			s.RequiresEndings = keys
		}
	}
}

func foldName(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, norm.NFKC.String(s)))
}

func stripRepeated(re *regexp.Regexp, s string) string {
	s = strings.TrimSpace(s)
	for {
		t := strings.TrimSpace(re.ReplaceAllString(s, ""))
		if t == s || t == "" {
			return s
		}
		s = t
	}
}

func FillNil(c *Chart) {
	c.Characters = orEmpty(c.Characters)
	c.Sections = orEmpty(c.Sections)
	c.Entries = orEmpty(c.Entries)
	c.Saves = orEmpty(c.Saves)
	c.Endings = orEmpty(c.Endings)
	c.Segments = orEmpty(c.Segments)
	for i := range c.Characters {
		c.Characters[i].Aliases = orEmpty(c.Characters[i].Aliases)
	}
	for i := range c.Sections {
		c.Sections[i].Characters = orEmpty(c.Sections[i].Characters)
	}
	for i := range c.Endings {
		c.Endings[i].Characters = orEmpty(c.Endings[i].Characters)
	}
	for i := range c.Segments {
		s := &c.Segments[i]
		s.RequiresEndings = orEmpty(s.RequiresEndings)
		s.Steps = orEmpty(s.Steps)
		for j := range s.Steps {
			s.Steps[j].Alt = orEmpty(s.Steps[j].Alt)
			s.Steps[j].AltZh = orEmpty(s.Steps[j].AltZh)
		}
	}
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func Validate(c Chart) []string {
	var problems []string
	keys := func(n int, key func(int) string) map[string]bool {
		m := make(map[string]bool, n)
		for i := range n {
			m[key(i)] = true
		}
		return m
	}
	chars := keys(len(c.Characters), func(i int) string { return c.Characters[i].Key })
	sections := keys(len(c.Sections), func(i int) string { return c.Sections[i].Key })
	entries := keys(len(c.Entries), func(i int) string { return c.Entries[i].Key })
	saves := keys(len(c.Saves), func(i int) string { return c.Saves[i].Key })
	endings := keys(len(c.Endings), func(i int) string { return c.Endings[i].Key })

	if len(c.Segments) == 0 {
		problems = append(problems, "no segments")
	}
	for _, e := range c.Endings {
		if !slices.Contains(EndingTypes, e.EndingType) {
			problems = append(problems, fmt.Sprintf("ending %s: ending_type %q", e.Key, e.EndingType))
		}
		for _, k := range e.Characters {
			if !chars[k] {
				problems = append(problems, fmt.Sprintf("ending %s: character %s", e.Key, k))
			}
		}
	}
	for _, s := range c.Sections {
		for _, k := range s.Characters {
			if !chars[k] {
				problems = append(problems, fmt.Sprintf("section %s: character %s", s.Key, k))
			}
		}
	}
	for _, e := range c.Entries {
		if e.UnlockedBy != nil && !endings[*e.UnlockedBy] {
			problems = append(problems, fmt.Sprintf("entry %s: unlocked_by %s", e.Key, *e.UnlockedBy))
		}
	}
	ref := func(where string, n Node, allowed ...string) {
		if !slices.Contains(allowed, n.NodeType) {
			problems = append(problems, fmt.Sprintf("%s: node_type %q", where, n.NodeType))
			return
		}
		pool := map[string]map[string]bool{NodeEntry: entries, NodeSave: saves, NodeEnding: endings}[n.NodeType]
		if pool == nil {
			return
		}
		if n.Key == nil || !pool[*n.Key] {
			problems = append(problems, fmt.Sprintf("%s: %s key %v", where, n.NodeType, deref(n.Key)))
		}
	}
	for _, s := range c.Segments {
		if s.Section != nil && !sections[*s.Section] {
			problems = append(problems, fmt.Sprintf("segment %s: section %s", s.Key, *s.Section))
		}
		ref("segment "+s.Key+" from", s.From, NodeEntry, NodeSave, NodeEnding)
		ref("segment "+s.Key+" to", s.To, NodeEnding, NodeCollect, NodeOpen)
		for _, k := range s.RequiresEndings {
			if !endings[k] {
				problems = append(problems, fmt.Sprintf("segment %s: requires ending %s", s.Key, k))
			}
		}
		for _, st := range s.Steps {
			switch st.StepType {
			case StepSave:
				if st.Save == nil || !saves[*st.Save] {
					problems = append(problems, fmt.Sprintf("segment %s: save step %v", s.Key, deref(st.Save)))
				}
			case StepChoice, StepAction, StepHeading, StepNote:
				if st.Text == nil || strings.TrimSpace(*st.Text) == "" {
					problems = append(problems, fmt.Sprintf("segment %s: empty %s", s.Key, st.StepType))
				}
			default:
				problems = append(problems, fmt.Sprintf("segment %s: step_type %q", s.Key, st.StepType))
			}
		}
	}
	return problems
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
