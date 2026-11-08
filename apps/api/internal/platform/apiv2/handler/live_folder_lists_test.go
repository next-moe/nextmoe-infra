package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"
	"api/pkg/trustclient"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type folderItemsPage struct {
	Items []struct {
		WorkID   string `json:"work_id"`
		Position int64  `json:"position"`
		Note     string `json:"note"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type directoryPage struct {
	Items []struct {
		ID            string   `json:"id"`
		FollowerCount int      `json:"follower_count"`
		ContentRating string   `json:"content_rating"`
		RecentWorkIDs []string `json:"recent_work_ids"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type fakeFolderTrust struct {
	decision string
	checked  []string
	reports  []trustclient.ReportRequest
}

func (f *fakeFolderTrust) Check(_ context.Context, req trustclient.CheckRequest) (string, []string, error) {
	f.checked = append(f.checked, req.Text)
	return f.decision, nil, nil
}

func (f *fakeFolderTrust) Scan(context.Context, trustclient.ScanRequest) (int64, bool, error) {
	return 1, false, nil
}

func (f *fakeFolderTrust) Report(_ context.Context, req trustclient.ReportRequest) (int64, error) {
	f.reports = append(f.reports, req)
	return 1, nil
}

func seedListFolder(t *testing.T, db *gorm.DB, owner int64, name string, works ...int64) int64 {
	t.Helper()
	f := &model.CatalogUserFolder{OwnerUID: owner, Name: name, Visibility: model.FolderVisibilityPublic, ItemCount: len(works)}
	require.NoError(t, db.Create(f).Error)
	for i, w := range works {
		require.NoError(t, db.Create(&model.CatalogUserFolderItem{
			FolderID: f.ID, WorkID: w, OwnerUID: owner, Position: int64(i+1) * model.FolderItemPositionGap,
		}).Error)
	}
	t.Cleanup(func() {
		db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderItem{})
		db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderFollow{})
		db.Delete(f)
	})
	return f.ID
}

func TestLiveFolderItemPatchAndPositionSort(t *testing.T) {
	env := liveCatalog(t)
	a := seedHoldableWork(t, env.db, "Order A")
	b := seedHoldableWork(t, env.db, "Order B")
	c := seedHoldableWork(t, env.db, "Order C")
	folder := seedListFolder(t, env.db, liveUID, "Order Folder", a, b, c)
	base := "/v2/me/folders/" + idstr(folder) + "/items"

	status, _, raw := liveDo(t, env, http.MethodPatch, base+"/"+idstr(c), liveUserToken,
		`{"before_work_id":"`+idstr(a)+`","note":"  先玩这个  "}`)
	require.Equal(t, 200, status, string(raw))
	var item struct {
		Note     string `json:"note"`
		Position int64  `json:"position"`
	}
	require.NoError(t, json.Unmarshal(raw, &item))
	require.Equal(t, "先玩这个", item.Note)

	status, _, raw = liveDo(t, env, http.MethodGet, base+"/"+idstr(c), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &item))
	require.Equal(t, "先玩这个", item.Note)
	status, _, raw = liveDo(t, env, http.MethodGet, base+"/99887766", liveUserToken, "")
	require.Equal(t, 404, status, string(raw))
	others := seedListFolder(t, env.db, livePlainUID, "Order Others", a)
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/"+idstr(others)+"/items/"+idstr(a), liveUserToken, "")
	require.Equal(t, 404, status, string(raw))

	var got []string
	cursor := ""
	for {
		url := base + "?sort=position&limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		status, _, raw = liveDo(t, env, http.MethodGet, url, liveUserToken, "")
		require.Equal(t, 200, status, string(raw))
		var page folderItemsPage
		require.NoError(t, json.Unmarshal(raw, &page))
		for _, it := range page.Items {
			got = append(got, it.WorkID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	require.Equal(t, []string{idstr(c), idstr(a), idstr(b)}, got)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/folders/"+idstr(folder)+"/items?sort=position", liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	var pub folderItemsPage
	require.NoError(t, json.Unmarshal(raw, &pub))
	require.Equal(t, idstr(c), pub.Items[0].WorkID)
	require.Equal(t, "先玩这个", pub.Items[0].Note, "the note is public with the folder")

	status, _, raw = liveDo(t, env, http.MethodPatch, base+"/"+idstr(c), liveUserToken,
		`{"after_work_id":"`+idstr(a)+`","before_work_id":"`+idstr(b)+`"}`)
	require.Equal(t, 422, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodPatch, base+"/"+idstr(c), liveUserToken, `{"after_work_id":"99887766"}`)
	require.Equal(t, 422, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodPatch, base+"/99887766", liveUserToken, `{"note":"x"}`)
	require.Equal(t, 404, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodPatch, base+"/"+idstr(c), liveUserToken, `{}`)
	require.Equal(t, 422, status, string(raw))

	trust := &fakeFolderTrust{decision: "deny"}
	env.cat.FolderTrust = trust
	t.Cleanup(func() { env.cat.FolderTrust = nil })
	status, _, raw = liveDo(t, env, http.MethodPatch, base+"/"+idstr(a), liveUserToken, `{"note":"违禁词"}`)
	require.Equal(t, 422, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.Equal(t, "/note", p.Errors[0].Pointer)
	require.Equal(t, []string{"违禁词"}, trust.checked)
}

func TestLiveFolderDirectoryAndFollows(t *testing.T) {
	env := liveCatalog(t)
	var works []int64
	for _, n := range []string{"Dir A", "Dir B", "Dir C"} {
		works = append(works, seedHoldableWork(t, env.db, n))
	}
	adult := seedHoldableWork(t, env.db, "Dir Adult")
	require.NoError(t, env.db.Model(&model.CatalogWork{}).Where("id = ?", adult).Update("content_rating", model.ContentRatingR18).Error)
	listed := seedListFolder(t, env.db, livePlainUID, "Dir Listed", works...)
	adultList := seedListFolder(t, env.db, livePlainUID, "Dir Adult List", works[0], works[1], adult)
	short := seedListFolder(t, env.db, livePlainUID, "Dir Short", works[0])

	ids := func(url string) directoryPage {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, url, liveAppKey, "")
		require.Equal(t, 200, status, string(raw))
		var page directoryPage
		require.NoError(t, json.Unmarshal(raw, &page))
		return page
	}
	contains := func(page directoryPage, id int64) bool {
		for _, it := range page.Items {
			if it.ID == idstr(id) {
				return true
			}
		}
		return false
	}
	page := ids("/v2/folders?contains_work_id=" + idstr(works[0]) + "&limit=100")
	require.True(t, contains(page, listed))
	for _, it := range page.Items {
		if it.ID == idstr(listed) {
			require.ElementsMatch(t, []string{idstr(works[0]), idstr(works[1]), idstr(works[2])}, it.RecentWorkIDs)
		}
	}
	require.False(t, contains(page, adultList), "nsfw absent leaves r18 folders out of the directory")
	require.False(t, contains(page, short), "a folder below the item floor is not a curated list")
	page = ids("/v2/folders?contains_work_id=" + idstr(works[0]) + "&nsfw=true&limit=100")
	require.True(t, contains(page, adultList))
	for _, it := range page.Items {
		if it.ID == idstr(adultList) {
			require.Equal(t, "r18", it.ContentRating)
		}
	}
	page = ids("/v2/folders?owner_uid=" + idstr(livePlainUID) + "&limit=100")
	require.True(t, contains(page, short), "the owner lane lists every public folder")
	require.True(t, contains(page, adultList), "the owner lane ignores nsfw")

	status, _, raw := liveDo(t, env, http.MethodPut, "/v2/me/followed-folders/"+idstr(listed), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var follow struct {
		Object string `json:"object"`
		Folder struct {
			FollowerCount int `json:"follower_count"`
		} `json:"folder"`
	}
	require.NoError(t, json.Unmarshal(raw, &follow))
	require.Equal(t, "folder_follow", follow.Object)
	require.Equal(t, 1, follow.Folder.FollowerCount)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/followed-folders?folder_ids="+idstr(listed)+","+idstr(short), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var batch struct {
		Items   []json.RawMessage `json:"items"`
		Missing []string          `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(raw, &batch))
	require.Len(t, batch.Items, 1)
	require.Equal(t, []string{idstr(short)}, batch.Missing)

	own := seedListFolder(t, env.db, liveUID, "Dir Own", works...)
	status, _, raw = liveDo(t, env, http.MethodPut, "/v2/me/followed-folders/"+idstr(own), liveUserToken, "")
	require.Equal(t, 422, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodDelete, "/v2/me/followed-folders/"+idstr(listed), liveUserToken, "")
	require.Equal(t, 204, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/followed-folders/"+idstr(listed), liveUserToken, "")
	require.Equal(t, 404, status, string(raw))
}

func TestLiveFolderReport(t *testing.T) {
	env := liveCatalog(t)
	w := seedHoldableWork(t, env.db, "Report Work")
	folder := seedListFolder(t, env.db, livePlainUID, "Report Folder", w)
	url := "/v2/me/folder-reports"
	body := func(rest string) string { return `{"folder_id":"` + idstr(folder) + `",` + rest + `}` }

	status, _, raw := liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"spam"`))
	require.Equal(t, 503, status, "an unbound trust client is unavailable, not a silent drop: %s", raw)

	trust := &fakeFolderTrust{}
	env.cat.FolderTrust = trust
	t.Cleanup(func() { env.cat.FolderTrust = nil })

	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"spam","note":" 广告 "`))
	require.Equal(t, 204, status, string(raw))
	require.Len(t, trust.reports, 1)
	r := trust.reports[0]
	require.Equal(t, RatingTrustSite, r.Site)
	require.Equal(t, FolderTrustSubjectKind, r.SubjectKind)
	require.Equal(t, idstr(folder), r.SubjectID)
	require.Equal(t, liveUID, r.ReporterID)
	require.Equal(t, livePlainUID, *r.AuthorID)
	require.Equal(t, "广告", *r.Note)
	require.True(t, strings.HasPrefix(*r.Snapshot, "名称: Report Folder"))

	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"spam","work_id":"`+idstr(w)+`"`))
	require.Equal(t, 422, status, "a work without a note has nothing to report: %s", raw)
	require.NoError(t, env.db.Model(&model.CatalogUserFolderItem{}).Where("folder_id = ?", folder).Update("note", "引流").Error)
	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"abuse","work_id":"`+idstr(w)+`"`))
	require.Equal(t, 204, status, string(raw))
	require.Equal(t, FolderNoteTrustSubjectKind, trust.reports[1].SubjectKind)
	require.Equal(t, idstr(folder)+":"+idstr(w), trust.reports[1].SubjectID)
	require.Equal(t, "引流", *trust.reports[1].Snapshot)

	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"bogus"`))
	require.Equal(t, 422, status, string(raw))
	other := seedHoldableWork(t, env.db, "Report Elsewhere")
	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"spam","work_id":"`+idstr(other)+`"`))
	require.Equal(t, 422, status, string(raw))
	require.NoError(t, env.db.Model(&model.CatalogUserFolder{}).Where("id = ?", folder).Update("visibility", model.FolderVisibilityPrivate).Error)
	status, _, raw = liveDo(t, env, http.MethodPost, url, liveUserToken, body(`"reason":"spam"`))
	require.Equal(t, 404, status, string(raw))
}
