package fanin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/contract"
	"api/internal/canon/ent/bookrelease"
	"api/internal/canon/ent/todo"
)

type fakePool struct {
	outbox  []contract.OutboxEntry
	snaps   map[string]contract.Snapshot
	recs    map[string]contract.Record
	answers []contract.InboxRow
}

func (p *fakePool) Outbox(_ context.Context, after int64, limit int) (contract.OutboxPage, error) {
	var page contract.OutboxPage
	for _, e := range p.outbox {
		if e.ID > after && len(page.Items) < limit {
			page.Items = append(page.Items, e)
			page.NextAfter = e.ID
		}
	}
	return page, nil
}

func (p *fakePool) Snapshot(_ context.Context, h string) (contract.Snapshot, error) {
	s, ok := p.snaps[h]
	if !ok {
		return s, errNotFound
	}
	return s, nil
}

func (p *fakePool) Record(_ context.Context, id string) (contract.Record, error) {
	r, ok := p.recs[id]
	if !ok {
		return r, errNotFound
	}
	return r, nil
}

func (p *fakePool) Answer(_ context.Context, rows []contract.InboxRow) error {
	p.answers = append(p.answers, rows...)
	return nil
}

func snapshot(url string, lines ...string) contract.Snapshot {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	h := hex.EncodeToString(sum[:])
	return contract.Snapshot{Hash: h, RequestedURL: url, FinalURL: url, FetchedAt: "2026-11-22T00:00:00Z", Loaded: true, Lines: lines}
}

func isbn() string {
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

func val(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func fromRecord(v any, path string) contract.Candidate {
	return contract.Candidate{Value: val(v), Tier: "registry", Evidence: []contract.Evidence{{RecordID: "ndl:1", Path: path}}}
}

func ref(r string) contract.Candidate { return contract.Candidate{Value: val("ref:" + r)} }

func TestFaninCommitsVerifiedRowsAndRefusesTheRest(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	code := isbn()
	name := fmt.Sprintf("test-%d", rand.Int64())
	fields := fmt.Sprintf(`{"title":"アクセル・ワールド","volume":"28","isbn":"%s","date":"2025.8","label":"電撃文庫 ; 4347"}`, code)
	page := snapshot("https://www.kadokawa.co.jp/product/322508000515/",
		"アクセル・ワールド28", "定価: 924円（本体840円+税）", "発売日：2025年08月08日", "ISBN："+code)
	pool := &fakePool{
		snaps: map[string]contract.Snapshot{page.Hash: page},
		recs:  map[string]contract.Record{"ndl:1": {ID: "ndl:1", Source: "ndl", Key: code, Hash: "x", FetchedAt: "2026-11-22T00:00:00Z", Fields: json.RawMessage(fields)}},
	}
	add := func(row contract.OutboxRow) {
		row.ContractVersion, row.Program = contract.Version, "ln-pilot"
		pool.outbox = append(pool.outbox, contract.OutboxEntry{ID: int64(len(pool.outbox) + 1), Row: row})
	}
	add(contract.OutboxRow{Kind: "changeset", Changeset: &contract.Changeset{LeadID: "l1", Entities: []contract.NewEntity{
		{Ref: "w", Entity: "work", Properties: map[string]contract.Candidate{"medium": {Value: val("novel")}}},
		{Ref: "t", Entity: "work_title", Properties: map[string]contract.Candidate{"work_id": ref("w"), "text": fromRecord("アクセル・ワールド", "title"), "lang": fromRecord("ja", "title"), "kind": fromRecord("official", "title")}},
		{Ref: "r", Entity: "release", Properties: map[string]contract.Candidate{"lang": fromRecord("ja", "title"), "format": {Value: val("paperback"), Evidence: []contract.Evidence{{RecordID: "ndl:1", Path: "label"}}}, "released": fromRecord("2025-08", "date")}},
		{Ref: "b", Entity: "book_release", Properties: map[string]contract.Candidate{"release_id": ref("r"), "isbn13": fromRecord(code, "isbn")}},
	}}})
	cands := []contract.Candidate{
		{Value: val("2025-08-08"), Evidence: []contract.Evidence{{SnapshotID: page.Hash, LineFrom: 3, LineTo: 3}}},
		{Value: val("2025-08-09"), Evidence: []contract.Evidence{{SnapshotID: page.Hash, LineFrom: 3, LineTo: 3}}},
		{Value: val("2025-08-08"), Evidence: []contract.Evidence{{SnapshotID: page.Hash, LineFrom: 3, LineTo: 9}}},
		{Value: val("2025-08-08"), Evidence: []contract.Evidence{{SnapshotID: "deadbeef", LineFrom: 1, LineTo: 1}}},
	}

	if _, err := New(name, pool, client).RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if len(pool.answers) != 1 || pool.answers[0].Result != "written" {
		t.Fatalf("changeset answer: %+v", pool.answers)
	}
	br, err := client.BookRelease.Query().Where(bookrelease.Isbn13(code)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := client.Release.Get(ctx, br.ReleaseID)
	if rel.Released == nil || *rel.Released != "2025-08" || rel.FieldState["released"].(map[string]any)["assurance"] != "registry" {
		t.Fatalf("release from the NDL record: %+v", rel)
	}
	if n, _ := client.EvidenceRecord.Query().Count(ctx); n == 0 {
		t.Error("the NDL record was not archived")
	}

	for _, c := range cands {
		c.Target = contract.Target{Entity: "release", ID: rel.ID}
		c.Tier = "official"
		cc := c
		add(contract.OutboxRow{Kind: "candidate", Candidate: &contract.Candidate{Target: cc.Target, Value: cc.Value, Tier: cc.Tier, Evidence: cc.Evidence, Property: "released"}})
	}
	add(contract.OutboxRow{Kind: "changeset", Changeset: &contract.Changeset{LeadID: "l2", Entities: []contract.NewEntity{
		{Ref: "b", Entity: "book_release", Properties: map[string]contract.Candidate{"release_id": {Value: val(rel.ID)}, "isbn13": fromRecord(code, "isbn")}},
	}}})
	pool.outbox = append(pool.outbox, contract.OutboxEntry{ID: int64(len(pool.outbox) + 1), Row: contract.OutboxRow{ContractVersion: "9.9.9", Kind: "lead_result"}})

	pool.answers = nil
	if _, err := New(name, pool, client).RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	want := []struct{ result, code string }{
		{"written", ""}, {"refused", "VALUE_NOT_ENTAILED"}, {"refused", "EXCERPT_NOT_ON_PAGE"},
		{"refused", "SNAPSHOT_MISSING"}, {"refused", "UNIQUE_CONFLICT"}, {"refused", "CONTRACT_VERSION_UNSUPPORTED"},
	}
	if len(pool.answers) != len(want) {
		t.Fatalf("answers %+v", pool.answers)
	}
	for i, w := range want {
		if a := pool.answers[i]; a.Result != w.result || a.Code != w.code {
			t.Errorf("answer %d: %s %s %s, want %s %s", i, a.Result, a.Code, a.Detail, w.result, w.code)
		}
	}
	rel, _ = client.Release.Get(ctx, rel.ID)
	if *rel.Released != "2025-08-08" || rel.FieldState["released"].(map[string]any)["assurance"] != "official" {
		t.Errorf("the official page should refine the date: %v %v", *rel.Released, rel.FieldState["released"])
	}

	if err := client.FaninCursor.DeleteOneID(name).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := client.Work.Query().Count(ctx)
	if _, err := New(name, pool, client).RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if after, _ := client.Work.Query().Count(ctx); after != before {
		t.Errorf("replaying the pool created %d more works", after-before)
	}
}

func TestSameNamedPersonBecomesOneTodo(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	name := fmt.Sprintf("同名%d", rand.Int64())
	rec := contract.Record{ID: "ndl:p", Source: "ndl", Key: "p", Hash: "x", FetchedAt: "2026-11-22T00:00:00Z",
		Fields: json.RawMessage(fmt.Sprintf(`{"creators":[{"name":%q}]}`, name))}
	pool := &fakePool{recs: map[string]contract.Record{"ndl:p": rec}}
	person := func() {
		pool.outbox = append(pool.outbox, contract.OutboxEntry{ID: int64(len(pool.outbox) + 1), Row: contract.OutboxRow{
			ContractVersion: contract.Version, Program: "ln-pilot", Kind: "changeset", Changeset: &contract.Changeset{LeadID: "l", Entities: []contract.NewEntity{
				{Ref: "p", Entity: "person", Properties: map[string]contract.Candidate{}},
				{Ref: "a", Entity: "person_alias", Properties: map[string]contract.Candidate{"person_id": ref("p"),
					"name": {Value: val(name), Evidence: []contract.Evidence{{RecordID: "ndl:p", Path: "creators.0.name"}}}}},
			}}}})
	}
	person()
	person()
	person()
	f := New(fmt.Sprintf("todo-%d", rand.Int64()), pool, client)
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if pool.answers[0].Result != "written" || pool.answers[1].Result != "todo" || pool.answers[2].Result != "todo" {
		t.Fatalf("answers %+v", pool.answers)
	}
	n, err := client.Todo.Query().Where(todo.SubjectKey("identity:person_alias:" + name)).Count(ctx)
	if err != nil || n != 1 {
		t.Errorf("todos for one question: %d %v", n, err)
	}
}
