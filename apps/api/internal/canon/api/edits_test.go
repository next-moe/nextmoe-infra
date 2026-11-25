package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
)

func TestEditingThroughTheFace(t *testing.T) {
	client, _ := canontest.Client(t)
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}, "user": {"editor"}}})
	title, code := canontest.Title("の庭"), canontest.ISBN()

	var schemas Schemas
	if c := call(t, h, http.MethodGet, "/v3/catalog/schemas", "", "", &schemas); c != http.StatusOK || len(schemas.Entities) < 10 {
		t.Fatalf("schemas %d %d", c, len(schemas.Entities))
	}
	body := fmt.Sprintf(`{"note":"new series","changes":[
		{"ref":"w","entity":"work","values":{"medium":"novel"}},
		{"entity":"work_title","values":{"work_id":"ref:w","text":%q,"lang":"ja","kind":"official"}},
		{"entity":"work_title","values":{"work_id":"ref:w","text":%q,"lang":"ja","kind":"alternative"}},
		{"ref":"r","entity":"release","values":{"lang":"ja","format":"paperback"}},
		{"entity":"release_content","values":{"release_id":"ref:r","work_id":"ref:w"}},
		{"entity":"book_release","values":{"release_id":"ref:r","isbn13":%q}}]}`, title, canontest.Title("の別名"), code)
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "user", body, nil); c != http.StatusForbidden {
		t.Errorf("editor without the key: %d", c)
	}
	var made struct {
		Batch string            `json:"batch"`
		IDs   map[string]string `json:"ids"`
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", body, &made); c != http.StatusCreated || made.IDs["w"] == "" {
		t.Fatalf("commit %d %+v", c, made)
	}
	var p Problem
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", body, &p); c != http.StatusConflict || p.Code != "IDENTITY_UNSURE" || len(p.Candidates) != 1 || p.Candidates[0].ID != made.IDs["w"] {
		t.Errorf("a second work of that title: %d %+v", c, p)
	}

	var found struct {
		Items []LookupItem `json:"items"`
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/lookup?entity=work&q="+url.QueryEscape(title[:len(title)-3]), "mod", "", &found); c != http.StatusOK || len(found.Items) != 1 || found.Items[0].ID != made.IDs["w"] {
		t.Errorf("lookup %d %+v", c, found)
	}
	var rec Record
	if c := call(t, h, http.MethodGet, "/v3/catalog/records/work/"+made.IDs["w"], "mod", "", &rec); c != http.StatusOK {
		t.Fatalf("record %d", c)
	}
	if rec.Values["medium"] != "novel" || rec.State["medium"].By != "human" || len(rec.Children["work_title"]) != 2 || rec.Children["work_title"][0].Values["work_id"] != made.IDs["w"] {
		t.Errorf("record %+v", rec)
	}
	edit := func(version int, status string) int {
		return call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", fmt.Sprintf(
			`{"note":"n","changes":[{"entity":"work","id":%q,"expect_version":%d,"values":{"status":%q}}]}`, made.IDs["w"], version, status), &p)
	}
	if c := edit(rec.Version, "ongoing"); c != http.StatusCreated {
		t.Errorf("edit %d", c)
	}
	if c := edit(rec.Version, "complete"); c != http.StatusConflict || p.Code != "VERSION_CONFLICT" {
		t.Errorf("stale edit %d %+v", c, p)
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", `{"note":"n","changes":[{"entity":"release","values":{"lang":"ja"}}]}`, &p); c != http.StatusUnprocessableEntity {
		t.Errorf("a release without its format: %d %+v", c, p)
	}

	alias := rec.Children["work_title"][1]
	var removed struct {
		Batch string `json:"batch"`
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", fmt.Sprintf(`{"note":"not its name","changes":[{"entity":"work_title","id":%q,"remove":true}]}`, alias.ID), &removed); c != http.StatusCreated {
		t.Fatalf("remove a title: %d", c)
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", fmt.Sprintf(`{"note":"n","changes":[{"entity":"work","id":%q,"remove":true}]}`, made.IDs["w"]), &p); c != http.StatusUnprocessableEntity {
		t.Errorf("removing a work: %d", c)
	}
	call(t, h, http.MethodGet, "/v3/catalog/records/work/"+made.IDs["w"], "mod", "", &rec)
	if len(rec.Children["work_title"]) != 1 {
		t.Errorf("titles after the removal: %d", len(rec.Children["work_title"]))
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits/"+removed.Batch+"/undo", "mod", `{"note":"it was"}`, nil); c != http.StatusOK {
		t.Errorf("undo the removal: %d", c)
	}
	call(t, h, http.MethodGet, "/v3/catalog/records/work/"+made.IDs["w"], "mod", "", &rec)
	if len(rec.Children["work_title"]) != 2 {
		t.Errorf("titles after the undo: %d", len(rec.Children["work_title"]))
	}

	var revs struct {
		Items []RevisionItem `json:"items"`
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/revisions?entity=work&id="+made.IDs["w"]+"&root=true", "mod", "", &revs); c != http.StatusOK || len(revs.Items) < 6 {
		t.Errorf("revisions %d %d", c, len(revs.Items))
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/commits/nope/undo", "mod", `{"note":"x"}`, nil); c != http.StatusNotFound {
		t.Errorf("undo of nothing: %d", c)
	}
}

func TestPatrolAndProgramModes(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}, "adm": {"admin"}}})
	t.Cleanup(func() { _ = client.ProgramState.DeleteOneID("ln-pilot").Exec(ctx) })
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:ln-pilot", Reviewer: "review:grok"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": v("ref:w"), "text": v(canontest.Title("の庭")), "lang": v("ja"), "kind": v("official")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	seen := func() *PatrolItem {
		cursor := ""
		for {
			var page struct {
				Items      []PatrolItem `json:"items"`
				NextCursor string       `json:"next_cursor"`
			}
			if c := call(t, h, http.MethodGet, "/v3/catalog/patrol?program=ln-pilot&limit=100&cursor="+cursor, "mod", "", &page); c != http.StatusOK {
				t.Fatalf("patrol %d", c)
			}
			for _, it := range page.Items {
				if it.Batch == res.Batch {
					return &it
				}
			}
			if page.NextCursor == "" {
				return nil
			}
			cursor = page.NextCursor
		}
	}
	it := seen()
	if it == nil || len(it.Revisions) != 2 || it.ReviewedBy != "review:grok" {
		t.Fatalf("patrol item %+v", it)
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/patrol/"+res.Batch, "mod", "", nil); c != http.StatusOK {
		t.Errorf("mark %d", c)
	}
	if seen() != nil {
		t.Error("a patrolled commit is still listed")
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/patrol/nope", "mod", "", nil); c != http.StatusNotFound {
		t.Errorf("patrol of nothing: %d", c)
	}

	mode := `{"mode":"auto","note":"gate passed"}`
	if c := call(t, h, http.MethodPut, "/v3/catalog/programs/ln-pilot/mode", "mod", mode, nil); c != http.StatusForbidden {
		t.Errorf("a moderator set the mode: %d", c)
	}
	if c := call(t, h, http.MethodPut, "/v3/catalog/programs/ln-pilot/mode", "adm", mode, nil); c != http.StatusOK {
		t.Errorf("admin: %d", c)
	}
	var programs struct {
		Items []ProgramItem `json:"items"`
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/programs", "mod", "", &programs); c != http.StatusOK || len(programs.Items) == 0 ||
		programs.Items[0].Mode != registry.ModeAuto || programs.Items[0].SetBy != "user:u-adm" || programs.Items[0].Commits == 0 {
		t.Errorf("programs %d %+v", c, programs.Items)
	}
}

func TestAPersonTakesOverALegacyNameWithItsPerson(t *testing.T) {
	client, db := canontest.Client(t)
	canontest.Legacy(t, db)
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}}})
	base := rand.Int64N(1_000_000_000) + 6_000_000_000
	name := "名義" + canontest.ISBN() + "郎"
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO %s.catalog_person VALUES (%d, NULL)`, identity.LegacySchema, base),
		fmt.Sprintf(`INSERT INTO %s.catalog_credit_name (id, person_id, name) VALUES (%d, %d, '%s')`, identity.LegacySchema, base+1, base, name),
		fmt.Sprintf(`INSERT INTO %s.catalog_credit VALUES (%d, 1)`, identity.LegacySchema, base+1),
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	create := func(extra string) (int, Problem, map[string]string) {
		var p Problem
		var out struct {
			IDs map[string]string `json:"ids"`
		}
		body := fmt.Sprintf(`{"note":"n","changes":[{"ref":"p","entity":"person","values":{}},{"ref":"a","entity":"person_alias","values":{"person_id":"ref:p","name":%q}%s}]}`, name, extra)
		c := call(t, h, http.MethodPost, "/v3/catalog/commits", "mod", body, &p)
		if c == http.StatusCreated {
			a, _ := client.PersonAlias.Get(context.Background(), base+1)
			if a != nil {
				out.IDs = map[string]string{"a": id(a.ID), "p": id(a.PersonID)}
			}
		}
		return c, p, out.IDs
	}
	if c, p, _ := create(""); c != http.StatusConflict || len(p.Candidates) != 1 || p.Candidates[0].Token != fmt.Sprintf("legacy:%d", base+1) {
		t.Fatalf("the legacy name is a candidate: %d %+v", c, p)
	}
	c, p, ids := create(fmt.Sprintf(`,"adopt_id":"%d"`, base+1))
	if c != http.StatusCreated || ids["a"] != id(base+1) || ids["p"] != id(base) {
		t.Errorf("take over: %d %+v %v", c, p, ids)
	}
}
