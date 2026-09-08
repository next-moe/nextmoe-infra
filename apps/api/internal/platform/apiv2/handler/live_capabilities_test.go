package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"

	"github.com/stretchr/testify/require"
)

type capsView struct {
	Object       string  `json:"object"`
	TargetObject string  `json:"target_object"`
	EntityType   string  `json:"entity_type"`
	EntityID     *string `json:"entity_id"`
	Fields       []struct {
		Key            string `json:"key"`
		Locked         bool   `json:"locked"`
		CanPropose     bool   `json:"can_propose"`
		CanReview      bool   `json:"can_review"`
		WouldAutomerge bool   `json:"would_automerge"`
	} `json:"fields"`
}

func liveCaps(t *testing.T, env *liveEnv, path, token string) capsView {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodGet, path, token, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var v capsView
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	require.NotEmpty(t, v.Fields)
	return v
}

func capsFor(t *testing.T, v capsView, key string) (canPropose, canReview, wouldAutomerge bool) {
	t.Helper()
	for _, f := range v.Fields {
		if f.Key == key {
			return f.CanPropose, f.CanReview, f.WouldAutomerge
		}
	}
	t.Fatalf("%s missing from the capability set", key)
	return
}

// The face exists so a client stops guessing. Before it, the forum hardcoded
// can_review = true for everyone and showed review affordances to users the
// server answers 403 to.
func TestLiveCapabilitiesSeparateTheActors(t *testing.T) {
	env := liveCatalog(t)
	const path = "/v2/me/edit-capabilities/work"
	const field = "catalog.work.titles"

	admin := liveCaps(t, env, path, liveUserToken)
	propose, review, automerge := capsFor(t, admin, field)
	require.True(t, propose, "kungal opens propose to any authenticated user")
	require.True(t, review, "an admin holds the work review permission")
	require.True(t, automerge, "work automerges for whoever could approve it")

	plain := liveCaps(t, env, path, livePlainToken)
	propose, review, automerge = capsFor(t, plain, field)
	require.True(t, propose, "a plain user may still propose on kungal")
	require.False(t, review, "a plain user holds no review permission")
	require.False(t, automerge, "so nothing they file skips the queue")

	// The moderation cap is a property of the face, not of the person: the same
	// admin through a developer-owned app reaches no verdict. This is the answer
	// the forum could never obtain, and misreading it as "capped for everyone"
	// is what froze the pipeline for eleven days.
	thirdParty := liveCaps(t, env, path, liveThirdPartyToken)
	propose, review, automerge = capsFor(t, thirdParty, field)
	require.True(t, propose, "a third-party app may still file proposals")
	require.False(t, review, "but reaches no verdict, whatever roles the user carries")
	require.False(t, automerge)
}

func TestLiveCapabilitiesShapeAndErrors(t *testing.T) {
	env := liveCatalog(t)

	v := liveCaps(t, env, "/v2/me/edit-capabilities/work", liveUserToken)
	require.Equal(t, "edit_capabilities", v.Object)
	require.Equal(t, "work", v.TargetObject)
	require.Equal(t, "catalog.work", v.EntityType)
	require.Nil(t, v.EntityID, "entity_id is null when the answer is type-level")

	// Every family the public schema face serves must answer here too, or the
	// two faces cannot be joined.
	for _, object := range []string{"work", "company", "character", "release", "tag", "engine", "series"} {
		liveCaps(t, env, "/v2/me/edit-capabilities/"+object, liveUserToken)
	}

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/nosuchfamily", liveUserToken, "")
	require.Equal(t, http.StatusNotFound, status, string(raw))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, raw).Code)

	// It is a /v2/me face: no credential is 401, never an anonymous answer.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work", "", "")
	require.Equal(t, http.StatusUnauthorized, status, string(raw))
	require.Equal(t, problem.CodeMissingCredential, liveProblem(t, raw).Code)
}
