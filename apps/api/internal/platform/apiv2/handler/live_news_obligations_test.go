package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"
	newsmodel "api/internal/platform/news/model"

	"github.com/gofiber/fiber/v3"
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
		Attribution string `json:"attribution"`
		ColumnURL   string `json:"column_url"`
	} `json:"source"`
	SourceURL string `json:"source_url"`
	Lane      string `json:"lane"`
}

const (
	obligHash        = "1111111111111111111111111111111111111111111111111111111111111111"
	obligAttribution = "reprinted from Oblig; click the title for the original"
	obligColumnURL   = "https://source.example.test/column"
)

func seedObligationSource(t *testing.T, env *liveEnv, key, column string) {
	t.Helper()
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES (?, 'Oblig Source', 'https://source.example.test', ?, ?, ?, true)
		ON CONFLICT (key) DO UPDATE SET homepage_url = EXCLUDED.homepage_url,
			attribution = EXCLUDED.attribution, column_url = EXCLUDED.column_url, active = true`,
		key, obligAttribution, liveUID, column).Error)
}

func seedObligationItemIn(t *testing.T, env *liveEnv, source, external, lane string, status int16, hash string) string {
	t.Helper()
	require.NoError(t, env.db.Exec(`DELETE FROM news_item WHERE external_id = ?`, external).Error)
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
			source_url, banner_hash, banner_origin_url, published_at, status)
		VALUES (?, ?, '', ?, 't', 'p', 'https://source.example.test/x', ?, '', now(), ?)`,
		source, lane, external, hash, status).Error)
	var id int64
	require.NoError(t, env.db.Raw(`SELECT id FROM news_item WHERE external_id = ?`, external).Scan(&id).Error)
	return idstr(id)
}

func seedObligationItem(t *testing.T, env *liveEnv, external string, status int16, hash string) string {
	t.Helper()
	seedObligationSource(t, env, "oblig_src", obligColumnURL)
	return seedObligationItemIn(t, env, "oblig_src", external, newsmodel.LaneNews, status, hash)
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

// Obligation 1, the half v2 dropped. Galgame 批评's only condition was 注明出处,
// and the shape we promised was the reprint notice at the top of every item plus
// a link to the column index. Both live on the source object because a consumer
// that renders one item, or mixes sources into one stream, has nothing else to
// build them from -- and a notice the consumer composes itself is not the
// partner's words. v1 shipped them; v2's first cut did not.
func TestLiveNewsSourceCarriesTheReprintNotice(t *testing.T) {
	env := liveCatalog(t)
	id := seedObligationItem(t, env, "oblig-attr", newsmodel.StatusPublished, "")

	_, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+id, "", "")
	var v newsPublicView
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	require.Equal(t, obligAttribution, v.Source.Attribution,
		"the reprint notice must be the source's own text, not something downstream composes")
	require.Equal(t, obligColumnURL, v.Source.ColumnURL)

	// Control: a source that publishes no column ships an empty string. Without
	// this, filling column_url from the wrong row would still pass above.
	seedObligationSource(t, env, "oblig_nocol", "")
	bare := seedObligationItemIn(t, env, "oblig_nocol", "oblig-nocol", newsmodel.LaneNews,
		newsmodel.StatusPublished, "")
	_, _, raw2 := liveDo(t, env, http.MethodGet, "/v2/news/"+bare, "", "")
	var v2 newsPublicView
	require.NoError(t, json.Unmarshal(raw2, &v2), string(raw2))
	require.Empty(t, v2.Source.ColumnURL)
	require.Equal(t, obligAttribution, v2.Source.Attribution,
		"a source without a column still owes its reprint notice")
}

// lane was a declared filter on /v2/news with no field to read it back from:
// you could narrow the feed by it and never learn what you had been given.
func TestLiveNewsItemReportsItsLane(t *testing.T) {
	env := liveCatalog(t)
	seedObligationSource(t, env, "oblig_src", obligColumnURL)
	column := seedObligationItemIn(t, env, "oblig_src", "oblig-lane-col", newsmodel.LaneColumn,
		newsmodel.StatusPublished, "")
	news := seedObligationItemIn(t, env, "oblig_src", "oblig-lane-news", newsmodel.LaneNews,
		newsmodel.StatusPublished, "")

	for id, want := range map[string]string{column: newsmodel.LaneColumn, news: newsmodel.LaneNews} {
		_, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+id, "", "")
		var v newsPublicView
		require.NoError(t, json.Unmarshal(raw, &v), string(raw))
		require.Equal(t, want, v.Lane)
	}

	// The field and the filter must be the same fact. A feed narrowed to
	// lane=column may not contain the news item, and every row it does return
	// must say so itself.
	_, _, raw := liveDo(t, env, http.MethodGet, "/v2/news?lane=column&source=oblig_src&limit=100", "", "")
	var page struct {
		Items []newsPublicView `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	ids := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		require.Equal(t, newsmodel.LaneColumn, it.Lane)
		ids = append(ids, it.ID)
	}
	require.Contains(t, ids, column)
	require.NotContains(t, ids, news)
}

// 2.17.0 shipped the 410 above and left the operation declaring only 404, with
// a description that still said "Withdrawn items are 404". contractWalk cannot
// catch that: it asserts that a status it OBSERVED is declared, and observing
// 410 needs a withdrawn row, which the credential-less walk has no database
// for. So the declaration is asserted here, beside the behaviour it describes.
// A mirror generating its client from the spec had no 410 branch while nine
// withdrawn rows sat in production.
func TestNewsGoneIsDeclaredNotJustImplemented(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := Setup(app).OpenAPI()

	item := doc.Paths["/v2/news/{id}"]
	require.NotNil(t, item)
	require.NotNil(t, item.Get)
	require.Contains(t, item.Get.Responses, "410",
		"the detail face answers 410 for a withdrawn item; the contract must say so")
	require.NotContains(t, item.Get.Description, "are 404",
		"the description outlived the behaviour once already")

	// Control: the list face has no gone semantics -- a withdrawn item simply
	// leaves it -- so a blanket 410 everywhere cannot pass as a fix.
	require.NotContains(t, doc.Paths["/v2/news"].Get.Responses, "410")
}
