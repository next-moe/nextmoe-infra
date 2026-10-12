package intromt

import (
	"context"
	"testing"

	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	curatedJa = "キュレーターが書いたあらすじ。"
	promoJa   = "DLsite限定特典付き！今すぐ購入。"
)

func curatedSource(t *testing.T) int16 {
	t.Helper()
	var curated int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curated).Error)
	require.NotZero(t, curated)
	return curated
}

func shownIntro(t *testing.T, workID int64, lang string) service.WorkIntroRow {
	t.Helper()
	d, err := service.NewReadService(testDB).WorkByIDPublic(context.Background(), workID, 0, false,
		service.ParsePublicFields("intros"))
	require.NoError(t, err)
	for _, in := range d.Intros {
		if in.Lang == lang {
			return in
		}
	}
	require.Failf(t, "no intro on the read side", "work %d lang %s", workID, lang)
	return service.WorkIntroRow{}
}

func TestTheMachineZhTranslatesTheJaTheReaderSees(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, _ := reg(t)
	curated := curatedSource(t)
	site := "kungal"

	w := mkWork(t, medium, "curated-and-dlsite", &site)
	mkIntro(t, w, "ja", promoJa, dlsite)
	mkIntro(t, w, "ja", curatedJa, curated)
	mkMachineIntro(t, w, "zh-Hans", "[译] "+promoJa, dlsite, hashSource(promoJa))
	require.Equal(t, curatedJa, shownIntro(t, w, "ja").Intro)

	reg2, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	cands, err := loadCandidates(ctx, testDB, reg2, PopulationClaimed, SourceJa, 0, 0, []int64{w})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	assert.Equal(t, curated, cands[0].JaSourceID)
	assert.Equal(t, curatedJa, cands[0].JaText)
	assert.Nil(t, cands[0].MZhID)
	assert.True(t, cands[0].MZhStrays)

	tr := &fakeTranslator{model: "mt", fn: func(ja string) string { return "[译] " + ja }}
	opts := Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed, WorkIDs: []int64{w}}
	st, err := Run(ctx, tr, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls)
	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 1, st.Pruned)

	rows := machineIntros(t, w)
	require.Len(t, rows, 1, "the DLsite-sourced machine row goes as a stray")
	assert.Equal(t, curated, rows[0].SourceID)
	assert.Equal(t, hashSource(curatedJa), rows[0].SrcHash)

	zh := shownIntro(t, w, "zh-Hans")
	assert.True(t, zh.Machine)
	assert.Equal(t, curated, zh.SourceID)
	assert.Equal(t, "[译] "+curatedJa, zh.Intro)

	st, err = Run(ctx, tr, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls, "the next night skips it")
	assert.Equal(t, 1, st.SkipUnchanged)
	assert.Zero(t, st.WouldPrune)
}

func TestImporterOnlyWorksKeepTheLowestSource(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	site := "kungal"

	dlsiteOnly := mkWork(t, medium, "dlsite-only", &site)
	mkIntro(t, dlsiteOnly, "ja", promoJa, dlsite)

	twoImporters := mkWork(t, medium, "bangumi-and-dlsite", &site)
	mkIntro(t, twoImporters, "ja", "DLsiteのあらすじ。", dlsite)
	mkIntro(t, twoImporters, "ja", "バンガミのあらすじ。", bangumi)
	lo := min(dlsite, bangumi)

	tr := &fakeTranslator{model: "mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed,
		WorkIDs: []int64{dlsiteOnly, twoImporters}})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Inserted)
	assert.Zero(t, st.Pruned)

	rows := machineIntros(t, dlsiteOnly)
	require.Len(t, rows, 1)
	assert.Equal(t, dlsite, rows[0].SourceID)
	assert.Equal(t, "[译] "+promoJa, shownIntro(t, dlsiteOnly, "zh-Hans").Intro)

	rows = machineIntros(t, twoImporters)
	require.Len(t, rows, 1)
	assert.Equal(t, lo, rows[0].SourceID)
	ja := shownIntro(t, twoImporters, "ja")
	assert.Equal(t, lo, ja.SourceID)
	assert.Equal(t, "[译] "+ja.Intro, shownIntro(t, twoImporters, "zh-Hans").Intro)
}

func TestTheEnglishLaneTranslatesTheEnTheReaderSees(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)
	var vndb int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)
	require.NotZero(t, vndb)
	site := "kungal"

	const curatedEn, vndbEn = "Our own English blurb.", "The VNDB description."
	w := mkWork(t, medium, "en-curated-and-vndb", &site)
	mkIntro(t, w, "en", vndbEn, vndb)
	mkIntro(t, w, "en", curatedEn, curated)
	mkMachineIntro(t, w, "zh-Hans", "[译] "+vndbEn, vndb, hashSource(vndbEn))
	require.Equal(t, curatedEn, shownIntro(t, w, "en").Intro)

	tr := &fakeTranslator{model: "mt", fn: func(src string) string { return "[译] " + src }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed,
		SourceLang: SourceEn, WorkIDs: []int64{w}})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 1, st.Pruned)

	rows := machineIntros(t, w)
	require.Len(t, rows, 1)
	assert.Equal(t, curated, rows[0].SourceID)
	assert.Equal(t, "[译] "+curatedEn, shownIntro(t, w, "zh-Hans").Intro)
}
