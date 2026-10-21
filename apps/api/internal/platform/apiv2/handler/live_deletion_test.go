package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestLiveDeletionRequestLifecycle(t *testing.T) {
	env := liveCatalog(t)
	var medium int16
	require.NoError(t, env.db.Raw(`SELECT medium_id FROM catalog_work WHERE id = ?`, env.fx.Work).Scan(&medium).Error)
	w := catmodel.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: fmt.Sprintf("削除候補%d", time.Now().UnixNano()),
		Status: catmodel.WorkStatusLive, Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
	}
	require.NoError(t, env.db.Create(&w).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_deletion_request WHERE entity_id = ?`, w.ID)
		env.db.Exec(`DELETE FROM catalog_revision WHERE entity_type = ? AND entity_id = ?`, catmodel.EntityTypeWork, w.ID)
		env.db.Unscoped().Delete(&catmodel.CatalogWork{}, w.ID)
	})
	path := "/v2/catalog/works/" + idstr(w.ID)
	status, _, raw := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, http.StatusOK, status, string(raw))

	body := func(reason string) string {
		return fmt.Sprintf(`{"object":"work","target_id":"%d","reason":"%s","note":"no such game anywhere"}`, w.ID, reason)
	}
	settings.Override(t, keys.CatalogDeletionRequestsPerDay, int64(0))
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/deletion-requests", livePlainToken, body("not_a_work"))
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))
	settings.Override(t, keys.CatalogDeletionRequestsPerDay, int64(10))

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/deletion-requests", livePlainToken, body("duplicate"))
	require.Equal(t, http.StatusUnprocessableEntity, status, "a duplicate is merged, not deleted: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/deletion-requests", livePlainToken, body("not_a_work"))
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec struct {
		ID           string  `json:"id"`
		State        string  `json:"state"`
		ExecuteAfter *string `json:"execute_after"`
	}
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "open", rec.State)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/deletion-requests", livePlainToken, body("spam_or_test"))
	require.Equal(t, http.StatusConflict, status, "one pending request per entry: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/moderation/deletion-requests", livePlainToken, "")
	require.Equal(t, http.StatusForbidden, status, "a plain user reads no deletion queue: %s", raw)

	decide := func(decision, reason string) (int, []byte) {
		b := fmt.Sprintf(`{"decision":"%s","reason":"%s"}`, decision, reason)
		st, _, r := liveDo(t, env, http.MethodPost, "/v2/moderation/deletion-requests/"+rec.ID+"/decisions", liveUserToken, b)
		return st, r
	}
	status, raw = decide("approve", "")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "approved", rec.State)
	require.NotNil(t, rec.ExecuteAfter, "approval starts the cooling-off")

	status, raw = decide("execute", "")
	require.Equal(t, http.StatusConflict, status, "execute inside the cooling-off is refused: %s", raw)

	require.NoError(t, env.db.Exec(`UPDATE catalog_deletion_request SET execute_after = now() - interval '1 minute' WHERE id = ?`, rec.ID).Error)
	status, raw = decide("execute", "")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "executed", rec.State)

	status, _, raw = liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, http.StatusGone, status, string(raw))
	var gone struct {
		Code          string `json:"code"`
		Object        string `json:"object"`
		DeletedReason string `json:"deleted_reason"`
		DeletedAt     string `json:"deleted_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &gone))
	require.Equal(t, "ENTITY_DELETED", gone.Code)
	require.Equal(t, "work", gone.Object)
	require.Equal(t, "not_a_work", gone.DeletedReason)
	require.NotEmpty(t, gone.DeletedAt)

	status, _, raw = liveDo(t, env, http.MethodGet, path+"/releases", liveAppKey, "")
	require.Equal(t, http.StatusGone, status, "sub-collections answer like the work itself: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/deletion-preview?object=work&target_id="+idstr(w.ID), livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var preview struct {
		Deleted bool `json:"deleted"`
	}
	require.NoError(t, json.Unmarshal(raw, &preview))
	require.True(t, preview.Deleted)

	status, raw = decide("restore", "the game exists after all")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "restored", rec.State)

	status, _, raw = liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, http.StatusOK, status, "the work is back under the same id: %s", raw)
}
