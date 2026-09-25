package handler

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) ListCharacters(ctx context.Context, q collect.Query, f characterFilter) (repr.List[repr.Character], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Character]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	if q.Sort == "relevance" && f.Q == "" && !q.Batch {
		p := problem.New(problem.CodeInvalidParameter, "", "", "sort=relevance requires q=.")
		p.Errors = []problem.FieldError{{Parameter: "sort", Reason: problem.ReasonNotAllowedValue, Detail: "pass q="}}
		return repr.List[repr.Character]{}, p
	}
	if q.Batch && (f.Q != "" || characterSearchSort(q.Sort)) {
		name := "q"
		if f.Q == "" {
			name = "sort"
		}
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", name+"= cannot be combined with ids= or refs=.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonNotAllowedValue, Detail: "ids=/refs= is a batch lane"}}
		return repr.List[repr.Character]{}, p
	}
	if !characterIndexLane(q, f.Q) {
		return c.listCharactersRegistry(ctx, q, f)
	}
	return c.listCharactersSearch(ctx, q, f)
}

func (c *Catalog) listCharactersRegistry(ctx context.Context, q collect.Query, f characterFilter) (repr.List[repr.Character], error) {
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeCharacter)
	if err != nil {
		return repr.List[repr.Character]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Character{}, nil, 0, q, missing), nil
	}
	data, lerr := c.Public.CharactersList(ctx, ids, q.Cursor, listLimit(q),
		catsvc.CharacterListIncludeFrom(q.Include), q.NSFW,
		catsvc.CharacterTraitFilter{TraitIDs: f.TraitIDs, MatchAny: f.MatchAny, Genders: f.Genders}, q.IncludeTotal)
	if lerr != nil {
		var se *catsvc.SexualTraitError
		if errors.As(lerr, &se) {
			return repr.List[repr.Character]{}, sexualTraitProblem(se)
		}
		return repr.List[repr.Character]{}, listCursorErr(lerr)
	}
	items := make([]repr.Character, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, characterFromRow(it, q.Include))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) listCharactersSearch(ctx context.Context, q collect.Query, f characterFilter) (repr.List[repr.Character], error) {
	page, perr := searchPage(q)
	if perr != nil {
		return repr.List[repr.Character]{}, perr
	}
	sort := q.Sort
	if f.Q != "" && (sort == "" || sort == "id") {
		sort = "relevance"
	}
	data, err := searchCharacters(c, ctx, catsvc.CharactersSearchFilter{
		Q: f.Q, TraitIDs: f.TraitIDs, MatchAny: f.MatchAny, Genders: f.Genders,
		NSFW: q.NSFW, Sort: sort, Page: page, Limit: q.Limit,
		Include: catsvc.CharacterListIncludeFrom(q.Include),
	})
	if err != nil {
		var se *catsvc.SexualTraitError
		if errors.As(err, &se) {
			return repr.List[repr.Character]{}, sexualTraitProblem(se)
		}
		if errors.Is(err, catsvc.ErrSearchUnavailable) {
			return repr.List[repr.Character]{}, problem.New(problem.CodeServiceUnavailable, "", "", "character search is not bound.")
		}
		return repr.List[repr.Character]{}, err
	}
	items := make([]repr.Character, 0, len(data.Items))
	for _, it := range data.Items {
		items = append(items, characterFromRow(it, q.Include))
	}
	if q.Page > 0 {
		return finishPageList(items, data.Total), nil
	}
	var next *string
	if int64(page)*int64(data.Limit) < data.Total && len(data.Items) > 0 {
		enc := collect.EncodeCursor(strconv.Itoa(page + 1))
		next = &enc
	}
	out := repr.NewList(items, next)
	if q.IncludeTotal {
		n := data.Total
		out.Total = &n
	}
	return out, nil
}

var searchCharacters = func(c *Catalog, ctx context.Context, f catsvc.CharactersSearchFilter) (catsvc.CharactersSearchPage, error) {
	return c.Public.CharactersSearch(ctx, f)
}

func (c *Catalog) ListCreditNames(ctx context.Context, q collect.Query, nameQ string) (repr.List[repr.CreditName], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.CreditName]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeCreditName)
	if err != nil {
		return repr.List[repr.CreditName]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.CreditName{}, nil, 0, q, missing), nil
	}
	data, lerr := c.Public.NamesList(ctx, ids, catsvc.NormalizeNameQuery(nameQ), q.Cursor, listLimit(q))
	if lerr != nil {
		return repr.List[repr.CreditName]{}, listCursorErr(lerr)
	}
	items := make([]repr.CreditName, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, creditNameFromRow(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) ListPersons(ctx context.Context, q collect.Query) (repr.List[repr.Person], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Person]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypePerson)
	if err != nil {
		return repr.List[repr.Person]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Person{}, nil, 0, q, missing), nil
	}
	data, lerr := c.Public.PersonsList(ctx, ids, q.Cursor, listLimit(q))
	if lerr != nil {
		return repr.List[repr.Person]{}, listCursorErr(lerr)
	}
	items := make([]repr.Person, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, personFromRow(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) ListTraits(ctx context.Context, q collect.Query, f traitFilter) (repr.List[repr.Trait], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Trait]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, entityTypeNone)
	if err != nil {
		return repr.List[repr.Trait]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Trait{}, nil, 0, q, missing), nil
	}
	var counts map[int64]int64
	if slices.Contains(q.Include, "character_count") {
		var cerr error
		counts, cerr = characterTraitCounts(c, ctx, q.NSFW)
		if cerr != nil {
			return repr.List[repr.Trait]{}, traitCountUnavailable(cerr)
		}
	}
	data, lerr := c.Public.TraitsList(ctx, ids, q.Cursor, listLimit(q), q.NSFW,
		catsvc.TraitListFilter{ParentID: f.ParentID, GroupID: f.GroupID, Root: f.Root}, q.Include)
	if lerr != nil {
		var se *catsvc.SexualTraitError
		if errors.As(lerr, &se) {
			return repr.List[repr.Trait]{}, sexualTraitProblem(se)
		}
		return repr.List[repr.Trait]{}, listCursorErr(lerr)
	}
	items := make([]repr.Trait, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, traitFromRow(it, q.Include))
		seen[it.ID] = true
	}
	attachTraitCharacterCounts(items, counts)
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

var characterTraitCounts = func(c *Catalog, ctx context.Context, nsfw bool) (map[int64]int64, error) {
	if c == nil || c.Public == nil {
		return nil, catsvc.ErrSearchUnavailable
	}
	return c.Public.CharacterTraitCounts(ctx, nsfw)
}

func traitCountUnavailable(err error) *problem.Problem {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p
	}
	return problem.New(problem.CodeServiceUnavailable, "", "", "character counts are unavailable.")
}

func attachTraitCharacterCounts(items []repr.Trait, counts map[int64]int64) {
	if counts == nil {
		return
	}
	for i := range items {
		id, _ := repr.ParseID(items[i].ID)
		n := int(counts[id])
		items[i].CharacterCount = &n
	}
}

func sexualTraitProblem(se *catsvc.SexualTraitError) *problem.Problem {
	msg := "trait " + strconv.FormatInt(se.ID, 10) + " is sexual; nsfw=true is required"
	p := problem.New(problem.CodeInvalidParameter, "", "", msg)
	p.Errors = []problem.FieldError{{
		Parameter: se.Param(),
		Reason:    problem.ReasonNotAllowedValue,
		Detail:    msg,
	}}
	return p
}

func listLimit(q collect.Query) int {
	if q.Batch {
		return 100
	}
	return q.Limit
}
