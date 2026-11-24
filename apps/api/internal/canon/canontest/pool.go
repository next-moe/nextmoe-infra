package canontest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
)

// Pool is an ingest pool held in memory.
type Pool struct {
	Rows    []contract.OutboxEntry
	Snaps   map[string]contract.Snapshot
	Recs    map[string]contract.Record
	Answers []contract.InboxRow
	Blocked []contract.BlockItem
	Orgs    []contract.RefBatch
	// NotFound is what a missing piece of evidence answers (fanin.ErrNotFound,
	// which this package cannot import).
	NotFound error
	// Down makes evidence reads fail the way an unreachable pool does.
	Down bool
}

func NewPool() *Pool {
	return &Pool{Snaps: map[string]contract.Snapshot{}, Recs: map[string]contract.Record{}, NotFound: errors.New("not in the pool")}
}

var errDown = errors.New("connection refused")

func (p *Pool) missing() error {
	if p.Down {
		return errDown
	}
	return p.NotFound
}

func (p *Pool) Outbox(_ context.Context, after int64, limit int) (contract.OutboxPage, error) {
	var page contract.OutboxPage
	for _, e := range p.Rows {
		if e.ID > after && len(page.Items) < limit {
			page.Items = append(page.Items, e)
			page.NextAfter = e.ID
		}
	}
	return page, nil
}

func (p *Pool) Snapshot(_ context.Context, h string) (contract.Snapshot, error) {
	s, ok := p.Snaps[h]
	if !ok || p.Down {
		return s, p.missing()
	}
	return s, nil
}

func (p *Pool) Record(_ context.Context, id string) (contract.Record, error) {
	r, ok := p.Recs[id]
	if !ok || p.Down {
		return r, p.missing()
	}
	return r, nil
}

func (p *Pool) Block(_ context.Context, items []contract.BlockItem) error {
	p.Blocked = append(p.Blocked, items...)
	return nil
}

func (p *Pool) Refs(_ context.Context, b contract.RefBatch) error {
	p.Orgs = append(p.Orgs, b)
	return nil
}

func (p *Pool) Answer(_ context.Context, rows []contract.InboxRow) error {
	p.Answers = append(p.Answers, rows...)
	return nil
}

// Take returns the answers since the last Take.
func (p *Pool) Take() []contract.InboxRow {
	out := p.Answers
	p.Answers = nil
	return out
}

// Changeset puts a changeset in the outbox and returns its id.
func (p *Pool) Changeset(lead string, es ...contract.NewEntity) int64 {
	return p.add(contract.OutboxRow{Kind: "changeset", Changeset: &contract.Changeset{LeadID: lead, Entities: es}})
}

func (p *Pool) Candidate(c contract.Candidate) int64 {
	return p.add(contract.OutboxRow{Kind: "candidate", Candidate: &c})
}

func (p *Pool) add(row contract.OutboxRow) int64 {
	row.ContractVersion, row.Program = contract.Version, "ln-pilot"
	id := int64(len(p.Rows) + 1)
	p.Rows = append(p.Rows, contract.OutboxEntry{ID: id, Row: row})
	return id
}

// AddRecord puts a registry record in the pool under id.
func (p *Pool) AddRecord(id, source string, fields map[string]any) {
	b, _ := json.Marshal(fields)
	p.Recs[id] = contract.Record{ID: id, Source: source, Key: id, Hash: "x", FetchedAt: "2026-11-22T00:00:00Z", Fields: b}
}

func (p *Pool) AddSnapshot(url string, lines ...string) contract.Snapshot {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	s := contract.Snapshot{Hash: hex.EncodeToString(sum[:]), RequestedURL: url, FinalURL: url, FetchedAt: "2026-11-22T00:00:00Z", Loaded: true, Lines: lines}
	p.Snaps[s.Hash] = s
	return s
}

func Val(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// Cite is a value read off a record's field.
func Cite(record string, v any, path string) contract.Candidate {
	return contract.Candidate{Value: Val(v), Tier: "registry", Evidence: []contract.Evidence{{RecordID: record, Path: path}}}
}

// Ref points at an entity of the same changeset.
func Ref(r string) contract.Candidate { return contract.Candidate{Value: Val("ref:" + r)} }

func New(ref, entity string, props map[string]contract.Candidate) contract.NewEntity {
	return contract.NewEntity{Ref: ref, Entity: entity, Properties: props}
}

// ISBN returns a fresh valid ISBN-13 so reruns on a shared test database do
// not collide.
func ISBN() string {
	base := fmt.Sprintf("978%09d", rand.IntN(1_000_000_000))
	sum := 0
	for i, r := range base {
		n := int(r - '0')
		if i%2 == 1 {
			n *= 3
		}
		sum += n
	}
	return fmt.Sprintf("%s%d", base, (10-sum%10)%10)
}

// Title is a fresh title that does not end in digits, which the identity
// search reads as a volume.
func Title(tail string) string { return "試験" + ISBN() + tail }

// Mode sets a program's mode for one test.
func Mode(t *testing.T, client *ent.Client, program, mode string) {
	t.Helper()
	ctx := context.Background()
	set := func(m string) {
		if err := client.ProgramState.Create().SetID(program).SetMode(m).SetSetBy("test").
			OnConflictColumns("id").UpdateNewValues().Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	set(mode)
	t.Cleanup(func() { _ = client.ProgramState.DeleteOneID(program).Exec(ctx) })
}
