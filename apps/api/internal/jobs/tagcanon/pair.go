package tagcanon

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/vndbtagmap"

	"gorm.io/gorm"
)

type ProposeOpts struct {
	DSN             string
	DlsiteDSN       string
	TagMapPath      string
	SingleThreshold int
	Out             string
	Workers         int
	MaxPairs        int
	MaxEdit         int
	Prior           string
	SkipOriginals   bool
}

func identityOrig(s string) string { return s }

type ProposeStats struct {
	Block             blockStats
	Pairs             int
	SingleProposed    int
	RelationCounts    map[Relation]int
	Errors            int
	SkippedPrior      int
	SkippedPriorNames int
}

func Propose(ctx context.Context, mt Matcher, opts ProposeOpts) (*ProposeStats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.Out == "" {
		return nil, fmt.Errorf("output path is required (--out)")
	}
	if mt == nil {
		return nil, fmt.Errorf("a matcher is required (use MockMatcher for the offline rehearsal)")
	}
	if opts.SingleThreshold <= 0 {
		opts.SingleThreshold = 100
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	pool, err := buildPool(ctx, db, opts)
	if err != nil {
		return nil, err
	}

	blockingPool := make([]candName, 0, len(pool))
	for _, c := range pool {
		if !isMeta(c.Norm) {
			blockingPool = append(blockingPool, c)
		}
	}
	pairs, bst := buildCandidatePairs(blockingPool, blockOpts{MaxPairs: opts.MaxPairs, MaxEdit: opts.MaxEdit})
	slog.Info("tag-pair blocking", "pool", len(blockingPool), "pairs", bst.Total,
		"substring", bst.Substring, "edit", bst.Edit, "cooccur", bst.Cooccur, "capped", bst.Capped)

	st := &ProposeStats{Block: bst, RelationCounts: map[Relation]int{}}

	priorNames := map[string]struct{}{}
	if opts.Prior != "" {
		priorKeys, names, err := loadPriorKeys(opts.Prior)
		if err != nil {
			return nil, fmt.Errorf("load prior verdicts: %w", err)
		}
		priorNames = names
		kept := pairs[:0]
		for _, p := range pairs {
			if _, judged := priorKeys[pairKey(p.A.SourceKey, p.A.Name, p.B.SourceKey, p.B.Name)]; judged {
				st.SkippedPrior++
				continue
			}
			kept = append(kept, p)
		}
		pairs = kept
		slog.Info("tag-pair prior skip", "prior", opts.Prior, "skipped", st.SkippedPrior,
			"prior_names", len(priorNames), "remaining", len(pairs))
	}

	var mu sync.Mutex
	var recs []pairRec

	runPooled(ctx, len(pairs), opts.Workers, func(i int) {
		p := pairs[i]
		v, model, err := mt.MatchPair(ctx, PairInput{
			ASourceKey: p.A.SourceKey, AName: p.A.Name, AOrig: p.A.Orig, AUsage: p.A.Usage,
			BSourceKey: p.B.SourceKey, BName: p.B.Name, BOrig: p.B.Orig, BUsage: p.B.Usage,
		})
		rec := pairRec{
			Kind:    "pair",
			ASource: p.A.SourceKey, AName: p.A.Name, AOrig: p.A.Orig, AUsage: p.A.Usage,
			BSource: p.B.SourceKey, BName: p.B.Name, BOrig: p.B.Orig, BUsage: p.B.Usage,
			Block: p.Block,
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			st.Errors++
			slog.Warn("match pair", "a", p.A.Name, "b", p.B.Name, "err", err)
			return
		}
		rec.Relation = string(v.Relation)
		rec.Confidence = v.Confidence
		rec.Reason = v.Reason
		rec.Model = model
		st.RelationCounts[v.Relation]++
		recs = append(recs, rec)
	})
	st.Pairs = len(pairs)

	singles := make([]candName, 0)
	for _, c := range pool {
		if c.Usage < opts.SingleThreshold {
			continue
		}
		if _, judged := priorNames[nameKey(c.SourceKey, c.Name)]; judged {
			st.SkippedPriorNames++
			continue
		}
		singles = append(singles, c)
	}
	runPooled(ctx, len(singles), opts.Workers, func(i int) {
		c := singles[i]
		v, model, err := mt.ClassifyName(ctx, NameInput{SourceKey: c.SourceKey, Name: c.Name, Orig: c.Orig, Usage: c.Usage})
		rec := pairRec{Kind: "single", Source: c.SourceKey, Name: c.Name, Orig: c.Orig, Usage: c.Usage}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			st.Errors++
			slog.Warn("classify name", "name", c.Name, "err", err)
			return
		}
		rec.Tier = i16p(v.Tier)
		rec.Kind_ = i16p(v.Kind)
		rec.Confidence = v.Confidence
		rec.Reason = v.Reason
		rec.Model = model
		recs = append(recs, rec)
		st.SingleProposed++
	})

	sortRecords(recs)
	if err := writeRecords(opts.Out, recs); err != nil {
		return nil, fmt.Errorf("write verdicts: %w", err)
	}
	slog.Info("tag-pair propose done", "pairs", st.Pairs, "singles", st.SingleProposed,
		"errors", st.Errors, "out", opts.Out)
	return st, nil
}

func buildPool(ctx context.Context, db *gorm.DB, opts ProposeOpts) ([]candName, error) {
	src, err := resolveSources(ctx, db)
	if err != nil {
		return nil, err
	}
	srcKeys := map[int16]string{src.vndb: sourceKeyVNDB, src.bangumi: sourceKeyBangumi, src.dlsite: sourceKeyDlsite}

	vndb, err := loadWorkTagVocab(ctx, db, src.vndb)
	if err != nil {
		return nil, err
	}
	bgm, err := loadWorkTagVocab(ctx, db, src.bangumi)
	if err != nil {
		return nil, err
	}
	dl, err := loadWorkTagVocab(ctx, db, src.dlsite)
	if err != nil {
		return nil, err
	}
	junkIdx, err := loadJunkIndex(ctx, db)
	if err != nil {
		return nil, err
	}
	mapped, err := loadMappedNames(ctx, db)
	if err != nil {
		return nil, err
	}
	rejected, err := loadRejectedNames(ctx, db)
	if err != nil {
		return nil, err
	}

	vndbOrig, dlOrig := identityOrig, identityOrig
	if !opts.SkipOriginals {
		vndbOrig = loadVndbOrig(opts.TagMapPath)
		if dlOrig, err = loadDlsiteOrig(ctx, opts.DlsiteDSN); err != nil {
			return nil, err
		}
	}
	bgmWorks, err := loadNameWorkIDs(ctx, db, src.bangumi)
	if err != nil {
		return nil, err
	}
	dlWorks, err := loadNameWorkIDs(ctx, db, src.dlsite)
	if err != nil {
		return nil, err
	}

	pool := make([]candName, 0, len(vndb)+len(bgm)+len(dl))
	add := func(e vocabEntry, orig func(string) string, works map[string]map[int64]struct{}) {
		if _, already := mapped[mapKey(e.SourceID, e.Name)]; already {
			return
		}
		if _, no := rejected[mapKey(e.SourceID, e.Name)]; no {
			return
		}
		c := candName{
			SourceID: e.SourceID, SourceKey: srcKeys[e.SourceID],
			Name: e.Name, Norm: e.Norm, Usage: e.Usage,
			Orig: e.Name,
		}
		if orig != nil {
			if o := orig(e.Name); o != "" {
				c.Orig = o
			}
		}
		if works != nil {
			c.workIDs = works[e.Name]
		}
		pool = append(pool, c)
	}
	for _, e := range vndb {
		add(e, vndbOrig, nil)
	}
	for i := range bgm {
		if bgm[i].Junk || bgmJunk(bgm[i].Norm, junkIdx) != "" {
			continue
		}
		add(bgm[i], nil, bgmWorks)
	}
	for _, e := range dl {
		add(e, dlOrig, dlWorks)
	}
	sort.Slice(pool, func(i, j int) bool {
		if pool[i].SourceID != pool[j].SourceID {
			return pool[i].SourceID < pool[j].SourceID
		}
		return pool[i].Name < pool[j].Name
	})
	return pool, nil
}

func loadMappedNames(ctx context.Context, db *gorm.DB) (map[string]struct{}, error) {
	var rows []struct {
		SourceID   int16  `gorm:"column:source_id"`
		SourceName string `gorm:"column:source_name"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT source_id, source_name FROM catalog_tag_source_map`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load mapped names: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[mapKey(r.SourceID, r.SourceName)] = struct{}{}
	}
	return out, nil
}

func mapKey(source int16, name string) string { return fmt.Sprintf("%d\x00%s", source, name) }

func loadNameWorkIDs(ctx context.Context, db *gorm.DB, sourceID int16) (map[string]map[int64]struct{}, error) {
	var rows []struct {
		Name   string `gorm:"column:name"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT name, work_id FROM catalog_work_tag WHERE source_id = ?`, sourceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load name work ids (source %d): %w", sourceID, err)
	}
	out := map[string]map[int64]struct{}{}
	for _, r := range rows {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		s := out[name]
		if s == nil {
			s = map[int64]struct{}{}
			out[name] = s
		}
		s[r.WorkID] = struct{}{}
	}
	return out, nil
}

func loadVndbOrig(tagMapPath string) func(string) string {
	var m map[string]string
	if tagMapPath == "" {
		m = vndbtagmap.Embedded()
	} else {
		parsed, err := ParseTagMap(tagMapPath)
		if err != nil {
			slog.Warn("tag-pair: tagMap unreadable — vndb originals fall back to zh names", "path", tagMapPath, "err", err)
			return func(s string) string { return s }
		}
		m = parsed
	}
	inv := make(map[string]string, len(m))
	for eng, zh := range m {
		if cur, ok := inv[zh]; !ok || eng < cur {
			inv[zh] = eng
		}
	}
	return func(zh string) string {
		if eng, ok := inv[zh]; ok {
			return eng
		}
		return zh
	}
}

func loadDlsiteOrig(ctx context.Context, dsn string) (func(string) string, error) {
	if dsn == "" {
		return func(s string) string { return s }, nil
	}
	db, err := openGorm(dsn)
	if err != nil {
		return nil, fmt.Errorf("connect dlsite staging db: %w", err)
	}
	defer func() {
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.Close()
		}
	}()
	var rows []struct {
		Zh string `gorm:"column:zh"`
		Ja string `gorm:"column:ja"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT z.name AS zh, j.name AS ja
		FROM genre_taxonomy z
		JOIN genre_taxonomy j ON j.genre_id = z.genre_id AND j.locale = 'ja_JP'
		WHERE z.locale = 'zh_CN'`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load dlsite genre taxonomy: %w", err)
	}
	zhToJa := make(map[string]string, len(rows))
	for _, r := range rows {
		zh := strings.TrimSpace(r.Zh)
		ja := strings.TrimSpace(r.Ja)
		if zh != "" && ja != "" {
			zhToJa[zh] = ja
		}
	}
	slog.Info("tag-pair: loaded dlsite ja originals", "genres", len(zhToJa))
	return func(zh string) string {
		if ja, ok := zhToJa[zh]; ok {
			return ja
		}
		return zh
	}, nil
}

func runPooled(ctx context.Context, n, workers int, fn func(i int)) {
	if workers <= 1 {
		for i := range n {
			if ctx.Err() != nil {
				return
			}
			fn(i)
		}
		return
	}
	ch := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range ch {
				if ctx.Err() != nil {
					continue
				}
				fn(i)
			}
		})
	}
	for i := range n {
		if ctx.Err() != nil {
			break
		}
		ch <- i
	}
	close(ch)
	wg.Wait()
}

func sortRecords(recs []pairRec) {
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Kind == "pair" {
			if a.ASource != b.ASource {
				return a.ASource < b.ASource
			}
			if a.AName != b.AName {
				return a.AName < b.AName
			}
			if a.BSource != b.BSource {
				return a.BSource < b.BSource
			}
			return a.BName < b.BName
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Name < b.Name
	})
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func pairKey(aSource, aName, bSource, bName string) string {
	ka := aSource + ":" + aName
	kb := bSource + ":" + bName
	if ka > kb {
		ka, kb = kb, ka
	}
	return ka + "\x00" + kb
}

func nameKey(source, name string) string { return source + ":" + name }

// The pair keys AND the name keys: a single/map/reject verdict is a judgment on
// that name, so re-classifying it next month is paid-for work done twice.
func loadPriorKeys(path string) (pairs, names map[string]struct{}, err error) {
	recs, err := readRecords(path)
	if err != nil {
		return nil, nil, err
	}
	pairs = make(map[string]struct{}, len(recs))
	names = make(map[string]struct{}, len(recs))
	for _, r := range recs {
		switch r.Kind {
		case "pair":
			pairs[pairKey(r.ASource, r.AName, r.BSource, r.BName)] = struct{}{}
		case "single", "map", "reject":
			if r.Source != "" && r.Name != "" {
				names[nameKey(r.Source, r.Name)] = struct{}{}
			}
		}
	}
	return pairs, names, nil
}
