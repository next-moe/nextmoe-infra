package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	libraryprogress "api/internal/canon/ent/librarypartprogress"
	"api/internal/canon/ent/workpart"

	"github.com/danielgtaylor/huma/v2"
)

type PartProgress struct {
	PartID    string    `json:"part_id"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PartProgressOutput struct{ Body PartProgress }

func (a *API) progress(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusNotFound}
	huma.Register(h, huma.Operation{OperationID: "putMyPartProgress", Method: http.MethodPut, Path: "/v3/me/parts/{id}/progress",
		Summary: "Mark a volume, chapter or episode", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *PutProgressInput) (*PartProgressOutput, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			if ok, err := a.client.WorkPart.Query().Where(workpart.ID(in.ID)).Exist(ctx); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				return nil, huma.Error404NotFound("part " + id(in.ID) + " does not exist")
			}
			out := &PartProgressOutput{Body: PartProgress{PartID: id(in.ID), State: in.Body.State, UpdatedAt: time.Now().UTC()}}
			if in.Body.State == "none" {
				tx, err := a.client.Tx(ctx)
				if err != nil {
					return nil, err
				}
				defer tx.Rollback()
				n, err := tx.LibraryPartProgress.Delete().Where(libraryprogress.ActorUID(uid), libraryprogress.PartID(in.ID)).Exec(ctx)
				if err != nil {
					return nil, err
				}
				if n > 0 {
					if err := tx.LibraryTombstone.Create().SetActorUID(uid).SetKind("part_progress").SetSubjectID(in.ID).Exec(ctx); err != nil {
						return nil, err
					}
				}
				return out, tx.Commit()
			}
			err = a.client.LibraryPartProgress.Create().SetActorUID(uid).SetPartID(in.ID).SetState(in.Body.State).
				OnConflictColumns(libraryprogress.FieldActorUID, libraryprogress.FieldPartID).UpdateNewValues().Exec(ctx)
			return out, err
		})
	huma.Register(h, huma.Operation{OperationID: "listMyChanges", Method: http.MethodGet, Path: "/v3/me/changes",
		Summary: "My library changes, removals included", Description: "Every device and site follows this to sync shelves and progress (12 F16).",
		Tags: []string{"library"}, Errors: []int{http.StatusUnauthorized, http.StatusBadRequest}},
		func(ctx context.Context, in *MeChangesInput) (*MeChangesOutput, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			at, kind, after, err := decodeMeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			rows, err := a.client.QueryContext(ctx, `SELECT kind, subject, state, at, id FROM (
				SELECT 'work_state' AS kind, work_id AS subject, state::text AS state, updated_at AS at, id FROM public.catalog_user_work_state WHERE actor_uid = $1
				UNION ALL SELECT 'part_progress', part_id, state, updated_at, id FROM canon.library_part_progress WHERE actor_uid = $1
				UNION ALL SELECT kind, subject_id, NULL, deleted_at, -id FROM canon.library_tombstone WHERE actor_uid = $1
			) c WHERE (at, kind, id) > ($2, $3, $4) ORDER BY at, kind, id LIMIT $5`, uid, at, kind, after, in.Limit)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := MeChangeList{Items: []MeChange{}, NextCursor: in.Cursor}
			for rows.Next() {
				var c MeChange
				var subject, n int64
				var state *string
				var k string
				if err := rows.Scan(&k, &subject, &state, &c.At, &n); err != nil {
					return nil, err
				}
				c.Kind, c.SubjectID = k, id(subject)
				if state != nil && k == "work_state" {
					code, _ := strconv.Atoi(*state)
					name := stateName(int16(code))
					state = &name
				}
				c.State = state
				out.Items = append(out.Items, c)
				out.NextCursor = encodeMeCursor(c.At, k, n)
			}
			return &MeChangesOutput{Body: out}, rows.Err()
		})
}
