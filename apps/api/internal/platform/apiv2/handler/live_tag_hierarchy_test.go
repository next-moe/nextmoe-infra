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

type liveTagNode struct {
	ID        string   `json:"id"`
	IsSexual  bool     `json:"is_sexual"`
	ParentIDs []string `json:"parent_ids"`
}

func TestLiveTagTree(t *testing.T) {
	env := liveCatalog(t)
	mk := func(name string) int64 {
		tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: name == "live-tree-d"}
		require.NoError(t, env.db.Create(&tag).Error)
		return tag.ID
	}
	root, a, b, c, d, outside := mk("live-tree-root"), mk("live-tree-a"), mk("live-tree-b"), mk("live-tree-c"), mk("live-tree-d"), mk("live-tree-outside")
	require.NoError(t, env.db.Create(&[]model.CatalogTagParent{
		{TagID: a, ParentID: root, Origin: model.TagParentOriginVNDB},
		{TagID: b, ParentID: root, Origin: model.TagParentOriginVNDB},
		{TagID: c, ParentID: a, Origin: model.TagParentOriginVNDB},
		{TagID: c, ParentID: b, Origin: model.TagParentOriginVNDB},
		{TagID: c, ParentID: outside, Origin: model.TagParentOriginCurated, Note: "reviewed"},
		{TagID: d, ParentID: c, Origin: model.TagParentOriginVNDB},
	}).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_tag_parent WHERE tag_id IN ?`, []int64{a, b, c, d})
		env.db.Exec(`DELETE FROM catalog_tag WHERE id IN ?`, []int64{root, a, b, c, d, outside})
	})
	get := func(id int64, include string) (map[string]json.RawMessage, int) {
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(id)+include, liveAppKey, "")
		require.Equal(t, 200, status, string(body))
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &out))
		var count int
		require.NoError(t, json.Unmarshal(out["descendant_count"], &count))
		return out, count
	}
	nodes := func(raw json.RawMessage) []liveTagNode {
		var out []liveTagNode
		require.NoError(t, json.Unmarshal(raw, &out))
		return out
	}

	body, count := get(root, "?include=descendants")
	desc := nodes(body["descendants"])
	require.Equal(t, []liveTagNode{
		{ID: idstr(a), ParentIDs: []string{idstr(root)}},
		{ID: idstr(b), ParentIDs: []string{idstr(root)}},
		{ID: idstr(c), ParentIDs: []string{idstr(a), idstr(b)}},
		{ID: idstr(d), IsSexual: true, ParentIDs: []string{idstr(c)}},
	}, desc, "nearest first; c's parent outside the subtree is not named")
	require.Len(t, desc, count)
	_, ok := body["ancestors"]
	require.False(t, ok, "ancestors must be absent unless asked for")

	body, _ = get(d, "?include=ancestors,descendants")
	require.Equal(t, []liveTagNode{
		{ID: idstr(c), ParentIDs: []string{idstr(a), idstr(b), idstr(outside)}},
		{ID: idstr(a), ParentIDs: []string{idstr(root)}},
		{ID: idstr(b), ParentIDs: []string{idstr(root)}},
		{ID: idstr(outside), ParentIDs: []string{}},
		{ID: idstr(root), ParentIDs: []string{}},
	}, nodes(body["ancestors"]))
	require.Equal(t, "[]", string(body["descendants"]), "a leaf answers an empty array, not null")

	body, _ = get(c, "?view=full")
	_, ok = body["descendants"]
	require.False(t, ok, "view=full does not expand the subtree")

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/catalog/tag-exclusion?exclude_tag_tree="+idstr(root), liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	var excl struct {
		TagIDs []string `json:"tag_ids"`
	}
	require.NoError(t, json.Unmarshal(raw, &excl))
	want := []string{idstr(root)}
	for _, n := range desc {
		want = append(want, n.ID)
	}
	require.ElementsMatch(t, want, excl.TagIDs, "descendants are what exclude_tag_tree adds")
}
