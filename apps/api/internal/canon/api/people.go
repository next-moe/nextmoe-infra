package api

import (
	"context"
	"net/http"

	"api/internal/canon/ent"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/org"
	"api/internal/canon/ent/person"
	"api/internal/canon/ent/personalias"

	"github.com/danielgtaylor/huma/v2"
)

type PersonOutput struct{ Body Person }

type PersonListInput struct {
	AliasIDs string `query:"alias_ids" doc:"Alias ids, which include every legacy credit-name id (D19); at most 100."`
	IDs      string `query:"ids"`
}

type PersonList struct {
	Items []Person `json:"items"`
}

type PersonListOutput struct{ Body PersonList }

type OrgOutput struct{ Body Org }

type OrgListInput struct {
	Kind   string `query:"kind" enum:"company,brand,label,magazine,platform,circle,translation_group," doc:"Only organizations of this kind."`
	IDs    string `query:"ids" doc:"Comma-separated ids, at most 100; does not paginate."`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"100"`
}

type OrgList struct {
	Items      []Org  `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type OrgListOutput struct{ Body OrgList }

func (a *API) people(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "getPerson", Method: http.MethodGet, Path: "/v3/catalog/persons/{id}",
		Summary: "Get one person with every alias and credit", Tags: []string{"persons"}, Errors: []int{http.StatusNotFound, http.StatusGone}},
		func(ctx context.Context, in *IDInput) (*PersonOutput, error) {
			p, err := a.personQuery().Where(person.ID(in.ID)).Only(ctx)
			if err := a.gone(ctx, "person", in.ID, err); err != nil {
				return nil, err
			}
			return &PersonOutput{Body: shapePerson(p)}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listPersons", Method: http.MethodGet, Path: "/v3/catalog/persons",
		Summary: "Resolve persons by alias ids or ids", Tags: []string{"persons"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *PersonListInput) (*PersonListOutput, error) {
			q := a.personQuery()
			switch {
			case in.AliasIDs != "":
				ids, err := parseIDs(in.AliasIDs)
				if err != nil {
					return nil, err
				}
				q = q.Where(person.HasAliasesWith(personalias.IDIn(ids...)))
			case in.IDs != "":
				ids, err := parseIDs(in.IDs)
				if err != nil {
					return nil, err
				}
				q = q.Where(person.IDIn(ids...))
			default:
				return nil, huma.Error400BadRequest("give alias_ids= or ids=")
			}
			ps, err := q.Limit(maxBatch).All(ctx)
			if err != nil {
				return nil, err
			}
			ids := make([]int64, len(ps))
			for i, p := range ps {
				ids[i] = p.ID
			}
			gone, err := a.goneSet(ctx, "person", ids)
			if err != nil {
				return nil, err
			}
			out := PersonList{Items: []Person{}}
			for _, p := range ps {
				if !gone[p.ID] {
					out.Items = append(out.Items, shapePerson(p))
				}
			}
			return &PersonListOutput{Body: out}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "getOrg", Method: http.MethodGet, Path: "/v3/catalog/orgs/{id}",
		Summary: "Get one organization", Tags: []string{"orgs"}, Errors: []int{http.StatusNotFound, http.StatusGone}},
		func(ctx context.Context, in *IDInput) (*OrgOutput, error) {
			o, err := a.client.Org.Query().Where(org.ID(in.ID)).WithNames().Only(ctx)
			if err := a.gone(ctx, "org", in.ID, err); err != nil {
				return nil, err
			}
			return &OrgOutput{Body: shapeOrg(o)}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listOrgs", Method: http.MethodGet, Path: "/v3/catalog/orgs",
		Summary: "List organizations", Tags: []string{"orgs"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *OrgListInput) (*OrgListOutput, error) {
			ids, err := parseIDs(in.IDs)
			if err != nil {
				return nil, err
			}
			q := a.client.Org.Query().WithNames().Order(ent.Asc(org.FieldID))
			if in.Kind != "" {
				q = q.Where(org.Kind(in.Kind))
			}
			if ids != nil {
				q = q.Where(org.IDIn(ids...))
			} else {
				after, err := decodeCursor(in.Cursor)
				if err != nil {
					return nil, err
				}
				q = q.Where(org.IDGT(after)).Limit(in.Limit + 1)
			}
			rows, err := q.All(ctx)
			if err != nil {
				return nil, err
			}
			out := OrgList{Items: []Org{}}
			if ids == nil && len(rows) > in.Limit {
				rows = rows[:in.Limit]
				out.NextCursor = encodeCursor(rows[len(rows)-1].ID)
			}
			all := make([]int64, len(rows))
			for i, o := range rows {
				all[i] = o.ID
			}
			gone, err := a.goneSet(ctx, "org", all)
			if err != nil {
				return nil, err
			}
			for _, o := range rows {
				if !gone[o.ID] {
					out.Items = append(out.Items, shapeOrg(o))
				}
			}
			return &OrgListOutput{Body: out}, nil
		})
}

func shapeOrg(o *ent.Org) Org {
	out := Org{ID: id(o.ID), Kind: o.Kind, ParentID: idp(o.ParentID), Names: []OrgName{}}
	for _, n := range o.Edges.Names {
		out.Names = append(out.Names, OrgName{Text: n.Text, Lang: n.Lang})
	}
	return out
}

func (a *API) personQuery() *ent.PersonQuery {
	return a.client.Person.Query().WithAliases(func(q *ent.PersonAliasQuery) {
		q.WithCredits(func(c *ent.CreditQuery) { c.WithAlias().Order(ent.Asc(credit.FieldID)) })
	})
}

func shapePerson(p *ent.Person) Person {
	out := Person{ID: id(p.ID), PrimaryAliasID: idp(p.PrimaryAliasID), Aliases: []Alias{}, Credits: []Credit{}}
	for _, al := range p.Edges.Aliases {
		out.Aliases = append(out.Aliases, Alias{ID: id(al.ID), Name: al.Name, Latin: al.Latin, Lang: al.Lang})
		for _, c := range al.Edges.Credits {
			out.Credits = append(out.Credits, shapeCredit(c))
		}
	}
	return out
}
