package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"
	newsmodel "api/internal/platform/news/model"

	"github.com/stretchr/testify/require"
)

// The three obligations D16 traded the credential gate for live in
// refs/api-v2/03-resources.md §2.4. Two of them were unmet on the wire: the
// source shipped without its homepage, and a withdrawn item answered 404.
type newsPublicView struct {
	ID     string `json:"id"`
	Banner *struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	} `json:"banner"`
	Source struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		HomepageURL string `json:"homepage_url"`
	} `json:"source"`
	SourceURL string `json:"source_url"`
}

const obligHash = "1111111111111111111111111111111111111111111111111111111111111111"

func seedObligationItem(t *testing.T, env *liveEnv, external string, status int16, hash string) string {
	t.Helper()
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES ('oblig_src', 'Oblig Source', 'https://source.example.test', 'reprinted from Oblig', ?, '', true)
		ON CONFLICT (key) DO UPDATE SET homepage_url = EXCLUDED.homepage_url, active = true`, liveUID).Error)
	require.NoError(t, env.db.Exec(`DELETE FROM news_item WHERE external_id = ?`, external).Error)
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
			source_url, banner_hash, banner_origin_url, published_at, status)
		VALUES ('oblig_src', ?, '', ?, 't', 'p', 'https://source.example.test/x', ?, '', now(), ?)`,
		newsmodel.LaneNews, external, hash, status).Error)
	var id int64
	require.NoError(t, env.db.Raw(`SELECT id FROM news_item WHERE external_id = ?`, external).Scan(&id).Error)
	return idstr(id)
}

// Obligation 1: 署名必发 -- the source object must carry display_name AND the
// site homepage, on every item, so a consumer rendering one item alone can
// still attribute it.
func TestLiveNewsSourceCarriesItsHomepage(t *testing.T) {
	env := liveCatalog(t)
	id := seedObligationItem(t, env, "oblig-home", newsmodel.StatusPublished, "")

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+id, "", "")
	require.Equal(t, http.StatusOK, status, string(raw))

	var v newsPublicView
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	require.Equal(t, "Oblig Source", v.Source.DisplayName)
	require.Equal(t, "https://source.example.test", v.Source.HomepageURL,
		"obligation 1 requires the source's own site, not just its name")
	require.NotEmpty(t, v.SourceURL)
}

// Obligation 2: the type carries `banner`, and B10 forbids publishing a bare
// content hash -- so it ships as the one Image type, url and all.
func TestLiveNewsBannerIsAnImageNotAHash(t *testing.T) {
	env := liveCatalog(t)
	id := seedObligationItem(t, env, "oblig-banner", newsmodel.StatusPublished, obligHash)

	_, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+id, "", "")
	var v newsPublicView
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	require.NotNil(t, v.Banner, "an item with a banner_hash must ship a banner")
	require.Equal(t, obligHash, v.Banner.Hash)
	require.NotEmpty(t, v.Banner.URL, "B10: never a bare hash")
	require.Contains(t, v.Banner.URL, liveNewsCDNBase)
	require.Contains(t, v.Banner.URL, obligHash)

	// Negative control: no hash means null, not an empty Image.
	none := seedObligationItem(t, env, "oblig-nobanner", newsmodel.StatusPublished, "")
	_, _, raw2 := liveDo(t, env, http.MethodGet, "/v2/news/"+none, "", "")
	var v2 newsPublicView
	require.NoError(t, json.Unmarshal(raw2, &v2), string(raw2))
	require.Nil(t, v2.Banner)
}

// Obligation 3: a withdrawn item answers 410, not 404. The face is public and
// credential-less, so mirrors exist; one that only sees the item leave the list
// never learns the copy it already took was pulled.
func TestLiveNewsWithdrawnDetailIsGone(t *testing.T) {
	env := liveCatalog(t)
	gone := seedObligationItem(t, env, "oblig-withdrawn", newsmodel.StatusWithdrawn, "")

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+gone, "", "")
	require.Equal(t, http.StatusGone, status, string(raw))
	require.Equal(t, problem.CodeGone, liveProblem(t, raw).Code)

	// Positive control: an id that never existed is still 404, so "everything
	// is 410 now" cannot pass as a fix.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news/98765432", "", "")
	require.Equal(t, http.StatusNotFound, status, string(raw))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, raw).Code)

	// And a pending item stays 404: it was never published, so no mirror can
	// hold a copy to invalidate.
	pending := seedObligationItem(t, env, "oblig-pending", newsmodel.StatusPending, "")
	status, _, _ = liveDo(t, env, http.MethodGet, "/v2/news/"+pending, "", "")
	require.Equal(t, http.StatusNotFound, status)
}
