package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveAnonymousCatalogReadsMatchTheKeyedBody(t *testing.T) {
	env := liveCatalog(t)
	paths := []string{liveReadURL(t, "/v2/catalog/works", env.fx) + "?nsfw=true"}
	for _, tmpl := range liveReadPaths {
		if _, _, anonymous := v2Security(tmpl); anonymous {
			paths = append(paths, liveReadURL(t, tmpl, env.fx))
		}
	}
	require.Greater(t, len(paths), 40, "the sweep reached too few anonymous reads to mean anything")

	for _, path := range paths {
		status, _, keyed := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		require.Equalf(t, 200, status, "keyed GET %s: %s", path, keyed)
		status, _, anon := liveDo(t, env, http.MethodGet, path, "", "")
		require.Equalf(t, 200, status, "anonymous GET %s: %s", path, anon)
		require.JSONEqf(t, string(keyed), string(anon), "GET %s differs between a key and no credential", path)
	}

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/claim-events", "", "")
	require.Equal(t, http.StatusUnauthorized, status, string(body))
	require.Equal(t, "MISSING_CREDENTIAL", liveProblemCode(t, body))
}
