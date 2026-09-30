package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestLiveCompanyFollowLifecycle(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/followed-companies/" + idstr(fx.Company)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(
			`DELETE FROM catalog_user_entity_follow WHERE actor_uid = ? AND entity_id = ?`,
			liveUID, fx.Company).Error)
	})

	status, _, body := liveDo(t, env, http.MethodPut, path, "", "")
	require.Equal(t, 401, status, string(body))

	status, _, body = liveDo(t, env, http.MethodPut, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var rec struct {
		Object    string `json:"object"`
		CompanyID string `json:"company_id"`
		CreatedAt string `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "company_follow", rec.Object)
	require.Equal(t, idstr(fx.Company), rec.CompanyID)

	status, _, body = liveDo(t, env, http.MethodPut, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var again struct {
		CreatedAt string `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(body, &again))
	require.Equal(t, rec.CreatedAt, again.CreatedAt)

	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/followed-companies", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items []struct {
			CompanyID string `json:"company_id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	var listed bool
	for _, it := range page.Items {
		if it.CompanyID == idstr(fx.Company) {
			listed = true
		}
	}
	require.True(t, listed)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/me/followed-companies?company_ids="+idstr(fx.Company)+","+idstr(fx.CompanyLogo), liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var batch struct {
		Items []struct {
			CompanyID string `json:"company_id"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Len(t, batch.Items, 1)
	require.Equal(t, idstr(fx.Company), batch.Items[0].CompanyID)
	require.Equal(t, []string{idstr(fx.CompanyLogo)}, batch.Missing)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/companies/"+idstr(fx.Company), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail struct {
		FollowerCount *int `json:"follower_count"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.FollowerCount)
	require.Equal(t, 1, *detail.FollowerCount)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/companies?ids="+idstr(fx.Company), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var listedCompanies struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &listedCompanies))
	require.NotEmpty(t, listedCompanies.Items)
	for _, it := range listedCompanies.Items {
		_, ok := it["follower_count"]
		require.False(t, ok)
	}

	status, _, _ = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status)
	status, _, _ = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/companies/"+idstr(fx.Company), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.FollowerCount)
	require.Equal(t, 0, *detail.FollowerCount)

	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 404, status, string(body))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, body).Code)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/followed-companies/999999999", liveUserToken, "")
	require.Equal(t, 404, status, string(body))
}

func TestLiveCompanyFollowPages(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	third := &model.CatalogLabel{DisplayName: "Third Followed", Kind: model.LabelKindGameBrand, FieldProvenance: datatypes.JSON([]byte("{}"))}
	require.NoError(t, env.db.Create(third).Error)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_entity_follow WHERE actor_uid = ?`, liveUID).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_label WHERE id = ?`, third.ID).Error)
	})
	followed := []int64{fx.Company, fx.CompanyLogo, third.ID}
	for _, id := range followed {
		status, _, body := liveDo(t, env, http.MethodPut, "/v2/me/followed-companies/"+idstr(id), liveUserToken, "")
		require.Equal(t, 200, status, string(body))
	}

	type page struct {
		Items []struct {
			CompanyID string `json:"company_id"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
		Total      *int64  `json:"total"`
	}
	read := func(url string) page {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, url, liveUserToken, "")
		require.Equal(t, 200, status, string(body))
		var p page
		require.NoError(t, json.Unmarshal(body, &p))
		return p
	}
	first := read("/v2/me/followed-companies?limit=2&include_total=true")
	require.Len(t, first.Items, 2)
	require.Equal(t, idstr(third.ID), first.Items[0].CompanyID)
	require.Equal(t, idstr(fx.CompanyLogo), first.Items[1].CompanyID)
	require.NotNil(t, first.Total)
	require.Equal(t, int64(3), *first.Total)
	require.NotNil(t, first.NextCursor)
	require.Regexp(t, `^cur_`, *first.NextCursor)

	second := read("/v2/me/followed-companies?limit=2&cursor=" + *first.NextCursor)
	require.Len(t, second.Items, 1)
	require.Equal(t, idstr(fx.Company), second.Items[0].CompanyID)
	require.Nil(t, second.NextCursor)
}

func TestLiveCompanyFollowOnAMergedID(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	const mergedAway = int64(90101)
	require.NoError(t, env.db.Create(&model.CatalogRedirect{
		EntityType: model.EntityTypeLabel, OldID: mergedAway, CurrentID: fx.Company,
	}).Error)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_redirect WHERE entity_type = ? AND old_id = ?`,
			model.EntityTypeLabel, mergedAway).Error)
	})

	status, _, body := liveDo(t, env, http.MethodPut, "/v2/me/followed-companies/"+idstr(mergedAway), liveUserToken, "")
	require.Equal(t, 404, status, string(body))
	p := liveProblem(t, body)
	require.Equal(t, problem.CodeEntityMerged, p.Code)
	require.Equal(t, idstr(fx.Company), p.CurrentID)

	var n int64
	require.NoError(t, env.db.Raw(`SELECT count(*) FROM catalog_user_entity_follow WHERE actor_uid = ?`, liveUID).Scan(&n).Error)
	require.Zero(t, n)
}

func TestLiveMyCalendar(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	empty := datatypes.JSON([]byte("{}"))
	y, m := int16(2024), int16(1)

	mkWork := func(name, olang string) int64 {
		t.Helper()
		w := &model.CatalogWork{
			MediumID: 1, OLang: olang, DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
			Extra: empty, FieldProvenance: empty,
		}
		require.NoError(t, env.db.Create(w).Error)
		rel := &model.CatalogRelease{
			WorkID: w.ID, Kind: model.ReleaseKindDefault,
			ReleasedY: &y, ReleasedM: &m, Extra: empty, FieldProvenance: empty,
		}
		require.NoError(t, env.db.Create(rel).Error)
		return w.ID
	}
	mkLabel := func(name string, kind int16) int64 {
		t.Helper()
		l := &model.CatalogLabel{DisplayName: name, Kind: kind, FieldProvenance: empty}
		require.NoError(t, env.db.Create(l).Error)
		return l.ID
	}
	link := func(workID, labelID int64) {
		t.Helper()
		require.NoError(t, env.db.Create(&model.CatalogWorkLabel{
			WorkID: workID, LabelID: labelID, Kind: model.WorkLabelKindBrand,
		}).Error)
	}

	zh := mkWork("Followed Chinese", "zh-Hans")
	link(zh, fx.Company)
	circle := mkLabel("Followed Circle", model.LabelKindDoujinCircle)
	circleWork := mkWork("Followed Doujin", "ja")
	link(circleWork, circle)
	other := mkLabel("Unfollowed Brand", model.LabelKindGameBrand)
	otherWork := mkWork("Unfollowed Release", "ja")
	link(otherWork, other)

	var extraWorks = []int64{zh, circleWork, otherWork}
	var extraLabels = []int64{circle, other}
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_entity_follow WHERE actor_uid = ?`, liveUID).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_release WHERE work_id IN ?`, extraWorks).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_work_label WHERE work_id IN ?`, extraWorks).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_work WHERE id IN ?`, extraWorks).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_label WHERE id IN ?`, extraLabels).Error)
	})

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/calendar?month=2024-01", "", "")
	require.Equal(t, 401, status, string(body))

	for _, id := range []int64{fx.Company, circle} {
		status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/followed-companies/"+idstr(id), liveUserToken, "")
		require.Equal(t, 200, status, string(body))
	}

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/calendar?month=2024-01", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	got := map[string]bool{}
	for _, it := range page.Items {
		got[it.ID] = true
	}
	require.Equal(t, map[string]bool{
		idstr(fx.Work):    true,
		idstr(zh):         true,
		idstr(circleWork): true,
	}, got)
	require.False(t, got[idstr(otherWork)])
}
