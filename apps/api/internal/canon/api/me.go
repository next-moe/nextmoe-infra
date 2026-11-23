package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/pkg/utils"

	"github.com/danielgtaylor/huma/v2"
)

// The library's work states live in catalog_user_work_state, which v2 also
// writes during the transition (10 §3); the codes are the legacy ones and
// already media-neutral. completion is game-only and left as it is.
var stateCodes = map[string]int16{"wish": 1, "in_progress": 2, "done": 3, "on_hold": 4, "dropped": 5}

func stateName(code int16) string {
	for k, v := range stateCodes {
		if v == code {
			return k
		}
	}
	return ""
}

type WorkState struct {
	WorkID    string    `json:"work_id"`
	State     string    `json:"state" enum:"wish,in_progress,done,on_hold,dropped"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MeWorkInput struct {
	AuthInput
	ID int64 `path:"id" minimum:"1"`
}

type PutStateInput struct {
	AuthInput
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		State      string     `json:"state" enum:"wish,in_progress,done,on_hold,dropped"`
		ObservedAt *time.Time `json:"observed_at,omitempty" doc:"When the client last saw this work's state; a later server write wins and answers 409 with it."`
	}
}

type DeleteStateInput struct {
	AuthInput
	ID         int64  `path:"id" minimum:"1"`
	ObservedAt string `query:"observed_at" doc:"RFC 3339; as on PUT."`
}

type PutProgressInput struct {
	AuthInput
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		State string `json:"state" enum:"done,none" doc:"none removes the progress."`
	}
}

type WorkStateOutput struct{ Body WorkState }

type MeChange struct {
	Kind      string    `json:"kind" enum:"work_state,part_progress,rating"`
	SubjectID string    `json:"subject_id" doc:"The work for work_state and rating, the part for part_progress."`
	State     *string   `json:"state" doc:"null when removed, and always for a rating."`
	Rating    *MyRating `json:"rating,omitempty" doc:"The rating as it is now; absent when removed."`
	At        time.Time `json:"at"`
}

type MeChangesInput struct {
	AuthInput
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"500" default:"200"`
}

type MeChangeList struct {
	Items      []MeChange `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

type MeChangesOutput struct{ Body MeChangeList }

func (a *API) user(ctx context.Context, header string) (int64, error) {
	claims, err := a.viewer(ctx, header)
	if err != nil {
		return 0, err
	}
	return int64(claims.ID), nil
}

func (a *API) viewer(ctx context.Context, header string) (*utils.TokenClaims, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || a.verifier == nil {
		return nil, huma.Error401Unauthorized("a bearer token is required")
	}
	claims, err := a.verifier.Parse(ctx, token)
	if err != nil || claims.ID == 0 {
		return nil, huma.Error401Unauthorized("the token is invalid, expired or names no user")
	}
	return claims, nil
}

// workExists is the WorkDirectory check (10 §3): a work is served by canon or
// still by the legacy catalog.
func (a *API) workExists(ctx context.Context, n int64) (bool, error) {
	rows, err := a.client.QueryContext(ctx, `SELECT EXISTS (SELECT 1 FROM canon.work WHERE id = $1)
		OR (to_regclass('public.catalog_work') IS NOT NULL AND EXISTS (SELECT 1 FROM public.catalog_work WHERE id = $1 AND deleted_at IS NULL))`, n)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var ok bool
	if rows.Next() {
		err = rows.Scan(&ok)
	}
	return ok, err
}

func (a *API) me(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict}
	huma.Register(h, huma.Operation{OperationID: "getMyWorkState", Method: http.MethodGet, Path: "/v3/me/works/{id}/state",
		Summary: "My state on a work", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *MeWorkInput) (*WorkStateOutput, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			st, err := a.readState(ctx, a.client.QueryContext, uid, in.ID)
			if err != nil {
				return nil, err
			}
			if st == nil {
				return nil, huma.Error404NotFound("no state on work " + id(in.ID))
			}
			return &WorkStateOutput{Body: *st}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "putMyWorkState", Method: http.MethodPut, Path: "/v3/me/works/{id}/state",
		Summary: "Set my state on a work", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *PutStateInput) (*WorkStateOutput, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			if ok, err := a.workExists(ctx, in.ID); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				return nil, huma.Error404NotFound("work " + id(in.ID) + " does not exist")
			}
			st, err := a.writeState(ctx, uid, in.ID, in.Body.State, in.Body.ObservedAt)
			if err != nil {
				return nil, err
			}
			return &WorkStateOutput{Body: *st}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "deleteMyWorkState", Method: http.MethodDelete, Path: "/v3/me/works/{id}/state",
		Summary: "Remove my state on a work", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *DeleteStateInput) (*struct{}, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			var observed *time.Time
			if in.ObservedAt != "" {
				t, perr := time.Parse(time.RFC3339Nano, in.ObservedAt)
				if perr != nil {
					return nil, huma.Error400BadRequest("observed_at is RFC 3339")
				}
				observed = &t
			}
			if _, err := a.writeState(ctx, uid, in.ID, "", observed); err != nil {
				return nil, err
			}
			return &struct{}{}, nil
		})
	a.progress(h)
}

type queryFn func(ctx context.Context, query string, args ...any) (*sql.Rows, error)

func (a *API) readState(ctx context.Context, q queryFn, uid, work int64) (*WorkState, error) {
	rows, err := q(ctx, `SELECT state, updated_at FROM public.catalog_user_work_state WHERE actor_uid = $1 AND work_id = $2`, uid, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var code int16
	st := WorkState{WorkID: id(work)}
	if err := rows.Scan(&code, &st.UpdatedAt); err != nil {
		return nil, err
	}
	st.State = stateName(code)
	return &st, nil
}

// writeState sets or (state "") removes one state. A write the client made
// before seeing a newer server value loses, and gets that value back (F16).
func (a *API) writeState(ctx context.Context, uid, work int64, state string, observed *time.Time) (*WorkState, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cur, err := a.readState(ctx, func(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
		return tx.QueryContext(ctx, q+" FOR UPDATE", args...)
	}, uid, work)
	if err != nil {
		return nil, err
	}
	if cur != nil && observed != nil && cur.UpdatedAt.After(*observed) {
		p := problem(http.StatusConflict, "INVALID_STATE_TRANSITION", fmt.Sprintf("the state changed at %s, after %s", cur.UpdatedAt.Format(time.RFC3339Nano), observed.Format(time.RFC3339Nano)))
		p.Current = cur
		return nil, p
	}
	if state == "" {
		if cur == nil {
			return nil, tx.Commit()
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM public.catalog_user_work_state WHERE actor_uid = $1 AND work_id = $2`, uid, work); err != nil {
			return nil, err
		}
		if err := tx.LibraryTombstone.Create().SetActorUID(uid).SetKind("work_state").SetSubjectID(work).Exec(ctx); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (actor_uid, work_id) DO UPDATE SET state = EXCLUDED.state, updated_at = now()`, uid, work, stateCodes[state]); err != nil {
		return nil, err
	}
	st, err := a.readState(ctx, tx.QueryContext, uid, work)
	if err != nil {
		return nil, err
	}
	return st, tx.Commit()
}

func encodeMeCursor(at time.Time, kind string, n int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + kind + "|" + strconv.FormatInt(n, 10)))
}

func decodeMeCursor(c string) (time.Time, string, int64, error) {
	if c == "" {
		return time.Time{}, "", 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.Split(string(b), "|")
	if err != nil || len(parts) != 3 {
		return time.Time{}, "", 0, huma.Error400BadRequest("cursor is not one this API issued")
	}
	at, err1 := time.Parse(time.RFC3339Nano, parts[0])
	n, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, "", 0, huma.Error400BadRequest("cursor is not one this API issued")
	}
	return at, parts[1], n, nil
}
