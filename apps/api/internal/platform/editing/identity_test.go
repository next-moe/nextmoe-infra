package editing_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	tagCreditName = "credit_name"
	tagCharacter  = "character"
	tagLabel      = "label"

	fCredits = "test.widget.credits"
)

// numericKeyCheck is the shape every synthetic key in this file uses:
// "credit:<int>:<int>:<int>" with a fixed segment count and no free text.
func numericKeyCheck(prefix string, segments int) func(string) error {
	return func(key string) error {
		parts := strings.Split(key, ":")
		if len(parts) != segments || parts[0] != prefix {
			return fmt.Errorf("%q is not a %s key", key, prefix)
		}
		for _, p := range parts[1:] {
			if _, err := strconv.Atoi(p); err != nil {
				return fmt.Errorf("%q has a non-numeric segment %q", key, p)
			}
		}
		return nil
	}
}

func creditsIdentity() *editing.IdentitySpec {
	return &editing.IdentitySpec{
		Segments: 4,
		KeyCheck: numericKeyCheck("credit", 4),
		Refs: []editing.IdentityRef{
			{Segment: 3, EntityTag: tagCreditName},
			{Segment: 4, EntityTag: tagCharacter, ZeroAbsent: true},
		},
	}
}

func listField(key string, identity *editing.IdentitySpec) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: identity,
		Validate: func(any) error { return nil },
		Apply:    func(context.Context, *gorm.DB, int64, any) error { return nil },
	}
}

func creditsRegistry(t *testing.T) *editing.Registry {
	t.Helper()
	spec := widgetSpec(testDB)
	credits := listField(fCredits, creditsIdentity())
	spec.Fields = append(spec.Fields, credits, editing.SuppressedFieldSpec(spec.Type, credits))
	reg := editing.NewRegistry()
	if err := reg.Register(spec); err != nil {
		t.Fatalf("register credits spec: %v", err)
	}
	return reg
}

func insertSuppressed(t *testing.T, entityID int64, fieldKey string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if err := testDB.Create(&editing.SuppressedRow{
			EntityType: "test.widget", EntityID: entityID, FieldKey: fieldKey, IdentityKey: k,
		}).Error; err != nil {
			t.Fatalf("insert %q on %d: %v", k, entityID, err)
		}
	}
}

func suppressedKeys(t *testing.T, entityID int64, fieldKey string) []string {
	t.Helper()
	keys, err := editing.LoadSuppressedKeys(testCtx, testDB, "test.widget", entityID, fieldKey)
	if err != nil {
		t.Fatalf("load keys for %d: %v", entityID, err)
	}
	return keys
}

func runFollow(t *testing.T, reg *editing.Registry, tag string, src, dst int64, scope []int64) {
	t.Helper()
	stmts := reg.IdentityFollowStmts(tag, src, dst, scope)
	if len(stmts) == 0 {
		t.Fatalf("tag %q produced no statements", tag)
	}
	for i, s := range stmts {
		if err := testDB.Exec(s.SQL, s.Args...).Error; err != nil {
			t.Fatalf("statement %d for tag %q: %v\n%s", i, tag, err, s.SQL)
		}
	}
}

func TestIdentityFollowRewritesAcrossEntities(t *testing.T) {
	cleanTables(t)
	reg := creditsRegistry(t)

	// Work 1 and work 2 both carry a credit for credit_name 7; neither work is
	// part of the credit_name merge, which is the whole point of the statement
	// not filtering on entity_id.
	insertSuppressed(t, 1, fCredits, "credit:2:7:31", "credit:2:9:31")
	insertSuppressed(t, 2, fCredits, "credit:5:7:0")
	insertSuppressed(t, 3, fCredits, "credit:5:7:0")
	// Work 3 already carries the key the rewrite would produce, so its source
	// row has nowhere to land and must be dropped instead of raising 23505.
	insertSuppressed(t, 3, fCredits, "credit:5:8:0")
	// A different field on the same rows must not move.
	insertSuppressed(t, 1, fRows, "credit:2:7:31")

	runFollow(t, reg, tagCreditName, 7, 8, nil)

	if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:2:8:31", "credit:2:9:31"}) {
		t.Fatalf("work 1 = %v", got)
	}
	if got := suppressedKeys(t, 2, fCredits); !equalKeys(got, []string{"credit:5:8:0"}) {
		t.Fatalf("work 2 = %v", got)
	}
	if got := suppressedKeys(t, 3, fCredits); !equalKeys(got, []string{"credit:5:8:0"}) {
		t.Fatalf("work 3 collided and must be left with the one surviving key: %v", got)
	}
	if got := suppressedKeys(t, 1, fRows); !equalKeys(got, []string{"credit:2:7:31"}) {
		t.Fatalf("another field's keys moved: %v", got)
	}

	t.Run("ZeroAbsentSegmentIsNeverRewritten", func(t *testing.T) {
		cleanTables(t)
		insertSuppressed(t, 1, fCredits, "credit:2:9:0", "credit:2:9:31")

		// 0 is "this credit names no character", not character id 0, so even a
		// merge addressed to it must leave the sentinel alone.
		runFollow(t, reg, tagCharacter, 0, 31, nil)
		if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:2:9:0", "credit:2:9:31"}) {
			t.Fatalf("the sentinel segment was rewritten: %v", got)
		}

		runFollow(t, reg, tagCharacter, 31, 44, nil)
		if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:2:9:0", "credit:2:9:44"}) {
			t.Fatalf("a real character id must still follow: %v", got)
		}
	})

	t.Run("ScopeLimitsTheRewriteToTheListedEntities", func(t *testing.T) {
		cleanTables(t)
		insertSuppressed(t, 1, fCredits, "credit:2:7:31")
		insertSuppressed(t, 2, fCredits, "credit:2:7:31")

		runFollow(t, reg, tagCreditName, 7, 8, []int64{1})
		if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:2:8:31"}) {
			t.Fatalf("in-scope entity = %v", got)
		}
		if got := suppressedKeys(t, 2, fCredits); !equalKeys(got, []string{"credit:2:7:31"}) {
			t.Fatalf("out-of-scope entity moved: %v", got)
		}
	})

	t.Run("AnUnreferencedTagProducesNothing", func(t *testing.T) {
		if stmts := reg.IdentityFollowStmts(tagLabel, 1, 2, nil); len(stmts) != 0 {
			t.Fatalf("label is in no identity key but produced %d statements", len(stmts))
		}
	})
}

func TestSuppressedFieldRequiresParentIdentity(t *testing.T) {
	spec := widgetSpec(testDB)
	parent := listField(fCredits, nil)
	spec.Fields = append(spec.Fields, parent, editing.SuppressedFieldSpec(spec.Type, parent))
	if err := editing.NewRegistry().Register(spec); err == nil {
		t.Fatal("a .suppressed companion over a parent with no IdentitySpec was accepted")
	} else if !strings.Contains(err.Error(), "Identity") {
		t.Fatalf("error does not name the missing declaration: %v", err)
	}

	orphan := widgetSpec(testDB)
	orphan.Fields = append(orphan.Fields, listField("test.widget.ghost"+editing.SuppressedFieldSuffix, nil))
	if err := editing.NewRegistry().Register(orphan); err == nil {
		t.Fatal("a .suppressed companion with no parent field at all was accepted")
	}

	check := numericKeyCheck("credit", 4)
	bad := []struct {
		why  string
		spec editing.IdentitySpec
	}{
		{"too few segments", editing.IdentitySpec{Segments: 1, KeyCheck: check}},
		{"no KeyCheck", editing.IdentitySpec{Segments: 4}},
		{"ref on the prefix segment", editing.IdentitySpec{Segments: 4, KeyCheck: check,
			Refs: []editing.IdentityRef{{Segment: 1, EntityTag: tagCharacter}}}},
		{"ref past the last segment", editing.IdentitySpec{Segments: 4, KeyCheck: check,
			Refs: []editing.IdentityRef{{Segment: 5, EntityTag: tagCharacter}}}},
		{"the same segment twice", editing.IdentitySpec{Segments: 4, KeyCheck: check,
			Refs: []editing.IdentityRef{{Segment: 3, EntityTag: tagCharacter}, {Segment: 3, EntityTag: tagCreditName}}}},
		{"an unset EntityTag", editing.IdentitySpec{Segments: 4, KeyCheck: check,
			Refs: []editing.IdentityRef{{Segment: 3}}}},
		{"free text next to a ref", editing.IdentitySpec{Segments: 4, KeyCheck: check, TrailingText: true,
			Refs: []editing.IdentityRef{{Segment: 3, EntityTag: tagCreditName}}}},
	}
	for _, c := range bad {
		s := widgetSpec(testDB)
		s.Fields = append(s.Fields, listField(fCredits, &c.spec))
		if err := editing.NewRegistry().Register(s); err == nil {
			t.Fatalf("%s was accepted", c.why)
		}
	}

	ok := widgetSpec(testDB)
	good := listField(fCredits, creditsIdentity())
	ok.Fields = append(ok.Fields, good, editing.SuppressedFieldSpec(ok.Type, good))
	if err := editing.NewRegistry().Register(ok); err != nil {
		t.Fatalf("a declared identity must register: %v", err)
	}
	textOnly := widgetSpec(testDB)
	tf := listField(fCredits, &editing.IdentitySpec{Segments: 4, TrailingText: true, KeyCheck: check})
	textOnly.Fields = append(textOnly.Fields, tf, editing.SuppressedFieldSpec(textOnly.Type, tf))
	if err := editing.NewRegistry().Register(textOnly); err != nil {
		t.Fatalf("free text without refs must register: %v", err)
	}
}

func TestSuppressedKeysMustBeCanonical(t *testing.T) {
	parent := listField(fCredits, creditsIdentity())
	companion := editing.SuppressedFieldSpec("test.widget", parent)

	good := []any{"credit:2:7:0", "credit:2:8:31"}
	if err := companion.Validate(good); err != nil {
		t.Fatalf("canonical keys rejected: %v", err)
	}
	bad := []struct {
		why  string
		keys []any
	}{
		{"wrong prefix", []any{"title:2:7:0"}},
		{"too few segments", []any{"credit:2:7"}},
		{"a non-numeric segment", []any{"credit:2:seven:0"}},
	}
	for _, c := range bad {
		if err := companion.Validate(c.keys); err == nil {
			t.Fatalf("%s was accepted by Validate", c.why)
		}
		// Apply runs the same gate: a proposal that predates a registry change
		// must not slip a non-canonical key past a merge.
		var valErr *editing.ValidationError
		err := companion.Apply(testCtx, testDB, 1, c.keys)
		if !errors.As(err, &valErr) {
			t.Fatalf("%s: Apply returned %v, want a ValidationError (422)", c.why, err)
		}
	}
}

func TestIdentityFollowStmtsCoverEveryDeclaredRef(t *testing.T) {
	reg := editing.NewRegistry()

	first := widgetSpec(testDB)
	credits := listField(fCredits, creditsIdentity())
	rows := listField(fRows, &editing.IdentitySpec{
		Segments: 3, KeyCheck: numericKeyCheck("row", 3),
		Refs: []editing.IdentityRef{{Segment: 2, EntityTag: tagLabel}},
	})
	text := listField("test.widget.notes", &editing.IdentitySpec{
		Segments: 4, TrailingText: true, KeyCheck: numericKeyCheck("note", 4),
	})
	first.Fields = append(first.Fields, credits, editing.SuppressedFieldSpec(first.Type, credits), rows, text)
	if err := reg.Register(first); err != nil {
		t.Fatalf("register first: %v", err)
	}

	base := widgetSpec(testDB)
	second := editing.EntityTypeSpec{
		Family: "test", Type: "test.gadget",
		LoadSnapshot: base.LoadSnapshot, DefaultPolicy: base.DefaultPolicy,
		Fields: []editing.FieldSpec{listField("test.gadget.roster", &editing.IdentitySpec{
			Segments: 3, KeyCheck: numericKeyCheck("roster", 3),
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: tagCharacter}},
		})},
	}
	if err := reg.Register(second); err != nil {
		t.Fatalf("register second: %v", err)
	}

	want := map[string][]string{
		tagCreditName: {"test.widget|" + fCredits},
		tagCharacter:  {"test.gadget|test.gadget.roster", "test.widget|" + fCredits},
		tagLabel:      {"test.widget|" + fRows},
	}
	for tag, pairs := range want {
		stmts := reg.IdentityFollowStmts(tag, 7, 8, nil)
		if len(stmts) != 2*len(pairs) {
			t.Fatalf("tag %q produced %d statements, want %d (one move + one drop per declared ref)",
				tag, len(stmts), 2*len(pairs))
		}
		for _, pair := range pairs {
			parts := strings.SplitN(pair, "|", 2)
			if !mentionsPair(stmts, parts[0], parts[1]) {
				t.Fatalf("tag %q declared on %s.%s produced no statement", tag, parts[0], parts[1])
			}
		}
	}

	// A field with an identity but no refs is exactly the text-key case, and it
	// must never be swept into a follow.
	for _, tag := range []string{tagCreditName, tagCharacter, tagLabel, "nobody"} {
		for _, s := range reg.IdentityFollowStmts(tag, 7, 8, nil) {
			if mentionsPair([]editing.Stmt{s}, "test.widget", "test.widget.notes") {
				t.Fatalf("a ref-free identity produced a follow statement: %s", s.SQL)
			}
		}
	}

	if got := reg.IdentityFollowStmts("nobody", 7, 8, nil); len(got) != 0 {
		t.Fatalf("an undeclared tag produced %d statements", len(got))
	}
	if got := reg.IdentityFollowStmts("", 7, 8, nil); len(got) != 0 {
		t.Fatalf("the empty tag produced %d statements", len(got))
	}
}

func TestRekeySuppressedFollowsRoleReclassification(t *testing.T) {
	cleanTables(t)
	reg := creditsRegistry(t)

	insertSuppressed(t, 1, fCredits, "credit:2:7:31", "credit:2:9:0")
	insertSuppressed(t, 2, fCredits, "credit:2:7:31", "credit:5:7:31")

	moved, dropped, err := reg.RekeySuppressed(testCtx, testDB, "test.widget", fCredits, []editing.KeyMove{
		{EntityID: 1, From: "credit:2:7:31", To: "credit:5:7:31"},
		{EntityID: 1, From: "credit:2:9:0", To: "credit:5:9:0"},
		// Entity 2 already carries the target key, so this row has nowhere to
		// land and is dropped rather than raising 23505.
		{EntityID: 2, From: "credit:2:7:31", To: "credit:5:7:31"},
		// A row that is not suppressed at all is neither moved nor dropped.
		{EntityID: 3, From: "credit:2:7:31", To: "credit:5:7:31"},
	})
	if err != nil {
		t.Fatalf("rekey: %v", err)
	}
	if moved != 2 || dropped != 1 {
		t.Fatalf("moved=%d dropped=%d, want 2 and 1", moved, dropped)
	}
	if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:5:7:31", "credit:5:9:0"}) {
		t.Fatalf("entity 1 = %v", got)
	}
	if got := suppressedKeys(t, 2, fCredits); !equalKeys(got, []string{"credit:5:7:31"}) {
		t.Fatalf("entity 2 = %v", got)
	}

	t.Run("KeyCheckRejectsAnIllegalTarget", func(t *testing.T) {
		cleanTables(t)
		insertSuppressed(t, 1, fCredits, "credit:2:7:31")
		if _, _, err := reg.RekeySuppressed(testCtx, testDB, "test.widget", fCredits,
			[]editing.KeyMove{{EntityID: 1, From: "credit:2:7:31", To: "credit:2:7"}}); err == nil {
			t.Fatal("a target key the field would reject on the write face was accepted")
		}
		if got := suppressedKeys(t, 1, fCredits); !equalKeys(got, []string{"credit:2:7:31"}) {
			t.Fatalf("a rejected batch must write nothing: %v", got)
		}
	})

	t.Run("AnUnregisteredFieldIsAnError", func(t *testing.T) {
		if _, _, err := reg.RekeySuppressed(testCtx, testDB, "test.widget", "test.widget.nope", nil); err == nil {
			t.Fatal("an unregistered field key was accepted")
		}
		if _, _, err := reg.RekeySuppressed(testCtx, testDB, "test.nothing", fCredits, nil); err == nil {
			t.Fatal("an unregistered entity type was accepted")
		}
	})
}

func mentionsPair(stmts []editing.Stmt, entityType, fieldKey string) bool {
	for _, s := range stmts {
		var hasType, hasField bool
		for _, a := range s.Args {
			str, ok := a.(string)
			if !ok {
				continue
			}
			hasType = hasType || str == entityType
			hasField = hasField || str == fieldKey
		}
		if hasType && hasField {
			return true
		}
	}
	return false
}

func equalKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	sort.Strings(got)
	sort.Strings(want)
	return fmt.Sprint(got) == fmt.Sprint(want)
}
