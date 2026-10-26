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

func TestCompactKeywordMatchesTheIndexNormalizer(t *testing.T) {
	host := os.Getenv(openSearchTestHostEnv)
	if host == "" {
		if os.Getenv(dbtest.RequireSearchEnv) != "" {
			t.Fatalf("%s is set but %s is empty", dbtest.RequireSearchEnv, openSearchTestHostEnv)
		}
		t.Skipf("%s is empty", openSearchTestHostEnv)
	}
	cli, err := opensearch.NewClient(config.OpenSearchConfig{Host: host, IndexPrefix: dbtest.SearchIndexPrefix("oskw")})
	require.NoError(t, err)
	eng := NewEngine(cli)
	require.NoError(t, eng.EnsureIndexes(t.Context()))
	defer func() {
		for _, uid := range IndexUIDs {
			_ = cli.Do(context.Background(), http.MethodDelete, "/"+cli.IndexName(uid), nil, nil)
		}
	}()

	for _, text := range []string{
		"Ren'Py", "Ren’Py", "D.C. ～ダ・カーポ～", "Fate/stay night [Réalta Nua]", "ＲＰＧ Maker",
		"「サクラノ詩」", "Muv-Luv Alternative", "けもの　フレンズ", "SAGA ＰＬＡＮＥＴＳ", "Riddle Joker!!",
		"Café ＆ Coffee", "ゆずソフト（YUZUSOFT）", "千恋＊万花", "Kanon (2002)", "「ＷＨＩＴＥ ＡＬＢＵＭ２」",
		"ﾊﾟﾗﾀﾞｲｽ", "Tears to Tiara 2: Heir of the Overlord", "Little Busters! Ecstasy", "Hello, World.",
	} {
		var resp struct {
			Tokens []struct {
				Token string `json:"token"`
			} `json:"tokens"`
		}
		require.NoError(t, cli.Do(t.Context(), http.MethodPost, "/"+cli.IndexName(IndexEngines)+"/_analyze",
			map[string]any{"normalizer": "kw_norm", "text": text}, &resp), text)
		require.Len(t, resp.Tokens, 1, text)
		require.Equal(t, resp.Tokens[0].Token, CompactKeyword(text), text)
	}
}

func TestKwStripPatternEscapesClassMetacharacters(t *testing.T) {
	p := kwStripPattern()
	require.Contains(t, p, `\[`)
	require.Contains(t, p, `\]`)
	require.Contains(t, p, `\-`)
	require.Contains(t, p, `\&`)
	require.Contains(t, p, "＇")
	require.Equal(t, "renpy", CompactKeyword("Ren'Py"))
	require.Equal(t, "dcダカーポ", CompactKeyword("D.C. ～ダ・カーポ～"))
}
