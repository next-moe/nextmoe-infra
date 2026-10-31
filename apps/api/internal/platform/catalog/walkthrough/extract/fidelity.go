package extract

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/walkthrough"

	"golang.org/x/text/unicode/norm"
)

// The model drops what it does not understand without saying so: walkthrough
// 3046 lost every 推理正解 / 不正解, its ■N話 headings, the ★ marks' legend,
// and cut 二人's route at ■４話 into a stub and a second run from START. Every
// repair here re-reads the source lines the chart was aligned to, so what it
// adds is verbatim and nothing is invented.

var (
	markerWord  = regexp.MustCompile(`(?i)(セーブ|START|はじめから|から開始|END|エンド|回収|終了)`)
	headingMark = regexp.MustCompile(`^[■□▼▽◆◇●]`)
	barTail     = regexp.MustCompile(`[━─]{2,}$`)
	barOnly     = regexp.MustCompile(`^[━─―ー\-=＝_]+$`)
	chapterHead = regexp.MustCompile(`(?i)^[■□▼▽◆◇●]?\s*(第?[0-9一二三四五六七八九十百]+\s*(話|章|部|幕|日目|週目)|episode|chapter|prologue|epilogue|プロローグ|エピローグ)`)
	timeHead    = regexp.MustCompile(`(?i)^[■□▼▽◆◇●○]?\s*[『「【《〈(]?\s*(` +
		`第?[0-9一二三四五六七八九十百]+\s*(話|章|部|幕)|最終(話|章)|final\s*chapter|序章|終章|` +
		`day\s*[0-9]+|[0-9]+(st|nd|rd|th)\s*day|[0-9]{1,2}日|[月火水木金土日]曜日?|` +
		`第?[0-9一二三四五六七八九十]+(週|周)(目)?\s*(第?[0-9一二三四五六七八九十]+日目)?|` +
		`残り\s*[0-9]+\s*(日|回|週|ターン)|ターン\s*[0-9]+|(am|pm|午前|午後)\s*[0-9]{1,2}[:：時][0-9]{0,2}分?` +
		`)\s*[』」】》〉)]?\s*[━─]*$`)

	verdictRemark = regexp.MustCompile(`^[(\[【]?((推理)?(正解|不正解)|成功|失敗|[○◯×✕◎△]+)[)\]】]?$`)
	effectItem    = `(?:[\p{L}\p{N}ー々・]{0,16}?\+[0-9]+|[\p{L}ー々・]*[\p{Han}\p{Hiragana}\p{Katakana}][\p{L}ー々・]*[\-−][0-9]+)`
	effectRemark  = regexp.MustCompile(`^[(\[【]?` + effectItem + `([&、,・/]?` + effectItem + `)*[)\]】]?$`)
	trailingSplit = regexp.MustCompile(`^(.+?)[\s\x{3000}:：→]+([^\s\x{3000}→]+)$`)

	markRun     = `(?:[(（]\s*[)）]|【\s*】|「\s*」|\[[^\]]{0,8}\]|[★☆◆◇●○◎■□▲△▼▽♪♥♡※⇒→])+`
	legendLine  = regexp.MustCompile(`^(` + markRun + `)\s*(?:の(?:付いた|ついた|ついている)?(?:選択肢|場所|箇所)|マーク|印)?\s*(?:は|が|：|:)`)
	legendMarks = regexp.MustCompile(`[(（]\s*[)）]|【\s*】|「\s*」|\[[^\]]{0,8}\]|[★☆◆◇●○◎■□▲△▼▽♪♥♡※⇒→]`)
	achieveWord = regexp.MustCompile(`(?i)(実績|トロフィー|アチーブ|achievement|trophy)`)
	textMarks   = "★☆⇒"

	listSplit  = regexp.MustCompile(`[、,，/／&＆＋+]`)
	endingTail = regexp.MustCompile(`(?i)\s*(END|ＥＮＤ|エンド|ルート)\s*[0-9０-９]*.*$`)
	sharedEnd  = regexp.MustCompile(`(?i)(BAD|バッド|ＢＡＤ|TRUE|トゥルー|ＴＲＵＥ|NORMAL|ノーマル|ＮＯＲＭＡＬ|HAPPY|ハッピー|ハーレム|GOOD|グッド)`)
)

func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

type sourceLines struct {
	raw, folded []string
}

func newSourceLines(text string) sourceLines {
	var s sourceLines
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "|"))
		s.raw = append(s.raw, l)
		s.folded = append(s.folded, fold(l))
	}
	return s
}

func (s sourceLines) find(t string, from int) int {
	for i := max(from, 0); i < len(s.folded); i++ {
		if strings.Contains(s.folded[i], t) {
			return i
		}
	}
	return -1
}

func (s sourceLines) isHeading(i int) bool {
	t := strings.TrimSpace(norm.NFKC.String(s.raw[i]))
	if t == "" || utf8.RuneCountInString(t) > 40 || markerWord.MatchString(t) {
		return false
	}
	return walkthrough.IsHeading(t) || chapterHead.MatchString(t) || timeHead.MatchString(t) || headingMark.MatchString(t) && barTail.MatchString(t)
}

func (s sourceLines) isFiller(i int) bool {
	t := strings.TrimSpace(norm.NFKC.String(s.raw[i]))
	return t == "" || barOnly.MatchString(fold(t)) || strings.HasPrefix(t, "※") || s.isHeading(i)
}

// Steps are searched in chart order from the last hit, so a line repeated
// across routes (both columns of 3046 ask 大丈夫か？) resolves in its own route.
func stepLines(s sourceLines, c *walkthrough.Chart) [][]int {
	at := make([][]int, len(c.Segments))
	cursor := 0
	saveLabel := map[string]string{}
	for _, sv := range c.Saves {
		saveLabel[sv.Key] = fold(sv.Label)
	}
	for i, seg := range c.Segments {
		at[i] = make([]int, len(seg.Steps))
		for j, st := range seg.Steps {
			at[i][j] = -1
			var t string
			switch {
			case st.Text != nil:
				t = fold(*st.Text)
			case st.StepType == walkthrough.StepSave && st.Save != nil:
				t = saveLabel[*st.Save]
			}
			if t == "" {
				continue
			}
			k := s.find(t, cursor)
			if k < 0 {
				k = s.find(t, 0)
			}
			if k >= 0 {
				at[i][j], cursor = k, k
			}
		}
	}
	return at
}

func lastHit(at []int) int {
	for j := len(at) - 1; j >= 0; j-- {
		if at[j] >= 0 {
			return at[j]
		}
	}
	return -1
}

func firstHit(at []int) int {
	for _, k := range at {
		if k >= 0 {
			return k
		}
	}
	return -1
}

func RepairFromSource(text string, c *walkthrough.Chart) {
	s := newSourceLines(text)
	dropRules(c)
	at := joinCutRoutes(s, c, stepLines(s, c))
	restoreRemarks(s, c, at)
	restoreHeadings(s, c, at)
	c.Legend = legendOf(s, c, at)
	markAchievements(c)
	splitMergedCharacters(c)
	nameEndingCharacters(c)
}

func dropRules(c *walkthrough.Chart) {
	for i := range c.Segments {
		c.Segments[i].Steps = slices.DeleteFunc(c.Segments[i].Steps, func(st walkthrough.Step) bool {
			return st.Text != nil && len(st.Alt) == 0 && barOnly.MatchString(fold(norm.NFKC.String(*st.Text)))
		})
	}
}

func joinCutRoutes(s sourceLines, c *walkthrough.Chart, at [][]int) [][]int {
	for i := 0; i+1 < len(c.Segments); {
		a, b := &c.Segments[i], c.Segments[i+1]
		p, q := lastHit(at[i]), firstHit(at[i+1])
		joined := a.To.NodeType == walkthrough.NodeOpen && b.From.NodeType == walkthrough.NodeEntry &&
			b.Condition == nil && sameSection(a.Section, b.Section) && p >= 0 && q > p
		for k := p + 1; joined && k < q; k++ {
			joined = s.isFiller(k)
		}
		if !joined {
			i++
			continue
		}
		a.Steps = append(a.Steps, b.Steps...)
		a.To = b.To
		at[i] = append(at[i], at[i+1]...)
		c.Segments = slices.Delete(c.Segments, i+1, i+2)
		at = slices.Delete(at, i+1, i+2)
	}
	return at
}

func sameSection(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func remarkLike(s string) bool {
	t := fold(norm.NFKC.String(s))
	return t != "" && (verdictRemark.MatchString(t) || effectRemark.MatchString(t))
}

// A line shared by several steps is left alone: what trails it belongs to the
// last of them, or to none.
func restoreRemarks(s sourceLines, c *walkthrough.Chart, at [][]int) {
	shared := map[int]int{}
	for _, seg := range at {
		for _, k := range seg {
			if k >= 0 {
				shared[k]++
			}
		}
	}
	for i := range c.Segments {
		for j := range c.Segments[i].Steps {
			st := &c.Segments[i].Steps[j]
			if st.Text == nil || st.StepType != walkthrough.StepChoice && st.StepType != walkthrough.StepAction {
				continue
			}
			if m := trailingSplit.FindStringSubmatch(strings.TrimSpace(*st.Text)); m != nil && st.Remark == nil && remarkLike(m[2]) {
				head, tail := strings.TrimSpace(m[1]), m[2]
				st.Text, st.Remark = &head, &tail
			}
			k := at[i][j]
			if k < 0 || shared[k] > 1 {
				continue
			}
			line, t := s.folded[k], fold(*st.Text)
			idx := strings.Index(line, t)
			if idx < 0 {
				continue
			}
			before, after := line[:idx], line[idx+len(t):]
			if mark := strings.Trim(before, "|│"); mark != "" && strings.Trim(mark, textMarks) == "" {
				text := mark + *st.Text
				st.Text = &text
			}
			for _, a := range st.Alt {
				fa := fold(a)
				for _, w := range []string{"(" + fa + ")", "（" + fa + "）"} {
					after = strings.Replace(after, w, "", 1)
				}
			}
			after = strings.TrimLeft(strings.Trim(after, "|│"), "」』\":：、,・/／")
			if st.Remark == nil && remarkLike(after) {
				st.Remark = &after
			}
		}
	}
}

func restoreHeadings(s sourceLines, c *walkthrough.Chart, at [][]int) {
	used := map[int]bool{}
	for _, seg := range at {
		for _, k := range seg {
			used[k] = true
		}
	}
	heading := func(k int) walkthrough.Step {
		t := s.raw[k]
		return walkthrough.Step{StepType: walkthrough.StepHeading, Text: &t, Alt: []string{}, AltZh: []*string{}}
	}
	for i := range c.Segments {
		seg := &c.Segments[i]
		var steps []walkthrough.Step
		prev := -1
		for j, st := range seg.Steps {
			k := at[i][j]
			switch {
			case k < 0:
			case prev < 0 && j == 0:
				var above []walkthrough.Step
				for h := k - 1; h >= 0 && !used[h] && s.isFiller(h); h-- {
					if s.isHeading(h) {
						above = append([]walkthrough.Step{heading(h)}, above...)
					}
				}
				steps = append(steps, above...)
			case prev >= 0 && k > prev+1:
				for h := prev + 1; h < k; h++ {
					if !used[h] && s.isHeading(h) {
						steps = append(steps, heading(h))
					}
				}
			}
			if k >= 0 {
				prev = k
			}
			steps = append(steps, st)
		}
		seg.Steps = steps
	}
}

// Only sentences defining a mark the chart uses are kept: the page's other
// prose is never republished.
func legendOf(s sourceLines, c *walkthrough.Chart, at [][]int) []walkthrough.Legend {
	used := map[int]bool{}
	for _, seg := range at {
		for _, k := range seg {
			used[k] = true
		}
	}
	var marked strings.Builder
	hasAlt := false
	for _, seg := range c.Segments {
		for _, st := range seg.Steps {
			if st.Text != nil {
				marked.WriteString(*st.Text)
			}
			hasAlt = hasAlt || len(st.Alt) > 0
		}
	}
	out := []walkthrough.Legend{}
	seen := map[string]bool{}
	for k, line := range s.raw {
		if used[k] {
			continue
		}
		for _, sentence := range strings.SplitAfter(line, "。") {
			sentence = strings.TrimSpace(sentence)
			m := legendLine.FindStringSubmatch(norm.NFKC.String(sentence))
			if m == nil || utf8.RuneCountInString(sentence) > 120 || seen[fold(sentence)] {
				continue
			}
			relevant := false
			for _, mark := range legendMarks.FindAllString(m[1], -1) {
				switch mark = fold(mark); {
				case mark == "()" || mark == "（）":
					relevant = relevant || hasAlt
				default:
					relevant = relevant || strings.Contains(marked.String(), mark)
				}
			}
			if relevant && len(out) < 16 {
				seen[fold(sentence)] = true
				out = append(out, walkthrough.Legend{Text: sentence})
			}
		}
	}
	return out
}

// markAchievements overrides the model's achievement flag: ★ and ☆ mean what
// each page's legend says (an extra choice, a deduction answer, an H event),
// and the flag had marked 4,627 ★ steps that no legend tied to an achievement.
func markAchievements(c *walkthrough.Chart) {
	var marks []string
	for _, l := range c.Legend {
		if m := legendLine.FindStringSubmatch(norm.NFKC.String(l.Text)); m != nil && achieveWord.MatchString(l.Text) {
			for _, mark := range legendMarks.FindAllString(m[1], -1) {
				if mark = fold(mark); mark != "()" && mark != "（）" {
					marks = append(marks, mark)
				}
			}
		}
	}
	for i := range c.Segments {
		for j := range c.Segments[i].Steps {
			st := &c.Segments[i].Steps[j]
			if st.StepType != walkthrough.StepChoice && st.StepType != walkthrough.StepAction {
				st.Achievement = false
				continue
			}
			text := deref(st.Text) + deref(st.Remark)
			st.Achievement = achieveWord.MatchString(text) || slices.ContainsFunc(marks, func(m string) bool {
				return strings.HasPrefix(strings.TrimSpace(deref(st.Text)), m)
			})
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nameParts(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || r == '・' })
}

// splitMergedCharacters undoes the model filing two people as one: the
// heading アーデルハイト、九折坂 二人 became one character with 二人 as an
// alias, so 二人's ending pinned アーデルハイト and her route read as 二人's.
func splitMergedCharacters(c *walkthrough.Chart) {
	owner := func(name string, self int) int {
		f := fold(name)
		for i, ch := range c.Characters {
			if i != self && fold(ch.Name) == f {
				return i
			}
		}
		return -1
	}
	for i := range c.Characters {
		ch := &c.Characters[i]
		ch.Aliases = slices.DeleteFunc(ch.Aliases, func(a string) bool {
			if owner(a, i) >= 0 {
				return true
			}
			parts := nameParts(a)
			if len(parts) < 2 || slices.ContainsFunc(parts, func(p string) bool { return fold(p) == fold(ch.Name) }) {
				return false
			}
			for _, p := range parts {
				if o := owner(p, i); o >= 0 {
					c.Characters[o].Aliases = append(c.Characters[o].Aliases, a)
					return true
				}
			}
			return false
		})
	}

	keyOf := func(item string) (int, bool) {
		f := fold(item)
		for i, ch := range c.Characters {
			if fold(ch.Name) == f {
				return i, false
			}
		}
		for i, ch := range c.Characters {
			if slices.ContainsFunc(ch.Aliases, func(a string) bool { return fold(a) == f }) {
				return i, true
			}
		}
		return -1, false
	}
	for si := range c.Sections {
		sec := &c.Sections[si]
		items := listSplit.Split(sec.Title, -1)
		if len(items) < 2 {
			continue
		}
		var keys []string
		resolved := true
		for _, item := range items {
			item = strings.TrimSpace(item)
			i, viaAlias := keyOf(item)
			if i < 0 {
				resolved = false
				continue
			}
			if viaAlias && slices.ContainsFunc(items, func(o string) bool { return fold(o) == fold(c.Characters[i].Name) }) {
				c.Characters[i].Aliases = slices.DeleteFunc(c.Characters[i].Aliases, func(a string) bool { return fold(a) == fold(item) })
				c.Characters = append(c.Characters, walkthrough.Character{Key: freeKey(c), Name: item, Aliases: []string{}})
				i = len(c.Characters) - 1
			}
			if k := c.Characters[i].Key; !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
		if resolved {
			sec.Characters = keys
		}
	}
}

func freeKey(c *walkthrough.Chart) string {
	for n := len(c.Characters) + 1; ; n++ {
		k := fmt.Sprintf("c%d", n)
		if !slices.ContainsFunc(c.Characters, func(ch walkthrough.Character) bool { return ch.Key == k }) {
			return k
		}
	}
}

func nameEndingCharacters(c *walkthrough.Chart) {
	for i := range c.Endings {
		e := &c.Endings[i]
		if len(e.Characters) > 0 || sharedEnd.MatchString(e.Name) {
			continue
		}
		core := fold(endingTail.ReplaceAllString(e.Name, ""))
		if core == "" {
			continue
		}
		var hits []string
		for _, ch := range c.Characters {
			names := append([]string{ch.Name}, ch.Aliases...)
			for _, n := range slices.Clone(names) {
				for _, p := range nameParts(n) {
					if utf8.RuneCountInString(p) >= 2 {
						names = append(names, p)
					}
				}
			}
			if slices.ContainsFunc(names, func(n string) bool { return fold(n) == core }) && !slices.Contains(hits, ch.Key) {
				hits = append(hits, ch.Key)
			}
		}
		if len(hits) == 1 {
			e.Characters = hits
		}
	}
}
