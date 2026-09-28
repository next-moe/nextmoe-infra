package editspec_test

import (
	"errors"
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func newReleaseEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newEngine(t)
	if err := testDB.Exec("TRUNCATE catalog_release RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate catalog_release: %v", err)
	}
	if err := editspec.RegisterRelease(e.Registry(), testDB); err != nil {
		t.Fatalf("register catalog.release: %v", err)
	}
	return e
}

func createReleaseOn(t *testing.T, workID int64) *model.CatalogRelease {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault, Extra: []byte(`{}`)}
	if err := testDB.Create(rel).Error; err != nil {
		t.Fatalf("create release: %v", err)
	}
	return rel
}

func reloadRelease(t *testing.T, id int64) model.CatalogRelease {
	t.Helper()
	var r model.CatalogRelease
	if err := testDB.Unscoped().First(&r, id).Error; err != nil {
		t.Fatalf("reload release %d: %v", id, err)
	}
	return r
}

func TestReleaseSchemaProjection(t *testing.T) {
	e := newReleaseEngine(t)
	spec, ok := e.Registry().Type(editspec.TypeRelease)
	if !ok {
		t.Fatal("catalog.release is not registered")
	}
	want := map[string]editing.FieldKind{
		editspec.FieldReleaseKind:     editing.KindEnum,
		editspec.FieldReleaseTitle:    editing.KindText,
		editspec.FieldReleaseLang:     editing.KindEnum,
		editspec.FieldReleasePlatform: editing.KindEnum,
		editspec.FieldReleaseEngine:   editing.KindRef,
		editspec.FieldReleaseReleased: editing.KindDate,
		editspec.FieldReleaseHidden:   editing.KindEnum,
	}
	if len(spec.Fields) != len(want) {
		t.Fatalf("fields = %d, want %d", len(spec.Fields), len(want))
	}
	for _, f := range spec.Fields {
		kind, ok := want[f.Key]
		if !ok {
			t.Errorf("unexpected field %s", f.Key)
			continue
		}
		if f.Kind != kind {
			t.Errorf("%s kind = %s, want %s", f.Key, f.Kind, kind)
		}
		if f.Validate == nil || f.Apply == nil {
			t.Errorf("%s missing Validate/Apply", f.Key)
		}
	}

	mustField := func(key string) *editing.FieldSpec {
		t.Helper()
		f, ok := spec.Field(key)
		if !ok {
			t.Fatalf("missing field %s", key)
		}
		return f
	}
	if err := mustField(editspec.FieldReleaseKind).Validate(float64(model.ReleaseKindPatch)); err != nil {
		t.Fatalf("kind 4 rejected: %v", err)
	}
	if err := mustField(editspec.FieldReleaseKind).Validate(nil); err == nil {
		t.Fatal("kind null must 422")
	}
	if err := mustField(editspec.FieldReleaseLang).Validate(nil); err != nil {
		t.Fatalf("lang null rejected: %v", err)
	}
	if err := mustField(editspec.FieldReleaseLang).Validate("ck"); err != nil {
		t.Fatalf("lang extra ck rejected: %v", err)
	}
	if err := mustField(editspec.FieldReleaseLang).Validate("ja"); err != nil {
		t.Fatalf("lang olang ja rejected: %v", err)
	}
	if err := mustField(editspec.FieldReleaseLang).Validate("xx"); err == nil {
		t.Fatal("lang xx must 422")
	}
	if err := mustField(editspec.FieldReleasePlatform).Validate("win"); err != nil {
		t.Fatalf("platform win rejected: %v", err)
	}
	if err := mustField(editspec.FieldReleasePlatform).Validate("pc"); err == nil {
		t.Fatal("platform pc (alias, not a stored code) must 422")
	}
	if err := mustField(editspec.FieldReleasePlatform).Validate(nil); err != nil {
		t.Fatalf("platform null rejected: %v", err)
	}
	if n := len(releasePlatformCodes(t, spec)); n != 47 {
		t.Fatalf("platform vocab size = %d, want 47", n)
	}
}

func releasePlatformCodes(t *testing.T, spec *editing.EntityTypeSpec) []string {
	t.Helper()
	f, ok := spec.Field(editspec.FieldReleasePlatform)
	if !ok {
		t.Fatal("platform field missing")
	}
	codes := []string{
		"and", "bdp", "dos", "drc", "dvd", "fm7", "fmt", "gba", "gbc", "ios",
		"lin", "mac", "mob", "msx", "n3d", "nds", "nes", "oth", "p88", "p98",
		"pce", "pcf", "ps1", "ps2", "ps3", "ps4", "ps5", "psp", "psv", "sat",
		"scd", "sfc", "smd", "sw2", "swi", "tdo", "vnd", "web", "wii", "win",
		"wiu", "x1s", "x68", "xb1", "xb3", "xbo", "xxs",
	}
	for _, c := range codes {
		if err := f.Validate(c); err != nil {
			t.Errorf("platform %q rejected: %v", c, err)
		}
	}
	return codes
}

func TestReleaseScalarEditsStampProvenance(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "発売日の主")
	rel := createReleaseOn(t, work.ID)
	title := "限定版"
	lang := "ck"
	plat := "swi"

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseKind:     float64(model.ReleaseKindPhysical),
		editspec.FieldReleaseTitle:    title,
		editspec.FieldReleaseLang:     lang,
		editspec.FieldReleasePlatform: plat,
	})
	if snap[editspec.FieldReleaseKind] != int64(model.ReleaseKindPhysical) {
		t.Fatalf("snapshot kind = %#v", snap[editspec.FieldReleaseKind])
	}
	if snap[editspec.FieldReleaseTitle] != title || snap[editspec.FieldReleaseLang] != lang ||
		snap[editspec.FieldReleasePlatform] != plat {
		t.Fatalf("snapshot scalars = %#v", snap)
	}

	got := reloadRelease(t, rel.ID)
	if got.Kind != model.ReleaseKindPhysical || got.Title == nil || *got.Title != title ||
		got.Lang == nil || *got.Lang != lang || got.Platform == nil || *got.Platform != plat {
		t.Fatalf("release after merge: %+v", got)
	}
	for _, column := range []string{"kind", "title", "lang", "platform"} {
		if head := provenance.FirstSource(got.FieldProvenance, column); head != provenance.SourceCurated {
			t.Errorf("catalog_release.field_provenance[%s] head = %q, want %q",
				column, head, provenance.SourceCurated)
		}
	}

	mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseTitle:    nil,
		editspec.FieldReleaseLang:     nil,
		editspec.FieldReleasePlatform: nil,
	})
	cleared := reloadRelease(t, rel.ID)
	if cleared.Title != nil || cleared.Lang != nil || cleared.Platform != nil {
		t.Fatalf("clearing nullable scalars left %+v", cleared)
	}
}

func TestReleaseDateComposite(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "日付の主")
	rel := createReleaseOn(t, work.ID)

	full := map[string]any{"y": float64(2020), "m": float64(6), "d": float64(14)}
	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseReleased: full,
	})
	sameJSON(t, "released", snap[editspec.FieldReleaseReleased], map[string]any{
		"y": int64(2020), "m": int64(6), "d": int64(14),
	})
	got := reloadRelease(t, rel.ID)
	if got.ReleasedY == nil || *got.ReleasedY != 2020 ||
		got.ReleasedM == nil || *got.ReleasedM != 6 ||
		got.ReleasedD == nil || *got.ReleasedD != 14 {
		t.Fatalf("atomic date write: %+v", got)
	}
	for _, column := range []string{"released_y", "released_m", "released_d"} {
		if head := provenance.FirstSource(got.FieldProvenance, column); head != provenance.SourceCurated {
			t.Errorf("date provenance[%s] = %q", column, head)
		}
	}

	snap = mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseReleased: nil,
	})
	if snap[editspec.FieldReleaseReleased] != nil {
		t.Fatalf("cleared date snapshot = %#v", snap[editspec.FieldReleaseReleased])
	}
	cleared := reloadRelease(t, rel.ID)
	if cleared.ReleasedY != nil || cleared.ReleasedM != nil || cleared.ReleasedD != nil {
		t.Fatalf("null date must clear all three: %+v", cleared)
	}

	editor := realActor(100, "admin")
	var valErr *editing.ValidationError
	for i, patch := range []any{
		map[string]any{"d": float64(1)},
		map[string]any{"y": float64(2020), "d": float64(1)},
		map[string]any{"y": float64(1949)},
		map[string]any{"y": float64(2201)},
		map[string]any{"y": float64(2020), "m": float64(0)},
		map[string]any{"y": float64(2020), "m": float64(13)},
		map[string]any{"y": float64(2020), "m": float64(6), "d": float64(0)},
		map[string]any{"y": float64(2020), "m": float64(6), "d": float64(32)},
		map[string]any{"y": float64(2020), "extra": float64(1)},
		"2020-06-14",
		float64(2020),
	} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeRelease, EntityID: rel.ID,
			Patch: map[string]any{editspec.FieldReleaseReleased: patch}, Actor: editor,
		}); !errors.As(err, &valErr) {
			t.Errorf("date case %d (%v): want ValidationError, got %v", i, patch, err)
		}
	}
}

func TestReleaseHiddenUnscopedAndIdempotent(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "隠蔽の主")
	rel := createReleaseOn(t, work.ID)

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseHidden: true,
	})
	if snap[editspec.FieldReleaseHidden] != true {
		t.Fatalf("hidden snapshot after hide = %#v", snap[editspec.FieldReleaseHidden])
	}
	hidden := reloadRelease(t, rel.ID)
	if !hidden.DeletedAt.Valid {
		t.Fatal("hide must set deleted_at")
	}
	if head := provenance.FirstSource(hidden.FieldProvenance, "deleted_at"); head != provenance.SourceCurated {
		t.Fatalf("hidden provenance = %q", head)
	}
	firstHide := hidden.DeletedAt.Time

	var scoped int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_release WHERE id = ? AND deleted_at IS NULL`, rel.ID).
		Scan(&scoped).Error; err != nil {
		t.Fatal(err)
	}
	if scoped != 0 {
		t.Fatal("a scoped count must not see the hidden row")
	}

	still, err := e.CurrentSnapshot(testCtx, editspec.TypeRelease, rel.ID)
	if err != nil {
		t.Fatalf("hidden release snapshot must still load: %v", err)
	}
	if still[editspec.FieldReleaseHidden] != true {
		t.Fatalf("unscoped snapshot hidden = %#v", still[editspec.FieldReleaseHidden])
	}

	spec, _ := e.Registry().Type(editspec.TypeRelease)
	hiddenField, ok := spec.Field(editspec.FieldReleaseHidden)
	if !ok {
		t.Fatal("hidden field missing")
	}
	time.Sleep(20 * time.Millisecond)
	if err := hiddenField.Apply(testCtx, testDB, rel.ID, true); err != nil {
		t.Fatalf("re-hide Apply: %v", err)
	}
	again := reloadRelease(t, rel.ID)
	if !again.DeletedAt.Valid || !again.DeletedAt.Time.Equal(firstHide) {
		t.Fatalf("re-hide must keep the original timestamp: first=%s again=%v",
			firstHide, again.DeletedAt)
	}

	snap = mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseHidden: false,
	})
	if snap[editspec.FieldReleaseHidden] != false {
		t.Fatalf("unhide snapshot = %#v", snap[editspec.FieldReleaseHidden])
	}
	live := reloadRelease(t, rel.ID)
	if live.DeletedAt.Valid {
		t.Fatalf("unhide must restore NULL deleted_at: %+v", live.DeletedAt)
	}
}

func TestReleaseSiteOverlays(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterRelease(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	spec, ok := reg.Type(editspec.TypeRelease)
	if !ok {
		t.Fatal("catalog.release is not registered")
	}
	for i := range spec.Fields {
		key := spec.Fields[i].Key
		kungal := spec.EffectivePolicy(key, "kungal")
		if kungal.Propose != editing.ProposeOpen || kungal.Automerge != editing.AutomergeReview || !kungal.OwnerReview {
			t.Fatalf("kungal policy for %s = %+v, want {open, review, ownerReview}", key, kungal)
		}
		if kungal.Review != editing.ReviewPerm("edit.catalog.release.review") {
			t.Fatalf("kungal ReviewPerm for %s = %s", key, kungal.Review)
		}
		for _, site := range []string{"letmoe", "letmoe-staging", "letmoe-dev"} {
			letmoe := spec.EffectivePolicy(key, site)
			if letmoe.Propose != editing.ProposeTrusted || letmoe.Automerge != editing.AutomergeOwner {
				t.Fatalf("%s policy for %s = %+v, want {trusted, owner}", site, key, letmoe)
			}
			if letmoe.Review != editing.ReviewPerm("edit.catalog.release.review") {
				t.Fatalf("%s ReviewPerm for %s = %s", site, key, letmoe.Review)
			}
		}
		nextmoe := spec.EffectivePolicy(key, "nextmoe")
		if nextmoe.Propose != editing.ProposePerm("edit.catalog.release") ||
			nextmoe.Automerge != editing.AutomergeNever {
			t.Fatalf("default policy for %s = %+v", key, nextmoe)
		}
	}
}

func TestReleaseValidatorsAndMissing(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "検証の主")
	rel := createReleaseOn(t, work.ID)
	editor := realActor(100, "admin")

	var valErr *editing.ValidationError
	cases := []map[string]any{
		{editspec.FieldReleaseKind: nil},
		{editspec.FieldReleaseKind: float64(9)},
		{editspec.FieldReleaseKind: "digital"},
		{editspec.FieldReleaseTitle: 1},
		{editspec.FieldReleaseLang: "xx"},
		{editspec.FieldReleaseLang: ""},
		{editspec.FieldReleasePlatform: "pc"},
		{editspec.FieldReleaseHidden: "yes"},
	}
	for i, patch := range cases {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeRelease, EntityID: rel.ID, Patch: patch, Actor: editor,
		}); !errors.As(err, &valErr) {
			t.Errorf("case %d (%v): want ValidationError, got %v", i, patch, err)
		}
	}

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeRelease, EntityID: 9_000_181,
		Patch: map[string]any{editspec.FieldReleaseTitle: "ghost"}, Actor: editor,
	}); !errors.Is(err, editing.ErrEntityNotFound) {
		t.Fatalf("missing release: %v", err)
	}
}

func TestReleaseEngineEdit(t *testing.T) {
	e := newReleaseEngine(t)
	if err := testDB.Exec("TRUNCATE catalog_engine RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate catalog_engine: %v", err)
	}
	engine := model.CatalogEngine{Name: "KiriKiri", Aliases: []byte("[]")}
	if err := testDB.Create(&engine).Error; err != nil {
		t.Fatalf("create engine: %v", err)
	}
	work := createWork(t, "エンジンの主")
	rel := createReleaseOn(t, work.ID)

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseEngine: float64(engine.ID),
	})
	if snap[editspec.FieldReleaseEngine] != engine.ID {
		t.Fatalf("snapshot engine_id = %#v, want %d", snap[editspec.FieldReleaseEngine], engine.ID)
	}
	got := reloadRelease(t, rel.ID)
	if got.EngineID == nil || *got.EngineID != engine.ID {
		t.Fatalf("release engine_id = %v, want %d", got.EngineID, engine.ID)
	}
	if head := provenance.FirstSource(got.FieldProvenance, "engine_id"); head != provenance.SourceCurated {
		t.Fatalf("field_provenance[engine_id] head = %q, want %q: the importer must see a person's choice", head, provenance.SourceCurated)
	}

	editor := realActor(100, "admin")
	var valErr *editing.ValidationError
	for name, value := range map[string]any{
		"unknown engine": float64(engine.ID + 1000),
		"not an integer": 1.5,
		"not positive":   float64(0),
		"a string":       "KiriKiri",
	} {
		prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeRelease, EntityID: rel.ID,
			Patch: map[string]any{editspec.FieldReleaseEngine: value}, Actor: editor,
		})
		if err == nil {
			_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
		}
		if !errors.As(err, &valErr) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}

	mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{editspec.FieldReleaseEngine: nil})
	if cleared := reloadRelease(t, rel.ID); cleared.EngineID != nil {
		t.Fatalf("clearing the engine left %d", *cleared.EngineID)
	}
}
