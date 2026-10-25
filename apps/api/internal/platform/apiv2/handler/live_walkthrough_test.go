package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestLiveWalkthroughFaces(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	get := func(path string) (int, []byte) {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		return status, body
	}

	status, body := get("/v2/catalog/walkthroughs/" + idstr(fx.Walkthrough))
	require.Equal(t, http.StatusOK, status, string(body))
	var w struct {
		Object           string `json:"object"`
		WorkID           string `json:"work_id"`
		WorkContentLimit string `json:"work_content_limit"`
		WorkRouteHints   bool   `json:"work_route_hints"`
		Source           string `json:"source"`
		Lang             string `json:"lang"`
		Characters       []struct {
			Key         string  `json:"key"`
			CharacterID *string `json:"character_id"`
		} `json:"characters"`
		EndingCount int `json:"ending_count"`
		Chart       *struct {
			Endings []struct {
				Name      string                    `json:"name"`
				Localized map[string]map[string]any `json:"localized"`
				Keys      []string                  `json:"character_keys"`
			} `json:"endings"`
			Segments []struct {
				FromNode struct {
					NodeType string  `json:"node_type"`
					Key      *string `json:"key"`
				} `json:"from_node"`
				Steps []struct {
					StepType     string                    `json:"step_type"`
					Text         *string                   `json:"text"`
					Localized    map[string]map[string]any `json:"localized"`
					Alternatives []struct {
						Text      string         `json:"text"`
						Localized map[string]any `json:"localized"`
					} `json:"alternatives"`
					SaveKey *string `json:"save_key"`
				} `json:"steps"`
			} `json:"segments"`
		} `json:"chart"`
	}
	require.NoError(t, json.Unmarshal(body, &w))
	assert.Equal(t, "walkthrough", w.Object)
	assert.Equal(t, idstr(fx.Work), w.WorkID)
	assert.Equal(t, model.DisplayLimitKeySFW, w.WorkContentLimit)
	assert.False(t, w.WorkRouteHints)
	assert.Equal(t, "seiya", w.Source)
	assert.Equal(t, "ja", w.Lang)
	assert.Equal(t, 1, w.EndingCount)
	require.Len(t, w.Characters, 1)
	require.NotNil(t, w.Characters[0].CharacterID)
	assert.Equal(t, idstr(fx.Character), *w.Characters[0].CharacterID)
	require.NotNil(t, w.Chart)
	require.Len(t, w.Chart.Endings, 1)
	assert.Equal(t, "直播END", w.Chart.Endings[0].Localized["zh-Hans"]["value"])
	assert.Equal(t, true, w.Chart.Endings[0].Localized["zh-Hans"]["is_machine"])
	assert.Equal(t, []string{"c1"}, w.Chart.Endings[0].Keys)
	steps := w.Chart.Segments[0].Steps
	require.Len(t, steps, 2)
	assert.Equal(t, "はい", *steps[0].Text)
	assert.Equal(t, "是", steps[0].Localized["zh-Hans"]["value"])
	require.Len(t, steps[0].Alternatives, 1)
	assert.Empty(t, steps[0].Alternatives[0].Localized, "an untranslated alternative has an empty localized map")
	assert.Nil(t, steps[1].Text)
	assert.Equal(t, "sv01", *steps[1].SaveKey)

	require.NoError(t, env.db.Exec(`UPDATE catalog_work SET has_route_hints = true WHERE id = ?`, fx.Work).Error)
	t.Cleanup(func() { env.db.Exec(`UPDATE catalog_work SET has_route_hints = false WHERE id = ?`, fx.Work) })
	status, body = get("/v2/catalog/walkthroughs/" + idstr(fx.Walkthrough))
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"work_route_hints":true`)

	limitOf := map[string]string{}
	list := func(qs string) []string {
		t.Helper()
		status, body := get("/v2/catalog/walkthroughs?" + qs)
		require.Equal(t, http.StatusOK, status, string(body))
		var page struct {
			Items []struct {
				ID               string          `json:"id"`
				WorkContentLimit string          `json:"work_content_limit"`
				Chart            json.RawMessage `json:"chart"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		var ids []string
		for _, it := range page.Items {
			assert.Nil(t, it.Chart, "the collection carries no chart")
			ids = append(ids, it.ID)
			limitOf[it.ID] = it.WorkContentLimit
		}
		return ids
	}
	assert.Equal(t, []string{idstr(fx.Walkthrough)}, list("work_id="+idstr(fx.Work)))
	assert.Equal(t, []string{idstr(fx.Walkthrough)}, list("character_id="+idstr(fx.Character)))
	assert.Empty(t, list("character_id="+idstr(fx.Character+100000)))

	empty := datatypes.JSON([]byte("{}"))
	r18 := &model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "Walkthrough R18",
		ContentRating: model.ContentRatingR18, Status: model.WorkStatusLive, Extra: empty, FieldProvenance: empty}
	require.NoError(t, env.db.Create(r18).Error)
	hidden := &model.CatalogWalkthrough{WorkID: r18.ID, SourceID: model.SourceSeiya, SourceURL: "https://seiya-saiga.com/game/r18.html",
		ChartVersion: 1, SourceHash: "r18", ExtractModel: "test",
		Chart: datatypes.JSON(`{"characters":[],"sections":[],"entries":[],"saves":[],"endings":[],"segments":[]}`)}
	require.NoError(t, env.db.Create(hidden).Error)
	t.Cleanup(func() {
		env.db.Delete(&model.CatalogWalkthrough{}, hidden.ID)
		env.db.Unscoped().Delete(&model.CatalogWork{}, r18.ID)
	})
	status, _ = get("/v2/catalog/walkthroughs/" + idstr(hidden.ID))
	assert.Equal(t, http.StatusNotFound, status, "an r18 work's walkthrough is hidden without nsfw=true")
	status, _ = get("/v2/catalog/walkthroughs/" + idstr(hidden.ID) + "?nsfw=true")
	assert.Equal(t, http.StatusOK, status)
	assert.Empty(t, list("work_id="+idstr(r18.ID)))
	assert.Equal(t, []string{idstr(hidden.ID)}, list("work_id="+idstr(r18.ID)+"&nsfw=true"))
	assert.Equal(t, model.DisplayLimitKeyNSFW, limitOf[idstr(hidden.ID)], "an unjudged unclaimed r18 work falls back to its rating")
	assert.Empty(t, list("work_id="+idstr(r18.ID)+"&nsfw=true&content_limit=sfw"))
	assert.Equal(t, []string{idstr(hidden.ID)}, list("work_id="+idstr(r18.ID)+"&nsfw=true&content_limit=nsfw"))

	require.NoError(t, env.db.Exec(`UPDATE catalog_work SET title_nsfw = false WHERE id = ?`, r18.ID).Error)
	assert.Equal(t, []string{idstr(hidden.ID)}, list("work_id="+idstr(r18.ID)+"&nsfw=true&content_limit=sfw"),
		"an r18 work on the sfw shelf passes content_limit=sfw: the display axis is not the age axis")
	assert.Equal(t, model.DisplayLimitKeySFW, limitOf[idstr(hidden.ID)])
	assert.Empty(t, list("work_id="+idstr(r18.ID)+"&content_limit=sfw"), "content_limit does not open the age gate")
	status, body = get("/v2/catalog/walkthroughs/" + idstr(hidden.ID) + "?nsfw=true")
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"work_content_limit":"sfw"`)

	status, body = get("/v2/catalog/walkthroughs?content_limit=adult")
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	incomplete := &model.CatalogWalkthrough{WorkID: r18.ID, SourceID: model.SourceSeiya, SourceURL: "https://seiya-saiga.com/game/half.html",
		ChartVersion: 1, SourceHash: "half", ExtractModel: "test", Status: model.WalkthroughStatusHidden,
		Chart:        datatypes.JSON(`{"characters":[],"sections":[],"entries":[],"saves":[],"endings":[],"segments":[]}`),
		ImportReview: datatypes.JSON(`{"missed":["○楽園幻想"],"end_markers":6,"endings_reached":5,"coverage":0.83}`)}
	require.NoError(t, env.db.Create(incomplete).Error)
	t.Cleanup(func() { env.db.Delete(&model.CatalogWalkthrough{}, incomplete.ID) })
	assert.NotContains(t, list("work_id="+idstr(r18.ID)+"&nsfw=true"), idstr(incomplete.ID), "a backlog walkthrough is hidden from the public faces")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/walkthrough-backlog?work_id="+idstr(r18.ID), liveUserToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var backlog struct {
		Items []struct {
			ID           string `json:"id"`
			ImportReview *struct {
				Missed         []string `json:"missed"`
				EndingsReached int      `json:"endings_reached"`
			} `json:"import_review"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &backlog))
	require.Len(t, backlog.Items, 1, "the r18 work's backlog needs no nsfw=true")
	assert.Equal(t, idstr(incomplete.ID), backlog.Items[0].ID)
	require.NotNil(t, backlog.Items[0].ImportReview)
	assert.Equal(t, []string{"○楽園幻想"}, backlog.Items[0].ImportReview.Missed)

	status, _, _ = liveDo(t, env, http.MethodGet, "/v2/me/walkthrough-backlog", liveAppKey, "")
	assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, status, "the backlog is an editing face")
}
