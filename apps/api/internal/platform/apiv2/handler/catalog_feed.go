package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
)

func (c *Catalog) ListChanges(ctx context.Context, q collect.Query) (repr.List[repr.Change], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Change]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	if q.Batch {
		return repr.List[repr.Change]{}, feedNoBatch("changes")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	data, err := c.Public.Changes(ctx, q.Cursor, limit)
	if err != nil {
		return repr.List[repr.Change]{}, listCursorErr(err)
	}
	items := make([]repr.Change, 0, len(data.Items))
	for _, it := range data.Items {
		target := it.EntityType
		if target == "" {
			target = "work"
		}
		if target == "name" {
			target = "credit_name"
		}
		if target == "label" {
			target = "company"
		}
		ch := repr.Change{
			Object: "change", TargetObject: target, ID: repr.ID(it.ID), UpdatedAt: it.Updated,
		}
		if it.Gone {
			gone := true
			ch.Gone = &gone
		}
		items = append(items, ch)
	}
	var next *string
	if len(data.Items) == limit && data.NextCursor != "" {
		s := data.NextCursor
		next = &s
	}
	var total int64
	if q.IncludeTotal {
		n, terr := c.Public.ChangesTotal(ctx)
		if terr != nil {
			return repr.List[repr.Change]{}, terr
		}
		total = n
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) ListRedirects(ctx context.Context, q collect.Query, object string) (repr.List[repr.Redirect], error) {
	if c == nil || c.Resolve == nil {
		return repr.List[repr.Redirect]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	if q.Batch {
		return repr.List[repr.Redirect]{}, feedNoBatch("redirects")
	}
	var filter *int16
	if object != "" {
		et, ok := objectEntityType(object)
		if !ok {
			p := problem.New(problem.CodeUnknownEnumValue, "", "", "object is not in the closed vocabulary.")
			p.Errors = []problem.FieldError{{
				Parameter: "object", Reason: problem.ReasonUnknownValue,
				Detail: "allowed values: work, release, character, credit_name, person, company, tag, engine",
				Params: &problem.FieldParams{Allowed: &[]string{"work", "release", "character", "credit_name", "person", "company", "tag", "engine"}},
			}}
			return repr.List[repr.Redirect]{}, p
		}
		filter = &et
	}
	cur, err := decodeRedirectInner(q.Cursor)
	if err != nil {
		return repr.List[repr.Redirect]{}, collectInvalidCursor()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, nextCur, err := c.Resolve.RedirectsSince(ctx, filter, cur, limit)
	if err != nil {
		return repr.List[repr.Redirect]{}, err
	}
	items := make([]repr.Redirect, 0, len(rows))
	for _, it := range rows {
		target, ok := entityTypeObject(it.EntityType)
		if !ok {
			continue
		}
		items = append(items, repr.Redirect{
			Object: "redirect", TargetObject: target,
			OldID: repr.ID(it.OldID), CurrentID: repr.ID(it.CurrentID),
			MergedAt: it.MergedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	var next *string
	if len(rows) == limit {
		s := encodeRedirectInner(nextCur)
		next = &s
	}
	var total int64
	if q.IncludeTotal {
		n, terr := c.Resolve.RedirectsTotal(ctx, filter)
		if terr != nil {
			return repr.List[repr.Redirect]{}, terr
		}
		total = n
	}
	return finishList(items, next, total, q, nil), nil
}

func feedNoBatch(name string) *problem.Problem {
	p := problem.New(problem.CodeInvalidParameter, "", "", name+" is a feed and does not take ids= or refs=.")
	p.Errors = []problem.FieldError{{Parameter: "ids", Reason: problem.ReasonInvalidFormat, Detail: "omit ids= and refs= on this collection"}}
	return p
}

func objectEntityType(object string) (int16, bool) {
	switch object {
	case "person":
		return catmodel.EntityTypePerson, true
	case "credit_name":
		return catmodel.EntityTypeCreditName, true
	case "company":
		return catmodel.EntityTypeLabel, true
	case "character":
		return catmodel.EntityTypeCharacter, true
	case "work":
		return catmodel.EntityTypeWork, true
	case "release":
		return catmodel.EntityTypeRelease, true
	case "tag":
		return catmodel.EntityTypeTag, true
	case "engine":
		return catmodel.EntityTypeEngine, true
	default:
		return 0, false
	}
}

func entityTypeObject(t int16) (string, bool) {
	switch t {
	case catmodel.EntityTypePerson:
		return "person", true
	case catmodel.EntityTypeCreditName:
		return "credit_name", true
	case catmodel.EntityTypeLabel:
		return "company", true
	case catmodel.EntityTypeCharacter:
		return "character", true
	case catmodel.EntityTypeWork:
		return "work", true
	case catmodel.EntityTypeRelease:
		return "release", true
	case catmodel.EntityTypeTag:
		return "tag", true
	case catmodel.EntityTypeEngine:
		return "engine", true
	default:
		return "", false
	}
}

func encodeRedirectInner(c repository.RedirectCursor) string {
	raw := fmt.Sprintf("%d:%d:%d", c.MergedAt.UnixNano(), c.EntityType, c.OldID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeRedirectInner(s string) (repository.RedirectCursor, error) {
	if s == "" {
		return repository.RedirectCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repository.RedirectCursor{}, err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 {
		return repository.RedirectCursor{}, fmt.Errorf("bad cursor arity")
	}
	nano, err1 := strconv.ParseInt(parts[0], 10, 64)
	et, err2 := strconv.ParseInt(parts[1], 10, 16)
	old, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return repository.RedirectCursor{}, fmt.Errorf("bad cursor fields")
	}
	return repository.RedirectCursor{MergedAt: time.Unix(0, nano), EntityType: int16(et), OldID: old}, nil
}
