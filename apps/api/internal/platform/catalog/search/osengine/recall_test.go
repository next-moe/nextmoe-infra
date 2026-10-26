package osengine

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/require"
)

const (
	openSearchTestHostEnv = "KUN_OPENSEARCH_TEST_HOST"
	recallDefaultLimit    = 8
)

type recallCase struct {
	Name        string `json:"name"`
	Index       string `json:"index"`
	Q           string `json:"q"`
	Limit       int    `json:"limit"`
	SearchIntro bool   `json:"search_intro"`
	MustID      string `json:"must_id"`
	MustRank    int    `json:"must_rank"`
	MustSubstr  string `json:"must_substr"`
}

func TestOpenSearchRecall(t *testing.T) {
	host := os.Getenv(openSearchTestHostEnv)
	if host == "" {
		if os.Getenv(dbtest.RequireSearchEnv) != "" {
			t.Fatalf("%s is set but %s is empty", dbtest.RequireSearchEnv, openSearchTestHostEnv)
		}
		t.Skipf("%s is empty", openSearchTestHostEnv)
	}

	cli, err := opensearch.NewClient(config.OpenSearchConfig{
		Host:        host,
		IndexPrefix: dbtest.SearchIndexPrefix("osrecall"),
	})
	require.NoError(t, err)
	defer func() {
		for _, uid := range IndexUIDs {
			err := cli.Do(context.Background(), http.MethodDelete, "/"+cli.IndexName(uid), nil, nil)
			if err != nil && !opensearch.IsNotFound(err) {
				t.Logf("delete %s: %v", cli.IndexName(uid), err)
			}
		}
	}()

	ctx := t.Context()
	eng := NewEngine(cli)
	cases := loadRecallCases(t)
	require.NoError(t, eng.EnsureIndexes(ctx))

	titles := make(map[string]string)
	var worksLines int
	for _, uid := range IndexUIDs {
		n := loadFixtureIndex(t, ctx, eng, uid, titles)
		if uid == IndexWorks {
			worksLines = n
			upsertExtra(t, eng, uid, separatorDocs(), titles)
		}
		upsertExtra(t, eng, uid, prefixDocs()[uid], titles)
		require.NoError(t, eng.Refresh(ctx, uid))
	}

	count, err := eng.Count(ctx, IndexWorks)
	require.NoError(t, err)
	require.Equal(t, int64(worksLines+len(separatorDocs())+len(prefixDocs()[IndexWorks])), count)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			assertRecall(t, tc, recallSearch(t, eng, tc), titles)
		})
	}
	for _, tc := range slices.Concat(separatorCases(), prefixCases()) {
		t.Run(tc.Name, func(t *testing.T) {
			assertRecall(t, tc, recallSearch(t, eng, tc), titles)
		})
	}
}

func upsertExtra(t *testing.T, eng *Engine, uid string, docs []BulkDoc, titles map[string]string) {
	t.Helper()
	if len(docs) == 0 {
		return
	}
	require.NoError(t, eng.Upsert(t.Context(), uid, docs))
	for _, d := range docs {
		src, _ := d.Source.(map[string]any)
		titles[d.ID] = titleBlobFromMap(src)
	}
}

func loadRecallCases(t *testing.T) []recallCase {
	t.Helper()
	var all []recallCase
	for _, name := range []string{"cjk_recall.json", "assoc_recall.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "fixture", name))
		require.NoError(t, err)
		var cases []recallCase
		require.NoError(t, json.Unmarshal(raw, &cases), name)
		all = append(all, cases...)
	}
	require.Len(t, all, 34)
	return all
}

func loadFixtureIndex(t *testing.T, ctx context.Context, eng *Engine, uid string, titles map[string]string) int {
	t.Helper()
	path := filepath.Join("testdata", "fixture", uid+".ndjson.gz")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gz.Close()

	var docs []BulkDoc
	dec := json.NewDecoder(gz)
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, path)
		id, _ := doc["id"].(string)
		require.NotEmpty(t, id, path)
		docs = append(docs, BulkDoc{ID: id, Source: doc})
		titles[id] = titleBlobFromMap(doc)
	}
	require.NoError(t, eng.Upsert(ctx, uid, docs))
	return len(docs)
}

func titleBlobFromMap(doc map[string]any) string {
	var b strings.Builder
	for _, k := range []string{"name_ja", "name_zh", "name_other", "latin"} {
		if s, ok := doc[k].(string); ok {
			b.WriteString(s)
		}
	}
	for _, k := range []string{"aliases_ja", "aliases_zh", "aliases_other"} {
		switch v := doc[k].(type) {
		case []any:
			for _, e := range v {
				if s, ok := e.(string); ok {
					b.WriteString(s)
				}
			}
		case []string:
			for _, s := range v {
				b.WriteString(s)
			}
		}
	}
	return b.String()
}

func recallSearch(t *testing.T, eng *Engine, tc recallCase) []string {
	t.Helper()
	limit := tc.Limit
	if limit <= 0 {
		limit = recallDefaultLimit
	}
	ctx := t.Context()
	if tc.Index == IndexWorks {
		res, err := eng.SearchWorks(ctx, spec.WorksQuery{
			Q:           tc.Q,
			Page:        1,
			Limit:       limit,
			SearchIntro: tc.SearchIntro,
			Filter:      spec.WorksFilter{OLang: spec.OLang{All: true}},
		})
		require.NoError(t, err)
		return res.DocIDs
	}
	res, err := eng.SearchEntities(ctx, tc.Index, spec.EntityQuery{Q: tc.Q, Limit: limit})
	require.NoError(t, err)
	ids := make([]string, 0, len(res.Hits))
	for _, raw := range res.Hits {
		var hit struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(raw, &hit))
		ids = append(ids, hit.ID)
	}
	return ids
}

func assertRecall(t *testing.T, tc recallCase, ids []string, titles map[string]string) {
	t.Helper()
	if tc.MustID != "" {
		require.Contains(t, ids, tc.MustID)
		if tc.MustRank > 0 {
			require.LessOrEqual(t, slices.Index(ids, tc.MustID)+1, tc.MustRank)
		}
	}
	if tc.MustSubstr == "" {
		return
	}
	var blob strings.Builder
	for _, id := range ids {
		blob.WriteString(titles[id])
	}
	require.Contains(t, blob.String(), tc.MustSubstr, "ids=%v", ids)
}

func separatorDocs() []BulkDoc {
	return []BulkDoc{
		{
			ID: "wsep1",
			Source: map[string]any{
				"id":          "wsep1",
				"entity_type": "work",
				"name_ja":     "ジュエリー・ハーツ・アカデミア",
				"popularity":  1.0,
				"sources":     []string{},
				"source_keys": []string{},
			},
		},
		{
			ID: "wsep2",
			Source: map[string]any{
				"id":          "wsep2",
				"entity_type": "work",
				"name_other":  "Fate/stay night",
				"popularity":  1.0,
				"sources":     []string{},
				"source_keys": []string{},
			},
		},
	}
}

func separatorCases() []recallCase {
	return []recallCase{
		{
			Name:     "separator omitted nakaguro",
			Index:    IndexWorks,
			Q:        "ジュエリーハーツアカデミア",
			MustID:   "wsep1",
			MustRank: 3,
		},
		{
			Name:     "separator Fate stay night",
			Index:    IndexWorks,
			Q:        "Fate stay night",
			MustID:   "wsep2",
			MustRank: 3,
		},
		{
			Name:     "separator fatestay night",
			Index:    IndexWorks,
			Q:        "fatestay night",
			MustID:   "wsep2",
			MustRank: 3,
		},
	}
}

func prefixDocs() map[string][]BulkDoc {
	doc := func(id, entityType, name string, popularity float64) BulkDoc {
		return BulkDoc{ID: id, Source: map[string]any{
			"id":          id,
			"entity_type": entityType,
			"name_other":  name,
			"popularity":  popularity,
			"sources":     []string{},
			"source_keys": []string{},
		}}
	}
	return map[string][]BulkDoc{
		IndexEngines: {
			doc("epre1", "engine", "GPK2", 1),
			doc("epre2", "engine", "SiglusEngine", 3),
			doc("epre3", "engine", "KiriKiri", 5),
			doc("epre4", "engine", "KiriKiri Z", 2),
		},
		IndexWorks: {
			doc("wpre1", "work", "Riddle Joker", 1),
			doc("wpre2", "work", "Zephyrwitch", 0),
			doc("wpre3", "work", "Zephyrwitch Legacy", 9.6),
			doc("wpre4", "work", "Zephyrwitch Medical Chart", 8.7),
			doc("wpre5", "work", "Quillmark Story", 0),
			doc("wpre6", "work", "Quillmark Tales", 0.5),
			doc("wpre7", "work", "Quillmark and the Long Night of Falling Stars", 8.8),
		},
	}
}

func prefixCases() []recallCase {
	return []recallCase{
		{Name: "prefix gpk -> GPK2", Index: IndexEngines, Q: "gpk", MustID: "epre1", MustRank: 1},
		{Name: "prefix siglus -> SiglusEngine", Index: IndexEngines, Q: "siglus", MustID: "epre2", MustRank: 1},
		{Name: "prefix keeps the exact name first", Index: IndexEngines, Q: "kirikiri", MustID: "epre3", MustRank: 1},
		{Name: "prefix kirikiri z", Index: IndexEngines, Q: "kirikiri z", MustID: "epre4", MustRank: 1},
		{Name: "prefix of a later word", Index: IndexWorks, Q: "riddle jok", MustID: "wpre1", MustRank: 3},
		{Name: "prefix of a single word", Index: IndexWorks, Q: "ridd", MustID: "wpre1", MustRank: 8},
		{Name: "an exact title outranks popular partial matches", Index: IndexWorks, Q: "zephyrwitch", MustID: "wpre2", MustRank: 1},
		{Name: "popularity orders equal word matches", Index: IndexWorks, Q: "quillmark", MustID: "wpre7", MustRank: 1},
	}
}
