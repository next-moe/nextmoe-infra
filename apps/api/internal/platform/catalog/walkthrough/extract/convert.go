package extract

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"api/internal/platform/catalog/walkthrough"

	"golang.org/x/text/unicode/norm"
)

// The extraction schema has no top-level endings array: with one, the model
// enumerated "ＢＡＤ ＥＮＤ" before writing any segment and never stopped
// (entry 10990, 24000 tokens, keys up to e834). An ending inline in the segment
// that reaches it is one per occurrence by construction.
type Extracted struct {
	NoChart    bool                    `json:"no_chart"`
	Characters []walkthrough.Character `json:"characters"`
	Sections   []walkthrough.Section   `json:"sections"`
	Entries    []walkthrough.Entry     `json:"entries"`
	Saves      []walkthrough.Save      `json:"saves"`
	Segments   []extractedSegment      `json:"segments"`
}

type extractedSegment struct {
	Key       string             `json:"key"`
	Section   *string            `json:"section"`
	From      walkthrough.Node   `json:"from"`
	Condition *string            `json:"condition"`
	Steps     []walkthrough.Step `json:"steps"`
	To        struct {
		NodeType string `json:"node_type"`
		Ending   *struct {
			Name       string   `json:"name"`
			EndingType string   `json:"ending_type"`
			Characters []string `json:"characters"`
		} `json:"ending"`
	} `json:"to"`
}

func (x Extracted) Chart() walkthrough.Chart {
	c := walkthrough.Chart{
		Characters: x.Characters,
		Sections:   x.Sections,
		Entries:    x.Entries,
		Saves:      x.Saves,
	}
	endingOf := map[string]string{}
	for _, s := range x.Segments {
		if s.To.NodeType == walkthrough.NodeEnding && s.To.Ending != nil {
			key := fmt.Sprintf("e%d", len(c.Endings)+1)
			endingOf[s.Key] = key
			c.Endings = append(c.Endings, walkthrough.Ending{
				Key:        key,
				Name:       s.To.Ending.Name,
				EndingType: s.To.Ending.EndingType,
				Characters: s.To.Ending.Characters,
			})
		}
	}
	bySegment := func(k *string) *string {
		if k == nil {
			return nil
		}
		if e, ok := endingOf[*k]; ok {
			return &e
		}
		return k
	}
	for i := range c.Entries {
		c.Entries[i].UnlockedBy = bySegment(c.Entries[i].UnlockedBy)
	}
	for _, s := range x.Segments {
		seg := walkthrough.Segment{
			Key:       s.Key,
			Section:   s.Section,
			From:      s.From,
			Condition: s.Condition,
			Steps:     slices.DeleteFunc(slices.Clone(s.Steps), emptyStep),
			To:        walkthrough.Node{NodeType: s.To.NodeType},
		}
		if seg.From.NodeType == walkthrough.NodeEnding {
			seg.From.Key = bySegment(seg.From.Key)
		}
		if k, ok := endingOf[s.Key]; ok {
			seg.To.Key = &k
		} else if seg.To.NodeType == walkthrough.NodeEnding {
			seg.To.NodeType = walkthrough.NodeOpen
		}
		c.Segments = append(c.Segments, seg)
	}
	repairSaves(&c)
	repairCharacterRefs(&c)
	repairEntries(&c)
	return c
}

func emptyStep(st walkthrough.Step) bool {
	return st.StepType != walkthrough.StepSave && (st.Text == nil || strings.TrimSpace(*st.Text) == "") && len(st.Alt) == 0
}

var saveDecor = regexp.MustCompile(`^[◆◇■【\[]+|[】\]]+$|[（(][^）)]*[）)]$|から$`)

func saveLabel(s string) string {
	s = strings.TrimSpace(s)
	for {
		t := strings.TrimSpace(saveDecor.ReplaceAllString(s, ""))
		if t == s {
			return norm.NFKC.String(t)
		}
		s = t
	}
}

func repairSaves(c *walkthrough.Chart) {
	byKey := map[string]bool{}
	byLabel := map[string]string{}
	for _, s := range c.Saves {
		byKey[s.Key] = true
		byLabel[saveLabel(s.Label)] = s.Key
	}
	resolve := func(ref string) *string {
		if byKey[ref] {
			return &ref
		}
		label := saveLabel(ref)
		if label == "" {
			return nil
		}
		if k, ok := byLabel[label]; ok {
			return &k
		}
		k := fmt.Sprintf("sv%02d", len(c.Saves)+1)
		for byKey[k] {
			k += "x"
		}
		c.Saves = append(c.Saves, walkthrough.Save{Key: k, Label: strings.Trim(strings.TrimSpace(ref), "◆【】")})
		byKey[k], byLabel[label] = true, k
		return &k
	}
	for i := range c.Segments {
		s := &c.Segments[i]
		if s.From.NodeType == walkthrough.NodeSave && s.From.Key != nil {
			s.From.Key = resolve(*s.From.Key)
		}
		for j := range s.Steps {
			st := &s.Steps[j]
			if st.StepType != walkthrough.StepSave {
				continue
			}
			switch {
			case st.Save != nil:
				st.Save = resolve(*st.Save)
			case st.Text != nil:
				st.Save = resolve(*st.Text)
			}
			st.Text = nil
		}
		s.Steps = slices.DeleteFunc(s.Steps, func(st walkthrough.Step) bool {
			return st.StepType == walkthrough.StepSave && st.Save == nil
		})
	}
}

func repairCharacterRefs(c *walkthrough.Chart) {
	keyOf := map[string]string{}
	for _, ch := range c.Characters {
		keyOf[ch.Key] = ch.Key
		keyOf[squash(ch.Name)] = ch.Key
		for _, a := range ch.Aliases {
			if _, taken := keyOf[squash(a)]; !taken {
				keyOf[squash(a)] = ch.Key
			}
		}
	}
	resolve := func(refs []string) []string {
		out := make([]string, 0, len(refs))
		for _, r := range refs {
			k, ok := keyOf[r]
			if !ok {
				k, ok = keyOf[squash(r)]
			}
			if !ok {
				k = fmt.Sprintf("c%d", len(c.Characters)+1)
				for keyOf[k] != "" {
					k += "x"
				}
				c.Characters = append(c.Characters, walkthrough.Character{Key: k, Name: r, Aliases: []string{}})
				keyOf[k], keyOf[squash(r)] = k, k
			}
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
		return out
	}
	for i := range c.Sections {
		c.Sections[i].Characters = resolve(c.Sections[i].Characters)
	}
	for i := range c.Endings {
		c.Endings[i].Characters = resolve(c.Endings[i].Characters)
	}
}

func repairEntries(c *walkthrough.Chart) {
	endings := map[string]bool{}
	for _, e := range c.Endings {
		endings[e.Key] = true
	}
	entries := map[string]bool{}
	for i, e := range c.Entries {
		entries[e.Key] = true
		if e.UnlockedBy != nil && !endings[*e.UnlockedBy] {
			c.Entries[i].UnlockedBy = nil
		}
	}
	for _, s := range c.Segments {
		if s.From.NodeType == walkthrough.NodeEntry && s.From.Key != nil && !entries[*s.From.Key] {
			c.Entries = append(c.Entries, walkthrough.Entry{Key: *s.From.Key, Label: strings.ToUpper(*s.From.Key)})
			entries[*s.From.Key] = true
		}
	}
}
