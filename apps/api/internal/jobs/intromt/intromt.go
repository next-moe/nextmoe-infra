package intromt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/introguard"

	"gorm.io/gorm"
)

const maxSamples = 30

type Opts struct {
	DSN        string
	Apply      bool
	Population Population
	SourceLang SourceLang
	Top        int
	Limit      int
	WorkIDs    []int64
	Force      bool
	Delay      time.Duration
	Workers    int
	// ChangedSince narrows the population to works with an intro row written in
	// that window — the frequent pass that follows editor saves between nights.
	ChangedSince time.Duration
}

type Sample struct {
	WorkID   int64
	Decision string
	Ja       string
	Zh       string
	MTModel  string
	Gloss    Glossary
}

type Stats struct {
	Candidates       int
	WithGlossary     int
	WouldInsert      int
	WouldRetranslate int
	SkipUnchanged    int
	WouldPrune       int
	Inserted         int
	Retranslated     int
	Pruned           int
	Refused          int
	Collapsed        int
	Errors           int
	// Untranslatable counts works with no translatable ja/en source;
	// WouldClear/Cleared the ones among them, or among refused answers, whose
	// machine rows were removed. WouldHeal counts machine rows that fail
	// introguard.Reject and are translated again; Rejected the answers refused;
	// SkipRejected the sources refused within introguard.RetryAfter.
	Untranslatable int
	WouldClear     int
	Cleared        int
	WouldHeal      int
	Rejected       int
	SkipRejected   int

	Samples []Sample
}

func Run(ctx context.Context, tr Translator, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the acceptance run")
	}
	if opts.Apply && tr == nil {
		return nil, fmt.Errorf("apply mode needs a translator")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	pop := opts.Population
	if pop == "" {
		pop = PopulationBodyless
	}
	src := opts.SourceLang
	if src == "" {
		src = SourceJa
	}
	workIDs := opts.WorkIDs
	if opts.ChangedSince > 0 {
		if workIDs, err = changedWorks(ctx, db, opts.ChangedSince); err != nil {
			return nil, fmt.Errorf("load changed works: %w", err)
		}
		if len(workIDs) == 0 {
			slog.Info("intro-mt: no work intro changed in the window", "changed_since", opts.ChangedSince)
			return &Stats{}, nil
		}
	}
	cands, err := loadCandidates(ctx, db, reg, pop, src, opts.Top, opts.Limit, workIDs)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	if err := attachGlossaries(ctx, db, cands, src); err != nil {
		return nil, fmt.Errorf("load glossaries: %w", err)
	}
	withGloss := 0
	for _, c := range cands {
		if len(c.Gloss) > 0 {
			withGloss++
		}
	}
	slog.Info("intro-mt candidates", "population", pop, "candidates", len(cands),
		"with_glossary", withGloss, "apply", opts.Apply, "top", opts.Top, "limit", opts.Limit,
		"work_ids", len(opts.WorkIDs), "force", opts.Force)
	if n := len(opts.WorkIDs); n > 0 && opts.ChangedSince == 0 && n != len(cands) {
		slog.Warn("named work ids did not all become candidates — the rest fail this lane's gates (medium, olang, a source zh intro, or no source-lang intro)",
			"named", n, "candidates", len(cands))
	}

	rejected, err := introguard.LoadRejections(ctx, db, workIntroTable)
	if err != nil {
		return nil, fmt.Errorf("load refused translations: %w", err)
	}
	r := &runner{db: db, tr: tr, force: opts.Force, rejected: rejected, stats: &Stats{Candidates: len(cands), WithGlossary: withGloss}}
	r.process(ctx, cands, opts.Apply, opts.Delay, opts.Workers)
	if err := r.touch(ctx); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	st := r.stats
	slog.Info("intro-mt done", "apply", opts.Apply,
		"candidates", st.Candidates, "with_glossary", st.WithGlossary, "would_insert", st.WouldInsert,
		"would_retranslate", st.WouldRetranslate, "skip_unchanged", st.SkipUnchanged,
		"would_prune", st.WouldPrune,
		"inserted", st.Inserted, "retranslated", st.Retranslated, "pruned", st.Pruned,
		"refused", st.Refused, "collapsed", st.Collapsed, "errors", st.Errors,
		"untranslatable", st.Untranslatable, "would_clear", st.WouldClear, "cleared", st.Cleared,
		"would_heal", st.WouldHeal, "rejected", st.Rejected, "skip_rejected", st.SkipRejected)
	return st, nil
}

func (r *runner) beginSample(c candidate, dec decision) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stats.Samples) >= maxSamples {
		return -1
	}
	r.stats.Samples = append(r.stats.Samples, Sample{
		WorkID: c.WorkID, Decision: decisionName(dec), Ja: c.JaText, Gloss: c.Gloss,
	})
	return len(r.stats.Samples) - 1
}

func (r *runner) finishSample(i int, zh, mtModel string) {
	if i < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Samples[i].Zh = zh
	r.stats.Samples[i].MTModel = mtModel
}

func decisionName(d decision) string {
	switch d {
	case decInsert:
		return "insert"
	case decRetrans:
		return "retranslate"
	default:
		return "skip_unchanged"
	}
}

func changedWorks(ctx context.Context, db *gorm.DB, window time.Duration) ([]int64, error) {
	var ids []int64
	err := db.WithContext(ctx).Raw(`SELECT DISTINCT work_id FROM catalog_work_intro WHERE updated_at >= ?`,
		time.Now().Add(-window)).Scan(&ids).Error
	return ids, err
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}
