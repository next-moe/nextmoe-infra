package docsync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type EmbedStats struct {
	Scanned    int
	Embedded   int
	FilterOnly int
}

type EmbedOptions struct {
	Batch       int
	EmbedBatch  int
	Concurrency int
	IDs         []int64
}

func (o EmbedOptions) scope() (scope, int) {
	if o.IDs == nil {
		return nil, o.Batch
	}
	s := scope(slices.Compact(slices.Sorted(slices.Values(o.IDs))))
	return s, max(len(s), 1)
}

type card struct {
	doc  catalogSearch.VectorDoc
	text string
}

type embedRun struct {
	idx   *catalogSearch.Indexer
	emb   catalogSearch.Embedder
	uid   string
	opts  EmbedOptions
	stats EmbedStats
}

func (r *embedRun) page(ctx context.Context, version string, cards []card) error {
	model := r.emb.Model()
	ids := make([]string, len(cards))
	for i := range cards {
		cards[i].doc.EmbedModel = model
		cards[i].doc.TextHash = catalogSearch.CardHash(version, model, cards[i].text)
		ids[i] = cards[i].doc.ID
	}
	stamps, err := r.idx.VectorStamps(ctx, r.uid, ids)
	if err != nil {
		return err
	}
	var fresh, stale []catalogSearch.VectorDoc
	var texts []string
	for _, c := range cards {
		if st, ok := stamps[c.doc.ID]; ok && st.EmbedModel == model && st.TextHash == c.doc.TextHash {
			fresh = append(fresh, c.doc)
			continue
		}
		stale = append(stale, c.doc)
		texts = append(texts, c.text)
	}
	r.stats.Scanned += len(cards)
	if err := r.idx.UpdateVectorFilters(ctx, r.uid, fresh); err != nil {
		return err
	}
	r.stats.FilterOnly += len(fresh)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(r.opts.Concurrency)
	for start := 0; start < len(stale); start += r.opts.EmbedBatch {
		end := min(start+r.opts.EmbedBatch, len(stale))
		chunk, chunkTexts := stale[start:end], texts[start:end]
		g.Go(func() error {
			vecs, err := r.emb.Embed(gctx, chunkTexts)
			if err != nil {
				return fmt.Errorf("embed %s %d..%d: %w", r.uid, chunk[0].CatalogID, chunk[len(chunk)-1].CatalogID, err)
			}
			for i := range chunk {
				chunk[i].Vector = vecs[i]
			}
			return r.idx.UpsertVectors(gctx, r.uid, chunk)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	r.stats.Embedded += len(stale)
	slog.Info("embed page", "index", r.uid, "scanned", r.stats.Scanned, "embedded", r.stats.Embedded, "filter_only", r.stats.FilterOnly)
	return nil
}

func loadTagNames(db *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := db.Raw(`SELECT id, name FROM catalog_tag`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("tag names: %w", err)
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

func EmbedWorks(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, emb catalogSearch.Embedder, opts EmbedOptions) (EmbedStats, error) {
	run := &embedRun{idx: idx, emb: emb, uid: catalogSearch.IndexWorkVectors, opts: opts}
	tagNames, err := loadTagNames(db)
	if err != nil {
		return run.stats, err
	}
	s, batch := opts.scope()
	err = forEachWorkInput(db, s, batch, func(ins []catalogSearch.WorkDocInput) error {
		cards := make([]card, len(ins))
		for i, in := range ins {
			var names []string
			for _, id := range in.TagIDs {
				if n := tagNames[id]; n != "" {
					names = append(names, n)
				}
			}
			cards[i] = card{
				doc:  catalogSearch.NewWorkVectorDoc(catalogSearch.BuildWorkDoc(in)),
				text: catalogSearch.WorkCardText(in, names),
			}
		}
		return run.page(ctx, catalogSearch.WorkCardVersion, cards)
	})
	return run.stats, err
}

type characterExtras struct {
	works  map[int64][]string
	traits map[int64][]string
	intros map[int64][]catalogSearch.WorkDocIntro
}

func loadCharacterExtras(db *gorm.DB, ids []int64) (characterExtras, error) {
	x := characterExtras{works: map[int64][]string{}, traits: map[int64][]string{}, intros: map[int64][]catalogSearch.WorkDocIntro{}}
	var works []struct {
		CharacterID int64  `gorm:"column:character_id"`
		Name        string `gorm:"column:display_name"`
	}
	if err := db.Raw(`SELECT wc.character_id, w.display_name
		FROM catalog_work_character wc JOIN catalog_work w ON w.id = wc.work_id
		WHERE wc.character_id IN ? AND `+editspec.NotSuppressedRosterSQL("wc")+` AND `+editspec.LiveWorkSQL("wc.work_id")+`
		ORDER BY wc.character_id, wc.kind, wc.work_id`, ids).Scan(&works).Error; err != nil {
		return x, fmt.Errorf("character works: %w", err)
	}
	for _, r := range works {
		if !slices.Contains(x.works[r.CharacterID], r.Name) {
			x.works[r.CharacterID] = append(x.works[r.CharacterID], r.Name)
		}
	}
	var traits []struct {
		CharacterID int64  `gorm:"column:character_id"`
		Name        string `gorm:"column:name"`
	}
	if err := db.Raw(`SELECT l.character_id, coalesce(nullif(t.name_zh, ''), t.name) AS name
		FROM catalog_character_trait_link l JOIN catalog_character_trait t ON t.id = l.trait_id
		WHERE l.character_id IN ? AND l.spoiler_level <= ? AND t.searchable AND NOT t.sexual_family
		ORDER BY l.character_id, l.id`, ids, model.SpoilerNone).Scan(&traits).Error; err != nil {
		return x, fmt.Errorf("character traits: %w", err)
	}
	for _, r := range traits {
		if !slices.Contains(x.traits[r.CharacterID], r.Name) {
			x.traits[r.CharacterID] = append(x.traits[r.CharacterID], r.Name)
		}
	}
	var intros []struct {
		CharacterID int64  `gorm:"column:character_id"`
		Lang        string `gorm:"column:lang"`
		Intro       string `gorm:"column:intro"`
	}
	if err := db.Raw(`SELECT i.character_id, i.lang, i.intro FROM catalog_character_intro i
		WHERE i.character_id IN ? AND `+editspec.NotSuppressedCharacterIntroSQL("i")+`
		ORDER BY i.character_id, i.lang, `+editspec.IntroFoldOrderSQL("i.source_id", "i.provenance")+`, i.id`, ids).Scan(&intros).Error; err != nil {
		return x, fmt.Errorf("character intros: %w", err)
	}
	for _, r := range intros {
		if !slices.ContainsFunc(x.intros[r.CharacterID], func(i catalogSearch.WorkDocIntro) bool { return i.Lang == r.Lang }) {
			x.intros[r.CharacterID] = append(x.intros[r.CharacterID], catalogSearch.WorkDocIntro{Lang: r.Lang, Text: r.Intro})
		}
	}
	return x, nil
}

func EmbedCharacters(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, emb catalogSearch.Embedder, opts EmbedOptions) (EmbedStats, error) {
	run := &embedRun{idx: idx, emb: emb, uid: catalogSearch.IndexCharacterVectors, opts: opts}
	s, batch := opts.scope()
	err := forEachCharacterDoc(ctx, db, s, batch, func(docs []catalogSearch.EntityDoc) error {
		ids := make([]int64, len(docs))
		for i, d := range docs {
			ids[i] = d.CatalogID
		}
		x, err := loadCharacterExtras(db, ids)
		if err != nil {
			return err
		}
		cards := make([]card, len(docs))
		for i, d := range docs {
			cards[i] = card{
				doc: catalogSearch.NewCharacterVectorDoc(d),
				text: catalogSearch.CharacterCardText(catalogSearch.CharacterCard{
					Doc: d, Works: x.works[d.CatalogID], Traits: x.traits[d.CatalogID], Intros: x.intros[d.CatalogID],
				}),
			}
		}
		return run.page(ctx, catalogSearch.CharacterCardVersion, cards)
	})
	return run.stats, err
}
