package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/ent/predicate"
	"api/internal/canon/ent/revision"
	"api/internal/canon/identity"
	"api/internal/canon/perm"

	"github.com/danielgtaylor/huma/v2"
)

type CandidateRef struct {
	Token string `json:"token" doc:"canon:<id> or legacy:<id>."`
	ID    string `json:"id"`
	Label string `json:"label"`
}

type EditChange struct {
	Ref           string         `json:"ref,omitempty" doc:"Names a new entity for later changes, which point at it as ref:<ref>."`
	Entity        string         `json:"entity"`
	ID            string         `json:"id,omitempty" doc:"Absent creates the entity."`
	ExpectVersion int            `json:"expect_version,omitempty" doc:"The version this edit was made against."`
	Values        map[string]any `json:"values,omitempty"`
	AdoptID       string         `json:"adopt_id,omitempty" doc:"The legacy id a new entity takes over."`
	Distinct      []string       `json:"distinct,omitempty" doc:"Candidate tokens this new entity is not."`
	Remove        bool           `json:"remove,omitempty" doc:"Takes out a title, an organization name, a credit or a content row."`
}

type EditInput struct {
	AuthInput
	Body struct {
		Note    string       `json:"note" minLength:"1" maxLength:"2000"`
		Changes []EditChange `json:"changes" minItems:"1" maxItems:"200"`
	}
}

type EditOutput struct {
	Body struct {
		Batch string            `json:"batch"`
		IDs   map[string]string `json:"ids"`
	}
}

type RevisionsInput struct {
	AuthInput
	Entity string `query:"entity"`
	ID     int64  `query:"id"`
	Root   bool   `query:"root" doc:"Also the entity's own child rows: a work's titles, parts and credits."`
	Batch  string `query:"batch"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
}

type RevisionsOutput struct {
	Body struct {
		Items      []RevisionItem `json:"items"`
		NextCursor string         `json:"next_cursor,omitempty"`
	}
}

type UndoInput struct {
	AuthInput
	Batch string `path:"batch"`
	Body  struct {
		Note string `json:"note" minLength:"1" maxLength:"2000"`
	}
}

func (a *API) edits(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}
	huma.Register(h, huma.Operation{OperationID: "commitChanges", Method: http.MethodPost, Path: "/v3/catalog/commits",
		Summary: "Write a set of changes, all or none", Description: "Needs canon.edit. The values are yours: agents may only propose others. A new work, name or organization resembling an existing one answers 409 IDENTITY_UNSURE with the candidates.",
		Tags: []string{"editing"}, Errors: errs, DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *EditInput) (*EditOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Edit)
			if err != nil {
				return nil, err
			}
			cs := commit.Changeset{Actor: commit.Actor{Kind: commit.ActorHuman, Key: s.key}, Note: in.Body.Note}
			for _, c := range in.Body.Changes {
				ch, err := c.change()
				if err != nil {
					return nil, huma.Error422UnprocessableEntity(err.Error())
				}
				cs.Changes = append(cs.Changes, ch)
			}
			res, err := a.commit.Apply(ctx, cs)
			if err != nil {
				return nil, commitError(err)
			}
			out := &EditOutput{}
			out.Body.Batch, out.Body.IDs = res.Batch, map[string]string{}
			for k, v := range res.IDs {
				out.Body.IDs[k] = id(v)
			}
			return out, nil
		})
	huma.Register(h, huma.Operation{OperationID: "listRevisions", Method: http.MethodGet, Path: "/v3/catalog/revisions",
		Summary: "Who changed what, newest first", Tags: []string{"editing"}, Errors: errs},
		func(ctx context.Context, in *RevisionsInput) (*RevisionsOutput, error) {
			if _, err := a.require(ctx, in.Authorization, perm.Edit); err != nil {
				return nil, err
			}
			return a.listRevisions(ctx, in)
		})
	huma.Register(h, huma.Operation{OperationID: "undoCommit", Method: http.MethodPost, Path: "/v3/catalog/commits/{batch}/undo",
		Summary: "Take back a commit", Description: "Needs canon.edit. Restores every field it changed and removes what it created; refused when anything was written, referenced or kept by a user since. Fields you restore are locked against agents.",
		Tags: []string{"editing"}, Errors: errs},
		func(ctx context.Context, in *UndoInput) (*DoneOutput, error) {
			s, err := a.require(ctx, in.Authorization, perm.Edit)
			if err != nil {
				return nil, err
			}
			err = a.commit.Undo(ctx, in.Batch, commit.Actor{Kind: commit.ActorHuman, Key: s.key}, in.Body.Note)
			var conflicts commit.Conflicts
			switch {
			case errors.Is(err, commit.ErrNothingToUndo):
				return nil, huma.Error404NotFound(err.Error())
			case errors.As(err, &conflicts):
				p := problem(http.StatusConflict, "", "the commit cannot be taken back as a whole")
				p.Errors = conflicts
				return nil, p
			case err != nil:
				return nil, err
			}
			out := &DoneOutput{}
			out.Body.State = "undone"
			return out, nil
		})
}

func (c EditChange) change() (commit.Change, error) {
	ch := commit.Change{Ref: c.Ref, Entity: c.Entity, ExpectVersion: c.ExpectVersion, Distinct: c.Distinct, Remove: c.Remove, Values: map[string]commit.Value{}}
	for dst, text := range map[*int64]string{&ch.ID: c.ID, &ch.AdoptID: c.AdoptID} {
		if text == "" {
			continue
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil || n <= 0 {
			return ch, errors.New("ids are positive decimal strings")
		}
		*dst = n
	}
	for key, v := range c.Values {
		raw, err := json.Marshal(v)
		if err != nil {
			return ch, err
		}
		// A cited id is a decimal string on this face and a number inside.
		if p, ok := commitable[c.Entity][key]; ok && p.Ref != "" {
			if s, isText := v.(string); isText {
				if _, err := strconv.ParseInt(s, 10, 64); err == nil {
					raw = json.RawMessage(s)
				}
			}
		}
		ch.Values[key] = commit.Value{Raw: raw}
	}
	return ch, nil
}

func commitError(err error) error {
	var refused commit.Refused
	if !errors.As(err, &refused) {
		return err
	}
	status, code := http.StatusUnprocessableEntity, refused[0].Code
	if slices.Contains([]string{"VERSION_CONFLICT", "IDENTITY_UNSURE", "UNIQUE_CONFLICT", "ENTITY_MERGED"}, code) {
		status = http.StatusConflict
	}
	p := problem(status, code, refused.Error())
	for _, r := range refused {
		p.Errors = append(p.Errors, r.Property+": "+r.Code+" "+r.Detail)
		p.Candidates = append(p.Candidates, candidateRefs(r.Candidates)...)
	}
	return p
}

func candidateRefs(cs []identity.Candidate) []CandidateRef {
	out := make([]CandidateRef, 0, len(cs))
	for _, c := range cs {
		out = append(out, CandidateRef{Token: c.Token(), ID: id(c.ID), Label: c.Label})
	}
	return out
}

func (a *API) listRevisions(ctx context.Context, in *RevisionsInput) (*RevisionsOutput, error) {
	var where []predicate.Revision
	switch {
	case in.Batch != "":
		where = append(where, revision.Batch(in.Batch))
	case in.Entity != "" && in.ID > 0 && in.Root:
		where = append(where, revision.RootEntity(in.Entity), revision.RootID(in.ID))
	case in.Entity != "" && in.ID > 0:
		where = append(where, revision.Entity(in.Entity), revision.EntityID(in.ID))
	default:
		return nil, huma.Error400BadRequest("give batch=, or entity= and id=")
	}
	if in.Cursor != "" {
		before, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		where = append(where, revision.IDLT(before))
	}
	rows, err := a.client.Revision.Query().Where(where...).Order(ent.Desc(revision.FieldID)).Limit(in.Limit + 1).All(ctx)
	if err != nil {
		return nil, err
	}
	out := &RevisionsOutput{}
	out.Body.Items = []RevisionItem{}
	if len(rows) > in.Limit {
		rows = rows[:in.Limit]
		out.Body.NextCursor = encodeCursor(rows[len(rows)-1].ID)
	}
	undone, err := a.undoneSet(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, shapeRevision(r, undone[r.ID]))
	}
	return out, nil
}

func (a *API) undoneSet(ctx context.Context, rows []*ent.Revision) (map[int64]bool, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	us, err := a.client.Revision.Query().Where(revision.UndoesIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		out[u.Undoes] = true
	}
	return out, nil
}
