package entityintromt

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	curatedJa = "人手で書いた紹介。明るく面倒見がよい。"
	vndbJa    = "VNDBの紹介文。ヒロインの一人。"
)

func shownCharacterIntro(t *testing.T, charID int64, lang string) service.WorkIntroRow {
	t.Helper()
	d, err := service.NewReadService(testDB).CharacterByID(context.Background(), charID, model.SpoilerSevere)
	require.NoError(t, err)
	for _, in := range d.Intros {
		if in.Lang == lang {
			return in
		}
	}
	require.Failf(t, "no intro on the read side", "character %d lang %s", charID, lang)
	return service.WorkIntroRow{}
}

func TestCharacterMachineZhTranslatesTheJaTheReaderSees(t *testing.T) {
	clean(t)
	vndb, _ := srcIDs(t)
	curated := curatedSourceID(t)
	ctx := context.Background()

	id := mkCharacter(t, "curated-and-vndb")
	mkCharIntro(t, id, "ja", vndbJa, vndb)
	mkCharIntro(t, id, "ja", curatedJa, curated)
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "VNDB来源的机翻", SourceID: vndb,
		Provenance: model.IntroProvenanceMachine, SrcHash: hashSource(vndbJa), MTModel: "old-mt",
	}).Error)
	require.Equal(t, curatedJa, shownCharacterIntro(t, id, "ja").Intro)

	cands, err := loadCandidates(ctx, testDB, charLane(t), 0, 0, []int64{id})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	assert.Equal(t, curated, cands[0].SourceID)
	assert.Equal(t, curatedJa, cands[0].Text)
	assert.True(t, cands[0].MZhStrays)

	tr := &countingTranslator{}
	opts := Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter, EntityIDs: []int64{id}}
	st, err := Run(ctx, tr, opts)
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.Equal(t, 1, st[0].Inserted)
	assert.Equal(t, 1, st[0].Pruned)

	rows := charMachines(t, id)
	require.Len(t, rows, 1, "the VNDB-sourced machine row goes as a stray")
	assert.Equal(t, curated, rows[0].SourceID)
	zh := shownCharacterIntro(t, id, "zh-Hans")
	assert.Equal(t, curated, zh.SourceID)
	assert.Equal(t, rows[0].Intro, zh.Intro)

	st, err = Run(ctx, tr, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.calls, "the next night skips it")
	assert.Equal(t, 1, st[0].SkipUnchanged)
}

func TestLabelMachineZhTranslatesTheJaTheReaderSees(t *testing.T) {
	clean(t)
	vndb, _ := srcIDs(t)
	curated := curatedSourceID(t)
	ctx := context.Background()

	id := mkLabel(t, "curated-and-vndb-label")
	mkLabelIntro(t, id, "ja", vndbJa, vndb)
	mkLabelIntro(t, id, "ja", curatedJa, curated)

	public := service.NewPublicService(testDB, service.NewReadService(testDB), nil, "")
	shown := func(lang string) string {
		l, ok, err := public.Label(ctx, id, false, false, 0, 0)
		require.NoError(t, err)
		require.True(t, ok)
		for _, in := range l.Intros {
			if in.Lang == lang {
				return in.Intro
			}
		}
		return ""
	}
	require.Equal(t, curatedJa, shown("ja"))

	st, err := Run(ctx, &countingTranslator{}, Opts{DSN: testDSN, Apply: true, Lane: LaneLabel, EntityIDs: []int64{id}})
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.Equal(t, 1, st[0].Inserted)

	row, ok := labelIntroRow(t, id, "zh-Hans")
	require.True(t, ok)
	assert.Equal(t, curated, row.SourceID)
	assert.EqualValues(t, model.IntroProvenanceMachine, row.Provenance)
	assert.Equal(t, row.Intro, shown("zh-Hans"))
}
