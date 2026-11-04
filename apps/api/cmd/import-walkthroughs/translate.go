package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/llmsuggest"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"

	"gorm.io/gorm"
)

// A chunk is capped by length as well as count: 80 long strings took the
// cursor gateway up to 620s, past the client's 180s timeout, and 19 charts
// failed after every retry.
const (
	translateChunk      = 80
	translateChunkRunes = 1500
)

type glossaryTerm struct {
	Ja string `json:"ja"`
	Zh string `json:"zh"`
}

type translateJob struct {
	EntryID   int64
	Strings   []string
	Glossary  []glossaryTerm
	KeepNames []string
	Known     map[string]string
}

func runTranslate(ctx context.Context, src, cat *gorm.DB, l *llm, entries []sourceEntry, o extractOpts) error {
	charts, err := gatedCharts(ctx, src, entryIDs(entries))
	if err != nil {
		return err
	}
	pins, err := loadMatches(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	works, err := loadWorkMatches(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	rosters, err := rosterNames(ctx, cat, slices.Collect(maps.Values(works)))
	if err != nil {
		return err
	}
	done, err := settledTranslations(ctx, src)
	if err != nil {
		return err
	}
	prev, err := loadTranslations(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	var jobs []translateJob
	for _, id := range keysOf(charts) {
		if _, matched := pins[id]; !matched {
			continue
		}
		job := buildTranslateJob(id, charts[id], pins[id], rosters[works[id]])
		if len(job.Strings) == 0 || !o.Redo && done[id] == translateHash(job) {
			continue
		}
		if !o.Redo {
			job.Known = prev[id].strings
		}
		jobs = append(jobs, job)
	}
	fmt.Printf("\n=== import-walkthroughs translate model=%s prompt=%s charts=%d todo=%d ===\n",
		l.model, translatePromptVersion, len(charts), len(jobs))

	schema := mustSchema(translateSchemaJSON)
	var finished, failed, refused, asked atomic.Int64
	sem := make(chan struct{}, max(o.Workers, 1))
	var wg sync.WaitGroup
	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(job translateJob) {
			defer wg.Done()
			defer func() { <-sem }()
			out, st, err := translateOne(ctx, l, schema, job, o.MaxTokens)
			refused.Add(int64(st.refused))
			asked.Add(int64(st.asked))
			errText := ""
			if err != nil {
				failed.Add(1)
				errText = err.Error()
				slog.Warn("translate failed", "entry", job.EntryID, "err", err)
			}
			if err := storeTranslation(ctx, src, job, translatedBy(prev[job.EntryID].model, l.model, st.asked), out, errText); err != nil {
				slog.Error("store translation", "entry", job.EntryID, "err", err)
			}
			if n := finished.Add(1); n%25 == 0 || n == int64(len(jobs)) {
				slog.Info("progress", "done", n, "of", len(jobs), "failed", failed.Load(), "asked", asked.Load(), "refused", refused.Load())
			}
		}(job)
	}
	wg.Wait()
	fmt.Printf("translated=%d failed=%d asked_strings=%d refused_strings=%d\n",
		finished.Load()-failed.Load(), failed.Load(), asked.Load(), refused.Load())
	return ctx.Err()
}

func loadMatches(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]map[string]characterPin, error) {
	var rows []struct {
		EntryID    int64
		Characters string
	}
	if err := src.WithContext(ctx).Table("wt_stage.match").
		Select("entry_id, characters::text AS characters").
		Where("entry_id IN ? AND work_id IS NOT NULL", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]map[string]characterPin, len(rows))
	for _, r := range rows {
		m := map[string]characterPin{}
		if err := json.Unmarshal([]byte(r.Characters), &m); err != nil {
			return nil, fmt.Errorf("entry %d match: %w", r.EntryID, err)
		}
		out[r.EntryID] = m
	}
	return out, nil
}

type rosterName struct {
	CharacterID int64
	Names       []string
	Zh          string
}

func rosterNames(ctx context.Context, cat *gorm.DB, workIDs []int64) (map[int64][]rosterName, error) {
	out := map[int64][]rosterName{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID      int64
		CharacterID int64
		Name        string
		Zh          string
	}
	if err := cat.WithContext(ctx).Raw(`
SELECT wc.work_id, c.id AS character_id, n.name,
       COALESCE((SELECT a.name FROM catalog_character_alias a
                  WHERE a.character_id = c.id AND a.lang = 'zh-Hans' AND a.provenance = ?
                  ORDER BY a.is_primary_for_locale DESC, a.id LIMIT 1), '') AS zh
FROM catalog_work_character wc
JOIN catalog_character c ON c.id = wc.character_id AND c.deleted_at IS NULL
JOIN LATERAL (
  SELECT c.display_name AS name
  UNION SELECT a.name FROM catalog_character_alias a WHERE a.character_id = c.id AND a.lang IN ('ja', '')
) n ON true
WHERE wc.work_id IN ?
ORDER BY wc.work_id, c.id`, model.AliasProvenanceSource, workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	index := map[[2]int64]int{}
	for _, r := range rows {
		k := [2]int64{r.WorkID, r.CharacterID}
		i, ok := index[k]
		if !ok {
			i = len(out[r.WorkID])
			index[k] = i
			out[r.WorkID] = append(out[r.WorkID], rosterName{CharacterID: r.CharacterID, Zh: r.Zh})
		}
		if n := strings.TrimSpace(r.Name); n != "" && !slices.Contains(out[r.WorkID][i].Names, n) {
			out[r.WorkID][i].Names = append(out[r.WorkID][i].Names, n)
		}
	}
	return out, nil
}

func nameParts(name string) []string {
	parts := []string{name}
	for _, p := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '\u3000' || r == '・' }) {
		if utf8.RuneCountInString(p) >= 2 && p != name {
			parts = append(parts, p)
		}
	}
	return parts
}

func buildTranslateJob(entryID int64, c *walkthrough.Chart, pins map[string]characterPin, roster []rosterName) translateJob {
	job := translateJob{EntryID: entryID, Glossary: []glossaryTerm{}, KeepNames: []string{}}
	add := func(s *string) {
		if s != nil && strings.TrimSpace(*s) != "" && !slices.Contains(job.Strings, *s) {
			job.Strings = append(job.Strings, *s)
		}
	}
	for _, s := range c.Segments {
		add(s.Condition)
		for _, st := range s.Steps {
			add(st.Text)
			for i := range st.Alt {
				add(&st.Alt[i])
			}
			add(st.Remark)
		}
	}
	for _, l := range c.Legend {
		add(&l.Text)
	}
	for _, e := range c.Endings {
		add(&e.Name)
	}
	for _, s := range c.Sections {
		add(&s.Title)
	}
	for _, s := range c.Saves {
		add(&s.Label)
	}
	for _, e := range c.Entries {
		add(&e.Label)
	}
	keep := func(n string) {
		for _, p := range nameParts(n) {
			if !slices.Contains(job.KeepNames, p) {
				job.KeepNames = append(job.KeepNames, p)
			}
		}
	}
	translated := map[string]bool{}
	zhOf := map[int64]string{}
	namesOf := map[int64][]string{}
	for _, r := range roster {
		// A zh-Hans alias that copies the Japanese name is no translation: taking it for one
		// left 美紀 unprotected, and the model wrote 美纪.
		if slices.ContainsFunc(r.Names, func(n string) bool { return nameKey(n) == nameKey(r.Zh) }) {
			r.Zh = ""
		}
		zhOf[r.CharacterID] = r.Zh
		namesOf[r.CharacterID] = r.Names
		for _, n := range r.Names {
			if r.Zh != "" {
				job.Glossary = append(job.Glossary, glossaryTerm{Ja: n, Zh: r.Zh})
				translated[nameKey(n)] = true
			} else {
				keep(n)
			}
		}
	}
	for _, ch := range c.Characters {
		id := pins[ch.Key].CharacterID
		zh := zhOf[id]
		for _, n := range append([]string{ch.Name}, ch.Aliases...) {
			switch n = strings.TrimSpace(n); {
			case n == "" || translated[nameKey(n)]:
			case zh != "":
				job.Glossary = append(job.Glossary, glossaryTerm{Ja: n, Zh: shortZh(n, namesOf[id], zh)})
				translated[nameKey(n)] = true
			default:
				keep(n)
			}
		}
	}
	job.KeepNames = slices.DeleteFunc(job.KeepNames, func(n string) bool { return translated[nameKey(n)] })
	return job
}

// A short name took its pin's full zh name, so grok wrote 憂緒に駆け寄る as
// 跑向鹿谷忧绪. A kanji given or family name of a full name takes the matching
// end of the zh name instead; anything else keeps the full name, which is
// still the right person.
func shortZh(short string, fullNames []string, zh string) string {
	s, z := []rune(nameNoise.ReplaceAllString(short, "")), []rune(zh)
	if !allHan(s) {
		return zh
	}
	for _, full := range fullNames {
		parts := strings.FieldsFunc(full, func(r rune) bool { return unicode.IsSpace(r) || r == '・' })
		if len(parts) != 2 {
			continue
		}
		joined := []rune(parts[0] + parts[1])
		switch {
		case parts[1] == string(s) && allHan([]rune(parts[1])) && len(z) > len(s):
			return string(z[len(z)-len(s):])
		case parts[0] == string(s) && len(joined) == len(z) && allHan(joined):
			return string(z[:len(s)])
		}
	}
	return zh
}

func allHan(rs []rune) bool {
	for _, r := range rs {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return len(rs) > 0
}

func translateHash(job translateJob) string {
	b, _ := json.Marshal(struct {
		S []string
		G []glossaryTerm
		K []string
	}{job.Strings, job.Glossary, job.KeepNames})
	return hashText(string(b))
}

func settledTranslations(ctx context.Context, src *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		EntryID   int64
		InputHash string
	}
	err := src.WithContext(ctx).Table("wt_stage.translation").
		Where("prompt_version = ? AND error = ''", translatePromptVersion).Scan(&rows).Error
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.EntryID] = r.InputHash
	}
	return out, err
}

type translateStats struct {
	asked, refused int
}

func translateOne(ctx context.Context, l *llm, schema map[string]any, job translateJob, maxTokens int) (map[string]string, translateStats, error) {
	out := map[string]string{}
	var st translateStats
	mask := newNameMask(job.KeepNames, 2)
	var todo []string
	for _, ja := range job.Strings {
		if zh, ok := job.Known[ja]; ok {
			out[ja] = zh
			continue
		}
		if mask.onlyNames(ja) {
			out[ja] = ja
		} else {
			todo = append(todo, ja)
		}
	}
	var refused []string
	var run func(mask nameMask, chunk []string) error
	run = func(mask nameMask, chunk []string) error {
		masked := make([]string, len(chunk))
		for i, ja := range chunk {
			masked[i] = mask.hide(ja)
		}
		in, err := json.Marshal(struct {
			Glossary []glossaryTerm `json:"glossary"`
			Strings  []string       `json:"strings"`
		}{job.Glossary, masked})
		if err != nil {
			return err
		}
		var got struct {
			T []string `json:"t"`
		}
		// grok answers a chunk it will not translate in prose (这…, 我…), and the
		// cursor gateway blocks some with 422; either failed the whole chart, so
		// a refused chunk is split like a short one.
		if _, err := l.chatJSON(ctx, translateSystem, string(in), "translation", schema, maxTokens, &got); err != nil && !refusedChunk(err) {
			return err
		}
		if len(got.T) != len(chunk) {
			if len(chunk) == 1 {
				st.asked++
				refused = append(refused, chunk[0])
				return nil
			}
			half := len(chunk) / 2
			if err := run(mask, chunk[:half]); err != nil {
				return err
			}
			return run(mask, chunk[half:])
		}
		st.asked += len(chunk)
		for i, ja := range chunk {
			zh, ok := mask.restore(masked[i], strings.TrimSpace(got.T[i]))
			if !ok || zh == "" || !keepsNames(ja, zh, job.KeepNames) {
				refused = append(refused, ja)
				continue
			}
			out[ja] = zh
		}
		return nil
	}
	runAll := func(mask nameMask, todo []string) error {
		for _, chunk := range chunkStrings(todo) {
			if err := run(mask, chunk); err != nil {
				return err
			}
		}
		return nil
	}
	if err := runAll(mask, todo); err != nil {
		return out, st, err
	}
	full := newNameMask(job.KeepNames, 1)
	first := refused
	todo, refused = nil, nil
	for _, ja := range first {
		if full.hide(ja) == mask.hide(ja) {
			refused = append(refused, ja)
		} else if full.onlyNames(ja) {
			out[ja] = ja
		} else {
			todo = append(todo, ja)
		}
	}
	err := runAll(full, todo)
	st.refused = len(refused)
	return out, st, err
}

func chunkStrings(todo []string) [][]string {
	var out [][]string
	start, runes := 0, 0
	for i, s := range todo {
		n := utf8.RuneCountInString(s)
		if i > start && (i-start == translateChunk || runes+n > translateChunkRunes) {
			out = append(out, todo[start:i])
			start, runes = i, 0
		}
		runes += n
	}
	if start < len(todo) {
		out = append(out, todo[start:])
	}
	return out
}

func refusedChunk(err error) bool {
	var se *llmsuggest.StatusError
	return errors.Is(err, errNotJSON) || errors.As(err, &se) && se.Status == http.StatusUnprocessableEntity
}

var placeholderRe = regexp.MustCompile(`\{N[0-9]+\}`)

// nameMask swaps every kept name of two or more runes for a {N<i>} token
// before a string reaches the model and swaps it back after: asked to keep
// the names verbatim, glm-5.2 still transliterated them, and the keepsNames
// guard dropped 40.9k of the 287k strings on 2026-10-25. A one-rune name
// stays unmasked at first, because 愛 or 藍 is as often a word as a name; but
// the model then wrote 鈴を追う as 追铃, the guard refused it, and 1,556 choice
// steps went without zh-Hans, so a refused string is asked once more with
// one-rune names masked too.
type nameMask struct {
	hider, shower *strings.Replacer
}

func newNameMask(names []string, minRunes int) nameMask {
	long := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return utf8.RuneCountInString(n) < minRunes })
	slices.SortStableFunc(long, func(a, b string) int { return utf8.RuneCountInString(b) - utf8.RuneCountInString(a) })
	var hide, show []string
	for i, n := range long {
		tok := fmt.Sprintf("{N%d}", i+1)
		hide = append(hide, n, tok)
		show = append(show, tok, n)
	}
	return nameMask{hider: strings.NewReplacer(hide...), shower: strings.NewReplacer(show...)}
}

func (m nameMask) hide(s string) string {
	return m.hider.Replace(s)
}

func (m nameMask) onlyNames(s string) bool {
	h := m.hide(s)
	return h != s && !strings.ContainsFunc(placeholderRe.ReplaceAllString(h, ""), unicode.IsLetter)
}

func (m nameMask) restore(masked, zh string) (string, bool) {
	want, got := placeholderRe.FindAllString(masked, -1), placeholderRe.FindAllString(zh, -1)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		return "", false
	}
	return m.shower.Replace(zh), true
}

func keepsNames(ja, zh string, names []string) bool {
	for _, n := range names {
		if strings.Contains(ja, n) && !strings.Contains(zh, n) {
			return false
		}
	}
	return true
}

func translatedBy(prev, model string, asked int) string {
	switch {
	case prev == "" || prev == model:
		return model
	case asked == 0:
		return prev
	default:
		return prev + "+" + model
	}
}

func storeTranslation(ctx context.Context, src *gorm.DB, job translateJob, model string, out map[string]string, errText string) error {
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return src.WithContext(ctx).Exec(`
INSERT INTO wt_stage.translation (entry_id, prompt_version, model, input_hash, strings, error, created_at)
VALUES (?, ?, ?, ?, ?::jsonb, ?, now())
ON CONFLICT (entry_id, prompt_version) DO UPDATE SET model = EXCLUDED.model, input_hash = EXCLUDED.input_hash,
  strings = EXCLUDED.strings, error = EXCLUDED.error, created_at = now()
WHERE EXCLUDED.error = '' OR wt_stage.translation.error <> ''`,
		job.EntryID, translatePromptVersion, model, translateHash(job), string(b), errText).Error
}
