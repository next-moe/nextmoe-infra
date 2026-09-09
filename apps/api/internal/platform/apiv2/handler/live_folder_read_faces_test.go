package handler

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type holdingsPage struct {
	Object string `json:"object"`
	Items  []struct {
		Object    string   `json:"object"`
		WorkID    string   `json:"work_id"`
		FolderIDs []string `json:"folder_ids"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type holdersPage struct {
	Object string `json:"object"`
	Items  []struct {
		Object   string `json:"object"`
		OwnerUID string `json:"owner_uid"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type folderListPage struct {
	Items []struct {
		ID         string `json:"id"`
		OwnerUID   string `json:"owner_uid"`
		Visibility string `json:"visibility"`
		IsDefault  bool   `json:"is_default"`
		ItemCount  int    `json:"item_count"`
		CreatedAt  string `json:"created_at"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"items"`
	Total      *int64  `json:"total"`
	NextCursor *string `json:"next_cursor"`
}

// A work nothing else in the fixture touches, so the holder set is exactly what
// this test seeds.
func seedHoldableWork(t *testing.T, db *gorm.DB, name string) int64 {
	t.Helper()
	empty := datatypes.JSON([]byte("{}"))
	w := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	require.NoError(t, db.Create(w).Error)
	t.Cleanup(func() { db.Delete(w) })
	return w.ID
}

func seedFolderHolding(t *testing.T, db *gorm.DB, ownerUID, workID int64, visibility int16, name string) int64 {
	t.Helper()
	f := &model.CatalogUserFolder{OwnerUID: ownerUID, Name: name, Visibility: visibility, ItemCount: 1}
	require.NoError(t, db.Create(f).Error)
	item := &model.CatalogUserFolderItem{FolderID: f.ID, WorkID: workID, OwnerUID: ownerUID}
	require.NoError(t, db.Create(item).Error)
	t.Cleanup(func() {
		db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderItem{})
		db.Delete(f)
	})
	return f.ID
}

func TestLiveFolderHoldingsBatch(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx

	held := seedHoldableWork(t, env.db, "Holdings Target")
	a := seedFolderHolding(t, env.db, liveUID, held, model.FolderVisibilityPrivate, "Holdings A")
	b := seedFolderHolding(t, env.db, liveUID, held, model.FolderVisibilityPublic, "Holdings B")
	unheld := seedHoldableWork(t, env.db, "Holdings Miss")
	// Somebody else's folder on the same work: the answer is the bearer's own
	// membership, never anybody else's.
	seedFolderHolding(t, env.db, livePlainUID, held, model.FolderVisibilityPublic, "Stranger's")

	url := "/v2/me/folders/holdings?work_ids=" + idstr(held) + "," + idstr(unheld) + ",99887766"
	status, ct, raw := liveDo(t, env, http.MethodGet, url, liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, ct, "json")

	var page holdingsPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Equal(t, "list", page.Object)
	require.Nil(t, page.NextCursor, "this lane does not paginate")
	require.Len(t, page.Items, 1, "a work held in no folder is left out, and so is an id that names nothing")
	require.Equal(t, "folder_holding", page.Items[0].Object)
	require.Equal(t, idstr(held), page.Items[0].WorkID)
	require.Equal(t, []string{idstr(min(a, b)), idstr(max(a, b))}, page.Items[0].FolderIDs)

	// The fixture's own work sits in three folders of both visibilities: every
	// one of them is the owner's, so every one of them is reported.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/holdings?work_ids="+idstr(fx.Work), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Len(t, page.Items, 1)
	require.GreaterOrEqual(t, len(page.Items[0].FolderIDs), 3)
	require.Contains(t, page.Items[0].FolderIDs, idstr(fx.FolderPublic))
	require.Contains(t, page.Items[0].FolderIDs, idstr(fx.Folder))
}

func TestLiveFolderHoldingsBatchBound(t *testing.T) {
	env := liveCatalog(t)

	ids := make([]string, 0, 101)
	for i := range 100 {
		ids = append(ids, strconv.Itoa(900000+i))
	}
	status, _, raw := liveDo(t, env, http.MethodGet,
		"/v2/me/folders/holdings?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 200, status, "100 is the bound, not one past it: %s", raw)

	ids = append(ids, "999999")
	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/me/folders/holdings?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeTooManyIDs, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/holdings", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
	require.NotEmpty(t, p.Errors)
	require.Equal(t, "work_ids", p.Errors[0].Parameter)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/folders/holdings?work_ids=abc", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeInvalidParameter, liveProblem(t, raw).Code)
}

// The whole reason this face exists: a notification has to reach the person who
// keeps the work in a folder nobody else can see. A holders list that only
// carried public folders would look healthy on every display face and silently
// drop most of its audience.
func TestLiveFolderHoldersIncludesPrivateFolders(t *testing.T) {
	env := liveCatalog(t)

	work := seedHoldableWork(t, env.db, "Holders Target")
	seedFolderHolding(t, env.db, 90001, work, model.FolderVisibilityPrivate, "Private Holder")
	seedFolderHolding(t, env.db, 90002, work, model.FolderVisibilityPublic, "Public Holder")
	// A second private folder for the same person: the lane answers accounts,
	// not memberships.
	seedFolderHolding(t, env.db, 90001, work, model.FolderVisibilityPrivate, "Private Holder II")

	status, ct, raw := liveDo(t, env, http.MethodGet,
		"/v2/folders/holders?work_id="+idstr(work), liveAppKeyHolder, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, ct, "json")

	var page holdersPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Equal(t, "list", page.Object)
	require.Nil(t, page.NextCursor)
	require.Len(t, page.Items, 2, "one row per account, owner_uid-ascending")
	require.Equal(t, "folder_holder", page.Items[0].Object)
	require.Equal(t, []string{"90001", "90002"},
		[]string{page.Items[0].OwnerUID, page.Items[1].OwnerUID})

	// Nothing but the uid: a folder id or a name here would turn the face into a
	// "who favourited what" index.
	var open struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &open))
	for _, item := range open.Items {
		require.ElementsMatch(t, []string{"object", "owner_uid"}, mapKeys(item))
	}

	// A work nobody holds is an empty list, not a 404.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/folders/holders?work_id=99887766", liveAppKeyHolder, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Empty(t, page.Items)
	require.Nil(t, page.NextCursor)
}

func TestLiveFolderHoldersPagesAndBoundsItsLimit(t *testing.T) {
	env := liveCatalog(t)

	work := seedHoldableWork(t, env.db, "Holders Paging")
	for _, uid := range []int64{90011, 90012, 90013} {
		seedFolderHolding(t, env.db, uid, work, model.FolderVisibilityPrivate, "Holder "+idstr(uid))
	}

	seen, cursor := []string{}, ""
	for range 5 {
		url := "/v2/folders/holders?work_id=" + idstr(work) + "&limit=1"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		status, _, raw := liveDo(t, env, http.MethodGet, url, liveAppKeyHolder, "")
		require.Equal(t, 200, status, string(raw))
		var page holdersPage
		require.NoError(t, json.Unmarshal(raw, &page), string(raw))
		for _, it := range page.Items {
			seen = append(seen, it.OwnerUID)
		}
		if page.NextCursor == nil {
			break
		}
		requireOpaqueCursor(t, "/v2/folders/holders", *page.NextCursor)
		cursor = *page.NextCursor
	}
	require.Equal(t, []string{"90011", "90012", "90013"}, seen)

	status, _, raw := liveDo(t, env, http.MethodGet,
		"/v2/folders/holders?work_id="+idstr(work)+"&limit=100", liveAppKeyHolder, "")
	require.Equal(t, 200, status, "100 is the bound, not one past it: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/folders/holders?work_id="+idstr(work)+"&limit=101", liveAppKeyHolder, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeLimitTooLarge, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/folders/holders", liveAppKeyHolder, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeInvalidParameter, liveProblem(t, raw).Code)
}

// The preview a moderator reads before the purge on the same path: it has to
// show what the DELETE would take, which includes the private folders.
func TestLiveModerationUserFoldersPreview(t *testing.T) {
	env := liveCatalog(t)

	work := seedHoldableWork(t, env.db, "Preview Target")
	priv := seedFolderHolding(t, env.db, 90021, work, model.FolderVisibilityPrivate, "Preview Private")
	pub := seedFolderHolding(t, env.db, 90021, work, model.FolderVisibilityPublic, "Preview Public")

	status, ct, raw := liveDo(t, env, http.MethodGet,
		"/v2/moderation/users/90021/folders?include_total=true", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, ct, "json")

	var page folderListPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(2), *page.Total)
	require.Equal(t, []string{idstr(priv), idstr(pub)},
		[]string{page.Items[0].ID, page.Items[1].ID}, "id-ascending, like /v2/me/folders")
	require.Equal(t, "private", page.Items[0].Visibility)
	require.Equal(t, "public", page.Items[1].Visibility)
	require.Equal(t, "90021", page.Items[0].OwnerUID)
	require.Equal(t, 1, page.Items[0].ItemCount)
	require.False(t, page.Items[0].IsDefault)
	require.NotEmpty(t, page.Items[0].CreatedAt)
	require.NotEmpty(t, page.Items[0].UpdatedAt)

	// Paginated the same way the owner's own lane is.
	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/moderation/users/90021/folders?limit=1", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	requireOpaqueCursor(t, "/v2/moderation/users/{uid}/folders", *page.NextCursor)
	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/moderation/users/90021/folders?limit=1&cursor="+*page.NextCursor, liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(pub), page.Items[0].ID)

	// An account holding none is 200 with an empty list, the same answer the
	// purge gives it.
	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/moderation/users/"+idstr(liveEmptyUID)+"/folders", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Empty(t, page.Items)

	// The person's own token without moderation standing gets nothing here.
	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/moderation/users/90021/folders", livePlainToken, "")
	require.Equal(t, 403, status, string(raw))
	require.Equal(t, problem.CodePermissionRequired, liveProblem(t, raw).Code)
}

func mapKeys(m map[string]any) []string {
	return slices.Collect(maps.Keys(m))
}
