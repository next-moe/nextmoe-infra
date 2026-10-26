package handler

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestLiveMintClaimTitleWithNullLatin(t *testing.T) {
	env := liveCatalog(t)

	status, rec, p := liveCreateClaim(t, env, livePlainToken,
		`{"display_name":"Null Latin Mint","field_values":{"catalog.work.olang":"ja",`+
			`"catalog.work.titles":[{"lang":"ja","title":"ヌルラテン","kind":0,"latin":null}]}}`)
	require.Equal(t, 201, status, p.Detail)

	var rows []model.CatalogWorkTitle
	require.NoError(t, env.db.Where("work_id = ?", liveNum(t, rec.ID)).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "ヌルラテン", rows[0].Title)
	require.Nil(t, rows[0].Latin)
}
