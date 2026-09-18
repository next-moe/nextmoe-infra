package intromt

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkMachineIntro(t *testing.T, workID int64, lang, intro string, source int16, hash string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
		WorkID: workID, Lang: lang, Intro: intro, SourceID: source,
		Provenance: model.IntroProvenanceMachine, SrcHash: hash, MTModel: "old-mt",
	}).Error)
}

func machineIntros(t *testing.T, workID int64) []model.CatalogWorkIntro {
	t.Helper()
	var rows []model.CatalogWorkIntro
	require.NoError(t, testDB.Where(
		"work_id = ? AND lang = 'zh-Hans' AND provenance = 1", workID,
	).Order("source_id").Find(&rows).Error)
	return rows
}

func TestAStrayMachineRowNoLongerRetranslatesNightly(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	lo, hi := min(dlsite, bangumi), max(dlsite, bangumi)

	w := mkWork(t, medium, "stray-nightly", nil)
	mkIntro(t, w, "ja", "選ばれた日本語のあらすじ。", hi)
	mkMachineIntro(t, w, "zh-Hans", "过期的机翻", lo, "not-the-chosen-hash")

	tr := &fakeTranslator{model: "mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls)
	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 1, st.WouldPrune)
	assert.Equal(t, 1, st.Pruned)

	rows := machineIntros(t, w)
	require.Len(t, rows, 1, "exactly one zh-Hans machine row remains")
	assert.Equal(t, hi, rows[0].SourceID, "the remaining row is at the chosen source")
	assert.Equal(t, "[译] 選ばれた日本語のあらすじ。", rows[0].Intro)

	tr2 := &fakeTranslator{model: "mt", fn: func(string) string { return "SHOULD-NOT-BE-CALLED" }}
	st, err = Run(ctx, tr2, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, 0, tr2.calls)
	assert.Equal(t, 1, st.SkipUnchanged)
	assert.Equal(t, 0, st.WouldRetranslate)
	assert.Equal(t, 0, st.WouldPrune)
}

func TestPruneOnlyNeedsNoTranslation(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	lo, hi := min(dlsite, bangumi), max(dlsite, bangumi)

	w := mkWork(t, medium, "prune-only", nil)
	mkIntro(t, w, "ja", "現行のあらすじ。", hi)

	tr := &fakeTranslator{model: "mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w}})
	require.NoError(t, err)
	require.Equal(t, 1, st.Inserted)
	callsAfterSeed := tr.calls

	var kept model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id = ? AND lang = 'zh-Hans' AND source_id = ?", w, hi).First(&kept).Error)

	mkMachineIntro(t, w, "zh-Hans", "过期的机翻", lo, "stray-hash")

	st, err = Run(ctx, tr, Opts{DSN: testDSN, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkipUnchanged)
	assert.Equal(t, 1, st.WouldPrune)
	assert.Equal(t, callsAfterSeed, tr.calls)
	assert.Empty(t, st.Samples)
	assert.Len(t, machineIntros(t, w), 2, "dry run deletes nothing")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET updated_at = now() - interval '1 second' WHERE id = ?`, w).Error)
	var before time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w).Scan(&before).Error)

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, callsAfterSeed, tr.calls, "prune-only must not call the translator")
	assert.Equal(t, 1, st.Pruned)
	assert.Empty(t, st.Samples)

	rows := machineIntros(t, w)
	require.Len(t, rows, 1)
	assert.Equal(t, hi, rows[0].SourceID)
	assert.Equal(t, kept.Intro, rows[0].Intro)
	assert.Equal(t, kept.SrcHash, rows[0].SrcHash)

	var after time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w).Scan(&after).Error)
	assert.True(t, after.After(before), "prune-only must touch the work so readers refetch")
}

func TestAFailedTranslationKeepsTheStray(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	lo, hi := min(dlsite, bangumi), max(dlsite, bangumi)

	w := mkWork(t, medium, "failed-keeps-stray", nil)
	mkIntro(t, w, "ja", "選ばれた日本語のあらすじ。", hi)
	mkMachineIntro(t, w, "zh-Hans", "过期的机翻", lo, "stray-hash")

	tr := &fakeTranslator{err: errors.New("gateway down")}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Errors)
	assert.Equal(t, 1, tr.calls)
	assert.Zero(t, st.Pruned)
	assert.Zero(t, st.Inserted)

	rows := machineIntros(t, w)
	require.Len(t, rows, 1)
	assert.Equal(t, lo, rows[0].SourceID, "the stray is still there")
	assert.EqualValues(t, 0, introCount(t,
		"WHERE work_id = ? AND lang = 'zh-Hans' AND provenance = 1 AND source_id = ?", w, hi),
		"no row at the chosen source")
}

func TestPruneTouchesOnlyStrays(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	lo, hi := min(dlsite, bangumi), max(dlsite, bangumi)

	w1 := mkWork(t, medium, "prune-target", nil)
	mkIntro(t, w1, "ja", "選ばれた日本語のあらすじ。", hi)
	mkMachineIntro(t, w1, "zh-Hans", "过期的机翻", lo, "stray-hash")
	mkMachineIntro(t, w1, "zh-Hant", "繁體機譯", lo, "hant-hash")

	w2 := mkWork(t, medium, "other-work", nil)
	mkIntro(t, w2, "ja", "別作品のあらすじ。", hi)
	mkMachineIntro(t, w2, "zh-Hans", "另一作品的机翻", lo, "other-hash")

	tr := &fakeTranslator{model: "mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, WorkIDs: []int64{w1}})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Pruned)

	assert.EqualValues(t, 0, introCount(t,
		"WHERE work_id = ? AND lang = 'zh-Hans' AND provenance = 1 AND source_id = ?", w1, lo),
		"the stray must go")
	assert.EqualValues(t, 1, introCount(t,
		"WHERE work_id = ? AND lang = 'zh-Hant' AND provenance = 1 AND source_id = ?", w1, lo),
		"zh-Hant machine row at the stray's source survives")
	assert.EqualValues(t, 1, introCount(t,
		"WHERE work_id = ? AND lang = 'ja' AND provenance = 0 AND source_id = ?", w1, hi),
		"ja source row survives")
	assert.EqualValues(t, 1, introCount(t,
		"WHERE work_id = ? AND lang = 'zh-Hans' AND provenance = 1 AND source_id = ?", w2, lo),
		"another work's zh-Hans machine row at the same source_id survives")
}
