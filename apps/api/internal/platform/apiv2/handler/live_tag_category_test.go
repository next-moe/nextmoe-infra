package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveTagCategory struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Order       int    `json:"order"`
	TagCount    int    `json:"tag_count"`
}

type liveCategorizedTag struct {
	ID          string `json:"id"`
	WorkCount   int    `json:"work_count"`
	TagCategory *struct {
		Object      string `json:"object"`
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"tag_category"`
}

func TestLiveTagCategories(t *testing.T) {
	env := liveCatalog(t)
	genre := model.TagCategoryGenre
	mk := func(name string, cat *int16, works int) int64 {
		tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent, CategoryID: cat}
		require.NoError(t, env.db.Create(&tag).Error)
		if works > 0 {
			require.NoError(t, env.db.Exec(`INSERT INTO catalog_tag_work_count (tag_id, n_all, n_sfw, n_nsfw, computed_at)
				VALUES (?, ?, ?, 0, now())`, tag.ID, works, works).Error)
		}
		return tag.ID
	}
	few, many, also, loose := mk("live-cat-few", &genre, 5), mk("live-cat-many", &genre, 9), mk("live-cat-also", &genre, 5), mk("live-cat-loose", nil, 0)
	ids := []int64{few, many, also, loose}
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_tag_work_count WHERE tag_id IN ?`, ids)
		env.db.Exec(`DELETE FROM catalog_tag WHERE id IN ?`, ids)
	})

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tag-categories", "", "")
	require.Equal(t, 200, status, string(body))
	var cats struct {
		Items []liveTagCategory `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &cats))
	require.Len(t, cats.Items, len(model.TagCategorySeed))
	require.Equal(t, liveTagCategory{Object: "tag_category", ID: "genre", DisplayName: "题材类型", Order: 10, TagCount: 3}, cats.Items[0])
	require.Equal(t, "adult", cats.Items[len(cats.Items)-1].ID)

	var page struct {
		Items      []liveCategorizedTag `json:"items"`
		NextCursor *string              `json:"next_cursor"`
	}
	var order []string
	path := "/v2/catalog/tags?category=genre&sort=work_count&limit=2"
	for {
		status, _, body = liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		require.Equal(t, 200, status, string(body))
		page.NextCursor = nil
		require.NoError(t, json.Unmarshal(body, &page))
		for _, it := range page.Items {
			order = append(order, it.ID)
		}
		if page.NextCursor == nil {
			break
		}
		path = "/v2/catalog/tags?category=genre&sort=work_count&limit=2&cursor=" + *page.NextCursor
	}
	require.Equal(t, []string{idstr(many), idstr(few), idstr(also)}, order, "work_count descending, ties by id, across pages")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(many), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail liveCategorizedTag
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.TagCategory)
	require.Equal(t, "tag_category", detail.TagCategory.Object)
	require.Equal(t, "genre", detail.TagCategory.ID)
	require.Equal(t, "题材类型", detail.TagCategory.DisplayName)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags?category=none&ids="+idstr(loose)+","+idstr(few), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var none struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &none))
	require.Len(t, none.Items, 1)
	require.JSONEq(t, `"`+idstr(loose)+`"`, string(none.Items[0]["id"]))
	require.JSONEq(t, `null`, string(none.Items[0]["tag_category"]), "an uncategorized tag carries null, not an absent key")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags?category=bogus", liveAppKey, "")
	require.Equal(t, 400, status, string(body))
	require.Contains(t, string(body), "UNKNOWN_ENUM_VALUE")
}
