package entityintromt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/infrastructure/database"
)

const maxSamples = 30

const (
	LaneCharacter = "character"
	LanePerson    = "person"
	LaneLabel     = "label"
)

type laneDef struct {
	key         string
	introTable  string
	idCol       string
	entityTable string
}

var lanes = []laneDef{
	{LaneCharacter, "catalog_character_intro", "character_id", "catalog_character"},
	{LanePerson, "catalog_person_intro", "person_id", "catalog_person"},
	{LaneLabel, "catalog_label_intro", "label_id", "catalog_label"},
}

type Opts struct {
	DSN     string
	Apply   bool
	Lane    string
	Limit   int
	Offset  int
	Delay   time.Duration
	Workers int
	Force   bool
	// EntityIDs is meaningful only together with Lane: the three lanes number
	// their entities independently, so the same integers name a different row in
	// each one. Run rejects the combination rather than filtering all three.
	EntityIDs []int64
}

type Sample struct {
	Lane     string
	EntityID int64
	Decision string
	SrcLang  string
	Src      string
	Zh       string
	MTModel  string
	Gloss    Glossary
}

type LaneStats struct {
	Lane             string
	Candidates       int
	WithGlossary     int
	FromJa           int
	FromEn           int
	WouldInsert      int
	WouldRetranslate int
	SkipUnchanged    int
	SkipShortSource  int
	WouldPrune       int
	Inserted         int
	Retranslated     int
	Pruned           int
	Refused          int
	Errors           int

	Samples []Sample
}

func Run(ctx context.Context, tr Translator, opts Opts) ([]*LaneStats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the acceptance run")
	}
	if opts.Apply && tr == nil {
		return nil, fmt.Errorf("apply mode needs a translator")
	}
	selected, err := selectLanes(opts.Lane)
	if err != nil {
		return nil, err
	}
	if len(opts.EntityIDs) > 0 && opts.Lane == "" {
		return nil, fmt.Errorf("--entity-ids needs --lane: entity ids are per-lane, so an unlaned list would also match unrelated persons and labels holding the same numbers")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	var out []*LaneStats
	for _, lane := range selected {
		cands, err := loadCandidates(ctx, db, lane, opts.Limit, opts.Offset, opts.EntityIDs)
		if err != nil {
			return nil, fmt.Errorf("load %s candidates: %w", lane.key, err)
		}
		if err := attachGlossaries(ctx, db, lane, cands); err != nil {
			return nil, fmt.Errorf("load %s glossaries: %w", lane.key, err)
		}
		st := &LaneStats{Lane: lane.key, Candidates: len(cands)}
		for _, c := range cands {
			if c.SrcLang == string(SourceJa) {
				st.FromJa++
			} else {
				st.FromEn++
			}
			if len(c.Gloss) > 0 {
				st.WithGlossary++
			}
		}
		slog.Info("entity-intro-mt candidates", "lane", lane.key,
			"candidates", len(cands), "from_ja", st.FromJa, "from_en", st.FromEn,
			"with_glossary", st.WithGlossary,
			"apply", opts.Apply, "limit", opts.Limit, "offset", opts.Offset,
			"entity_ids", len(opts.EntityIDs), "force", opts.Force)
		if n := len(opts.EntityIDs); n > 0 && n != len(cands) {
			slog.Warn("named entity ids did not all become candidates — the rest fail this lane's gates (deleted, a source zh intro, or no ja/en source intro)",
				"lane", lane.key, "named", n, "candidates", len(cands))
		}

		r := &runner{db: db, tr: tr, lane: lane, force: opts.Force, stats: st}
		r.process(ctx, cands, opts.Apply, opts.Delay, opts.Workers)
		if err := r.touch(ctx); err != nil {
			return nil, fmt.Errorf("touch %s entities: %w", lane.key, err)
		}
		slog.Info("entity-intro-mt lane done", "lane", lane.key, "apply", opts.Apply,
			"candidates", st.Candidates, "with_glossary", st.WithGlossary, "would_insert", st.WouldInsert,
			"would_retranslate", st.WouldRetranslate, "skip_unchanged", st.SkipUnchanged,
			"skip_short_source", st.SkipShortSource, "would_prune", st.WouldPrune,
			"inserted", st.Inserted, "retranslated", st.Retranslated, "pruned", st.Pruned,
			"refused", st.Refused, "errors", st.Errors)
		out = append(out, st)
		if ctx.Err() != nil {
			break
		}
	}
	return out, nil
}

func selectLanes(key string) ([]laneDef, error) {
	if key == "" {
		return lanes, nil
	}
	for _, l := range lanes {
		if l.key == key {
			return []laneDef{l}, nil
		}
	}
	return nil, fmt.Errorf("unknown lane %q (want %q, %q or %q)", key, LaneCharacter, LanePerson, LaneLabel)
}
