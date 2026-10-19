package editing_test

import (
	"context"
	"testing"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

func TestRegisterBackfillsEffectiveMaxSuppressed(t *testing.T) {
	noopValidate := func(any) error { return nil }
	noopApply := func(context.Context, *gorm.DB, int64, any) error { return nil }
	defaulted := editing.FieldSpec{
		Key: "test.caps.rows", Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{Segments: 2, KeyCheck: numericKeyCheck("row", 2)},
		Validate: noopValidate, Apply: noopApply,
	}
	declared := editing.FieldSpec{
		Key: "test.caps.cols", Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity:      &editing.IdentitySpec{Segments: 2, KeyCheck: numericKeyCheck("col", 2)},
		MaxSuppressed: 500,
		Validate:      noopValidate, Apply: noopApply,
	}
	bare := editing.FieldSpec{
		Key: "test.caps.tags", Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Validate: noopValidate, Apply: noopApply,
	}
	spec := editing.EntityTypeSpec{
		Family: "test", Type: "test.caps",
		LoadSnapshot: func(context.Context, int64) (map[string]any, error) { return map[string]any{}, nil },
		DefaultPolicy: editing.Policy{
			Propose: editing.ProposePerm(permPropose), Review: editing.ReviewPerm(permReview),
			Automerge: editing.AutomergeNever,
		},
		Fields: []editing.FieldSpec{
			defaulted, editing.SuppressedFieldSpec("test.caps", defaulted),
			declared, editing.SuppressedFieldSpec("test.caps", declared),
			bare,
		},
	}
	reg := editing.NewRegistry()
	if err := reg.Register(spec); err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Type("test.caps")
	if !ok {
		t.Fatal("type not registered")
	}
	byKey := map[string]int{}
	for i := range got.Fields {
		byKey[got.Fields[i].Key] = got.Fields[i].MaxSuppressed
	}
	if byKey["test.caps.rows"] != byKey["test.caps.rows"+editing.SuppressedFieldSuffix] || byKey["test.caps.rows"] <= 0 {
		t.Fatalf("defaulted parent must publish the companion's cap: %v", byKey)
	}
	if byKey["test.caps.cols"] != 500 || byKey["test.caps.cols"+editing.SuppressedFieldSuffix] != 500 {
		t.Fatalf("declared cap must survive: %v", byKey)
	}
	if byKey["test.caps.tags"] != 0 {
		t.Fatalf("a list with no suppression companion has no suppression set: %v", byKey)
	}
}
