package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
)

type liveQueueItem struct {
	ID             string    `json:"id"`
	ChangedFields  *[]string `json:"changed_fields"`
	ConflictFields *[]string `json:"conflict_fields"`
}

func liveQueue(t *testing.T, env *liveEnv, token, query string) ([]liveQueueItem, string) {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/moderation/proposals?"+query, token, "")
	require.Equal(t, http.StatusOK, status, "%s: %s", query, body)
	var page struct {
		Items      []liveQueueItem `json:"items"`
		NextCursor *string         `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	return page.Items, next
}

func liveQueueIDs(items []liveQueueItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func liveMergePatch(t *testing.T, env *liveEnv, workID int64, patch map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"entity_type": editspec.TypeWork, "entity_id": idstr(workID), "patch": patch})
	require.NoError(t, err)
	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/proposals", liveCentralToken, string(raw))
	require.Equal(t, http.StatusCreated, status, string(body))
	var prop liveProposalBody
	require.NoError(t, json.Unmarshal(body, &prop))
	require.Equal(t, "merged", prop.State, "a moderator's work edit applies at once")
}

func liveRevisionIDs(t *testing.T, env *liveEnv, workID int64) []int64 {
	t.Helper()
	var ids []int64
	require.NoError(t, env.db.Raw(`SELECT id FROM edit_revision WHERE entity_type = ? AND entity_id = ? ORDER BY seq`,
		editspec.TypeWork, workID).Scan(&ids).Error)
	return ids
}

func TestLiveModerationQueueFiltersSortsAndInlinesPatches(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Queue Gap Probe", 30561)
	stale := liveOpenProposal(t, env, work, "Queue Gap Renamed")
	langProp, rev, err := env.cat.Engine.CreateProposal(context.Background(), editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work,
		Patch: map[string]any{editspec.FieldWorkOLang: "en"},
		Actor: editing.PolicyContext{UserID: liveSecondPlainUID, Site: liveSite},
	})
	require.NoError(t, err)
	require.Nil(t, rev)
	lang := idstr(langProp.ID)
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Queue Gap Landed"})

	scope := "object=work&entity_id=" + idstr(work)
	items, _ := liveQueue(t, env, liveCentralToken, scope+"&include=patch")
	byID := map[string]liveQueueItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	require.Len(t, byID, 2)
	require.Equal(t, []string{editspec.FieldWorkDisplayName}, *byID[stale.ID].ChangedFields)
	require.Equal(t, []string{editspec.FieldWorkDisplayName}, *byID[stale.ID].ConflictFields,
		"the merged rename landed after the proposal's base revision")
	require.Empty(t, *byID[lang].ConflictFields)

	plain, _ := liveQueue(t, env, liveCentralToken, scope)
	for _, it := range plain {
		require.Nil(t, it.ChangedFields, "without include=patch the block is absent")
	}

	conflicted, _ := liveQueue(t, env, liveCentralToken, scope+"&has_conflict=true")
	require.Equal(t, []string{stale.ID}, liveQueueIDs(conflicted))
	byField, _ := liveQueue(t, env, liveCentralToken, scope+"&field="+url.QueryEscape(editspec.FieldWorkOLang))
	require.Equal(t, []string{lang}, liveQueueIDs(byField))

	first, next := liveQueue(t, env, liveCentralToken, scope+"&sort=filed_asc&limit=1")
	require.Equal(t, []string{stale.ID}, liveQueueIDs(first), "oldest waiting first")
	require.NotEmpty(t, next)
	second, _ := liveQueue(t, env, liveCentralToken, scope+"&sort=filed_asc&limit=1&cursor="+url.QueryEscape(next))
	require.Equal(t, []string{lang}, liveQueueIDs(second))

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals?"+scope+"&sort=updated_desc&cursor="+url.QueryEscape(next), liveCentralToken, "")
	require.Equal(t, http.StatusBadRequest, status, "a cursor from another sort is refused: %s", body)
	require.Equal(t, "INVALID_CURSOR", liveProblemCode(t, body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/proposals?"+scope+"&site="+liveOtherSite, liveUserToken, "")
	require.Equal(t, http.StatusForbidden, status, "a downstream site cannot widen its queue: %s", body)
	require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body))
	other, _ := liveQueue(t, env, liveCentralToken, scope+"&site="+liveOtherSite)
	require.Empty(t, other, "both proposals were filed on kungal")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/proposals?field=Not-A-Key", liveCentralToken, "")
	require.Equal(t, http.StatusBadRequest, status, string(body))
	require.Equal(t, "INVALID_PARAMETER", liveProblemCode(t, body))
}

func TestLiveMyProposalsSortByUpdateAndFilterSince(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Mine Sort Probe", 30562)
	older := liveOpenProposal(t, env, work, "Mine Sort One")
	newer := liveOpenProposal(t, env, work, "Mine Sort Two")
	require.NoError(t, env.db.Exec(`UPDATE edit_proposal SET updated_at = '2026-01-01T00:00:00Z' WHERE id = ?`, newer.ID).Error)
	require.NoError(t, env.db.Exec(`UPDATE edit_proposal SET updated_at = '2026-02-01T00:00:00Z' WHERE id = ?`, older.ID).Error)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/me/proposals?object=work&entity_id="+idstr(work)+"&sort=updated_desc", livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var page struct {
		Items []liveProposalBody `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Equal(t, []string{older.ID, newer.ID}, []string{page.Items[0].ID, page.Items[1].ID})

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/me/proposals?object=work&entity_id="+idstr(work)+"&updated_since=2026-01-15T00:00:00Z", livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	page.Items = nil
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, older.ID, page.Items[0].ID)
}

func TestLiveRevertUndoModeAndBaseline(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Undo Probe Original", 30563)
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Undo One"})
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Undo Two"})
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkOLang: "en"})
	revs := liveRevisionIDs(t, env, work)
	require.Len(t, revs, 3)

	revert := func(body string) (int, []byte) {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodPost, "/v2/moderation/reverts", liveCentralToken, body)
		return status, raw
	}

	status, raw := revert(fmt.Sprintf(`{"revision_id":%q,"mode":"undo"}`, idstr(revs[1])))
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.Equal(t, "Undo One", liveWorkName(t, env, work), "undo puts back only the name revision 2 changed")
	var olang string
	require.NoError(t, env.db.Raw(`SELECT olang FROM catalog_work WHERE id = ?`, work).Scan(&olang).Error)
	require.Equal(t, "en", olang, "revision 3's language change stays")

	status, raw = revert(fmt.Sprintf(`{"revision_id":%q,"mode":"undo"}`, idstr(revs[0])))
	require.Equal(t, http.StatusConflict, status, "later revisions changed the name: %s", raw)
	p := liveProblem(t, raw)
	require.Equal(t, "REVERT_CONFLICT", p.Code)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "/fields/"+editspec.FieldWorkDisplayName, p.Errors[0].Pointer)
	status, raw = revert(fmt.Sprintf(`{"revision_id":%q,"mode":"undo","force":true}`, idstr(revs[0])))
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.Equal(t, "Undo Probe Original", liveWorkName(t, env, work), "undoing the first edit puts back the baseline")

	status, raw = revert(fmt.Sprintf(`{"revision_id":%q,"mode":"undo","baseline":true}`, idstr(revs[0])))
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	status, raw = revert(fmt.Sprintf(`{"revision_id":%q,"force":true}`, idstr(revs[0])))
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))

	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Undo Three"})
	status, raw = revert(fmt.Sprintf(`{"revision_id":%q,"baseline":true}`, idstr(revs[2])))
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.Equal(t, "Undo Probe Original", liveWorkName(t, env, work))
	require.NoError(t, env.db.Raw(`SELECT olang FROM catalog_work WHERE id = ?`, work).Scan(&olang).Error)
	require.Equal(t, "ja", olang, "restoring the baseline restores every field")
}

func TestLiveRevisionsPublishTheBaseline(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Baseline Face Original", 30564)
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Baseline Face One"})
	liveMergePatch(t, env, work, map[string]any{editspec.FieldWorkDisplayName: "Baseline Face Two"})
	revs := liveRevisionIDs(t, env, work)

	type face struct {
		ID           string `json:"id"`
		HasBaseline  *bool  `json:"has_baseline"`
		DiffFromBase *bool  `json:"diff_from_baseline"`
		DiffBase     *string
		Diff         []struct {
			Key  string `json:"key"`
			From any    `json:"from"`
			To   any    `json:"to"`
		} `json:"diff"`
	}
	get := func(path string) face {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, path, "", "")
		require.Equal(t, http.StatusOK, status, string(raw))
		var f face
		require.NoError(t, json.Unmarshal(raw, &f), string(raw))
		return f
	}

	first := get("/v2/catalog/revisions/" + idstr(revs[0]) + "?include=diff")
	require.NotNil(t, first.HasBaseline)
	require.True(t, *first.HasBaseline)
	require.NotNil(t, first.DiffFromBase, "a first revision with a baseline diffs against it")
	require.Len(t, first.Diff, 1)
	require.Equal(t, "Baseline Face Original", first.Diff[0].From)

	second := get("/v2/catalog/revisions/" + idstr(revs[1]) + "?include=diff&diff_base=baseline")
	require.Nil(t, second.HasBaseline, "only the first revision carries the flag")
	require.NotNil(t, second.DiffFromBase)
	require.Equal(t, "Baseline Face Original", second.Diff[0].From)
	require.Equal(t, "Baseline Face Two", second.Diff[0].To)

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/catalog/revisions?object=work&entity_id="+idstr(work)+"&sort=recorded_asc", "", "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var page struct {
		Items []face `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.Items[0].HasBaseline)
	require.Nil(t, page.Items[1].HasBaseline)

	require.NoError(t, env.db.Exec(`UPDATE edit_revision SET baseline_snapshot = NULL WHERE id = ?`, revs[0]).Error)
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/catalog/revisions/"+idstr(revs[1])+"?include=diff&diff_base=baseline", "", "")
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
}
