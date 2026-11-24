// Package commit is canon's single write path (plan 07 §1): every change,
// from fanin, the editor or a migration, is validated against the registry
// and applied in one transaction, all of a changeset or none of it.
package commit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/identity"
	"api/internal/canon/registry"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type ActorKind string

const (
	ActorAgent  ActorKind = "agent"
	ActorHuman  ActorKind = "human"
	ActorSystem ActorKind = "system"
)

// Actor is who a value belongs to. Reviewer approved an agent's write (D29):
// the value stays the agent's, so stronger evidence may still replace it, and
// the approval is on the stamp and the revision.
type Actor struct {
	Kind     ActorKind `json:"kind"`
	Key      string    `json:"key"`
	Reviewer string    `json:"reviewer,omitempty"`
}

type EvidenceRef struct {
	Snapshot string `json:"snapshot,omitempty"`
	LineFrom int    `json:"line_from,omitempty"`
	LineTo   int    `json:"line_to,omitempty"`
	Record   string `json:"record,omitempty"`
	Path     string `json:"path,omitempty"`
}

// Value's Source is the registered source its evidence comes from; two sources
// of one tier that disagree conflict, one source that changes its mind does not.
type Value struct {
	Raw      json.RawMessage `json:"raw"`
	Tier     registry.Tier   `json:"tier,omitempty"`
	Source   string          `json:"source,omitempty"`
	Evidence []EvidenceRef   `json:"evidence,omitempty"`
}

// Change creates an entity when ID is zero; Ref names it for later changes
// of the same changeset, which point at it as "ref:<Ref>".
type Change struct {
	Ref    string           `json:"ref,omitempty"`
	Entity string           `json:"entity"`
	ID     int64            `json:"id,omitempty"`
	Values map[string]Value `json:"values,omitempty"`
	// AdoptID creates the entity under a legacy id a reviewer or a person
	// judged to be the same one (D30).
	AdoptID int64 `json:"adopt_id,omitempty"`
	// ExpectVersion refuses the change when the entity moved on; 0 does not check.
	ExpectVersion int `json:"expect_version,omitempty"`
	// Distinct lists the identity candidates judged to be other entities, as
	// Candidate.Token; one missing from it refuses the create (06 §6 I1).
	Distinct []string `json:"distinct,omitempty"`
	// Authority is a registry authority (NDL) the name was printed under.
	Authority string `json:"authority,omitempty"`
}

type Changeset struct {
	Actor   Actor    `json:"actor"`
	Note    string   `json:"note,omitempty"`
	Changes []Change `json:"changes"`
	// Key, when set, makes Apply idempotent: a second Apply with the same key
	// writes nothing and returns the first result.
	Key string `json:"key,omitempty"`
}

type Result struct {
	IDs      map[string]int64
	Batch    string
	Replayed bool
}

type Refusal struct {
	Change     int
	Property   string
	Code       string
	Detail     string
	Candidates []identity.Candidate
}

type Refused []Refusal

func (r Refused) Error() string {
	parts := make([]string, len(r))
	for i, x := range r {
		parts[i] = fmt.Sprintf("change %d %s: %s %s", x.Change, x.Property, x.Code, x.Detail)
	}
	return strings.Join(parts, "; ")
}

func (r Refused) Has(code string) bool {
	for _, x := range r {
		if x.Code == code {
			return true
		}
	}
	return false
}

type Service struct {
	client *ent.Client
	index  map[string]map[string]registry.Property
	now    func() time.Time
}

func New(client *ent.Client) *Service {
	return &Service{client: client, index: contract.Index(), now: time.Now}
}

func (s *Service) Apply(ctx context.Context, cs Changeset) (Result, error) {
	return s.run(ctx, cs, true)
}

// Check is Apply that keeps nothing: what a changeset would be refused for now.
func (s *Service) Check(ctx context.Context, cs Changeset) error {
	_, err := s.run(ctx, cs, false)
	return err
}

// Prior is the result a key was already applied with.
func (s *Service) Prior(ctx context.Context, key string) (Result, bool, error) {
	k, err := s.client.CommitKey.Get(ctx, key)
	if ent.IsNotFound(err) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	return Result{IDs: k.Ids, Batch: k.Batch, Replayed: true}, true, nil
}

func (s *Service) run(ctx context.Context, cs Changeset, keep bool) (Result, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	if cs.Key != "" {
		prior, err := tx.CommitKey.Get(ctx, cs.Key)
		if err == nil {
			return Result{IDs: prior.Ids, Batch: prior.Batch, Replayed: true}, nil
		}
		if !ent.IsNotFound(err) {
			return Result{}, err
		}
	}
	a := &applier{s: s, tx: tx, cs: cs, batch: uuid.NewString(), ids: map[string]int64{}, created: map[string]Change{}}
	for i, ch := range cs.Changes {
		if err := a.apply(ctx, i, ch); err != nil {
			return Result{}, err
		}
	}
	if !keep {
		return Result{}, nil
	}
	if cs.Key != "" {
		if err := tx.CommitKey.Create().SetID(cs.Key).SetIds(a.ids).SetBatch(a.batch).Exec(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Result{}, uniqueRefusal(err, -1)
	}
	return Result{IDs: a.ids, Batch: a.batch}, nil
}

// uniqueRefusal turns a unique-index race (two commits between the pre-check
// and the insert) into the same refusal the pre-check gives.
func uniqueRefusal(err error, change int) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return Refused{{Change: change, Code: "UNIQUE_CONFLICT", Detail: pg.ConstraintName}}
	}
	return err
}
