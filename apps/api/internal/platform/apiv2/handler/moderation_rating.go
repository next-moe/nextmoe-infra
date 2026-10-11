package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

func registerModerationRatings(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "deleteModerationRating", Method: http.MethodDelete, Path: "/v2/moderation/ratings/{id}",
		Summary:     "Delete any rating",
		Description: "Removes the rating from every site at once; the deletion reaches /v2/ratings/changes as a gone entry and the work's nextmoe score follows. 204 with no body. Requires a user access token whose holder moderates.",
		Tags:        []string{"moderation"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusServiceUnavailable),
		DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteModerationRating(cat))
}

func deleteModerationRating(cat *Catalog) func(context.Context, *ratingPathInput) (*struct{}, error) {
	return func(ctx context.Context, in *ratingPathInput) (*struct{}, error) {
		if err := requireModerator(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if cat == nil || cat.Ratings == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound."))
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		if err := cat.Ratings.ModerationDelete(ctx, id); err != nil {
			return nil, catalogErr(ctx, ratingErr(err, ""))
		}
		return &struct{}{}, nil
	}
}
