package handler

import (
	"context"
	"errors"
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
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeCharacter)
	if err != nil {
		return repr.List[repr.Character]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Character{}, nil, 0, q, missing), nil
	}
	data, lerr := c.Public.CharactersList(ctx, ids, q.Cursor, listLimit(q),
		catsvc.CharacterListIncludeFrom(q.Include), q.NSFW,
		catsvc.CharacterTraitFilter{TraitIDs: f.TraitIDs, MatchAny: f.MatchAny}, q.IncludeTotal)
	if lerr != nil {
		var se *catsvc.SexualTraitError
		if errors.As(lerr, &se) {
			p := problem.New(problem.CodeInvalidParameter, "", "",
				"trait "+strconv.FormatInt(se.ID, 10)+" is sexual; nsfw=true is required")
			p.Errors = []problem.FieldError{{
				Parameter: "trait_id",
				Reason:    problem.ReasonNotAllowedValue,
				Detail:    "trait " + strconv.FormatInt(se.ID, 10) + " is sexual; nsfw=true is required",
			}}
			return repr.List[repr.Character]{}, p
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

func (c *Catalog) ListTraits(ctx context.Context, q collect.Query) (repr.List[repr.Trait], error) {
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
	data, lerr := c.Public.TraitsList(ctx, ids, q.Cursor, listLimit(q))
	if lerr != nil {
		return repr.List[repr.Trait]{}, listCursorErr(lerr)
	}
	items := make([]repr.Trait, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, traitFromRow(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func listLimit(q collect.Query) int {
	if q.Batch {
		return 100
	}
	return q.Limit
}
