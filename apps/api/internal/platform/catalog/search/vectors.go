package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"api/internal/platform/catalog/search/osengine"
	"api/internal/platform/catalog/search/spec"
)

type (
	VectorStamp = osengine.VectorStamp
	VectorHit   = osengine.VectorHit
)

func (i *Indexer) EnsureVectorIndex(ctx context.Context, uid string, dims int) error {
	return i.engine.EnsureVectorIndex(ctx, uid, dims)
}

func (i *Indexer) UpsertVectors(ctx context.Context, uid string, docs []VectorDoc) error {
	if len(docs) == 0 {
		return nil
	}
	return i.engine.UpsertVectors(ctx, uid, docs)
}

func (i *Indexer) UpdateVectorFilters(ctx context.Context, uid string, docs []VectorDoc) error {
	if len(docs) == 0 {
		return nil
	}
	return i.engine.UpdateVectorFilters(ctx, uid, docs)
}

func (i *Indexer) VectorStamps(ctx context.Context, uid string, ids []string) (map[string]VectorStamp, error) {
	return i.engine.VectorStamps(ctx, uid, ids)
}

func (i *Indexer) SearchWorkVectors(ctx context.Context, uid string, vector []float32, k int, f spec.WorksFilter) ([]VectorHit, error) {
	return i.engine.SearchWorkVectors(ctx, uid, vector, k, f)
}

func (i *Indexer) SearchCharacterVectors(ctx context.Context, uid string, vector []float32, k int, q spec.CharacterQuery) ([]VectorHit, error) {
	return i.engine.SearchCharacterVectors(ctx, uid, vector, k, q)
}

const (
	WorkCardVersion      = "work-card-v1"
	CharacterCardVersion = "character-card-v1"

	cardIntroRunes   = 600
	cardMaxTags      = 40
	cardMaxTitleRows = 8
	cardMaxWorks     = 5
)

type VectorDoc struct {
	ID         string
	CatalogID  int64
	EmbedModel string
	TextHash   string
	Vector     []float32
	Filters    map[string]any
}

func (d VectorDoc) MarshalJSON() ([]byte, error) {
	m := maps.Clone(d.Filters)
	if m == nil {
		m = map[string]any{}
	}
	m["id"], m["catalog_id"], m["embed_model"], m["text_hash"] = d.ID, d.CatalogID, d.EmbedModel, d.TextHash
	if d.Vector != nil {
		m["vector"] = d.Vector
	}
	return json.Marshal(m)
}

// Filter maps carry every key with an explicit empty value: the filter-only
// refresh is a partial update, and a key left out keeps its old value — a work
// that lost its last tag would stay findable by that tag.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func idList(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

func NewWorkVectorDoc(d EntityDoc) VectorDoc {
	var released any
	if d.ReleasedOrd != 0 {
		released = d.ReleasedOrd
	}
	return VectorDoc{ID: d.ID, CatalogID: d.CatalogID, Filters: map[string]any{
		"content_rating": d.ContentRating,
		"claimed":        d.Claimed,
		"claim_state":    nullable(d.ClaimState),
		"content_limit":  nullable(d.ContentLimit),
		"olang":          nullable(d.OLang),
		"tag_ids":        idList(d.TagIDs),
		"label_ids":      idList(d.LabelIDs),
		"engine_ids":     idList(d.EngineIDs),
		"series_ids":     idList(d.SeriesIDs),
		"released_ord":   released,
	}}
}

func NewCharacterVectorDoc(d EntityDoc) VectorDoc {
	return VectorDoc{ID: d.ID, CatalogID: d.CatalogID, Filters: map[string]any{
		"gender":        d.Gender,
		"trait_ids":     idList(d.TraitIDs),
		"trait_ids_sfw": idList(d.TraitIDsSFW),
	}}
}

func WorkCardText(in WorkDocInput, tagNames []string) string {
	var b strings.Builder
	b.WriteString(in.DisplayName)
	seen := map[string]bool{in.DisplayName: true}
	rows := 0
	for _, t := range in.Titles {
		if seen[t.Title] || rows >= cardMaxTitleRows {
			continue
		}
		seen[t.Title] = true
		rows++
		b.WriteString(" / ")
		b.WriteString(t.Title)
	}
	if len(tagNames) > 0 {
		if len(tagNames) > cardMaxTags {
			tagNames = tagNames[:cardMaxTags]
		}
		b.WriteString("\n")
		b.WriteString(strings.Join(tagNames, ", "))
	}
	writeCardIntros(&b, in.Intros)
	return b.String()
}

func writeCardIntros(b *strings.Builder, intros []WorkDocIntro) {
	for _, want := range []string{"zh", "ja", "other"} {
		for _, intro := range intros {
			if bucket(intro.Lang) != want || strings.TrimSpace(intro.Text) == "" {
				continue
			}
			b.WriteString("\n")
			b.WriteString(truncateRunes(strings.TrimSpace(intro.Text), cardIntroRunes))
			break
		}
	}
}

type CharacterCard struct {
	Doc    EntityDoc
	Works  []string
	Traits []string
	Intros []WorkDocIntro
}

func CharacterCardText(c CharacterCard) string {
	var b strings.Builder
	names := []string{}
	for _, n := range []string{c.Doc.NameJa, c.Doc.NameZh, c.Doc.NameOther, c.Doc.Latin} {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	for _, list := range [][]string{c.Doc.AliasesJa, c.Doc.AliasesZh, c.Doc.AliasesOther} {
		for _, n := range list {
			if len(names) >= cardMaxTitleRows {
				break
			}
			if n != "" && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	b.WriteString(strings.Join(names, " / "))
	if len(c.Works) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(c.Works[:min(len(c.Works), cardMaxWorks)], " / "))
	}
	if len(c.Traits) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(c.Traits[:min(len(c.Traits), cardMaxTags)], ", "))
	}
	writeCardIntros(&b, c.Intros)
	return b.String()
}

func CardHash(version, model, text string) string {
	sum := sha256.Sum256([]byte(version + "\x00" + model + "\x00" + text))
	return hex.EncodeToString(sum[:16])
}
