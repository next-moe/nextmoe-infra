package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveTagBlock struct {
	Object    string `json:"object"`
	TagID     string `json:"tag_id"`
	Mode      string `json:"mode"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func TestLiveTagBlockLifecycle(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/blocked-tags/" + idstr(fx.Tag)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_tag_block WHERE actor_uid = ?`, liveUID).Error)
	})

	status, _, body := liveDo(t, env, http.MethodPut, path, "", "")
	require.Equal(t, 401, status, string(body))

	status, _, body = liveDo(t, env, http.MethodPut, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var rec liveTagBlock
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, liveTagBlock{Object: "tag_block", TagID: idstr(fx.Tag), Mode: "tree", CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt}, rec)

	status, _, body = liveDo(t, env, http.MethodPut, path, liveUserToken, `{"mode":"exact"}`)
	require.Equal(t, 200, status, string(body))
	var exact liveTagBlock
	require.NoError(t, json.Unmarshal(body, &exact))
	require.Equal(t, "exact", exact.Mode)
	require.Equal(t, rec.CreatedAt, exact.CreatedAt)

	status, _, body = liveDo(t, env, http.MethodPut, path, liveUserToken, `{"mode":"subtree"}`)
	require.Contains(t, []int{400, 422}, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "exact", rec.Mode)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/me/blocked-tags?tag_ids="+idstr(fx.Tag)+","+idstr(fx.TagSexual), liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var batch struct {
		Items   []liveTagBlock `json:"items"`
		Missing []string       `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Len(t, batch.Items, 1)
	require.Equal(t, idstr(fx.Tag), batch.Items[0].TagID)
	require.Equal(t, []string{idstr(fx.TagSexual)}, batch.Missing)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/blocked-tags/"+idstr(fx.TagSexual), liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/blocked-tags?limit=1&include_total=true", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items      []liveTagBlock `json:"items"`
		NextCursor *string        `json:"next_cursor"`
		Total      *int64         `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.TagSexual), page.Items[0].TagID, "newest block first")
	require.NotNil(t, page.NextCursor)
	require.NotNil(t, page.Total)
	require.EqualValues(t, 2, *page.Total)
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/blocked-tags?limit=1&cursor="+*page.NextCursor, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	page.NextCursor = nil
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.Tag), page.Items[0].TagID)
	require.Nil(t, page.NextCursor)

	status, _, _ = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status)
	status, _, _ = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status)
	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 404, status, string(body))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, body).Code)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/blocked-tags/999999999", liveUserToken, "")
	require.Equal(t, 404, status, string(body))
}

func TestLiveTagBlockCap(t *testing.T) {
	env := liveCatalog(t)
	var ids []int64
	for i := 0; i < model.TagBlocksPerUserMax+1; i++ {
		tag := model.CatalogTag{Name: "live-cap-tag-" + idstr(int64(i)), Tier: model.TagTierLongtail, Kind: model.TagKindContent}
		require.NoError(t, env.db.Create(&tag).Error)
		ids = append(ids, tag.ID)
	}
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_tag_block WHERE actor_uid = ?`, liveUID).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_tag WHERE id IN ?`, ids).Error)
	})
	for _, id := range ids[:model.TagBlocksPerUserMax] {
		require.NoError(t, env.db.Create(&model.CatalogUserTagBlock{ActorUID: liveUID, TagID: id, ClientID: liveClient}).Error)
	}
	status, _, body := liveDo(t, env, http.MethodPut, "/v2/me/blocked-tags/"+idstr(ids[model.TagBlocksPerUserMax]), liveUserToken, "")
	require.Equal(t, 422, status, string(body))
	require.Equal(t, problem.CodeValidationFailed, liveProblem(t, body).Code)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/blocked-tags/"+idstr(ids[0]), liveUserToken, `{"mode":"exact"}`)
	require.Equal(t, 200, status, "changing the mode of a stored block is not a new block: %s", body)
}

func TestLiveWorksExcludeTags(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	parent := model.CatalogTag{Name: "live-exclude-parent", Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, env.db.Create(&parent).Error)
	require.NoError(t, env.db.Create(&model.CatalogTagParent{TagID: fx.Tag, ParentID: parent.ID, Origin: model.TagParentOriginCurated, Note: "test"}).Error)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_tag_parent WHERE parent_id = ?`, parent.ID).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_tag WHERE id = ?`, parent.ID).Error)
	})

	listed := func(query string) bool {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=100&"+query, liveAppKey, "")
		require.Equal(t, 200, status, string(body))
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		for _, it := range page.Items {
			if it.ID == idstr(fx.Attributed) {
				return true
			}
		}
		return false
	}
	require.True(t, listed("claimed=true"), "positive control: the tagged work is on the page")
	require.False(t, listed("claimed=true&exclude_tag_id="+idstr(fx.Tag)))
	require.True(t, listed("claimed=true&exclude_tag_id="+idstr(parent.ID)), "exact on the parent leaves the child's works")
	require.False(t, listed("claimed=true&exclude_tag_tree="+idstr(parent.ID)))
	require.True(t, listed("claimed=true&exclude_tag_tree="+idstr(fx.TagSexual)))

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?ids="+idstr(fx.Attributed)+"&exclude_tag_tree="+idstr(parent.ID), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var batch struct {
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Equal(t, []string{idstr(fx.Attributed)}, batch.Missing)

	many := make([]string, 101)
	for i := range many {
		many[i] = idstr(int64(i + 1))
	}
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/works?exclude_tag_tree="+strings.Join(many, ","), liveAppKey, "")
	require.Equal(t, 400, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/works?exclude_tag_id=abc", liveAppKey, "")
	require.Equal(t, 400, status, string(body))
}
