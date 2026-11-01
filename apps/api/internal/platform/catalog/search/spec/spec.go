package spec

import "strings"

const queryOperators = "-\"－＂"

func SanitizeQuery(q string) string {
	if strings.ContainsAny(q, queryOperators) {
		q = strings.Map(func(r rune) rune {
			if strings.ContainsRune(queryOperators, r) {
				return ' '
			}
			return r
		}, q)
	}
	return strings.TrimSpace(q)
}

var RatingSources = []string{"bangumi", "vndb", "erogamescape", "dlsite", "nextmoe"}

type OLang struct {
	All    bool
	Values []string
}

type WorksFilter struct {
	DocID            string
	ContentRatingNot *int16
	ContentRating    *int16
	Claimed          *bool
	ClaimStates      []string
	ClaimStateNot    string
	DisplayLimits    []string
	TagIDs           []int64
	ExcludeTagIDs    []int64
	LabelID          int64
	EngineID         int64
	SeriesID         int64
	ReleasedAfter    int64
	ReleasedBefore   int64
	OLang            OLang
}

type WorksQuery struct {
	Q           string
	Filter      WorksFilter
	SortLane    string
	FacetAttrs  []string
	Page        int
	Limit       int
	SearchIntro bool
}

type EntityQuery struct {
	Q                string
	Page             int
	Limit            int
	Locales          []string
	ContentRatingNot *int16
	SexualNot        *bool
}

type CharacterQuery struct {
	Q             string
	Page          int
	Limit         int
	Sort          string
	TraitIDs      []int64
	TraitMatchAny bool
	NSFW          bool
	Genders       []int16
}

const RankConsensus = "consensus"

type RankingQuery struct {
	Filter      WorksFilter
	Platforms   []string
	Circulation string
	PlaytimeMin int
	PlaytimeMax int

	By              string
	Field           string
	Asc             bool
	Sources         []string
	MinSources      int
	MinVotes        map[string]int
	MaxVotes        map[string]int
	TotalVotes      int
	TotalMax        int
	Min             *float64
	Max             *float64
	Z               float64
	DivergenceVotes map[string]float64

	FacetAttrs []string
	Page       int
	Limit      int
}
