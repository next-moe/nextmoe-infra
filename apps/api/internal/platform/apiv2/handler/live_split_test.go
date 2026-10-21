package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestLiveSplitRequestLifecycle(t *testing.T) {
	env := liveCatalog(t)
	var medium int16
	require.NoError(t, env.db.Raw(`SELECT medium_id FROM catalog_work WHERE id = ?`, env.fx.Work).Scan(&medium).Error)
	w := catmodel.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: fmt.Sprintf("拆分候補%d", time.Now().UnixNano()),
		Status: catmodel.WorkStatusLive, Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
	}
	require.NoError(t, env.db.Create(&w).Error)
	str := func(s string) *string { return &s }
	keep := catmodel.CatalogRelease{WorkID: w.ID, Title: str("本編"), Lang: str("ja"), Extra: datatypes.JSON(`{"vndb_id":"r900001"}`)}
	plus := catmodel.CatalogRelease{WorkID: w.ID, Title: str("本編+ ダウンロード版"), Lang: str("ja"), Extra: datatypes.JSON(`{"vndb_id":"r900002"}`)}
	require.NoError(t, env.db.Create(&keep).Error)
	require.NoError(t, env.db.Create(&plus).Error)
	var newWork int64
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_split_request WHERE entity_id = ?`, w.ID)
		env.db.Exec(`DELETE FROM catalog_merge_proposal WHERE source_entity_id = ? OR target_entity_id = ?`, newWork, w.ID)
		env.db.Exec(`DELETE FROM catalog_redirect WHERE old_id = ?`, newWork)
		env.db.Exec(`DELETE FROM catalog_match_candidate WHERE a_id IN (?, ?) OR b_id IN (?, ?)`, w.ID, newWork, w.ID, newWork)
		env.db.Exec(`DELETE FROM catalog_revision WHERE entity_type = ? AND entity_id IN (?, ?)`, catmodel.EntityTypeWork, w.ID, newWork)
		env.db.Exec(`DELETE FROM catalog_release WHERE work_id IN (?, ?)`, w.ID, newWork)
		env.db.Exec(`DELETE FROM catalog_work_title WHERE work_id IN (?, ?)`, w.ID, newWork)
		env.db.Unscoped().Delete(&catmodel.CatalogWork{}, []int64{w.ID, newWork})
	})

	status, _, raw := liveDo(t, env, http.MethodGet, fmt.Sprintf("/v2/me/split-preview?object=work&target_id=%d&release_ids=%d", w.ID, plus.ID), livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var preview struct {
		Releases []struct {
			ID        string `json:"id"`
			Selected  bool   `json:"selected"`
			Tombstone bool   `json:"tombstone"`
		} `json:"releases"`
		SuggestedName string `json:"suggested_name"`
		Blockers      []any  `json:"blockers"`
	}
	require.NoError(t, json.Unmarshal(raw, &preview))
	require.Len(t, preview.Releases, 2)
	require.Equal(t, "本編+", preview.SuggestedName)
	require.Empty(t, preview.Blockers)

	body := fmt.Sprintf(`{"object":"work","target_id":"%d","release_ids":["%d"],"display_name":"本編+","olang":"ja",
		"titles":[{"lang":"ja","title":"本編+","title_kind":"official"}],"note":"追加ルートの完全版"}`, w.ID, plus.ID)
	settings.Override(t, keys.CatalogSplitRequestsPerDay, int64(0))
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/split-requests", livePlainToken, body)
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))
	settings.Override(t, keys.CatalogSplitRequestsPerDay, int64(10))

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/split-requests", livePlainToken, body)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec struct {
		ID           string  `json:"id"`
		State        string  `json:"state"`
		DisplayName  string  `json:"display_name"`
		NewWorkID    *string `json:"new_work_id"`
		ExecuteAfter *string `json:"execute_after"`
		Moved        []struct {
			Key   string `json:"key"`
			Count int64  `json:"count"`
		} `json:"moved"`
	}
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "open", rec.State)
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/split-requests", livePlainToken, body)
	require.Equal(t, http.StatusConflict, status, "one pending split per work: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/moderation/split-requests", livePlainToken, "")
	require.Equal(t, http.StatusForbidden, status, "a plain user reads no split queue: %s", raw)
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/split-requests/"+rec.ID, livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))

	amend := fmt.Sprintf(`{"release_ids":["%d"],"display_name":"本編+ 完全版","olang":"ja","titles":[{"lang":"ja","title":"本編+","title_kind":"official"}]}`, plus.ID)
	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/moderation/split-requests/"+rec.ID, liveUserToken, amend)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "本編+ 完全版", rec.DisplayName)

	decide := func(decision, reason string) (int, []byte) {
		b := fmt.Sprintf(`{"decision":"%s","reason":"%s"}`, decision, reason)
		st, _, r := liveDo(t, env, http.MethodPost, "/v2/moderation/split-requests/"+rec.ID+"/decisions", liveUserToken, b)
		return st, r
	}
	status, raw = decide("approve", "")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "approved", rec.State)
	require.NotNil(t, rec.ExecuteAfter)
	status, raw = decide("execute", "")
	require.Equal(t, http.StatusConflict, status, "execute inside the cooling-off is refused: %s", raw)

	require.NoError(t, env.db.Exec(`UPDATE catalog_split_request SET execute_after = now() - interval '1 minute' WHERE id = ?`, rec.ID).Error)
	status, raw = decide("execute", "")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "executed", rec.State)
	require.NotNil(t, rec.NewWorkID)
	fmt.Sscan(*rec.NewWorkID, &newWork)
	moved := map[string]int64{}
	for _, m := range rec.Moved {
		moved[m.Key] = m.Count
	}
	require.Equal(t, int64(1), moved["releases"])
	require.Equal(t, int64(1), moved["tombstones"])

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/catalog/works/"+*rec.NewWorkID, liveAppKey, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/catalog/splits", liveAppKey, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	require.True(t, strings.Contains(string(raw), `"new_work_id":"`+*rec.NewWorkID+`"`), string(raw))

	status, raw = decide("undo", "")
	require.Equal(t, http.StatusUnprocessableEntity, status, "an undo needs a reason: %s", raw)
	status, raw = decide("undo", "VNDB 仍把它算作同一部")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "undone", rec.State)
	var back int64
	require.NoError(t, env.db.Raw(`SELECT work_id FROM catalog_release WHERE id = ?`, plus.ID).Scan(&back).Error)
	require.Equal(t, w.ID, back)
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/catalog/works/"+*rec.NewWorkID, liveAppKey, "")
	require.Equal(t, http.StatusNotFound, status, "the new id now redirects to the original: %s", raw)
	require.Contains(t, string(raw), "ENTITY_MERGED")
}
