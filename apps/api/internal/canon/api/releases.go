package api

import (
	"context"
	"net/http"

	"api/internal/canon/ent"
	"api/internal/canon/ent/bookrelease"
	"api/internal/canon/ent/release"

	"github.com/danielgtaylor/huma/v2"
)

type ReleaseOutput struct{ Body Release }

type ReleaseListInput struct {
	ISBN string `query:"isbn" doc:"ISBN-13, hyphens allowed."`
	JAN  string `query:"jan"`
	IDs  string `query:"ids" doc:"Comma-separated ids, at most 100."`
}

type ReleaseList struct {
	Items []Release `json:"items"`
}

type ReleaseListOutput struct{ Body ReleaseList }

func (a *API) releases(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "getRelease", Method: http.MethodGet, Path: "/v3/catalog/releases/{id}",
		Summary: "Get one release", Tags: []string{"releases"}, Errors: []int{http.StatusNotFound, http.StatusGone}},
		func(ctx context.Context, in *IDInput) (*ReleaseOutput, error) {
			r, err := a.releaseQuery().Where(release.ID(in.ID)).Only(ctx)
			if err := a.gone(ctx, "release", in.ID, err); err != nil {
				return nil, err
			}
			return &ReleaseOutput{Body: shapeRelease(r)}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listReleases", Method: http.MethodGet, Path: "/v3/catalog/releases",
		Summary: "Find releases by ISBN, JAN or ids", Tags: []string{"releases"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *ReleaseListInput) (*ReleaseListOutput, error) {
			q := a.releaseQuery()
			switch {
			case in.ISBN != "":
				q = q.Where(release.HasBookWith(bookrelease.Isbn13(digits(in.ISBN))))
			case in.JAN != "":
				q = q.Where(release.HasBookWith(bookrelease.Jan(digits(in.JAN))))
			case in.IDs != "":
				ids, err := parseIDs(in.IDs)
				if err != nil {
					return nil, err
				}
				q = q.Where(release.IDIn(ids...))
			default:
				return nil, huma.Error400BadRequest("give isbn=, jan= or ids=")
			}
			rs, err := q.Limit(maxBatch).All(ctx)
			if err != nil {
				return nil, err
			}
			ids := make([]int64, len(rs))
			for i, r := range rs {
				ids[i] = r.ID
			}
			gone, err := a.goneSet(ctx, "release", ids)
			if err != nil {
				return nil, err
			}
			out := ReleaseList{Items: []Release{}}
			for _, r := range rs {
				if !gone[r.ID] {
					out.Items = append(out.Items, shapeRelease(r))
				}
			}
			return &ReleaseListOutput{Body: out}, nil
		})
}

func (a *API) releaseQuery() *ent.ReleaseQuery {
	return a.client.Release.Query().WithBook().WithContents()
}

func shapeRelease(r *ent.Release) Release {
	out := Release{ID: id(r.ID), Lang: r.Lang, EditionKind: r.EditionKind, Format: r.Format, Released: r.Released,
		ReleaseStatus: r.ReleaseStatus, OfficialURL: r.OfficialURL, Contents: []Content{}, UpdatedAt: r.UpdatedAt}
	if b := r.Edges.Book; b != nil {
		out.Book = &BookRelease{ISBN13: b.Isbn13, JAN: b.Jan, LabelNumber: b.LabelNumber, Pages: b.Pages,
			PublisherID: idp(b.PublisherID), LabelID: idp(b.LabelID)}
	}
	for _, c := range r.Edges.Contents {
		out.Contents = append(out.Contents, Content{WorkID: id(c.WorkID), PartID: idp(c.PartID), Portion: c.Portion})
	}
	return out
}

func digits(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}
