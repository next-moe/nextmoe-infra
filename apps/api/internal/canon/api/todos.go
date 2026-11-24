package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/ent/todo"
	"api/internal/canon/perm"
	"api/internal/canon/registry"
	"api/internal/canon/review"

	"github.com/danielgtaylor/huma/v2"
)

type Todo struct {
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	SubjectKey string           `json:"subject_key"`
	Program    string           `json:"program"`
	State      string           `json:"state" enum:"open,done,rejected,superseded"`
	Detail     string           `json:"detail"`
	Source     string           `json:"source"`
	Suggestion map[string]any   `json:"suggestion" doc:"The pool row that opened it. Accepting commits it as the program's write with you as its reviewer; an agent_on_human todo becomes your own edit."`
	Questions  []map[string]any `json:"questions" doc:"Identity questions; accepting answers every one."`
	Notes      []map[string]any `json:"notes" doc:"What reviewers who left it open found."`
	ResolvedBy string           `json:"resolved_by"`
	CreatedAt  time.Time        `json:"created_at"`
}

type TodoListInput struct {
	AuthInput
	State  string `query:"state" enum:"open,done,rejected,superseded," default:"open"`
	Kind   string `query:"kind"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type TodoList struct {
	Items      []Todo `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type TodoListOutput struct{ Body TodoList }

type TodoInput struct {
	AuthInput
	ID int `path:"id" minimum:"1"`
}

type TodoSheetOutput struct{ Body review.Sheet }

type TodoAcceptInput struct {
	AuthInput
	ID   int `path:"id" minimum:"1"`
	Body struct {
		Answers map[string]TodoAnswer `json:"answers,omitempty" doc:"Keyed by question ref."`
		Omit    []string              `json:"omit,omitempty" doc:"Refs to leave out; whatever points at one goes with it."`
		Note    string                `json:"note,omitempty" maxLength:"2000"`
	}
}

type TodoAnswer struct {
	Use   string `json:"use,omitempty" doc:"The existing canon id it is."`
	Adopt string `json:"adopt,omitempty" doc:"The legacy id it is; the new entity takes it over."`
	New   bool   `json:"new,omitempty" doc:"None of the candidates."`
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
			s, err := a.staffOf(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			kinds := s.todoKinds()
			if in.Kind != "" {
				p, ok := perm.ForTodo(in.Kind)
				if !ok {
					return nil, huma.Error422UnprocessableEntity("kind must be one of " + strings.Join(registry.TodoKinds, ", "))
				}
				if !s.can(p) {
					return nil, forbidden(p)
				}
				kinds = []string{in.Kind}
			}
			if len(kinds) == 0 {
				return nil, huma.Error403Forbidden("the review queue needs " + string(perm.TodoValue) + " or " + string(perm.TodoIdentity))
			}
			after, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			rows, err := a.client.Todo.Query().Where(todo.State(in.State), todo.KindIn(kinds...), todo.IDGT(int(after))).
				Order(ent.Asc(todo.FieldID)).Limit(in.Limit + 1).All(ctx)
			if err != nil {
				return nil, err
			}
			out := TodoList{Items: []Todo{}}
			if len(rows) > in.Limit {
				rows = rows[:in.Limit]
				out.NextCursor = encodeCursor(int64(rows[len(rows)-1].ID))
			}
			for _, t := range rows {
				item := Todo{ID: strconv.Itoa(t.ID), Kind: t.Kind, SubjectKey: t.SubjectKey, Program: t.Program, State: t.State,
					Detail: t.Detail, Source: t.Source, Suggestion: t.Suggestion, Questions: t.Questions, Notes: t.Notes,
					ResolvedBy: t.ResolvedBy, CreatedAt: t.CreatedAt}
				if item.Questions == nil {
					item.Questions = []map[string]any{}
				}
				if item.Notes == nil {
					item.Notes = []map[string]any{}
				}
				out.Items = append(out.Items, item)
			}
			return &TodoListOutput{Body: out}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "getTodo", Method: http.MethodGet, Path: "/v3/catalog/todos/{id}",
		Summary: "One todo with the words behind every value", Tags: []string{"review"}, Errors: errs},
		func(ctx context.Context, in *TodoInput) (*TodoSheetOutput, error) {
			s, err := a.staffOf(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			t, err := a.client.Todo.Get(ctx, in.ID)
			if ent.IsNotFound(err) {
				return nil, huma.Error404NotFound("todo " + strconv.Itoa(in.ID) + " does not exist")
			}
			if err != nil {
				return nil, err
			}
			if p, ok := perm.ForTodo(t.Kind); !ok || !s.can(p) {
				return nil, forbidden(p)
			}
			sheet, err := review.SheetOf(ctx, a.client, t)
			if err != nil {
				return nil, err
			}
			return &TodoSheetOutput{Body: sheet}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "acceptTodo", Method: http.MethodPost, Path: "/v3/catalog/todos/{id}/accept",
		Summary: "Accept a todo and commit what it holds", Tags: []string{"review"}, Errors: errs},
		func(ctx context.Context, in *TodoAcceptInput) (*TodoResult, error) {
			t, who, err := a.decidable(ctx, in.Authorization, in.ID)
			if err != nil {
				return nil, err
			}
			d := review.Decision{Answers: map[string]review.Answer{}, Omit: in.Body.Omit, Note: in.Body.Note}
			for ref, v := range in.Body.Answers {
				ans := review.Answer{New: v.New}
				for dst, text := range map[*int64]string{&ans.Use: v.Use, &ans.Adopt: v.Adopt} {
					if text == "" {
						continue
					}
					if *dst, err = strconv.ParseInt(text, 10, 64); err != nil || *dst <= 0 {
						return nil, huma.Error422UnprocessableEntity("use and adopt are ids")
					}
				}
				d.Answers[ref] = ans
			}
			ids, err := review.Accept(ctx, a.client, t.ID, review.Reviewer{Key: who, Human: true}, d)
			if err := decisionError(err); err != nil {
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
			t, who, err := a.decidable(ctx, in.Authorization, in.ID)
			if err != nil {
				return nil, err
			}
			if err := decisionError(review.Reject(ctx, a.client, t.ID, review.Reviewer{Key: who, Human: true}, in.Body.Note)); err != nil {
				return nil, err
			}
			out := &TodoResult{}
			out.Body.State = "rejected"
			return out, nil
		})
}

func (a *API) decidable(ctx context.Context, header string, n int) (*ent.Todo, string, error) {
	s, err := a.staffOf(ctx, header)
	if err != nil {
		return nil, "", err
	}
	if len(s.todoKinds()) == 0 {
		return nil, "", huma.Error403Forbidden("deciding a todo needs " + string(perm.TodoValue) + " or " + string(perm.TodoIdentity))
	}
	t, err := a.openTodo(ctx, n)
	if err != nil {
		return nil, "", err
	}
	if p, _ := perm.ForTodo(t.Kind); !s.can(p) {
		return nil, "", forbidden(p)
	}
	return t, s.key, nil
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

// decisionError turns what review refuses into a problem the editor can show.
func decisionError(err error) error {
	var refused commit.Refused
	var invalid review.Invalid
	switch {
	case err == nil:
		return nil
	case errors.Is(err, review.ErrSettled):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, review.ErrPersonOnly):
		return huma.Error403Forbidden(err.Error())
	case errors.As(err, &refused):
		return huma.Error422UnprocessableEntity(refused.Error())
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Error())
	}
	return err
}
