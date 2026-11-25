package api

import (
	"context"
	"net/http"
	"sort"

	"api/internal/canon/ent"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/releasecontent"
	"api/internal/canon/ent/work"

	"github.com/danielgtaylor/huma/v2"
)

type WorkOutput struct{ Body Work }

type WorkList struct {
	Items      []Work `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type WorkListInput struct {
	Medium string `query:"medium" enum:"game,novel,manga,anime,audio," doc:"Only works of this medium."`
	IDs    string `query:"ids" doc:"Comma-separated ids, at most 100; does not paginate."`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type WorkListOutput struct{ Body WorkList }

func (a *API) works(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "getWork", Method: http.MethodGet, Path: "/v3/catalog/works/{id}",
		Summary: "Get one work", Tags: []string{"works"}, Errors: []int{http.StatusNotFound, http.StatusGone}},
		func(ctx context.Context, in *IDInput) (*WorkOutput, error) {
			w, err := a.workQuery().Where(work.ID(in.ID)).Only(ctx)
			if err := a.gone(ctx, "work", in.ID, err); err != nil {
				return nil, err
			}
			out, err := a.shapeWorks(ctx, []*ent.Work{w})
			if err != nil {
				return nil, err
			}
			return &WorkOutput{Body: out[0]}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listWorks", Method: http.MethodGet, Path: "/v3/catalog/works",
		Summary: "List works", Tags: []string{"works"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *WorkListInput) (*WorkListOutput, error) {
			ids, err := parseIDs(in.IDs)
			if err != nil {
				return nil, err
			}
			q := a.workQuery()
			if in.Medium != "" {
				q = q.Where(work.Medium(in.Medium))
			}
			if ids != nil {
				q = q.Where(work.IDIn(ids...))
			} else {
				after, err := decodeCursor(in.Cursor)
				if err != nil {
					return nil, err
				}
				q = q.Where(work.IDGT(after)).Order(ent.Asc(work.FieldID)).Limit(in.Limit + 1)
			}
			ws, err := q.All(ctx)
			if err != nil {
				return nil, err
			}
			next := ""
			if ids == nil && len(ws) > in.Limit {
				ws = ws[:in.Limit]
				next = encodeCursor(ws[len(ws)-1].ID)
			}
			gone, err := a.goneSet(ctx, "work", workIDs(ws))
			if err != nil {
				return nil, err
			}
			visible := ws[:0]
			for _, w := range ws {
				if !gone[w.ID] {
					visible = append(visible, w)
				}
			}
			items, err := a.shapeWorks(ctx, visible)
			if err != nil {
				return nil, err
			}
			return &WorkListOutput{Body: WorkList{Items: items, NextCursor: next}}, nil
		})
}

func (a *API) workQuery() *ent.WorkQuery {
	return a.client.Work.Query().WithTitles().WithBook().WithParts().
		WithCredits(func(q *ent.CreditQuery) { q.WithAlias().Order(ent.Asc(credit.FieldPosition), ent.Asc(credit.FieldID)) })
}

func workIDs(ws []*ent.Work) []int64 {
	out := make([]int64, len(ws))
	for i, w := range ws {
		out[i] = w.ID
	}
	return out
}

func (a *API) shapeWorks(ctx context.Context, ws []*ent.Work) ([]Work, error) {
	contents, err := a.client.ReleaseContent.Query().Where(releasecontent.WorkIDIn(workIDs(ws)...)).All(ctx)
	if err != nil {
		return nil, err
	}
	byPart := map[int64][]string{}
	for _, c := range contents {
		if c.PartID != nil {
			byPart[*c.PartID] = append(byPart[*c.PartID], id(c.ReleaseID))
		}
	}
	out := make([]Work, 0, len(ws))
	for _, w := range ws {
		v := Work{ID: id(w.ID), Medium: w.Medium, Status: w.Status, ContentRating: w.ContentRating, DisplaySafety: w.DisplaySafety, Olang: w.Olang,
			Titles: []Title{}, Parts: []Part{}, Credits: []Credit{}, UpdatedAt: w.UpdatedAt}
		titles := w.Edges.Titles
		sort.SliceStable(titles, func(i, j int) bool { return titles[i].Position < titles[j].Position })
		for _, t := range titles {
			v.Titles = append(v.Titles, Title{Text: t.Text, Lang: t.Lang, Kind: t.Kind})
		}
		if b := w.Edges.Book; b != nil {
			v.Book = &BookWork{Category: b.Category}
		}
		parts := w.Edges.Parts
		sort.SliceStable(parts, func(i, j int) bool { return ordinal(parts[i]) < ordinal(parts[j]) })
		for _, p := range parts {
			rel := byPart[p.ID]
			if rel == nil {
				rel = []string{}
			}
			v.Parts = append(v.Parts, Part{ID: id(p.ID), ParentID: idp(p.ParentID), Kind: p.Kind, Ordinal: p.Ordinal,
				Label: p.Label, Released: p.Released, ReleaseIDs: rel})
		}
		for _, c := range w.Edges.Credits {
			v.Credits = append(v.Credits, shapeCredit(c))
		}
		out = append(out, v)
	}
	return out, nil
}

func ordinal(p *ent.WorkPart) float64 {
	if p.Ordinal == nil {
		return 1e12 + float64(p.ID)
	}
	return *p.Ordinal
}
