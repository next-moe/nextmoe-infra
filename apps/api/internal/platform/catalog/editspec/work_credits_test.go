package editspec_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

const upstreamSourceID = int16(2)

func createCreditName(t *testing.T, name string) *model.CatalogCreditName {
	t.Helper()
	cn := &model.CatalogCreditName{
		Name: name, Lang: "ja",
		Kind: model.CreditNameKindMain, LinkVisibility: model.LinkVisibilityPublic,
	}
	if err := testDB.Create(cn).Error; err != nil {
		t.Fatalf("create credit name %q: %v", name, err)
	}
	return cn
}

func roleIDByKey(t *testing.T, key string) int64 {
	t.Helper()
	var id int64
	if err := testDB.Raw(`SELECT id FROM catalog_role WHERE key = ?`, key).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("seeded role %q: id=%d err=%v", key, id, err)
	}
	return id
}

// importCredit reproduces importer.insertCredits: the row belongs to the
// importer and carries its source, never the curated lane.
func importCredit(t *testing.T, workID, creditNameID, roleID int64, characterID *int64) int64 {
	t.Helper()
	src := upstreamSourceID
	row := model.CatalogCredit{
		WorkID: workID, CreditNameID: creditNameID, RoleID: roleID,
		CharacterID: characterID, Spoiler: model.SpoilerNone, SourceID: &src,
	}
	if err := testDB.Create(&row).Error; err != nil {
		t.Fatalf("import credit: %v", err)
	}
	return row.ID
}

func creditsSnapshot(t *testing.T, e *editing.Engine, workID int64) []any {
	t.Helper()
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, workID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	raw, ok := snap[editspec.FieldWorkCredits].([]any)
	if !ok {
		t.Fatalf("snapshot %s = %#v, want a list", editspec.FieldWorkCredits, snap[editspec.FieldWorkCredits])
	}
	return raw
}

func TestCreditsLaneRoundTrip(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "署名テスト")
	scenario, va := roleIDByKey(t, "scenario"), roleIDByKey(t, "voice-actor")
	writer := createCreditName(t, "脚本家")
	actor := createCreditName(t, "声優")
	ch := createCharacter(t, "主人公")

	upstreamID := importCredit(t, work.ID, writer.ID, scenario, nil)

	editor := realActor(100)
	reviewer := realActor(200, "ren")
	list := []any{
		map[string]any{"role_id": float64(va), "credit_name_id": float64(actor.ID),
			"character_id": float64(ch.ID), "note": "追加キャスト"},
	}
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCredits: list}, Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose credits: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, "ok"); err != nil {
		t.Fatalf("merge credits: %v", err)
	}

	var rows []model.CatalogCredit
	if err := testDB.Where("work_id = ?", work.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("reload credits: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the upstream row plus one curated row", len(rows))
	}
	if rows[0].ID != upstreamID || rows[0].SourceID == nil || *rows[0].SourceID != upstreamSourceID {
		t.Fatalf("the upstream row was touched: %+v", rows[0])
	}
	curated := rows[1]
	if curated.SourceID == nil || *curated.SourceID != 12 {
		t.Fatalf("curated row source = %v, want 12", curated.SourceID)
	}
	if curated.RoleID != va || curated.CreditNameID != actor.ID ||
		curated.CharacterID == nil || *curated.CharacterID != ch.ID || curated.Note != "追加キャスト" {
		t.Fatalf("curated row = %+v", curated)
	}

	// The snapshot shows the curated lane only — the upstream row is the
	// importer's and is not this field's value.
	sameJSON(t, editspec.FieldWorkCredits, creditsSnapshot(t, e, work.ID), []any{
		map[string]any{"role_id": va, "credit_name_id": actor.ID, "character_id": ch.ID, "note": "追加キャスト"},
	})

	// A replace that drops the element removes the curated row and only it.
	prop2, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCredits: []any{}}, Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose empty credits: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop2.ID, reviewer, "ok"); err != nil {
		t.Fatalf("merge empty credits: %v", err)
	}
	rows = nil
	if err := testDB.Where("work_id = ?", work.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("reload credits: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != upstreamID {
		t.Fatalf("clearing the curated lane must leave the upstream row: %+v", rows)
	}
}

func TestCreditsUpstreamCollisionRejected(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "衝突テスト")
	va := roleIDByKey(t, "voice-actor")
	actor := createCreditName(t, "声優")
	ch := createCharacter(t, "主人公")
	importCredit(t, work.ID, actor.ID, va, &ch.ID)

	clashing := []any{
		map[string]any{"role_id": float64(va), "credit_name_id": float64(actor.ID),
			"character_id": float64(ch.ID)},
	}
	clash, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCredits: clashing}, Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	// Same shape as the alias lane: the collision is a fact about the table, so
	// it is caught where the rows are written rather than by the pure validator.
	var valErr *editing.ValidationError
	_, err = e.MergeProposal(testCtx, clash.ID, realActor(200, "ren"), "")
	if !errors.As(err, &valErr) {
		t.Fatalf("re-adding an upstream credit returned %v, want a ValidationError (422)", err)
	}
	if !strings.Contains(valErr.Reason, editspec.FieldWorkCreditsSuppr) {
		t.Fatalf("the 422 must point at %s, got %q", editspec.FieldWorkCreditsSuppr, valErr.Reason)
	}
	if !strings.Contains(valErr.Reason, "声優") || !strings.Contains(valErr.Reason, "主人公") {
		t.Fatalf("the 422 must name the row it collided with: %q", valErr.Reason)
	}

	// The same tuple on a role the importer did not use is not a collision.
	free := []any{
		map[string]any{"role_id": float64(roleIDByKey(t, "scenario")),
			"credit_name_id": float64(actor.ID), "character_id": float64(ch.ID)},
	}
	if err := testDB.Exec(`DELETE FROM edit_proposal`).Error; err != nil {
		t.Fatalf("clear proposals: %v", err)
	}
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCredits: free}, Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("a free tuple must be accepted: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); err != nil {
		t.Fatalf("merge free tuple: %v", err)
	}
}

func TestCreditsRejectUnknownReferences(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "参照テスト")
	scenario := roleIDByKey(t, "scenario")
	writer := createCreditName(t, "脚本家")
	ch := createCharacter(t, "主人公")

	// A deprecated role must be refused as well: the vocabulary retired it, and
	// letting a person newly assign one puts it back into circulation by hand.
	deprecated := roleIDByKey(t, "voice-actor")
	if err := testDB.Exec(`UPDATE catalog_role SET is_deprecated = true WHERE id = ?`, deprecated).Error; err != nil {
		t.Fatalf("deprecate role: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`UPDATE catalog_role SET is_deprecated = false WHERE id = ?`, deprecated)
	})

	el := func(role, name, character int64) []any {
		obj := map[string]any{"role_id": float64(role), "credit_name_id": float64(name)}
		if character != 0 {
			obj["character_id"] = float64(character)
		}
		return []any{obj}
	}
	for _, c := range []struct {
		name  string
		value []any
		want  string
	}{
		{"UnknownRole", el(9_000_001, writer.ID, 0), "no role has id 9000001"},
		{"UnknownCreditName", el(scenario, 9_000_002, 0), "no credited name has id 9000002"},
		{"UnknownCharacter", el(scenario, writer.ID, 9_000_003), "no character has id 9000003"},
		{"DeprecatedRole", el(deprecated, writer.ID, ch.ID), fmt.Sprintf("role %d is deprecated", deprecated)},
	} {
		t.Run(c.name, func(t *testing.T) {
			prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
				EntityType: editspec.TypeWork, EntityID: work.ID,
				Patch: map[string]any{editspec.FieldWorkCredits: c.value}, Actor: realActor(100),
			})
			if err != nil {
				t.Fatalf("propose: %v", err)
			}
			var valErr *editing.ValidationError
			if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); !errors.As(err, &valErr) {
				t.Fatalf("merge returned %v, want a ValidationError (422) rather than a raw 23503", err)
			}
			if valErr.Key != editspec.FieldWorkCredits {
				t.Fatalf("the 422 must name the field, got %q", valErr.Key)
			}
			if !strings.Contains(valErr.Reason, c.want) {
				t.Fatalf("the 422 must name the offending id: want %q in %q", c.want, valErr.Reason)
			}
			var rows int64
			if err := testDB.Model(&model.CatalogCredit{}).Where("work_id = ?", work.ID).
				Count(&rows).Error; err != nil {
				t.Fatalf("count credits: %v", err)
			}
			if rows != 0 {
				t.Fatalf("a rejected apply must write nothing, found %d rows", rows)
			}
			if err := testDB.Exec(`DELETE FROM edit_proposal`).Error; err != nil {
				t.Fatalf("clear proposals: %v", err)
			}
		})
	}

	// A soft-deleted character is gone as far as every read face is concerned,
	// so assigning one is the same mistake as assigning an id that never existed.
	if err := testDB.Delete(&model.CatalogCharacter{}, ch.ID).Error; err != nil {
		t.Fatalf("soft-delete character: %v", err)
	}
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCredits: el(scenario, writer.ID, ch.ID)},
		Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	var valErr *editing.ValidationError
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); !errors.As(err, &valErr) {
		t.Fatalf("a merged-away character was accepted: %v", err)
	}
}

func TestCreditIdentitySQLMatchesGo(t *testing.T) {
	newEngine(t)
	work := createWork(t, "SQL 対 Go")
	scenario, va := roleIDByKey(t, "scenario"), roleIDByKey(t, "voice-actor")
	writer, actor := createCreditName(t, "脚本家"), createCreditName(t, "声優")
	ch := createCharacter(t, "主人公")

	importCredit(t, work.ID, writer.ID, scenario, nil)
	importCredit(t, work.ID, actor.ID, va, &ch.ID)
	importCredit(t, work.ID, actor.ID, va, nil)
	importCredit(t, work.ID, actor.ID, scenario, &ch.ID)

	var got []struct {
		ID           int64  `gorm:"column:id"`
		RoleID       int64  `gorm:"column:role_id"`
		CreditNameID int64  `gorm:"column:credit_name_id"`
		CharacterID  *int64 `gorm:"column:character_id"`
		Key          string `gorm:"column:key"`
	}
	if err := testDB.Raw(`SELECT c.id, c.role_id, c.credit_name_id, c.character_id, ` +
		editspec.CreditIdentitySQL("c") + ` AS key
		FROM catalog_credit c WHERE c.work_id = ` + fmt.Sprint(work.ID) + ` ORDER BY c.id`).
		Scan(&got).Error; err != nil {
		t.Fatalf("compute keys in SQL: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("rows = %d, want 4", len(got))
	}
	for _, r := range got {
		var charID int64
		if r.CharacterID != nil {
			charID = *r.CharacterID
		}
		if want := editspec.CreditIdentity(r.RoleID, r.CreditNameID, charID); r.Key != want {
			t.Fatalf("row %d: SQL key %q, Go key %q", r.ID, r.Key, want)
		}
	}

	target := got[2] // the NULL-character row: its key must carry the 0 sentinel
	if !strings.HasSuffix(target.Key, ":0") {
		t.Fatalf("a NULL character must render as the 0 sentinel, got %q", target.Key)
	}
	if err := testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: work.ID, FieldKey: editspec.FieldWorkCredits,
		IdentityKey: target.Key,
	}).Error; err != nil {
		t.Fatalf("suppress: %v", err)
	}
	var surviving []int64
	if err := testDB.Raw(`SELECT c.id FROM catalog_credit c
		WHERE c.work_id = ` + fmt.Sprint(work.ID) + ` AND ` +
		editspec.NotSuppressedCreditSQL("c") + ` ORDER BY c.id`).Scan(&surviving).Error; err != nil {
		t.Fatalf("apply predicate: %v", err)
	}
	if len(surviving) != 3 {
		t.Fatalf("predicate kept %d rows, want 3", len(surviving))
	}
	for _, id := range surviving {
		if id == target.ID {
			t.Fatalf("row %d is suppressed but survived the predicate", id)
		}
	}
}

func TestCreditSuppressionKeyValidation(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	spec, _ := reg.Type(editspec.TypeWork)
	companion, ok := spec.Field(editspec.FieldWorkCreditsSuppr)
	if !ok {
		t.Fatalf("%s is not registered", editspec.FieldWorkCreditsSuppr)
	}
	for _, c := range []struct {
		why  string
		keys []any
	}{
		{"the wrong prefix", []any{"title:1:2:0"}},
		{"too few segments", []any{"credit:1:2"}},
		{"a non-numeric segment", []any{"credit:1:two:0"}},
		{"a leading zero", []any{"credit:1:02:0"}},
		{"a zero role", []any{"credit:0:2:0"}},
		{"a negative id", []any{"credit:1:-2:0"}},
	} {
		if err := companion.Validate(c.keys); err == nil {
			t.Fatalf("%s was accepted", c.why)
		}
	}
	if err := companion.Validate([]any{editspec.CreditIdentity(1, 2, 0)}); err != nil {
		t.Fatalf("the field's own helper must produce a key the field accepts: %v", err)
	}
}

// TestIdentityFollowStmtsCoverEveryDeclaredRef is the engine test of the same
// name run against the REAL registry. Until catalog.work.credits existed no
// registered field declared Refs, so every merge's follow step produced zero
// statements and the engine's version of this test was proving a property of a
// synthetic spec only.
func TestIdentityFollowStmtsCoverEveryDeclaredRef(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	// A tag is followed once per field that names it, so registering a second
	// field with the same tag adds a move/drop pair with nobody editing the
	// merge machine: catalog.work.roster (R2c-2) is TagCharacter's second user.
	for _, c := range []struct {
		tag    string
		fields []string
	}{
		{editspec.TagCreditName, []string{editspec.FieldWorkCredits}},
		{editspec.TagCharacter, []string{editspec.FieldWorkCredits, editspec.FieldWorkRoster}},
	} {
		stmts := reg.IdentityFollowStmts(c.tag, 7, 8, nil)
		if len(stmts) != 2*len(c.fields) {
			t.Fatalf("tag %q produced %d statements, want %d (one move + one drop per field in %v)",
				c.tag, len(stmts), 2*len(c.fields), c.fields)
		}
		for _, s := range stmts {
			var hasType bool
			var field string
			for _, a := range s.Args {
				str, _ := a.(string)
				hasType = hasType || str == editspec.TypeWork
				for _, f := range c.fields {
					if str == f {
						field = f
					}
				}
			}
			if !hasType || field == "" {
				t.Fatalf("tag %q produced a statement addressed to none of %s.%v: %s",
					c.tag, editspec.TypeWork, c.fields, s.SQL)
			}
		}
	}
	labelStmts := reg.IdentityFollowStmts(editspec.TagLabel, 7, 8, nil)
	if len(labelStmts) != 4 {
		t.Fatalf("tag %q produced %d statements, want the move + drop pairs of %s.%s and %s.%s", editspec.TagLabel, len(labelStmts),
			editspec.TypeLabel, editspec.FieldLabelRelations, editspec.TypeWork, editspec.FieldWorkLabels)
	}
	seen := map[string]int{}
	for _, s := range labelStmts {
		var typ, field string
		for _, a := range s.Args {
			switch str, _ := a.(string); str {
			case editspec.TypeLabel, editspec.TypeWork:
				typ = str
			case editspec.FieldLabelRelations, editspec.FieldWorkLabels:
				field = str
			}
		}
		seen[typ+"/"+field]++
	}
	if seen[editspec.TypeLabel+"/"+editspec.FieldLabelRelations] != 2 || seen[editspec.TypeWork+"/"+editspec.FieldWorkLabels] != 2 {
		t.Fatalf("tag %q statements by type/field = %v", editspec.TagLabel, seen)
	}
	for _, tag := range []string{editspec.TagWork, editspec.TagPerson} {
		if got := reg.IdentityFollowStmts(tag, 7, 8, nil); len(got) != 0 {
			t.Fatalf("tag %q is in no identity key but produced %d statements", tag, len(got))
		}
	}
}
