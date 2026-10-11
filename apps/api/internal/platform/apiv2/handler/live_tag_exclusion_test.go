package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestLiveTagExclusion(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	parent := model.CatalogTag{Name: "live-exclusion-parent", Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, env.db.Create(&parent).Error)
	require.NoError(t, env.db.Create(&model.CatalogTagParent{TagID: fx.Tag, ParentID: parent.ID, Origin: model.TagParentOriginCurated, Note: "test"}).Error)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_tag_parent WHERE parent_id = ?`, parent.ID).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_tag WHERE id = ?`, parent.ID).Error)
	})

	type exclusion struct {
		Object  string   `json:"object"`
		TagIDs  []string `json:"tag_ids"`
		WorkIDs []string `json:"work_ids"`
	}
	get := func(query string) exclusion {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tag-exclusion?"+query, "", "")
		require.Equal(t, 200, status, string(body))
		var out exclusion
		require.NoError(t, json.Unmarshal(body, &out))
		return out
	}

	tree := get("exclude_tag_tree=" + idstr(parent.ID))
	require.Equal(t, "tag_exclusion", tree.Object)
	require.ElementsMatch(t, []string{idstr(parent.ID), idstr(fx.Tag)}, tree.TagIDs)
	require.Contains(t, tree.WorkIDs, idstr(fx.Attributed))

	exact := get("exclude_tag_id=" + idstr(parent.ID))
	require.Equal(t, []string{idstr(parent.ID)}, exact.TagIDs)
	require.NotNil(t, exact.WorkIDs)
	require.Empty(t, exact.WorkIDs, "an exact id never reaches its children's works")

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?ids="+idstr(fx.Attributed)+"&exclude_tag_tree="+idstr(parent.ID), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var batch struct {
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Equal(t, []string{idstr(fx.Attributed)}, batch.Missing, "the works face drops what this face lists")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tag-exclusion", "", "")
	require.Equal(t, 400, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tag-exclusion?exclude_tag_id=x", "", "")
	require.Equal(t, 400, status, string(body))
}
