package problem

import (
	"fmt"
	"strings"
	"testing"
)

func TestRegistryClosedAndDisjoint(t *testing.T) {
	if len(Codes) == 0 || len(Reasons) == 0 {
		t.Fatal("registries are empty")
	}
	seen := map[string]string{}
	for _, d := range Codes {
		if !NamePattern.MatchString(d.Code) {
			t.Errorf("code %q fails the name pattern", d.Code)
		}
		if d.Title == "" || d.Description == "" {
			t.Errorf("code %s missing title/description", d.Code)
		}
		if Kebab(d.Code) == "" || CodeFromKebab(Kebab(d.Code)) != d.Code {
			t.Errorf("code %s is not reversible with its type URI suffix %q", d.Code, Kebab(d.Code))
		}
		want := TypeURIPrefix + string(d.Domain) + "/" + Kebab(d.Code)
		if d.TypeURI() != want {
			t.Errorf("type URI %s want %s", d.TypeURI(), want)
		}
		if strings.ContainsAny(d.TypeURI(), "0123456789") && strings.Contains(d.Code, "404") {
			t.Errorf("numeric code leaked into type URI: %s", d.TypeURI())
		}
		if prev, ok := seen[d.Code]; ok {
			t.Errorf("code %s duplicated in %s and %s", d.Code, prev, d.Domain)
		}
		seen[d.Code] = string(d.Domain)
		if d.Status < 400 || d.Status > 599 {
			t.Errorf("code %s has non-error status %d", d.Code, d.Status)
		}
	}
	for _, r := range Reasons {
		if !NamePattern.MatchString(r.Reason) {
			t.Errorf("reason %q fails the name pattern", r.Reason)
		}
		if _, clash := seen[r.Reason]; clash {
			t.Errorf("reason %s collides with a top-level code", r.Reason)
		}
		if r.Title == "" || r.Description == "" {
			t.Errorf("reason %s missing title/description", r.Reason)
		}
	}
}

func TestReasonParamsMatchContract(t *testing.T) {
	want := map[string][]string{
		ReasonTooLong:      {ParamMaxLength},
		ReasonTooShort:     {ParamMinLength},
		ReasonOutOfRange:   {ParamMinimum, ParamMaximum},
		ReasonTooManyItems: {ParamMaxItems},
		ReasonTooFewItems:  {ParamMinItems},
		ReasonUnknownValue: {ParamAllowed},
	}
	if err := reasonParamsOK(Reasons, want); err != nil {
		t.Fatal(err)
	}
	if err := reasonParamsOK([]ReasonDef{{Reason: "X", Params: []string{"nope"}}}, want); err == nil {
		t.Fatal("positive control: a params key outside the seven must fail")
	}
	foundFew, foundInProgress := false, false
	for i, r := range Reasons {
		if r.Reason == ReasonTooManyItems {
			if i+1 >= len(Reasons) || Reasons[i+1].Reason != ReasonTooFewItems {
				t.Fatal("TOO_FEW_ITEMS must sit right after TOO_MANY_ITEMS")
			}
			foundFew = true
		}
	}
	if !foundFew {
		t.Fatal("TOO_MANY_ITEMS missing")
	}
	for i, d := range Codes {
		if d.Code == CodeIdempotencyKeyReused {
			if i+1 >= len(Codes) || Codes[i+1].Code != CodeIdempotencyRequestInProgress {
				t.Fatal("IDEMPOTENCY_REQUEST_IN_PROGRESS must sit right after IDEMPOTENCY_KEY_REUSED")
			}
			foundInProgress = true
		}
	}
	if !foundInProgress {
		t.Fatal("IDEMPOTENCY_KEY_REUSED missing")
	}
}

func reasonParamsOK(defs []ReasonDef, want map[string][]string) error {
	allowed := map[string]bool{
		ParamMaxLength: true, ParamMinLength: true, ParamMinimum: true, ParamMaximum: true,
		ParamMaxItems: true, ParamMinItems: true, ParamAllowed: true,
	}
	for _, r := range defs {
		for _, p := range r.Params {
			if !allowed[p] {
				return fmt.Errorf("reason %s declares unknown params key %q", r.Reason, p)
			}
		}
		exp := want[r.Reason]
		if !slicesEqual(r.Params, exp) {
			return fmt.Errorf("reason %s params %v want %v", r.Reason, r.Params, exp)
		}
	}
	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLookupUnknown(t *testing.T) {
	if _, ok := Lookup("NOT_A_CODE"); ok {
		t.Fatal("unknown code looked up")
	}
	if _, ok := LookupReason("NOT_A_REASON"); ok {
		t.Fatal("unknown reason looked up")
	}
	got, ok := Lookup(CodeRateLimited)
	if !ok || got.Status != 429 {
		t.Fatalf("RATE_LIMITED lookup = %+v ok=%v", got, ok)
	}
}

func TestULIDShape(t *testing.T) {
	id := newULID()
	if len(id) != 26 {
		t.Fatalf("ulid length %d", len(id))
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')
		if c == 'I' || c == 'L' || c == 'O' || c == 'U' {
			ok = false
		}
		if !ok {
			t.Fatalf("ulid %q has non-crockford char %q at %d", id, c, i)
		}
	}
}
