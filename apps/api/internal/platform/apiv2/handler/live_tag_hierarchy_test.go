package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveTagRef struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	IsSexual    bool   `json:"is_sexual"`
}

type liveTagHierarchy struct {
	ID              string        `json:"id"`
	Parents         []liveTagRef  `json:"parents"`
	ChildCount      int           `json:"child_count"`
	DescendantCount int           `json:"descendant_count"`
	Children        *[]liveTagRef `json:"children"`
}

func TestLiveTagHierarchy(t *testing.T) {
	env := liveCatalog(t)
	mk := func(name string, sexual bool) int64 {
		tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: sexual}
		require.NoError(t, env.db.Create(&tag).Error)
		return tag.ID
	}
	root, mid, leaf, other := mk("live-hier-root", false), mk("live-hier-mid", true), mk("live-hier-leaf", false), mk("live-hier-other", false)
	require.NoError(t, env.db.Create(&[]model.CatalogTagParent{
		{TagID: mid, ParentID: root, Origin: model.TagParentOriginVNDB},
		{TagID: leaf, ParentID: mid, Origin: model.TagParentOriginVNDB},
		{TagID: leaf, ParentID: other, Origin: model.TagParentOriginCurated, Note: "reviewed"},
	}).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_tag_parent WHERE tag_id IN ?`, []int64{mid, leaf})
		env.db.Exec(`DELETE FROM catalog_tag WHERE id IN ?`, []int64{root, mid, leaf, other})
	})

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(root)+"?include=children", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var got liveTagHierarchy
	require.NoError(t, json.Unmarshal(body, &got))
	require.NotNil(t, got.Parents, "parents is an empty array on a root, never null")
	require.Empty(t, got.Parents)
	require.Equal(t, 1, got.ChildCount)
	require.Equal(t, 2, got.DescendantCount)
	require.NotNil(t, got.Children)
	require.Equal(t, []liveTagRef{{Object: "tag", ID: idstr(mid), DisplayName: "live-hier-mid", IsSexual: true}}, *got.Children)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(leaf), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	_, ok := bare["children"]
	require.False(t, ok, "children must be absent without include=children")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags?ids="+idstr(leaf)+","+idstr(mid), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var list struct {
		Items []liveTagHierarchy `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Items, 2)
	byID := map[string]liveTagHierarchy{}
	for _, it := range list.Items {
		byID[it.ID] = it
	}
	require.Equal(t, []liveTagRef{
		{Object: "tag", ID: idstr(mid), DisplayName: "live-hier-mid", IsSexual: true},
		{Object: "tag", ID: idstr(other), DisplayName: "live-hier-other"},
	}, byID[idstr(leaf)].Parents)
	require.Equal(t, 0, byID[idstr(leaf)].DescendantCount)
	require.Equal(t, 1, byID[idstr(mid)].ChildCount)
	require.Equal(t, 1, byID[idstr(mid)].DescendantCount)
	require.Nil(t, byID[idstr(mid)].Children, "the list lane does not carry children")
}
