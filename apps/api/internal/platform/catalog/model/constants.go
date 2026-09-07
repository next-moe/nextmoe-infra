package model

const (
	EntityTypePerson     int16 = 0
	EntityTypeCreditName int16 = 1
	EntityTypeLabel      int16 = 3
	EntityTypeCharacter  int16 = 4
	EntityTypeWork       int16 = 5
	EntityTypeRelease    int16 = 6
	EntityTypeTag        int16 = 7
	EntityTypeEngine     int16 = 8
)

const (
	GenderMale   int16 = 1
	GenderFemale int16 = 2
	GenderOther  int16 = 3
)

const (
	BloodTypeA  int16 = 1
	BloodTypeB  int16 = 2
	BloodTypeAB int16 = 3
	BloodTypeO  int16 = 4
)

const (
	CreditNameKindMain            int16 = 0
	CreditNameKindPenName         int16 = 1
	CreditNameKindDistinctPersona int16 = 2
	CreditNameKindFormerName      int16 = 3
)

const (
	LinkVisibilityPublic int16 = 0
	LinkVisibilityHidden int16 = 1
)

const (
	AliasKindTranslation     int16 = 0
	AliasKindSpellingVariant int16 = 1
	AliasKindSearchHint      int16 = 2
)

const (
	AliasProvenanceSource  int16 = 0
	AliasProvenanceMachine int16 = 1
)

// catalog_character_trait.name_zh_provenance. Curated is the ingest tool's own
// word for 0 (a hand-checked vocabulary row), not "a source published it" —
// there is no upstream Chinese trait vocabulary to be sourced from.
const (
	TraitNameZhProvenanceCurated int16 = 0
	TraitNameZhProvenanceMachine int16 = 1
)

const (
	LabelKindGameBrand    int16 = 0
	LabelKindBunko        int16 = 1
	LabelKindPublisher    int16 = 2
	LabelKindAnimeStudio  int16 = 3
	LabelKindDoujinCircle int16 = 4
	LabelKindGroup        int16 = 5
)

// LabelRelation — the corporate-structure vocabulary of a
// catalog_label_relation edge (wave 186), read as "other_label_id is <relation>
// of label_id". Constants, not a registry, for the LabelKind reason: the set is
// small and every new value ships with code.
//
// The vocabulary is four INVERSE PAIRS, and the pairing is load-bearing: the
// graph is stored mirrored, so writing a fact means writing the edge under one
// code and its mirror under the code opposite it here.
//
//	Parent      ↔ Subsidiary   the corporate ownership axis
//	Imprint     ↔ ImprintOf    a brand operating under another label
//	Spawned     ↔ Origin       a spin-off and the label it came out of
//	SucceededBy ↔ Formerly     the succession axis (renamed / reorganized)
//
// 0 is deliberately absent: an unset relation is not a fact, and leaving the
// zero value unassigned makes a forgotten column write fail a CHECK-free table
// loudly at read time instead of silently rendering "parent".
const (
	LabelRelationParent      int16 = 1
	LabelRelationSubsidiary  int16 = 2
	LabelRelationImprint     int16 = 3
	LabelRelationImprintOf   int16 = 4
	LabelRelationSpawned     int16 = 5
	LabelRelationOrigin      int16 = 6
	LabelRelationSucceededBy int16 = 7
	LabelRelationFormerly    int16 = 8
)

var LabelRelationKey = map[int16]string{
	LabelRelationParent:      "parent",
	LabelRelationSubsidiary:  "subsidiary",
	LabelRelationImprint:     "imprint",
	LabelRelationImprintOf:   "imprint_of",
	LabelRelationSpawned:     "spawned",
	LabelRelationOrigin:      "origin",
	LabelRelationSucceededBy: "succeeded_by",
	LabelRelationFormerly:    "formerly",
}

const (
	WorkLabelKindCircle    int16 = 0
	WorkLabelKindPublisher int16 = 1
	WorkLabelKindDeveloper int16 = 2
	WorkLabelKindBrand     int16 = 3
)

const (
	WorkCharacterKindUnknown   int16 = 0
	WorkCharacterKindMain      int16 = 1
	WorkCharacterKindSecondary int16 = 2
	WorkCharacterKindAppears   int16 = 3
)

const (
	SeriesMemberKindUnknown    int16 = 0
	SeriesMemberKindMain       int16 = 1
	SeriesMemberKindFandisc    int16 = 2
	SeriesMemberKindSideStory  int16 = 3
	SeriesMemberKindCollection int16 = 4
)

const (
	RevisionActionCreated      int16 = 0
	RevisionActionUpdated      int16 = 1
	RevisionActionMergedSource int16 = 2
	RevisionActionMergedTarget int16 = 3
	RevisionActionSplit        int16 = 4
	RevisionActionImported     int16 = 5
	RevisionActionRedirect     int16 = 6
	RevisionActionReverted     int16 = 7
	RevisionActionDeleted      int16 = 8
)

const (
	ContentRatingAllAges   int16 = 0
	ContentRatingSensitive int16 = 1
	ContentRatingR18       int16 = 2
)

// Per-image sexual levels (covers/screenshots): 0 safe, 1 suggestive
// (underwear/swimsuit), 2 explicit. The public read path and the repincovers
// pin job must draw the display-safe line at the same level: the read path
// used to hide sexual >= 1 while the pin job pinned sexual = 1 covers, so
// display-sfw works whose covers were all suggestive rendered no cover for
// anyone — 101 of the top 400 works by popularity listed with cover: null.
const (
	SexualSafe       int16 = 0
	SexualSuggestive int16 = 1
	SexualExplicit   int16 = 2
)

const (
	WorkStatusLive       int16 = 0
	WorkStatusStub       int16 = 1
	WorkStatusMerged     int16 = 2
	WorkStatusQuarantine int16 = 3
)

const (
	IntroProvenanceSource  int16 = 0
	IntroProvenanceMachine int16 = 1
)

const (
	ClaimStateLive     int16 = 0
	ClaimStateDraft    int16 = 1
	ClaimStateHidden   int16 = 2
	ClaimStatePending  int16 = 3
	ClaimStateDeclined int16 = 4
)

const (
	ClaimStateKeyNone     = "none"
	ClaimStateKeyLive     = "live"
	ClaimStateKeyDraft    = "draft"
	ClaimStateKeyPending  = "pending"
	ClaimStateKeyDeclined = "declined"
	ClaimStateKeyHidden   = "hidden"
)

func ClaimStateKey(site *string, productWorkID *int64, claimState *int16) string {
	if site == nil || *site == "" || productWorkID == nil {
		return ClaimStateKeyNone
	}
	if claimState == nil {
		return ClaimStateKeyLive
	}
	switch *claimState {
	case ClaimStateLive:
		return ClaimStateKeyLive
	case ClaimStateDraft:
		return ClaimStateKeyDraft
	case ClaimStatePending:
		return ClaimStateKeyPending
	case ClaimStateDeclined:
		return ClaimStateKeyDeclined
	default:
		return ClaimStateKeyHidden
	}
}

const (
	DisplayLimitKeySFW  = "sfw"
	DisplayLimitKeyNSFW = "nsfw"
)

const WikiContentLimitNSFW = "nsfw"

func DisplayLimitKey(site *string, productWorkID *int64, displayNSFW bool, contentRating int16) string {
	if site == nil || *site == "" || productWorkID == nil {
		if contentRating == ContentRatingR18 {
			return DisplayLimitKeyNSFW
		}
		return DisplayLimitKeySFW
	}
	if displayNSFW {
		return DisplayLimitKeyNSFW
	}
	return DisplayLimitKeySFW
}

const (
	WorkTitleKindOfficial     int16 = 0
	WorkTitleKindAlias        int16 = 1
	WorkTitleKindAbbreviation int16 = 2
	WorkTitleKindSearchHint   int16 = 3
)

const (
	WorkTitleProvenanceSource  int16 = 0
	WorkTitleProvenanceMachine int16 = 1
)

const (
	ReleaseKindDefault  int16 = 0
	ReleaseKindDigital  int16 = 1
	ReleaseKindPhysical int16 = 2
	ReleaseKindTrial    int16 = 3
	ReleaseKindPatch    int16 = 4
)

const (
	LinkKindExact    int16 = 0
	LinkKindProbable int16 = 1
	LinkKindRelated  int16 = 2
)

const (
	PopularityMetricDownloads int16 = 0
	PopularityMetricWishlist  int16 = 1
	PopularityMetricReviews   int16 = 2

	PopularityMetricBgmWish    int16 = 10
	PopularityMetricBgmCollect int16 = 11
	PopularityMetricBgmDoing   int16 = 12
	PopularityMetricBgmOnHold  int16 = 13
	PopularityMetricBgmDropped int16 = 14

	PopularityMetricFavorites int16 = 20
)

const (
	CandidateReasonSharedExternalID int16 = 0
	CandidateReasonNameNormEqual    int16 = 1
	CandidateReasonNameFuzzy        int16 = 2
	CandidateReasonImporterSuggest  int16 = 3
	CandidateReasonLLMSuggest       int16 = 4
	CandidateReasonAliasDeclared    int16 = 5
)

const (
	CandidateStatusPending     int16 = 0
	CandidateStatusAccepted    int16 = 1
	CandidateStatusRejected    int16 = 2
	CandidateStatusDeferred    int16 = 3
	CandidateStatusNeedsManual int16 = 4
)

const (
	ProposalStatusOpen      int16 = 0
	ProposalStatusApproved  int16 = 1
	ProposalStatusExecuted  int16 = 2
	ProposalStatusRejected  int16 = 3
	ProposalStatusWithdrawn int16 = 4
)
