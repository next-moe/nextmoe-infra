package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func (i *Indexer) SearchWorkVectors(ctx context.Context, vector []float32, k int, f spec.WorksFilter) ([]VectorHit, error) {
	return i.engine.SearchWorkVectors(ctx, vector, k, f)
}

const (
	workCardVersion  = "work-card-v1"
	cardIntroRunes   = 600
	cardMaxTags      = 40
	cardMaxTitleRows = 8
)

type VectorDoc struct {
	ID         string    `json:"id"`
	CatalogID  int64     `json:"catalog_id"`
	EmbedModel string    `json:"embed_model"`
	TextHash   string    `json:"text_hash"`
	Vector     []float32 `json:"vector,omitempty"`

	ContentRating *int16  `json:"content_rating,omitempty"`
	Claimed       *bool   `json:"claimed,omitempty"`
	ClaimState    string  `json:"claim_state,omitempty"`
	ContentLimit  string  `json:"content_limit,omitempty"`
	OLang         string  `json:"olang,omitempty"`
	TagIDs        []int64 `json:"tag_ids,omitempty"`
	LabelIDs      []int64 `json:"label_ids,omitempty"`
	EngineIDs     []int64 `json:"engine_ids,omitempty"`
	SeriesIDs     []int64 `json:"series_ids,omitempty"`
	ReleasedOrd   int64   `json:"released_ord,omitempty"`
}

func NewWorkVectorDoc(d EntityDoc) VectorDoc {
	return VectorDoc{
		ID: d.ID, CatalogID: d.CatalogID,
		ContentRating: d.ContentRating, Claimed: d.Claimed, ClaimState: d.ClaimState,
		ContentLimit: d.ContentLimit, OLang: d.OLang,
		TagIDs: d.TagIDs, LabelIDs: d.LabelIDs, EngineIDs: d.EngineIDs, SeriesIDs: d.SeriesIDs,
		ReleasedOrd: d.ReleasedOrd,
	}
}

func (d VectorDoc) FilterFields() map[string]any {
	str := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	ids := func(v []int64) []int64 {
		if v == nil {
			return []int64{}
		}
		return v
	}
	var released any
	if d.ReleasedOrd != 0 {
		released = d.ReleasedOrd
	}
	return map[string]any{
		"content_rating": d.ContentRating,
		"claimed":        d.Claimed,
		"claim_state":    str(d.ClaimState),
		"content_limit":  str(d.ContentLimit),
		"olang":          str(d.OLang),
		"tag_ids":        ids(d.TagIDs),
		"label_ids":      ids(d.LabelIDs),
		"engine_ids":     ids(d.EngineIDs),
		"series_ids":     ids(d.SeriesIDs),
		"released_ord":   released,
	}
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
	for _, want := range []string{"zh", "ja", "other"} {
		for _, intro := range in.Intros {
			if bucket(intro.Lang) != want || strings.TrimSpace(intro.Text) == "" {
				continue
			}
			b.WriteString("\n")
			b.WriteString(truncateRunes(strings.TrimSpace(intro.Text), cardIntroRunes))
			break
		}
	}
	return b.String()
}

func CardHash(model, text string) string {
	sum := sha256.Sum256([]byte(workCardVersion + "\x00" + model + "\x00" + text))
	return hex.EncodeToString(sum[:16])
}
