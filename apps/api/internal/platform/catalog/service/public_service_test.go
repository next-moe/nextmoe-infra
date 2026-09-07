package service

import (
	"reflect"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	srcb "api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	srcVNDB         int16 = 2
	srcErogamescape int16 = 5
	srcDlsite       int16 = 4
)

func newPublicSvc() *PublicService {
	return NewPublicService(testDB, NewReadService(testDB), testResolve, "")
}

func TestCharacterIntroElectionDerivedException(t *testing.T) {
	cleanTables(t)
	ch := model.CatalogCharacter{DisplayName: "選挙対象"}
	if err := testDB.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(src, prov int16, text string) {
		if err := testDB.Create(&model.CatalogCharacterIntro{
			CharacterID: ch.ID, Lang: "zh-Hans", Intro: text, SourceID: src, Provenance: prov,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk(srcVNDB, 1, "翻译机器行")
	mk(sourceDerived, 1, "提取机器行")

	svc := newPublicSvc()
	intros, err := svc.characterIntros(t.Context(), ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(intros) != 1 || intros[0].Intro != "提取机器行" || !intros[0].Machine || intros[0].Source != "derived" {
		t.Fatalf("derived must win among machine rows, got %+v", intros)
	}

	mk(srcErogamescape, 0, "源文行")
	intros, err = svc.characterIntros(t.Context(), ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(intros) != 1 || intros[0].Intro != "源文行" || intros[0].Machine {
		t.Fatalf("a source row must still beat the derived extraction, got %+v", intros)
	}
}

func createWorkX(t *testing.T, medium, rating, status int16, name string) *model.CatalogWork {
	t.Helper()
	w := &model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, ContentRating: rating, Status: status}
	if err := testDB.Create(w).Error; err != nil {
		t.Fatalf("create work: %v", err)
	}
	return w
}

func claimWork(t *testing.T, id int64, site string, productWorkID int64) {
	t.Helper()
	if err := testDB.Exec(`UPDATE catalog_work SET site = ?, product_work_id = ? WHERE id = ?`,
		site, productWorkID, id).Error; err != nil {
		t.Fatalf("claim work: %v", err)
	}
}

func createRelease(t *testing.T, workID int64, y, m, d int16) *model.CatalogRelease {
	t.Helper()
	r := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	if y != 0 {
		r.ReleasedY = &y
	}
	if m != 0 {
		r.ReleasedM = &m
	}
	if d != 0 {
		r.ReleasedD = &d
	}
	if err := testDB.Create(r).Error; err != nil {
		t.Fatalf("create release: %v", err)
	}
	return r
}

func createWorkRelation(t *testing.T, a, b int64) {
	t.Helper()
	var relTypeID int64
	if err := testDB.Raw(`SELECT id FROM catalog_relation_type ORDER BY id LIMIT 1`).Scan(&relTypeID).Error; err != nil || relTypeID == 0 {
		t.Fatalf("seeded relation type lookup: id=%d err=%v", relTypeID, err)
	}
	if err := testDB.Create(&model.CatalogWorkRelation{AWorkID: a, BWorkID: b, RelationTypeID: relTypeID}).Error; err != nil {
		t.Fatalf("create work relation: %v", err)
	}
}

func TestPublicLookupExactOnly(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Karenai")
	claimWork(t, w.ID, "galgame_wiki", 1)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v19658", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v99999", model.LinkKindProbable)

	for _, in := range []string{"v19658", "19658"} {
		data, found, err := svc.Lookup(ctx, "vndb", in, false)
		if err != nil || !found {
			t.Fatalf("lookup %q: found=%v err=%v", in, found, err)
		}
		if data.Work == nil || data.Work.ID != w.ID {
			t.Fatalf("lookup %q: work=%v", in, data.Work)
		}
		if data.ClaimedBy == nil || data.ClaimedBy.Site != "galgame_wiki" || data.ClaimedBy.WorkID != 1 {
			t.Fatalf("lookup %q: claimed_by=%v", in, data.ClaimedBy)
		}
		if data.Work.Medium != "galgame" || data.Work.ContentRating != "all_ages" {
			t.Fatalf("lookup %q: brief=%+v", in, data.Work)
		}
	}
	if _, found, _ := svc.Lookup(ctx, "vndb", "v99999", false); found {
		t.Fatal("probable anchor must not resolve on the public face")
	}
	if _, found, _ := svc.Lookup(ctx, "vndb", "v00000", false); found {
		t.Fatal("unknown external id must 404")
	}
	if _, found, _ := svc.Lookup(ctx, "nosuchsource", "x", false); found {
		t.Fatal("unknown source key must 404")
	}

	addExternalRef(t, model.EntityTypeWork, w.ID, srcErogamescape, "23956", model.LinkKindExact)
	// "erogamespace" was the registry key until the rename; callers that learned
	// it from the old doc string must keep resolving.
	for _, src := range []string{"erogamescape", "erogamespace"} {
		if _, found, err := svc.Lookup(ctx, src, "23956", false); err != nil || !found {
			t.Fatalf("lookup via %q: found=%v err=%v", src, found, err)
		}
	}
	detail, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("work detail: found=%v err=%v", found, err)
	}
	for _, r := range detail.Refs {
		if r.Source == "erogamespace" {
			t.Fatal("public refs must carry erogamescape, never the legacy misspelling")
		}
	}
}

func TestPublicLookupR18Hidden(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "R18 game")
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v100", model.LinkKindExact)
	if _, found, err := svc.Lookup(ctx, "vndb", "v100", false); err != nil || found {
		t.Fatalf("r18 work must be hidden from lookup: found=%v err=%v", found, err)
	}
}

func TestPublicLookupReleaseAnchor(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "SKU work")
	rel := createRelease(t, w.ID, 2016, 11, 25)
	addExternalRef(t, model.EntityTypeRelease, rel.ID, srcDlsite, "RJ123456", model.LinkKindExact)

	data, found, err := svc.Lookup(ctx, "dlsite", "RJ123456", false)
	if err != nil || !found || data.Work == nil || data.Work.ID != w.ID {
		t.Fatalf("release anchor lookup: found=%v work=%v err=%v", found, data.Work, err)
	}
}

func TestPublicLookupBatchOrder(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "batch hit")
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v1", model.LinkKindExact)

	items, err := svc.LookupBatch(ctx, []dto.PublicLookupPair{
		{Source: "vndb", ExternalID: "v1"},
		{Source: "vndb", ExternalID: "v404"},
		{Source: "vndb", ExternalID: "1"},
	}, false)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("batch len=%d", len(items))
	}
	if items[0].Work == nil || items[0].Work.ID != w.ID {
		t.Fatalf("item0 miss: %+v", items[0])
	}
	if items[1].Work != nil {
		t.Fatalf("item1 should be a null miss: %+v", items[1])
	}
	if items[2].Work == nil || items[2].Work.ID != w.ID {
		t.Fatalf("item2 (normalized) miss: %+v", items[2])
	}
	for i, it := range items {
		if it.Type != "work" {
			t.Fatalf("item%d type=%q want the resolved default \"work\"", i, it.Type)
		}
	}
}

const srcBangumiPub int16 = 3

func TestPublicLookupTypedEntities(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "同 id 作品")
	claimWork(t, w.ID, "galgame_wiki", 7)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcBangumiPub, "12345", model.LinkKindExact)

	ch := createCharacter(t, "同 id 角色")
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcBangumiPub, "12345", model.LinkKindExact)

	n := createCreditName(t, nil, "テスト脚本家")
	addExternalRef(t, model.EntityTypeCreditName, n.ID, srcVNDB, "54321", model.LinkKindExact)

	lbl := &model.CatalogLabel{DisplayName: "テストブランド", Kind: model.LabelKindGameBrand}
	if err := testDB.Create(lbl).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcVNDB, "p129", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcVNDB, "p999", model.LinkKindProbable)

	untyped, found, err := svc.Lookup(ctx, "bangumi", "12345", false)
	if err != nil || !found || untyped.Work == nil || untyped.Work.ID != w.ID {
		t.Fatalf("absent type must resolve the work: found=%v work=%v err=%v", found, untyped.Work, err)
	}
	if untyped.ClaimedBy == nil || untyped.ClaimedBy.Site != "galgame_wiki" {
		t.Fatalf("absent type must keep claimed_by: %+v", untyped.ClaimedBy)
	}
	if untyped.Name != nil || untyped.Character != nil || untyped.Label != nil {
		t.Fatalf("the work lane must leave the typed blocks empty: %+v", untyped)
	}

	got, found, err := svc.LookupTyped(ctx, "bangumi", "12345", model.EntityTypeCharacter, false)
	if err != nil || !found || got.Character == nil || got.Character.ID != ch.ID {
		t.Fatalf("type=character: found=%v character=%+v err=%v", found, got.Character, err)
	}
	if got.Work != nil || got.ClaimedBy != nil || got.Name != nil || got.Label != nil {
		t.Fatalf("type=character must populate character only: %+v", got)
	}

	got, found, err = svc.LookupTyped(ctx, "vndb", "54321", model.EntityTypeCreditName, false)
	if err != nil || !found || got.Name == nil || got.Name.ID != n.ID {
		t.Fatalf("type=name: found=%v name=%+v err=%v", found, got.Name, err)
	}
	if got.Work != nil || got.ClaimedBy != nil || got.Character != nil || got.Label != nil {
		t.Fatalf("type=name must populate name only: %+v", got)
	}

	got, found, err = svc.LookupTyped(ctx, "vndb", "p129", model.EntityTypeLabel, false)
	if err != nil || !found || got.Label == nil || got.Label.ID != lbl.ID {
		t.Fatalf("type=label: found=%v label=%+v err=%v", found, got.Label, err)
	}
	if got.Work != nil || got.ClaimedBy != nil || got.Name != nil || got.Character != nil {
		t.Fatalf("type=label must populate label only: %+v", got)
	}

	for _, c := range []struct {
		name       string
		source     string
		ext        string
		entityType int16
	}{
		{"work anchor asked as a name", "bangumi", "12345", model.EntityTypeCreditName},
		{"character anchor asked as a label", "bangumi", "12345", model.EntityTypeLabel},
		{"label anchor asked as a work", "vndb", "p129", model.EntityTypeWork},
		{"probable label anchor", "vndb", "p999", model.EntityTypeLabel},
		{"unknown source", "nosuchsource", "p129", model.EntityTypeLabel},
	} {
		if _, found, err := svc.LookupTyped(ctx, c.source, c.ext, c.entityType, false); err != nil || found {
			t.Fatalf("%s must miss: found=%v err=%v", c.name, found, err)
		}
	}
}

func TestPublicLookupTypedNSFWParity(t *testing.T) {
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE catalog_character_trait RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate trait vocab: %v", err)
	}
	svc := newPublicSvc()
	ctx := t.Context()

	ch := createCharacter(t, "隠しヒロイン")
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcVNDB, "c777", model.LinkKindExact)
	for _, tr := range []model.CatalogCharacterTrait{
		{VndbTID: "i1", Name: "Long Hair", Sexual: false, Searchable: true, Applicable: true},
		{VndbTID: "i2", Name: "Sexual Trait", Sexual: true, Searchable: true, Applicable: true},
	} {
		trait := tr
		if err := testDB.Create(&trait).Error; err != nil {
			t.Fatalf("create trait %s: %v", trait.Name, err)
		}
		if err := testDB.Create(&model.CatalogCharacterTraitLink{
			CharacterID: ch.ID, TraitID: trait.ID, SpoilerLevel: 0,
		}).Error; err != nil {
			t.Fatalf("link trait %s: %v", trait.Name, err)
		}
	}

	sfw, found, err := svc.LookupTyped(ctx, "vndb", "c777", model.EntityTypeCharacter, false)
	if err != nil || !found || sfw.Character == nil {
		t.Fatalf("nsfw=false must still resolve the identity: found=%v err=%v", found, err)
	}
	if len(sfw.Character.Traits) != 1 || sfw.Character.Traits[0].Name != "Long Hair" {
		t.Fatalf("nsfw=false traits = %+v (want the safe one only)", sfw.Character.Traits)
	}
	direct, found, err := svc.Character(ctx, ch.ID, false, false, 0, 0, 0)
	if err != nil || !found {
		t.Fatalf("direct character: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(*sfw.Character, direct) {
		t.Fatalf("lookup record must equal the detail projection:\nlookup=%+v\ndetail=%+v", *sfw.Character, direct)
	}

	nsfwData, found, err := svc.LookupTyped(ctx, "vndb", "c777", model.EntityTypeCharacter, true)
	if err != nil || !found || nsfwData.Character == nil {
		t.Fatalf("nsfw=true character: found=%v err=%v", found, err)
	}
	if len(nsfwData.Character.Traits) != 2 {
		t.Fatalf("nsfw=true traits = %+v (want the sexual trait to join)", nsfwData.Character.Traits)
	}
}

func TestPublicLookupBatchMixedTypes(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "batch 作品")
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v1", model.LinkKindExact)
	ch := createCharacter(t, "batch 角色")
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcBangumiPub, "12345", model.LinkKindExact)
	lbl := &model.CatalogLabel{DisplayName: "batch ブランド", Kind: model.LabelKindGameBrand}
	if err := testDB.Create(lbl).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcVNDB, "p129", model.LinkKindExact)

	items, err := svc.LookupBatch(ctx, []dto.PublicLookupPair{
		{Source: "vndb", ExternalID: "v1"},
		{Source: "bangumi", ExternalID: "12345", Type: "character"},
		{Source: "vndb", ExternalID: "p129", Type: "label"},
		{Source: "vndb", ExternalID: "p129", Type: "work"},
		{Source: "vndb", ExternalID: "v1", Type: "name"},
	}, false)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("batch len=%d want 5", len(items))
	}
	wantTypes := []string{"work", "character", "label", "work", "name"}
	for i, want := range wantTypes {
		if items[i].Type != want {
			t.Fatalf("item%d type=%q want %q", i, items[i].Type, want)
		}
	}
	if items[0].Work == nil || items[0].Work.ID != w.ID || items[0].Character != nil {
		t.Fatalf("item0 (work) = %+v", items[0])
	}
	if items[1].Character == nil || items[1].Character.ID != ch.ID || items[1].Work != nil {
		t.Fatalf("item1 (character) = %+v", items[1])
	}
	if items[2].Label == nil || items[2].Label.ID != lbl.ID || items[2].Work != nil {
		t.Fatalf("item2 (label) = %+v", items[2])
	}
	for _, i := range []int{3, 4} {
		it := items[i]
		if it.Work != nil || it.ClaimedBy != nil || it.Name != nil || it.Character != nil || it.Label != nil {
			t.Fatalf("item%d must be an all-null miss: %+v", i, it)
		}
	}
}

func TestPublicWorkDetailFetchableSet(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	live := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "live galgame")
	anime := createWorkX(t, 4, model.ContentRatingAllAges, model.WorkStatusLive, "anime")
	stub := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusStub, "stub")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "r18")
	merged := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusMerged, "merged")

	cases := []struct {
		id   int64
		want bool
	}{
		{live.ID, true}, {anime.ID, false}, {stub.ID, false}, {r18.ID, false}, {merged.ID, false},
		{99999, false},
	}
	for _, c := range cases {
		_, found, err := svc.WorkDetail(ctx, c.id, PublicInclude{}, false, 0, PublicFields{})
		if err != nil {
			t.Fatalf("work %d: %v", c.id, err)
		}
		if found != c.want {
			t.Fatalf("work %d: found=%v want=%v", c.id, found, c.want)
		}
	}
}

func TestPublicWorkRefsExactOnlyAndRelations(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "main")
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v42", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, 3, "555", model.LinkKindProbable)

	sfwOther := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "sfw related")
	r18Other := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "r18 related")
	createWorkRelation(t, w.ID, sfwOther.ID)
	createWorkRelation(t, w.ID, r18Other.ID)

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{Relations: true}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("detail: found=%v err=%v", found, err)
	}
	if len(rec.Refs) != 1 || rec.Refs[0].Source != "vndb" || rec.Refs[0].ExternalID != "v42" {
		t.Fatalf("refs must be exact-only: %+v", rec.Refs)
	}
	if len(rec.Relations) != 1 || rec.Relations[0].Work.ID != sfwOther.ID {
		t.Fatalf("relations must drop the r18 end: %+v", rec.Relations)
	}
}

func TestPublicWorkCreditsInclude(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "credited")
	name := createCreditName(t, nil, "麻枝准")
	createCredit(t, w.ID, name.ID, seededRoleID(t), nil)

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{Credits: true}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("detail: found=%v err=%v", found, err)
	}
	if len(rec.Credits) != 1 || len(rec.Credits[0].Credits) != 1 || rec.Credits[0].Credits[0].ID != name.ID {
		t.Fatalf("credits projection: %+v", rec.Credits)
	}
	if rec.Credits[0].Credits[0].LabelID != 0 || rec.Credits[0].Credits[0].Label != "" {
		t.Fatalf("personal credit must carry no label: %+v", rec.Credits[0].Credits[0])
	}
	if rec.Credits[0].Credits[0].Source != "" {
		t.Fatalf("sourceless credit must omit source: %+v", rec.Credits[0].Credits[0])
	}

	bare, _, _ := svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if bare.Credits != nil {
		t.Fatalf("bare record must omit credits: %+v", bare.Credits)
	}
}

// "LabelSigner" misreads the column and the name is kept so the misreading
// stays visible: catalog_credit.label_id does not mean "signed to this brand".
// Its only writer is the bangumi importer, which sets it when the credited
// entity is itself a company or a circle (p.Type 2/3) — 21,425 rows, all
// bangumi. It is therefore bangumi's modelling history, not a field a person
// restates, which is why catalog.work.credits does not expose it.
func TestPublicWorkCreditsLabelSigner(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "signed")
	name := createCreditName(t, nil, "Key")
	label := &model.CatalogLabel{DisplayName: "Visual Arts", Kind: model.LabelKindGameBrand}
	if err := testDB.Create(label).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	c := createCredit(t, w.ID, name.ID, seededRoleID(t), nil)
	var srcID int16
	if err := testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'erogamescape'`).Scan(&srcID).Error; err != nil || srcID == 0 {
		t.Fatalf("erogamescape source id=%d err=%v", srcID, err)
	}
	if err := testDB.Model(c).Updates(map[string]any{"label_id": label.ID, "source_id": srcID}).Error; err != nil {
		t.Fatalf("set signer: %v", err)
	}

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{Credits: true}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("detail: found=%v err=%v", found, err)
	}
	if len(rec.Credits) != 1 || len(rec.Credits[0].Credits) != 1 {
		t.Fatalf("credits projection: %+v", rec.Credits)
	}
	item := rec.Credits[0].Credits[0]
	if item.LabelID != label.ID || item.Label != "Visual Arts" {
		t.Fatalf("signer must reach the wire with its id and name: %+v", item)
	}
	if item.ID != name.ID {
		t.Fatalf("signer must not displace the credited name: %+v", item)
	}
	if item.Source != "erogamescape" {
		t.Fatalf("credit must name its attributing source, publicly spelled: %q", item.Source)
	}
}

func TestPublicSiblingNameCarriesDisplayName(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	p := createPerson(t, "Key")
	main := createCreditName(t, &p.ID, "麻枝准")
	sib := createCreditName(t, &p.ID, "織田杏子")
	if err := testDB.Model(sib).Update("lang", "zh-Hant").Error; err != nil {
		t.Fatalf("set sibling lang: %v", err)
	}

	got, found, err := svc.Name(ctx, main.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("name: found=%v err=%v", found, err)
	}
	if len(got.Siblings) != 1 {
		t.Fatalf("public sibling must surface: %+v", got.Siblings)
	}
	s := got.Siblings[0]
	if s.DisplayName != "織田杏子" {
		t.Fatalf("sibling display_name = %q, want the name of record", s.DisplayName)
	}
	if s.Lang != "zh-Hant" {
		t.Fatalf("sibling lang = %q, want the tag the buckets could not carry", s.Lang)
	}
}

const publicPhotoHash = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"

func TestPublicNameHiddenLinkAndR18Drop(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	p := createPerson(t, "Key")
	i16 := func(v int16) *int16 { return &v }
	if err := testDB.Model(p).Updates(map[string]any{
		"photo_hash": publicPhotoHash, "gender": i16(1),
		"birth_y": i16(1975), "birth_m": i16(1), "birth_d": i16(3),
	}).Error; err != nil {
		t.Fatalf("set person block: %v", err)
	}
	nPublic := createCreditName(t, &p.ID, "麻枝准")
	nHidden := &model.CatalogCreditName{
		PersonID: &p.ID, Name: "裏名義", Kind: model.CreditNameKindDistinctPersona,
		LinkVisibility: model.LinkVisibilityHidden,
	}
	if err := testDB.Create(nHidden).Error; err != nil {
		t.Fatalf("create hidden name: %v", err)
	}

	sfw := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "sfw")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "r18")
	role := seededRoleID(t)
	createCredit(t, sfw.ID, nPublic.ID, role, nil)
	createCredit(t, r18.ID, nPublic.ID, role, nil)

	got, found, err := svc.Name(ctx, nPublic.ID, true, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("person: found=%v err=%v", found, err)
	}
	if got.PersonID != p.ID {
		t.Fatalf("public link must expose person_id: %d", got.PersonID)
	}
	if len(got.Siblings) != 0 {
		t.Fatalf("hidden sibling must not surface: %+v", got.Siblings)
	}
	if len(got.Credits) != 1 || got.Credits[0].Work.ID != sfw.ID {
		t.Fatalf("r18 credit must be dropped: %+v", got.Credits)
	}
	if got.PhotoHash != publicPhotoHash {
		t.Fatalf("public link must expose photo_hash: %q", got.PhotoHash)
	}
	if got.Gender == nil || *got.Gender != 1 {
		t.Fatalf("public link must expose gender: %v", got.Gender)
	}
	if got.BirthY == nil || *got.BirthY != 1975 || got.BirthM == nil || got.BirthD == nil {
		t.Fatalf("public link must expose the fuzzy birth date: %v/%v/%v", got.BirthY, got.BirthM, got.BirthD)
	}

	h, found, err := svc.Name(ctx, nHidden.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("hidden person: found=%v err=%v", found, err)
	}
	if h.PersonID != 0 {
		t.Fatalf("hidden link must withhold person_id: %d", h.PersonID)
	}
	if h.PhotoHash != "" || h.Gender != nil || h.BirthY != nil || h.BirthM != nil || h.BirthD != nil {
		t.Fatalf("hidden link must withhold the person block: %q %v %v/%v/%v",
			h.PhotoHash, h.Gender, h.BirthY, h.BirthM, h.BirthD)
	}
}

func TestPublicLabelIntrosLinks(t *testing.T) {
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE catalog_label_intro RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate label intro: %v", err)
	}
	svc := newPublicSvc()
	ctx := t.Context()

	const (
		srcBangumi      int16 = 3
		srcOfficialSite int16 = 9
		srcTwitter      int16 = 10
		srcCien         int16 = 14
	)

	lbl := &model.CatalogLabel{DisplayName: "ALcot", Kind: model.LabelKindPublisher}
	if err := testDB.Create(lbl).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}

	for _, in := range []model.CatalogLabelIntro{
		{LabelID: lbl.ID, Lang: "en", Intro: "English intro.", SourceID: srcVNDB},
		{LabelID: lbl.ID, Lang: "ja", Intro: "勝つ紹介。", SourceID: srcBangumi},
		{LabelID: lbl.ID, Lang: "ja", Intro: "負ける紹介。", SourceID: srcDlsite},
	} {
		if err := testDB.Create(&in).Error; err != nil {
			t.Fatalf("create label intro: %v", err)
		}
	}

	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcOfficialSite, "www.alcot.biz", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcTwitter, "alcot_official", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcCien, "29601", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcVNDB, "p129", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeLabel, lbl.ID, srcDlsite, "VG02192", model.LinkKindProbable)

	got, found, err := svc.Label(ctx, lbl.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("label: found=%v err=%v", found, err)
	}

	if len(got.Intros) != 2 {
		t.Fatalf("intros len=%d want 2: %+v", len(got.Intros), got.Intros)
	}
	if got.Intros[0] != (dto.PublicIntro{Lang: "en", Intro: "English intro.", Source: "vndb"}) {
		t.Fatalf("intros[0]=%+v", got.Intros[0])
	}
	if got.Intros[1] != (dto.PublicIntro{Lang: "ja", Intro: "勝つ紹介。", Source: "bangumi"}) {
		t.Fatalf("intros[1] (lowest source_id must win the language)=%+v", got.Intros[1])
	}

	wantLinks := []dto.PublicLabelLink{
		{Source: "official_site", URL: "https://www.alcot.biz"},
		{Source: "twitter", URL: "https://x.com/alcot_official"},
		{Source: "cien", URL: "https://ci-en.dlsite.com/creator/29601"},
	}
	if len(got.Links) != len(wantLinks) {
		t.Fatalf("links len=%d want %d: %+v", len(got.Links), len(wantLinks), got.Links)
	}
	for i, w := range wantLinks {
		if got.Links[i] != w {
			t.Fatalf("links[%d]=%+v want %+v", i, got.Links[i], w)
		}
	}
	for _, lk := range got.Links {
		if lk.Source == "vndb" || lk.Source == "dlsite" {
			t.Fatalf("identity anchor leaked into links: %+v", lk)
		}
	}

	bare := &model.CatalogLabel{DisplayName: "Bare", Kind: model.LabelKindGameBrand}
	if err := testDB.Create(bare).Error; err != nil {
		t.Fatalf("create bare label: %v", err)
	}
	b, found, err := svc.Label(ctx, bare.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("bare label: found=%v err=%v", found, err)
	}
	if b.Intros == nil || len(b.Intros) != 0 {
		t.Fatalf("intros must be [] non-null: %+v", b.Intros)
	}
	if b.Links == nil || len(b.Links) != 0 {
		t.Fatalf("links must be [] non-null: %+v", b.Links)
	}
}

func TestPublicNSFWGate(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "R18作品")
	safe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年齢作品")
	createWorkRelation(t, r18.ID, safe.ID)
	addExternalRef(t, model.EntityTypeWork, r18.ID, srcVNDB, "v104", model.LinkKindExact)
	if err := testDB.Create(&model.CatalogWorkPopularity{WorkID: r18.ID, SourceID: 3, Metric: model.PopularityMetricBgmWish, Value: 42}).Error; err != nil {
		t.Fatalf("popularity fixture: %v", err)
	}
	if err := testDB.Create(&model.CatalogWorkTag{WorkID: r18.ID, Name: "泣きゲー", Count: 7, SourceID: 3}).Error; err != nil {
		t.Fatalf("tag fixture: %v", err)
	}

	if _, found, err := svc.WorkDetail(ctx, r18.ID, PublicInclude{}, false, 0, PublicFields{}); err != nil || found {
		t.Fatalf("default r18 detail: found=%v err=%v (want hidden)", found, err)
	}
	if _, found, _ := svc.Lookup(ctx, "vndb", "v104", false); found {
		t.Fatal("default r18 lookup resolved (want miss)")
	}

	rec, found, err := svc.WorkDetail(ctx, r18.ID, PublicInclude{Relations: true}, true, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("nsfw r18 detail: found=%v err=%v", found, err)
	}
	if rec.ContentRating != "r18" {
		t.Fatalf("content_rating = %q, want r18", rec.ContentRating)
	}
	if len(rec.Popularity) != 2 || rec.Popularity[0].Metric != "bgm_wish" || rec.Popularity[0].Value != 42 || rec.Popularity[0].Source != "bangumi" {
		t.Fatalf("popularity facet = %+v", rec.Popularity)
	}
	if len(rec.Tags) != 1 || rec.Tags[0].Name != "泣きゲー" || rec.Tags[0].Source != "bangumi" {
		t.Fatalf("tags facet = %+v", rec.Tags)
	}
	if len(rec.Relations) != 1 || rec.Relations[0].Work.ID != safe.ID {
		t.Fatalf("relations = %+v", rec.Relations)
	}
	if _, found, _ = svc.Lookup(ctx, "vndb", "v104", true); !found {
		t.Fatal("nsfw r18 lookup missed (want hit)")
	}

	recSafe, _, err := svc.WorkDetail(ctx, safe.ID, PublicInclude{Relations: true}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("safe detail: %v", err)
	}
	if len(recSafe.Relations) != 0 {
		t.Fatalf("safe relations nsfw-off = %+v (want r18 end dropped)", recSafe.Relations)
	}
	recSafe, _, _ = svc.WorkDetail(ctx, safe.ID, PublicInclude{Relations: true}, true, 0, PublicFields{})
	if len(recSafe.Relations) != 1 || recSafe.Relations[0].Work.ID != r18.ID {
		t.Fatalf("safe relations nsfw-on = %+v", recSafe.Relations)
	}
}

func TestPublicCharacterTraits(t *testing.T) {
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE catalog_character_trait RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate trait vocab: %v", err)
	}
	svc := newPublicSvc()
	ctx := t.Context()

	ch := &model.CatalogCharacter{DisplayName: "テスト嬢", Lang: "ja"}
	if err := testDB.Create(ch).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	mkTrait := func(tid, gid, name, nameZh string, sexual bool) int64 {
		tr := &model.CatalogCharacterTrait{VndbTID: tid, Name: name, NameZh: nameZh, GroupTID: gid, Sexual: sexual, Searchable: true, Applicable: true}
		if err := testDB.Create(tr).Error; err != nil {
			t.Fatalf("create trait %s: %v", name, err)
		}
		return tr.ID
	}
	mkTrait("i0", "", "Hair", "毛发", false)
	tSafe := mkTrait("i1", "i0", "Long Hair", "长发", false)
	tSexual := mkTrait("i2", "", "Sexual Trait", "", true)
	tSpoiler := mkTrait("i3", "", "Hidden Past", "", false)
	link := func(traitID int64, spoiler int16) {
		if err := testDB.Create(&model.CatalogCharacterTraitLink{CharacterID: ch.ID, TraitID: traitID, SpoilerLevel: spoiler}).Error; err != nil {
			t.Fatalf("link trait: %v", err)
		}
	}
	link(tSafe, 0)
	link(tSexual, 0)
	link(tSpoiler, 2)

	rec, found, err := svc.Character(ctx, ch.ID, false, false, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("character: found=%v err=%v", found, err)
	}
	if len(rec.Traits) != 1 || rec.Traits[0].Name != "Long Hair" {
		t.Fatalf("default traits = %+v (want safe only)", rec.Traits)
	}
	if rec.Traits[0].Localized["zh-Hans"].Value != "长发" ||
		rec.Traits[0].GroupLocalized["zh-Hans"].Value != "毛发" {
		t.Fatalf("zh names = %+v / %+v (want 长发 / 毛发)",
			rec.Traits[0].Localized, rec.Traits[0].GroupLocalized)
	}
	recNSFW, _, _ := svc.Character(ctx, ch.ID, false, true, 0, 50, 0)
	for _, tr := range recNSFW.Traits {
		if tr.Name == "Sexual Trait" && (len(tr.Localized) != 0 || len(tr.GroupLocalized) != 0) {
			t.Fatalf("unrendered trait leaked zh fields: %+v", tr)
		}
	}
	rec, _, _ = svc.Character(ctx, ch.ID, false, true, 0, 50, 0)
	if len(rec.Traits) != 2 {
		t.Fatalf("nsfw traits = %+v (want 2: sexual joins)", rec.Traits)
	}
	rec, _, _ = svc.Character(ctx, ch.ID, false, true, 2, 50, 0)
	if len(rec.Traits) != 3 {
		t.Fatalf("nsfw+spoilers traits = %+v (want 3)", rec.Traits)
	}
	rec, _, _ = svc.Character(ctx, ch.ID, false, false, 2, 50, 0)
	if len(rec.Traits) != 2 {
		t.Fatalf("spoilers-only traits = %+v (want 2: sexual still dropped)", rec.Traits)
	}
}

func TestPublicCharacterIntrosImage(t *testing.T) {
	cleanTables(t)
	svc := NewPublicService(testDB, NewReadService(testDB), testResolve, "http://cdn.test/img")
	ctx := t.Context()

	c := &model.CatalogCharacter{DisplayName: "紹介娘", Lang: "ja"}
	if err := testDB.Create(c).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := testDB.Exec(`UPDATE catalog_character SET image_hash = ? WHERE id = ?`, "abc123hash", c.ID).Error; err != nil {
		t.Fatalf("set image: %v", err)
	}
	for _, row := range []model.CatalogCharacterIntro{
		{CharacterID: c.ID, Lang: "ja", Intro: "vndb の紹介", SourceID: 2},
		{CharacterID: c.ID, Lang: "zh-Hans", Intro: "bgm 简介", SourceID: 3},
	} {
		if err := testDB.Create(&row).Error; err != nil {
			t.Fatalf("intro fixture: %v", err)
		}
	}

	rec, found, err := svc.Character(ctx, c.ID, false, false, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("character: found=%v err=%v", found, err)
	}
	if len(rec.Intros) != 2 {
		t.Fatalf("intros = %+v (want ja + zh-Hans)", rec.Intros)
	}
	if rec.Intros[0].Lang != "ja" || rec.Intros[0].Source != "vndb" || rec.Intros[0].Intro != "vndb の紹介" {
		t.Fatalf("ja intro = %+v", rec.Intros[0])
	}
	if rec.Intros[1].Lang != "zh-Hans" || rec.Intros[1].Source != "bangumi" {
		t.Fatalf("zh intro = %+v", rec.Intros[1])
	}
	if rec.Image == "" {
		t.Fatal("image URL empty (want CDN URL from hash)")
	}
}

func TestPublicNameIntros(t *testing.T) {
	cleanTables(t)
	if err := srcb.EnsureSchema(testDB); err != nil {
		t.Fatalf("src_bangumi schema: %v", err)
	}
	if err := testDB.Exec(`TRUNCATE src_bangumi.person RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatalf("truncate person: %v", err)
	}
	svc := newPublicSvc()
	ctx := t.Context()

	n := &model.CatalogCreditName{Name: "テスト声優", Lang: "ja"}
	if err := testDB.Create(n).Error; err != nil {
		t.Fatalf("create name: %v", err)
	}
	addExternalRef(t, model.EntityTypeCreditName, n.ID, int16(3), "999001", model.LinkKindExact)
	if err := testDB.Exec(`INSERT INTO src_bangumi.person (id, name, type, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (999001, 'p', 1, '', '', '日本の声優。', 0, 0, 'x', now())`).Error; err != nil {
		t.Fatalf("person fixture: %v", err)
	}

	rec, found, err := svc.Name(ctx, n.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("name: found=%v err=%v", found, err)
	}
	if len(rec.Intros) != 1 || rec.Intros[0].Lang != "ja" || rec.Intros[0].Source != "bangumi" || rec.Intros[0].Intro != "日本の声優。" {
		t.Fatalf("intros = %+v", rec.Intros)
	}
}

// The bridged intro joins src_bangumi.person on external_id::bigint, so a
// bangumi ref whose external_id is not a number would be a 500 on a read face
// rather than a missing intro. Production holds 22,991 such refs and every one
// is numeric, which is exactly why nothing would notice the first one that is
// not.
func TestPublicNameIntrosSurviveANonNumericBangumiRef(t *testing.T) {
	cleanTables(t)
	if err := srcb.EnsureSchema(testDB); err != nil {
		t.Fatalf("src_bangumi schema: %v", err)
	}
	if err := testDB.Exec(`TRUNCATE src_bangumi.person RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatalf("truncate person: %v", err)
	}
	svc := newPublicSvc()

	n := &model.CatalogCreditName{Name: "壊れた参照", Lang: "ja"}
	if err := testDB.Create(n).Error; err != nil {
		t.Fatalf("create name: %v", err)
	}
	addExternalRef(t, model.EntityTypeCreditName, n.ID, int16(3), "p999001", model.LinkKindExact)

	rec, found, err := svc.Name(t.Context(), n.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("name: found=%v err=%v", found, err)
	}
	if len(rec.Intros) != 0 {
		t.Fatalf("intros = %+v, want none", rec.Intros)
	}
}

func TestPublicNamePersonIntros(t *testing.T) {
	cleanTables(t)
	if err := srcb.EnsureSchema(testDB); err != nil {
		t.Fatalf("src_bangumi schema: %v", err)
	}
	if err := testDB.Exec(`TRUNCATE src_bangumi.person RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatalf("truncate person: %v", err)
	}
	svc := newPublicSvc()
	ctx := t.Context()

	p := createPerson(t, "Person")
	visible := createCreditName(t, &p.ID, "公開名義")
	hidden := &model.CatalogCreditName{
		PersonID: &p.ID, Name: "裏名義", Kind: model.CreditNameKindDistinctPersona,
		LinkVisibility: model.LinkVisibilityHidden,
	}
	if err := testDB.Create(hidden).Error; err != nil {
		t.Fatalf("create hidden name: %v", err)
	}

	for _, row := range []model.CatalogPersonIntro{
		{PersonID: p.ID, Lang: "ja", Intro: "人物レベルの紹介。", SourceID: 3, Provenance: 0},
		{PersonID: p.ID, Lang: "zh-Hans", Intro: "人物级简介（机翻）。", SourceID: 3, Provenance: 1, MTModel: "test-model"},
	} {
		if err := testDB.Create(&row).Error; err != nil {
			t.Fatalf("create person intro: %v", err)
		}
	}

	addExternalRef(t, model.EntityTypeCreditName, visible.ID, int16(3), "999101", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, hidden.ID, int16(3), "999102", model.LinkKindExact)
	if err := testDB.Exec(`INSERT INTO src_bangumi.person (id, name, type, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (999101, 'a', 1, '', '', '名義レベルの紹介。', 0, 0, 'x', now()),
		       (999102, 'b', 1, '', '', '裏名義レベルの紹介。', 0, 0, 'x', now())`).Error; err != nil {
		t.Fatalf("person fixture: %v", err)
	}

	got, found, err := svc.Name(ctx, visible.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("visible name: found=%v err=%v", found, err)
	}
	if len(got.Intros) != 2 {
		t.Fatalf("want one intro per language, got %+v", got.Intros)
	}
	if got.Intros[0].Lang != "ja" || got.Intros[0].Intro != "人物レベルの紹介。" || got.Intros[0].Machine {
		t.Fatalf("person source row must win ja: %+v", got.Intros[0])
	}
	if got.Intros[0].Source != "bangumi" {
		t.Fatalf("source must be the catalog_source key: %q", got.Intros[0].Source)
	}
	if got.Intros[1].Lang != "zh-Hans" || !got.Intros[1].Machine {
		t.Fatalf("machine row must surface flagged: %+v", got.Intros[1])
	}

	h, found, err := svc.Name(ctx, hidden.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("hidden name: found=%v err=%v", found, err)
	}
	if h.PersonID != 0 {
		t.Fatalf("hidden link must withhold person_id: %d", h.PersonID)
	}
	if len(h.Intros) != 1 || h.Intros[0].Intro != "裏名義レベルの紹介。" || h.Intros[0].Machine {
		t.Fatalf("hidden link must withhold the person lane and keep its own bridge: %+v", h.Intros)
	}

	orphan := createCreditName(t, nil, "孤児名義")
	addExternalRef(t, model.EntityTypeCreditName, orphan.ID, int16(3), "999103", model.LinkKindExact)
	if err := testDB.Exec(`INSERT INTO src_bangumi.person (id, name, type, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (999103, 'c', 1, '', '', '孤児レベルの紹介。', 0, 0, 'x', now())`).Error; err != nil {
		t.Fatalf("orphan person fixture: %v", err)
	}
	o, found, err := svc.Name(ctx, orphan.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("orphan name: found=%v err=%v", found, err)
	}
	if len(o.Intros) != 1 || o.Intros[0].Intro != "孤児レベルの紹介。" {
		t.Fatalf("orphan must still answer from its own bridge: %+v", o.Intros)
	}
}

func TestPublicNameAliases(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	p := createPerson(t, "緒方剛志")
	head := createCreditName(t, &p.ID, "緒方剛志")
	sibling := createCreditName(t, &p.ID, "Ogata Takeshi")

	createNameAlias(t, head.ID, "绪方刚志", "zh-Hans")
	createNameAlias(t, head.ID, "绪方刚", "zh-Hans")
	createNameAlias(t, head.ID, "绪方刚志", "")
	createNameAlias(t, head.ID, "緒方剛志", "ja")
	createNameAlias(t, sibling.ID, "尾形武", "zh-Hans")
	hint := &model.CatalogNameAlias{CreditNameID: head.ID, Name: "ogatakoji-hint",
		Kind: model.AliasKindSearchHint}
	if err := testDB.Create(hint).Error; err != nil {
		t.Fatalf("create search hint: %v", err)
	}

	rec, found, err := svc.Name(ctx, head.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("name: found=%v err=%v", found, err)
	}
	if len(rec.Aliases) != 3 ||
		rec.Aliases[0].Value != "绪方刚" || rec.Aliases[0].Lang != "zh-Hans" ||
		rec.Aliases[1].Value != "绪方刚志" || rec.Aliases[1].Lang != "zh-Hans" ||
		rec.Aliases[2].Value != "绪方刚志" || rec.Aliases[2].Lang != "" {
		t.Fatalf("aliases = %+v (want the zh spellings per lang, display name excluded)", rec.Aliases)
	}
	for _, a := range rec.Aliases {
		if a.Value == "尾形武" {
			t.Fatal("a sibling's alias must never be attributed to this name")
		}
	}
	sib, found, err := svc.Name(ctx, sibling.ID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("sibling: found=%v err=%v", found, err)
	}
	if len(sib.Aliases) != 1 || sib.Aliases[0].Value != "尾形武" {
		t.Fatalf("sibling aliases = %+v", sib.Aliases)
	}

	bare := createCreditName(t, nil, "無別名")
	rec, _, err = svc.Name(ctx, bare.ID, false, false, 50, 0)
	if err != nil {
		t.Fatalf("bare name: %v", err)
	}
	if rec.Aliases == nil || len(rec.Aliases) != 0 {
		t.Fatalf("an alias-less name must serialize []: %#v", rec.Aliases)
	}
}

func createSameSeriesEdge(t *testing.T, a, b int64) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkRelation{AWorkID: a, BWorkID: b, RelationTypeID: 7}).Error; err != nil {
		t.Fatalf("create same_series edge: %v", err)
	}
}

func TestSeriesSiblingsTransitiveClosure(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := newPublicSvc()

	hub := createWork(t, "シリーズ中枢")
	l1 := createWork(t, "シリーズ枝1")
	l2 := createWork(t, "シリーズ枝2")
	l3 := createWork(t, "シリーズ枝3")
	lone := createWork(t, "無関係作品")
	createSameSeriesEdge(t, hub.ID, l1.ID)
	createSameSeriesEdge(t, hub.ID, l2.ID)
	createSameSeriesEdge(t, hub.ID, l3.ID)

	ids := func(bs []dto.PublicWorkBrief) map[int64]bool {
		m := map[int64]bool{}
		for _, b := range bs {
			m[b.ID] = true
		}
		return m
	}

	rec, found, err := svc.WorkDetail(ctx, l1.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("leaf detail: found=%v err=%v", found, err)
	}
	got := ids(rec.SeriesSiblings)
	if len(got) != 3 || !got[hub.ID] || !got[l2.ID] || !got[l3.ID] || got[l1.ID] {
		t.Fatalf("leaf l1 siblings = %v (want hub,l2,l3; not self)", got)
	}

	recH, _, err := svc.WorkDetail(ctx, hub.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("hub detail: %v", err)
	}
	if gh := ids(recH.SeriesSiblings); len(gh) != 3 || !gh[l1.ID] || !gh[l2.ID] || !gh[l3.ID] {
		t.Fatalf("hub siblings = %v (want l1,l2,l3)", gh)
	}

	recL, _, err := svc.WorkDetail(ctx, lone.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("lone detail: %v", err)
	}
	if recL.SeriesSiblings == nil || len(recL.SeriesSiblings) != 0 {
		t.Fatalf("lone siblings = %v (want empty non-nil)", recL.SeriesSiblings)
	}
}

func siblingIDs(t *testing.T, workID int64) []int64 {
	t.Helper()
	rows, err := NewReadService(testDB).loadSeriesSiblings(t.Context(), workID)
	if err != nil {
		t.Fatalf("loadSeriesSiblings(%d): %v", workID, err)
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.WorkID)
	}
	return out
}

func TestSeriesSiblingsClosureShapes(t *testing.T) {
	cleanTables(t)

	t.Run("multi-hop chain", func(t *testing.T) {
		cleanTables(t)
		w := make([]*model.CatalogWork, 5)
		for i := range w {
			w[i] = createWork(t, "鎖"+string(rune('A'+i)))
		}
		for i := 0; i < len(w)-1; i++ {
			createSameSeriesEdge(t, w[i].ID, w[i+1].ID)
		}
		got := siblingIDs(t, w[4].ID)
		want := []int64{w[0].ID, w[1].ID, w[2].ID, w[3].ID}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tail siblings = %v (want %v, ascending by id)", got, want)
		}
		if got := siblingIDs(t, w[2].ID); len(got) != 4 {
			t.Fatalf("middle siblings = %v (want 4)", got)
		}
	})

	t.Run("cycle terminates", func(t *testing.T) {
		cleanTables(t)
		a := createWork(t, "環A")
		b := createWork(t, "環B")
		c := createWork(t, "環C")
		createSameSeriesEdge(t, a.ID, b.ID)
		createSameSeriesEdge(t, b.ID, c.ID)
		createSameSeriesEdge(t, c.ID, a.ID)
		createSameSeriesEdge(t, b.ID, a.ID)
		got := siblingIDs(t, a.ID)
		if want := []int64{b.ID, c.ID}; !reflect.DeepEqual(got, want) {
			t.Fatalf("cycle siblings = %v (want %v, no dups, no self)", got, want)
		}
	})

	t.Run("no edges", func(t *testing.T) {
		cleanTables(t)
		lone := createWork(t, "孤立作品")
		createWork(t, "無関係")
		if got := siblingIDs(t, lone.ID); len(got) != 0 {
			t.Fatalf("lone siblings = %v (want none)", got)
		}
		if got := siblingIDs(t, lone.ID+9999); len(got) != 0 {
			t.Fatalf("missing work siblings = %v (want none)", got)
		}
	})

	t.Run("soft-deleted node bridges but drops", func(t *testing.T) {
		cleanTables(t)
		a := createWork(t, "橋A")
		mid := createWork(t, "橋M")
		c := createWork(t, "橋C")
		createSameSeriesEdge(t, a.ID, mid.ID)
		createSameSeriesEdge(t, mid.ID, c.ID)
		if err := testDB.Delete(&model.CatalogWork{}, mid.ID).Error; err != nil {
			t.Fatalf("soft delete mid: %v", err)
		}
		if got := siblingIDs(t, a.ID); !reflect.DeepEqual(got, []int64{c.ID}) {
			t.Fatalf("siblings across deleted node = %v (want [%d])", got, c.ID)
		}
		if got := siblingIDs(t, mid.ID); !reflect.DeepEqual(got, []int64{a.ID, c.ID}) {
			t.Fatalf("deleted node's own siblings = %v (want [%d %d])", got, a.ID, c.ID)
		}
	})
}

func TestPublicCharacterLocalizedNames(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	ch := createCharacter(t, "美坂香里")
	createCharacterAlias(t, ch.ID, "美坂香里", "ja")
	createCharacterAlias(t, ch.ID, "美坂香裡", "ja")
	primary := &model.CatalogCharacterAlias{
		CharacterID: ch.ID, Name: "美坂香里", Lang: "zh-Hans",
		Kind: model.AliasKindTranslation, IsPrimaryForLocale: true,
	}
	if err := testDB.Create(primary).Error; err != nil {
		t.Fatalf("create zh primary: %v", err)
	}
	hint := &model.CatalogCharacterAlias{
		CharacterID: ch.ID, Name: "misaka-kaori-hint", Lang: "en",
		Kind: model.AliasKindSearchHint,
	}
	if err := testDB.Create(hint).Error; err != nil {
		t.Fatalf("create search hint: %v", err)
	}

	rec, found, err := svc.Character(ctx, ch.ID, false, false, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("character: found=%v err=%v", found, err)
	}

	if rec.DisplayName != "美坂香里" {
		t.Fatalf("display_name = %q", rec.DisplayName)
	}
	if len(rec.Aliases) != 1 || rec.Aliases[0].Value != "美坂香裡" || rec.Aliases[0].Lang != "ja" {
		t.Fatalf("aliases = %+v (want the ja variant only)", rec.Aliases)
	}
	zh, ok := rec.Localized["zh-Hans"]
	if !ok || zh.Value != "美坂香里" || zh.Kind != "translation" {
		t.Fatalf("localized[zh-Hans] = %+v (ok=%v), want the sourced zh name", zh, ok)
	}
	if _, leaked := rec.Localized["en"]; leaked {
		t.Fatal("a search-hint row must never reach localized{}")
	}
	for _, a := range rec.Aliases {
		if a.Value == "misaka-kaori-hint" {
			t.Fatal("a search-hint row must never reach aliases[]")
		}
	}

	bare := createCharacter(t, "無別名")
	rec, _, err = svc.Character(ctx, bare.ID, false, false, 0, 50, 0)
	if err != nil {
		t.Fatalf("bare character: %v", err)
	}
	if rec.Aliases == nil || len(rec.Aliases) != 0 {
		t.Fatalf("an alias-less character must serialize []: %#v", rec.Aliases)
	}
	if rec.Localized == nil || len(rec.Localized) != 0 {
		t.Fatalf("an alias-less character must serialize {}: %#v", rec.Localized)
	}
}

func TestPublicCharacterLocalizedMachineFill(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	mkAlias := func(characterID int64, name, lang string, kind, provenance int16) {
		t.Helper()
		a := &model.CatalogCharacterAlias{
			CharacterID: characterID, Name: name, Lang: lang,
			Kind: kind, Provenance: provenance,
		}
		if err := testDB.Create(a).Error; err != nil {
			t.Fatalf("create alias %q/%s: %v", name, lang, err)
		}
	}

	filled := createCharacter(t, "白河ことり")
	mkAlias(filled.ID, "白河ことり", "ja", model.AliasKindSpellingVariant, model.AliasProvenanceSource)
	mkAlias(filled.ID, "白河小鸟", "zh-Hans", model.AliasKindTranslation, model.AliasProvenanceMachine)

	rec, found, err := svc.Character(ctx, filled.ID, false, false, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("character: found=%v err=%v", found, err)
	}
	zh, ok := rec.Localized["zh-Hans"]
	if !ok {
		t.Fatalf("localized = %+v, want a machine zh-Hans fill-in", rec.Localized)
	}
	if zh.Value != "白河小鸟" || zh.Kind != "translation" || !zh.Machine {
		t.Fatalf("localized[zh-Hans] = %+v, want the seeded machine row flagged machine:true", zh)
	}
	if ja, ok := rec.Localized["ja"]; !ok || ja.Value != "白河ことり" || ja.Machine {
		t.Fatalf("localized[ja] = %+v (ok=%v), want the source row unflagged", ja, ok)
	}

	sourced := createCharacter(t, "神岸あかり")
	mkAlias(sourced.ID, "神岸明", "zh-Hans", model.AliasKindTranslation, model.AliasProvenanceSource)
	mkAlias(sourced.ID, "神岸朱里", "zh-Hans", model.AliasKindTranslation, model.AliasProvenanceMachine)

	rec, found, err = svc.Character(ctx, sourced.ID, false, false, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("sourced character: found=%v err=%v", found, err)
	}
	zh, ok = rec.Localized["zh-Hans"]
	if !ok || zh.Value != "神岸明" || zh.Machine {
		t.Fatalf("localized[zh-Hans] = %+v (ok=%v), a source row must beat the machine row", zh, ok)
	}
	if len(rec.Aliases) != 2 {
		t.Fatalf("aliases = %+v, want both zh spellings", rec.Aliases)
	}
	for _, a := range rec.Aliases {
		if a.Value == "神岸朱里" && !a.Machine {
			t.Fatalf("the shadowed machine spelling must stay flagged in aliases[]: %+v", a)
		}
	}
}

// TestPublicCharacterWorksCarryKindAndSpoiler guards the wave-10 addition: the
// S2S face has always rendered the roster strength on this list while the public
// one dropped it, so a person editing kind/spoiler was editing a value the
// public site (and the MCP tool that proxies it) could not show.
func TestPublicCharacterWorksCarryKindAndSpoiler(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	pub := newPublicSvc()

	rostered := createWork(t, "出演あり")
	creditOnly := createWork(t, "配音のみ")
	ch := createCharacter(t, "主人公")
	createWorkCharacter(t, rostered.ID, ch.ID, model.WorkCharacterKindSecondary, model.SpoilerSevere)
	cn := createCreditName(t, nil, "声優")
	createCredit(t, creditOnly.ID, cn.ID, roleByKey(t, "voice-actor"), &ch.ID)

	got, ok, err := pub.Character(ctx, ch.ID, true, true, 0, 50, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, got.Works, 2)

	byWork := map[int64]dto.PublicCharacterWork{}
	for _, w := range got.Works {
		byWork[w.Work.ID] = w
	}
	r := byWork[rostered.ID]
	assert.Equal(t, "secondary", r.Kind, "the public face names the kind, it does not number it")
	assert.EqualValues(t, model.SpoilerSevere, r.Spoiler)
	assert.Equal(t, editspec.RosterIdentity(ch.ID), r.Identity)

	c := byWork[creditOnly.ID]
	assert.Equal(t, "unknown", c.Kind, "no roster edge renders as unknown, never as an empty string")
	assert.EqualValues(t, model.SpoilerNone, c.Spoiler)
	assert.Empty(t, c.Identity)
}
