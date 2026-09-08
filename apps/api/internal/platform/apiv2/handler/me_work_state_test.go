package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/stretchr/testify/require"
)

func TestLiveWorkStateLifecycle(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/work-states/" + idstr(fx.Work)

	status, _, body := liveDo(t, env, http.MethodPut, path, liveUserToken,
		`{"state":"doing","completion":"one_route"}`)
	require.Equal(t, 200, status, string(body))
	var rec repr.UserWorkState
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "work_state", rec.Object)
	require.Equal(t, idstr(fx.Work), rec.WorkID)
	require.Equal(t, "doing", rec.State)
	require.NotNil(t, rec.Completion)
	require.Equal(t, "one_route", *rec.Completion)

	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "doing", rec.State)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/work-states", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var page repr.List[repr.UserWorkState]
	require.NoError(t, json.Unmarshal(body, &page))
	require.NotEmpty(t, page.Items)

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/work-states", liveUserToken,
		`{"items":[{"work_id":"`+idstr(fx.Work)+`","state":"done"},{"work_id":"`+idstr(fx.Work)+`","state":"wish","completion":"main"}]}`)
	require.Equal(t, 207, status, string(body))
	var batch repr.List[repr.WorkStateBatchItem]
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Len(t, batch.Items, 2)
	require.Equal(t, 200, batch.Items[0].Status)
	require.NotNil(t, batch.Items[0].State)
	require.Equal(t, "done", *batch.Items[0].State)
	require.Equal(t, 422, batch.Items[1].Status)
	require.NotNil(t, batch.Items[1].Problem)
	require.Equal(t, problem.CodeValidationFailed, batch.Items[1].Problem.Code)

	status, _, _ = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status)

	status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 404, status, string(body))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, body).Code)
}
