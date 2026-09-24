package repr

type Ref struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Source     string   `json:"source" maxLength:"64" doc:"Open vocabulary sources. Must not be used as a discriminant."`
	ExternalID string   `json:"external_id" maxLength:"256" doc:"Verbatim upstream id. Must not be used as a discriminant beyond exact match. It is an identity anchor at this entity's own granularity, not necessarily an addressable page: a credit-name vndb ref is a staff-alias id with no page of its own. Browseable URLs come from links."`
}

type Claim struct {
	_            struct{} `json:"-" additionalProperties:"true"`
	Site         string   `json:"site" maxLength:"64" pattern:"^[a-z][a-z0-9_]*$" doc:"Claiming site key."`
	SiteWorkID   string   `json:"site_work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The site's own work id. kungal and moyu number their pages by catalog id, so there it equals the work's id; other sites number their own."`
	State        string   `json:"state" enum:"live,draft,pending,declined,hidden" doc:"Claim lifecycle state."`
	ContentLimit string   `json:"content_limit" enum:"sfw,nsfw" doc:"Editorial display axis for this claim."`
}

type List[T any] struct {
	_          struct{}                 `json:"-" additionalProperties:"true"`
	Object     string                   `json:"object" enum:"list" doc:"Type discriminant. Always list."`
	Items      []T                      `json:"items" doc:"Members of this page. Empty array, never null."`
	NextCursor *string                  `json:"next_cursor,omitempty" pattern:"^cur_[A-Za-z0-9._~-]+$" maxLength:"512" doc:"Opaque keyset cursor. Omitted on the last page."`
	Total      *int64                   `json:"total,omitempty" minimum:"0" doc:"Present only when include_total=true. Same visibility gate as items."`
	Missing    *[]string                `json:"missing,omitempty" doc:"ids requested but not visible. Present only on the ids=/refs= batch lane."`
	Facets     *map[string][]FacetValue `json:"facets,omitempty" doc:"Named facet buckets. Present only when facets= is requested."`
}

type FacetValue struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Value       string   `json:"value" maxLength:"256" doc:"Token to pass back to the same filter. Must not be used as a discriminant."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Count       int      `json:"count" minimum:"0" doc:"Hits in this bucket after the same filters as items."`
}

func NewList[T any](items []T, nextCursor *string) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Object: "list", Items: items, NextCursor: nextCursor}
}

type Work struct {
	_                    struct{}                 `json:"-" additionalProperties:"true"`
	Object               string                   `json:"object" enum:"work" doc:"Type discriminant. Always work."`
	ID                   string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id. JSON string of a decimal integer."`
	Medium               string                   `json:"medium" enum:"galgame,manga,novel,anime,asmr,doujin_game,music" doc:"Never null."`
	DisplayName          string                   `json:"display_name" maxLength:"512" doc:"Primary label. Never empty. Must not be used as a discriminant."`
	Latin                *string                  `json:"latin" maxLength:"512" doc:"Romanization. null if unrecorded. Must not be used as a discriminant."`
	Localized            map[string]LocalizedText `json:"localized" doc:"BCP-47 keys, sparse. Empty object if none. Must not be used as a discriminant."`
	OLang                string                   `json:"olang" maxLength:"32" format:"bcp47" doc:"Original language, BCP-47. Open vocabulary languages."`
	ContentRating        string                   `json:"content_rating" enum:"all_ages,sensitive,r18" doc:"Age axis of the work."`
	ContentLimit         string                   `json:"content_limit" enum:"sfw,nsfw" doc:"Display axis: whether this work may be shown to a reader who has not opted into adult content. Set on every work, claimed or not, and the value the content_limit filter selects on. It is not content_rating: an r18 game whose cover art is safe is sfw, and a work whose every cover is graded explicit is nsfw whatever its rating. A claimed work carries the same value in claim.content_limit."`
	ReleaseDate          *string                  `json:"release_date" format:"date" maxLength:"10" doc:"Calendar date. null when release_status is announced, cancelled, or unknown."`
	ReleaseDatePrecision *string                  `json:"release_date_precision" enum:"day,month,year" doc:"null when release_date is null. month dates sit on the 1st; year dates sit on January 1."`
	ReleaseStatus        string                   `json:"release_status" enum:"released,dated,announced,cancelled,unknown" doc:"World state of the release, distinct from our knowledge gap."`
	Cover                *CoverSlot               `json:"cover" doc:"Selected portrait image. null if none."`
	Banner               *CoverSlot               `json:"banner" doc:"Selected landscape image. null if none. origin=screenshot marks the fallback for a work with no landscape cover."`
	Claim                *Claim                   `json:"claim" doc:"null if unclaimed. Never omitted on view=basic."`
	CreatedAt            string                   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	UpdatedAt            string                   `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	Titles               *[]WorkTitle             `json:"titles,omitempty" doc:"Present when include=titles. Empty array if none."`
	Refs                 *[]Ref                   `json:"refs,omitempty" doc:"Present when include=refs. Empty array if none."`
	Relations            *[]Relation              `json:"relations,omitempty" doc:"Present when include=relations. Empty array if none."`
	Credits              *[]CreditGroup           `json:"credits,omitempty" doc:"Present when include=credits. Empty array if none."`
	Releases             *[]Release               `json:"releases,omitempty" doc:"Present when include=releases. Empty array if none."`
	Popularity           *[]Popularity            `json:"popularity,omitempty" doc:"Present when include=popularity. Empty array if none."`
	Ratings              *[]Rating                `json:"ratings,omitempty" doc:"Present when include=ratings. Empty array if none."`
	Tags                 *[]WorkTag               `json:"tags,omitempty" doc:"Present when include=tags. Empty array if none."`
	Playtimes            *[]Playtime              `json:"playtimes,omitempty" doc:"Present when include=playtimes. Empty array if none."`
	Series               *[]WorkSeriesRef         `json:"series,omitempty" doc:"Present when include=series. Empty array if none."`
	Platforms            *[]WorkPlatform          `json:"platforms,omitempty" doc:"Present when include=platforms. Empty array if none."`
	Intros               *[]Intro                 `json:"intros,omitempty" doc:"Present when include=intros. Empty array if none."`
	Covers               *[]Cover                 `json:"covers,omitempty" doc:"Present when include=covers. Empty array if none."`
	Screenshots          *[]Screenshot            `json:"screenshots,omitempty" doc:"Present when include=screenshots. Empty array if none."`
	Characters           *[]WorkCharacter         `json:"characters,omitempty" doc:"Present when include=characters. Empty array if none."`
	Companies            *[]WorkCompany           `json:"companies,omitempty" doc:"Present when include=companies. Empty array if none."`
	Engines              *[]WorkEngineRef         `json:"engines,omitempty" doc:"Present when include=engines. Empty array if none."`
	Links                *[]WorkLink              `json:"links,omitempty" doc:"Present when include=links. Empty array if none."`
	ViaCompany           *ViaCompany              `json:"via_company,omitempty" doc:"Present only on the company_id=&company_rollup=true lane, on rows attributed through a one-hop imprint or subsidiary: the intermediate company. Absent when the work is attributed to the queried company directly."`
}

type ViaCompany struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Object      string                   `json:"object" enum:"company" doc:"Type discriminant. Always company."`
	ID          string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog company id."`
	DisplayName string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
}

func NewWork(id int64, medium, display, olang, rating, releaseStatus, created, updated string, latin *string, localized map[string]LocalizedText, releaseDate, releasePrecision *string, cover, banner *CoverSlot, claim *Claim, contentLimit string) (Work, bool) {
	if _, ok := MediumFromKey(medium); !ok {
		return Work{}, false
	}
	if localized == nil {
		localized = map[string]LocalizedText{}
	}
	return Work{
		Object: "work", ID: ID(id), Medium: medium, DisplayName: display, Latin: latin,
		Localized: localized, OLang: olang, ContentRating: rating,
		ReleaseDate: releaseDate, ReleaseDatePrecision: releasePrecision, ReleaseStatus: releaseStatus,
		Cover: cover, Banner: banner, Claim: claim, ContentLimit: contentLimit, CreatedAt: created, UpdatedAt: updated,
	}, true
}
