package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeriesIntroHumanLaneWinsTheLangFold(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := newPublicSvc()

	series := model.CatalogSeries{DisplayName: "シリーズ", SourceID: curatedIntroSource}
	require.NoError(t, testDB.Create(&series).Error)
	require.NoError(t, testDB.Create(&[]model.CatalogSeriesIntro{
		{SeriesID: series.ID, Lang: "ja", Intro: "上流の紹介", SourceID: bangumiIntroSource},
		{SeriesID: series.ID, Lang: "ja", Intro: "人手の紹介", SourceID: curatedIntroSource},
		{SeriesID: series.ID, Lang: "en", Intro: "upstream", SourceID: bangumiIntroSource},
	}).Error)

	rec, ok, err := svc.SeriesDetail(ctx, series.ID, false, true, 10, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, rec.Intros, 2, "one intro per language")
	byLang := map[string]string{}
	for _, in := range rec.Intros {
		byLang[in.Lang] = in.Intro + "/" + in.Source
	}
	assert.Equal(t, "人手の紹介/curated", byLang["ja"])
	assert.Equal(t, "upstream/bangumi", byLang["en"])
}
