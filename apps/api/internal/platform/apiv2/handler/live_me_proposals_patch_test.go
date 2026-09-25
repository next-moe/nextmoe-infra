package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"

	"github.com/stretchr/testify/require"
)

type proposalPatchPair struct {
	ID             string          `json:"id"`
	Patch          json.RawMessage `json:"patch"`
	EffectivePatch json.RawMessage `json:"effective_patch"`
}

func TestLiveMyProposalListCarriesPatches(t *testing.T) {
	env := liveCatalog(t)
	amendedWork := liveDraftClaim(t, env, "List Patch Amended", 30401)
	plainWork := liveDraftClaim(t, env, "List Patch Plain", 30402)
	amended := liveOpenProposal(t, env, amendedWork, "List Patch Proposed")
	plain := liveOpenProposal(t, env, plainWork, "List Patch Untouched")

	detailPath := "/v2/me/proposals/" + amended.ID
	status, _, body := liveDoHeader(t, env, http.MethodPost, detailPath+"/amendments", liveUserToken,
		`{"set":{"catalog.work.display_name":"List Patch Amended Twice"},"note":"fold"}`,
		map[string]string{"If-Match": liveETag(t, env, detailPath, livePlainToken)})
	require.Equal(t, 201, status, string(body))

	detail := func(id string) proposalPatchPair {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/proposals/"+id+"?include=patch", livePlainToken, "")
		require.Equal(t, 200, status, string(raw))
		var p proposalPatchPair
		require.NoError(t, json.Unmarshal(raw, &p), string(raw))
		return p
	}
	list := func(query string) map[string]proposalPatchPair {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/proposals?limit=100"+query, livePlainToken, "")
		require.Equal(t, 200, status, string(raw))
		var page struct {
			Items []proposalPatchPair `json:"items"`
		}
		require.NoError(t, json.Unmarshal(raw, &page), string(raw))
		out := map[string]proposalPatchPair{}
		for _, it := range page.Items {
			out[it.ID] = it
		}
		return out
	}

	for _, query := range []string{"&include=patch", "&view=full"} {
		rows := list(query)
		require.Contains(t, rows, amended.ID, query)
		require.Contains(t, rows, plain.ID, query)
		for _, id := range []string{amended.ID, plain.ID} {
			want := detail(id)
			require.JSONEq(t, string(want.Patch), string(rows[id].Patch), "%s %s patch", query, id)
			require.JSONEq(t, string(want.EffectivePatch), string(rows[id].EffectivePatch), "%s %s effective_patch", query, id)
		}
		require.JSONEq(t, `{"catalog.work.display_name":"List Patch Proposed"}`, string(rows[amended.ID].Patch))
		require.JSONEq(t, `{"catalog.work.display_name":"List Patch Amended Twice"}`, string(rows[amended.ID].EffectivePatch),
			"the amendment is folded in")
		require.JSONEq(t, string(rows[plain.ID].Patch), string(rows[plain.ID].EffectivePatch))
	}

	for id, row := range list("") {
		require.Nil(t, row.Patch, id)
		require.Nil(t, row.EffectivePatch, id)
	}

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/proposals?include=amendments", livePlainToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeUnknownInclude, liveProblem(t, raw).Code)
}
