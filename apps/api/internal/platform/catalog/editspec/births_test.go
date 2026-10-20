package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func newBirthEngine(t *testing.T) *editing.Engine {
	t.Helper()
	for _, table := range []string{
		"edit_proposal_amendment", "edit_proposal", "edit_revision", "edit_suppressed_row", "edit_policy_override",
		"catalog_release", "catalog_character_alias", "catalog_character", "catalog_label_alias", "catalog_label",
		"catalog_name_alias", "catalog_person", "catalog_credit_name", "catalog_engine", "catalog_series", "catalog_work",
		"catalog_tag", "catalog_character_trait",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("register all: %v", err)
	}
	return editing.NewEngine(testDB, reg)
}

func TestCharacterBirthWaitsForReviewThenCreatesTheRow(t *testing.T) {
	e := newBirthEngine(t)
	prop, rev, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeCharacter,
		Patch:      map[string]any{editspec.FieldCharacterDisplayName: "新人物", editspec.FieldCharacterLang: "ja"},
		Actor:      realActor(100),
	})
	if err != nil {
		t.Fatalf("create birth: %v", err)
	}
	if rev != nil || prop.Status != editing.StatusOpen || prop.EntityID != 0 {
		t.Fatalf("a user's character birth must queue with entity_id 0: status %d entity %d rev %v", prop.Status, prop.EntityID, rev)
	}
	var n int64
	testDB.Model(&model.CatalogCharacter{}).Count(&n)
	if n != 0 {
		t.Fatalf("an open birth wrote %d rows", n)
	}

	rev, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "moderator"), "")
	if err != nil {
		t.Fatalf("merge birth: %v", err)
	}
	if rev.EntityID == 0 || rev.Seq != 1 || rev.Action != editing.ActionCreated {
		t.Fatalf("birth revision = %+v", rev)
	}
	var c model.CatalogCharacter
	if err := testDB.First(&c, rev.EntityID).Error; err != nil {
		t.Fatalf("born row: %v", err)
	}
	if c.DisplayName != "新人物" || c.Lang != "ja" {
		t.Fatalf("born row = %+v", c)
	}
	if got := provenance.FirstSource(c.FieldProvenance, "display_name"); !provenance.IsHuman(got) {
		t.Fatalf("display_name stamp = %q, want a human source", got)
	}
	var stored editing.Proposal
	testDB.First(&stored, prop.ID)
	if stored.EntityID != rev.EntityID || stored.Status != editing.StatusMerged {
		t.Fatalf("merged birth proposal = entity %d status %d, want %d merged", stored.EntityID, stored.Status, rev.EntityID)
	}
}

func TestDeclinedBirthCreatesNothing(t *testing.T) {
	e := newBirthEngine(t)
	prop, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeLabel,
		Patch:      map[string]any{editspec.FieldLabelName: "新ブランド"},
		Actor:      realActor(100),
	})
	if err != nil {
		t.Fatalf("create birth: %v", err)
	}
	if err := e.DeclineProposal(testCtx, prop.ID, realActor(200, "moderator"), "dup"); err != nil {
		t.Fatalf("decline: %v", err)
	}
	var n int64
	testDB.Model(&model.CatalogLabel{}).Count(&n)
	if n != 0 {
		t.Fatalf("a declined birth wrote %d label rows", n)
	}
}

func TestPlainUserCannotDecideABirth(t *testing.T) {
	e := newBirthEngine(t)
	prop, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeSeries,
		Patch:      map[string]any{editspec.FieldSeriesName: "新シリーズ"},
		Actor:      realActor(100),
	})
	if err != nil {
		t.Fatalf("create birth: %v", err)
	}
	var permErr *editing.PermissionError
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(101), ""); !errors.As(err, &permErr) {
		t.Fatalf("merge by a plain user: %v, want PermissionError", err)
	}
}

func TestEditorReleaseBirthAppliesAtOnceUnderItsWork(t *testing.T) {
	e := newBirthEngine(t)
	work := createWork(t, "親作品")
	editor := realActor(100, "editor")
	editor.TrustTier = editing.TrustedTier
	prop, rev, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeRelease,
		Params:     map[string]any{editspec.BirthParamWorkID: float64(work.ID)},
		Patch:      map[string]any{editspec.FieldReleaseKind: float64(0), editspec.FieldReleaseTitle: "初回限定版"},
		Actor:      editor,
	})
	if err != nil {
		t.Fatalf("create release birth: %v", err)
	}
	if rev == nil || prop.Status != editing.StatusMerged {
		t.Fatalf("a trusted editor's release birth must apply at once: status %d", prop.Status)
	}
	var r model.CatalogRelease
	if err := testDB.First(&r, rev.EntityID).Error; err != nil {
		t.Fatalf("born release: %v", err)
	}
	if r.WorkID != work.ID || r.Title == nil || *r.Title != "初回限定版" {
		t.Fatalf("born release = %+v", r)
	}
}

func TestReleaseBirthNeedsALiveWork(t *testing.T) {
	e := newBirthEngine(t)
	var valErr *editing.ValidationError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeRelease,
		Params:     map[string]any{editspec.BirthParamWorkID: float64(999999)},
		Patch:      map[string]any{editspec.FieldReleaseKind: float64(0)},
		Actor:      realActor(100),
	})
	if !errors.As(err, &valErr) || valErr.Key != editing.CreateFieldKey {
		t.Fatalf("release under a missing work: %v, want a ValidationError on %s", err, editing.CreateFieldKey)
	}
}

func TestPersonIsBornFromAnUnattributedCreditName(t *testing.T) {
	e := newBirthEngine(t)
	cn := model.CatalogCreditName{Name: "山田太郎"}
	if err := testDB.Create(&cn).Error; err != nil {
		t.Fatal(err)
	}
	mod := realActor(200, "moderator")
	prop, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypePerson,
		Params:     map[string]any{editspec.BirthParamCreditNameID: float64(cn.ID)},
		Actor:      realActor(100),
	})
	if err != nil {
		t.Fatalf("create person birth: %v", err)
	}
	rev, err := e.MergeProposal(testCtx, prop.ID, mod, "")
	if err != nil {
		t.Fatalf("merge person birth: %v", err)
	}
	var p model.CatalogPerson
	if err := testDB.First(&p, rev.EntityID).Error; err != nil {
		t.Fatalf("born person: %v", err)
	}
	if p.DisplayName != "山田太郎" || p.PrimaryCreditNameID == nil || *p.PrimaryCreditNameID != cn.ID {
		t.Fatalf("born person = %+v", p)
	}
	testDB.First(&cn, cn.ID)
	if cn.PersonID == nil || *cn.PersonID != p.ID {
		t.Fatalf("credit name not attributed to the new person: %+v", cn)
	}

	var valErr *editing.ValidationError
	_, _, err = e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypePerson,
		Params:     map[string]any{editspec.BirthParamCreditNameID: float64(cn.ID)},
		Actor:      realActor(100),
	})
	if !errors.As(err, &valErr) {
		t.Fatalf("a second person from an attributed name: %v, want ValidationError", err)
	}
}

func TestBirthDuplicatesFindTheExistingName(t *testing.T) {
	e := newBirthEngine(t)
	existing := model.CatalogCharacter{DisplayName: "Ｓａｋｕｒａ"}
	if err := testDB.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	hints, err := e.BirthDuplicates(testCtx, editspec.TypeCharacter, nil,
		map[string]any{editspec.FieldCharacterDisplayName: "sakura"})
	if err != nil {
		t.Fatalf("duplicates: %v", err)
	}
	if len(hints) != 1 || hints[0].ID != existing.ID {
		t.Fatalf("hints = %+v, want the NFKC-folded match %d", hints, existing.ID)
	}
}

func TestEngineBirthRefusesATakenName(t *testing.T) {
	e := newBirthEngine(t)
	if err := testDB.Create(&model.CatalogEngine{Name: "KiriKiri", Aliases: []byte(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	var valErr *editing.ValidationError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeEngine,
		Patch:      map[string]any{editspec.FieldEngineName: "kirikiri"},
		Actor:      realActor(100),
	})
	if !errors.As(err, &valErr) {
		t.Fatalf("engine with a taken name: %v, want ValidationError", err)
	}
}

func TestCreatePolicyOverrideClosesCreation(t *testing.T) {
	e := newBirthEngine(t)
	reg := e.Registry()
	locked := editing.ProposeLocked
	if errs := reg.SetPolicyOverrides([]editing.PolicyOverride{{
		EntityType: editspec.TypeCharacter, FieldKey: editing.CreateFieldKey, Propose: &locked,
	}}); len(errs) != 0 {
		t.Fatalf("override: %v", errs)
	}
	var permErr *editing.PermissionError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeCharacter,
		Patch:      map[string]any{editspec.FieldCharacterDisplayName: "閉鎖"},
		Actor:      realActor(100),
	})
	if !errors.As(err, &permErr) || permErr.Key != editing.CreateFieldKey {
		t.Fatalf("birth under a locked create policy: %v, want PermissionError on %s", err, editing.CreateFieldKey)
	}
	if err := reg.ValidateOverride(editing.PolicyOverride{EntityType: editspec.TypeWork, FieldKey: editing.CreateFieldKey, Propose: &locked}); err == nil {
		t.Fatal("works are not created through the engine, so a work create override must be refused")
	}
}

func TestBirthRequiresItsNameField(t *testing.T) {
	e := newBirthEngine(t)
	var valErr *editing.ValidationError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeCharacter,
		Patch:      map[string]any{editspec.FieldCharacterLang: "ja"},
		Actor:      realActor(100),
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldCharacterDisplayName {
		t.Fatalf("birth without display_name: %v", err)
	}
	if _, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeWork, Patch: map[string]any{editspec.FieldWorkDisplayName: "x"}, Actor: realActor(100),
	}); !errors.Is(err, editing.ErrNotCreatable) {
		t.Fatalf("work birth through the engine: %v, want ErrNotCreatable", err)
	}
}

func TestVocabularyBirthsAreReviewerOnly(t *testing.T) {
	e := newBirthEngine(t)
	var permErr *editing.PermissionError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeTag,
		Patch:      map[string]any{editspec.FieldTagName: "新タグ"},
		Actor:      realActor(100, "editor"),
	})
	if !errors.As(err, &permErr) || permErr.Key != editing.CreateFieldKey {
		t.Fatalf("an editor proposing a tag: %v, want PermissionError on %s", err, editing.CreateFieldKey)
	}

	prop, rev, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeTag,
		Patch:      map[string]any{editspec.FieldTagName: "新タグ"},
		Actor:      realActor(200, "moderator"),
	})
	if err != nil {
		t.Fatalf("a reviewer proposing a tag: %v", err)
	}
	if rev != nil {
		t.Fatal("a vocabulary birth waits for a second reviewer")
	}
	rev, err = e.MergeProposal(testCtx, prop.ID, realActor(201, "moderator"), "")
	if err != nil {
		t.Fatalf("merge tag birth: %v", err)
	}
	var tag model.CatalogTag
	if err := testDB.First(&tag, rev.EntityID).Error; err != nil || tag.Name != "新タグ" {
		t.Fatalf("born tag = %+v, %v", tag, err)
	}

	var valErr *editing.ValidationError
	_, _, err = e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeTag,
		Patch:      map[string]any{editspec.FieldTagName: "新タグ"},
		Actor:      realActor(200, "moderator"),
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldTagName {
		t.Fatalf("a second tag with the same name: %v, want ValidationError on the name", err)
	}
}

func TestTraitBirthNamesAreUniqueWithinAGroup(t *testing.T) {
	e := newBirthEngine(t)
	hair := model.CatalogCharacterTrait{VndbTID: "i1", Name: "Hair"}
	eyes := model.CatalogCharacterTrait{VndbTID: "i35", Name: "Eyes"}
	blondHair := model.CatalogCharacterTrait{VndbTID: "i2", Name: "Blond", GroupTID: "i1"}
	for _, row := range []*model.CatalogCharacterTrait{&hair, &eyes, &blondHair} {
		if err := testDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	mod := realActor(200, "moderator")
	var valErr *editing.ValidationError
	_, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeTrait,
		Patch:      map[string]any{editspec.FieldTraitName: "blond", editspec.FieldTraitGroup: float64(hair.ID)},
		Actor:      mod,
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldTraitName {
		t.Fatalf("a second Blond under Hair: %v, want ValidationError on the name", err)
	}

	prop, _, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeTrait,
		Patch:      map[string]any{editspec.FieldTraitName: "Blond", editspec.FieldTraitGroup: float64(eyes.ID)},
		Actor:      mod,
	})
	if err != nil {
		t.Fatalf("Blond under Eyes: %v", err)
	}
	rev, err := e.MergeProposal(testCtx, prop.ID, realActor(201, "moderator"), "")
	if err != nil {
		t.Fatalf("merge trait birth: %v", err)
	}
	var born model.CatalogCharacterTrait
	if err := testDB.First(&born, rev.EntityID).Error; err != nil {
		t.Fatal(err)
	}
	if born.GroupTID != "i35" || born.Name != "Blond" {
		t.Fatalf("born trait = %+v", born)
	}

	hints, err := e.BirthDuplicates(testCtx, editspec.TypeTrait, nil, map[string]any{editspec.FieldTraitName: "BLOND"})
	if err != nil || len(hints) != 2 {
		t.Fatalf("trait duplicate hints = %+v, %v; want both Blonds", hints, err)
	}
}
