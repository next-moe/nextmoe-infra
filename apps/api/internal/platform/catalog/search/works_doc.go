package search

import (
	"math"
	"slices"
	"strconv"

	"api/internal/platform/catalog/model"
)

func GuessLang(s string) string {
	hasHan := false
	for _, r := range s {
		if (r >= 'ぁ' && r <= 'ん') || (r >= 'ァ' && r <= 'ヶ') || r == 'ー' {
			return "ja"
		}
		if r >= 0x4E00 && r <= 0x9FFF {
			hasHan = true
		}
	}
	if hasHan {
		return "zh"
	}
	return "en"
}

type WorkDocTitle struct {
	Lang  string
	Title string
	Latin string
}

type WorkDocIntro struct {
	Lang string
	Text string
}

type WorkDocInput struct {
	ID            int64
	DisplayName   string
	OLang         string
	ContentRating int16
	Claimed       bool
	ClaimState    string
	ContentLimit  string
	ReleasedOrd   int64
	UpdatedTS     int64
	Popularity    float64
	Sources       []string
	SourceKeys    []string
	Titles        []WorkDocTitle
	TagIDs        []int64
	LabelIDs      []int64
	EngineIDs     []int64
	SeriesIDs     []int64
	Intros        []WorkDocIntro
	Ratings       map[string]float64
}

var alreadyBayesianRatingSources = []string{"vndb", "nextmoe"}

func RatingSortScore(source string, score float64, votes int, sourceMean float64) float64 {
	if slices.Contains(alreadyBayesianRatingSources, source) {
		return score
	}
	c := float64(model.RatingPriorVotes)
	return (c*sourceMean + float64(votes)*score) / (c + float64(votes))
}

func BuildWorkDoc(in WorkDocInput) EntityDoc {
	rating, claimed := in.ContentRating, in.Claimed
	// Every work document must carry a claim_state. ClaimState is omitempty, and
	// Meilisearch answers `claim_state != 'hidden'` with zero hits and no error
	// when no document in the index has the attribute at all — one state-less
	// document is a hole, an index of them empties the whole works search. An
	// unclaimed work is "none", which is what model.ClaimStateKey returns for it.
	claimState := in.ClaimState
	if claimState == "" {
		claimState = model.ClaimStateKeyNone
	}
	d := EntityDoc{
		ID: WorkDocID(in.ID), EntityType: "work", CatalogID: in.ID,
		Sources: in.Sources, SourceKeys: in.SourceKeys, Rating: in.Ratings,
		ContentRating: &rating, Popularity: in.Popularity,
		Claimed: &claimed, ClaimState: claimState, ContentLimit: in.ContentLimit, OLang: in.OLang,
		TagIDs: in.TagIDs, LabelIDs: in.LabelIDs,
		EngineIDs: in.EngineIDs, SeriesIDs: in.SeriesIDs,
		ReleasedOrd: in.ReleasedOrd, UpdatedTS: in.UpdatedTS,
	}
	seen := map[string]bool{in.DisplayName: true}
	d.SetNameOrAlias(GuessLang(in.DisplayName), in.DisplayName)
	for _, t := range in.Titles {
		if d.Latin == "" && t.Latin != "" {
			d.Latin = t.Latin
		}
		if seen[t.Title] {
			continue
		}
		seen[t.Title] = true
		lang := t.Lang
		if lang == "" {
			lang = GuessLang(t.Title)
		}
		d.SetNameOrAlias(lang, t.Title)
	}
	for _, intro := range in.Intros {
		d.SetIntro(intro.Lang, intro.Text)
	}
	d.TruncateIntros()
	return d
}

type TagDocInput struct {
	ID         int64
	Name       string
	Tier       int16
	Kind       int16
	WorkCount  int
	Sources    []string
	SourceKeys []string
	Aliases    []string
}

func BuildTagDoc(in TagDocInput) EntityDoc {
	tier, kind := in.Tier, in.Kind
	d := EntityDoc{
		ID: TagDocID(in.ID), EntityType: "tag",
		Sources: in.Sources, SourceKeys: in.SourceKeys,
		Tier: &tier, Kind: &kind,
		Popularity: math.Log1p(float64(in.WorkCount)),
	}
	d.SetName(GuessLang(in.Name), in.Name)
	for _, a := range in.Aliases {
		if a != in.Name {
			d.AddAlias(GuessLang(a), a)
		}
	}
	return d
}

func TagDocID(tagID int64) string { return "t" + strconv.FormatInt(tagID, 10) }
