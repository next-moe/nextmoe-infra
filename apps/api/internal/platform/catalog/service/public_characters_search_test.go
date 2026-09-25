package service

import (
	"context"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/chardocs"
	"api/internal/testsupport/dbtest"

	"gorm.io/datatypes"
)

func charactersSearchIndexer(t *testing.T) *catsearch.Indexer {
	t.Helper()
	client := dbtest.OpenSearchClient(t, worksSearchTestPrefix)
	idx := catsearch.NewOpenSearchIndexer(client)
	ctx := context.Background()
	if err := idx.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Do(context.Background(), http.MethodDelete, "/"+client.IndexName(catsearch.IndexCharacters), nil, nil)
	})
	if err := idx.RecreateIndex(ctx, catsearch.IndexCharacters); err != nil {
		t.Fatalf("recreate characters index: %v", err)
	}
	return idx
}

func waitCharactersIndexed(t *testing.T, idx *catsearch.Indexer, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		n, err := idx.Count(t.Context(), catsearch.IndexCharacters)
		if err == nil && int(n) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("index did not reach %d docs (last err %v)", want, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type charParity struct {
	svc                             *PublicService
	idx                             *catsearch.Indexer
	p, q, a, s                      int64
	chain, pq, onlyQ, spoil         int64
	sex, femaleG, male, multi, dead int64
	popHi, popMid, popLo, popR18    int64
}

func seedCharacterParity(t *testing.T) charParity {
	t.Helper()
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE catalog_character_trait_parent RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	empty := datatypes.JSON([]byte("{}"))
	idx := charactersSearchIndexer(t)
	svc := newPublicSvc().WithWorksSearch(idx)

	trait := func(tid, name string, sexual bool) *model.CatalogCharacterTrait {
		t.Helper()
		row := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, GroupTID: "i90000", Alias: "", Description: "",
			Sexual: sexual, Searchable: true, Applicable: true,
		}
		if err := testDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	parent := func(child, par int64) {
		t.Helper()
		if err := testDB.Create(&model.CatalogCharacterTraitParent{TraitID: child, ParentID: par}).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := trait("i90001", "P", false)
	c := trait("i90002", "C", false)
	g := trait("i90003", "G", false)
	q := trait("i90004", "Q", false)
	a := trait("i90005", "A", false)
	b := trait("i90006", "B", false)
	m := trait("i90007", "M", false)
	s := trait("i90008", "S", true)
	parent(c.ID, p.ID)
	parent(g.ID, c.ID)
	parent(m.ID, a.ID)
	parent(m.ID, b.ID)
	parent(s.ID, p.ID)
	if err := testDB.Exec(model.TraitSexualFamilySQL).Error; err != nil {
		t.Fatal(err)
	}

	character := func(name string, gender *int16) *model.CatalogCharacter {
		t.Helper()
		row := &model.CatalogCharacter{
			DisplayName: name, Lang: "ja", Gender: gender,
			Extra: empty, FieldProvenance: empty,
		}
		if err := testDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	link := func(characterID, traitID int64, spoiler int16) {
		t.Helper()
		if err := testDB.Create(&model.CatalogCharacterTraitLink{
			CharacterID: characterID, TraitID: traitID, SpoilerLevel: spoiler,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	fem, male := model.GenderFemale, model.GenderMale
	cChain := character("parity-chain", nil)
	cPQ := character("parity-pq", nil)
	cQ := character("parity-q", nil)
	cSpoil := character("parity-spoil", nil)
	cSex := character("parity-sex", nil)
	cFemaleG := character("parity-female", &fem)
	cMale := character("parity-male", &male)
	cMulti := character("parity-multi", nil)
	cDead := character("parity-dead", nil)
	cR18 := character("parity-r18-only", nil)
	link(cChain.ID, g.ID, model.SpoilerNone)
	link(cPQ.ID, p.ID, model.SpoilerNone)
	link(cPQ.ID, q.ID, model.SpoilerNone)
	link(cQ.ID, q.ID, model.SpoilerNone)
	link(cSpoil.ID, p.ID, model.SpoilerMild)
	link(cSex.ID, s.ID, model.SpoilerNone)
	link(cFemaleG.ID, g.ID, model.SpoilerNone)
	link(cMulti.ID, m.ID, model.SpoilerNone)
	link(cDead.ID, p.ID, model.SpoilerNone)

	wHi := createWork(t, "pop-hi")
	wMid := createWork(t, "pop-mid")
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: wHi.ID, SourceID: 2, Metric: model.PopularityMetricBgmCollect, Value: 1000,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: wMid.ID, SourceID: 2, Metric: model.PopularityMetricDownloads, Value: 100,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createWorkCharacter(t, wHi.ID, cFemaleG.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, wMid.ID, cChain.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, wMid.ID, cDead.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	wR18 := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: "pop-r18",
		ContentRating: model.ContentRatingR18, Status: model.WorkStatusLive,
	}
	if err := testDB.Create(wR18).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: wR18.ID, SourceID: 2, Metric: model.PopularityMetricBgmCollect, Value: 100000,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createWorkCharacter(t, wR18.ID, cR18.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	var rows []chardocs.Row
	if err := testDB.Raw(`SELECT id, display_name, lang, coalesce(latin,'') AS latin, gender
		FROM catalog_character WHERE deleted_at IS NULL ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	charCtx, err := chardocs.Load(t.Context(), testDB)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := charCtx.Build(t.Context(), testDB, rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.UpsertBatch(t.Context(), catsearch.IndexCharacters, docs); err != nil {
		t.Fatal(err)
	}
	if err := idx.Refresh(t.Context(), catsearch.IndexCharacters); err != nil {
		t.Fatal(err)
	}
	waitCharactersIndexed(t, idx, len(rows))

	if err := testDB.Delete(cDead).Error; err != nil {
		t.Fatal(err)
	}

	return charParity{
		svc: svc, idx: idx,
		p: p.ID, q: q.ID, a: a.ID, s: s.ID,
		chain: cChain.ID, pq: cPQ.ID, onlyQ: cQ.ID, spoil: cSpoil.ID,
		sex: cSex.ID, femaleG: cFemaleG.ID, male: cMale.ID, multi: cMulti.ID, dead: cDead.ID,
		popHi: cFemaleG.ID, popMid: cChain.ID, popLo: cDead.ID, popR18: cR18.ID,
	}
}

func registryCharIDs(t *testing.T, svc *PublicService, nsfw bool, f CharacterTraitFilter) []int64 {
	t.Helper()
	page, err := svc.CharactersList(t.Context(), nil, "", 100, CharacterListInclude{}, nsfw, f, false)
	if err != nil {
		t.Fatalf("CharactersList %+v nsfw=%v: %v", f, nsfw, err)
	}
	out := make([]int64, len(page.Items))
	for i, it := range page.Items {
		out[i] = it.ID
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func indexCharPage(t *testing.T, svc *PublicService, nsfw bool, f CharacterTraitFilter, sort string) CharactersSearchPage {
	t.Helper()
	if sort == "" {
		sort = "id"
	}
	data, err := svc.CharactersSearch(t.Context(), CharactersSearchFilter{
		TraitIDs: f.TraitIDs, MatchAny: f.MatchAny, Genders: f.Genders,
		NSFW: nsfw, Sort: sort, Page: 1, Limit: 100,
	})
	if err != nil {
		t.Fatalf("CharactersSearch %+v nsfw=%v: %v", f, nsfw, err)
	}
	return data
}

func indexCharIDs(t *testing.T, svc *PublicService, nsfw bool, f CharacterTraitFilter) []int64 {
	t.Helper()
	data := indexCharPage(t, svc, nsfw, f, "id")
	out := make([]int64, len(data.Items))
	for i, it := range data.Items {
		out[i] = it.ID
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestCharactersSearchParityWithRegistry(t *testing.T) {
	c := seedCharacterParity(t)
	filters := []struct {
		name string
		f    CharacterTraitFilter
	}{
		{name: "none", f: CharacterTraitFilter{}},
		{name: "trait P", f: CharacterTraitFilter{TraitIDs: []int64{c.p}}},
		{name: "trait P,Q all", f: CharacterTraitFilter{TraitIDs: []int64{c.p, c.q}}},
		{name: "trait P,Q any", f: CharacterTraitFilter{TraitIDs: []int64{c.p, c.q}, MatchAny: true}},
		{name: "gender female", f: CharacterTraitFilter{Genders: []int16{model.GenderFemale}}},
		{name: "trait P and female", f: CharacterTraitFilter{TraitIDs: []int64{c.p}, Genders: []int16{model.GenderFemale}}},
	}
	for _, nsfw := range []bool{false, true} {
		for _, tc := range filters {
			reg := registryCharIDs(t, c.svc, nsfw, tc.f)
			idx := indexCharIDs(t, c.svc, nsfw, tc.f)
			if !reflect.DeepEqual(reg, idx) {
				t.Fatalf("nsfw=%v %s registry %v index %v", nsfw, tc.name, reg, idx)
			}
		}
	}

	none := indexCharPage(t, c.svc, false, CharacterTraitFilter{}, "id")
	liveNone := registryCharIDs(t, c.svc, false, CharacterTraitFilter{})
	if none.Total != int64(len(liveNone)+1) {
		t.Fatalf("page-mode total %d, live set %d; nightly index still counts the soft-deleted row", none.Total, len(liveNone))
	}
	if len(none.Items) != len(liveNone) {
		t.Fatalf("hydrated page %d, live set %d; page must be short by the soft-deleted row", len(none.Items), len(liveNone))
	}

	pCount := map[bool]int64{}
	for _, nsfw := range []bool{false, true} {
		pPage := indexCharPage(t, c.svc, nsfw, CharacterTraitFilter{TraitIDs: []int64{c.p}}, "id")
		counts, err := c.svc.CharacterTraitCounts(t.Context(), nsfw)
		if err != nil {
			t.Fatal(err)
		}
		if counts[c.p] != pPage.Total {
			t.Fatalf("nsfw=%v character_count[%d]=%d, page-mode total=%d", nsfw, c.p, counts[c.p], pPage.Total)
		}
		pCount[nsfw] = counts[c.p]
	}
	// S is a sexual child of the non-sexual P, so the fixture only exercises the
	// sfw/nsfw split if P's count differs between the two: a search or count
	// reading trait_ids for an sfw request passed every assertion above when S
	// had no parent.
	if pCount[false] >= pCount[true] {
		t.Fatalf("P counts sfw=%d nsfw=%d; the fixture must make the sexual descendant visible only with nsfw", pCount[false], pCount[true])
	}

	pop := indexCharPage(t, c.svc, true, CharacterTraitFilter{}, "popularity")
	got := make([]int64, len(pop.Items))
	for i, it := range pop.Items {
		got[i] = it.ID
	}
	if len(got) < 3 || got[0] != c.popR18 || got[1] != c.popHi || got[2] != c.popMid {
		t.Fatalf("nsfw popularity order %v, want %d then %d then %d then id-asc zeros", got, c.popR18, c.popHi, c.popMid)
	}
	for i := 3; i < len(got)-1; i++ {
		if got[i] > got[i+1] {
			t.Fatalf("id tie-break broken at %v", got[i:])
		}
	}
	for _, id := range got {
		if id == c.dead {
			t.Fatal("soft-deleted character hydrated")
		}
	}

	popSFW := indexCharPage(t, c.svc, false, CharacterTraitFilter{}, "popularity")
	sfwGot := make([]int64, len(popSFW.Items))
	for i, it := range popSFW.Items {
		sfwGot[i] = it.ID
	}
	if len(sfwGot) < 2 || sfwGot[0] == c.popR18 {
		t.Fatalf("sfw popularity first %v, r18-only %d must not lead", sfwGot, c.popR18)
	}
	if sfwGot[0] != c.popHi || sfwGot[1] != c.popMid {
		t.Fatalf("sfw popularity order %v, want %d then %d", sfwGot, c.popHi, c.popMid)
	}
}
