package review_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/bookrelease"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/personalias"
	"api/internal/canon/ent/todo"
	"api/internal/canon/ent/worktitle"
	"api/internal/canon/fanin"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
	"api/internal/canon/review"
)

type props = map[string]contract.Candidate

var (
	grok   = review.Reviewer{Key: "review:grok"}
	person = review.Reviewer{Key: "user:1", Human: true}
	ctx    = context.Background()
)

type world struct {
	t      *testing.T
	client *ent.Client
	pool   *canontest.Pool
	fanin  *fanin.Fanin
	name   string
}

func newWorld(t *testing.T, mode string) *world {
	client, db := canontest.Client(t)
	canontest.Legacy(t, db)
	canontest.Mode(t, client, "ln-pilot", mode)
	w := &world{t: t, client: client, pool: canontest.NewPool(), name: fmt.Sprintf("review-%d", rand.Int64())}
	w.fanin = fanin.New(w.name, w.pool, client)
	return w
}

func (w *world) run() []contract.InboxRow {
	w.t.Helper()
	if _, err := w.fanin.RunOnce(ctx, 50); err != nil {
		w.t.Fatal(err)
	}
	return w.pool.Take()
}

func (w *world) open() []*ent.Todo {
	ts, err := w.client.Todo.Query().Where(todo.State("open"), todo.SourceHasPrefix("pool:"+w.name+":")).Order(ent.Asc(todo.FieldID)).All(ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	return ts
}

func (w *world) one() *ent.Todo {
	w.t.Helper()
	ts := w.open()
	if len(ts) != 1 {
		w.t.Fatalf("%d open todos", len(ts))
	}
	return ts[0]
}

// book offers one volume of a series by one author, as lane A does.
func (w *world) book(lead, title, isbn, author, authority string) {
	rec := "ndl:" + isbn
	w.pool.AddRecord(rec, "ndl", map[string]any{"title": title, "isbn": isbn, "label": "電撃文庫",
		"creators": []map[string]any{{"name": author, "role_text": "著", "authority": authority}}})
	c := func(v any, path string) contract.Candidate { return canontest.Cite(rec, v, path) }
	named := func(ref, path string) contract.Candidate {
		x := canontest.Ref(ref)
		x.Tier, x.Evidence = "registry", []contract.Evidence{{RecordID: rec, Path: path}}
		return x
	}
	w.pool.Changeset(lead,
		canontest.New("work", "work", props{"medium": {Value: canontest.Val("novel")}}),
		canontest.New("work.title", "work_title", props{"work_id": canontest.Ref("work"), "text": c(title, "title"), "lang": c("ja", "title"), "kind": c("official", "title")}),
		canontest.New("person.a", "person", props{}),
		canontest.New("alias.a", "person_alias", props{"person_id": canontest.Ref("person.a"), "name": c(author, "creators.0.name")}),
		canontest.New("credit.0", "credit", props{"work_id": canontest.Ref("work"), "alias_id": named("alias.a", "creators.0.name"), "role": c("author", "creators.0.role_text")}),
		canontest.New("part", "work_part", props{"work_id": canontest.Ref("work"), "kind": c("volume", "isbn")}),
		canontest.New("release.1", "release", props{"lang": c("ja", "title"), "format": c("paperback", "label")}),
		canontest.New("content.1", "release_content", props{"release_id": canontest.Ref("release.1"), "work_id": named("work", "title"), "part_id": canontest.Ref("part")}),
		canontest.New("book.1", "book_release", props{"release_id": canontest.Ref("release.1"), "isbn13": c(isbn, "isbn")}),
	)
}

func author() string { return "著者" + canontest.ISBN() + "郎" }

func TestInReviewAVerifiedRowWaitsAndIsWrittenAsTheProgramsOwn(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	title, isbn := canontest.Title("の庭"), canontest.ISBN()
	w.book("l", title, isbn, author(), "")
	if a := w.run(); a[0].Result != "todo" || a[0].Code != "AWAITING_REVIEW" {
		t.Fatalf("answer %+v", a)
	}
	if n, _ := w.client.BookRelease.Query().Where(bookrelease.Isbn13(isbn)).Count(ctx); n != 0 {
		t.Fatal("a row in review was written")
	}
	td := w.one()
	ids, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Note: "checked on the publisher's page"})
	if err != nil {
		t.Fatal(err)
	}
	br, err := w.client.BookRelease.Query().Where(bookrelease.Isbn13(isbn)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st := br.FieldState["isbn13"].(map[string]any)
	if st["by"] != "agent" || st["key"] != "program:ln-pilot" || st["reviewed_by"] != "review:grok" || st["source"] != "ndl" {
		t.Errorf("stamp %v", st)
	}
	if a := w.run(); len(a) != 1 || a[0].Result != "written" || a[0].IDs["work"] != ids["work"] || a[0].IDs["alias.a"] == 0 {
		t.Errorf("the pool learns the ids: %+v", a)
	}
	w.book("l", title, isbn, w.lastAuthor(), "")
	if a := w.run(); a[0].Result != "written" || a[0].IDs["work"] != ids["work"] {
		t.Errorf("the same content again is answered as it was settled: %+v", a)
	}
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{}); !errors.Is(err, review.ErrSettled) {
		t.Errorf("accepting twice: %v", err)
	}
}

func (w *world) lastAuthor() string {
	rows := w.pool.Rows
	for _, ne := range rows[len(rows)-1].Row.Changeset.Entities {
		if ne.Entity == "person_alias" {
			return string(ne.Properties["name"].Value[1 : len(ne.Properties["name"].Value)-1])
		}
	}
	return ""
}

func TestWhatCanonWouldRefuseNeverReachesAReviewer(t *testing.T) {
	w := newWorld(t, registry.ModeAuto)
	isbn := canontest.ISBN()
	w.book("a", canontest.Title("の庭"), isbn, author(), "")
	w.run()
	canontest.Mode(t, w.client, "ln-pilot", registry.ModeReview)
	w.book("b", canontest.Title("の森"), isbn, author(), "")
	if a := w.run(); a[0].Result != "refused" || a[0].Code != "UNIQUE_CONFLICT" {
		t.Errorf("answer %+v", a)
	}
	if len(w.open()) != 0 {
		t.Error("a todo was filed for it")
	}
}

func TestNewerContentOfALeadReplacesTheOpenTodo(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	title, who := canontest.Title("の庭"), author()
	w.book("l", title, canontest.ISBN(), who, "")
	w.run()
	first := w.one()
	w.book("l", title, canontest.ISBN(), who, "")
	w.run()
	if second := w.one(); second.ID == first.ID {
		t.Error("the open todo still holds the old content")
	}
	if got, _ := w.client.Todo.Get(ctx, first.ID); got.State != "superseded" || !got.Answered {
		t.Errorf("the old todo: %s answered=%v", got.State, got.Answered)
	}
}

func TestARejectionIsRememberedForTheSameContent(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	title, isbn, who := canontest.Title("の庭"), canontest.ISBN(), author()
	w.book("l", title, isbn, who, "")
	w.run()
	if err := review.Reject(ctx, w.client, w.one().ID, grok, ""); err == nil {
		t.Error("a rejection without a reason")
	}
	if err := review.Reject(ctx, w.client, w.one().ID, grok, "an artbook"); err != nil {
		t.Fatal(err)
	}
	w.book("l", title, isbn, who, "")
	for i, a := range w.run() {
		if a.Result != "refused" || a.Code != "REJECTED_IN_REVIEW" || a.Detail != "an artbook" {
			t.Errorf("answer %d: %+v", i, a)
		}
	}
	if len(w.open()) != 0 {
		t.Error("the same content was asked again")
	}
}

func TestASameNamedAuthorIsAQuestionUntilTheAuthorityMatches(t *testing.T) {
	w := newWorld(t, registry.ModeAuto)
	who, authority := author(), "0"+canontest.ISBN()
	w.book("a", canontest.Title("の庭"), canontest.ISBN(), who, authority)
	first := w.run()[0]
	if first.Result != "written" {
		t.Fatalf("first %+v", first)
	}
	alias := first.IDs["alias.a"]

	w.book("b", canontest.Title("の森"), canontest.ISBN(), who, authority)
	second := w.run()[0]
	if second.Result != "written" {
		t.Fatalf("one NDL authority is one person: %+v", second)
	}
	if n, _ := w.client.Credit.Query().Where(credit.AliasID(alias)).Count(ctx); n != 2 {
		t.Errorf("both works credit the one alias: %d", n)
	}

	w.book("c", canontest.Title("の海"), canontest.ISBN(), who, "")
	if a := w.run()[0]; a.Result != "todo" || a.Code != "IDENTITY_UNSURE" {
		t.Fatalf("no authority, so a question: %+v", a)
	}
	td := w.one()
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{}); err == nil {
		t.Error("an unanswered question was accepted")
	}
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Answers: map[string]review.Answer{"alias.a": {Use: alias}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.client.PersonAlias.Query().Where(personalias.Name(who)).Count(ctx); n != 1 {
		t.Errorf("%d aliases of one name", n)
	}
	var settled map[string]int64
	for _, a := range w.run() {
		settled = a.IDs
	}
	if settled["alias.a"] != alias || settled["person.a"] == 0 {
		t.Errorf("the pool learns who it was: %v", settled)
	}

	w.book("d", canontest.Title("の空"), canontest.ISBN(), who, "")
	w.run()
	if _, err := review.Accept(ctx, w.client, w.one().ID, grok, review.Decision{Answers: map[string]review.Answer{"alias.a": {New: true}}}); err != nil {
		t.Fatalf("told apart: %v", err)
	}
	if n, _ := w.client.PersonAlias.Query().Where(personalias.Name(who)).Count(ctx); n != 2 {
		t.Errorf("a namesake is another person: %d aliases", n)
	}
}

func TestAVolumeOfAKnownSeriesIsAddedToIt(t *testing.T) {
	w := newWorld(t, registry.ModeAuto)
	title, who, authority := canontest.Title("の庭"), author(), "1"+canontest.ISBN()
	w.book("a", title, canontest.ISBN(), who, authority)
	work := w.run()[0].IDs["work"]
	w.book("b", title+"　２", canontest.ISBN(), who, authority)
	if a := w.run()[0]; a.Result != "todo" {
		t.Fatalf("a title with its volume run in is a question: %+v", a)
	}
	ids, err := review.Accept(ctx, w.client, w.one().ID, grok, review.Decision{Answers: map[string]review.Answer{"work": {Use: work}}})
	if err != nil {
		t.Fatal(err)
	}
	if ids["work"] != work {
		t.Errorf("ids %v", ids)
	}
	ws, _ := w.client.Work.Get(ctx, work)
	parts, _ := ws.QueryParts().Count(ctx)
	titles, _ := w.client.WorkTitle.Query().Where(worktitle.WorkID(work)).Count(ctx)
	credits, _ := w.client.Credit.Query().Where(credit.WorkID(work)).Count(ctx)
	if parts != 2 || titles != 1 || credits != 1 {
		t.Errorf("parts %d titles %d credits %d", parts, titles, credits)
	}
}

func TestALegacyNameAndItsPersonKeepTheirIDs(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	who := author()
	base := rand.Int64N(1_000_000_000) + 8_000_000_000
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO %[1]s.catalog_person VALUES (%[2]d, NULL)`, identity.LegacySchema, base),
		fmt.Sprintf(`INSERT INTO %[1]s.catalog_credit_name (id, person_id, name) VALUES (%[2]d, %[3]d, '%[4]s'), (%[5]d, NULL, '%[4]s')`, identity.LegacySchema, base+1, base, who, base+2),
		fmt.Sprintf(`INSERT INTO %[1]s.catalog_credit VALUES (%[2]d, 1), (%[3]d, 2)`, identity.LegacySchema, base+1, base+2),
	} {
		if _, err := w.client.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	w.book("l", canontest.Title("の庭"), canontest.ISBN(), who, "")
	w.run()
	td := w.one()
	if td.Kind != registry.TodoIdentity || len(td.Questions) != 1 || len(td.Questions[0]["candidates"].([]any)) != 2 {
		t.Fatalf("todo %s %v", td.Kind, td.Questions)
	}
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Answers: map[string]review.Answer{"alias.a": {Adopt: base + 50}}}); err == nil {
		t.Error("a reviewer adopted an id that was no candidate")
	}
	ids, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Answers: map[string]review.Answer{"alias.a": {Adopt: base + 1}}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.client.PersonAlias.Get(ctx, base+1)
	if err != nil || a.PersonID != base || ids["alias.a"] != base+1 || ids["person.a"] != base {
		t.Errorf("alias %+v %v ids %v", a, err, ids)
	}
}

func TestOmittingLeavesOutWhatPointsAtIt(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	isbn, who := canontest.ISBN(), author()
	w.book("l", canontest.Title("の庭"), isbn, who, "")
	w.run()
	td := w.one()
	var invalid review.Invalid
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Omit: []string{"credit.9"}}); !errors.As(err, &invalid) {
		t.Errorf("omitting what is not there: %v", err)
	}
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Omit: []string{"alias.a"}, Note: "a production company"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.client.PersonAlias.Query().Where(personalias.Name(who)).Count(ctx); n != 0 {
		t.Error("the omitted name was written")
	}
	if n, _ := w.client.BookRelease.Query().Where(bookrelease.Isbn13(isbn)).Count(ctx); n != 1 {
		t.Error("the rest was not")
	}
}

func TestAPersonsValueIsOnlyAPersonsToReplace(t *testing.T) {
	w := newWorld(t, registry.ModeAuto)
	isbn := canontest.ISBN()
	w.book("l", canontest.Title("の庭"), isbn, author(), "")
	rel := w.run()[0].IDs["release.1"]
	if _, err := commit.New(w.client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorHuman, Key: "user:1"}, Changes: []commit.Change{
		{Entity: "release", ID: rel, Values: map[string]commit.Value{"released": {Raw: canontest.Val("2025-08-01")}}}}}); err != nil {
		t.Fatal(err)
	}
	page := w.pool.AddSnapshot("https://www.kadokawa.co.jp/product/1/", "発売日：2025年08月08日")
	w.pool.Candidate(contract.Candidate{Target: contract.Target{Entity: "release", ID: rel}, Property: "released", Value: canontest.Val("2025-08-08"), Tier: "official",
		Evidence: []contract.Evidence{{SnapshotID: page.Hash, LineFrom: 1, LineTo: 1}}})
	if a := w.run(); a[0].Result != "todo" || a[0].Code != "HUMAN_LOCKED" {
		t.Fatalf("answer %+v", a)
	}
	td := w.one()
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{}); !errors.Is(err, review.ErrPersonOnly) {
		t.Errorf("grok replaced a person's value: %v", err)
	}
	if _, err := review.Accept(ctx, w.client, td.ID, person, review.Decision{}); err != nil {
		t.Fatal(err)
	}
	got, _ := w.client.Release.Get(ctx, rel)
	if *got.Released != "2025-08-08" || got.FieldState["released"].(map[string]any)["by"] != "human" {
		t.Errorf("a person took it over: %v %v", *got.Released, got.FieldState["released"])
	}
}

func TestUndoingAReviewedCommitTellsThePool(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	title, isbn, who := canontest.Title("の庭"), canontest.ISBN(), author()
	w.book("l", title, isbn, who, "")
	w.run()
	td := w.one()
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{}); err != nil {
		t.Fatal(err)
	}
	w.run()
	settled, _ := w.client.Todo.Get(ctx, td.ID)
	batch, _ := settled.Resolution["batch"].(string)
	if err := commit.New(w.client).Undo(ctx, batch, commit.Actor{Kind: commit.ActorHuman, Key: "user:1"}, "an artbook"); err != nil {
		t.Fatal(err)
	}
	// Reruns on one database can push the program over its undo limit.
	canontest.Mode(t, w.client, "ln-pilot", registry.ModeReview)
	if a := w.run(); len(a) != 1 || a[0].Result != "refused" || a[0].Code != "REJECTED_IN_REVIEW" {
		t.Errorf("answers %+v", a)
	}
	w.book("l", title, isbn, who, "")
	if a := w.run(); a[0].Code != "REJECTED_IN_REVIEW" {
		t.Errorf("the same content comes back: %+v", a)
	}
}

func TestASheetShowsTheWordsAndWhatIsKnownOfACandidate(t *testing.T) {
	w := newWorld(t, registry.ModeAuto)
	who, title := author(), canontest.Title("の庭")
	w.book("a", title, canontest.ISBN(), who, "")
	w.run()
	w.book("b", canontest.Title("の海"), canontest.ISBN(), who, "")
	w.run()
	td := w.one()
	sheets, err := review.Sheets(ctx, w.client, []string{registry.TodoIdentity}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	i := -1
	for j, s := range sheets {
		if s.TodoID == td.ID {
			i = j
		}
	}
	if i < 0 {
		t.Fatal("the todo is not exported")
	}
	s := sheets[i]
	if len(s.Questions) != 1 || len(s.Questions[0].Candidates) != 1 || s.Questions[0].Candidates[0].Facts[0] != "author of "+title {
		t.Errorf("questions %+v", s.Questions)
	}
	seen := false
	for _, ch := range s.Changes {
		for _, v := range ch.Values {
			if ch.Entity == "person_alias" && v.Property == "name" {
				seen = len(v.Evidence) == 1 && v.Evidence[0] != ""
			}
		}
	}
	if !seen {
		t.Errorf("the name carries its excerpt: %+v", s.Changes)
	}
	if err := review.Settle(ctx, w.client, "review:grok", review.Verdict{TodoID: td.ID, Verdict: "unsure", Decision: review.Decision{Note: "two people could be meant"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := w.client.Todo.Get(ctx, td.ID)
	if got.State != "open" || len(got.Notes) != 1 || got.Notes[0]["by"] != "review:grok" {
		t.Errorf("unsure leaves it open with a note: %s %v", got.State, got.Notes)
	}
}

func TestWhatATodoAcceptedBeforeMadeIsFoundByTheNext(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	who, authority := author(), "2"+canontest.ISBN()
	title := canontest.Title("の庭")
	w.book("a", title, canontest.ISBN(), who, authority)
	w.book("b", canontest.Title("の森"), canontest.ISBN(), who, authority)
	w.book("c", title+" ５", canontest.ISBN(), author(), "")
	w.run()
	ts := w.open()
	if len(ts) != 3 || ts[0].Kind != registry.TodoFirstWrite || ts[2].Kind != registry.TodoFirstWrite {
		t.Fatalf("%d todos", len(ts))
	}
	first, err := review.Accept(ctx, w.client, ts[0].ID, grok, review.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := review.Accept(ctx, w.client, ts[1].ID, grok, review.Decision{})
	if err != nil {
		t.Fatalf("the same authority is the same person: %v", err)
	}
	if second["alias.a"] != first["alias.a"] || second["person.a"] != first["person.a"] {
		t.Errorf("the pool learns the alias: %v, want %v", second, first)
	}
	var invalid review.Invalid
	if _, err := review.Accept(ctx, w.client, ts[2].ID, grok, review.Decision{}); !errors.As(err, &invalid) {
		t.Fatalf("a title made since: %v", err)
	}
	td, _ := w.client.Todo.Get(ctx, ts[2].ID)
	if td.State != "open" || td.Kind != registry.TodoIdentity || len(td.Questions) != 1 || td.Questions[0]["ref"] != "work" {
		t.Fatalf("the todo asks about it now: %s %s %v", td.State, td.Kind, td.Questions)
	}
	if _, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Answers: map[string]review.Answer{"work": {Use: first["work"]}}}); err != nil {
		t.Fatal(err)
	}
}

func TestALegacyIDTakenOverMeanwhileIsCited(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	who := author()
	base := rand.Int64N(1_000_000_000) + 7_000_000_000
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO %s.catalog_credit_name (id, name) VALUES (%d, '%s')`, identity.LegacySchema, base, who),
		fmt.Sprintf(`INSERT INTO %s.catalog_credit VALUES (%d, 1)`, identity.LegacySchema, base),
	} {
		if _, err := w.client.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	w.book("a", canontest.Title("の庭"), canontest.ISBN(), who, "")
	w.book("b", canontest.Title("の森"), canontest.ISBN(), who, "")
	w.run()
	for _, td := range w.open() {
		ids, err := review.Accept(ctx, w.client, td.ID, grok, review.Decision{Answers: map[string]review.Answer{"alias.a": {Adopt: base}},
			Evidence: []review.Found{{URL: "https://www.kadokawa.co.jp/product/1/", Quote: "著者紹介"}}})
		if err != nil || ids["alias.a"] != base {
			t.Errorf("todo %d: %v %v", td.ID, ids, err)
		}
	}
	if n, _ := w.client.Credit.Query().Where(credit.AliasID(base)).Count(ctx); n != 2 {
		t.Errorf("%d credits on the one alias", n)
	}
}

func TestVolumesOfOneSeriesSettleInAnyOrder(t *testing.T) {
	w := newWorld(t, registry.ModeReview)
	label, err := w.client.Org.Create().SetKind("label").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := canontest.Title("文庫")
	if err := w.client.OrgName.Create().SetOrgID(label.ID).SetText(name).SetLang("ja").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	title, who := canontest.Title("の庭"), author()
	for _, lead := range []string{"a", "b"} {
		isbn := canontest.ISBN()
		w.book(lead, title, isbn, who, "")
		row := w.pool.Rows[len(w.pool.Rows)-1].Row.Changeset
		rec := w.pool.Recs["ndl:"+isbn]
		w.pool.AddRecord(rec.ID, "ndl", map[string]any{"title": title, "isbn": isbn, "label": name + "文庫", "name": name,
			"creators": []map[string]any{{"name": who, "role_text": "著"}}})
		for i, ne := range row.Entities {
			if ne.Entity == "book_release" {
				row.Entities[i].Properties["label_id"] = canontest.Cite("ndl:"+isbn, label.ID, "name")
			}
		}
	}
	w.run()
	ts := w.open()
	if len(ts) != 2 {
		t.Fatalf("%d todos", len(ts))
	}
	first, err := review.Accept(ctx, w.client, ts[0].ID, grok, review.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := review.Accept(ctx, w.client, ts[1].ID, grok, review.Decision{})
	if err != nil || second["work"] != first["work"] || second["alias.a"] != first["alias.a"] || second["person.a"] != first["person.a"] {
		t.Errorf("second %v %v, first %v", second, err, first)
	}
}
