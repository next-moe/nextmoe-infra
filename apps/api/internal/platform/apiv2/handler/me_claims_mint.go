package handler

import (
	"context"
	"maps"
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	catsvc "api/internal/platform/catalog/service"
)

func claimMintFields(displayName string, fields map[string]any, detail string) (map[string]any, *problem.Problem) {
	out := make(map[string]any, len(fields)+1)
	maps.Copy(out, fields)
	if displayName != "" {
		out[editspec.FieldWorkDisplayName] = displayName
	}
	if name, ok := out[editspec.FieldWorkDisplayName].(string); !ok || strings.TrimSpace(name) == "" {
		p := problem.New(problem.CodeValidationFailed, "", "", detail)
		p.Errors = []problem.FieldError{{Pointer: "/display_name", Reason: problem.ReasonRequired,
			Detail: "send display_name, or " + editspec.FieldWorkDisplayName + " inside field_values"}}
		return nil, p
	}
	return out, nil
}

func unionWorkLinks(fields map[string]any, refLinks []any) {
	if len(refLinks) == 0 {
		return
	}
	given, present := fields[editspec.FieldWorkLinks]
	existing, ok := given.([]any)
	if present && !ok {
		return
	}
	seen := make(map[string]struct{}, len(existing)+len(refLinks))
	out := make([]any, 0, len(existing)+len(refLinks))
	for _, el := range existing {
		if s, isStr := el.(string); isStr {
			seen[s] = struct{}{}
		}
		out = append(out, el)
	}
	for _, l := range refLinks {
		s, _ := l.(string)
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, l)
	}
	fields[editspec.FieldWorkLinks] = out
}

func (c *Catalog) mintClaim(ctx context.Context, site string, uid, product int64, refs []catsvc.ClaimRef, fields map[string]any, confirmDuplicates bool) (repr.ClaimRecord, error) {
	rating := int16(0)
	if _, given := fields[editspec.FieldWorkContentRating]; !given {
		rating = c.Claims.DeriveContentRating(ctx, refs)
	}
	res, err := c.Claims.SubmitWork(ctx, catsvc.SubmitWorkParams{
		Site: site, ProductWorkID: product, ActorUID: uid,
		ContentRating: rating, Fields: fields,
		Trusted:           actsAsTrusted(ctx),
		ConfirmDuplicates: confirmDuplicates,
	})
	if err != nil {
		return repr.ClaimRecord{}, claimWriteErr(err)
	}
	return repr.ClaimRecord{Object: "claim", ID: repr.ID(res.WorkID), State: res.ClaimState}, nil
}
