package tagsafety

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type ApplyOpts struct {
	DSN           string
	In            string
	Reviewed      string
	ReviewOut     string
	MinConfidence float64
	Limit         int
	Run           bool
}

type SourceName struct {
	Source string
	Name   string
}

type VocabRow struct {
	ID     int64
	Sexual bool
	Tier   int16
}

type CatalogState struct {
	Vocab  map[string]VocabRow
	Mapped map[string]string
}

type Plan struct {
	WorkTagSexual []SourceName
	VocabSexual   []string
	VocabHidden   []string
	Review        []Verdict
	Counts        PlanCounts
	Truncated     bool
}

type PlanCounts struct {
	Total          int
	ByClass        map[Class]int
	Confident      map[Class]int
	BelowThreshold int
	Reviewed       int
	DeflagGuard    int
	UnmappedJunk   int
	AlreadySexual  int
	AlreadyHidden  int
	Buckets        map[string]int
}

type ApplyStats struct {
	Plan             Plan
	WorkTagRows      int64
	VocabSexualRows  int64
	VocabHiddenRows  int64
	Errors           int
	ReviewFileWrites int
}

func Apply(ctx context.Context, opts ApplyOpts) (*ApplyStats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.In == "" {
		return nil, fmt.Errorf("verdict JSONL is required (--in)")
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.90
	}
	verdicts, err := readVerdicts(opts.In)
	if err != nil {
		return nil, fmt.Errorf("read verdicts: %w", err)
	}
	var reviewed []ReviewedLine
	if opts.Reviewed != "" {
		if reviewed, err = readReviewed(opts.Reviewed); err != nil {
			return nil, fmt.Errorf("read reviewed: %w", err)
		}
	}

	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	sourceIDs, err := resolveSources(ctx, db, verdictSourceKeys(verdicts, reviewed))
	if err != nil {
		return nil, err
	}
	state, err := loadCatalogState(ctx, db, sourceIDs)
	if err != nil {
		return nil, err
	}

	plan := BuildPlan(verdicts, reviewed, state, PlanOpts{MinConfidence: opts.MinConfidence, Limit: opts.Limit})
	st := &ApplyStats{Plan: plan}

	if opts.ReviewOut != "" {
		if err := writeVerdicts(opts.ReviewOut, plan.Review); err != nil {
			return nil, fmt.Errorf("write review file: %w", err)
		}
		st.ReviewFileWrites = len(plan.Review)
	}
	if err := executePlan(ctx, &gormWriter{db: db, sources: sourceIDs}, plan, opts.Run, st); err != nil {
		return st, err
	}
	slog.Info("tag-safety apply done", "work_tag_rows", st.WorkTagRows,
		"vocab_sexual_rows", st.VocabSexualRows, "vocab_hidden_rows", st.VocabHiddenRows,
		"errors", st.Errors)
	return st, nil
}

type PlanOpts struct {
	MinConfidence float64
	Limit         int
}

func BuildPlan(verdicts []Verdict, reviewed []ReviewedLine, state CatalogState, opts PlanOpts) Plan {
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.90
	}
	plan := Plan{Counts: PlanCounts{
		ByClass:   map[Class]int{},
		Confident: map[Class]int{},
		Buckets:   map[string]int{},
	}}

	effective := mergeReviewed(verdicts, reviewed, &plan.Counts)

	workSeen := map[string]struct{}{}
	vocabSexSeen := map[string]struct{}{}
	vocabHidSeen := map[string]struct{}{}

	for _, v := range effective {
		cls := Class(v.Class)
		plan.Counts.Total++
		plan.Counts.ByClass[cls]++
		plan.Counts.Buckets[confidenceBucket(v.Confidence)]++

		if v.Confidence < opts.MinConfidence {
			plan.Counts.BelowThreshold++
			plan.Review = append(plan.Review, withNote(v, "below-confidence"))
			continue
		}
		plan.Counts.Confident[cls]++

		canonical, hasCanonical := canonicalName(v, state)
		row := state.Vocab[canonical]

		switch cls {
		case ClassSexual:
			if v.Source != VocabSource {
				if _, dup := workSeen[v.key()]; !dup {
					workSeen[v.key()] = struct{}{}
					plan.WorkTagSexual = append(plan.WorkTagSexual, SourceName{Source: v.Source, Name: v.Name})
				}
			}
			if hasCanonical {
				switch {
				case row.Sexual:
					plan.Counts.AlreadySexual++
				default:
					if _, dup := vocabSexSeen[canonical]; !dup {
						vocabSexSeen[canonical] = struct{}{}
						plan.VocabSexual = append(plan.VocabSexual, canonical)
					}
				}
			}
		case ClassJunk:
			if !hasCanonical {
				plan.Counts.UnmappedJunk++
				continue
			}
			if row.Tier == model.TagTierHidden {
				plan.Counts.AlreadyHidden++
				continue
			}
			if _, dup := vocabHidSeen[canonical]; !dup {
				vocabHidSeen[canonical] = struct{}{}
				plan.VocabHidden = append(plan.VocabHidden, canonical)
			}
		case ClassNormal:
			if hasCanonical && row.Sexual {
				plan.Counts.DeflagGuard++
				plan.Review = append(plan.Review, withNote(v, "deflag-candidate: catalog_tag.sexual=true but classified normal — human ruling required"))
			}
		}
	}

	sort.Slice(plan.WorkTagSexual, func(i, j int) bool {
		if plan.WorkTagSexual[i].Source != plan.WorkTagSexual[j].Source {
			return plan.WorkTagSexual[i].Source < plan.WorkTagSexual[j].Source
		}
		return plan.WorkTagSexual[i].Name < plan.WorkTagSexual[j].Name
	})
	sort.Strings(plan.VocabSexual)
	sort.Strings(plan.VocabHidden)
	sort.Slice(plan.Review, func(i, j int) bool {
		if plan.Review[i].Source != plan.Review[j].Source {
			return plan.Review[i].Source < plan.Review[j].Source
		}
		return plan.Review[i].Name < plan.Review[j].Name
	})

	applyLimit(&plan, opts.Limit)
	return plan
}

func applyLimit(plan *Plan, limit int) {
	if limit <= 0 {
		return
	}
	budget := limit
	take := func(n int) int {
		if n > budget {
			n = budget
		}
		budget -= n
		return n
	}
	total := len(plan.WorkTagSexual) + len(plan.VocabSexual) + len(plan.VocabHidden)
	plan.WorkTagSexual = plan.WorkTagSexual[:take(len(plan.WorkTagSexual))]
	plan.VocabSexual = plan.VocabSexual[:take(len(plan.VocabSexual))]
	plan.VocabHidden = plan.VocabHidden[:take(len(plan.VocabHidden))]
	plan.Truncated = total > limit
}

func mergeReviewed(verdicts []Verdict, reviewed []ReviewedLine, counts *PlanCounts) []Verdict {
	byKey := make(map[string]ReviewedLine, len(reviewed))
	for _, r := range reviewed {
		byKey[verdictKey(r.Source, r.Name)] = r
	}
	out := make([]Verdict, 0, len(verdicts)+len(reviewed))
	used := make(map[string]struct{}, len(reviewed))
	for _, v := range verdicts {
		if r, ok := byKey[v.key()]; ok {
			used[v.key()] = struct{}{}
			counts.Reviewed++
			v.Class = r.Class
			v.Confidence = 1
			v.Reason = firstNonEmpty(r.Reason, v.Reason)
			v.Model = "human"
		}
		out = append(out, v)
	}
	for _, r := range reviewed {
		k := verdictKey(r.Source, r.Name)
		if _, hit := used[k]; hit {
			continue
		}
		counts.Reviewed++
		out = append(out, Verdict{Source: r.Source, Name: r.Name, Class: r.Class, Confidence: 1, Reason: r.Reason, Model: "human"})
	}
	return out
}

func canonicalName(v Verdict, state CatalogState) (string, bool) {
	name := v.Name
	if v.Source != VocabSource {
		mapped, ok := state.Mapped[v.key()]
		if !ok {
			return "", false
		}
		name = mapped
	}
	if _, ok := state.Vocab[name]; !ok {
		return "", false
	}
	return name, true
}

func withNote(v Verdict, note string) Verdict {
	v.Note = note
	return v
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func confidenceBucket(c float64) string {
	switch {
	case c >= 0.90:
		return "0.90-1.00"
	case c >= 0.70:
		return "0.70-0.90"
	case c >= 0.50:
		return "0.50-0.70"
	default:
		return "0.00-0.50"
	}
}

func verdictSourceKeys(verdicts []Verdict, reviewed []ReviewedLine) []string {
	seen := map[string]struct{}{}
	add := func(s string) {
		if s != "" && s != VocabSource {
			seen[s] = struct{}{}
		}
	}
	for _, v := range verdicts {
		add(v.Source)
	}
	for _, r := range reviewed {
		add(r.Source)
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

type writer interface {
	setWorkTagSexual(ctx context.Context, source, name string) (int64, error)
	setTagSexual(ctx context.Context, name string) (int64, error)
	setTagHidden(ctx context.Context, name string) (int64, error)
}

func executePlan(ctx context.Context, w writer, plan Plan, run bool, st *ApplyStats) error {
	if !run {
		slog.Info("tag-safety apply (dry)", "work_tag_sexual", len(plan.WorkTagSexual),
			"vocab_sexual", len(plan.VocabSexual), "vocab_hidden", len(plan.VocabHidden),
			"review", len(plan.Review), "truncated", plan.Truncated)
		return nil
	}
	for _, t := range plan.WorkTagSexual {
		n, err := w.setWorkTagSexual(ctx, t.Source, t.Name)
		if err != nil {
			st.Errors++
			slog.Warn("set work tag sexual", "source", t.Source, "name", t.Name, "err", err)
			continue
		}
		st.WorkTagRows += n
	}
	for _, name := range plan.VocabSexual {
		n, err := w.setTagSexual(ctx, name)
		if err != nil {
			st.Errors++
			slog.Warn("set canonical sexual", "name", name, "err", err)
			continue
		}
		st.VocabSexualRows += n
	}
	for _, name := range plan.VocabHidden {
		n, err := w.setTagHidden(ctx, name)
		if err != nil {
			st.Errors++
			slog.Warn("set canonical hidden", "name", name, "err", err)
			continue
		}
		st.VocabHiddenRows += n
	}
	return nil
}

type gormWriter struct {
	db      *gorm.DB
	sources map[string]int16
}

func (g *gormWriter) setWorkTagSexual(ctx context.Context, source, name string) (int64, error) {
	id, ok := g.sources[source]
	if !ok {
		return 0, fmt.Errorf("source %q not resolved", source)
	}
	res := g.db.WithContext(ctx).Exec(
		`UPDATE catalog_work_tag SET sexual = true WHERE source_id = ? AND name = ? AND sexual = false AND `+editspec.NotCuratedLaneSQL("source_id"), id, name)
	return res.RowsAffected, res.Error
}

func (g *gormWriter) setTagSexual(ctx context.Context, name string) (int64, error) {
	res := g.db.WithContext(ctx).Exec(
		`UPDATE catalog_tag SET sexual = true, updated_at = now() WHERE name = ? AND sexual = false AND `+
			editspec.NotHumanFieldSQL("field_provenance", "sexual"), name)
	return res.RowsAffected, res.Error
}

func (g *gormWriter) setTagHidden(ctx context.Context, name string) (int64, error) {
	res := g.db.WithContext(ctx).Exec(
		`UPDATE catalog_tag SET tier = ?, updated_at = now() WHERE name = ? AND tier <> ? AND `+
			editspec.NotHumanFieldSQL("field_provenance", "tier"),
		model.TagTierHidden, name, model.TagTierHidden)
	return res.RowsAffected, res.Error
}

func loadCatalogState(ctx context.Context, db *gorm.DB, sources map[string]int16) (CatalogState, error) {
	st := CatalogState{Vocab: map[string]VocabRow{}, Mapped: map[string]string{}}

	var tags []struct {
		ID     int64  `gorm:"column:id"`
		Name   string `gorm:"column:name"`
		Tier   int16  `gorm:"column:tier"`
		Sexual bool   `gorm:"column:sexual"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, name, tier, sexual FROM catalog_tag`).Scan(&tags).Error; err != nil {
		return st, fmt.Errorf("load canonical vocabulary: %w", err)
	}
	byID := make(map[int64]string, len(tags))
	for _, t := range tags {
		st.Vocab[t.Name] = VocabRow{ID: t.ID, Sexual: t.Sexual, Tier: t.Tier}
		byID[t.ID] = t.Name
	}

	idToKey := make(map[int16]string, len(sources))
	for k, id := range sources {
		idToKey[id] = k
	}
	var maps []struct {
		SourceID   int16  `gorm:"column:source_id"`
		SourceName string `gorm:"column:source_name"`
		TagID      int64  `gorm:"column:tag_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT source_id, source_name, tag_id FROM catalog_tag_source_map`).Scan(&maps).Error; err != nil {
		return st, fmt.Errorf("load tag source map: %w", err)
	}
	for _, m := range maps {
		key, ok := idToKey[m.SourceID]
		if !ok {
			continue
		}
		if name, ok := byID[m.TagID]; ok {
			st.Mapped[verdictKey(key, m.SourceName)] = name
		}
	}
	return st, nil
}
