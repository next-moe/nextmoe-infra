package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/revision"
	"api/internal/canon/perm"
	"api/internal/canon/registry"
	"api/internal/canon/review"

	"github.com/danielgtaylor/huma/v2"
)

// bookkeeping is never edited: the commit service writes it.
var bookkeeping = []string{"field_state", "version"}

// childOf lists the rows shown and edited with a public entity, and the field
// by which they belong to it.
var childOf = map[string]map[string]string{
	"work":    {"work_title": "work_id", "work_part": "work_id", "book_work": "work_id", "credit": "work_id", "release_content": "work_id"},
	"release": {"release_content": "release_id", "book_release": "release_id", "credit": "release_id"},
	"person":  {"person_alias": "person_id"},
	"org":     {"org_name": "org_id"},
}

type SchemaProperty struct {
	Key       string   `json:"key"`
	Shape     string   `json:"shape"`
	Vocab     string   `json:"vocab,omitempty"`
	Ref       string   `json:"ref,omitempty"`
	Doc       string   `json:"doc"`
	Clauses   []string `json:"clauses"`
	AgentMay  bool     `json:"agent_may" doc:"Agents may write it; otherwise only people do."`
	Immutable bool     `json:"immutable" doc:"Fixed when the entity is created."`
}

type SchemaEntity struct {
	Name       string            `json:"name"`
	Properties []SchemaProperty  `json:"properties"`
	Children   map[string]string `json:"children" doc:"Child entity → the field naming this one."`
}

type Schemas struct {
	Entities     []SchemaEntity        `json:"entities"`
	Vocabularies []registry.Vocabulary `json:"vocabularies"`
}

type SchemasOutput struct{ Body Schemas }

type RecordInput struct {
	AuthInput
	Entity string `path:"entity"`
	ID     int64  `path:"id" minimum:"1"`
}

type FieldState struct {
	By         string   `json:"by" enum:"agent,human,system"`
	Key        string   `json:"key"`
	Tier       string   `json:"tier"`
	Source     string   `json:"source"`
	ReviewedBy string   `json:"reviewed_by"`
	At         string   `json:"at"`
	Evidence   []string `json:"evidence" doc:"The words the value was read from."`
}

type Record struct {
	Entity   string                 `json:"entity"`
	ID       string                 `json:"id"`
	Version  int                    `json:"version"`
	Values   map[string]any         `json:"values"`
	State    map[string]*FieldState `json:"state"`
	Called   string                 `json:"called" doc:"What a person would call it; empty when it has no name."`
	Is       map[string]string      `json:"is" doc:"What each cited id is called."`
	Held     bool                   `json:"held"`
	Children map[string][]Record    `json:"children,omitempty"`
}

type RecordOutput struct{ Body Record }

func (a *API) records(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "getSchemas", Method: http.MethodGet, Path: "/v3/catalog/schemas",
		Summary: "What every entity is made of", Description: "Derived from the property registry; the editor renders from it.", Tags: []string{"editing"}},
		func(ctx context.Context, _ *struct{}) (*SchemasOutput, error) {
			return &SchemasOutput{Body: schemas()}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "getRecord", Method: http.MethodGet, Path: "/v3/catalog/records/{entity}/{id}",
		Summary: "An entity as the editor sees it: every field, who wrote it and from what words", Tags: []string{"editing"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound}},
		func(ctx context.Context, in *RecordInput) (*RecordOutput, error) {
			if _, err := a.require(ctx, in.Authorization, perm.Edit); err != nil {
				return nil, err
			}
			if _, ok := commitable[in.Entity]; !ok {
				return nil, huma.Error404NotFound("unknown entity " + in.Entity)
			}
			rec, found, err := a.record(ctx, in.Entity, in.ID)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, a.gone(ctx, in.Entity, in.ID, &ent.NotFoundError{})
			}
			rec.Children = map[string][]Record{}
			for child, field := range childOf[in.Entity] {
				ids, err := a.idsOf(ctx, child, field, in.ID)
				if err != nil {
					return nil, err
				}
				rec.Children[child] = []Record{}
				for _, n := range ids {
					c, _, err := a.record(ctx, child, n)
					if err != nil {
						return nil, err
					}
					rec.Children[child] = append(rec.Children[child], c)
				}
			}
			return &RecordOutput{Body: rec}, nil
		})
}

// commitable is every entity a changeset may write, from the registry.
var commitable = func() map[string]map[string]registry.Property {
	out := map[string]map[string]registry.Property{}
	for entity, props := range contract.Index() {
		if _, stamped := props["field_state"]; stamped {
			out[entity] = props
		}
	}
	return out
}()

func schemas() Schemas {
	out := Schemas{Vocabularies: registry.Vocabularies, Entities: []SchemaEntity{}}
	names := make([]string, 0, len(commitable))
	for n := range commitable {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		e := SchemaEntity{Name: n, Properties: []SchemaProperty{}, Children: childOf[n]}
		if e.Children == nil {
			e.Children = map[string]string{}
		}
		for _, key := range sortedKeys(commitable[n]) {
			p := commitable[n][key]
			if slices.Contains(bookkeeping, key) {
				continue
			}
			clauses := p.Clauses
			if clauses == nil {
				clauses = []string{}
			}
			e.Properties = append(e.Properties, SchemaProperty{Key: key, Shape: string(p.Shape), Vocab: p.Vocab, Ref: p.Ref, Doc: p.Doc,
				Clauses: clauses, AgentMay: p.Agent, Immutable: p.ScopedByProgram})
		}
		out.Entities = append(out.Entities, e)
	}
	return out
}

func (a *API) record(ctx context.Context, entity string, n int64) (Record, bool, error) {
	out := Record{Entity: entity, ID: id(n), Values: map[string]any{}, State: map[string]*FieldState{}, Is: map[string]string{}}
	rows, err := a.client.QueryContext(ctx, `SELECT to_jsonb(t) FROM canon.`+entity+` t WHERE id = $1`, n)
	if err != nil {
		return out, false, err
	}
	var b []byte
	found := rows.Next()
	if found {
		err = rows.Scan(&b)
	}
	rows.Close()
	if err != nil || !found {
		return out, false, err
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		return out, false, err
	}
	if v, ok := row["version"].(float64); ok {
		out.Version = int(v)
	}
	if out.Called, err = review.Called(ctx, a.client, entity, n); err != nil {
		return out, false, err
	}
	state, _ := row["field_state"].(map[string]any)
	evidence, err := a.evidence(ctx, entity, n)
	if err != nil {
		return out, false, err
	}
	for key, p := range commitable[entity] {
		if slices.Contains(bookkeeping, key) {
			continue
		}
		out.Values[key] = idText(p, row[key])
		if f, ok := row[key].(float64); ok && p.Ref != "" {
			if out.Is[key], err = review.Called(ctx, a.client, p.Ref, int64(f)); err != nil {
				return out, false, err
			}
		}
		st, ok := state[key].(map[string]any)
		if !ok {
			continue
		}
		text := func(k string) string { s, _ := st[k].(string); return s }
		out.State[key] = &FieldState{By: text("by"), Key: text("key"), Tier: text("assurance"), Source: text("source"),
			ReviewedBy: text("reviewed_by"), At: text("at"), Evidence: evidence[key]}
		if out.State[key].Evidence == nil {
			out.State[key].Evidence = []string{}
		}
	}
	held, err := a.client.QueryContext(ctx, `SELECT 1 FROM canon.hold WHERE entity = $1 AND entity_id = $2 AND released_at IS NULL LIMIT 1`, entity, n)
	if err != nil {
		return out, false, err
	}
	out.Held = held.Next()
	held.Close()
	return out, true, held.Err()
}

// idText keeps a cited id a decimal string, as everywhere on this face.
func idText(p registry.Property, v any) any {
	if f, ok := v.(float64); ok && p.Shape == registry.ShapeRef {
		return strconv.FormatInt(int64(f), 10)
	}
	return v
}

// evidence is, per field, the words of the latest write that gave any.
func (a *API) evidence(ctx context.Context, entity string, n int64) (map[string][]string, error) {
	revs, err := a.client.Revision.Query().Where(revision.Entity(entity), revision.EntityID(n), revision.ActionIn("create", "update", "undo")).
		Order(ent.Desc(revision.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range revs {
		for key, raw := range r.Changes {
			if _, seen := out[key]; seen {
				continue
			}
			c, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			var refs []commit.EvidenceRef
			b, _ := json.Marshal(c["evidence"])
			_ = json.Unmarshal(b, &refs)
			out[key] = []string{}
			for _, e := range refs {
				words, err := review.Excerpt(ctx, a.client, e)
				if err != nil {
					return nil, fmt.Errorf("revision %d %s: %w", r.ID, key, err)
				}
				out[key] = append(out[key], words)
			}
		}
	}
	return out, nil
}

func (a *API) idsOf(ctx context.Context, entity, field string, n int64) ([]int64, error) {
	rows, err := a.client.QueryContext(ctx, `SELECT id FROM canon.`+entity+` WHERE `+field+` = $1 ORDER BY id`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

type RevisionItem struct {
	ID         string         `json:"id"`
	Entity     string         `json:"entity"`
	EntityID   string         `json:"entity_id"`
	Action     string         `json:"action"`
	ActorKind  string         `json:"actor_kind"`
	ActorKey   string         `json:"actor_key"`
	ReviewedBy string         `json:"reviewed_by"`
	Batch      string         `json:"batch" doc:"The commit; undo takes back all of it."`
	Note       string         `json:"note"`
	Changes    map[string]any `json:"changes"`
	Undone     bool           `json:"undone"`
	At         time.Time      `json:"at"`
}

func shapeRevision(r *ent.Revision, undone bool) RevisionItem {
	return RevisionItem{ID: id(r.ID), Entity: r.Entity, EntityID: id(r.EntityID), Action: r.Action, ActorKind: r.ActorKind,
		ActorKey: r.ActorKey, ReviewedBy: r.ReviewedBy, Batch: r.Batch, Note: r.Note, Changes: r.Changes, Undone: undone, At: r.CreatedAt}
}
