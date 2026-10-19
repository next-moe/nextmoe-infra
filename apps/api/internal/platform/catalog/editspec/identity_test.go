package editspec_test

import (
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func TestRegisterAllRegistersEveryEditableType(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	for _, typ := range []string{
		editspec.TypeWork, editspec.TypeCharacter, editspec.TypeRelease,
		editspec.TypeLabel, editspec.TypeTag, editspec.TypeEngine, editspec.TypeSeries,
	} {
		if _, ok := reg.Type(typ); !ok {
			t.Fatalf("RegisterAll did not register %q", typ)
		}
	}
}

func TestTextIdentityKeysDeclareTrailingText(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	for _, c := range []struct{ typ, field string }{
		{editspec.TypeWork, editspec.FieldWorkTitles},
		{editspec.TypeCharacter, editspec.FieldCharacterAliases},
	} {
		spec, ok := reg.Type(c.typ)
		if !ok {
			t.Fatalf("%q is not registered", c.typ)
		}
		f, ok := spec.Field(c.field)
		if !ok {
			t.Fatalf("%q has no field %q", c.typ, c.field)
		}
		if f.Identity == nil || !f.Identity.TrailingText || len(f.Identity.Refs) != 0 {
			t.Fatalf("%s identity = %+v; the last segment is unescaped text, so it must declare "+
				"TrailingText and can never grow an entity ref", c.field, f.Identity)
		}
		if f.Identity.KeyCheck == nil {
			t.Fatalf("%s declares no KeyCheck", c.field)
		}
	}
}

func TestSuppressionRejectsNonCanonicalKeys(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "正規化キー")
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	spec, _ := reg.Type(editspec.TypeWork)
	companion, ok := spec.Field(editspec.FieldWorkTitlesSuppr)
	if !ok {
		t.Fatal("catalog.work.titles.suppressed is not registered")
	}

	bad := []struct {
		why  string
		keys []any
	}{
		{"a zero-width space in the text segment", []any{"title:1::\u30b5\u30af\u30e9\u200b\u30ce\u8a69"}},
		{"edge whitespace in the text segment", []any{"title:1::Sakura no Uta "}},
		{"too few segments", []any{"title:1:ja"}},
		{"a non-numeric kind", []any{"title:alias:ja:サクラノ詩"}},
		{"the wrong prefix", []any{"alias:1:ja:サクラノ詩"}},
		{"an empty text segment", []any{"title:1:ja:"}},
	}
	var valErr *editing.ValidationError
	for _, c := range bad {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitlesSuppr: c.keys},
			Actor: realActor(100),
		}); !errors.As(err, &valErr) {
			t.Fatalf("%s: propose returned %v, want a ValidationError (422)", c.why, err)
		}
		if err := companion.Apply(testCtx, testDB, work.ID, c.keys); !errors.As(err, &valErr) {
			t.Fatalf("%s: Apply returned %v, want a ValidationError (422)", c.why, err)
		}
	}

	if err := companion.Validate([]any{"title:1::Sakura no Uta "}); err == nil {
		t.Fatal("edge whitespace was accepted")
	} else if !strings.Contains(err.Error(), `send "title:1::Sakura no Uta"`) {
		t.Fatalf("the error must hand back the canonical key, got %q", err.Error())
	}

	canonical := []any{editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "", "Sakura no Uta ")}
	if err := companion.Validate(canonical); err != nil {
		t.Fatalf("the field's own helper must produce a key the field accepts: %v", err)
	}
}
