package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type myWorksPage struct {
	Object string `json:"object"`
	Items  []struct {
		Object    string   `json:"object"`
		WorkID    string   `json:"work_id"`
		FolderIDs []string `json:"folder_ids"`
		Playtime  *struct {
			Object  string `json:"object"`
			WorkID  string `json:"work_id"`
			Minutes int    `json:"minutes"`
		} `json:"playtime"`
		WorkState *struct {
			Object     string  `json:"object"`
			WorkID     string  `json:"work_id"`
			State      string  `json:"state"`
			Completion *string `json:"completion"`
		} `json:"work_state"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      *int64  `json:"total"`
}

func seedPlaytime(t *testing.T, db *gorm.DB, uid, workID int64, clientID string, minutes int) {
	t.Helper()
	row := &model.CatalogUserPlaytime{ActorUID: uid, WorkID: workID, ClientID: clientID, Minutes: minutes}
	require.NoError(t, db.Create(row).Error)
	t.Cleanup(func() { db.Delete(row) })
}

func seedWorkState(t *testing.T, db *gorm.DB, uid, workID int64, state int16, completion *int16) {
	t.Helper()
	row := &model.CatalogUserWorkState{ActorUID: uid, WorkID: workID, State: state, Completion: completion}
	require.NoError(t, db.Create(row).Error)
	t.Cleanup(func() { db.Delete(row) })
}

func TestLiveMyWorksAnswersEveryAskedWork(t *testing.T) {
	env := liveCatalog(t)

	full := seedHoldableWork(t, env.db, "My Works Full")
	a := seedFolderHolding(t, env.db, liveUID, full, model.FolderVisibilityPrivate, "My Works A")
	b := seedFolderHolding(t, env.db, liveUID, full, model.FolderVisibilityPublic, "My Works B")
	seedPlaytime(t, env.db, liveUID, full, "desktop", 30)
	seedPlaytime(t, env.db, liveUID, full, "phone", 90)
	main := model.WorkCompletionMain
	seedWorkState(t, env.db, liveUID, full, model.WorkStateDone, &main)

	stateOnly := seedHoldableWork(t, env.db, "My Works State Only")
	seedWorkState(t, env.db, liveUID, stateOnly, model.WorkStateWish, nil)

	strangers := seedHoldableWork(t, env.db, "My Works Stranger")
	seedFolderHolding(t, env.db, livePlainUID, strangers, model.FolderVisibilityPublic, "Stranger's")
	seedPlaytime(t, env.db, livePlainUID, strangers, "desktop", 45)
	seedWorkState(t, env.db, livePlainUID, strangers, model.WorkStateDoing, nil)

	asked := []string{idstr(stateOnly), idstr(full), "99887766", idstr(strangers), idstr(full)}
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(asked, ","), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))

	var page myWorksPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Equal(t, "list", page.Object)
	require.Nil(t, page.NextCursor, "this lane does not paginate")
	require.Len(t, page.Items, 4, "one item per distinct work, the repeated id answered once")
	var order []string
	for _, it := range page.Items {
		require.Equal(t, "user_work", it.Object)
		order = append(order, it.WorkID)
	}
	require.Equal(t, []string{idstr(stateOnly), idstr(full), "99887766", idstr(strangers)}, order)

	st := page.Items[0]
	require.Empty(t, st.FolderIDs)
	require.NotNil(t, st.FolderIDs, "folder_ids is an empty array, never null")
	require.Nil(t, st.Playtime)
	require.NotNil(t, st.WorkState)
	require.Equal(t, "wish", st.WorkState.State)
	require.Nil(t, st.WorkState.Completion)

	f := page.Items[1]
	require.Equal(t, []string{idstr(min(a, b)), idstr(max(a, b))}, f.FolderIDs)
	require.NotNil(t, f.Playtime)
	require.Equal(t, 90, f.Playtime.Minutes, "the most minutes any client reported, as /v2/me/playtimes answers")
	require.Equal(t, idstr(full), f.Playtime.WorkID)
	require.NotNil(t, f.WorkState)
	require.Equal(t, "done", f.WorkState.State)
	require.Equal(t, "main", *f.WorkState.Completion)

	for _, it := range page.Items[2:] {
		require.Empty(t, it.FolderIDs, "%s: another account's rows are never the bearer's", it.WorkID)
		require.Nil(t, it.Playtime, it.WorkID)
		require.Nil(t, it.WorkState, it.WorkID)
	}

	// The batch lanes this face stands in for answer the same values.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/playtimes?work_ids="+idstr(full), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, string(raw), `"minutes":90`)

	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/me/work-states?work_ids="+idstr(full)+",99887766,"+idstr(stateOnly), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var states struct {
		Items []struct {
			WorkID string `json:"work_id"`
			State  string `json:"state"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(raw, &states), string(raw))
	require.Len(t, states.Items, 2)
	require.Equal(t, idstr(full), states.Items[0].WorkID, "request order")
	require.Equal(t, "done", states.Items[0].State)
	require.Equal(t, idstr(stateOnly), states.Items[1].WorkID)
	require.Equal(t, []string{"99887766"}, states.Missing)
}

func TestLiveMyWorksBatchBound(t *testing.T) {
	env := liveCatalog(t)

	ids := make([]string, 0, 101)
	for i := range 100 {
		ids = append(ids, strconv.Itoa(900000+i))
	}
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 200, status, "100 is the bound, not one past it: %s", raw)

	ids = append(ids, "999999")
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeTooManyIDs, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids=abc", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeInvalidParameter, liveProblem(t, raw).Code)
}

func TestLiveMyWorksWalksEveryRecordedWork(t *testing.T) {
	env := liveCatalog(t)

	folderOnly := seedHoldableWork(t, env.db, "Walk Folder Only")
	shelf := seedFolderHolding(t, env.db, liveWalkerUID, folderOnly, model.FolderVisibilityPrivate, "Walk Shelf")
	playOnly := seedHoldableWork(t, env.db, "Walk Playtime Only")
	seedPlaytime(t, env.db, liveWalkerUID, playOnly, "desktop", 12)
	stateOnly := seedHoldableWork(t, env.db, "Walk State Only")
	seedWorkState(t, env.db, liveWalkerUID, stateOnly, model.WorkStateWish, nil)
	everything := seedHoldableWork(t, env.db, "Walk Everything")
	both := seedFolderHolding(t, env.db, liveWalkerUID, everything, model.FolderVisibilityPublic, "Walk Both")
	seedPlaytime(t, env.db, liveWalkerUID, everything, "phone", 75)
	seedWorkState(t, env.db, liveWalkerUID, everything, model.WorkStateDone, nil)

	strangers := seedHoldableWork(t, env.db, "Walk Stranger")
	seedFolderHolding(t, env.db, livePlainUID, strangers, model.FolderVisibilityPublic, "Stranger's Walk")
	seedPlaytime(t, env.db, livePlainUID, strangers, "desktop", 45)
	seedWorkState(t, env.db, livePlainUID, strangers, model.WorkStateDoing, nil)

	// A membership is the folder owner's, whatever the item row's copy says.
	planted := seedHoldableWork(t, env.db, "Walk Planted")
	theirs := seedFolderHolding(t, env.db, livePlainUID, planted, model.FolderVisibilityPublic, "Planted In")
	require.NoError(t, env.db.Model(&model.CatalogUserFolderItem{}).
		Where("folder_id = ?", theirs).Update("owner_uid", liveWalkerUID).Error)

	want := []int64{folderOnly, playOnly, stateOnly, everything}
	slices.Sort(want)

	get := func(url string) myWorksPage {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, url, liveWalkerToken, "")
		require.Equal(t, 200, status, string(raw))
		var page myWorksPage
		require.NoError(t, json.Unmarshal(raw, &page), string(raw))
		return page
	}

	whole := get("/v2/me/works?limit=100&include_total=true")
	require.Nil(t, whole.NextCursor)
	require.NotNil(t, whole.Total)
	require.Equal(t, int64(len(want)), *whole.Total)
	var got []string
	for _, it := range whole.Items {
		got = append(got, it.WorkID)
	}
	var wantIDs []string
	for _, id := range want {
		wantIDs = append(wantIDs, idstr(id))
	}
	require.Equal(t, wantIDs, got, "every recorded work once, ascending; nobody else's")

	byID := map[string]int{}
	for i, it := range whole.Items {
		byID[it.WorkID] = i
	}
	e := whole.Items[byID[idstr(everything)]]
	require.Equal(t, []string{idstr(both)}, e.FolderIDs)
	require.NotNil(t, e.Playtime)
	require.Equal(t, 75, e.Playtime.Minutes)
	require.NotNil(t, e.WorkState)
	require.Equal(t, "done", e.WorkState.State)
	f := whole.Items[byID[idstr(folderOnly)]]
	require.Equal(t, []string{idstr(shelf)}, f.FolderIDs)
	require.Nil(t, f.Playtime)
	require.Nil(t, f.WorkState)
	p := whole.Items[byID[idstr(playOnly)]]
	require.NotNil(t, p.FolderIDs)
	require.Empty(t, p.FolderIDs)
	require.Equal(t, 12, p.Playtime.Minutes)
	require.Nil(t, p.WorkState)
	s := whole.Items[byID[idstr(stateOnly)]]
	require.Empty(t, s.FolderIDs)
	require.Nil(t, s.Playtime)
	require.Equal(t, "wish", s.WorkState.State)

	batch := get("/v2/me/works?work_ids=" + strings.Join(wantIDs, ","))
	require.Equal(t, whole.Items, batch.Items, "the walk answers what the batch lane answers")

	var walked []string
	url := "/v2/me/works?limit=1"
	for pages := 0; ; pages++ {
		require.Less(t, pages, len(want)+1, "the walk must end")
		page := get(url)
		require.LessOrEqual(t, len(page.Items), 1)
		for _, it := range page.Items {
			walked = append(walked, it.WorkID)
		}
		if page.NextCursor == nil {
			break
		}
		url = "/v2/me/works?limit=1&cursor=" + *page.NextCursor
	}
	require.Equal(t, wantIDs, walked, "limit=1 pages add up to the whole walk, no repeat and no gap")

	first := get("/v2/me/works?limit=2&include_total=true")
	require.Len(t, first.Items, 2)
	require.NotNil(t, first.NextCursor)
	require.Equal(t, int64(len(want)), *first.Total, "total counts the collection, not the page")

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/works?cursor="+collect.EncodeCursor("not-a-work"), liveWalkerToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeInvalidCursor, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?ids="+idstr(everything), liveWalkerToken, "")
	require.Equal(t, 400, status, "work_ids= is this face's batch lane, not ids=: %s", raw)
}
