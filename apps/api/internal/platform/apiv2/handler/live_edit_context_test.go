package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var liveUsersOfCalls atomic.Int64

func liveUsersOf(_ context.Context, ids []int64) (map[int64]repr.UserRef, error) {
	liveUsersOfCalls.Add(1)
	out := make(map[int64]repr.UserRef, len(ids))
	for _, id := range ids {
		name := fmt.Sprintf("已注销#%d", id)
		if id == liveUID || id == livePlainUID {
			name = fmt.Sprintf("user%d", id)
		}
		out[id] = repr.UserRef{Object: "user", ID: repr.ID(id), Name: name}
	}
	return out, nil
}

func TestLiveEditContextFace(t *testing.T) {
	env := liveCatalog(t)
	path := "/v2/me/edit-context/work/" + idstr(env.fx.Work)
	status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
	require.Equal(t, 200, status, string(body))

	var ec struct {
		Object   string `json:"object"`
		EntityID string `json:"entity_id"`
		Fields   []struct {
			Key        string            `json:"key"`
			Value      any               `json:"value"`
			Provenance []json.RawMessage `json:"provenance"`
			Rows       *[]struct {
				Source *struct {
					Key     string `json:"key"`
					Display string `json:"display"`
				} `json:"source"`
				Identity  *string `json:"identity"`
				Present   bool    `json:"present"`
				HumanLane bool    `json:"human_lane"`
			} `json:"rows"`
		} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(body, &ec), string(body))
	assert.Equal(t, "edit_context", ec.Object)
	assert.Equal(t, idstr(env.fx.Work), ec.EntityID)

	byKey := map[string]int{}
	for i, f := range ec.Fields {
		byKey[f.Key] = i
	}
	spec, _ := env.cat.EditTypes.Type(editspec.TypeWork)
	require.Len(t, ec.Fields, len(spec.Fields), "every registered field, companions included")

	olang := ec.Fields[byKey[editspec.FieldWorkOLang]]
	assert.NotNil(t, olang.Provenance, "a scalar field always answers an array")
	assert.Nil(t, olang.Rows)

	credits := ec.Fields[byKey[editspec.FieldWorkCredits]]
	require.NotNil(t, credits.Rows, "credits are stored per source")
	for _, r := range *credits.Rows {
		assert.True(t, r.Present)
		require.NotNil(t, r.Identity, "a suppressible field keys every row")
	}
	assert.Nil(t, ec.Fields[byKey[editspec.FieldWorkCredits+".suppressed"]].Rows)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/edit-context/work/999999999", livePlainToken, "")
	assert.Equal(t, 404, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/edit-context/nonsense/1", livePlainToken, "")
	assert.Equal(t, 404, status, string(body))
}

type livePerson struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

func TestLiveEditRecordsCarryPeople(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "People Target", 30501)
	prop := liveOpenProposal(t, env, work, "People Renamed")

	type record struct {
		ID          string      `json:"id"`
		ProposerUID string      `json:"proposer_uid"`
		Proposer    *livePerson `json:"proposer"`
		DecidedBy   *livePerson `json:"decided_by"`
		ActorUID    string      `json:"actor_uid"`
		Actor       *livePerson `json:"actor"`
	}
	one := func(path, token string) record {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, path, token, "")
		require.Equal(t, 200, status, string(raw))
		var r record
		require.NoError(t, json.Unmarshal(raw, &r), string(raw))
		return r
	}
	page := func(path, token string) []record {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, path, token, "")
		require.Equal(t, 200, status, string(raw))
		var p struct {
			Items []record `json:"items"`
		}
		require.NoError(t, json.Unmarshal(raw, &p), string(raw))
		return p.Items
	}

	for _, c := range []struct{ path, token string }{
		{"/v2/me/proposals/" + prop.ID, livePlainToken},
		{"/v2/moderation/proposals/" + prop.ID, liveUserToken},
		{"/v2/catalog/proposals/" + prop.ID, liveAppKey},
	} {
		r := one(c.path, c.token)
		require.NotNil(t, r.Proposer, c.path)
		assert.Equal(t, livePerson{Object: "user", ID: idstr(livePlainUID), Name: fmt.Sprintf("user%d", livePlainUID)}, *r.Proposer, c.path)
		assert.Nil(t, r.DecidedBy, "%s: an open proposal has no decider", c.path)
	}

	liveOpenProposal(t, env, liveDraftClaim(t, env, "People Second", 30502), "People Second Renamed")
	before := liveUsersOfCalls.Load()
	items := page("/v2/moderation/proposals?limit=50", liveUserToken)
	assert.Equal(t, before+1, liveUsersOfCalls.Load(), "one account lookup per response, however many records")
	require.GreaterOrEqual(t, len(items), 2)
	for _, it := range items {
		require.NotNil(t, it.Proposer, it.ID)
		assert.Equal(t, it.ProposerUID, it.Proposer.ID)
	}

	detailPath := "/v2/me/proposals/" + prop.ID
	status, _, raw := liveDoHeader(t, env, http.MethodPost, detailPath+"/amendments", liveUserToken,
		`{"set":{"catalog.work.display_name":"People Amended"},"note":"tidy"}`,
		map[string]string{"If-Match": liveETag(t, env, detailPath, livePlainToken)})
	require.Equal(t, 201, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, detailPath+"?include=amendments", livePlainToken, "")
	require.Equal(t, 200, status, string(raw))
	var amended struct {
		Amendments []struct {
			AmenderUID string      `json:"amender_uid"`
			Amender    *livePerson `json:"amender"`
		} `json:"amendments"`
	}
	require.NoError(t, json.Unmarshal(raw, &amended), string(raw))
	require.Len(t, amended.Amendments, 1)
	require.NotNil(t, amended.Amendments[0].Amender)
	assert.Equal(t, idstr(liveUID), amended.Amendments[0].Amender.ID)

	status, _, raw = liveDoHeader(t, env, http.MethodPost, "/v2/moderation/proposals/"+prop.ID+"/decisions", liveUserToken,
		`{"decision":"merge","note":"ok"}`,
		map[string]string{"If-Match": liveETag(t, env, "/v2/moderation/proposals/"+prop.ID, liveUserToken)})
	require.Equal(t, 201, status, string(raw))
	merged := one("/v2/catalog/proposals/"+prop.ID, liveAppKey)
	require.NotNil(t, merged.DecidedBy)
	assert.Equal(t, idstr(liveUID), merged.DecidedBy.ID)

	revs := page("/v2/catalog/revisions?object=work&entity_id="+idstr(work), liveAppKey)
	require.NotEmpty(t, revs)
	for _, r := range revs {
		require.NotNil(t, r.Actor, r.ID)
		assert.Equal(t, r.ActorUID, r.Actor.ID)
	}
}
