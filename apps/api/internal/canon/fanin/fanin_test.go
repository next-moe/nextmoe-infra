package fanin

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/contract"
	"api/internal/canon/ent/bookrelease"
	"api/internal/canon/registry"
	"api/internal/canon/takedown"
)

type props = map[string]contract.Candidate

func fresh(tag string) string { return fmt.Sprintf("%s-%d", tag, rand.Int64()) }

func TestFaninCommitsVerifiedRowsAndRefusesTheRest(t *testing.T) {
	client, _ := canontest.Client(t)
	canontest.Mode(t, client, "ln-pilot", registry.ModeAuto)
	ctx := context.Background()
	code, title := canontest.ISBN(), canontest.Title("の世界")
	name := fresh("test")
	pool := canontest.NewPool()
	pool.NotFound = ErrNotFound
	pool.AddRecord("ndl:1", "ndl", map[string]any{"title": title, "volume": "28", "isbn": code, "date": "2025.8", "label": "電撃文庫 ; 4347"})
	page := pool.AddSnapshot("https://www.kadokawa.co.jp/product/322508000515/",
		title+"28", "定価: 924円（本体840円+税）", "発売日：2025年08月08日", "ISBN："+code)
	from := func(v any, path string) contract.Candidate { return canontest.Cite("ndl:1", v, path) }
	pool.Changeset("l1",
		canontest.New("w", "work", props{"medium": {Value: canontest.Val("novel")}}),
		canontest.New("t", "work_title", props{"work_id": canontest.Ref("w"), "text": from(title, "title"), "lang": from("ja", "title"), "kind": from("official", "title")}),
		canontest.New("r", "release", props{"lang": from("ja", "title"), "format": from("paperback", "label"), "released": from("2025-08", "date")}),
		canontest.New("b", "book_release", props{"release_id": canontest.Ref("r"), "isbn13": from(code, "isbn")}),
	)
	f := New(name, pool, client)
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if a := pool.Take(); len(a) != 1 || a[0].Result != "written" {
		t.Fatalf("changeset answer: %+v", a)
	}
	br, err := client.BookRelease.Query().Where(bookrelease.Isbn13(code)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := client.Release.Get(ctx, br.ReleaseID)
	st := rel.FieldState["released"].(map[string]any)
	if rel.Released == nil || *rel.Released != "2025-08" || st["assurance"] != "registry" || st["source"] != "ndl" {
		t.Fatalf("release from the NDL record: %+v", rel)
	}
	if br.FieldState["isbn13"].(map[string]any)["source"] != "ndl" {
		t.Errorf("book_release is stamped too: %v", br.FieldState)
	}
	if n, _ := client.EvidenceRecord.Query().Count(ctx); n == 0 {
		t.Error("the NDL record was not archived")
	}

	at := func(from, to int) []contract.Evidence {
		return []contract.Evidence{{SnapshotID: page.Hash, LineFrom: from, LineTo: to}}
	}
	for _, c := range []contract.Candidate{
		{Value: canontest.Val("2025-08-08"), Evidence: at(3, 3)},
		{Value: canontest.Val("2025-08-09"), Evidence: at(3, 3)},
		{Value: canontest.Val("2025-08-08"), Evidence: at(3, 9)},
		{Value: canontest.Val("2025-08-08"), Evidence: []contract.Evidence{{SnapshotID: "deadbeef", LineFrom: 1, LineTo: 1}}},
	} {
		c.Target, c.Tier, c.Property = contract.Target{Entity: "release", ID: rel.ID}, "official", "released"
		pool.Candidate(c)
	}
	pool.Changeset("l2", canontest.New("b", "book_release", props{"release_id": {Value: canontest.Val(rel.ID)}, "isbn13": from(code, "isbn")}))
	pool.Rows = append(pool.Rows, contract.OutboxEntry{ID: int64(len(pool.Rows) + 1), Row: contract.OutboxRow{ContractVersion: "9.9.9", Kind: "lead_result"}})
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	want := []struct{ result, code string }{
		{"written", ""}, {"refused", "VALUE_NOT_ENTAILED"}, {"refused", "EXCERPT_NOT_ON_PAGE"},
		{"refused", "SNAPSHOT_MISSING"}, {"refused", "UNIQUE_CONFLICT"}, {"refused", "CONTRACT_VERSION_UNSUPPORTED"},
	}
	got := pool.Take()
	if len(got) != len(want) {
		t.Fatalf("answers %+v", got)
	}
	for i, w := range want {
		if a := got[i]; a.Result != w.result || a.Code != w.code {
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
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if a := pool.Take(); a[0].Result != "written" || a[0].IDs["w"] == 0 {
		t.Errorf("a replayed row answers as it did: %+v", a[0])
	}
	if after, _ := client.Work.Query().Count(ctx); after != before {
		t.Errorf("replaying the pool created %d more works", after-before)
	}
}

func TestARefIsWrittenOnlyWhereItsNameIs(t *testing.T) {
	client, _ := canontest.Client(t)
	canontest.Mode(t, client, "ln-pilot", registry.ModeAuto)
	ctx := context.Background()
	label, err := client.Org.Create().SetKind("label").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	labelName := canontest.Title("文庫")
	if err := client.OrgName.Create().SetOrgID(label.ID).SetText(labelName).SetLang("ja").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	pool := canontest.NewPool()
	pool.AddRecord("ndl:a", "ndl", map[string]any{"label": labelName, "other": labelName + "f",
		"creators": []map[string]any{{"name": "山田" + canontest.ISBN() + "花", "role_text": "著"}, {"name": "佐藤", "role_text": "イラスト"}}})
	release := func(lead, path string) {
		pool.Changeset(lead,
			canontest.New("r", "release", props{"lang": canontest.Cite("ndl:a", "ja", "label"), "format": canontest.Cite("ndl:a", "paperback", "label")}),
			canontest.New("b", "book_release", props{"release_id": canontest.Ref("r"), "label_id": canontest.Cite("ndl:a", label.ID, path)}))
	}
	release("named", "label")
	release("other", "other")
	credited := func(lead, rolePath string) {
		name := canontest.Cite("ndl:a", firstName(pool), "creators.0.name")
		pool.Changeset(lead,
			canontest.New("w", "work", props{"medium": {Value: canontest.Val("novel")}}),
			canontest.New("p", "person", props{}),
			canontest.New("a", "person_alias", props{"person_id": canontest.Ref("p"), "name": name}),
			canontest.New("c", "credit", props{"work_id": canontest.Ref("w"), "alias_id": citeRef("a", "creators.0.name"), "role": canontest.Cite("ndl:a", roleAt(rolePath), rolePath)}))
	}
	credited("together", "creators.0.role_text")
	credited("apart", "creators.1.role_text")
	if _, err := New(fresh("named"), pool, client).RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	want := []string{"written", "refused", "written", "refused"}
	for i, a := range pool.Take() {
		if a.Result != want[i] {
			t.Errorf("row %d: %s %s %s, want %s", i+1, a.Result, a.Code, a.Detail, want[i])
		}
	}
	br, err := client.BookRelease.Query().Where(bookrelease.LabelID(label.ID)).Only(ctx)
	if err != nil || br.FieldState["label_id"].(map[string]any)["assurance"] != "registry" {
		t.Errorf("the label is written and graded: %+v %v", br, err)
	}
}

func firstName(p *canontest.Pool) string {
	v, _ := lookup(p.Recs["ndl:a"].Fields, "creators.0.name")
	return v
}

func roleAt(path string) string {
	if path == "creators.0.role_text" {
		return "author"
	}
	return "illustrator"
}

func citeRef(ref, path string) contract.Candidate {
	c := canontest.Ref(ref)
	c.Tier, c.Evidence = "registry", []contract.Evidence{{RecordID: "ndl:a", Path: path}}
	return c
}

func TestAStoppedProgramsRowsStayInThePool(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	canontest.Mode(t, client, "ln-pilot", registry.ModeStopped)
	pool := canontest.NewPool()
	pool.Changeset("l", canontest.New("w", "work", props{"medium": {Value: canontest.Val("novel")}}))
	f := New(fresh("stopped"), pool, client)
	if n, err := f.RunOnce(ctx, 10); err != nil || n != 0 || len(pool.Take()) != 0 {
		t.Errorf("rows handled: %d %v", n, err)
	}
	canontest.Mode(t, client, "ln-pilot", registry.ModeAuto)
	if n, err := f.RunOnce(ctx, 10); err != nil || n != 1 {
		t.Errorf("after starting again: %d %v", n, err)
	}
}

func TestTakedownsReachThePoolAndLiftOnRestore(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	w, err := client.Work.Create().SetMedium("novel").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	td, err := takedown.Take(ctx, client, takedown.Notice{NoticeID: "trust:9", Scope: "entity", Entity: "work", EntityID: w.ID,
		Reason: "rights holder", By: "user:1", ExcludeHost: "rights.example.jp"})
	if err != nil {
		t.Fatal(err)
	}
	pool := canontest.NewPool()
	f := New(fresh("block"), pool, client)
	mine := func() []contract.BlockItem {
		var out []contract.BlockItem
		for _, it := range pool.Blocked {
			if it.TakedownID == td.ID {
				out = append(out, it)
			}
		}
		pool.Blocked = nil
		return out
	}
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	got := mine()
	if len(got) != 2 || !got[0].Active || got[0].Kind != "entity" || got[0].EntityID != w.ID || got[1].Kind != "host" || got[1].Value != "rights.example.jp" {
		t.Fatalf("blocked %+v", got)
	}
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if again := mine(); len(again) != 0 {
		t.Errorf("pushed twice: %+v", again)
	}
	if err := takedown.Restore(ctx, client, td.ID, "user:1", "counter-notice"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if lifted := mine(); len(lifted) != 2 || lifted[0].Active || lifted[1].Active {
		t.Errorf("restore did not lift: %+v", lifted)
	}
}

func TestAnUnreachablePoolIsNotAVerdict(t *testing.T) {
	client, _ := canontest.Client(t)
	canontest.Mode(t, client, "ln-pilot", registry.ModeAuto)
	ctx := context.Background()
	pool := canontest.NewPool()
	pool.NotFound = ErrNotFound
	title := canontest.Title("の庭")
	pool.AddRecord("ndl:a", "ndl", map[string]any{"title": title})
	pool.Changeset("l", canontest.New("w", "work", props{"medium": {Value: canontest.Val("novel")}}),
		canontest.New("t", "work_title", props{"work_id": canontest.Ref("w"), "text": canontest.Cite("ndl:a", title, "title"), "lang": canontest.Cite("ndl:a", "ja", "title"), "kind": canontest.Cite("ndl:a", "official", "title")}))
	f := New(fresh("down"), pool, client)
	pool.Down = true
	if _, err := f.RunOnce(ctx, 10); err == nil || len(pool.Take()) != 0 {
		t.Error("a pool that cannot be read answered a row")
	}
	pool.Down = false
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if a := pool.Take(); len(a) != 1 || a[0].Result != "written" {
		t.Errorf("answers %+v", a)
	}
}

func TestOrgNamesReachThePoolOnceUntilTheyChange(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	org, err := client.Org.Create().SetKind("label").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := canontest.Title("文庫")
	if err := client.OrgName.Create().SetOrgID(org.ID).SetText(name).SetLang("ja").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	pool := canontest.NewPool()
	f := New(fresh("refs"), pool, client)
	for range 2 {
		if _, err := f.RunOnce(ctx, 10); err != nil {
			t.Fatal(err)
		}
	}
	if len(pool.Orgs) != 1 {
		t.Fatalf("pushed %d times", len(pool.Orgs))
	}
	sent := false
	for _, it := range pool.Orgs[0].Items {
		sent = sent || it.ID == org.ID && it.Name == name
	}
	if !sent {
		t.Error("the label is not in the list")
	}
	if err := client.Hold.Create().SetEntity("org").SetEntityID(org.ID).SetReason("dispute").SetCreatedBy("user:1").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if len(pool.Orgs) != 2 {
		t.Errorf("a held organization is withdrawn: %d pushes", len(pool.Orgs))
	}
}
