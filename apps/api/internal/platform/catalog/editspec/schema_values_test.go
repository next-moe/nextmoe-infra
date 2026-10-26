package editspec_test

import (
	"strings"
	"testing"

	"api/internal/platform/apiv2/vocab"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
)

func fullRegistry(t *testing.T) *editing.Registry {
	t.Helper()
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("register all: %v", err)
	}
	return reg
}

// Enums whose token set has no published vocabulary, each with the reason.
// A new enum field either names a vocabulary or earns a line here — the gate
// fails on silence, never on judgment.
var enumsWithoutVocabulary = map[string]string{
	editspec.FieldReleaseHidden:     "bool-as-enum: hidden/visible, no token set worth publishing",
	editspec.FieldWorkDisplayNSFW:   "bool-as-enum: forced-NSFW override flag, no token set worth publishing",
	editspec.FieldWorkRouteHints:    "bool-as-enum: the game shows route hints itself, no token set worth publishing",
	editspec.FieldReleaseFreeware:   "nullable bool-as-enum: freeware flag, null when no source recorded it",
	editspec.FieldReleaseOfficial:   "nullable bool-as-enum: official-release flag, null when no source recorded it",
	editspec.FieldTagSexual:         "bool-as-enum: the tag's sexual flag, no token set worth publishing",
	editspec.FieldTraitApplicable:   "bool-as-enum: whether the trait can be put on a character",
	editspec.FieldTraitSearchable:   "bool-as-enum: whether the trait is offered in search",
	editspec.FieldTraitSexual:       "bool-as-enum: the trait's sexual flag, no token set worth publishing",
	editspec.FieldWalkthroughHidden: "bool-as-enum: hidden/live, no token set worth publishing",
	editspec.FieldWalkthroughLang:   "two tokens, ja and zh-Hans: the language vocabulary's other tokens are not chart languages",
}

func TestSchemaValueSpecCompleteness(t *testing.T) {
	reg := fullRegistry(t)
	memberTypes := map[string]bool{
		"text": true, "int": true, "enum": true, "bool": true, "ref": true, "imagehash": true,
	}
	for _, typeName := range reg.Types() {
		spec, _ := reg.Type(typeName)
		for i := range spec.Fields {
			f := &spec.Fields[i]
			switch f.Kind {
			case editing.KindList:
				if f.Value == nil || f.Value.Element == nil {
					t.Errorf("%s: list field declares no element shape", f.Key)
					continue
				}
			case editing.KindEnum:
				if _, excluded := enumsWithoutVocabulary[f.Key]; excluded {
					if f.Value != nil && f.Value.Vocabulary != "" {
						t.Errorf("%s: names a vocabulary but sits on the no-vocabulary list", f.Key)
					}
				} else if f.Value == nil || f.Value.Vocabulary == "" {
					t.Errorf("%s: enum field names no vocabulary", f.Key)
				}
			}
			if f.Value == nil {
				continue
			}
			if f.Value.Vocabulary != "" {
				if _, ok := vocab.Lookup(f.Value.Vocabulary); !ok {
					t.Errorf("%s: vocabulary %q is not published", f.Key, f.Value.Vocabulary)
				}
			} else if f.Value.Coded {
				t.Errorf("%s: declares int encoding with no vocabulary to index", f.Key)
			}
			if f.Value.Base != 0 && !f.Value.Coded {
				t.Errorf("%s: carries base %d but is not int-encoded", f.Key, f.Value.Base)
			}
			el := f.Value.Element
			if el == nil {
				continue
			}
			if f.Kind != editing.KindList {
				t.Errorf("%s: element shape on a non-list field", f.Key)
			}
			switch el.Type {
			case "object":
				if len(el.Members) == 0 {
					t.Errorf("%s: object element lists no members", f.Key)
				}
			case "text", "ref":
				if len(el.Members) != 0 {
					t.Errorf("%s: scalar %s element lists members", f.Key, el.Type)
				}
			default:
				t.Errorf("%s: unknown element type %q", f.Key, el.Type)
			}
			seen := map[string]bool{}
			for _, m := range el.Members {
				if m.Key == "" || seen[m.Key] {
					t.Errorf("%s: member key %q empty or duplicate", f.Key, m.Key)
				}
				seen[m.Key] = true
				if !memberTypes[m.Type] {
					t.Errorf("%s.%s: unknown member type %q", f.Key, m.Key, m.Type)
				}
				if m.Vocabulary != "" {
					if _, ok := vocab.Lookup(m.Vocabulary); !ok {
						t.Errorf("%s.%s: vocabulary %q is not published", f.Key, m.Key, m.Vocabulary)
					}
				}
			}
		}
	}
}

func TestScalarNullabilityMatchesValidators(t *testing.T) {
	reg := fullRegistry(t)
	for _, typeName := range reg.Types() {
		spec, _ := reg.Type(typeName)
		for i := range spec.Fields {
			f := &spec.Fields[i]
			if f.Kind == editing.KindList {
				continue
			}
			declared := f.Value != nil && f.Value.Nullable
			accepted := f.Validate(nil) == nil
			if declared != accepted {
				t.Errorf("%s: declares nullable=%v but the validator accepts null=%v",
					f.Key, declared, accepted)
			}
		}
	}
}

func sampleMemberValue(m editing.ElementMember) any {
	if m.Vocabulary != "" {
		if m.Type == "int" {
			return float64(m.Base)
		}
		return vocab.Tokens(m.Vocabulary)[0]
	}
	switch m.Type {
	case "text":
		return "x"
	case "bool":
		return false
	case "imagehash":
		return strings.Repeat("0123456789abcdef", 4)
	default:
		return float64(1)
	}
}

// The schema publishes these members as nullable, and the editor's create-work
// page sent "latin": null accordingly — every new work with an original title
// was refused with "element 0: latin must be a non-empty string when present".
// Null is checked against the validator's verdict on the absent member, not
// against what it stores: on catalog.work.credits an absent release_id keeps
// the stored one while null clears it.
func TestNullableMembersAcceptNull(t *testing.T) {
	reg := fullRegistry(t)
	for _, typeName := range reg.Types() {
		spec, _ := reg.Type(typeName)
		for i := range spec.Fields {
			f := &spec.Fields[i]
			if f.Validate == nil || f.Value == nil || f.Value.Element == nil || f.Value.Element.Type != "object" {
				continue
			}
			sample := func() map[string]any {
				el := map[string]any{}
				for _, m := range f.Value.Element.Members {
					el[m.Key] = sampleMemberValue(m)
				}
				return el
			}
			if err := f.Validate([]any{sample()}); err != nil {
				t.Errorf("%s: sample element rejected: %v", f.Key, err)
				continue
			}
			for _, m := range f.Value.Element.Members {
				if !m.Nullable {
					continue
				}
				absent, null := sample(), sample()
				delete(absent, m.Key)
				null[m.Key] = nil
				wantErr := f.Validate([]any{absent})
				gotErr := f.Validate([]any{null})
				if (wantErr == nil) != (gotErr == nil) {
					t.Errorf("%s.%s: absent gives %v but null gives %v", f.Key, m.Key, wantErr, gotErr)
				}
			}
		}
	}
}

func TestVocabularyTokensPassValidators(t *testing.T) {
	reg := fullRegistry(t)
	for _, typeName := range reg.Types() {
		spec, _ := reg.Type(typeName)
		for i := range spec.Fields {
			f := &spec.Fields[i]
			if f.Kind != editing.KindEnum || f.Value == nil || f.Value.Vocabulary == "" {
				continue
			}
			tokens := vocab.Tokens(f.Value.Vocabulary)
			if len(tokens) == 0 {
				t.Errorf("%s: vocabulary %q publishes no tokens", f.Key, f.Value.Vocabulary)
				continue
			}
			base := f.Value.Base
			if !f.Value.Coded {
				for _, tok := range tokens {
					if err := f.Validate(tok); err != nil {
						t.Errorf("%s: token %q rejected by a token-encoded field: %v", f.Key, tok, err)
					}
				}
				if f.Validate("zz-no-such-token") == nil {
					t.Errorf("%s: accepts a string outside vocabulary %q", f.Key, f.Value.Vocabulary)
				}
				if f.Validate(float64(base)) == nil {
					t.Errorf("%s: declares token encoding but accepts the integer %d", f.Key, base)
				}
				continue
			}
			// Coded, so the wire must carry Base plus the token's index in the
			// vocabulary's published order — the contract
			// repr.SchemaField.Encoding publishes. Every code in range passes,
			// the first code past either end fails, and a token string is
			// refused, or the declaration is lying about this vocabulary.
			if f.Validate(tokens[0]) == nil {
				t.Errorf("%s: declares int encoding but accepts the token %q", f.Key, tokens[0])
			}
			for idx := range tokens {
				if err := f.Validate(float64(base + idx)); err != nil {
					t.Errorf("%s: code %d (%s) rejected: %v", f.Key, base+idx, tokens[idx], err)
				}
			}
			if f.Validate(float64(base+len(tokens))) == nil {
				t.Errorf("%s: accepts code %d beyond vocabulary %q",
					f.Key, base+len(tokens), f.Value.Vocabulary)
			}
			if f.Validate(float64(base-1)) == nil {
				t.Errorf("%s: accepts code %d below vocabulary %q",
					f.Key, base-1, f.Value.Vocabulary)
			}
		}
	}
}

func TestTitleKindIndexesMatchVocabulary(t *testing.T) {
	reg := fullRegistry(t)
	spec, ok := reg.Type(editspec.TypeWork)
	if !ok {
		t.Fatal("catalog.work not registered")
	}
	f, ok := spec.Field(editspec.FieldWorkTitles)
	if !ok {
		t.Fatal("titles not registered")
	}
	titles := func(kind float64, lang string) []any {
		return []any{
			map[string]any{"lang": "ja", "title": "公式名", "kind": float64(0)},
			map[string]any{"lang": lang, "title": "別名", "kind": kind},
		}
	}
	if n := len(vocab.Tokens("title_kind")); n != 3 {
		t.Fatalf("title_kind publishes %d tokens", n)
	}
	for kind := 0; kind < 3; kind++ {
		if err := f.Validate(titles(float64(kind), "en")); err != nil {
			t.Errorf("kind %d rejected: %v", kind, err)
		}
	}
	if f.Validate(titles(3, "en")) == nil {
		t.Error("kind 3 accepted beyond title_kind")
	}
	// Index 1 must be the alias token: only an alias may carry an empty lang.
	if err := f.Validate(titles(1, "")); err != nil {
		t.Errorf("alias (kind 1) with empty lang rejected: %v", err)
	}
	if f.Validate(titles(0, "")) == nil {
		t.Error("official (kind 0) with empty lang accepted")
	}
}
