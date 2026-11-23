package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"api/internal/canon/ent"
	"api/internal/canon/merge"
	"api/internal/canon/perm"
	"api/internal/canon/takedown"

	"github.com/danielgtaylor/huma/v2"
)

type MergeInput struct {
	AuthInput
	Body struct {
		Entity   string `json:"entity" enum:"work,work_part,release,person,org"`
		Loser    string `json:"loser" doc:"The id that will redirect to the survivor."`
		Survivor string `json:"survivor"`
		Reason   string `json:"reason" minLength:"1" maxLength:"2000"`
	}
}

type UndoMergeInput struct {
	AuthInput
	ID int `path:"id" minimum:"1"`
}

type TakedownInput struct {
	AuthInput
	Body struct {
		NoticeID    string `json:"notice_id" minLength:"1" doc:"The trust inbox notice this answers."`
		Scope       string `json:"scope,omitempty" enum:"entity,asset,media_set,synopsis" default:"entity"`
		Entity      string `json:"entity" enum:"work,work_part,release,person,org"`
		EntityID    string `json:"entity_id"`
		Asset       string `json:"asset,omitempty" doc:"Asset hash, for scope=asset."`
		Reason      string `json:"reason" minLength:"1" maxLength:"2000"`
		ExcludeHost string `json:"exclude_host,omitempty" doc:"The rights holder's host, when they ask not to be crawled."`
	}
}

type RestoreInput struct {
	AuthInput
	ID   int `path:"id" minimum:"1"`
	Body struct {
		Note string `json:"note" minLength:"1" maxLength:"2000" doc:"Why, e.g. the counter-notice."`
	}
}

type CreatedOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

type DoneOutput struct {
	Body struct {
		State string `json:"state"`
	}
}

func (a *API) curation(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}
	huma.Register(h, huma.Operation{OperationID: "mergeEntities", Method: http.MethodPost, Path: "/v3/catalog/merges",
		Summary: "Merge two entities into one", Description: "Needs canon.merge. The loser's id redirects to the survivor; undoing restores both.",
		Tags: []string{"curation"}, Errors: errs, DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *MergeInput) (*CreatedOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Merge)
			if err != nil {
				return nil, err
			}
			loser, lerr := strconv.ParseInt(in.Body.Loser, 10, 64)
			survivor, serr := strconv.ParseInt(in.Body.Survivor, 10, 64)
			if lerr != nil || serr != nil || loser <= 0 || survivor <= 0 {
				return nil, huma.Error422UnprocessableEntity("loser and survivor are canon ids")
			}
			n, err := merge.Merge(ctx, a.client, in.Body.Entity, loser, survivor, s.key, in.Body.Reason)
			switch {
			case errors.Is(err, merge.ErrSame):
				return nil, huma.Error422UnprocessableEntity(err.Error())
			case errors.Is(err, merge.ErrHeld):
				return nil, huma.Error409Conflict(err.Error())
			case errors.Is(err, sql.ErrNoRows):
				return nil, huma.Error404NotFound(err.Error())
			case err != nil:
				return nil, err
			}
			out := &CreatedOutput{}
			out.Body.ID = strconv.Itoa(n)
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "undoMerge", Method: http.MethodPost, Path: "/v3/catalog/merges/{id}/undo",
		Summary: "Undo a merge", Description: "Needs canon.merge.", Tags: []string{"curation"}, Errors: errs},
		func(ctx context.Context, in *UndoMergeInput) (*DoneOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Merge)
			if err != nil {
				return nil, err
			}
			err = merge.Unmerge(ctx, a.client, in.ID, s.key)
			switch {
			case errors.Is(err, merge.ErrUndone):
				return nil, huma.Error409Conflict(err.Error())
			case ent.IsNotFound(err) || errors.Is(err, sql.ErrNoRows):
				return nil, huma.Error404NotFound("merge " + strconv.Itoa(in.ID) + " does not exist")
			case err != nil:
				return nil, err
			}
			out := &DoneOutput{}
			out.Body.State = "undone"
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "takeDown", Method: http.MethodPost, Path: "/v3/catalog/takedowns",
		Summary: "Take down an entity or one of its assets", Description: "Needs canon.takedown. Hides the target on every read face, holds it against agent writes and tells ingest.",
		Tags: []string{"curation"}, Errors: errs, DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *TakedownInput) (*CreatedOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Takedown)
			if err != nil {
				return nil, err
			}
			n, perr := strconv.ParseInt(in.Body.EntityID, 10, 64)
			if perr != nil || n <= 0 {
				return nil, huma.Error422UnprocessableEntity("entity_id is a canon id")
			}
			if in.Body.Scope == "asset" && in.Body.Asset == "" {
				return nil, huma.Error422UnprocessableEntity("scope=asset names the asset hash")
			}
			rows, err := a.client.QueryContext(ctx, `SELECT 1 FROM canon.`+in.Body.Entity+` WHERE id = $1`, n)
			if err != nil {
				return nil, err
			}
			exists := rows.Next()
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
			if !exists {
				return nil, huma.Error404NotFound(in.Body.Entity + " " + id(n) + " does not exist")
			}
			td, err := takedown.Take(ctx, a.client, takedown.Notice{NoticeID: in.Body.NoticeID, Scope: in.Body.Scope, Entity: in.Body.Entity,
				EntityID: n, Asset: in.Body.Asset, Reason: in.Body.Reason, By: s.key, ExcludeHost: in.Body.ExcludeHost})
			if err != nil {
				return nil, err
			}
			out := &CreatedOutput{}
			out.Body.ID = strconv.Itoa(td.ID)
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "restoreTakedown", Method: http.MethodPost, Path: "/v3/catalog/takedowns/{id}/restore",
		Summary: "Restore a takedown", Description: "Needs canon.takedown.", Tags: []string{"curation"}, Errors: errs},
		func(ctx context.Context, in *RestoreInput) (*DoneOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Takedown)
			if err != nil {
				return nil, err
			}
			err = takedown.Restore(ctx, a.client, in.ID, s.key, in.Body.Note)
			switch {
			case errors.Is(err, takedown.ErrRestored):
				return nil, huma.Error409Conflict(err.Error())
			case ent.IsNotFound(err):
				return nil, huma.Error404NotFound("takedown " + strconv.Itoa(in.ID) + " does not exist")
			case err != nil:
				return nil, err
			}
			out := &DoneOutput{}
			out.Body.State = "restored"
			return out, nil
		})
}
