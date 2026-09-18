package entityintromt

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingTranslator struct {
	calls int
}

func (c *countingTranslator) Translate(ctx context.Context, text string, src SourceLang, gloss Glossary) (string, string, error) {
	c.calls++
	return MockTranslator{Model: "test"}.Translate(ctx, text, src, gloss)
}

func charMachines(t *testing.T, charID int64) []machineRow {
	t.Helper()
	var rows []machineRow
	require.NoError(t, testDB.Raw(`
		SELECT intro, source_id, src_hash, mt_model, updated_at
		FROM catalog_character_intro
		WHERE character_id = ? AND lang = 'zh-Hans' AND provenance = 1
		ORDER BY source_id`, charID).Scan(&rows).Error)
	return rows
}

func TestStrayEntityRowIsPrunedButTheDerivedRowStays(t *testing.T) {
	clean(t)
	vndb, bangumi := srcIDs(t)
	lo, hi := min(vndb, bangumi), max(vndb, bangumi)
	ctx := context.Background()

	id := mkCharacter(t, "stray-char")
	const srcText = "ヒロインの一人。明るく面倒見がよい。"
	mkCharIntro(t, id, "ja", srcText, hi)

	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "过期的机翻", SourceID: lo,
		Provenance: model.IntroProvenanceMachine, SrcHash: "stray-hash", MTModel: "old-mt",
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hant", Intro: "繁體機譯", SourceID: lo,
		Provenance: model.IntroProvenanceMachine, SrcHash: "hant-hash", MTModel: "old-mt",
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "从作品简介抽出的中文", SourceID: model.SourceDerived,
		Provenance: model.IntroProvenanceMachine, SrcHash: "derived-hash", MTModel: "extract-char-intros",
	}).Error)

	var derivedBefore machineRow
	require.NoError(t, testDB.Raw(`
		SELECT intro, source_id, src_hash, mt_model, updated_at
		FROM catalog_character_intro
		WHERE character_id = ? AND source_id = ? AND lang = 'zh-Hans' AND provenance = 1`,
		id, model.SourceDerived).Scan(&derivedBefore).Error)

	tr := &countingTranslator{}
	opts := Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter, EntityIDs: []int64{id}}
	st, err := Run(ctx, tr, opts)
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.Equal(t, 1, tr.calls)
	assert.Equal(t, 1, st[0].Inserted)
	assert.Equal(t, 1, st[0].Pruned)

	rows := charMachines(t, id)
	bySrc := map[int16]machineRow{}
	for _, row := range rows {
		bySrc[row.SourceID] = row
	}
	_, hasStray := bySrc[lo]
	assert.False(t, hasStray, "the stray must go")
	require.Contains(t, bySrc, hi, "one row at the chosen source")
	require.Contains(t, bySrc, model.SourceDerived)
	assert.Equal(t, derivedBefore.Intro, bySrc[model.SourceDerived].Intro)
	assert.Equal(t, derivedBefore.SrcHash, bySrc[model.SourceDerived].SrcHash)
	assert.Equal(t, derivedBefore.MTModel, bySrc[model.SourceDerived].MTModel)
	assert.True(t, bySrc[model.SourceDerived].UpdatedAt.Equal(derivedBefore.UpdatedAt),
		"the derived row is unchanged")
	assert.Len(t, rows, 2)
	var hant int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_character_intro
		WHERE character_id = ? AND lang = 'zh-Hant' AND source_id = ?`, id, lo).Scan(&hant).Error)
	assert.EqualValues(t, 1, hant, "the zh-Hant machine row at the stray's source survives")

	st, err = Run(ctx, tr, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls, "second run must not translate again")
	assert.Equal(t, 1, st[0].SkipUnchanged)
	assert.Zero(t, st[0].WouldRetranslate)
	assert.Zero(t, st[0].WouldPrune)
}

func TestAShortSourceNeverPrunes(t *testing.T) {
	clean(t)
	vndb, bangumi := srcIDs(t)
	lo, hi := min(vndb, bangumi), max(vndb, bangumi)
	ctx := context.Background()

	id := mkCharacter(t, "short-src")
	mkCharIntro(t, id, "ja", "沙耶。", hi)
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "过期的机翻，这是目前仅有的中文", SourceID: lo,
		Provenance: model.IntroProvenanceMachine, SrcHash: "stray-hash", MTModel: "old-mt",
	}).Error)

	tr := &countingTranslator{}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter, EntityIDs: []int64{id}})
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.Zero(t, tr.calls)
	assert.Equal(t, 1, st[0].SkipShortSource)
	assert.Zero(t, st[0].WouldPrune)
	assert.Zero(t, st[0].Pruned)

	rows := charMachines(t, id)
	require.Len(t, rows, 1)
	assert.Equal(t, lo, rows[0].SourceID, "the stray is still there")
	assert.Equal(t, "过期的机翻，这是目前仅有的中文", rows[0].Intro)
}

func TestAnEntityPruneNeedsNoTranslation(t *testing.T) {
	clean(t)
	vndb, bangumi := srcIDs(t)
	lo, hi := min(vndb, bangumi), max(vndb, bangumi)
	ctx := context.Background()

	id := mkCharacter(t, "prune-only-char")
	mkCharIntro(t, id, "ja", "ヒロインの一人。明るく面倒見がよい。", hi)
	tr := &countingTranslator{}
	opts := Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter, EntityIDs: []int64{id}}
	st, err := Run(ctx, tr, opts)
	require.NoError(t, err)
	require.Equal(t, 1, st[0].Inserted)
	kept := charMachines(t, id)
	require.Len(t, kept, 1)

	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "过期的机翻", SourceID: lo,
		Provenance: model.IntroProvenanceMachine, SrcHash: "stray-hash", MTModel: "old-mt",
	}).Error)

	dry := opts
	dry.Apply = false
	st, err = Run(ctx, tr, dry)
	require.NoError(t, err)
	assert.Equal(t, 1, st[0].SkipUnchanged)
	assert.Equal(t, 1, st[0].WouldPrune)
	assert.Len(t, charMachines(t, id), 2, "dry run deletes nothing")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_character SET updated_at = now() - interval '1 second' WHERE id = ?`, id).Error)
	var before time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_character WHERE id = ?`, id).Scan(&before).Error)

	st, err = Run(ctx, tr, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls, "prune-only must not call the translator")
	assert.Equal(t, 1, st[0].Pruned)
	rows := charMachines(t, id)
	require.Len(t, rows, 1)
	assert.Equal(t, hi, rows[0].SourceID)
	assert.Equal(t, kept[0].Intro, rows[0].Intro)
	assert.Equal(t, kept[0].SrcHash, rows[0].SrcHash)

	var after time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_character WHERE id = ?`, id).Scan(&after).Error)
	assert.True(t, after.After(before), "prune-only must touch the character")
}
