package service

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func characterEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := provEngine(t)
	require.NoError(t, editspec.RegisterCharacter(e.Registry(), testDB))
	return e
}

func suppressCharacterAlias(t *testing.T, characterID int64, kind int16, lang, name string) {
	t.Helper()
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType:  editspec.TypeCharacter,
		EntityID:    characterID,
		FieldKey:    editspec.FieldCharacterAliases,
		IdentityKey: editspec.CharacterAliasIdentity(kind, lang, name),
	}).Error)
}

func TestReadFacesExcludeSuppressedCharacterAliases(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	public := NewPublicService(testDB, read, testResolve, "")

	ch := createCharacter(t, "主人公")
	good := createCharacterAlias(t, ch.ID, "しゅじんこう", "ja")
	bad := createCharacterAlias(t, ch.ID, "誤った別名", "ja")
	_ = good

	before, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, before.Aliases, 2)
	pubBefore, ok, err := public.Character(ctx, ch.ID, false, true, model.SpoilerSevere, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, pubBefore.Aliases, 2)

	suppressCharacterAlias(t, ch.ID, bad.Kind, bad.Lang, bad.Name)

	after, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, after.Aliases, 1)
	assert.Equal(t, "しゅじんこう", after.Aliases[0].Name)

	pubAfter, ok, err := public.Character(ctx, ch.ID, false, true, model.SpoilerSevere, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, pubAfter.Aliases, 1)
	assert.Equal(t, "しゅじんこう", pubAfter.Aliases[0].Value)

	// The suppressed row is still in the table: the roster importer owns it and
	// the exclusion is a read-path rule, not a delete.
	var live int64
	require.NoError(t, testDB.Model(&model.CatalogCharacterAlias{}).
		Where("character_id = ?", ch.ID).Count(&live).Error)
	assert.EqualValues(t, 2, live)
}

func TestMergeCarriesCharacterAliasSuppressions(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := createCharacter(t, "同じキャラ")
	source := createCharacter(t, "同じ キャラ")
	createCharacterAlias(t, target.ID, "生き残る別名", "ja")
	srcOnly := createCharacterAlias(t, source.ID, "誤別名", "ja")
	srcShared := createCharacterAlias(t, source.ID, "共通別名", "ja")
	dstShared := createCharacterAlias(t, target.ID, "共通別名", "zh-Hans")

	suppressCharacterAlias(t, source.ID, srcOnly.Kind, srcOnly.Lang, srcOnly.Name)
	suppressCharacterAlias(t, source.ID, srcShared.Kind, srcShared.Lang, srcShared.Name)
	// The same identity key already sits on the target: the rehang must fold it,
	// not duplicate it into a unique-constraint violation.
	suppressCharacterAlias(t, target.ID, srcShared.Kind, srcShared.Lang, srcShared.Name)
	_ = dstShared

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeCharacter, source.ID, target.ID, 7, "suppression rehang")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	keys, err := editing.LoadSuppressedKeys(ctx, testDB, editspec.TypeCharacter, target.ID, editspec.FieldCharacterAliases)
	require.NoError(t, err)
	assert.Len(t, keys, 2, "both suppressions land on the survivor, deduplicated")

	left, err := editing.LoadSuppressedKeys(ctx, testDB, editspec.TypeCharacter, source.ID, editspec.FieldCharacterAliases)
	require.NoError(t, err)
	assert.Empty(t, left, "nothing may stay stranded on the merged-away character")

	read := NewReadService(testDB)
	detail, err := read.CharacterByID(ctx, target.ID, model.SpoilerSevere)
	require.NoError(t, err)
	names := make([]string, 0, len(detail.Aliases))
	for _, a := range detail.Aliases {
		names = append(names, a.Name)
	}
	assert.Equal(t, []string{"生き残る別名", "共通別名"}, names,
		"the ja 共通別名 rehung from the source stays suppressed; the zh-Hans one is a different key")
}

func TestMergeKeepsEngineEditedCharacterName(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := characterEngine(t)

	target := createCharacter(t, "上流の暫定名")
	source := createCharacter(t, "上流の別名")
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_character SET field_provenance =
		 '{"display_name":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		source.ID).Error)

	humanEdit(t, e, editspec.TypeCharacter, target.ID,
		map[string]any{editspec.FieldCharacterDisplayName: "人が決めた正式名"})

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeCharacter, source.ID, target.ID, 7, "dup")
	require.NoError(t, err)
	resolveToSource(t, p.ID, "display_name")
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogCharacter
	require.NoError(t, testDB.First(&merged, target.ID).Error)
	assert.Equal(t, "人が決めた正式名", merged.DisplayName,
		"an engine edit must survive a merge that resolves the field to the source")
	assert.Equal(t, provenance.SourceCurated, firstProvSource(merged.FieldProvenance, "display_name"))
}

// The curated lane sorts AFTER every importer id (12 > 2/3/4/5), so before this
// ordering term a hand-written intro was folded away by the one-row-per-lang
// rule: saved, readable in the editor, and absent from every face.
func TestHumanIntroWinsTheLangFold(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	public := NewPublicService(testDB, read, testResolve, "")

	const bangumiSourceID, curatedSourceID = 3, 12

	w := createWork(t, "作品")
	require.NoError(t, testDB.Create(&[]model.CatalogWorkIntro{
		{WorkID: w.ID, Lang: "ja", Intro: "上流の紹介", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceSource},
		{WorkID: w.ID, Lang: "ja", Intro: "人手の紹介", SourceID: curatedSourceID, Provenance: model.IntroProvenanceSource},
	}).Error)

	intros := map[int64]introFaces{}
	require.NoError(t, read.nativeWorkIntros(ctx, []int64{w.ID}, intros))
	require.Len(t, intros[w.ID].all, 1)
	assert.Equal(t, "人手の紹介", intros[w.ID].all[0].Intro)

	ch := createCharacter(t, "キャラ")
	require.NoError(t, testDB.Create(&[]model.CatalogCharacterIntro{
		{CharacterID: ch.ID, Lang: "ja", Intro: "上流のキャラ紹介", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceSource},
		{CharacterID: ch.ID, Lang: "ja", Intro: "人手のキャラ紹介", SourceID: curatedSourceID, Provenance: model.IntroProvenanceSource},
	}).Error)

	detail, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, detail.Intros, 1)
	assert.Equal(t, "人手のキャラ紹介", detail.Intros[0].Intro)

	pub, ok, err := public.Character(ctx, ch.ID, false, true, model.SpoilerSevere, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, pub.Intros, 1)
	assert.Equal(t, "人手のキャラ紹介", pub.Intros[0].Intro)
	assert.Equal(t, "curated", pub.Intros[0].Source)
}

// The other half of the same ordering. The human lane wins its own provenance
// tier and nothing more: a machine translation that happens to sit in the
// curated lane (entity-intro-mt writes prov=1 under the ja row's source_id) must
// still lose to an upstream ORIGINAL, or the fix to the fold would have inverted
// the source-outranks-machine axis on its way past.
func TestCuratedMachineTranslationDoesNotBeatAnUpstreamOriginal(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	public := NewPublicService(testDB, read, testResolve, "")

	const bangumiSourceID, curatedSourceID = 3, 12

	w := createWork(t, "作品")
	require.NoError(t, testDB.Create(&[]model.CatalogWorkIntro{
		{WorkID: w.ID, Lang: "zh-Hans", Intro: "上流の原文", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceSource},
		{WorkID: w.ID, Lang: "zh-Hans", Intro: "curated 車道の機械翻訳", SourceID: curatedSourceID, Provenance: model.IntroProvenanceMachine},
	}).Error)

	intros := map[int64]introFaces{}
	require.NoError(t, read.nativeWorkIntros(ctx, []int64{w.ID}, intros))
	require.Len(t, intros[w.ID].all, 1)
	assert.Equal(t, "上流の原文", intros[w.ID].all[0].Intro)
	assert.False(t, intros[w.ID].all[0].Machine)

	ch := createCharacter(t, "キャラ")
	require.NoError(t, testDB.Create(&[]model.CatalogCharacterIntro{
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "上流の原文", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceSource},
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "curated 車道の機械翻訳", SourceID: curatedSourceID, Provenance: model.IntroProvenanceMachine},
	}).Error)

	detail, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, detail.Intros, 1)
	assert.Equal(t, "上流の原文", detail.Intros[0].Intro)

	pub, ok, err := public.Character(ctx, ch.ID, false, true, model.SpoilerSevere, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, pub.Intros, 1)
	assert.Equal(t, "上流の原文", pub.Intros[0].Intro)
	assert.Equal(t, "bangumi", pub.Intros[0].Source)
}

// The machine tier is not the human lane's to decide. This shipped-by-a-hair
// case is two translations of the same language: preferring ours moved 5,806
// production works onto a machine translation of our `en` row while their ja
// face kept rendering bangumi's Japanese original — two languages, two different
// source materials. The dev snapshot shows 4 such works, so only production
// could tell.
func TestCuratedMachineTranslationDoesNotBeatAnUpstreamMachineTranslation(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	public := NewPublicService(testDB, read, testResolve, "")

	const bangumiSourceID, curatedSourceID = 3, 12

	w := createWork(t, "作品")
	require.NoError(t, testDB.Create(&[]model.CatalogWorkIntro{
		{WorkID: w.ID, Lang: "zh-Hans", Intro: "上流の機械翻訳", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceMachine},
		{WorkID: w.ID, Lang: "zh-Hans", Intro: "curated 車道の機械翻訳", SourceID: curatedSourceID, Provenance: model.IntroProvenanceMachine},
	}).Error)

	intros := map[int64]introFaces{}
	require.NoError(t, read.nativeWorkIntros(ctx, []int64{w.ID}, intros))
	require.Len(t, intros[w.ID].all, 1)
	assert.Equal(t, "上流の機械翻訳", intros[w.ID].all[0].Intro)
	assert.EqualValues(t, bangumiSourceID, intros[w.ID].all[0].SourceID)

	ch := createCharacter(t, "キャラ")
	require.NoError(t, testDB.Create(&[]model.CatalogCharacterIntro{
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "上流の機械翻訳", SourceID: bangumiSourceID, Provenance: model.IntroProvenanceMachine},
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "curated 車道の機械翻訳", SourceID: curatedSourceID, Provenance: model.IntroProvenanceMachine},
	}).Error)

	detail, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, detail.Intros, 1)
	assert.Equal(t, "上流の機械翻訳", detail.Intros[0].Intro)

	pub, ok, err := public.Character(ctx, ch.ID, false, true, model.SpoilerSevere, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, pub.Intros, 1)
	assert.Equal(t, "bangumi", pub.Intros[0].Source)
}

// With the human lane gated to provenance=0 the machine tier is ranked by the
// wave-178 rule alone: a derived extraction (source 18) outranks a translation,
// including one in the curated lane.
func TestDerivedStillWinsTheMachineTierOverTheCuratedLane(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)

	const curatedSourceID = 12

	ch := createCharacter(t, "キャラ")
	require.NoError(t, testDB.Create(&[]model.CatalogCharacterIntro{
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "curated 車道の機械翻訳", SourceID: curatedSourceID, Provenance: model.IntroProvenanceMachine},
		{CharacterID: ch.ID, Lang: "zh-Hans", Intro: "derived 抽出", SourceID: sourceDerived, Provenance: model.IntroProvenanceMachine},
	}).Error)

	detail, err := read.CharacterByID(ctx, ch.ID, model.SpoilerSevere)
	require.NoError(t, err)
	require.Len(t, detail.Intros, 1)
	assert.Equal(t, "derived 抽出", detail.Intros[0].Intro)
}
