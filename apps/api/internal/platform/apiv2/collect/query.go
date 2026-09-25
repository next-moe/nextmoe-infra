package collect

import (
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
)

const DefaultLimit = 20

// The batch bound the whole v2 surface shares: ids=, refs=, work_ids= and
// every request-body array. Three POST bodies carried no bound at all while
// the spec's prose promised this one, so the cap was a server surprise on the
// one op (batch playtimes) whose entire contract is per-item partial failure.
const MaxBatchItems = 100

const MaxPageDepth = 10000

type Raw struct {
	Cursor       string
	Limit        string
	View         string
	Include      string
	Fields       string
	IDs          string
	Refs         string
	IncludeTotal string
	Facets       string
	Sort         string
	NSFW         string
	Page         string
}

type Spec struct {
	Sort    []string
	Include []string
	FullSet []string
	Fields  []string
	Facets  []string
	// ids= and refs= ride the shared collection input onto every list op, so
	// four faces declared and parsed a batch lane they had no code for: q.Batch
	// then zeroed the limit and suppressed next_cursor, and
	// GET /v2/moderation/claims?ids=1234 answered 200 with the first 20 pending
	// claims, no missing[] and no cursor. Marked rather than inferred, and
	// marked on the few that lack the lane rather than the many that have it:
	// a wrong mark here can only over-refuse a named collection, never silently
	// break the ids= hydration every consumer runs against the catalog lanes.
	NoBatch bool
	Pages   bool
}

type Query struct {
	Cursor       string
	Limit        int
	View         string
	Include      []string
	Fields       []string
	IDs          []string
	Refs         []repr.Ref
	IncludeTotal bool
	Facets       []string
	Sort         string
	Batch        bool
	NSFW         bool
	Page         int
}

func Parse(raw Raw, spec Spec) (Query, *problem.Problem) {
	q := Query{View: "basic", Limit: DefaultLimit}
	if spec.Sort != nil {
		q.Sort = spec.Sort[0]
	}

	if raw.View != "" {
		v, err := parse.Enum(raw.View, "view", []string{"basic", "full"})
		if err != nil {
			return Query{}, err
		}
		q.View = v
	}

	limit, err := parse.Limit(raw.Limit, DefaultLimit, parse.MaxPageLimit)
	if err != nil {
		return Query{}, err
	}
	q.Limit = limit

	if raw.Sort != "" {
		if len(spec.Sort) == 0 {
			return Query{}, problem.New(problem.CodeUnknownSort, "", "", "this collection does not take sort.")
		}
		s, err := parse.Enum(raw.Sort, "sort", spec.Sort)
		if err != nil {
			d, _ := problem.Lookup(problem.CodeUnknownSort)
			err.Code = d.Code
			err.Title = d.Title
			err.Type = d.TypeURI()
			err.Status = d.Status
			return Query{}, err
		}
		q.Sort = s
	}

	ids, err := splitIDs(raw.IDs)
	if err != nil {
		return Query{}, err
	}
	refs, err := splitRefs(raw.Refs)
	if err != nil {
		return Query{}, err
	}
	q.IDs, q.Refs = ids, refs
	q.Batch = len(ids) > 0 || len(refs) > 0

	if q.Batch && spec.NoBatch {
		name := "ids"
		if len(ids) == 0 {
			name = "refs"
		}
		p := problem.New(problem.CodeInvalidParameter, "", "", "this collection has no batch lane.")
		p.Errors = []problem.FieldError{{
			Parameter: name,
			Reason:    problem.ReasonNotAllowedValue,
			Detail:    "ids= and refs= are not accepted here; page this collection with cursor=",
		}}
		return Query{}, p
	}

	if raw.Cursor != "" && q.Batch {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "cursor cannot be combined with ids or refs.")
		p.Errors = []problem.FieldError{{
			Parameter: "cursor",
			Reason:    problem.ReasonNotAllowedValue,
			Detail:    "ids= and refs= are a batch lane and do not paginate",
		}}
		return Query{}, p
	}
	page, err := parsePage(raw.Page, q.Limit, q.Batch, raw.Cursor, spec.Pages)
	if err != nil {
		return Query{}, err
	}
	q.Page = page
	if raw.Cursor != "" {
		key, err := DecodeCursor(raw.Cursor)
		if err != nil {
			return Query{}, err
		}
		q.Cursor = key
	}

	if q.Batch {
		q.Limit = 0
	}

	inc, err := tokens(raw.Include, "include", spec.Include, problem.CodeUnknownInclude)
	if err != nil {
		return Query{}, err
	}
	if q.View == "full" {
		inc = union(inc, spec.FullSet)
	}
	q.Include = inc

	fields, err := tokens(raw.Fields, "fields", spec.Fields, problem.CodeUnknownField)
	if err != nil {
		return Query{}, err
	}
	if len(fields) > 0 {
		fields = union(fields, []string{"object", "id"})
		sort.Strings(fields)
	}
	q.Fields = fields

	if raw.IncludeTotal != "" {
		v, err := parse.Bool(raw.IncludeTotal, "include_total")
		if err != nil {
			return Query{}, err
		}
		q.IncludeTotal = v
	}

	facets, err := tokens(raw.Facets, "facets", spec.Facets, problem.CodeUnknownFacet)
	if err != nil {
		return Query{}, err
	}
	q.Facets = facets

	if raw.NSFW != "" {
		v, err := parse.Bool(raw.NSFW, "nsfw")
		if err != nil {
			return Query{}, err
		}
		q.NSFW = v
	}
	return q, nil
}

func parsePage(raw string, limit int, batch bool, cursor string, pages bool) (int, *problem.Problem) {
	if raw == "" {
		return 0, nil
	}
	if !pages {
		p := problem.New(problem.CodeInvalidParameter, "", "", "this collection does not take page.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonNotAllowedValue,
			Detail:    "page= is accepted only on /v2/catalog/works and /v2/catalog/search; page this collection with cursor=",
		}}
		return 0, p
	}
	n, conv := strconv.Atoi(raw)
	if conv != nil {
		p := problem.New(problem.CodeInvalidParameter, "", "", "page is invalid.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonInvalidFormat,
			Detail:    "expected a positive integer",
		}}
		return 0, p
	}
	if n < 1 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "page is out of range.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonOutOfRange,
			Detail:    "page starts at 1",
			Params:    &problem.FieldParams{Minimum: problem.Ptr(1.0)},
		}}
		return 0, p
	}
	if cursor != "" {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "page cannot be combined with cursor.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonNotAllowedValue,
			Detail:    "page= selects page mode and does not take cursor=",
		}}
		return 0, p
	}
	if batch {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "page cannot be combined with ids or refs.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonNotAllowedValue,
			Detail:    "ids= and refs= are a batch lane and do not paginate",
		}}
		return 0, p
	}
	if int64(n)*int64(limit) > int64(MaxPageDepth) {
		p := problem.New(problem.CodeInvalidParameter, "", "", "page is out of range.")
		p.Errors = []problem.FieldError{{
			Parameter: "page",
			Reason:    problem.ReasonOutOfRange,
			Detail:    "page times limit may not exceed " + strconv.Itoa(MaxPageDepth),
			Params: &problem.FieldParams{
				Minimum: problem.Ptr(1.0),
				Maximum: problem.Ptr(float64(MaxPageDepth / limit)),
			},
		}}
		return 0, p
	}
	return n, nil
}

func tokens(raw, name string, allowed []string, code string) ([]string, *problem.Problem) {
	if raw == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if seen[t] {
			continue
		}
		// `allowed != nil` used to guard this compare, so a spec that left the
		// list unset meant "everything is allowed" rather than "nothing is".
		// Only Facets was ever left nil, and the result was that
		// /v2/catalog/works 400s facets=bogus_facet while all seven entity
		// list faces answered 200 to the same token the spec promises is a 400
		// UNKNOWN_FACET.
		if !contains(allowed, t) {
			p := problem.New(code, "", "", "unknown "+name+" token.")
			p.Errors = []problem.FieldError{{
				Parameter: name,
				Reason:    problem.ReasonUnknownValue,
				Detail:    "allowed values: " + strings.Join(allowed, ", "),
				Params:    &problem.FieldParams{Allowed: problem.Ptr(append([]string(nil), allowed...))},
			}}
			return nil, p
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, nil
}

func splitIDs(raw string) ([]string, *problem.Problem) {
	if raw == "" {
		return nil, nil
	}
	var ids []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		ids = append(ids, t)
	}
	if len(ids) > MaxBatchItems {
		p := problem.New(problem.CodeTooManyIDs, "", "", "ids= accepts at most 100 values.")
		p.Errors = []problem.FieldError{{
			Parameter: "ids",
			Reason:    problem.ReasonTooManyItems,
			Detail:    "maximum 100",
			Params:    &problem.FieldParams{MaxItems: problem.Ptr(MaxBatchItems)},
		}}
		return nil, p
	}
	return ids, nil
}

func splitRefs(raw string) ([]repr.Ref, *problem.Problem) {
	if raw == "" {
		return nil, nil
	}
	var refs []repr.Ref
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		src, ext, ok := strings.Cut(t, ":")
		if !ok || src == "" || ext == "" {
			p := problem.New(problem.CodeInvalidParameter, "", "", "refs= entries must be source:external_id.")
			p.Errors = []problem.FieldError{{
				Parameter: "refs",
				Reason:    problem.ReasonInvalidFormat,
				Detail:    "expected source:external_id",
			}}
			return nil, p
		}
		refs = append(refs, repr.Ref{Source: src, ExternalID: ext})
	}
	if len(refs) > MaxBatchItems {
		p := problem.New(problem.CodeTooManyIDs, "", "", "refs= accepts at most 100 values.")
		p.Errors = []problem.FieldError{{
			Parameter: "refs",
			Reason:    problem.ReasonTooManyItems,
			Detail:    "maximum 100",
			Params:    &problem.FieldParams{MaxItems: problem.Ptr(MaxBatchItems)},
		}}
		return nil, p
	}
	return refs, nil
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, xs := range [][]string{a, b} {
		for _, x := range xs {
			if seen[x] {
				continue
			}
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
