package osengine

import (
	"context"
	"net/http"
	"os"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/require"
)

func TestSearchPersonsCollapsesCreditNamesToPeople(t *testing.T) {
	host := os.Getenv(openSearchTestHostEnv)
	if host == "" {
		if os.Getenv(dbtest.RequireSearchEnv) != "" {
			t.Fatalf("%s is set but %s is empty", dbtest.RequireSearchEnv, openSearchTestHostEnv)
		}
		t.Skipf("%s is empty", openSearchTestHostEnv)
	}
	cli, err := opensearch.NewClient(config.OpenSearchConfig{Host: host, IndexPrefix: dbtest.SearchIndexPrefix("ospersons")})
	require.NoError(t, err)
	eng := NewEngine(cli)
	require.NoError(t, eng.EnsureIndexes(t.Context()))
	defer func() {
		for _, uid := range IndexUIDs {
			_ = cli.Do(context.Background(), http.MethodDelete, "/"+cli.IndexName(uid), nil, nil)
		}
	}()

	name := func(id, n string, person any, popularity float64) BulkDoc {
		src := map[string]any{"id": id, "entity_type": "credit_name", "name_other": n, "popularity": popularity}
		if person != nil {
			src["person_id"] = person
		}
		return BulkDoc{ID: id, Source: src}
	}
	require.NoError(t, eng.Upsert(t.Context(), IndexCreditNames, []BulkDoc{
		name("n1", "Quorvan Ikeda", 7, 1),
		name("n2", "Quorvan Ikeda (alt)", 7, 1),
		name("n3", "Quorvan Sato", 8, 6),
		name("n4", "Quorvan Hidden", nil, 9),
		name("n5", "Someone Else", 9, 9),
	}))
	require.NoError(t, eng.Refresh(t.Context(), IndexCreditNames))

	res, err := eng.SearchPersons(t.Context(), spec.EntityQuery{Q: "quorv", Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{8, 7}, res.IDs, "one hit per person, prefix matched, popularity breaks the tie")
	require.EqualValues(t, 2, res.Total, "a credit name without a public person link finds no person")

	res, err = eng.SearchPersons(t.Context(), spec.EntityQuery{Q: "quorvan", Page: 2, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []int64{7}, res.IDs)
}
