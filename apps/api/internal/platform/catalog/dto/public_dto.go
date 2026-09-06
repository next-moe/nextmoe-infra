package dto

import "time"

type PublicClaimedBy struct {
	Site         string `json:"site"`
	WorkID       int64  `json:"work_id"`
	State        string `json:"state" doc:"live|draft|pending|declined|hidden"`
	ContentLimit string `json:"content_limit" doc:"sfw|nsfw"`
}

type PublicWorkBrief struct {
	ID            int64                          `json:"id"`
	Medium        string                         `json:"medium"`
	DisplayName   string                         `json:"display_name"`
	ContentRating string                         `json:"content_rating"`
	OLang         string                         `json:"olang"`
	Created       string                         `json:"created"`
	Updated       string                         `json:"updated"`
	ClaimedBy     *PublicClaimedBy               `json:"claimed_by"`
	Latin         string                         `json:"latin,omitempty" doc:"romanisation of display_name, from the title row display_name was taken from; absent when that row records none"`
	Localized     map[string]PublicLocalizedName `json:"localized" doc:"preferred title per locale, keyed by canonically-cased BCP-47 tag; {} when none. SPARSE by design — render localized[yourLocale] ?? display_name ?? latin, never a blank"`
}

type PublicCatalogTitle struct {
	Lang    string `json:"lang"`
	Title   string `json:"title"`
	Latin   string `json:"latin,omitempty"`
	Kind    string `json:"kind"`
	Machine bool   `json:"machine,omitempty" doc:"true when the title is a machine translation, not a title a source published"`
}

type PublicCatalogRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

type PublicImageMeta struct {
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Thumbhash string `json:"thumbhash,omitempty"`
	Sexual    *int16 `json:"sexual,omitempty" doc:"0 safe / 1 suggestive / 2 explicit; absent = not yet assessed"`
}

type PublicRelation struct {
	RelationType string          `json:"relation_type"`
	Phrase       string          `json:"phrase"`
	Work         PublicWorkBrief `json:"work"`
}

type PublicCreditItem struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang"`
	Latin       string                         `json:"latin,omitempty"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name ?? latin"`
	CharacterID int64                          `json:"character_id,omitempty"`
	Character   string                         `json:"character,omitempty"`
	LabelID     int64                          `json:"label_id,omitempty"`
	Label       string                         `json:"label,omitempty"`
	Source      string                         `json:"source,omitempty"`
	Identity    string                         `json:"identity" doc:"Opaque row identity for catalog.work.credits.suppressed; echo it back, never rebuild it"`
}

type PublicCreditGroup struct {
	RoleKey  string             `json:"role_key"`
	RoleName string             `json:"role_name"`
	Credits  []PublicCreditItem `json:"credits"`
}

type PublicCatalogWork struct {
	ID             int64                          `json:"id"`
	Medium         string                         `json:"medium"`
	DisplayName    string                         `json:"display_name"`
	Latin          string                         `json:"latin,omitempty" doc:"romanisation of display_name, from the title row display_name was taken from; absent when that row records none"`
	Localized      map[string]PublicLocalizedName `json:"localized" doc:"preferred title per locale, keyed by canonically-cased BCP-47 tag; {} when none. SPARSE by design — render localized[yourLocale] ?? display_name ?? latin, never a blank"`
	OLang          string                         `json:"olang"`
	ContentRating  string                         `json:"content_rating"`
	ReleaseDate    *string                        `json:"release_date"`
	Created        string                         `json:"created"`
	Updated        string                         `json:"updated"`
	Titles         []PublicCatalogTitle           `json:"titles"`
	Refs           []PublicCatalogRef             `json:"refs"`
	ClaimedBy      *PublicClaimedBy               `json:"claimed_by"`
	Relations      []PublicRelation               `json:"relations,omitempty"`
	Credits        []PublicCreditGroup            `json:"credits,omitempty"`
	Releases       []PublicRelease                `json:"releases"`
	Popularity     []PublicPopularity             `json:"popularity"`
	Ratings        []PublicRating                 `json:"ratings"`
	Tags           []PublicTag                    `json:"tags"`
	Playtimes      []PublicPlaytime               `json:"playtimes"`
	Series         []PublicSeries                 `json:"series"`
	Platforms      []PublicPlatform               `json:"platforms"`
	Intros         []PublicIntro                  `json:"intros"`
	Covers         []PublicCover                  `json:"covers"`
	CoverSlots     *PublicWorkCoverSlots          `json:"cover_slots"`
	Screenshots    []PublicScreenshot             `json:"screenshots"`
	Characters     []PublicRosterCharacter        `json:"characters"`
	Labels         []PublicWorkLabel              `json:"labels"`
	Engines        []PublicWorkEngine             `json:"engines"`
	Links          []PublicWorkLink               `json:"links"`
	SeriesSiblings []PublicWorkBrief              `json:"series_siblings"`
}

type PublicLookupData struct {
	Work      *PublicWorkBrief `json:"work"`
	ClaimedBy *PublicClaimedBy `json:"claimed_by"`
	Name      *PublicName      `json:"name,omitempty"`
	Character *PublicCharacter `json:"character,omitempty"`
	Label     *PublicLabel     `json:"label,omitempty"`
}

type PublicLookupPair struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Type       string `json:"type,omitempty"`
}

type PublicLookupBatchRequest struct {
	Items []PublicLookupPair `json:"items"`
}

type PublicLookupBatchItem struct {
	Source     string           `json:"source"`
	ExternalID string           `json:"external_id"`
	Type       string           `json:"type"`
	Work       *PublicWorkBrief `json:"work"`
	ClaimedBy  *PublicClaimedBy `json:"claimed_by"`
	Name       *PublicName      `json:"name,omitempty"`
	Character  *PublicCharacter `json:"character,omitempty"`
	Label      *PublicLabel     `json:"label,omitempty"`
}

type PublicLookupBatchData struct {
	Items []PublicLookupBatchItem `json:"items"`
}

type PublicResolveRequest struct {
	EntityType string  `json:"entity_type"`
	IDs        []int64 `json:"ids"`
}

type PublicResolveData struct {
	Mappings   map[string]int64 `json:"mappings"`
	Redirected []int64          `json:"redirected"`
}

type PublicRedirectItem struct {
	EntityType string    `json:"entity_type"`
	OldID      int64     `json:"old_id"`
	CurrentID  int64     `json:"current_id"`
	MergedAt   time.Time `json:"merged_at"`
}

type PublicRedirectsData struct {
	Items      []PublicRedirectItem `json:"items"`
	NextCursor *string              `json:"next_cursor"`
}

type PublicMovedData struct {
	EntityType string `json:"entity_type"`
	ID         int64  `json:"id"`
	CurrentID  int64  `json:"current_id"`
}

type PublicLocalizedName struct {
	Value   string `json:"value"`
	Kind    string `json:"kind" doc:"which vocabulary the elected row was drawn from, and they do not overlap: an ENTITY name (character, label, credit name, and every projection of them) is translation|spelling_variant, taken from the alias tables; a WORK title is official|alias|abbreviation, taken from catalog_work_title. Read it per entity type — a works localized{} never says translation"`
	Machine bool   `json:"machine,omitempty" doc:"true = machine-translated fill-in, present only when this locale has no source-provenance name (a source name always wins the slot); render it like any name but do not treat it as authoritative"`
}

type PublicAlias struct {
	Value   string `json:"value"`
	Lang    string `json:"lang,omitempty" doc:"BCP-47 language of this spelling; empty when unrecorded"`
	Kind    string `json:"kind" doc:"translation|spelling_variant"`
	Machine bool   `json:"machine,omitempty"`
}

type PublicIntro struct {
	Lang    string `json:"lang"`
	Intro   string `json:"intro"`
	Source  string `json:"source"`
	Machine bool   `json:"machine,omitempty"`
}

type PublicSiblingName struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang,omitempty" doc:"BCP-47 language of display_name; empty when unrecorded"`
	Latin       string                         `json:"latin,omitempty"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name ?? latin"`
}

type PublicNameRole struct {
	RoleKey     string `json:"role_key"`
	RoleName    string `json:"role_name"`
	CharacterID int64  `json:"character_id,omitempty"`
	Character   string `json:"character,omitempty"`
	Identity    string `json:"identity" doc:"Opaque row identity for catalog.work.credits.suppressed; echo it back, never rebuild it"`
}

type PublicNameCredit struct {
	Work  PublicWorkBrief  `json:"work"`
	Roles []PublicNameRole `json:"roles"`
}

type PublicName struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang,omitempty" doc:"BCP-47 language of display_name; empty when unrecorded"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none. SPARSE by design — render localized[yourLocale] ?? display_name ?? latin, never a blank"`
	Latin       string                         `json:"latin,omitempty"`
	PersonID    int64                          `json:"person_id,omitempty"`
	Siblings    []PublicSiblingName            `json:"siblings"`
	Aliases     []PublicAlias                  `json:"aliases" doc:"alternate spellings of THIS credited name; deduplicated, the name itself excluded, [] when it has none"`
	PhotoHash   string                         `json:"photo_hash" doc:"person photo content hash in the image service; \"\" = no photo (or the person link is hidden)"`
	PhotoMeta   *PublicImageMeta               `json:"photo_meta,omitempty" doc:"dimensions, thumbhash and grading of photo_hash; absent when there is no photo or the image service did not answer"`
	Gender      *int16                         `json:"gender,omitempty" doc:"person gender code; absent = unknown, orphan or hidden link"`
	BirthY      *int16                         `json:"birth_y,omitempty" doc:"fuzzy birth date, year; absent = not recorded at this precision"`
	BirthM      *int16                         `json:"birth_m,omitempty" doc:"fuzzy birth date, month"`
	BirthD      *int16                         `json:"birth_d,omitempty" doc:"fuzzy birth date, day"`
	Links       []PublicPersonLink             `json:"links"`
	Intros      []PublicIntro                  `json:"intros"`
	Refs        []PublicCatalogRef             `json:"refs"`
	Credits     []PublicNameCredit             `json:"credits,omitempty"`
	NextOffset  *int                           `json:"next_offset,omitempty"`
}

// No `identity` here — see dto.WorkCharacterVA.
type PublicVoiceName struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang"`
	Latin       string                         `json:"latin,omitempty"`
	PersonID    int64                          `json:"person_id,omitempty" doc:"catalog person this credited name resolves to; absent when unlinked or the link is hidden"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name ?? latin"`
}

type PublicCharacterWork struct {
	Work     PublicWorkBrief   `json:"work"`
	Kind     string            `json:"kind" doc:"roster appearance strength on this work: main|secondary|appears|unknown (unknown also when reached only through a voice credit)"`
	Spoiler  int16             `json:"spoiler" doc:"roster appearance spoiler level on this work: 0=none 1=minor 2=major (0 also when reached only through a voice credit)"`
	Identity string            `json:"identity,omitempty" doc:"Opaque row identity for catalog.work.roster.suppressed on THIS work; echo it back, never rebuild it. Absent when the work is reached only through a voice credit"`
	Voices   []PublicVoiceName `json:"voices"`
}

type PublicCharacter struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang,omitempty" doc:"BCP-47 language of display_name; empty when unrecorded"`
	Aliases     []PublicAlias                  `json:"aliases" doc:"alternate spellings; deduplicated, the display name excluded, [] when it has none"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none. SPARSE by design — render localized[yourLocale] ?? display_name ?? latin, never a blank"`
	Latin       string                         `json:"latin,omitempty"`
	Refs        []PublicCatalogRef             `json:"refs"`
	Works       []PublicCharacterWork          `json:"works,omitempty"`
	NextOffset  *int                           `json:"next_offset,omitempty"`
	Traits      []PublicCharacterTrait         `json:"traits"`
	Intros      []PublicIntro                  `json:"intros"`
	Image       string                         `json:"image,omitempty"`
	ImageMeta   *PublicImageMeta               `json:"image_meta,omitempty" doc:"dimensions, thumbhash and grading of image; absent when there is no image or the image service did not answer"`
	Figure      string                         `json:"figure,omitempty"`
	FigureMeta  *PublicImageMeta               `json:"figure_meta,omitempty" doc:"dimensions, thumbhash and grading of figure; absent when there is no figure or the image service did not answer"`
}

type PublicLabelWork struct {
	Work PublicWorkBrief `json:"work"`
	Kind string          `json:"kind"`
}

type PublicLabelLink struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

type PublicLabelRelation struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name"`
	Relation    string                         `json:"relation" doc:"parent|subsidiary|imprint|imprint_of|spawned|origin|succeeded_by|formerly — reads \"<display_name> is the <relation> of this label\""`
}

type PublicLabelVia struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name"`
}

type PublicPersonLink struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

type PublicLabel struct {
	ID               int64                          `json:"id"`
	DisplayName      string                         `json:"display_name"`
	Latin            string                         `json:"latin,omitempty"`
	Kind             string                         `json:"kind"`
	Lang             string                         `json:"lang,omitempty"`
	Aliases          []PublicAlias                  `json:"aliases"`
	Localized        map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none. SPARSE by design — render localized[yourLocale] ?? display_name ?? latin, never a blank"`
	WorkCount        int                            `json:"work_count"`
	ImprintWorkCount int                            `json:"imprint_work_count" doc:"works reachable one hop down through imprints/subsidiaries and NOT attributed to this label itself; follow it with works?label_id=<id>&label_rollup=1"`
	LogoHash         string                         `json:"logo_hash" doc:"brand logo content hash in the image service; \"\" = this label has no logo"`
	LogoMeta         *PublicImageMeta               `json:"logo_meta,omitempty" doc:"dimensions, thumbhash and grading of logo_hash; absent when there is no logo or the image service did not answer"`
	Refs             []PublicCatalogRef             `json:"refs"`
	Intros           []PublicIntro                  `json:"intros"`
	Links            []PublicLabelLink              `json:"links"`
	Relations        []PublicLabelRelation          `json:"relations"`
	Works            []PublicLabelWork              `json:"works,omitempty"`
	NextOffset       *int                           `json:"next_offset,omitempty"`
}

type PublicEntityHit struct {
	ID            int64                          `json:"id"`
	EntityType    string                         `json:"entity_type"`
	DisplayName   string                         `json:"display_name"`
	Latin         string                         `json:"latin,omitempty"`
	Localized     map[string]PublicLocalizedName `json:"localized,omitempty" doc:"names/characters/labels/works hits: preferred name per locale, same election as the entity's detail face — render localized[yourLocale] ?? display_name. Absent when the entity has no localized name (tags hits never carry it)"`
	Sources       []string                       `json:"sources"`
	ContentRating string                         `json:"content_rating,omitempty"`
	Tier          string                         `json:"tier,omitempty"`
	Kind          string                         `json:"kind,omitempty"`
}

type PublicEntitySearchData struct {
	Items []PublicEntityHit `json:"items"`
	Total int64             `json:"total"`
}

type PublicPopularity struct {
	Source string `json:"source"`
	Metric string `json:"metric" doc:"downloads|wishlist|reviews|bgm_wish|bgm_collect|bgm_doing|bgm_on_hold|bgm_dropped"`
	Value  int64  `json:"value"`
}

type PublicRating struct {
	Source       string         `json:"source"`
	Score        float64        `json:"score"`
	VoteCount    int            `json:"vote_count"`
	Rank         *int           `json:"rank,omitempty"`
	Distribution []RatingBucket `json:"distribution,omitempty" doc:"vote histogram on the source-native scale, ascending, sparse (an absent bucket has no votes). Work-detail responses only (works/{id} and works/{id}/ratings) — the works-list ratings block never carries it. All four sources publish it: bangumi 1-10, dlsite 1-5, vndb 1-10, erogamescape 0-100 in decile steps (0, 10, ... 100). The bars do not share one denominator: bangumi and dlsite publish the histogram together with the aggregate, so their bars sum to vote_count; erogamescape bars are computed from the reviews mirror, which syncs on a cursor independent of the row score and vote_count come from, so sum-of-bars is the histogram's own denominator and need not equal vote_count; vndb bars come from the public votes dump, which omits votes on private lists, so they sum to at most vote_count"`
	Stats        *RatingStats   `json:"stats,omitempty" doc:"spread of the same vote population as score, work-detail responses only (works/{id} and works/{id}/ratings). Carried by erogamescape (average, stdev, min and max, on its 0-100 scale) and by vndb (average only — the plain mean beside the bayesian-smoothed rating that score holds). bangumi and dlsite carry no stats"`
}

type PublicTag struct {
	Name        string `json:"name"`
	Count       int    `json:"count,omitempty"`
	Source      string `json:"source"`
	CanonicalID int64  `json:"canonical_id,omitempty"`
	Tier        string `json:"tier,omitempty" doc:"core|longtail|hidden"`
	Kind        string `json:"kind,omitempty" doc:"content|meta"`
	Spoiler     int16  `json:"spoiler" doc:"0=none 1=minor 2=major"`
	Sexual      bool   `json:"sexual"`
	WorkCount   *int   `json:"work_count,omitempty"`
}

type PublicPlaytime struct {
	Source    string `json:"source"`
	Minutes   int    `json:"minutes"`
	VoteCount int    `json:"vote_count"`
}

type PublicSeries struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	MemberCount int    `json:"member_count"`
}

type PublicPlatform struct {
	Platform string `json:"platform"`
	Source   string `json:"source"`
}

type PublicCover struct {
	ID             int64  `json:"id,omitempty"`
	VoteCount      int    `json:"vote_count"`
	URL            string `json:"url"`
	Kind           string `json:"kind,omitempty"`
	PortraitPinned bool   `json:"portrait_pinned"`
	Sexual         int16  `json:"sexual"`
	Violence       int16  `json:"violence"`
	Source         string `json:"source"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Thumbhash      string `json:"thumbhash,omitempty"`
}

type PublicScreenshot struct {
	URL       string `json:"url"`
	Caption   string `json:"caption,omitempty"`
	Sexual    int16  `json:"sexual"`
	Violence  int16  `json:"violence"`
	Source    string `json:"source"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Thumbhash string `json:"thumbhash,omitempty"`
}

// No `identity` here — see dto.WorkCharacterVA.
type PublicRosterVoice struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Lang        string                         `json:"lang,omitempty"`
	Latin       string                         `json:"latin,omitempty"`
	PersonID    int64                          `json:"person_id,omitempty" doc:"catalog person this credited name resolves to; absent when unlinked or the link is hidden"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name ?? latin"`
}

type PublicRosterCharacter struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Latin       string                         `json:"latin,omitempty"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, same election as the character detail face (machine fill-ins flagged); {} when none — render localized[yourLocale] ?? display_name ?? latin"`
	Kind        string                         `json:"kind" doc:"main|secondary|appears|unknown"`
	Spoiler     int16                          `json:"spoiler" doc:"0=none 1=minor 2=major"`
	Image       string                         `json:"image,omitempty"`
	ImageMeta   *PublicImageMeta               `json:"image_meta,omitempty" doc:"dimensions, thumbhash and grading of image; absent when there is no image or the image service did not answer"`
	Figure      string                         `json:"figure,omitempty"`
	FigureMeta  *PublicImageMeta               `json:"figure_meta,omitempty" doc:"dimensions, thumbhash and grading of figure; absent when there is no figure or the image service did not answer"`
	Identity    string                         `json:"identity,omitempty" doc:"Opaque row identity for catalog.work.roster.suppressed; echo it back, never rebuild it. Present when the character is on the roster; ABSENT when it appears only through a voice credit, in which case kind is unknown and spoiler is 0"`
	Voices      []PublicRosterVoice            `json:"voices"`
}

type PublicWorkLabel struct {
	ID          int64                          `json:"id"`
	DisplayName string                         `json:"display_name"`
	Localized   map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name"`
	LabelKind   string                         `json:"label_kind"`
	Kind        string                         `json:"kind" doc:"primary attribution nature: circle|publisher|developer|brand. When the company acted in several capacities this is the most identifying one (brand, circle, developer, publisher in that order) and kinds[] carries them all."`
	Kinds       []string                       `json:"kinds" doc:"every capacity this company acted in, sorted; always at least one"`
	Lang        string                         `json:"lang,omitempty"`
	WorkCount   int                            `json:"work_count"`
	LogoHash    string                         `json:"logo_hash" doc:"brand logo content hash in the image service; \"\" = this label has no logo"`
}

type PublicWorkEngine struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	WorkCount int    `json:"work_count"`
}

type PublicWorkLink struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

type PublicRelease struct {
	ID        int64              `json:"id"`
	Kind      string             `json:"kind" doc:"default|digital|physical|trial|patch"`
	Date      *string            `json:"date"`
	Title     string             `json:"title,omitempty"`
	Lang      string             `json:"lang,omitempty"`
	Platform  string             `json:"platform,omitempty"`
	Platforms []string           `json:"platforms,omitempty"`
	Refs      []PublicCatalogRef `json:"refs"`
	Labels    []PublicWorkLabel  `json:"labels"`
}

type PublicCharacterTrait struct {
	ID             int64                          `json:"id"`
	Name           string                         `json:"name"`
	Group          string                         `json:"group,omitempty"`
	Localized      map[string]PublicLocalizedName `json:"localized" doc:"preferred trait name per locale, keyed by canonically-cased BCP-47 tag; {} when the vocabulary row has no localized name. Today the only key is zh-Hans — render localized[yourLocale] ?? name, never a blank"`
	GroupLocalized map[string]PublicLocalizedName `json:"group_localized" doc:"same shape for the trait's root group; {} for a root trait or a group with no localized name — render group_localized[yourLocale] ?? group"`
	Spoiler        int16                          `json:"spoiler" doc:"0=none 1=minor 2=major"`
	Sexual         bool                           `json:"sexual"`
	Lie            bool                           `json:"lie"`
}

type PublicWorkListItem struct {
	ID            int64            `json:"id"`
	Medium        string           `json:"medium"`
	DisplayName   string           `json:"display_name"`
	ContentRating string           `json:"content_rating"`
	OLang         string           `json:"olang"`
	ReleaseDate   *string          `json:"release_date"`
	ClaimedBy     *PublicClaimedBy `json:"claimed_by"`
	Cover         string           `json:"cover,omitempty"`
	Created       string           `json:"created"`
	Updated       string           `json:"updated"`
	ViaLabel      *PublicLabelVia  `json:"via_label,omitempty"`

	Latin     string                         `json:"latin,omitempty" doc:"include=titles only: romanisation of display_name, from the title row display_name was taken from"`
	Localized map[string]PublicLocalizedName `json:"localized,omitempty" doc:"include=titles only, and absent (never {}) when this work has no localized title — the block is include-gated exactly like titles, so its absence means \"not requested or none\", not \"none\". SPARSE by design — render localized[yourLocale] ?? display_name ?? latin"`

	Intros  []PublicIntro         `json:"intros,omitempty" doc:"include=intros only: one intro per language, same shape and election as the work-detail intros block"`
	Labels  []PublicWorkLabel     `json:"labels,omitempty"`
	Ratings []PublicRating        `json:"ratings,omitempty"`
	Covers  *PublicWorkCoverSlots `json:"covers,omitempty"`
	Refs    []PublicCatalogRef    `json:"refs,omitempty"`
	Tags    []PublicTag           `json:"tags,omitempty" doc:"include=tags only: same shape and election as the work-detail tags block, cut at the default spoiler ceiling"`
	Credits []PublicCreditGroup   `json:"credits,omitempty" doc:"include=credits only: same groups, ordering and suppression as the work-detail credits block"`
}

type PublicCoverSlot struct {
	URL       string `json:"url"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Thumbhash string `json:"thumbhash,omitempty"`
	Sexual    int16  `json:"sexual"`
	Violence  int16  `json:"violence"`
	Source    string `json:"source"`
	Origin    string `json:"origin" enum:"cover,screenshot,censored" doc:"Which pool the image was elected from. screenshot only ever appears on banner, as the fallback for a work with no landscape cover. censored is a first-party blurred stand-in derived from explicit official art — served when a work has no safe cover art (and, without the R18 opt-in, nothing else could fill the slot)"`
}

type PublicWorkCoverSlots struct {
	Portrait *PublicCoverSlot `json:"portrait"`
	Banner   *PublicCoverSlot `json:"banner"`
}

type PublicWorksListData struct {
	Items      []PublicWorkListItem `json:"items"`
	NextCursor *string              `json:"next_cursor"`
	Total      int64                `json:"-"`
}

type PublicChangeItem struct {
	EntityType string `json:"entity_type"`
	ID         int64  `json:"id"`
	Updated    string `json:"updated"`
	Gone       bool   `json:"gone,omitempty"`
}

type PublicChangesData struct {
	Items      []PublicChangeItem `json:"items"`
	NextCursor string             `json:"next_cursor"`
}

type PublicTagDetail struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Tier       string            `json:"tier" doc:"core|longtail|hidden"`
	Kind       string            `json:"kind" doc:"content|meta"`
	WorkCount  int               `json:"work_count"`
	Sexual     bool              `json:"sexual"`
	Intros     []PublicIntro     `json:"intros" doc:"tag descriptions per language. machine is ALWAYS false on this face — catalog_tag_intro carries no provenance column, so the flag records \"unknown\", not \"human-written\"; do not read an absent machine here as a quality signal"`
	Works      []PublicWorkBrief `json:"works,omitempty"`
	NextOffset *int              `json:"next_offset,omitempty"`
}

type PublicLabelListItem struct {
	ID           int64                          `json:"id"`
	DisplayName  string                         `json:"display_name"`
	Latin        string                         `json:"latin,omitempty"`
	Lang         string                         `json:"lang,omitempty" doc:"BCP-47 language of display_name; empty when unrecorded"`
	Localized    map[string]PublicLocalizedName `json:"localized" doc:"preferred name per locale, keyed by canonically-cased BCP-47 tag; {} when none — render localized[yourLocale] ?? display_name"`
	Aliases      []PublicAlias                  `json:"aliases" doc:"alternate spellings of this label, same election and shape as the label detail face; deduplicated by value+lang, display_name excluded, [] when it has none"`
	Kind         string                         `json:"kind" doc:"game_brand|bunko|publisher|anime_studio|doujin_circle|group|other"`
	WorkCount    int                            `json:"work_count"`
	LogoHash     string                         `json:"logo_hash" doc:"brand logo content hash in the image service; \"\" = this label has no logo"`
	LogoMeta     *PublicImageMeta               `json:"logo_meta,omitempty" doc:"dimensions, thumbhash and grading of logo_hash; absent when there is no logo or the image service did not answer"`
	HasRelations bool                           `json:"has_relations" doc:"true = this label has corporate-family edges; follow labels/{id}/relation-graph. False rows need no such call"`
}

type PublicLabelsListData struct {
	Items      []PublicLabelListItem `json:"items"`
	NextCursor *string               `json:"next_cursor"`
	Total      int64                 `json:"total"`
}

type PublicTagListItem struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Tier      string `json:"tier" doc:"core|longtail|hidden"`
	Kind      string `json:"kind" doc:"content|meta"`
	WorkCount int    `json:"work_count"`
	Sexual    bool   `json:"sexual"`
}

type PublicTagsListData struct {
	Items      []PublicTagListItem `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	Total      int64               `json:"total"`
}

type PublicEngineListItem struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	WorkCount   int      `json:"work_count"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
}

type PublicEnginesListData struct {
	Items      []PublicEngineListItem `json:"items"`
	NextCursor *string                `json:"next_cursor"`
	Total      int64                  `json:"total"`
}

type PublicRoleListItem struct {
	ID         int64  `json:"id"`
	Key        string `json:"key"`
	Category   string `json:"category"`
	NameCN     string `json:"name_cn"`
	NameJA     string `json:"name_ja"`
	NameEN     string `json:"name_en"`
	Deprecated bool   `json:"deprecated"`
}

type PublicRolesListData struct {
	Items      []PublicRoleListItem `json:"items"`
	NextCursor *string              `json:"next_cursor"`
	Total      int64                `json:"total"`
}

type PublicCalendarData struct {
	Month      string               `json:"month,omitempty" doc:"YYYY-MM — the month bucket only"`
	Year       string               `json:"year,omitempty" doc:"YYYY — the pending bucket only"`
	Count      int64                `json:"count"`
	Items      []PublicWorkListItem `json:"items"`
	NextCursor *string              `json:"next_cursor"`
	Meta       PublicCalendarMeta   `json:"meta"`
}

type PublicCalendarMeta struct {
	Today    string `json:"today"`
	MinMonth string `json:"min_month,omitempty"`
	MaxMonth string `json:"max_month,omitempty"`
	HasPrev  *bool  `json:"has_prev,omitempty"`
	HasNext  *bool  `json:"has_next,omitempty"`
}

type PublicEngine struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	WorkCount   int                `json:"work_count"`
	Description string             `json:"description"`
	Aliases     []string           `json:"aliases"`
	Refs        []PublicCatalogRef `json:"refs"`
}

type PublicWorksSearchData struct {
	Total  int64                       `json:"total"`
	Page   int                         `json:"page"`
	Limit  int                         `json:"limit"`
	Items  []PublicWorkListItem        `json:"items"`
	Facets map[string]map[string]int64 `json:"facets,omitempty"`
}

type PublicReleaseFeedItem struct {
	ID        int64              `json:"id"`
	Kind      string             `json:"kind" doc:"default|digital|physical|trial|patch"`
	Date      *string            `json:"date"`
	Title     string             `json:"title,omitempty"`
	Lang      string             `json:"lang,omitempty"`
	Platform  string             `json:"platform,omitempty"`
	Platforms []string           `json:"platforms,omitempty"`
	Refs      []PublicCatalogRef `json:"refs"`
	Labels    []PublicWorkLabel  `json:"labels"`
	IsFirst   bool               `json:"is_first"`
	Work      PublicWorkListItem `json:"work"`
}

type PublicReleaseFeedData struct {
	Count      int64                   `json:"count"`
	Items      []PublicReleaseFeedItem `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}
