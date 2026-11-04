package osengine

import (
	"context"
	"net/http"
	"os"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/require"
)

func TestChineseAnalyzersFoldTraditionalOntoSimplified(t *testing.T) {
	host := os.Getenv(openSearchTestHostEnv)
	if host == "" {
		if os.Getenv(dbtest.RequireSearchEnv) != "" {
			t.Fatalf("%s is set but %s is empty", dbtest.RequireSearchEnv, openSearchTestHostEnv)
		}
		t.Skipf("%s is empty", openSearchTestHostEnv)
	}
	cli, err := opensearch.NewClient(config.OpenSearchConfig{Host: host, IndexPrefix: dbtest.SearchIndexPrefix("osanalysis")})
	require.NoError(t, err)
	name := cli.IndexName(IndexWorks)
	ctx := context.Background()
	_ = cli.Do(ctx, http.MethodDelete, "/"+name, nil, nil)
	body, err := IndexBody(IndexWorks)
	require.NoError(t, err)
	require.NoError(t, cli.Do(ctx, http.MethodPut, "/"+name, body, nil))
	t.Cleanup(func() { _ = cli.Do(context.Background(), http.MethodDelete, "/"+name, nil, nil) })

	tokens := func(analyzer, text string) []string {
		var out struct {
			Tokens []struct {
				Token string `json:"token"`
			} `json:"tokens"`
		}
		require.NoError(t, cli.Do(ctx, http.MethodPost, "/"+name+"/_analyze",
			map[string]any{"analyzer": analyzer, "text": text}, &out))
		got := make([]string, len(out.Tokens))
		for i, tok := range out.Tokens {
			got[i] = tok.Token
		}
		return got
	}
	// Only the ngram lane can promise equal tokens: icu_tokenizer segments by
	// dictionary before the transform runs, so 秋葉 is one word while 秋叶 is two
	// characters, and no ICU char filter can fold the script earlier.
	for _, pair := range [][2]string{{"白晝夢", "白昼梦"}, {"秋葉", "秋叶"}, {"戀愛", "恋爱"}} {
		require.Equal(t, tokens("cjk_ngram", pair[1]), tokens("cjk_ngram", pair[0]), "%s vs %s", pair[0], pair[1])
	}
}
