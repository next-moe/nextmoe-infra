package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"

	"gorm.io/gorm"
)

const translateChunk = 80

type glossaryTerm struct {
	Ja string `json:"ja"`
	Zh string `json:"zh"`
}

type translateJob struct {
	EntryID   int64
	Strings   []string
	Glossary  []glossaryTerm
	KeepNames []string
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
	var jobs []translateJob
	for _, id := range keysOf(charts) {
		if _, matched := pins[id]; !matched {
			continue
		}
		job := buildTranslateJob(id, charts[id], pins[id], rosters[works[id]])
		if len(job.Strings) == 0 || !o.Redo && done[id] == translateHash(job) {
			continue
		}
		jobs = append(jobs, job)
	}
	fmt.Printf("\n=== import-walkthroughs translate model=%s prompt=%s charts=%d todo=%d ===\n",
		l.model, translatePromptVersion, len(charts), len(jobs))

	schema := mustSchema(translateSchemaJSON)
	var finished, failed, refusedNames atomic.Int64
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
			out, refused, err := translateOne(ctx, l, schema, job, o.MaxTokens)
			refusedNames.Add(int64(refused))
			errText := ""
			if err != nil {
				failed.Add(1)
				errText = err.Error()
				slog.Warn("translate failed", "entry", job.EntryID, "err", err)
			}
			if err := storeTranslation(ctx, src, job, l.model, out, errText); err != nil {
				slog.Error("store translation", "entry", job.EntryID, "err", err)
			}
			if n := finished.Add(1); n%25 == 0 || n == int64(len(jobs)) {
				slog.Info("progress", "done", n, "of", len(jobs), "failed", failed.Load(), "refused_name_strings", refusedNames.Load())
			}
		}(job)
	}
	wg.Wait()
	fmt.Printf("translated=%d failed=%d refused_name_strings=%d\n", finished.Load()-failed.Load(), failed.Load(), refusedNames.Load())
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
		}
	}
	for _, e := range c.Endings {
		add(&e.Name)
	}
	for _, s := range c.Sections {
		add(&s.Title)
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
	for _, r := range roster {
		// A zh-Hans alias that copies the Japanese name is no translation: taking it for one
		// left 美紀 unprotected, and the model wrote 美纪.
		if slices.ContainsFunc(r.Names, func(n string) bool { return nameKey(n) == nameKey(r.Zh) }) {
			r.Zh = ""
		}
		zhOf[r.CharacterID] = r.Zh
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
		zh := zhOf[pins[ch.Key].CharacterID]
		for _, n := range append([]string{ch.Name}, ch.Aliases...) {
			switch n = strings.TrimSpace(n); {
			case n == "" || translated[nameKey(n)]:
			case zh != "":
				job.Glossary = append(job.Glossary, glossaryTerm{Ja: n, Zh: zh})
				translated[nameKey(n)] = true
			default:
				keep(n)
			}
		}
	}
	job.KeepNames = slices.DeleteFunc(job.KeepNames, func(n string) bool { return translated[nameKey(n)] })
	return job
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

func translateOne(ctx context.Context, l *llm, schema map[string]any, job translateJob, maxTokens int) (map[string]string, int, error) {
	out := map[string]string{}
	refused := 0
	var run func(chunk []string) error
	run = func(chunk []string) error {
		in, err := json.Marshal(struct {
			Glossary []glossaryTerm `json:"glossary"`
			Strings  []string       `json:"strings"`
		}{job.Glossary, chunk})
		if err != nil {
			return err
		}
		var got struct {
			T []string `json:"t"`
		}
		if _, err := l.chatJSON(ctx, translateSystem, string(in), "translation", schema, maxTokens, &got); err != nil {
			return err
		}
		if len(got.T) != len(chunk) {
			if len(chunk) == 1 {
				refused++
				return nil
			}
			half := len(chunk) / 2
			if err := run(chunk[:half]); err != nil {
				return err
			}
			return run(chunk[half:])
		}
		for i, ja := range chunk {
			zh := strings.TrimSpace(got.T[i])
			if zh == "" || !keepsNames(ja, zh, job.KeepNames) {
				refused++
				continue
			}
			out[ja] = zh
		}
		return nil
	}
	for start := 0; start < len(job.Strings); start += translateChunk {
		if err := run(job.Strings[start:min(start+translateChunk, len(job.Strings))]); err != nil {
			return out, refused, err
		}
	}
	return out, refused, nil
}

func keepsNames(ja, zh string, names []string) bool {
	for _, n := range names {
		if strings.Contains(ja, n) && !strings.Contains(zh, n) {
			return false
		}
	}
	return true
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
  strings = EXCLUDED.strings, error = EXCLUDED.error, created_at = now()`,
		job.EntryID, translatePromptVersion, model, translateHash(job), string(b), errText).Error
}
