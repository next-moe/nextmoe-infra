package handler

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedCoverGraded(t *testing.T, db *gorm.DB, workID int64, hash string, sexual int16) {
	t.Helper()
	cv := &model.CatalogWorkCover{WorkID: workID, ImageHash: hash, Kind: "main", SourceID: 2, Sexual: sexual}
	require.NoError(t, db.Create(cv).Error)
	t.Cleanup(func() { db.Delete(cv) })
}

// An unclaimed all_ages work whose only cover is explicit is the shape the
// forum showed to sfw readers: it had no claim block, so the forum fell back on
// content_rating. The verdict now rides on the work itself.
func TestLiveContentLimitOnUnclaimedWorks(t *testing.T) {
	env := liveCatalog(t)

	explicit := seedHoldableWork(t, env.db, "Shelf Explicit Unclaimed")
	seedCoverGraded(t, env.db, explicit, strings.Repeat("e", 64), model.SexualExplicit)
	safe := seedHoldableWork(t, env.db, "Shelf Safe Unclaimed")
	seedCoverGraded(t, env.db, safe, strings.Repeat("f", 64), model.SexualSafe)

	for id, want := range map[int64]string{explicit: "nsfw", safe: "sfw"} {
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/catalog/works/"+idstr(id), liveAppKey, "")
		require.Equal(t, 200, status, string(raw))
		var w map[string]any
		require.NoError(t, json.Unmarshal(raw, &w), string(raw))
		require.Nil(t, w["claim"], "the fixture work is unclaimed")
		require.Equal(t, "all_ages", w["content_rating"])
		require.Equal(t, want, w["content_limit"], "detail %d", id)
	}

	url := "/v2/catalog/works?nsfw=true&fields=content_limit&ids=" + idstr(explicit) + "," + idstr(safe)
	status, _, raw := liveDo(t, env, http.MethodGet, url, liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Len(t, page.Items, 2)
	got := map[string]any{}
	for _, it := range page.Items {
		require.ElementsMatch(t, []string{"object", "id", "content_limit"}, slices.Collect(maps.Keys(it)), "fields=content_limit keeps only it: %s", raw)
		got[it["id"].(string)] = it["content_limit"]
	}
	require.Equal(t, map[string]any{idstr(explicit): "nsfw", idstr(safe): "sfw"}, got)
}
