package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/todo"
	"api/internal/canon/registry"

	"github.com/danielgtaylor/huma/v2"
)

type Todo struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	SubjectKey string         `json:"subject_key"`
	State      string         `json:"state" enum:"open,done,rejected"`
	Detail     string         `json:"detail"`
	Source     string         `json:"source"`
	Suggestion map[string]any `json:"suggestion" doc:"The pool row that opened it; accepting applies it as a person's commit."`
	ResolvedBy string         `json:"resolved_by"`
	CreatedAt  time.Time      `json:"created_at"`
}

type TodoListInput struct {
	AuthInput
	State  string `query:"state" enum:"open,done,rejected," default:"open"`
	Kind   string `query:"kind"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type TodoList struct {
	Items      []Todo `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type TodoListOutput struct{ Body TodoList }

type TodoAcceptInput struct {
	AuthInput
	ID   int `path:"id" minimum:"1"`
	Body struct {
		UseIDs map[string]string `json:"use_ids,omitempty" doc:"ref → an existing canon id: that entity is the same one, so it is cited, not created."`
	}
}

type TodoRejectInput struct {
	AuthInput
	ID   int `path:"id" minimum:"1"`
	Body struct {
		Note string `json:"note" minLength:"1" maxLength:"2000"`
	}
}

type TodoResult struct {
	Body struct {
		State string            `json:"state"`
		IDs   map[string]string `json:"ids,omitempty"`
	}
}

func (a *API) todos(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}
	huma.Register(h, huma.Operation{OperationID: "listTodos", Method: http.MethodGet, Path: "/v3/catalog/todos",
		Summary: "Review queue", Tags: []string{"review"}, Errors: errs},
		func(ctx context.Context, in *TodoListInput) (*TodoListOutput, error) {
			if _, err := a.reviewer(ctx, in.Authorization); err != nil {
				return nil, err
			}
			after, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			q := a.client.Todo.Query().Where(todo.State(in.State), todo.IDGT(int(after))).Order(ent.Asc(todo.FieldID)).Limit(in.Limit + 1)
			if in.Kind != "" {
				q = q.Where(todo.Kind(in.Kind))
			}
			rows, err := q.All(ctx)
			if err != nil {
				return nil, err
			}
			out := TodoList{Items: []Todo{}}
			if len(rows) > in.Limit {
				rows = rows[:in.Limit]
				out.NextCursor = encodeCursor(int64(rows[len(rows)-1].ID))
			}
			for _, t := range rows {
				out.Items = append(out.Items, Todo{ID: strconv.Itoa(t.ID), Kind: t.Kind, SubjectKey: t.SubjectKey, State: t.State,
					Detail: t.Detail, Source: t.Source, Suggestion: t.Suggestion, ResolvedBy: t.ResolvedBy, CreatedAt: t.CreatedAt})
			}
			return &TodoListOutput{Body: out}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "acceptTodo", Method: http.MethodPost, Path: "/v3/catalog/todos/{id}/accept",
		Summary: "Accept a todo: apply its suggestion as your commit", Tags: []string{"review"}, Errors: errs},
		func(ctx context.Context, in *TodoAcceptInput) (*TodoResult, error) {
			who, err := a.reviewer(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			t, err := a.openTodo(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			use := map[string]int64{}
			for ref, v := range in.Body.UseIDs {
				n, perr := strconv.ParseInt(v, 10, 64)
				if perr != nil || n <= 0 {
					return nil, huma.Error422UnprocessableEntity("use_ids values are canon ids")
				}
				use[ref] = n
			}
			cs, err := suggestionChangeset(t.Suggestion, use, commit.Actor{Kind: commit.ActorHuman, Key: who})
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			cs.Key, cs.Note = "todo:"+strconv.Itoa(t.ID), "accepted todo "+strconv.Itoa(t.ID)
			res, err := a.commit.Apply(ctx, cs)
			var refused commit.Refused
			if errors.As(err, &refused) {
				return nil, huma.Error422UnprocessableEntity(refused.Error())
			}
			if err != nil {
				return nil, err
			}
			ids := map[string]int64{}
			for k, v := range use {
				ids[k] = v
			}
			for k, v := range res.IDs {
				ids[k] = v
			}
			if err := a.client.Todo.UpdateOneID(t.ID).SetState("done").SetResolvedBy(who).
				SetResolution(map[string]any{"result": "written", "ids": ids}).Exec(ctx); err != nil {
				return nil, err
			}
			out := &TodoResult{}
			out.Body.State, out.Body.IDs = "done", map[string]string{}
			for k, v := range ids {
				out.Body.IDs[k] = id(v)
			}
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "rejectTodo", Method: http.MethodPost, Path: "/v3/catalog/todos/{id}/reject",
		Summary: "Reject a todo", Tags: []string{"review"}, Errors: errs},
		func(ctx context.Context, in *TodoRejectInput) (*TodoResult, error) {
			who, err := a.reviewer(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			t, err := a.openTodo(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := a.client.Todo.UpdateOneID(t.ID).SetState("rejected").SetResolvedBy(who).
				SetResolution(map[string]any{"result": "refused", "code": "REJECTED_IN_REVIEW", "detail": in.Body.Note}).Exec(ctx); err != nil {
				return nil, err
			}
			out := &TodoResult{}
			out.Body.State = "rejected"
			return out, nil
		})
}

func (a *API) openTodo(ctx context.Context, n int) (*ent.Todo, error) {
	t, err := a.client.Todo.Get(ctx, n)
	if ent.IsNotFound(err) {
		return nil, huma.Error404NotFound("todo " + strconv.Itoa(n) + " does not exist")
	}
	if err != nil {
		return nil, err
	}
	if t.State != "open" {
		return nil, huma.Error409Conflict("todo " + strconv.Itoa(n) + " is already " + t.State)
	}
	return t, nil
}

// suggestionChangeset turns the pool row a todo carries into a person's
// changeset. A ref the reviewer maps to an existing id is cited instead of
// created, which is how "this is the same person" is said.
func suggestionChangeset(s map[string]any, use map[string]int64, actor commit.Actor) (commit.Changeset, error) {
	b, _ := json.Marshal(s)
	var row contract.OutboxRow
	if err := json.Unmarshal(b, &row); err != nil {
		return commit.Changeset{}, err
	}
	cs := commit.Changeset{Actor: actor}
	value := func(c contract.Candidate) commit.Value {
		raw := c.Value
		var ref string
		if json.Unmarshal(raw, &ref) == nil && strings.HasPrefix(ref, "ref:") {
			if n, ok := use[strings.TrimPrefix(ref, "ref:")]; ok {
				raw, _ = json.Marshal(n)
			}
		}
		return commit.Value{Raw: raw, Tier: registry.Tier(c.Tier)}
	}
	switch {
	case row.Candidate != nil:
		c := row.Candidate
		cs.Changes = []commit.Change{{Entity: c.Target.Entity, ID: c.Target.ID, Values: map[string]commit.Value{c.Property: value(*c)}}}
	case row.Changeset != nil:
		for _, ne := range row.Changeset.Entities {
			if _, cited := use[ne.Ref]; cited {
				continue
			}
			ch := commit.Change{Ref: ne.Ref, Entity: ne.Entity, Values: map[string]commit.Value{}}
			for k, c := range ne.Properties {
				ch.Values[k] = value(c)
			}
			cs.Changes = append(cs.Changes, ch)
		}
	default:
		return cs, errors.New("the todo carries no candidate or changeset")
	}
	if len(cs.Changes) == 0 {
		return cs, errors.New("use_ids cites every entity; nothing is left to write")
	}
	return cs, nil
}
