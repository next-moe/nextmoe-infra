package api

import (
	"context"
	"net/http"
	"strings"

	"api/internal/canon/ent"
	"api/internal/canon/ent/predicate"
	"api/internal/canon/ent/xreflink"
	"api/internal/canon/xref"

	"github.com/danielgtaylor/huma/v2"
)

type XrefInput struct {
	Refs   string `query:"refs" doc:"Comma-separated source:external_id, e.g. vndb:v17,bangumi:1234; at most 100."`
	Entity string `query:"entity" enum:"work," doc:"With ids=, the reverse lookup."`
	IDs    string `query:"ids"`
}

type Xref struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Entity     string `json:"entity"`
	EntityID   string `json:"entity_id"`
	URL        string `json:"url"`
}

type XrefList struct {
	Items []Xref `json:"items"`
}

type XrefOutput struct{ Body XrefList }

func (a *API) xrefs(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "listXrefs", Method: http.MethodGet, Path: "/v3/catalog/xref",
		Summary:     "Third-party database ids",
		Description: "A read model outside canon (D22): VNDB, Bangumi, ErogameScape and HowLongToBeat ids mapped to catalog ids. The entity may still be served by v2.",
		Tags:        []string{"xref"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *XrefInput) (*XrefOutput, error) {
			q := a.client.XrefLink.Query()
			switch {
			case in.Refs != "":
				refs := strings.Split(in.Refs, ",")
				if len(refs) > maxBatch {
					return nil, huma.Error400BadRequest("refs= takes at most 100")
				}
				var any []predicate.XrefLink
				for _, r := range refs {
					src, ext, ok := strings.Cut(strings.TrimSpace(r), ":")
					if !ok || ext == "" {
						return nil, huma.Error400BadRequest("refs= wants source:external_id")
					}
					any = append(any, xreflink.And(xreflink.Source(src), xreflink.ExternalID(ext)))
				}
				q = q.Where(xreflink.Or(any...))
			case in.Entity != "" && in.IDs != "":
				ids, err := parseIDs(in.IDs)
				if err != nil {
					return nil, err
				}
				q = q.Where(xreflink.Entity(in.Entity), xreflink.EntityIDIn(ids...))
			default:
				return nil, huma.Error400BadRequest("give refs= or entity= with ids=")
			}
			rows, err := q.Order(ent.Asc(xreflink.FieldSource), ent.Asc(xreflink.FieldExternalID)).Limit(4 * maxBatch).All(ctx)
			if err != nil {
				return nil, err
			}
			out := XrefList{Items: []Xref{}}
			for _, r := range rows {
				out.Items = append(out.Items, Xref{Source: r.Source, ExternalID: r.ExternalID, Entity: r.Entity, EntityID: id(r.EntityID), URL: xref.URL(r.Source, r.ExternalID)})
			}
			return &XrefOutput{Body: out}, nil
		})
}
