package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editorigin"

	"github.com/stretchr/testify/require"
)

type liveOriginRecord struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	OriginSite *string `json:"origin_site"`
}

func liveProposeFrom(t *testing.T, env *liveEnv, token, declared string) liveOriginRecord {
	t.Helper()
	body := fmt.Sprintf(`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q}}`,
		editspec.TypeWork, idstr(env.fx.Work), editspec.FieldWorkDisplayName,
		fmt.Sprintf("origin probe %d", time.Now().UnixNano()))
	headers := map[string]string{}
	if declared != "" {
		headers[editorigin.Header] = declared
	}
	status, _, raw := liveDoHeader(t, env, http.MethodPost, "/v2/me/proposals", token, body, headers)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec liveOriginRecord
	require.NoError(t, json.Unmarshal(raw, &rec), string(raw))
	return rec
}

func liveOriginList(t *testing.T, env *liveEnv, path, token string) []liveOriginRecord {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodGet, path, token, "")
	require.Equal(t, http.StatusOK, status, "%s: %s", path, raw)
	var page struct {
		Items []liveOriginRecord `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	return page.Items
}

func originOf(r liveOriginRecord) string {
	if r.OriginSite == nil {
		return ""
	}
	return *r.OriginSite
}

func TestLiveOriginSiteAttribution(t *testing.T) {
	env := liveCatalog(t)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM edit_revision WHERE proposal_id IN (SELECT id FROM edit_proposal WHERE patch::text LIKE '%origin probe%')`)
		env.db.Exec(`DELETE FROM edit_proposal WHERE patch::text LIKE '%origin probe%'`)
	})

	cases := []struct {
		name     string
		token    string
		declared string
		want     string
	}{
		{"a site token is its own property", liveUserToken, "", editorigin.Kungal},
		{"a site token cannot claim another property", liveUserToken, editorigin.Letmoe, editorigin.Kungal},
		{"moyu shares the forum's catalog site yet keeps its own origin", liveOtherSiteToken, editorigin.Kungal, editorigin.Moyu},
		{"the editor carries the site its user came from", liveCentralToken, editorigin.Letmoe, editorigin.Letmoe},
		{"the editor drops a value off the list", liveCentralToken, "galgame_wiki", editorigin.Editor},
		{"the editor opened directly is the editor", liveCentralToken, "", editorigin.Editor},
	}
	ids := map[string]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := liveProposeFrom(t, env, c.token, c.declared)
			require.Equal(t, c.want, originOf(rec))
			ids[rec.ID] = c.want

			var stored *string
			require.NoError(t, env.db.Raw(`SELECT origin_site FROM edit_proposal WHERE id = ?`, rec.ID).Scan(&stored).Error)
			require.NotNil(t, stored)
			require.Equal(t, c.want, *stored)

			if rec.State == "merged" {
				var revOrigin *string
				require.NoError(t, env.db.Raw(`SELECT origin_site FROM edit_revision WHERE proposal_id = ?`, rec.ID).Scan(&revOrigin).Error)
				require.NotNil(t, revOrigin, "an automerged revision carries its proposal's origin")
				require.Equal(t, c.want, *revOrigin)
			}
		})
	}

	t.Run("revision and proposal faces filter on origin_site", func(t *testing.T) {
		path := fmt.Sprintf("/v2/catalog/revisions?object=work&entity_id=%d&origin_site=letmoe&limit=100", env.fx.Work)
		revs := liveOriginList(t, env, path, liveAppKey)
		require.NotEmpty(t, revs)
		for _, r := range revs {
			require.Equal(t, editorigin.Letmoe, originOf(r))
		}

		props := liveOriginList(t, env, "/v2/catalog/proposals?origin_site=moyu&limit=100", liveAppKey)
		require.NotEmpty(t, props)
		for _, p := range props {
			require.Equal(t, editorigin.Moyu, originOf(p))
		}

		mine := liveOriginList(t, env, "/v2/me/proposals?origin_site=editor&limit=100", liveCentralToken)
		require.NotEmpty(t, mine)
		for _, p := range mine {
			require.Equal(t, editorigin.Editor, originOf(p))
		}
	})

	t.Run("an unknown origin_site is 400, not an empty page", func(t *testing.T) {
		for _, path := range []string{
			"/v2/catalog/revisions?origin_site=galgame_wiki",
			"/v2/catalog/proposals?origin_site=galgame_wiki",
		} {
			status, _, raw := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
			require.Equal(t, http.StatusBadRequest, status, "%s: %s", path, raw)
			require.Equal(t, "UNKNOWN_ENUM_VALUE", liveProblemCode(t, raw))
		}
	})
}

func TestLiveMergeRequestOriginSite(t *testing.T) {
	env := liveCatalog(t)
	a := catmodel.CatalogLabel{DisplayName: fmt.Sprintf("来源甲%d", time.Now().UnixNano())}
	b := catmodel.CatalogLabel{DisplayName: fmt.Sprintf("来源乙%d", time.Now().UnixNano())}
	require.NoError(t, env.db.Create(&a).Error)
	require.NoError(t, env.db.Create(&b).Error)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM catalog_merge_proposal WHERE source_entity_id = ? AND target_entity_id = ?`, a.ID, b.ID)
		env.db.Unscoped().Delete(&catmodel.CatalogLabel{}, []int64{a.ID, b.ID})
	})
	body := fmt.Sprintf(`{"object":"company","source_id":"%d","target_id":"%d","reason":"same brand"}`, a.ID, b.ID)
	status, _, raw := liveDoHeader(t, env, http.MethodPost, "/v2/me/merge-requests", liveCentralToken, body,
		map[string]string{editorigin.Header: editorigin.Kungal})
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rec liveOriginRecord
	require.NoError(t, json.Unmarshal(raw, &rec), string(raw))
	require.Equal(t, editorigin.Kungal, originOf(rec))

	mine := liveOriginList(t, env, "/v2/me/merge-requests?origin_site=kungal", liveCentralToken)
	require.NotEmpty(t, mine)
	found := false
	for _, m := range mine {
		require.Equal(t, editorigin.Kungal, originOf(m))
		found = found || m.ID == rec.ID
	}
	require.True(t, found, "the filtered list keeps the request just filed")
}
