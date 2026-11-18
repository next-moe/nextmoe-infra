package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type playtimeSourcesInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type playtimeSourcesOutput struct {
	Body repr.List[repr.PlaytimeSource]
}
type deletePlaytimeSourceInput struct {
	WorkID   string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	ClientID string `path:"client_id" minLength:"1" maxLength:"50" pattern:"^[A-Za-z0-9._-]+$" doc:"The app whose row to delete, as client_id in the sources list."`
}

const playtimeSourcesGate = "Requires a user access token from a first-party site app; any other app gets 403 PERMISSION_REQUIRED, because the list names the other apps the user runs."

func registerPlaytimeSources(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listMyPlaytimeSources", Method: http.MethodGet, Path: "/v2/me/playtimes/{work_id}/sources",
		Summary: "List which apps reported my playtime on one work",
		Description: "One row per app, most minutes first. GET /v2/me/playtimes/{work_id} answers the most minutes among these rows. " +
			"An empty list when no app reported. " + playtimeSourcesGate,
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyPlaytimeSources(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyPlaytimeSource", Method: http.MethodDelete, Path: "/v2/me/playtimes/{work_id}/sources/{client_id}",
		Summary: "Delete one app's playtime on one work",
		Description: "Deletes the row the named app reported, whichever first-party app asks. 404 when that app has no row on the work. " +
			"Once no app's row is left, the work's daily play activity items are withdrawn. 204 with no body. " + playtimeSourcesGate,
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyPlaytimeSource(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyPlaytimeSources", Method: http.MethodDelete, Path: "/v2/me/playtimes/{work_id}/sources",
		Summary: "Delete every app's playtime on one work",
		Description: "Deletes every app's row on the work and withdraws its daily play activity items. Idempotent: 204 also when nothing was recorded. " +
			playtimeSourcesGate,
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyPlaytimeSources(cat))
}

func listMyPlaytimeSources(cat *Catalog) func(context.Context, *playtimeSourcesInput) (*playtimeSourcesOutput, error) {
	return func(ctx context.Context, in *playtimeSourcesInput) (*playtimeSourcesOutput, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		list, err := cat.ListPlaytimeSources(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &playtimeSourcesOutput{Body: list}, nil
	}
}

func deleteMyPlaytimeSource(cat *Catalog) func(context.Context, *deletePlaytimeSourceInput) (*struct{}, error) {
	return func(ctx context.Context, in *deletePlaytimeSourceInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeletePlaytimeSource(ctx, id, in.ClientID); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func deleteMyPlaytimeSources(cat *Catalog) func(context.Context, *playtimeSourcesInput) (*struct{}, error) {
	return func(ctx context.Context, in *playtimeSourcesInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeletePlaytimeSources(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func (c *Catalog) ListPlaytimeSources(ctx context.Context, workID int64) (repr.List[repr.PlaytimeSource], error) {
	if c == nil || c.Playtime == nil || c.AppNameOf == nil {
		return repr.List[repr.PlaytimeSource]{}, problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, err := requireFirstPartyUser(ctx)
	if err != nil {
		return repr.List[repr.PlaytimeSource]{}, err
	}
	rows, err := c.Playtime.ListSources(ctx, uid, workID)
	if err != nil {
		return repr.List[repr.PlaytimeSource]{}, playtimeErr(err)
	}
	caller := clientIDFrom(ctx)
	items := make([]repr.PlaytimeSource, 0, len(rows))
	for _, r := range rows {
		name, err := c.AppNameOf(ctx, r.ClientID)
		if err != nil {
			return repr.List[repr.PlaytimeSource]{}, err
		}
		src := repr.PlaytimeSource{
			Object: "playtime_source", ClientID: r.ClientID, Minutes: r.Minutes,
			UpdatedAt: rfc3339(r.UpdatedAt), Self: r.ClientID == caller,
		}
		if name != "" {
			src.AppName = &name
		}
		if r.LastPlayedAt != nil {
			at := rfc3339(*r.LastPlayedAt)
			src.LastPlayedAt = &at
		}
		items = append(items, src)
	}
	return repr.NewList(items, nil), nil
}

func (c *Catalog) DeletePlaytimeSource(ctx context.Context, workID int64, clientID string) error {
	if c == nil || c.Playtime == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, err := requireFirstPartyUser(ctx)
	if err != nil {
		return err
	}
	return playtimeErr(c.Playtime.DeleteSource(ctx, uid, workID, clientID))
}

func (c *Catalog) DeletePlaytimeSources(ctx context.Context, workID int64) error {
	if c == nil || c.Playtime == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, err := requireFirstPartyUser(ctx)
	if err != nil {
		return err
	}
	return playtimeErr(c.Playtime.DeleteSources(ctx, uid, workID))
}

var _ = catsvc.ErrPlaytimeSourceNotFound
