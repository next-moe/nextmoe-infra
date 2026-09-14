package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstantGroupsHaveUniqueValues(t *testing.T) {
	groups := map[string][]int16{
		"entity_type": {
			EntityTypePerson, EntityTypeCreditName,
			EntityTypeLabel, EntityTypeCharacter, EntityTypeWork, EntityTypeRelease,
			EntityTypeTag, EntityTypeEngine,
		},
		"gender": {GenderMale, GenderFemale, GenderOther},
		"credit_name_kind": {
			CreditNameKindMain, CreditNameKindPenName,
			CreditNameKindDistinctPersona, CreditNameKindFormerName,
		},
		"link_visibility": {LinkVisibilityPublic, LinkVisibilityHidden},
		"alias_kind":      {AliasKindTranslation, AliasKindSpellingVariant, AliasKindSearchHint},
		"label_kind": {
			LabelKindGameBrand, LabelKindBunko, LabelKindPublisher,
			LabelKindAnimeStudio, LabelKindDoujinCircle, LabelKindGroup,
		},
		"revision_action": {
			RevisionActionCreated, RevisionActionUpdated, RevisionActionMergedSource,
			RevisionActionMergedTarget, RevisionActionSplit, RevisionActionImported,
			RevisionActionRedirect, RevisionActionReverted,
		},
		"relation_domain": {RelationDomainWork, RelationDomainEntity},
		"content_rating":  {ContentRatingAllAges, ContentRatingSensitive, ContentRatingR18},
		"work_status":     {WorkStatusLive, WorkStatusStub, WorkStatusMerged, WorkStatusQuarantine},
		"work_title_kind": {
			WorkTitleKindOfficial, WorkTitleKindAlias,
			WorkTitleKindAbbreviation, WorkTitleKindSearchHint,
		},
		"release_kind": {
			ReleaseKindDefault, ReleaseKindDigital, ReleaseKindPhysical,
			ReleaseKindTrial, ReleaseKindPatch,
		},
		"link_kind": {LinkKindExact, LinkKindProbable, LinkKindRelated},
		"candidate_reason": {
			CandidateReasonSharedExternalID, CandidateReasonNameNormEqual,
			CandidateReasonNameFuzzy, CandidateReasonImporterSuggest, CandidateReasonLLMSuggest,
		},
		"candidate_status": {
			CandidateStatusPending, CandidateStatusAccepted,
			CandidateStatusRejected, CandidateStatusDeferred, CandidateStatusNeedsManual,
		},
		"proposal_status": {
			ProposalStatusOpen, ProposalStatusApproved, ProposalStatusExecuted,
			ProposalStatusRejected, ProposalStatusWithdrawn,
		},
		"spoiler": {SpoilerNone, SpoilerMild, SpoilerSevere},
		"work_character_kind": {
			WorkCharacterKindUnknown, WorkCharacterKindMain,
			WorkCharacterKindSecondary, WorkCharacterKindAppears,
		},
		"label_relation": {
			LabelRelationParent, LabelRelationSubsidiary,
			LabelRelationImprint, LabelRelationImprintOf,
			LabelRelationSpawned, LabelRelationOrigin,
			LabelRelationSucceededBy, LabelRelationFormerly,
		},
	}
	for name, values := range groups {
		seen := make(map[int16]bool, len(values))
		for _, v := range values {
			assert.False(t, seen[v], "%s: duplicate value %d", name, v)
			seen[v] = true
		}
	}
}

func TestDisplayLimitKeyIsATwoValuePartition(t *testing.T) {
	wiki, empty := "galgame_wiki", ""
	pwid := int64(42)

	for _, tc := range []struct {
		name string
		in   WorkShelf
		want string
	}{
		{"bodyless all_ages", WorkShelf{ContentRating: ContentRatingAllAges}, DisplayLimitKeySFW},
		{"bodyless sensitive", WorkShelf{ContentRating: ContentRatingSensitive}, DisplayLimitKeySFW},
		{"bodyless r18", WorkShelf{ContentRating: ContentRatingR18}, DisplayLimitKeyNSFW},
		{"empty site is bodyless", WorkShelf{Site: &empty, ProductWorkID: &pwid, DisplayNSFW: true,
			ContentRating: ContentRatingR18}, DisplayLimitKeyNSFW},
		{"site without a product work id is bodyless", WorkShelf{Site: &wiki, DisplayNSFW: true,
			ContentRating: ContentRatingAllAges}, DisplayLimitKeySFW},
		{"claimed, editorially sfw, game r18", WorkShelf{Site: &wiki, ProductWorkID: &pwid,
			ContentRating: ContentRatingR18}, DisplayLimitKeySFW},
		{"claimed, editorially nsfw, game all_ages", WorkShelf{Site: &wiki, ProductWorkID: &pwid,
			DisplayNSFW: true, ContentRating: ContentRatingAllAges}, DisplayLimitKeyNSFW},
		{"claimed, editorially nsfw, game r18", WorkShelf{Site: &wiki, ProductWorkID: &pwid,
			DisplayNSFW: true, ContentRating: ContentRatingR18}, DisplayLimitKeyNSFW},
		{"claimed, nothing declared", WorkShelf{Site: &wiki, ProductWorkID: &pwid,
			ContentRating: ContentRatingR18}, DisplayLimitKeySFW},
		{"claimed, editorially sfw, but every cover is explicit", WorkShelf{Site: &wiki, ProductWorkID: &pwid,
			ContentRating: ContentRatingR18, CoverArtAllExplicit: true}, DisplayLimitKeyNSFW},
		{"bodyless all_ages whose only cover art is explicit", WorkShelf{ContentRating: ContentRatingAllAges,
			CoverArtAllExplicit: true}, DisplayLimitKeyNSFW},
	} {
		got := DisplayLimitKey(tc.in)
		assert.Equal(t, tc.want, got, "%s", tc.name)
		assert.Contains(t, []string{DisplayLimitKeySFW, DisplayLimitKeyNSFW}, got,
			"%s: the vocabulary is exactly two values", tc.name)
	}

	assert.NotEqual(t,
		DisplayLimitKey(WorkShelf{Site: &wiki, ProductWorkID: &pwid, ContentRating: ContentRatingR18}),
		DisplayLimitKey(WorkShelf{ContentRating: ContentRatingR18}),
		"an r18 game reads sfw when claimed with safe material, nsfw when bodyless")
}

// TestDisplayLimitKeyGrewWithoutMovingAnything pins the 2026-09 change as
// additive over the whole input space: with no cover art graded explicit,
// every combination answers exactly what it answered before. The old rule is
// spelled out here instead of called, because a regression pin that calls the
// code it pins cannot fail.
func TestDisplayLimitKeyGrewWithoutMovingAnything(t *testing.T) {
	wiki, empty := "galgame_wiki", ""
	pwid := int64(42)
	before := func(site *string, productWorkID *int64, displayNSFW bool, contentRating int16) string {
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

	sites := []*string{nil, &empty, &wiki}
	pwids := []*int64{nil, &pwid}
	ratings := []int16{ContentRatingAllAges, ContentRatingSensitive, ContentRatingR18}
	cases := 0
	for _, site := range sites {
		for _, pw := range pwids {
			for _, rating := range ratings {
				for _, display := range []bool{false, true} {
					in := WorkShelf{Site: site, ProductWorkID: pw, DisplayNSFW: display, ContentRating: rating}
					assert.Equal(t, before(site, pw, display, rating), DisplayLimitKey(in),
						"site=%v pwid=%v display=%v rating=%d moved without cover art doing so", site, pw, display, rating)
					in.CoverArtAllExplicit = true
					assert.Equal(t, DisplayLimitKeyNSFW, DisplayLimitKey(in),
						"a work owning only explicit cover art can never be on the sfw shelf")
					cases++
				}
			}
		}
	}
	assert.Equal(t, len(sites)*len(pwids)*len(ratings)*2, cases)
}

func TestLabelRelationVocabularyPairsAndRenders(t *testing.T) {
	inverse := map[int16]int16{
		LabelRelationParent:      LabelRelationSubsidiary,
		LabelRelationImprint:     LabelRelationImprintOf,
		LabelRelationSpawned:     LabelRelationOrigin,
		LabelRelationSucceededBy: LabelRelationFormerly,
	}
	for a, b := range inverse {
		assert.NotEqual(t, a, b, "a relation is never its own inverse")
		assert.Contains(t, LabelRelationKey, a)
		assert.Contains(t, LabelRelationKey, b)
	}
	assert.Len(t, LabelRelationKey, 2*len(inverse))
	assert.Equal(t, "imprint_of", LabelRelationKey[LabelRelationImprintOf])
	assert.Equal(t, "succeeded_by", LabelRelationKey[LabelRelationSucceededBy])
	assert.NotContains(t, LabelRelationKey, int16(0))
}

func TestEntityTypeValuesArePinned(t *testing.T) {
	assert.Equal(t, int16(0), EntityTypePerson)
	assert.Equal(t, int16(1), EntityTypeCreditName)
	assert.Equal(t, int16(3), EntityTypeLabel)
	assert.Equal(t, int16(4), EntityTypeCharacter)
	assert.Equal(t, int16(5), EntityTypeWork)
	assert.Equal(t, int16(6), EntityTypeRelease)
	assert.Equal(t, int16(7), EntityTypeTag)
	assert.Equal(t, int16(8), EntityTypeEngine)
}

func TestWorkStatusValuesArePinned(t *testing.T) {
	assert.Equal(t, int16(0), WorkStatusLive)
	assert.Equal(t, int16(1), WorkStatusStub)
	assert.Equal(t, int16(2), WorkStatusMerged)
	assert.Equal(t, int16(3), WorkStatusQuarantine)
}
