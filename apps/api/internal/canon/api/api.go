package api

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/canon/ent"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Work struct {
	ID     string `json:"id" doc:"Work id, a decimal string."`
	Medium string `json:"medium" enum:"game,novel,manga,anime,audio"`
}

type WorkInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type WorkOutput struct {
	Body Work
}

func New(client *ent.Client) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("NextMoe Catalog v3", "0.1.0"))
	huma.Register(api, huma.Operation{
		OperationID: "getWork",
		Method:      http.MethodGet,
		Path:        "/v3/catalog/works/{id}",
		Summary:     "Get one work",
		Tags:        []string{"works"},
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *WorkInput) (*WorkOutput, error) {
		w, err := client.Work.Get(ctx, in.ID)
		if ent.IsNotFound(err) {
			return nil, huma.Error404NotFound("work " + strconv.FormatInt(in.ID, 10) + " does not exist")
		}
		if err != nil {
			return nil, err
		}
		return &WorkOutput{Body: Work{ID: strconv.FormatInt(w.ID, 10), Medium: w.Medium}}, nil
	})
	return mux, api
}
