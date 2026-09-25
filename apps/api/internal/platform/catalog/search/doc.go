package search

import (
	"math"
	"strings"
)

type EntityDoc struct {
	ID            string   `json:"id"`
	EntityType    string   `json:"entity_type"`
	NameJa        string   `json:"name_ja,omitempty"`
	NameZh        string   `json:"name_zh,omitempty"`
	NameOther     string   `json:"name_other,omitempty"`
	Latin         string   `json:"latin,omitempty"`
	AliasesJa     []string `json:"aliases_ja,omitempty"`
	AliasesZh     []string `json:"aliases_zh,omitempty"`
	AliasesOther  []string `json:"aliases_other,omitempty"`
	Sources       []string `json:"sources"`
	SourceKeys    []string `json:"source_keys"`
	PersonID      *int64   `json:"person_id,omitempty"`
	Kind          *int16   `json:"kind,omitempty"`
	ContentRating *int16   `json:"content_rating,omitempty"`
	Popularity    float64  `json:"popularity"`

	Claimed      *bool   `json:"claimed,omitempty"`
	ClaimState   string  `json:"claim_state,omitempty"`
	ContentLimit string  `json:"content_limit,omitempty"`
	OLang        string  `json:"olang,omitempty"`
	TagIDs       []int64 `json:"tag_ids,omitempty"`
	LabelIDs     []int64 `json:"label_ids,omitempty"`
	EngineIDs    []int64 `json:"engine_ids,omitempty"`
	SeriesIDs    []int64 `json:"series_ids,omitempty"`
	ReleasedOrd  int64   `json:"released_ord,omitempty"`
	UpdatedTS    int64   `json:"updated_ts,omitempty"`
	Tier         *int16  `json:"tier,omitempty"`
	Sexual       *bool   `json:"sexual,omitempty"`

	CatalogID   int64   `json:"catalog_id,omitempty"`
	Gender      *int16  `json:"gender,omitempty"`
	TraitIDs    []int64 `json:"trait_ids,omitempty"`
	TraitIDsSFW []int64 `json:"trait_ids_sfw,omitempty"`

	IntroJa    string `json:"intro_ja,omitempty"`
	IntroZh    string `json:"intro_zh,omitempty"`
	IntroOther string `json:"intro_other,omitempty"`
}

func (d *EntityDoc) SetIntro(lang, text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	switch bucket(lang) {
	case "zh":
		d.IntroZh = appendIntro(d.IntroZh, text)
	case "ja":
		d.IntroJa = appendIntro(d.IntroJa, text)
	default:
		d.IntroOther = appendIntro(d.IntroOther, text)
	}
}

func appendIntro(cur, add string) string {
	if cur == "" {
		return add
	}
	return cur + "\n" + add
}

func (d *EntityDoc) TruncateIntros() {
	d.IntroJa = truncateRunes(d.IntroJa, IntroMaxRunes)
	d.IntroZh = truncateRunes(d.IntroZh, IntroMaxRunes)
	d.IntroOther = truncateRunes(d.IntroOther, IntroMaxRunes)
}

const IntroMaxRunes = 2000

func truncateRunes(s string, max int) string {
	if s == "" {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}

func (d *EntityDoc) SetName(lang, name string) {
	switch bucket(lang) {
	case "zh":
		d.NameZh = name
	case "ja":
		d.NameJa = name
	default:
		d.NameOther = name
	}
}

func (d *EntityDoc) SetNameOrAlias(lang, name string) {
	switch bucket(lang) {
	case "zh":
		if d.NameZh == "" {
			d.NameZh = name
			return
		}
	case "ja":
		if d.NameJa == "" {
			d.NameJa = name
			return
		}
	default:
		if d.NameOther == "" {
			d.NameOther = name
			return
		}
	}
	d.AddAlias(lang, name)
}

func (d *EntityDoc) AddAlias(lang, name string) {
	switch bucket(lang) {
	case "zh":
		d.AliasesZh = append(d.AliasesZh, name)
	case "ja":
		d.AliasesJa = append(d.AliasesJa, name)
	default:
		d.AliasesOther = append(d.AliasesOther, name)
	}
}

func bucket(lang string) string {
	switch {
	case strings.HasPrefix(lang, "zh"):
		return "zh"
	case strings.HasPrefix(lang, "ja"), lang == "":
		return "ja"
	default:
		return "other"
	}
}

func Popularity(creditCount int) float64 { return math.Log1p(float64(creditCount)) }
