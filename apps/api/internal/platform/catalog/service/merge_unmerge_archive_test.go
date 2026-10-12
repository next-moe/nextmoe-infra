package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mergeProposal(t *testing.T, entityType int16, src, dst int64) int64 {
	t.Helper()
	p, err := testMerge.ProposeMerge(t.Context(), entityType, src, dst, 7, "test merge")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
	return p.ID
}

func skippedBy(res UnmergeResult) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, s := range res.Archive.Skipped {
		if out[s.Table] == nil {
			out[s.Table] = map[string]int{}
		}
		out[s.Table][s.Reason]++
	}
	return out
}

func TestUnmergeRestoresWhatTheMergeArchived(t *testing.T) {
	cleanTables(t)
	l := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, loserJaTitle).ID
	s := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, survivorDisplay).ID
	seedLoser(t, l, true)
	seedSurvivor(t, s)
	pid := mergeProposal(t, model.EntityTypeWork, l, s)

	res, err := testMerge.Unmerge(t.Context(), pid, nil)
	require.NoError(t, err)
	w := res.NewID

	assert.Equal(t, map[string]int{"catalog_work_intro": 2, "catalog_work_cover": 1}, res.Archive.Restored)
	assert.Equal(t, map[string]map[string]int{
		"catalog_work_title": {skipCollides: 4},
		"catalog_work_intro": {skipSurvivorSide: 1},
	}, skippedBy(res), "the snapshot already rebuilt the titles; the survivor's own replaced machine intro is not the loser's")

	var intros []model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id = ?", w).Order("lang").Find(&intros).Error)
	require.Len(t, intros, 2)
	assert.Equal(t, "loser dlsite ja", intros[0].Intro)
	assert.Equal(t, "loser mt zh", intros[1].Intro)
	var cover model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ?", w).First(&cover).Error)
	assert.Equal(t, lShared, cover.ImageHash)
	assert.False(t, cover.MergedIn, "a restored row is the rebuilt work's own again")

	var shared int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_cover WHERE work_id = ? AND image_hash = ?`,
		s, lShared).Scan(&shared).Error)
	assert.EqualValues(t, 1, shared, "the survivor keeps its own copy")
}

func TestUnmergeOfAnAbsorbHandsTheEditionItsAnchorsBack(t *testing.T) {
	f := newAbsorbFixture(t)
	seedLoser(t, f.loser, false)
	pid := absorbNow(t, f.loser, f.survivor)

	res, err := testMerge.Unmerge(t.Context(), pid, nil)
	require.NoError(t, err)
	w := res.NewID
	assert.Equal(t, 3, res.Archive.Restored["catalog_external_ref"])
	assert.Equal(t, 4, res.Archive.Restored["catalog_work_intro"])
	assert.Equal(t, 3, res.Archive.Restored["catalog_work_cover"])

	assertLoserRefs(t, w, map[string]int16{
		"erogamescape:6000": model.LinkKindExact, "bangumi:200": model.LinkKindExact, "dlsite:RJ123": model.LinkKindExact,
	})
	var copies int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_external_ref WHERE entity_type = ? AND entity_id = ? AND matched_by = ?`,
		model.EntityTypeWork, f.survivor, absorbMatchedBy).Scan(&copies).Error)
	assert.Zero(t, copies, "the base work drops the related copies of anchors that went back")

	var reason string
	require.NoError(t, testDB.Raw(`SELECT reason FROM catalog_redirect WHERE entity_type = ? AND old_id = ?`,
		model.EntityTypeWork, f.loser).Scan(&reason).Error)
	assert.False(t, redirectIsAbsorb(reason), "late rows on the old id now follow the rebuilt work, not an absorb")
}

func TestLabelMergeKeepsTheSurvivorsNamesAndIntros(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()
	mkAlias := func(label int64, name, lang string, primary bool, prov int16) {
		t.Helper()
		require.NoError(t, testDB.Create(&model.CatalogLabelAlias{LabelID: label, Name: name, Lang: lang,
			Kind: model.AliasKindSpellingVariant, IsPrimaryForLocale: primary, Provenance: prov}).Error)
	}
	mkIntro := func(label int64, lang, text string, src, prov int16) {
		t.Helper()
		require.NoError(t, testDB.Create(&model.CatalogLabelIntro{LabelID: label, Lang: lang, Intro: text,
			SourceID: src, Provenance: prov}).Error)
	}
	loser := createLabel(t, "ゆずソフト(DLsite)", model.LabelKindGameBrand)
	survivor := createLabel(t, "ゆずソフト", model.LabelKindGameBrand)
	mkAlias(loser, "あいうえお", "ja", true, model.AliasProvenanceSource)
	mkAlias(loser, "柚子机翻", "zh-Hant", false, model.AliasProvenanceMachine)
	mkAlias(loser, "柚子社", "zh-Hans", false, model.AliasProvenanceSource)
	mkIntro(loser, "ja", "loser vndb ja", srcVNDB, model.IntroProvenanceSource)
	mkAlias(survivor, "ゆずソフト", "ja", false, model.AliasProvenanceSource)
	mkAlias(survivor, "柚子軟體", "zh-Hant", false, model.AliasProvenanceSource)
	mkIntro(survivor, "ja", "survivor dlsite ja", srcDlsite, model.IntroProvenanceSource)

	localized := func() map[string]string {
		got, err := svc.LocalizedForEntities(ctx, "label", []int64{survivor})
		require.NoError(t, err)
		return localizedStrings(got[survivor])
	}
	intros := func() []string {
		rows, err := svc.labelIntros(ctx, survivor)
		require.NoError(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, r.Lang+":"+r.Intro)
		}
		return out
	}
	beforeNames, beforeIntros := localized(), intros()

	mergeNow(t, model.EntityTypeLabel, loser, survivor)

	wantNames := beforeNames
	wantNames["zh-Hans"] = "柚子社|spelling_variant|false"
	assert.Equal(t, wantNames, localized(), "a primary ja alias of the loser does not take the survivor's ja")
	assert.Equal(t, beforeIntros, intros(), "a lower source id from the loser does not take the survivor's ja intro")

	var demoted []string
	require.NoError(t, testDB.Raw(`SELECT name FROM catalog_label_alias WHERE label_id = ? AND lang = ''`, survivor).
		Scan(&demoted).Error)
	assert.Equal(t, []string{"あいうえお"}, demoted, "the loser's ja alias stays findable without a language")
	var machine int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_label_alias WHERE name = ?`, "柚子机翻").Scan(&machine).Error)
	assert.Zero(t, machine, "a loser machine alias in a language the survivor names is dropped")
}

func TestCharacterMergeKeepsTheSurvivorsNames(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	loser, survivor := createCharacter(t, "椎名 深夏(EG)"), createCharacter(t, "椎名深夏")
	require.NoError(t, testDB.Create(&model.CatalogCharacterAlias{CharacterID: loser.ID, Name: "Shiina Minatsu",
		Lang: "en", Kind: model.AliasKindTranslation, IsPrimaryForLocale: true}).Error)
	require.NoError(t, testDB.Create(&model.CatalogCharacterAlias{CharacterID: survivor.ID, Name: "Minatsu Shiina",
		Lang: "en", Kind: model.AliasKindSpellingVariant}).Error)
	before, err := svc.LocalizedForEntities(t.Context(), "character", []int64{survivor.ID})
	require.NoError(t, err)

	mergeNow(t, model.EntityTypeCharacter, loser.ID, survivor.ID)

	after, err := svc.LocalizedForEntities(t.Context(), "character", []int64{survivor.ID})
	require.NoError(t, err)
	assert.Equal(t, localizedStrings(before[survivor.ID]), localizedStrings(after[survivor.ID]),
		"the loser's primary translation does not outrank the survivor's en name")
}
