package handler

import (
	"testing"

	"api/internal/platform/apiv2/collect"
	catsvc "api/internal/platform/catalog/service"

	"github.com/stretchr/testify/require"
)

func TestNameSearchListPagesByRelevancePage(t *testing.T) {
	rows := []catsvc.EntityListRow{{ID: 4}, {ID: 2}}
	conv := func(r catsvc.EntityListRow) int64 { return r.ID }

	out := nameSearchList(collect.Query{IncludeTotal: true}, catsvc.NameSearchPage{Items: rows, Total: 5, Page: 1, Limit: 2}, conv)
	require.Equal(t, []int64{4, 2}, out.Items)
	require.NotNil(t, out.NextCursor)
	require.Equal(t, collect.EncodeCursor("2"), *out.NextCursor)
	require.EqualValues(t, 5, *out.Total)

	out = nameSearchList(collect.Query{}, catsvc.NameSearchPage{Items: rows, Total: 4, Page: 2, Limit: 2}, conv)
	require.Nil(t, out.NextCursor, "the last page has no next cursor")
	require.Nil(t, out.Total, "total only when asked")

	out = nameSearchList(collect.Query{Page: 3}, catsvc.NameSearchPage{Items: rows, Total: 9, Page: 3, Limit: 2}, conv)
	require.EqualValues(t, 9, *out.Total, "page mode always reports the total")
}
