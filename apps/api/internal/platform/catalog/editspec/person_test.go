package editspec_test

import (
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func newPersonEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newEngine(t)
	for _, table := range []string{"catalog_name_alias", "catalog_person_intro", "catalog_person"} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	if err := editspec.RegisterPerson(e.Registry(), testDB); err != nil {
		t.Fatalf("register catalog.person: %v", err)
	}
	if err := editspec.RegisterCreditName(e.Registry(), testDB); err != nil {
		t.Fatalf("register catalog.credit_name: %v", err)
	}
	return e
}

func createPersonCreditName(t *testing.T, name string, personID *int64) *model.CatalogCreditName {
	t.Helper()
	cn := &model.CatalogCreditName{Name: name, Lang: "ja", PersonID: personID}
	if err := testDB.Create(cn).Error; err != nil {
		t.Fatalf("create credit name: %v", err)
	}
	return cn
}

func createPersonWithName(t *testing.T, name string) (*model.CatalogPerson, *model.CatalogCreditName) {
	t.Helper()
	p := &model.CatalogPerson{DisplayName: name}
	if err := testDB.Create(p).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	cn := createPersonCreditName(t, name, &p.ID)
	if err := testDB.Model(p).Update("primary_credit_name_id", cn.ID).Error; err != nil {
		t.Fatal(err)
	}
	return p, cn
}

func reloadPerson(t *testing.T, id int64) model.CatalogPerson {
	t.Helper()
	var p model.CatalogPerson
	if err := testDB.First(&p, id).Error; err != nil {
		t.Fatalf("reload person %d: %v", id, err)
	}
	return p
}

func reloadCreditName(t *testing.T, id int64) model.CatalogCreditName {
	t.Helper()
	var cn model.CatalogCreditName
	if err := testDB.First(&cn, id).Error; err != nil {
		t.Fatalf("reload credit name %d: %v", id, err)
	}
	return cn
}

func proposeErr(t *testing.T, e *editing.Engine, entityType string, id int64, patch map[string]any) error {
	t.Helper()
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: id, Patch: patch, Actor: realActor(100),
	})
	if err != nil {
		return err
	}
	_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
	return err
}

func TestPersonScalarEditsStampEveryColumn(t *testing.T) {
	e := newPersonEngine(t)
	p, _ := createPersonWithName(t, "山田太郎")

	mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{
		editspec.FieldPersonGender:     float64(model.GenderMale),
		editspec.FieldPersonBirthYear:  float64(1980),
		editspec.FieldPersonBirthMonth: float64(4),
		editspec.FieldPersonBirthDay:   float64(1),
		editspec.FieldPersonPhoto:      "abc123",
	})
	got := reloadPerson(t, p.ID)
	if got.Gender == nil || *got.Gender != model.GenderMale || got.BirthY == nil || *got.BirthY != 1980 ||
		got.BirthM == nil || *got.BirthM != 4 || got.BirthD == nil || *got.BirthD != 1 || got.PhotoHash != "abc123" {
		t.Fatalf("person after merge: %+v", got)
	}
	for _, column := range []string{"gender", "birth_y", "birth_m", "birth_d", "photo_hash"} {
		if head := provenance.FirstSource(got.FieldProvenance, column); head != provenance.SourceCurated {
			t.Errorf("catalog_person.field_provenance[%s] head = %q, want curated", column, head)
		}
	}

	mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{
		editspec.FieldPersonBirthYear: nil,
		editspec.FieldPersonPhoto:     "",
	})
	cleared := reloadPerson(t, p.ID)
	if cleared.BirthY != nil || cleared.PhotoHash != "" {
		t.Fatalf("a null/empty edit must clear the column: %+v", cleared)
	}
	if head := provenance.FirstSource(cleared.FieldProvenance, "photo_hash"); head != provenance.SourceCurated {
		t.Fatalf("clearing the photo must stamp it human (head %q), or personphotos refills it", head)
	}
}

func TestPersonPrimaryNameRenamesThePerson(t *testing.T) {
	e := newPersonEngine(t)
	p, _ := createPersonWithName(t, "旧名義")
	other := createPersonCreditName(t, "新名義", &p.ID)
	stranger := createPersonCreditName(t, "他人", nil)

	mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{editspec.FieldPersonPrimaryName: float64(other.ID)})
	got := reloadPerson(t, p.ID)
	if got.PrimaryCreditNameID == nil || *got.PrimaryCreditNameID != other.ID || got.DisplayName != "新名義" {
		t.Fatalf("primary/display after edit: %+v", got)
	}

	err := proposeErr(t, e, editspec.TypePerson, p.ID, map[string]any{editspec.FieldPersonPrimaryName: float64(stranger.ID)})
	var verr *editing.ValidationError
	if !errors.As(err, &verr) || verr.Key != editspec.FieldPersonPrimaryName {
		t.Fatalf("a name of someone else must be refused, got %v", err)
	}
}

func TestPersonIntrosWriteOnlyTheHumanLane(t *testing.T) {
	e := newPersonEngine(t)
	p, _ := createPersonWithName(t, "作曲家")
	upstream := model.CatalogPersonIntro{PersonID: p.ID, Lang: "ja", Intro: "Bangumi の紹介", SourceID: 3}
	if err := testDB.Create(&upstream).Error; err != nil {
		t.Fatal(err)
	}
	intros := []any{map[string]any{"lang": "ja", "intro": "人手の紹介"}}
	snap := mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{editspec.FieldPersonIntros: intros})
	sameJSON(t, "person intros", snap[editspec.FieldPersonIntros], intros)

	var rows []model.CatalogPersonIntro
	if err := testDB.Where("person_id = ?", p.ID).Order("source_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Intro != "Bangumi の紹介" || rows[1].SourceID != 12 {
		t.Fatalf("the upstream row must stay and the curated row join it: %+v", rows)
	}
}

func TestPersonLinksCuratedLaneAndSuppression(t *testing.T) {
	e := newPersonEngine(t)
	p, _ := createPersonWithName(t, "イラストレーター")
	sources := map[string]int16{}
	var srcRows []model.CatalogSource
	if err := testDB.Find(&srcRows).Error; err != nil {
		t.Fatal(err)
	}
	for _, s := range srcRows {
		sources[s.Key] = s.ID
	}
	for _, r := range []model.CatalogExternalRef{
		{EntityType: model.EntityTypePerson, EntityID: p.ID, SourceID: sources["vndb"], ExternalID: "s42",
			LinkKind: model.LinkKindExact, MatchedBy: "import:person-mint"},
		{EntityType: model.EntityTypePerson, EntityID: p.ID, SourceID: sources["twitter"], ExternalID: "old_handle",
			LinkKind: model.LinkKindRelated, MatchedBy: "rule:vndb-extlink"},
	} {
		if err := testDB.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}

	links := []any{"https://bgm.tv/person/777", "https://www.pixiv.net/users/9"}
	snap := mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{editspec.FieldPersonLinks: links})
	sameJSON(t, "person links", snap[editspec.FieldPersonLinks], []any{"https://bgm.tv/person/777", "https://www.pixiv.net/users/9"})
	var bgm model.CatalogExternalRef
	if err := testDB.Where("entity_type = ? AND entity_id = ? AND source_id = ?",
		model.EntityTypePerson, p.ID, sources["bangumi"]).First(&bgm).Error; err != nil {
		t.Fatal(err)
	}
	if bgm.LinkKind != model.LinkKindProbable || bgm.MatchedBy != "curated" {
		t.Fatalf("a human may assert a person anchor only as probable: %+v", bgm)
	}

	err := proposeErr(t, e, editspec.TypePerson, p.ID, map[string]any{
		editspec.FieldPersonLinks: []any{"https://vndb.org/v17"},
	})
	if err == nil || !strings.Contains(err.Error(), "work page") {
		t.Fatalf("a work page is not a person link, got %v", err)
	}
	err = proposeErr(t, e, editspec.TypePerson, p.ID, map[string]any{
		editspec.FieldPersonLinks: []any{"https://x.com/old_handle"},
	})
	var verr *editing.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Reason, "upstream") {
		t.Fatalf("re-adding an upstream link must point at the suppression field, got %v", err)
	}

	hidden := editspec.PersonLinkIdentity("twitter", "old_handle")
	mergeOn(t, e, editspec.TypePerson, p.ID, map[string]any{editspec.FieldPersonLinksSuppr: []any{hidden}})
	var visible int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND `+editspec.NotSuppressedPersonLinkSQL("r", "src"),
		model.EntityTypePerson, p.ID).Scan(&visible).Error; err != nil {
		t.Fatal(err)
	}
	if visible != 3 {
		t.Fatalf("the suppressed related link must drop out and every other row stay, visible = %d", visible)
	}

	err = proposeErr(t, e, editspec.TypePerson, p.ID, map[string]any{
		editspec.FieldPersonLinksSuppr: []any{editspec.PersonLinkIdentity("vndb", "s42"), hidden},
	})
	if !errors.As(err, &verr) || verr.Key != editspec.FieldPersonLinksSuppr {
		t.Fatalf("hiding an identity anchor must be refused, got %v", err)
	}
}

func TestCreditNameScalarEditsAndRename(t *testing.T) {
	e := newPersonEngine(t)
	p, cn := createPersonWithName(t, "旧表記")

	mergeOn(t, e, editspec.TypeCreditName, cn.ID, map[string]any{
		editspec.FieldCreditNameName:           "  新表記​ ",
		editspec.FieldCreditNameLatin:          "Shin Hyouki",
		editspec.FieldCreditNameLang:           "zh-Hans",
		editspec.FieldCreditNameKind:           float64(model.CreditNameKindPenName),
		editspec.FieldCreditNameLinkVisibility: float64(model.LinkVisibilityHidden),
		editspec.FieldCreditNameNote:           "同人名義",
	})
	got := reloadCreditName(t, cn.ID)
	if got.Name != "新表記" || got.Latin == nil || *got.Latin != "Shin Hyouki" ||
		got.Kind != model.CreditNameKindPenName || got.LinkVisibility != model.LinkVisibilityHidden || got.Note != "同人名義" {
		t.Fatalf("credit name after merge: %+v", got)
	}
	for _, column := range []string{"name", "latin", "lang", "kind", "link_visibility", "note"} {
		if head := provenance.FirstSource(got.FieldProvenance, column); head != provenance.SourceCurated {
			t.Errorf("catalog_credit_name.field_provenance[%s] head = %q, want curated", column, head)
		}
	}
	if reloadPerson(t, p.ID).DisplayName != "新表記" {
		t.Fatal("renaming the primary credit name must rename the person")
	}
}

func TestCreditNamePersonAttribution(t *testing.T) {
	e := newPersonEngine(t)
	a, aMain := createPersonWithName(t, "Aさん")
	aAlt := createPersonCreditName(t, "A別名", &a.ID)
	b, bMain := createPersonWithName(t, "Bさん")
	orphan := createPersonCreditName(t, "孤児名義", nil)
	work := createWork(t, "クレジット用")
	if err := testDB.Create(&model.CatalogCredit{WorkID: work.ID, CreditNameID: aAlt.ID, RoleID: 1}).Error; err != nil {
		t.Fatal(err)
	}

	empty := &model.CatalogPerson{DisplayName: "未命名"}
	if err := testDB.Create(empty).Error; err != nil {
		t.Fatal(err)
	}
	mergeOn(t, e, editspec.TypeCreditName, orphan.ID, map[string]any{editspec.FieldCreditNamePerson: float64(empty.ID)})
	if got := reloadPerson(t, empty.ID); got.PrimaryCreditNameID == nil || *got.PrimaryCreditNameID != orphan.ID || got.DisplayName != "孤児名義" {
		t.Fatalf("attaching to a person without a primary makes the name primary: %+v", got)
	}

	mergeOn(t, e, editspec.TypeCreditName, aMain.ID, map[string]any{editspec.FieldCreditNamePerson: float64(b.ID)})
	if got := reloadCreditName(t, aMain.ID); got.PersonID == nil || *got.PersonID != b.ID {
		t.Fatalf("the name must move to B: %+v", got)
	}
	if got := reloadPerson(t, a.ID); got.PrimaryCreditNameID == nil || *got.PrimaryCreditNameID != aAlt.ID || got.DisplayName != "A別名" {
		t.Fatalf("A's primary moved away, so A re-elects its remaining name: %+v", got)
	}
	if got := reloadPerson(t, b.ID); *got.PrimaryCreditNameID != bMain.ID {
		t.Fatalf("B keeps its own primary: %+v", got)
	}
	if head := provenance.FirstSource(reloadCreditName(t, aMain.ID).FieldProvenance, "person_id"); head != provenance.SourceCurated {
		t.Fatalf("person_id stamp head = %q", head)
	}

	var verr *editing.ValidationError
	err := proposeErr(t, e, editspec.TypeCreditName, aAlt.ID, map[string]any{editspec.FieldCreditNamePerson: nil})
	if !errors.As(err, &verr) || !strings.Contains(verr.Reason, "only credit name") {
		t.Fatalf("detaching a person's last name must be refused, got %v", err)
	}
	err = proposeErr(t, e, editspec.TypeCreditName, bMain.ID, map[string]any{editspec.FieldCreditNamePerson: float64(99999)})
	if !errors.As(err, &verr) || !strings.Contains(verr.Reason, "does not exist") {
		t.Fatalf("an unknown person must be refused, got %v", err)
	}

	mergeOn(t, e, editspec.TypeCreditName, aMain.ID, map[string]any{editspec.FieldCreditNamePerson: nil})
	detached := reloadCreditName(t, aMain.ID)
	if detached.PersonID != nil {
		t.Fatalf("detach left person_id %v", *detached.PersonID)
	}
	if head := provenance.FirstSource(detached.FieldProvenance, "person_id"); head != provenance.SourceCurated {
		t.Fatalf("a human detach must be stamped so the link lanes leave it alone, head = %q", head)
	}
}

func TestCreditNameAliasesCuratedLaneAndSuppression(t *testing.T) {
	e := newPersonEngine(t)
	_, cn := createPersonWithName(t, "表記")
	bgm := int16(3)
	if err := testDB.Create(&model.CatalogNameAlias{
		CreditNameID: cn.ID, Name: "Hyouki", Lang: "en", Kind: model.AliasKindTranslation, SourceID: &bgm,
	}).Error; err != nil {
		t.Fatal(err)
	}

	aliases := []any{map[string]any{"name": "ひょうき", "lang": "ja", "kind": float64(model.AliasKindSpellingVariant)}}
	snap := mergeOn(t, e, editspec.TypeCreditName, cn.ID, map[string]any{editspec.FieldCreditNameAliases: aliases})
	sameJSON(t, "aliases", snap[editspec.FieldCreditNameAliases],
		[]any{map[string]any{"name": "ひょうき", "lang": "ja", "kind": float64(model.AliasKindSpellingVariant)}})

	err := proposeErr(t, e, editspec.TypeCreditName, cn.ID, map[string]any{
		editspec.FieldCreditNameAliases: []any{map[string]any{"name": "Hyouki", "lang": "en", "kind": float64(0)}},
	})
	var verr *editing.ValidationError
	if !errors.As(err, &verr) || verr.Key != editspec.FieldCreditNameAliases {
		t.Fatalf("an upstream alias cannot be re-added through the curated lane, got %v", err)
	}

	key := editspec.CharacterAliasIdentity(model.AliasKindTranslation, "en", "Hyouki")
	mergeOn(t, e, editspec.TypeCreditName, cn.ID, map[string]any{editspec.FieldCreditNameAliasesSuppr: []any{key}})
	var live []string
	if err := testDB.Raw(`SELECT a.name FROM catalog_name_alias a WHERE a.credit_name_id = ? AND `+
		editspec.NotSuppressedNameAliasSQL("a")+` ORDER BY a.name`, cn.ID).Scan(&live).Error; err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0] != "ひょうき" {
		t.Fatalf("the suppressed upstream alias must drop out of the read path: %v", live)
	}
}
