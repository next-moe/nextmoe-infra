package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveFolderView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Visibility  string   `json:"visibility"`
	ItemCount   int      `json:"item_count"`
	Tags        []string `json:"tags"`
	CoverWorkID *string  `json:"cover_work_id"`
	CopiedFrom  *string  `json:"copied_from_folder_id"`
}

func TestLiveFolderTagsCoverAndUnlisted(t *testing.T) {
	env := liveCatalog(t)
	a := seedHoldableWork(t, env.db, "Tag A")
	b := seedHoldableWork(t, env.db, "Tag B")
	c := seedHoldableWork(t, env.db, "Tag C")

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/folders", liveUserToken,
		`{"name":"Live Tagged","visibility":"unlisted","tags":["#催泪"," 催泪 ","入坑向"]}`)
	require.Equal(t, 201, status, string(raw))
	var f liveFolderView
	require.NoError(t, json.Unmarshal(raw, &f))
	t.Cleanup(func() {
		env.db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderItem{})
		env.db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderTag{})
		env.db.Delete(&model.CatalogUserFolder{}, f.ID)
	})
	require.Equal(t, "unlisted", f.Visibility)
	require.Equal(t, []string{"催泪", "入坑向"}, f.Tags)
	require.Nil(t, f.CoverWorkID)
	require.Nil(t, f.CopiedFrom)
	for _, w := range []int64{a, b, c} {
		status, _, raw = liveDo(t, env, http.MethodPut, "/v2/me/folders/"+f.ID+"/items/"+idstr(w), liveUserToken, "")
		require.Equal(t, 200, status, string(raw))
	}

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/folders/"+f.ID, liveAppKey, "")
	require.Equal(t, 200, status, "an unlisted folder opens by id: %s", raw)
	page := func(url string) directoryPage {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, url, liveAppKey, "")
		require.Equal(t, 200, status, string(raw))
		var p directoryPage
		require.NoError(t, json.Unmarshal(raw, &p))
		return p
	}
	require.Empty(t, page("/v2/folders?tag=催泪&nsfw=true").Items, "but stays out of every listing")

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/folders/"+f.ID, liveUserToken,
		`{"visibility":"public","cover_work_id":"`+idstr(b)+`"}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &f))
	require.Equal(t, idstr(b), *f.CoverWorkID)
	listed := page("/v2/folders?tag=催泪&nsfw=true")
	require.Len(t, listed.Items, 1)
	require.Equal(t, f.ID, listed.Items[0].ID)
	require.Len(t, page("/v2/folders?q=live%20tag&nsfw=true").Items, 1, "q ignores case")
	require.Empty(t, page("/v2/folders?q=nothing-like-it&nsfw=true").Items)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/folders/tags?nsfw=true&limit=100", liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	var tags struct {
		Items []struct {
			Tag         string `json:"tag"`
			FolderCount int    `json:"folder_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &tags))
	require.Contains(t, tags.Items, struct {
		Tag         string `json:"tag"`
		FolderCount int    `json:"folder_count"`
	}{"入坑向", 1})

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/folders/"+f.ID, liveUserToken, `{"cover_work_id":"99887766"}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, "/cover_work_id", liveProblem(t, raw).Errors[0].Pointer)
	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/folders/"+f.ID, liveUserToken, `{"tags":["a","b","c","d","e","f"]}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, "/tags", liveProblem(t, raw).Errors[0].Pointer)
	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/folders/"+f.ID, liveUserToken, `{"cover_work_id":"","tags":[]}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &f))
	require.Nil(t, f.CoverWorkID)
	require.Equal(t, []string{}, f.Tags)

	trust := &fakeFolderTrust{decision: "deny"}
	env.cat.FolderTrust = trust
	t.Cleanup(func() { env.cat.FolderTrust = nil })
	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/folders/"+f.ID, liveUserToken, `{"tags":["违禁词"]}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, "/tags", liveProblem(t, raw).Errors[0].Pointer)
}

func TestLiveFolderCopyProgressAndUpdates(t *testing.T) {
	env := liveCatalog(t)
	a := seedHoldableWork(t, env.db, "Copy A")
	b := seedHoldableWork(t, env.db, "Copy B")
	c := seedHoldableWork(t, env.db, "Copy C")
	src := seedListFolder(t, env.db, livePlainUID, "Copy Source", a, b, c)

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/folders/copies", liveUserToken, `{"folder_id":"`+idstr(src)+`"}`)
	require.Equal(t, 201, status, string(raw))
	var cp liveFolderView
	require.NoError(t, json.Unmarshal(raw, &cp))
	t.Cleanup(func() {
		env.db.Where("folder_id = ?", cp.ID).Delete(&model.CatalogUserFolderItem{})
		env.db.Delete(&model.CatalogUserFolder{}, cp.ID)
	})
	require.Equal(t, "Copy Source", cp.Name)
	require.Equal(t, "源自「Copy Source」", cp.Description)
	require.Equal(t, "private", cp.Visibility)
	require.Equal(t, 3, cp.ItemCount)
	require.Equal(t, idstr(src), *cp.CopiedFrom)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/progress?folder_ids="+idstr(src)+","+cp.ID+",99887766", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var progress struct {
		Items []struct {
			FolderID  string `json:"folder_id"`
			ItemCount int    `json:"item_count"`
			Collected int    `json:"collected"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(raw, &progress))
	require.Len(t, progress.Items, 2)
	require.Equal(t, idstr(src), progress.Items[0].FolderID)
	require.Equal(t, 3, progress.Items[0].Collected, "the copy holds every work of the source")
	require.Equal(t, []string{"99887766"}, progress.Missing)
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/progress", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodPut, "/v2/me/followed-folders/"+idstr(src), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, env.db.Exec("UPDATE catalog_user_folder_follow SET seen_at = now() - interval '1 hour' WHERE folder_id = ?", src).Error)
	require.NoError(t, env.db.Exec("UPDATE catalog_user_folder_item SET created_at = now() - interval '2 hours' WHERE folder_id = ?", src).Error)
	d := seedHoldableWork(t, env.db, "Copy D")
	require.NoError(t, env.db.Create(&model.CatalogUserFolderItem{FolderID: src, WorkID: d, OwnerUID: livePlainUID, Position: 9 * model.FolderItemPositionGap}).Error)

	updates := func() int {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/followed-folders/updates", liveUserToken, "")
		require.Equal(t, 200, status, string(raw))
		var u struct {
			UpdatedCount int `json:"updated_count"`
		}
		require.NoError(t, json.Unmarshal(raw, &u))
		return u.UpdatedCount
	}
	require.Equal(t, 1, updates())
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/followed-folders/"+idstr(src), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var follow struct {
		NewItemCount int    `json:"new_item_count"`
		SeenAt       string `json:"seen_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &follow))
	require.Equal(t, 1, follow.NewItemCount)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/followed-folders/"+idstr(src)+"/seen", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &follow))
	require.Zero(t, follow.NewItemCount)
	require.Zero(t, updates())
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/followed-folders/"+idstr(src)+"/seen", liveUserToken, `{"at":"yesterday"}`)
	require.Equal(t, 422, status, string(raw))
}
