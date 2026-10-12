package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Production shape: 2,720 ja works showed a localized.ja that was not their
// display_name, because a merged-in official title had the lower id.
func TestTheDisplayNameWinsItsLocaleOverAnOlderDuplicate(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, survivorDisplay)
	addWorkTitle(t, w.ID, "ja", loserJaTitle, model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "zh-Hans", "先来的中文名", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "ja", survivorDisplay, model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "zh-Hans", "后来的中文名", model.WorkTitleKindOfficial)

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, survivorDisplay, rec.Localized["ja"].Value, "detail face")
	assert.Equal(t, "先来的中文名", rec.Localized["zh-Hans"].Value, "elsewhere the id still decides")

	page, err := svc.WorksList(t.Context(), WorksListFilter{IDs: []int64{w.ID}, Include: ParseWorksListInclude("names")}, "", 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, survivorDisplay, page.Items[0].Localized["ja"].Value, "list face")

	names, err := svc.WorkNamesByID(t.Context(), []int64{w.ID})
	require.NoError(t, err)
	assert.Equal(t, survivorDisplay, names[w.ID].Localized["ja"].Value, "brief and search hydration")
}

func TestASourceTitleStillOutranksTheDisplayNameAsMachineText(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Machine Name")
	addWorkTitle(t, w.ID, "en", "Source Name", model.WorkTitleKindAlias)
	addWorkTitleP(t, w.ID, "en", "Machine Name", model.WorkTitleKindOfficial, model.WorkTitleProvenanceMachine)

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "Source Name", rec.Localized["en"].Value, "the tie-break sits below provenance and kind")
}
