package editspec_test

import (
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/datatypes"
)

// importRosterEdge reproduces importer.insertRosterEdges: the row belongs to an
// importer, and no importer ever writes field_provenance.
func importRosterEdge(t *testing.T, workID, characterID int64, kind, spoiler int16) *model.CatalogWorkCharacter {
	t.Helper()
	e := &model.CatalogWorkCharacter{
		WorkID: workID, CharacterID: characterID, Kind: kind, Spoiler: spoiler,
		MatchedBy: "import:character-roster-vndb",
	}
	if err := testDB.Create(e).Error; err != nil {
		t.Fatalf("import roster edge: %v", err)
	}
	return e
}

func rosterProv(t *testing.T, rowID int64) datatypes.JSON {
	t.Helper()
	var row model.CatalogWorkCharacter
	if err := testDB.Select("id", "field_provenance").First(&row, rowID).Error; err != nil {
		t.Fatalf("reload roster row %d: %v", rowID, err)
	}
	return row.FieldProvenance
}

func rosterEl(characterID int64, kind, spoiler int16) map[string]any {
	return map[string]any{
		"character_id": float64(characterID), "kind": float64(kind), "spoiler": float64(spoiler),
	}
}

func reloadRosterEdge(t *testing.T, rowID int64) model.CatalogWorkCharacter {
	t.Helper()
	var row model.CatalogWorkCharacter
	if err := testDB.First(&row, rowID).Error; err != nil {
		t.Fatalf("reload roster row %d: %v", rowID, err)
	}
	return row
}

func rosterKeyCheckFor(t *testing.T) func(string) error {
	t.Helper()
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	spec, ok := reg.Type(editspec.TypeWork)
	if !ok {
		t.Fatalf("%s is not registered", editspec.TypeWork)
	}
	f, ok := spec.Field(editspec.FieldWorkRoster)
	if !ok {
		t.Fatalf("%s has no field %s", editspec.TypeWork, editspec.FieldWorkRoster)
	}
	if f.Identity == nil || f.Identity.KeyCheck == nil {
		t.Fatalf("%s declares no identity KeyCheck", editspec.FieldWorkRoster)
	}
	return f.Identity.KeyCheck
}

func TestRosterIdentityGoAndSQLAgree(t *testing.T) {
	newEngine(t)
	work := createWork(t, "同一キー")
	var want []string
	for _, name := range []string{"主人公", "ヒロイン", "友人"} {
		ch := createCharacter(t, name)
		importRosterEdge(t, work.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)
		want = append(want, editspec.RosterIdentity(ch.ID))
	}
	var got []string
	if err := testDB.Raw(`SELECT `+editspec.RosterIdentitySQL("wc")+` FROM catalog_work_character wc
		WHERE wc.work_id = ? ORDER BY wc.character_id`, work.ID).Scan(&got).Error; err != nil {
		t.Fatalf("recompute the key in SQL: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("SQL produced %d keys, Go produced %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d: SQL %q, Go %q", i, got[i], want[i])
		}
	}
}

func TestRosterKeyCheckRejectsMalformed(t *testing.T) {
	check := rosterKeyCheckFor(t)
	if err := check(editspec.RosterIdentity(42)); err != nil {
		t.Fatalf("the field's own key must pass its own check: %v", err)
	}
	for _, bad := range []string{
		"", "roster", "roster:", "roster:1:2", "credit:1", "1", ":1",
		"roster:0", "roster:-1", "roster:01", "roster:+1", "roster: 1", "roster:1 ",
		"roster:abc", "roster:1.0", "roster:٤٢",
	} {
		if err := check(bad); err == nil {
			t.Errorf("%q was accepted as an identity key", bad)
		}
	}
}

// TestProvenanceRowsResolverRunsBeforeApply is the wave's signature test. The
// resolver returns the rows whose stored value differs from the submitted one,
// so running it after Apply returns the empty set on every edit: no stamp is
// written and nothing reports an error. Move the resolver call in
// editing.ApplyField below f.Apply and this goes red.
func TestProvenanceRowsResolverRunsBeforeApply(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "花名册")
	ch := createCharacter(t, "主人公")
	edge := importRosterEdge(t, work.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	mergeField(t, e, work.ID, editspec.FieldWorkRoster, []any{
		rosterEl(ch.ID, model.WorkCharacterKindSecondary, model.SpoilerNone),
	})

	if got := reloadRosterEdge(t, edge.ID).Kind; got != model.WorkCharacterKindSecondary {
		t.Fatalf("kind = %d, want the submitted value", got)
	}
	if got := provenance.FirstSource(rosterProv(t, edge.ID), "kind"); got != provenance.SourceCurated {
		t.Fatalf("field_provenance[kind] head = %q, want %q — the row the edit changed carries no human stamp",
			got, provenance.SourceCurated)
	}
}

func TestProvenanceStampsOnlyChangedRows(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "花名册")
	a := createCharacter(t, "A")
	b := createCharacter(t, "B")
	c := createCharacter(t, "C")
	ea := importRosterEdge(t, work.ID, a.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	eb := importRosterEdge(t, work.ID, b.ID, model.WorkCharacterKindSecondary, model.SpoilerSevere)
	ec := importRosterEdge(t, work.ID, c.ID, model.WorkCharacterKindAppears, model.SpoilerNone)

	// The edit face echoes the whole roster back; only B's spoiler moves.
	mergeField(t, e, work.ID, editspec.FieldWorkRoster, []any{
		rosterEl(a.ID, model.WorkCharacterKindMain, model.SpoilerNone),
		rosterEl(b.ID, model.WorkCharacterKindSecondary, model.SpoilerNone),
		rosterEl(c.ID, model.WorkCharacterKindAppears, model.SpoilerNone),
	})

	if got := provenance.FirstSource(rosterProv(t, eb.ID), "spoiler"); got != provenance.SourceCurated {
		t.Errorf("B spoiler stamp = %q, want %q", got, provenance.SourceCurated)
	}
	if got := provenance.FirstSource(rosterProv(t, eb.ID), "kind"); got != "" {
		t.Errorf("B kind stamp = %q; kind did not change, so nobody claimed it", got)
	}
	for _, row := range []struct {
		name string
		id   int64
	}{{"A", ea.ID}, {"C", ec.ID}} {
		doc := rosterProv(t, row.id)
		if string(doc) != "{}" {
			t.Errorf("%s field_provenance = %s, want {} — resubmitting an unchanged row is not an assertion about it",
				row.name, doc)
		}
	}
}

func TestApplyRosterPatchesOnlyNamedRows(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "部分編集")
	a, b := createCharacter(t, "A"), createCharacter(t, "B")
	ea := importRosterEdge(t, work.ID, a.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	eb := importRosterEdge(t, work.ID, b.ID, model.WorkCharacterKindAppears, model.SpoilerSevere)

	mergeField(t, e, work.ID, editspec.FieldWorkRoster, []any{
		rosterEl(a.ID, model.WorkCharacterKindSecondary, model.SpoilerMild),
	})

	got := reloadRosterEdge(t, ea.ID)
	if got.Kind != model.WorkCharacterKindSecondary || got.Spoiler != model.SpoilerMild {
		t.Fatalf("named row = kind %d spoiler %d, want the submitted values", got.Kind, got.Spoiler)
	}
	untouched := reloadRosterEdge(t, eb.ID)
	if untouched.Kind != model.WorkCharacterKindAppears || untouched.Spoiler != model.SpoilerSevere {
		t.Fatalf("an unnamed row was rewritten: %+v", untouched)
	}
	if string(rosterProv(t, eb.ID)) != "{}" {
		t.Errorf("an unnamed row was stamped: %s", rosterProv(t, eb.ID))
	}
	var rows int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_work_character WHERE work_id = ?`, work.ID).
		Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("rows = %d, want 2 — an absent element is not a deletion", rows)
	}
}

func TestApplyRosterRejectsUnknownCharacter(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "参照テスト")
	onRoster := createCharacter(t, "出演")
	importRosterEdge(t, work.ID, onRoster.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	elsewhere := createCharacter(t, "他作品のキャラ")
	other := createWork(t, "別作品")
	importRosterEdge(t, other.ID, elsewhere.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	for _, c := range []struct {
		name        string
		characterID int64
	}{
		{"NoSuchCharacter", 9_000_001},
		{"OnAnotherWorksRoster", elsewhere.ID},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := testDB.Exec(`DELETE FROM edit_proposal`).Error; err != nil {
				t.Fatalf("clear proposals: %v", err)
			}
			prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
				EntityType: editspec.TypeWork, EntityID: work.ID,
				Patch: map[string]any{editspec.FieldWorkRoster: []any{
					rosterEl(c.characterID, model.WorkCharacterKindMain, model.SpoilerNone),
				}},
				Actor: realActor(100),
			})
			if err != nil {
				t.Fatalf("propose: %v", err)
			}
			var valErr *editing.ValidationError
			if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); !errors.As(err, &valErr) {
				t.Fatalf("merge returned %v, want a ValidationError (422) rather than a raw foreign-key 500", err)
			}
			if !strings.Contains(valErr.Reason, editspec.FieldWorkRosterSuppr) {
				t.Fatalf("the 422 must point at %s, got %q", editspec.FieldWorkRosterSuppr, valErr.Reason)
			}
		})
	}
}

func TestApplyRosterRejectsDuplicateCharacter(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "重複要素")
	ch := createCharacter(t, "主人公")
	importRosterEdge(t, work.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	// Not deduplicated the way credits are: the two elements carry DIFFERENT
	// kinds, so folding them would be picking one of the proposer's two answers.
	_, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkRoster: []any{
			rosterEl(ch.ID, model.WorkCharacterKindMain, model.SpoilerNone),
			rosterEl(ch.ID, model.WorkCharacterKindAppears, model.SpoilerNone),
		}},
		Actor: realActor(100),
	})
	var valErr *editing.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("propose returned %v, want a ValidationError (422)", err)
	}
	if !strings.Contains(valErr.Reason, "twice") {
		t.Fatalf("the 422 must say the character appears twice: %q", valErr.Reason)
	}
}

// TestApplyRosterNeverInserts is structural, not behavioural: the field offers
// no way to add or remove an edge, because one edge is the input of the vndb
// attach channel and creating one here would mint identity anchors from the
// content write face.
func TestApplyRosterNeverInserts(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "行数不変")
	a, b := createCharacter(t, "A"), createCharacter(t, "B")
	importRosterEdge(t, work.ID, a.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	importRosterEdge(t, work.ID, b.ID, model.WorkCharacterKindAppears, model.SpoilerNone)

	count := func() int64 {
		t.Helper()
		var n int64
		if err := testDB.Raw(`SELECT count(*) FROM catalog_work_character`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()

	mergeField(t, e, work.ID, editspec.FieldWorkRoster, []any{
		rosterEl(a.ID, model.WorkCharacterKindSecondary, model.SpoilerNone),
	})
	if got := count(); got != before {
		t.Fatalf("rows = %d, want %d — a partial submission must not delete", got, before)
	}

	mergeField(t, e, work.ID, editspec.FieldWorkRoster, []any{})
	if got := count(); got != before {
		t.Fatalf("rows = %d after an empty submission, want %d", got, before)
	}
}

func TestRosterSnapshotCarriesEveryPhysicalRow(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "スナップショット")
	a, b := createCharacter(t, "A"), createCharacter(t, "B")
	importRosterEdge(t, work.ID, a.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	importRosterEdge(t, work.ID, b.ID, model.WorkCharacterKindAppears, model.SpoilerSevere)

	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sameJSON(t, editspec.FieldWorkRoster, snap[editspec.FieldWorkRoster], []any{
		map[string]any{"character_id": a.ID, "kind": model.WorkCharacterKindMain, "spoiler": model.SpoilerNone},
		map[string]any{"character_id": b.ID, "kind": model.WorkCharacterKindAppears, "spoiler": model.SpoilerSevere},
	})
}
