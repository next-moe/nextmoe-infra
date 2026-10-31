package extract

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/walkthrough"

	"golang.org/x/text/unicode/norm"
)

// PartRunes is the page length above which a page is extracted in parts. Whole
// pages of 10k+ runes took grok 10–15 minutes and most ended in a timeout or
// cursor's "Agent Looping Detected" (2026-10-31: 46 of 312 calls finished).
const PartRunes = 5000

// SplitPage cuts a page into parts of about PartRunes, preferring the line
// after an ending, then a heading or a route start, so that a part rarely
// begins inside a route.
func SplitPage(text string) []string {
	if utf8.RuneCountInString(text) <= PartRunes {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	var parts []string
	start, runes, fallback := 0, 0, -1
	for i, l := range lines {
		n := utf8.RuneCountInString(l) + 1
		if i > start && runes >= PartRunes/2 {
			t := strings.TrimSpace(l)
			prev := strings.TrimSpace(lines[i-1])
			switch {
			case endMarker.MatchString(prev):
				parts, start, runes, fallback = append(parts, strings.Join(lines[start:i], "\n")), i, 0, -1
			case fallback < 0 && (chartStart.MatchString(t) || isHeadingLine(t) || headingMark.MatchString(t)):
				fallback = i
			}
		}
		if runes > PartRunes*3/2 && i > start {
			cut := i
			if fallback > start {
				cut = fallback
			}
			parts = append(parts, strings.Join(lines[start:cut], "\n"))
			start, runes, fallback = cut, 0, -1
			for _, l := range lines[cut:i] {
				runes += utf8.RuneCountInString(l) + 1
			}
		}
		runes += n
	}
	return append(parts, strings.Join(lines[start:], "\n"))
}

// PartPrompt is the user message for part k (1-based) of n, given what the
// earlier parts produced.
func PartPrompt(part string, k, n int, so Extracted) string {
	if n == 1 {
		return part
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This is part %d of %d of one walkthrough page. Extract only this part.\n", k, n)
	if k > 1 {
		b.WriteString("Earlier parts already defined the items below. Refer to them by these keys and do not list them again:\n")
		for _, c := range so.Characters {
			fmt.Fprintf(&b, "- character %s = %s\n", c.Key, c.Name)
		}
		for _, s := range so.Sections {
			fmt.Fprintf(&b, "- section %s = %s\n", s.Key, s.Title)
		}
		for _, e := range so.Entries {
			fmt.Fprintf(&b, "- entry %s = %s\n", e.Key, e.Label)
		}
		for _, s := range so.Saves {
			fmt.Fprintf(&b, "- save %s = %s\n", s.Key, s.Label)
		}
		for _, s := range so.Segments {
			if s.To.Ending != nil {
				fmt.Fprintf(&b, "- segment %s reaches the ending %s\n", s.Key, s.To.Ending.Name)
			}
		}
		fmt.Fprintf(&b, "Start every key you define in this part with p%d_.\n", k)
	}
	b.WriteString("\n")
	b.WriteString(part)
	return b.String()
}

// Merge appends a part's extraction to the page's. A character, section, entry
// or save the part defines again (same name, title or label) becomes the one
// already there; a key the part does not define refers to an earlier part.
func Merge(so *Extracted, part Extracted, k int) {
	if part.NoChart {
		return
	}
	so.NoChart = false
	pfx := fmt.Sprintf("p%d_", k)
	rename := func(kind, key string) string {
		if k == 1 {
			return key
		}
		return pfx + kind + "_" + strings.TrimPrefix(key, pfx)
	}
	// deepseek defined p2_s7 and then filed segments under s7, and listed an
	// ending's characters as p2_c2 for a character it keyed c2; both dangled.
	resolver := func(own map[string]string, earlier map[string]bool) func(*string) *string {
		return func(key *string) *string {
			if key == nil {
				return nil
			}
			for _, k := range []string{*key, pfx + *key, strings.TrimPrefix(*key, pfx)} {
				if v, ok := own[k]; ok {
					return &v
				}
				if earlier[k] {
					return &k
				}
			}
			return key
		}
	}
	keysOf := func(n int, key func(int) string) map[string]bool {
		out := make(map[string]bool, n)
		for i := range n {
			out[key(i)] = true
		}
		return out
	}

	chars := map[string]string{}
	charRef := resolver(chars, keysOf(len(so.Characters), func(i int) string { return so.Characters[i].Key }))
	for _, c := range part.Characters {
		if i := findCharacter(so.Characters, c); i >= 0 {
			chars[c.Key] = so.Characters[i].Key
			for _, a := range append([]string{c.Name}, c.Aliases...) {
				if squash(a) != squash(so.Characters[i].Name) && !containsSquashed(so.Characters[i].Aliases, a) {
					so.Characters[i].Aliases = append(so.Characters[i].Aliases, a)
				}
			}
			continue
		}
		chars[c.Key] = rename("c", c.Key)
		c.Key = chars[c.Key]
		so.Characters = append(so.Characters, c)
	}
	charRefs := func(refs []string) []string {
		out := make([]string, 0, len(refs))
		for _, r := range refs {
			out = append(out, *charRef(&r))
		}
		return out
	}

	sections := map[string]string{}
	sectionRef := resolver(sections, keysOf(len(so.Sections), func(i int) string { return so.Sections[i].Key }))
	for _, s := range part.Sections {
		if i := slices.IndexFunc(so.Sections, func(h walkthrough.Section) bool { return squash(h.Title) == squash(s.Title) }); i >= 0 {
			sections[s.Key] = so.Sections[i].Key
			continue
		}
		sections[s.Key] = rename("s", s.Key)
		s.Key, s.Characters = sections[s.Key], charRefs(s.Characters)
		so.Sections = append(so.Sections, s)
	}

	segments := map[string]string{}
	segmentRef := resolver(segments, keysOf(len(so.Segments), func(i int) string { return so.Segments[i].Key }))
	for _, s := range part.Segments {
		segments[s.Key] = rename("g", s.Key)
	}

	entries := map[string]string{}
	entryRef := resolver(entries, keysOf(len(so.Entries), func(i int) string { return so.Entries[i].Key }))
	for _, e := range part.Entries {
		if i := slices.IndexFunc(so.Entries, func(h walkthrough.Entry) bool { return labelKey(h.Label) == labelKey(e.Label) }); i >= 0 {
			entries[e.Key] = so.Entries[i].Key
			continue
		}
		entries[e.Key] = rename("n", e.Key)
		e.Key, e.UnlockedBy = entries[e.Key], segmentRef(e.UnlockedBy)
		so.Entries = append(so.Entries, e)
	}

	saves := map[string]string{}
	saveRef := resolver(saves, keysOf(len(so.Saves), func(i int) string { return so.Saves[i].Key }))
	for _, s := range part.Saves {
		if i := slices.IndexFunc(so.Saves, func(h walkthrough.Save) bool { return saveLabel(h.Label) == saveLabel(s.Label) }); i >= 0 {
			saves[s.Key] = so.Saves[i].Key
			continue
		}
		saves[s.Key] = rename("v", s.Key)
		s.Key = saves[s.Key]
		so.Saves = append(so.Saves, s)
	}

	for _, s := range part.Segments {
		s.Key = segments[s.Key]
		s.Section = sectionRef(s.Section)
		switch s.From.NodeType {
		case walkthrough.NodeEntry:
			s.From.Key = entryRef(s.From.Key)
		case walkthrough.NodeSave:
			s.From.Key = saveRef(s.From.Key)
		case walkthrough.NodeEnding:
			s.From.Key = segmentRef(s.From.Key)
		}
		for i := range s.Steps {
			s.Steps[i].Save = saveRef(s.Steps[i].Save)
		}
		if s.To.Ending != nil {
			s.To.Ending.Characters = charRefs(s.To.Ending.Characters)
		}
		so.Segments = append(so.Segments, s)
	}
}

func findCharacter(have []walkthrough.Character, c walkthrough.Character) int {
	for i, h := range have {
		if squash(h.Name) == squash(c.Name) || containsSquashed(h.Aliases, c.Name) || containsSquashed(c.Aliases, h.Name) {
			return i
		}
	}
	return -1
}

func containsSquashed(list []string, s string) bool {
	for _, x := range list {
		if squash(x) == squash(s) {
			return true
		}
	}
	return false
}

func labelKey(s string) string {
	return strings.ToUpper(squash(norm.NFKC.String(strings.Trim(strings.TrimSpace(s), "【】[]"))))
}
