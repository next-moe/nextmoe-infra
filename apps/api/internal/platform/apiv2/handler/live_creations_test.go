package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/apiv2/problem"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
)

func TestLiveCreationGatesDuplicatesAndQueues(t *testing.T) {
	env := liveCatalog(t)
	name := fmt.Sprintf("ライブ新会社%d", time.Now().UnixNano())
	existing := catmodel.CatalogLabel{DisplayName: name}
	require.NoError(t, env.db.Create(&existing).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM edit_proposal WHERE patch->>'catalog.label.name' = ?`, name)
		env.db.Unscoped().Delete(&catmodel.CatalogLabel{}, existing.ID)
	})
	body := fmt.Sprintf(`{"object":"company","patch":{"catalog.label.name":%q}}`, name)

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/creations", livePlainToken, body)
	require.Equal(t, http.StatusConflict, status, string(raw))
	var prob struct {
		Code     string `json:"code"`
		Suspects []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"suspects"`
	}
	require.NoError(t, json.Unmarshal(raw, &prob))
	require.Equal(t, problem.CodeDuplicateSuspects, prob.Code)
	require.NotEmpty(t, prob.Suspects)
	require.Equal(t, name, prob.Suspects[0].DisplayName)

	body = fmt.Sprintf(`{"object":"company","patch":{"catalog.label.name":%q},"confirm_duplicates":true}`, name)
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/creations", livePlainToken, body)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec struct {
		State    string `json:"state"`
		EntityID string `json:"entity_id"`
		Creation *struct {
			Params map[string]any `json:"params"`
		} `json:"creation"`
	}
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "open", rec.State, "a company birth queues for review")
	require.Equal(t, "0", rec.EntityID)
	require.NotNil(t, rec.Creation)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/creations", livePlainToken, `{"object":"work","patch":{"catalog.work.display_name":"x"}}`)
	require.Equal(t, http.StatusUnprocessableEntity, status, "work is not in the creatable enum: %s", raw)
}

func TestLiveCapabilitiesAnnounceCreation(t *testing.T) {
	env := liveCatalog(t)
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/release", livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var v struct {
		Creation *struct {
			CanCreate      bool `json:"can_create"`
			WouldAutomerge bool `json:"would_automerge"`
			ParamSpecs     []struct {
				Key      string `json:"key"`
				Required bool   `json:"required"`
			} `json:"param_specs"`
			RequiredFields []string `json:"required_fields"`
		} `json:"creation"`
	}
	require.NoError(t, json.Unmarshal(raw, &v))
	require.NotNil(t, v.Creation)
	require.True(t, v.Creation.CanCreate)
	require.False(t, v.Creation.WouldAutomerge, "a plain user's release queues")
	require.Len(t, v.Creation.ParamSpecs, 1)
	require.Equal(t, "work_id", v.Creation.ParamSpecs[0].Key)
	require.Contains(t, v.Creation.RequiredFields, "catalog.release.kind")

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work", livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var w struct {
		Creation *json.RawMessage `json:"creation"`
	}
	require.NoError(t, json.Unmarshal(raw, &w))
	require.Nil(t, w.Creation, "works are minted through claims, not the engine")
}

func TestLiveMergeRequestQuotaAndModeration(t *testing.T) {
	env := liveCatalog(t)
	a := catmodel.CatalogLabel{DisplayName: fmt.Sprintf("合併元%d", time.Now().UnixNano())}
	b := catmodel.CatalogLabel{DisplayName: fmt.Sprintf("合併先%d", time.Now().UnixNano())}
	require.NoError(t, env.db.Create(&a).Error)
	require.NoError(t, env.db.Create(&b).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_merge_proposal WHERE source_entity_id = ? AND target_entity_id = ?`, a.ID, b.ID)
		env.db.Unscoped().Delete(&catmodel.CatalogLabel{}, []int64{a.ID, b.ID})
	})
	body := fmt.Sprintf(`{"object":"company","source_id":"%d","target_id":"%d","reason":"same brand"}`, a.ID, b.ID)

	settings.Override(t, keys.CatalogMergeRequestsPerDay, int64(0))
	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/merge-requests", livePlainToken, body)
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))

	settings.Override(t, keys.CatalogMergeRequestsPerDay, int64(10))
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/merge-requests", livePlainToken, body)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "open", rec.State)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/moderation/merge-requests", livePlainToken, "")
	require.Equal(t, http.StatusForbidden, status, "a plain user reads no merge queue: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/moderation/merge-requests/"+rec.ID+"/decisions", liveUserToken, `{"decision":"approve"}`)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var decided struct {
		State        string  `json:"state"`
		ExecuteAfter *string `json:"execute_after"`
	}
	require.NoError(t, json.Unmarshal(raw, &decided))
	require.Equal(t, "approved", decided.State)
	require.NotNil(t, decided.ExecuteAfter, "approval starts the cooling-off")

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/moderation/merge-requests/"+rec.ID+"/decisions", liveUserToken, `{"decision":"execute"}`)
	require.Equal(t, http.StatusConflict, status, "execute inside the cooling-off is refused: %s", raw)
}
