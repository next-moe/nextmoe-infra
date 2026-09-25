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

func TestLiveCharacterListAdditions(t *testing.T) {
	env := liveCatalog(t)
	db := env.db
	empty := datatypes.JSON([]byte("{}"))

	trait := func(tid, name string, sexual bool) *model.CatalogCharacterTrait {
		t.Helper()
		row := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, GroupTID: "i66200", Alias: "", Description: "",
			Sexual: sexual, Searchable: true, Applicable: true,
		}
		require.NoError(t, db.Create(row).Error)
		return row
	}
	character := func(name string, gender *int16) *model.CatalogCharacter {
		t.Helper()
		row := &model.CatalogCharacter{DisplayName: name, Lang: "ja", Gender: gender, Extra: empty, FieldProvenance: empty}
		require.NoError(t, db.Create(row).Error)
		return row
	}
	work := func(name string, rating int16) *model.CatalogWork {
		t.Helper()
		row := &model.CatalogWork{
			MediumID: 1, OLang: "ja", DisplayName: name,
			ContentRating: rating, Status: model.WorkStatusLive,
			Extra: empty, FieldProvenance: empty,
		}
		require.NoError(t, db.Create(row).Error)
		return row
	}

	p := trait("i66201", "ListAdd P", false)
	c := trait("i66202", "ListAdd C", false)
	sr := trait("i66203", "ListAdd SR", true)
	x := trait("i66204", "ListAdd X", false)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitParent{TraitID: c.ID, ParentID: p.ID}).Error)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitParent{TraitID: x.ID, ParentID: sr.ID}).Error)

	fem, male := model.GenderFemale, model.GenderMale
	cf := character("ListAdd female", &fem)
	cm := character("ListAdd male", &male)
	cn := character("ListAdd none", nil)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitLink{CharacterID: cf.ID, TraitID: c.ID, SpoilerLevel: 0}).Error)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitLink{CharacterID: cm.ID, TraitID: p.ID, SpoilerLevel: 0}).Error)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitLink{CharacterID: cn.ID, TraitID: x.ID, SpoilerLevel: 0}).Error)

	sfw := work("ListAdd SFW", model.ContentRatingAllAges)
	r18 := work("ListAdd R18", model.ContentRatingR18)
	require.NoError(t, db.Create(&model.CatalogWorkCharacter{
		WorkID: sfw.ID, CharacterID: cf.ID, Kind: model.WorkCharacterKindMain, Spoiler: 0,
		MatchedBy: "test", FieldProvenance: empty,
	}).Error)
	require.NoError(t, db.Create(&model.CatalogWorkCharacter{
		WorkID: r18.ID, CharacterID: cf.ID, Kind: model.WorkCharacterKindMain, Spoiler: 0,
		MatchedBy: "test", FieldProvenance: empty,
	}).Error)

	traitIDs := []int64{p.ID, c.ID, sr.ID, x.ID}
	charIDs := []int64{cf.ID, cm.ID, cn.ID}
	workIDs := []int64{sfw.ID, r18.ID}
	t.Cleanup(func() {
		db.Where("character_id IN ?", charIDs).Delete(&model.CatalogCharacterTraitLink{})
		db.Where("character_id IN ?", charIDs).Delete(&model.CatalogWorkCharacter{})
		db.Where("trait_id IN ? OR parent_id IN ?", traitIDs, traitIDs).Delete(&model.CatalogCharacterTraitParent{})
		db.Unscoped().Where("id IN ?", charIDs).Delete(&model.CatalogCharacter{})
		db.Unscoped().Where("id IN ?", workIDs).Delete(&model.CatalogWork{})
		db.Where("id IN ?", traitIDs).Delete(&model.CatalogCharacterTrait{})
	})
	require.NoError(t, db.Exec(model.TraitSexualFamilySQL).Error)

	scope := "ids=" + idstr(cf.ID) + "," + idstr(cm.ID) + "," + idstr(cn.ID)
	get := func(qs string) (int, []byte) {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/characters?"+qs, liveAppKey, "")
		return status, body
	}
	type item struct {
		ID              string    `json:"id"`
		MatchedTraitIDs *[]string `json:"matched_trait_ids"`
		WorkCount       *int      `json:"work_count"`
	}
	type page struct {
		Items []item `json:"items"`
	}
	decode := func(body []byte) page {
		t.Helper()
		var p page
		require.NoError(t, json.Unmarshal(body, &p), string(body))
		return p
	}
	idsOf := func(p page) []string {
		out := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			out = append(out, it.ID)
		}
		return out
	}

	status, body := get(scope + "&gender=female")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(cf.ID)}, idsOf(decode(body)))

	status, body = get(scope + "&gender=female,male")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(cf.ID), idstr(cm.ID)}, idsOf(decode(body)))

	status, body = get(scope + "&gender=bogus")
	require.Equal(t, 400, status, string(body))
	pbm := liveProblem(t, body)
	require.Equal(t, problem.CodeUnknownEnumValue, pbm.Code)
	require.NotEmpty(t, pbm.Errors)
	require.Equal(t, "gender", pbm.Errors[0].Parameter)

	status, body = get(scope + "&trait_id=" + idstr(p.ID))
	require.Equal(t, 200, status, string(body))
	got := decode(body)
	require.ElementsMatch(t, []string{idstr(cf.ID), idstr(cm.ID)}, idsOf(got))
	byID := map[string]item{}
	for _, it := range got.Items {
		byID[it.ID] = it
	}
	require.NotNil(t, byID[idstr(cf.ID)].MatchedTraitIDs)
	require.Equal(t, []string{idstr(c.ID)}, *byID[idstr(cf.ID)].MatchedTraitIDs)
	require.NotNil(t, byID[idstr(cm.ID)].MatchedTraitIDs)
	require.Equal(t, []string{idstr(p.ID)}, *byID[idstr(cm.ID)].MatchedTraitIDs)

	status, body = get(scope)
	require.Equal(t, 200, status, string(body))
	for _, it := range decode(body).Items {
		require.Nil(t, it.MatchedTraitIDs, it.ID)
	}

	status, body = get("ids=" + idstr(cf.ID) + "&include=work_count")
	require.Equal(t, 200, status, string(body))
	got = decode(body)
	require.Len(t, got.Items, 1)
	require.NotNil(t, got.Items[0].WorkCount)
	sfwCount := *got.Items[0].WorkCount

	status, appBody := func() (int, []byte) {
		t.Helper()
		st, _, b := liveDo(t, env, http.MethodGet,
			"/v2/catalog/characters/"+idstr(cf.ID)+"/appearances?limit=100", liveAppKey, "")
		return st, b
	}()
	require.Equal(t, 200, status, string(appBody))
	var apps struct {
		Items []json.RawMessage `json:"items"`
		Total *int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(appBody, &apps), string(appBody))
	if apps.Total != nil {
		require.Equal(t, int(*apps.Total), sfwCount)
	} else {
		require.Equal(t, len(apps.Items), sfwCount)
	}

	status, body = get("ids=" + idstr(cf.ID) + "&include=work_count&nsfw=true")
	require.Equal(t, 200, status, string(body))
	got = decode(body)
	require.Len(t, got.Items, 1)
	require.NotNil(t, got.Items[0].WorkCount)
	nsfwCount := *got.Items[0].WorkCount
	require.Greater(t, nsfwCount, sfwCount)

	st, _, nsfwApp := liveDo(t, env, http.MethodGet,
		"/v2/catalog/characters/"+idstr(cf.ID)+"/appearances?limit=100&nsfw=true", liveAppKey, "")
	require.Equal(t, 200, st, string(nsfwApp))
	require.NoError(t, json.Unmarshal(nsfwApp, &apps), string(nsfwApp))
	if apps.Total != nil {
		require.Equal(t, int(*apps.Total), nsfwCount)
	} else {
		require.Equal(t, len(apps.Items), nsfwCount)
	}

	status, body = get(scope + "&trait_id=" + idstr(x.ID))
	require.Equal(t, 400, status, string(body))
	pbm = liveProblem(t, body)
	require.Equal(t, problem.CodeInvalidParameter, pbm.Code)
	require.NotEmpty(t, pbm.Errors)
	require.Equal(t, "trait_id", pbm.Errors[0].Parameter)
	require.Equal(t, problem.ReasonNotAllowedValue, pbm.Errors[0].Reason)
}
