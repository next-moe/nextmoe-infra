package editspec_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

const (
	bgmTagSource    int16 = 3
	dlsiteTagSource int16 = 4
)

func mkCanonicalTag(t *testing.T, name string) model.CatalogTag {
	t.Helper()
	tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent}
	if err := testDB.Create(&tag).Error; err != nil {
		t.Fatalf("create tag %q: %v", name, err)
	}
	return tag
}

func mapTagName(t *testing.T, source int16, name string, tagID int64) {
	t.Helper()
	if err := testDB.Create(&model.CatalogTagSourceMap{SourceID: source, SourceName: name, TagID: tagID}).Error; err != nil {
		t.Fatalf("map %d/%q: %v", source, name, err)
	}
}

func mkImportedWorkTag(t *testing.T, workID int64, source int16, name string) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkTag{WorkID: workID, Name: name, SourceID: source, Count: 1}).Error; err != nil {
		t.Fatalf("work tag %d/%q: %v", source, name, err)
	}
}

func workTagRows(t *testing.T, workID int64) []string {
	t.Helper()
	var rows []model.CatalogWorkTag
	if err := testDB.Where("work_id = ?", workID).Order("source_id, name").Find(&rows).Error; err != nil {
		t.Fatalf("load work tags: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%d:%s", r.SourceID, r.Name))
	}
	return out
}

func TestWorkTagSuppressionDeletesEverySourceAndStaysOnItsWork(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "WHITE ALBUM2")
	other := createWork(t, "别的作品")

	anal := mkCanonicalTag(t, "肛交")
	romance := mkCanonicalTag(t, "恋爱")
	mapTagName(t, vndbSource, "肛交", anal.ID)
	mapTagName(t, bgmTagSource, "肛交", anal.ID)
	mapTagName(t, dlsiteTagSource, "アナル", anal.ID)
	mapTagName(t, vndbSource, "恋爱", romance.ID)

	for _, row := range []struct {
		work   int64
		source int16
		name   string
	}{
		{work.ID, vndbSource, "肛交"},
		{work.ID, bgmTagSource, "肛交"},
		{work.ID, dlsiteTagSource, "アナル"},
		{work.ID, vndbSource, "恋爱"},
		{work.ID, bgmTagSource, "unmapped"},
		{other.ID, vndbSource, "肛交"},
	} {
		mkImportedWorkTag(t, row.work, row.source, row.name)
	}

	key := editspec.WorkTagIdentity(anal.ID)
	snap := mergeField(t, e, work.ID, editspec.FieldWorkTagIDsSuppr, suppressList(key))
	sameJSON(t, editspec.FieldWorkTagIDsSuppr, snap[editspec.FieldWorkTagIDsSuppr], []any{key})

	if got, want := workTagRows(t, work.ID), []string{"2:恋爱", "3:unmapped"}; !slices.Equal(got, want) {
		t.Fatalf("tag rows on the suppressing work = %v, want %v", got, want)
	}
	if got, want := workTagRows(t, other.ID), []string{"2:肛交"}; !slices.Equal(got, want) {
		t.Fatalf("a suppression must not reach another work: %v, want %v", got, want)
	}

	var row editing.SuppressedRow
	if err := testDB.Where("entity_type = ? AND entity_id = ?", editspec.TypeWork, work.ID).First(&row).Error; err != nil {
		t.Fatalf("load suppression row: %v", err)
	}
	if row.FieldKey != editspec.FieldWorkTagIDs || row.IdentityKey != key {
		t.Fatalf("suppression row = %+v", row)
	}

	for source, want := range map[int16]string{vndbSource: "肛交", bgmTagSource: "肛交", dlsiteTagSource: "アナル"} {
		set, err := editspec.LoadSuppressedWorkTags(testCtx, testDB, source)
		if err != nil {
			t.Fatalf("load suppressed for source %d: %v", source, err)
		}
		if !set.Has(work.ID, want) || set.Has(other.ID, want) || set.Has(work.ID, "恋爱") {
			t.Fatalf("source %d: suppressed set = %v", source, set)
		}
	}

	mergeField(t, e, work.ID, editspec.FieldWorkTagIDsSuppr, []any{})
	if got, want := workTagRows(t, work.ID), []string{"2:恋爱", "3:unmapped"}; !slices.Equal(got, want) {
		t.Fatalf("lifting a suppression restores nothing by itself; the importers re-add: %v", got)
	}
	set, err := editspec.LoadSuppressedWorkTags(testCtx, testDB, vndbSource)
	if err != nil {
		t.Fatal(err)
	}
	if set.Has(work.ID, "肛交") {
		t.Fatal("a lifted suppression still blocks the importer")
	}
}

func TestCuratedTagsAndSuppressionCannotContradict(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	anal := mkCanonicalTag(t, "肛交")
	romance := mkCanonicalTag(t, "恋爱")
	mapTagName(t, vndbSource, "肛交", anal.ID)
	mkImportedWorkTag(t, work.ID, vndbSource, "肛交")
	key := editspec.WorkTagIdentity(anal.ID)

	mergeField(t, e, work.ID, editspec.FieldWorkTagIDs, []any{anal.ID, romance.ID})
	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkTagIDsSuppr, suppressList(key))
	if !strings.Contains(valErr.Reason, editspec.FieldWorkTagIDs) {
		t.Fatalf("the 422 must point at %s, got %q", editspec.FieldWorkTagIDs, valErr.Reason)
	}
	if got, want := workTagRows(t, work.ID), []string{"2:肛交", "12:恋爱", "12:肛交"}; !slices.Equal(got, want) {
		t.Fatalf("a rejected suppression must roll back: %v, want %v", got, want)
	}

	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{
			editspec.FieldWorkTagIDs:      []any{romance.ID},
			editspec.FieldWorkTagIDsSuppr: suppressList(key),
		},
		Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); err != nil {
		t.Fatalf("dropping the curated edge and suppressing it in one edit: %v", err)
	}
	if got, want := workTagRows(t, work.ID), []string{"12:恋爱"}; !slices.Equal(got, want) {
		t.Fatalf("tag rows after drop+suppress = %v, want %v", got, want)
	}

	valErr = mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkTagIDs, []any{anal.ID, romance.ID})
	if !strings.Contains(valErr.Reason, editspec.FieldWorkTagIDsSuppr) {
		t.Fatalf("the 422 must point at %s, got %q", editspec.FieldWorkTagIDsSuppr, valErr.Reason)
	}
	if got, want := workTagRows(t, work.ID), []string{"12:恋爱"}; !slices.Equal(got, want) {
		t.Fatalf("a rejected curated edit must roll back: %v, want %v", got, want)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, editspec.FieldWorkTagIDs, snap[editspec.FieldWorkTagIDs], []any{romance.ID})

	mergeField(t, e, work.ID, editspec.FieldWorkTagIDsSuppr, []any{})
	snap = mergeField(t, e, work.ID, editspec.FieldWorkTagIDs, []any{anal.ID, romance.ID})
	sameJSON(t, editspec.FieldWorkTagIDs, snap[editspec.FieldWorkTagIDs], []any{anal.ID, romance.ID})
}

func TestWorkTagSuppressionKeys(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	var valErr *editing.ValidationError
	for _, bad := range []any{"12", "tag:0", "tag:-3", "tag:012", "tag:abc", "tag:1:2", "title:1::x"} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTagIDsSuppr: []any{bad}},
			Actor: realActor(100),
		}); !errors.As(err, &valErr) {
			t.Fatalf("key %q: err = %v, want ValidationError", bad, err)
		}
	}

	reg := editing.NewRegistry()
	if err := editspec.RegisterWork(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	spec, _ := reg.Type(editspec.TypeWork)
	companion, ok := spec.Field(editspec.FieldWorkTagIDsSuppr)
	if !ok {
		t.Fatalf("%s is not registered", editspec.FieldWorkTagIDsSuppr)
	}
	if err := companion.Validate([]any{editspec.WorkTagIdentity(1234)}); err != nil {
		t.Fatalf("the field's own helper must produce a key the field accepts: %v", err)
	}
	if editspec.FieldWorkTagIDsSuppr != editing.SuppressedFieldKey(editspec.FieldWorkTagIDs) {
		t.Fatalf("companion key %q does not follow the engine's suffix", editspec.FieldWorkTagIDsSuppr)
	}
	if parent, comp := spec.EffectivePolicy(editspec.FieldWorkTagIDs),
		spec.EffectivePolicy(editspec.FieldWorkTagIDsSuppr); parent != comp {
		t.Fatalf("parent %+v vs companion %+v", parent, comp)
	}
}
