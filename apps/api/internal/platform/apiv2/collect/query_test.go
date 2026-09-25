package collect

import (
	"testing"

	"api/internal/platform/apiv2/problem"
)

func TestWorkFullSetIsSubsetOfInclude(t *testing.T) {
	allowed := map[string]bool{}
	for _, tkn := range WorkInclude {
		allowed[tkn] = true
	}
	if len(WorkInclude) != 18 {
		t.Fatalf("include vocab %d, want 18", len(WorkInclude))
	}
	for _, tkn := range WorkFullSet {
		if !allowed[tkn] {
			t.Fatalf("FULL_SET token %s is not in include vocab", tkn)
		}
	}
}

func TestParseViewIncludeUnion(t *testing.T) {
	q, err := Parse(Raw{View: "full", Include: "characters"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if q.View != "full" {
		t.Fatalf("view=%s", q.View)
	}
	if !contains(q.Include, "titles") || !contains(q.Include, "characters") {
		t.Fatalf("full ∪ include: %v", q.Include)
	}
	_, err = Parse(Raw{View: "compact"}, WorkSpec())
	if err == nil || err.Code != problem.CodeUnknownEnumValue {
		t.Fatalf("bad view: %+v", err)
	}
	_, err = Parse(Raw{Include: "nope"}, WorkSpec())
	if err == nil || err.Code != problem.CodeUnknownInclude {
		t.Fatalf("bad include: %+v", err)
	}
}

func TestParseLimitAndCursorExclusiveWithIDs(t *testing.T) {
	_, err := Parse(Raw{Limit: "101"}, WorkSpec())
	if err == nil || err.Code != problem.CodeLimitTooLarge {
		t.Fatalf("limit: %+v", err)
	}
	_, err = Parse(Raw{IDs: "1,2", Cursor: EncodeCursor("1")}, WorkSpec())
	if err == nil || err.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("ids+cursor: %+v", err)
	}
	q, err := Parse(Raw{IDs: "1,2", Limit: "5"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !q.Batch || q.Limit != 0 {
		t.Fatalf("batch must ignore limit: %+v", q)
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "x"
	}
	raw := ids[0]
	for i := 1; i < 101; i++ {
		raw += "," + ids[i]
	}
	_, err = Parse(Raw{IDs: raw}, WorkSpec())
	if err == nil || err.Code != problem.CodeTooManyIDs {
		t.Fatalf("101 ids: %+v", err)
	}
}

func TestParseFieldsCanonicalAndUnknown(t *testing.T) {
	q, err := Parse(Raw{Fields: "cover,id,display_name,cover"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Fields) < 3 || q.Fields[0] != "cover" && q.Fields[0] != "display_name" && q.Fields[0] != "id" && q.Fields[0] != "object" {
		// just check object and id injected and cover not duplicated
	}
	seen := map[string]int{}
	for _, f := range q.Fields {
		seen[f]++
		if seen[f] > 1 {
			t.Fatalf("duplicate field %s", f)
		}
	}
	if seen["object"] != 1 || seen["id"] != 1 || seen["cover"] != 1 {
		t.Fatalf("fields=%v", q.Fields)
	}
	for i := 1; i < len(q.Fields); i++ {
		if q.Fields[i-1] > q.Fields[i] {
			t.Fatalf("fields not sorted: %v", q.Fields)
		}
	}
	_, err = Parse(Raw{Fields: "nope"}, WorkSpec())
	if err == nil || err.Code != problem.CodeUnknownField {
		t.Fatalf("unknown field: %+v", err)
	}
}

func TestParseRefsAndIncludeTotal(t *testing.T) {
	q, err := Parse(Raw{Refs: "vndb:v19658,dlsite:RJ01234567", IncludeTotal: "true"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Refs) != 2 || q.Refs[0].Source != "vndb" || q.Refs[1].ExternalID != "RJ01234567" {
		t.Fatalf("refs=%v", q.Refs)
	}
	if !q.IncludeTotal || !q.Batch {
		t.Fatalf("total/batch %+v", q)
	}
	_, err = Parse(Raw{Refs: "not-a-ref"}, WorkSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("bad ref: %+v", err)
	}
	_, err = Parse(Raw{IncludeTotal: "yes"}, WorkSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("include_total: %+v", err)
	}
}

func TestParseNSFW(t *testing.T) {
	q, err := Parse(Raw{}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if q.NSFW {
		t.Fatal("nsfw defaults false")
	}
	q, err = Parse(Raw{NSFW: "true"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !q.NSFW {
		t.Fatal("nsfw true")
	}
	q, err = Parse(Raw{NSFW: "false"}, WorkSpec())
	if err != nil {
		t.Fatal(err)
	}
	if q.NSFW {
		t.Fatal("nsfw false")
	}
	_, err = Parse(Raw{NSFW: "1"}, WorkSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("nsfw=1: %+v", err)
	}
}

func TestParsePageMode(t *testing.T) {
	q, err := Parse(Raw{Page: "1"}, WorkListSpec())
	if err != nil {
		t.Fatal(err)
	}
	if q.Page != 1 || q.Limit != DefaultLimit {
		t.Fatalf("page mode %+v", q)
	}
	q, err = Parse(Raw{}, WorkListSpec())
	if err != nil {
		t.Fatal(err)
	}
	if q.Page != 0 {
		t.Fatalf("empty page is cursor mode: %+v", q)
	}
	q, err = Parse(Raw{Page: "1"}, SearchSpec())
	if err != nil || q.Page != 1 {
		t.Fatalf("search spec page: %+v %v", q, err)
	}
	q, err = Parse(Raw{Page: "1"}, CharacterSpec())
	if err != nil || q.Page != 1 {
		t.Fatalf("character spec page: %+v %v", q, err)
	}
}

func TestParsePageNonInteger(t *testing.T) {
	_, err := Parse(Raw{Page: "abc"}, WorkListSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonInvalidFormat {
		t.Fatalf("errors: %+v", err.Errors)
	}
	if err.Errors[0].Params != nil {
		t.Fatalf("params: %+v", err.Errors[0].Params)
	}
}

func TestParsePageLessThanOne(t *testing.T) {
	for _, raw := range []string{"0", "-1"} {
		_, err := Parse(Raw{Page: raw}, WorkListSpec())
		if err == nil || err.Code != problem.CodeInvalidParameter {
			t.Fatalf("page=%s code: %+v", raw, err)
		}
		if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonOutOfRange {
			t.Fatalf("page=%s errors: %+v", raw, err.Errors)
		}
		p := err.Errors[0].Params
		if p == nil || p.Minimum == nil || *p.Minimum != 1 || p.Maximum != nil {
			t.Fatalf("page=%s params: %+v", raw, p)
		}
	}
}

func TestParsePageWithCursor(t *testing.T) {
	_, err := Parse(Raw{Page: "1", Cursor: EncodeCursor("3")}, WorkListSpec())
	if err == nil || err.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonNotAllowedValue {
		t.Fatalf("errors: %+v", err.Errors)
	}
}

func TestParsePageWithIDs(t *testing.T) {
	_, err := Parse(Raw{Page: "1", IDs: "1"}, WorkListSpec())
	if err == nil || err.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonNotAllowedValue {
		t.Fatalf("errors: %+v", err.Errors)
	}
}

func TestParsePageWithRefs(t *testing.T) {
	_, err := Parse(Raw{Page: "1", Refs: "vndb:v1"}, WorkListSpec())
	if err == nil || err.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonNotAllowedValue {
		t.Fatalf("errors: %+v", err.Errors)
	}
}

func TestParsePageTimesLimitExceedsDepth(t *testing.T) {
	_, err := Parse(Raw{Page: "501"}, WorkListSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("default limit code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonOutOfRange {
		t.Fatalf("default limit errors: %+v", err.Errors)
	}
	p := err.Errors[0].Params
	if p == nil || p.Minimum == nil || *p.Minimum != 1 || p.Maximum == nil || *p.Maximum != 500 {
		t.Fatalf("default limit params: %+v", p)
	}
	_, err = Parse(Raw{Page: "500"}, WorkListSpec())
	if err != nil {
		t.Fatalf("page*limit=10000 must be allowed: %+v", err)
	}
	_, err = Parse(Raw{Page: "101", Limit: "100"}, WorkListSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("limit=100 code: %+v", err)
	}
	p = err.Errors[0].Params
	if p == nil || p.Maximum == nil || *p.Maximum != 100 {
		t.Fatalf("limit=100 params: %+v", p)
	}
}

func TestParsePageRefusedWithoutSpec(t *testing.T) {
	_, err := Parse(Raw{Page: "1"}, VocabSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("code: %+v", err)
	}
	if len(err.Errors) != 1 || err.Errors[0].Parameter != "page" || err.Errors[0].Reason != problem.ReasonNotAllowedValue {
		t.Fatalf("errors: %+v", err.Errors)
	}
	_, err = Parse(Raw{Page: "abc"}, WorkSpec())
	if err == nil || err.Code != problem.CodeInvalidParameter {
		t.Fatalf("non-integer on a no-pages spec: %+v", err)
	}
	if err.Errors[0].Reason != problem.ReasonNotAllowedValue {
		t.Fatalf("spec.Pages is checked before the integer: %+v", err.Errors)
	}
}

func TestTraitSpecIntros(t *testing.T) {
	s := TraitSpec()
	if !contains(s.Include, "intros") || !contains(s.FullSet, "intros") {
		t.Fatalf("include=%v full=%v", s.Include, s.FullSet)
	}
	if contains(s.FullSet, "character_count") {
		t.Fatal("character_count must stay out of FullSet")
	}
	if !contains(s.Fields, "intros") {
		t.Fatalf("fields=%v", s.Fields)
	}
}
