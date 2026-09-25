package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type liveCharListPage struct {
	Items []struct {
		ID     string           `json:"id"`
		Traits *json.RawMessage `json:"traits"`
	} `json:"items"`
	NextCursor *string   `json:"next_cursor"`
	Total      *int64    `json:"total"`
	Missing    *[]string `json:"missing"`
}

func TestLiveCharacterTraitFilter(t *testing.T) {
	env := liveCatalog(t)
	db := env.db
	empty := datatypes.JSON([]byte("{}"))

	trait := func(tid, name string, sexual bool) *model.CatalogCharacterTrait {
		t.Helper()
		row := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, GroupTID: "i77000", Alias: "", Description: "", Sexual: sexual, SexualFamily: sexual,
		}
		require.NoError(t, db.Create(row).Error)
		return row
	}
	character := func(name string) *model.CatalogCharacter {
		t.Helper()
		row := &model.CatalogCharacter{DisplayName: name, Lang: "ja", Extra: empty, FieldProvenance: empty}
		require.NoError(t, db.Create(row).Error)
		return row
	}
	parent := func(child, par int64) {
		t.Helper()
		require.NoError(t, db.Create(&model.CatalogCharacterTraitParent{TraitID: child, ParentID: par}).Error)
	}
	link := func(characterID, traitID int64, spoiler int16) {
		t.Helper()
		require.NoError(t, db.Create(&model.CatalogCharacterTraitLink{
			CharacterID: characterID, TraitID: traitID, SpoilerLevel: spoiler,
		}).Error)
	}

	p := trait("i77001", "TraitFilter P", false)
	c := trait("i77002", "TraitFilter C", false)
	g := trait("i77003", "TraitFilter G", false)
	q := trait("i77004", "TraitFilter Q", false)
	s := trait("i77005", "TraitFilter S", true)
	p2 := trait("i77006", "TraitFilter P2", false)
	s2 := trait("i77007", "TraitFilter S2", true)
	parent(c.ID, p.ID)
	parent(g.ID, c.ID)
	parent(s2.ID, p2.ID)

	c1 := character("TraitFilter c1")
	c2 := character("TraitFilter c2")
	c3 := character("TraitFilter c3")
	c4 := character("TraitFilter c4")
	c5 := character("TraitFilter c5")
	c6 := character("TraitFilter c6")
	c7 := character("TraitFilter c7")
	link(c1.ID, g.ID, 0)
	link(c2.ID, p.ID, 0)
	link(c2.ID, q.ID, 0)
	link(c3.ID, q.ID, 0)
	link(c4.ID, p.ID, 1)
	link(c5.ID, s.ID, 0)
	link(c6.ID, g.ID, 0)
	link(c7.ID, s2.ID, 0)
	require.NoError(t, db.Delete(c6).Error)

	traitIDs := []int64{p.ID, c.ID, g.ID, q.ID, s.ID, p2.ID, s2.ID}
	charIDs := []int64{c1.ID, c2.ID, c3.ID, c4.ID, c5.ID, c6.ID, c7.ID}
	t.Cleanup(func() {
		db.Where("character_id IN ?", charIDs).Delete(&model.CatalogCharacterTraitLink{})
		db.Where("trait_id IN ? OR parent_id IN ?", traitIDs, traitIDs).Delete(&model.CatalogCharacterTraitParent{})
		db.Unscoped().Where("id IN ?", charIDs).Delete(&model.CatalogCharacter{})
		db.Where("id IN ?", traitIDs).Delete(&model.CatalogCharacterTrait{})
	})

	allow := map[string]bool{
		idstr(c1.ID): true, idstr(c2.ID): true, idstr(c3.ID): true,
		idstr(c4.ID): true, idstr(c5.ID): true, idstr(c6.ID): true, idstr(c7.ID): true,
	}
	get := func(qs string) (int, []byte, liveCharListPage) {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/characters?"+qs, liveAppKey, "")
		var page liveCharListPage
		if status == 200 {
			require.NoError(t, json.Unmarshal(body, &page), string(body))
		}
		return status, body, page
	}
	idsOf := func(page liveCharListPage) []string {
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}
	keys := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &raw), string(body))
		return raw
	}

	status, body, page := get("trait_id=" + idstr(p.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID), idstr(c2.ID)}, idsOf(page))

	status, body, page = get("trait_id=" + idstr(c.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID)}, idsOf(page))

	status, body, page = get("trait_id=" + idstr(g.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID)}, idsOf(page))

	status, body, page = get("trait_id=" + idstr(p.ID) + "," + idstr(q.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c2.ID)}, idsOf(page))

	status, body, page = get("trait_id=" + idstr(p.ID) + "," + idstr(q.ID) + "&trait_match=any")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID), idstr(c2.ID), idstr(c3.ID)}, idsOf(page))

	status, body, _ = get("trait_id=" + idstr(s.ID))
	require.Equal(t, 400, status, string(body))
	pbm := liveProblem(t, body)
	require.Equal(t, problem.CodeInvalidParameter, pbm.Code)
	require.NotEmpty(t, pbm.Errors)
	require.Equal(t, "trait_id", pbm.Errors[0].Parameter)
	require.Equal(t, problem.ReasonNotAllowedValue, pbm.Errors[0].Reason)

	status, body, page = get("trait_id=" + idstr(s.ID) + "&nsfw=true")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c5.ID)}, idsOf(page))

	status, body, page = get("trait_id=" + idstr(p2.ID))
	require.Equal(t, 200, status, string(body))
	require.Empty(t, idsOf(page))

	status, body, page = get("trait_id=" + idstr(p2.ID) + "&nsfw=true")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c7.ID)}, idsOf(page))

	missingID := int64(987654321)
	status, body, page = get("trait_id=" + idstr(missingID))
	require.Equal(t, 200, status, string(body))
	require.Empty(t, page.Items)
	_, hasCursor := keys(body)["next_cursor"]
	require.False(t, hasCursor, string(body))

	_, _, traitsRaw := liveDo(t, env, http.MethodGet, "/v2/catalog/traits?limit=1", liveAppKey, "")
	var traitsPage struct {
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(traitsRaw, &traitsPage), string(traitsRaw))
	require.NotEmpty(t, traitsPage.NextCursor)
	status, body, _ = get("trait_id=" + idstr(missingID) + "&cursor=" + traitsPage.NextCursor)
	require.Equal(t, 400, status, "a cursor from another lane is refused even when the filter matches nothing: %s", body)
	require.Equal(t, problem.CodeInvalidCursor, liveProblem(t, body).Code)

	status, body, page = get("trait_id=" + idstr(p.ID) + "," + idstr(missingID) + "&trait_match=any")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID), idstr(c2.ID)}, idsOf(page))

	status, body, page = get("ids=" + idstr(c1.ID) + "," + idstr(c3.ID) + "&trait_id=" + idstr(p.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID)}, idsOf(page))
	require.NotNil(t, page.Missing)
	require.Contains(t, *page.Missing, idstr(c3.ID))

	status, body, page = get("trait_id=" + idstr(p.ID) + "&include_total=true")
	require.Equal(t, 200, status, string(body))
	require.NotNil(t, page.Total)
	require.Equal(t, int64(2), *page.Total)

	status, body, page = get("trait_id=" + idstr(p.ID))
	require.Equal(t, 200, status, string(body))
	_, hasTotal := keys(body)["total"]
	require.False(t, hasTotal, string(body))

	status, body, page = get("trait_id=" + idstr(p.ID) + "&limit=1")
	require.Equal(t, 200, status, string(body))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	first := page.Items[0].ID
	status, body, page = get("trait_id=" + idstr(p.ID) + "&limit=1&cursor=" + *page.NextCursor)
	require.Equal(t, 200, status, string(body))
	require.Len(t, page.Items, 1)
	require.NotEqual(t, first, page.Items[0].ID)
	_, hasCursor = keys(body)["next_cursor"]
	require.False(t, hasCursor, string(body))
	require.ElementsMatch(t, []string{idstr(c1.ID), idstr(c2.ID)}, []string{first, page.Items[0].ID})

	status, body, page = get("trait_id=" + idstr(p.ID) + "&include=traits")
	require.Equal(t, 200, status, string(body))
	require.NotEmpty(t, idsOf(page))
	for _, it := range page.Items {
		if allow[it.ID] {
			require.NotNil(t, it.Traits, it.ID)
		}
	}
}
