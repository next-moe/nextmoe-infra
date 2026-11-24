package api

import (
	"context"
	"net/http"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/perm"
	"api/internal/canon/registry"

	"github.com/danielgtaylor/huma/v2"
)

type ProgramItem struct {
	Key       string         `json:"key"`
	Medium    string         `json:"medium"`
	Doc       string         `json:"doc"`
	Mode      string         `json:"mode" enum:"review,auto,stopped"`
	SetBy     string         `json:"set_by"`
	Note      string         `json:"note"`
	SetAt     *time.Time     `json:"set_at"`
	Todos     map[string]int `json:"todos" doc:"Todos by state."`
	Commits   int            `json:"commits" doc:"Commits this program has made."`
	Undone    int            `json:"undone" doc:"Of those, taken back by a person."`
	Unpatrol  int            `json:"unpatrolled" doc:"Of those, not looked at yet."`
	LastWrite *time.Time     `json:"last_write"`
}

type ProgramsOutput struct {
	Body struct {
		Items []ProgramItem `json:"items"`
	}
}

type ProgramModeInput struct {
	AuthInput
	Key  string `path:"key"`
	Body struct {
		Mode string `json:"mode" enum:"review,auto,stopped"`
		Note string `json:"note" minLength:"1" maxLength:"2000"`
	}
}

type PatrolInput struct {
	AuthInput
	Program string `query:"program"`
	Cursor  string `query:"cursor"`
	Limit   int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type PatrolItem struct {
	Batch      string         `json:"batch"`
	Program    string         `json:"program"`
	ReviewedBy string         `json:"reviewed_by"`
	At         time.Time      `json:"at"`
	Revisions  []RevisionItem `json:"revisions"`
}

type PatrolOutput struct {
	Body struct {
		Items      []PatrolItem `json:"items"`
		NextCursor string       `json:"next_cursor,omitempty"`
	}
}

type PatrolMarkInput struct {
	AuthInput
	Batch string `path:"batch"`
}

func (a *API) programs(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity}
	huma.Register(h, huma.Operation{OperationID: "listPrograms", Method: http.MethodGet, Path: "/v3/catalog/programs",
		Summary: "Ingest programs, their modes and their record", Tags: []string{"ingest"}, Errors: errs},
		func(ctx context.Context, in *struct{ AuthInput }) (*ProgramsOutput, error) {
			if _, err := a.require(ctx, in.Authorization, perm.Patrol); err != nil {
				return nil, err
			}
			out := &ProgramsOutput{}
			out.Body.Items = []ProgramItem{}
			for _, p := range registry.Programs {
				item, err := a.program(ctx, p)
				if err != nil {
					return nil, err
				}
				out.Body.Items = append(out.Body.Items, item)
			}
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "setProgramMode", Method: http.MethodPut, Path: "/v3/catalog/programs/{key}/mode",
		Summary: "Set a program's mode", Description: "Needs canon.ingest_manage.", Tags: []string{"ingest"}, Errors: errs},
		func(ctx context.Context, in *ProgramModeInput) (*DoneOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.IngestManage)
			if err != nil {
				return nil, err
			}
			if _, ok := registry.ProgramByKey(in.Key); !ok {
				return nil, huma.Error404NotFound("program " + in.Key + " is not registered")
			}
			if err := a.client.ProgramState.Create().SetID(in.Key).SetMode(in.Body.Mode).SetSetBy(s.key).SetNote(in.Body.Note).
				OnConflictColumns("id").UpdateNewValues().Exec(ctx); err != nil {
				return nil, err
			}
			out := &DoneOutput{}
			out.Body.State = in.Body.Mode
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listUnpatrolled", Method: http.MethodGet, Path: "/v3/catalog/patrol",
		Summary: "Agents' commits nobody has looked at, oldest first", Description: "Needs canon.patrol.", Tags: []string{"ingest"}, Errors: errs},
		func(ctx context.Context, in *PatrolInput) (*PatrolOutput, error) {
			if _, err := a.require(ctx, in.Authorization, perm.Patrol); err != nil {
				return nil, err
			}
			return a.unpatrolled(ctx, in)
		})
	huma.Register(h, huma.Operation{OperationID: "patrolCommit", Method: http.MethodPost, Path: "/v3/catalog/patrol/{batch}",
		Summary: "Mark an agent's commit as looked at", Description: "Needs canon.patrol. Changes no value; undo the commit to take it back.", Tags: []string{"ingest"}, Errors: errs},
		func(ctx context.Context, in *PatrolMarkInput) (*DoneOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Patrol)
			if err != nil {
				return nil, err
			}
			rows, err := a.client.QueryContext(ctx, `SELECT 1 FROM canon.revision WHERE batch = $1 AND actor_kind = 'agent' LIMIT 1`, in.Batch)
			if err != nil {
				return nil, err
			}
			found := rows.Next()
			rows.Close()
			if !found {
				return nil, huma.Error404NotFound("no agent commit " + in.Batch)
			}
			if err := a.client.Patrol.Create().SetBatch(in.Batch).SetBy(s.key).OnConflictColumns("batch").Ignore().Exec(ctx); err != nil {
				return nil, err
			}
			out := &DoneOutput{}
			out.Body.State = "patrolled"
			return out, nil
		})
}

func (a *API) program(ctx context.Context, p registry.Program) (ProgramItem, error) {
	item := ProgramItem{Key: p.Key, Medium: p.Medium, Doc: p.Doc, Mode: registry.ModeReview, Todos: map[string]int{}}
	st, err := a.client.ProgramState.Get(ctx, p.Key)
	switch {
	case ent.IsNotFound(err):
	case err != nil:
		return item, err
	default:
		item.Mode, item.SetBy, item.Note, item.SetAt = st.Mode, st.SetBy, st.Note, &st.UpdatedAt
	}
	rows, err := a.client.QueryContext(ctx, `SELECT state, count(*) FROM canon.todo WHERE program = $1 GROUP BY state`, p.Key)
	if err != nil {
		return item, err
	}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			rows.Close()
			return item, err
		}
		item.Todos[state] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return item, err
	}
	rows, err = a.client.QueryContext(ctx, `WITH b AS (SELECT batch, max(created_at) AS at FROM canon.revision
			WHERE actor_kind = 'agent' AND actor_key = $1 AND batch <> '' GROUP BY batch)
		SELECT count(*), max(at),
			count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM canon.patrol x WHERE x.batch = b.batch)),
			count(*) FILTER (WHERE EXISTS (SELECT 1 FROM canon.revision r JOIN canon.revision u ON u.undoes = r.id
				WHERE r.batch = b.batch AND u.actor_kind = 'human')) FROM b`, "program:"+p.Key)
	if err != nil {
		return item, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&item.Commits, &item.LastWrite, &item.Unpatrol, &item.Undone); err != nil {
			return item, err
		}
	}
	return item, rows.Err()
}

func (a *API) unpatrolled(ctx context.Context, in *PatrolInput) (*PatrolOutput, error) {
	after, err := decodeCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	actor := "%"
	if in.Program != "" {
		actor = "program:" + in.Program
	}
	rows, err := a.client.QueryContext(ctx, `SELECT r.batch, min(r.id) FROM canon.revision r
		WHERE r.actor_kind = 'agent' AND r.batch <> '' AND r.actor_key LIKE $1
		AND NOT EXISTS (SELECT 1 FROM canon.patrol x WHERE x.batch = r.batch)
		GROUP BY r.batch HAVING min(r.id) > $2 ORDER BY 2 LIMIT $3`, actor, after, in.Limit+1)
	if err != nil {
		return nil, err
	}
	var batches []string
	last := map[string]int64{}
	for rows.Next() {
		var b string
		var n int64
		if err := rows.Scan(&b, &n); err != nil {
			rows.Close()
			return nil, err
		}
		batches = append(batches, b)
		last[b] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := &PatrolOutput{}
	out.Body.Items = []PatrolItem{}
	if len(batches) > in.Limit {
		batches = batches[:in.Limit]
		out.Body.NextCursor = encodeCursor(last[batches[len(batches)-1]])
	}
	for _, b := range batches {
		revs, err := a.listRevisions(ctx, &RevisionsInput{Batch: b, Limit: 100})
		if err != nil {
			return nil, err
		}
		item := PatrolItem{Batch: b, Revisions: revs.Body.Items}
		if len(item.Revisions) > 0 {
			r := item.Revisions[0]
			item.Program, item.ReviewedBy, item.At = r.ActorKey, r.ReviewedBy, r.At
		}
		out.Body.Items = append(out.Body.Items, item)
	}
	return out, nil
}
