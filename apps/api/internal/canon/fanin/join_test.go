package fanin

import (
	"context"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/workpart"
	"api/internal/canon/registry"
)

type volume struct {
	lead, title, isbn, author, illustrator string
	label                                  int64
}

func offer(pool *canontest.Pool, labelName string, v volume) {
	rec := "jpro:" + v.isbn
	creators := []map[string]any{{"name": v.author, "role_text": "著"}}
	if v.illustrator != "" {
		creators = append(creators, map[string]any{"name": v.illustrator, "role_text": "イラスト"})
	}
	pool.AddRecord(rec, "jpro", map[string]any{"title": v.title, "isbn": v.isbn, "label": labelName, "creators": creators})
	c := func(x any, path string) contract.Candidate { return canontest.Cite(rec, x, path) }
	named := func(ref, path string) contract.Candidate {
		x := canontest.Ref(ref)
		x.Tier, x.Evidence = "registry", []contract.Evidence{{RecordID: rec, Path: path}}
		return x
	}
	es := []contract.NewEntity{
		canontest.New("work", "work", props{"medium": {Value: canontest.Val("novel")}}),
		canontest.New("work.title", "work_title", props{"work_id": canontest.Ref("work"), "text": c(v.title, "title"), "lang": c("ja", "title"), "kind": c("official", "title")}),
		canontest.New("work.book", "book_work", props{"work_id": canontest.Ref("work"), "category": c("light_novel", "label")}),
		canontest.New("person.a", "person", props{}),
		canontest.New("alias.a", "person_alias", props{"person_id": canontest.Ref("person.a"), "name": c(v.author, "creators.0.name")}),
		canontest.New("credit.0", "credit", props{"work_id": canontest.Ref("work"), "alias_id": named("alias.a", "creators.0.name"), "role": c("author", "creators.0.role_text")}),
	}
	if v.illustrator != "" {
		es = append(es,
			canontest.New("person.b", "person", props{}),
			canontest.New("alias.b", "person_alias", props{"person_id": canontest.Ref("person.b"), "name": c(v.illustrator, "creators.1.name")}),
			canontest.New("credit.1", "credit", props{"work_id": canontest.Ref("work"), "alias_id": named("alias.b", "creators.1.name"), "role": c("illustrator", "creators.1.role_text")}))
	}
	es = append(es,
		canontest.New("part", "work_part", props{"work_id": canontest.Ref("work"), "kind": c("volume", "isbn")}),
		canontest.New("release.1", "release", props{"lang": c("ja", "title"), "format": c("paperback", "label")}),
		canontest.New("content.1", "release_content", props{"release_id": canontest.Ref("release.1"), "work_id": named("work", "title"), "part_id": canontest.Ref("part")}),
		canontest.New("book.1", "book_release", props{"release_id": canontest.Ref("release.1"), "isbn13": c(v.isbn, "isbn"), "label_id": c(v.label, "label")}))
	pool.Changeset(v.lead, es...)
}

func labelOrg(t *testing.T, client *ent.Client, name string) int64 {
	t.Helper()
	o, err := client.Org.Create().SetKind("label").Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.OrgName.Create().SetOrgID(o.ID).SetText(name).SetLang("ja").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func TestTheSameTitleAuthorAndLabelIsTheSameWork(t *testing.T) {
	client, db := canontest.Client(t)
	canontest.Legacy(t, db)
	canontest.Mode(t, client, "ln-pilot", registry.ModeAuto)
	ctx := context.Background()
	labelName, otherName := canontest.Title("文庫"), canontest.Title("文庫")
	label, other := labelOrg(t, client, labelName), labelOrg(t, client, otherName)
	title, author := canontest.Title("の庭"), "著者"+canontest.ISBN()+"郎"
	pool := canontest.NewPool()
	f := New(fresh("join"), pool, client)
	run := func() contract.InboxRow {
		if _, err := f.RunOnce(ctx, 10); err != nil {
			t.Fatal(err)
		}
		a := pool.Take()
		return a[len(a)-1]
	}
	offer(pool, labelName, volume{"a", title, canontest.ISBN(), author, "", label})
	first := run()
	if first.Result != "written" {
		t.Fatalf("first %+v", first)
	}
	offer(pool, labelName, volume{"b", title, canontest.ISBN(), author, "絵師" + canontest.ISBN() + "子", label})
	second := run()
	if second.Result != "written" || second.IDs["work"] != first.IDs["work"] || second.IDs["alias.a"] != first.IDs["alias.a"] {
		t.Fatalf("second %+v", second)
	}
	parts, _ := client.WorkPart.Query().Where(workpart.WorkID(first.IDs["work"])).Count(ctx)
	credits, _ := client.Credit.Query().Where(credit.WorkID(first.IDs["work"])).Count(ctx)
	if parts != 2 || credits != 2 {
		t.Errorf("parts %d, credits %d: the author once and the illustrator the record adds", parts, credits)
	}

	offer(pool, otherName, volume{"c", title, canontest.ISBN(), author, "", other})
	if a := run(); a.Result != "todo" {
		t.Errorf("another label is a question: %+v", a)
	}
	offer(pool, labelName, volume{"d", title, canontest.ISBN(), "別人" + canontest.ISBN() + "郎", "", label})
	if a := run(); a.Result != "todo" {
		t.Errorf("another author is a question: %+v", a)
	}
	offer(pool, labelName, volume{"e", title + " ３", canontest.ISBN(), author, "", label})
	if a := run(); a.Result != "todo" {
		t.Errorf("a title that only resembles it is a question: %+v", a)
	}
}
