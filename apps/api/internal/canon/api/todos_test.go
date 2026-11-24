package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/todo"
	"api/internal/canon/fanin"
	"api/pkg/utils"
)

type fakeVerifier map[string][]string

func (f fakeVerifier) Parse(_ context.Context, token string) (*utils.TokenClaims, error) {
	roles, ok := f[token]
	if !ok {
		return nil, errors.New("bad token")
	}
	return &utils.TokenClaims{UserUUID: "u-" + token, Roles: roles}, nil
}

func call(t *testing.T, h http.Handler, method, path, token, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

func TestReviewAcceptCitesTheSamePerson(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	name := "同名" + canontest.ISBN() + "郎"
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Ref: "p", Entity: "person", Values: map[string]commit.Value{}},
		{Ref: "a", Entity: "person_alias", Values: map[string]commit.Value{"person_id": v("ref:p"), "name": v(name)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	work, alias := res.IDs["w"], res.IDs["a"]
	pool := canontest.NewPool()
	pool.AddRecord("ndl:c", "ndl", map[string]any{"creators": []map[string]any{{"name": name, "role_text": "著"}}})
	cite := func(val any, path string) contract.Candidate { return canontest.Cite("ndl:c", val, path) }
	aliasRef := canontest.Ref("alias.x")
	aliasRef.Tier, aliasRef.Evidence = "registry", []contract.Evidence{{RecordID: "ndl:c", Path: "creators.0.name"}}
	pool.Changeset("l",
		canontest.New("person.x", "person", map[string]contract.Candidate{}),
		canontest.New("alias.x", "person_alias", map[string]contract.Candidate{"person_id": canontest.Ref("person.x"), "name": cite(name, "creators.0.name")}),
		canontest.New("credit.0", "credit", map[string]contract.Candidate{"work_id": {Value: canontest.Val(work)}, "alias_id": aliasRef, "role": cite("author", "creators.0.role_text")}))
	poolName := fmt.Sprintf("api-%d", rand.Int64())
	if _, err := fanin.New(poolName, pool, client).RunOnce(ctx, 10); err != nil {
		t.Fatal(err)
	}
	td, err := client.Todo.Query().Where(todo.SourceHasPrefix("pool:" + poolName + ":")).Only(ctx)
	if err != nil {
		t.Fatalf("%v, answers %+v", err, pool.Answers)
	}
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}, "user": {"editor"}}})

	if c := call(t, h, http.MethodGet, "/v3/catalog/todos", "", "", nil); c != http.StatusUnauthorized {
		t.Errorf("no token: %d", c)
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos", "user", "", nil); c != http.StatusForbidden {
		t.Errorf("editor: %d", c)
	}
	var list TodoList
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos?kind=identity&limit=100&cursor="+encodeCursor(int64(td.ID-1)), "mod", "", &list); c != http.StatusOK || len(list.Items) == 0 {
		t.Fatalf("list: %d %+v", c, list)
	}
	if it := list.Items[0]; it.ID != strconv.Itoa(td.ID) || it.Program != "ln-pilot" || len(it.Questions) != 1 || it.Questions[0]["ref"] != "alias.x" {
		t.Errorf("the todo carries its question: %+v", it)
	}
	path := "/v3/catalog/todos/" + strconv.Itoa(td.ID) + "/accept"
	if c := call(t, h, http.MethodPost, path, "mod", `{}`, nil); c != http.StatusUnprocessableEntity {
		t.Errorf("an unanswered question: %d", c)
	}
	body := fmt.Sprintf(`{"answers":{"alias.x":{"use":"%d"}}}`, alias)
	var out struct {
		State string            `json:"state"`
		IDs   map[string]string `json:"ids"`
	}
	if c := call(t, h, http.MethodPost, path, "mod", body, &out); c != http.StatusOK || out.State != "done" {
		t.Fatalf("accept: %d %+v", c, out)
	}
	n, _ := client.Credit.Query().Where(credit.WorkID(work), credit.AliasID(alias)).Count(ctx)
	if n != 1 || out.IDs["alias.x"] != strconv.FormatInt(alias, 10) || out.IDs["person.x"] != strconv.FormatInt(res.IDs["p"], 10) {
		t.Errorf("credit on the cited alias: %d, ids %v", n, out.IDs)
	}
	if c := call(t, h, http.MethodPost, path, "mod", body, nil); c != http.StatusConflict {
		t.Errorf("accepting twice: %d", c)
	}
	got, _ := client.Todo.Get(ctx, td.ID)
	if got.ResolvedBy != "user:u-mod" || got.Resolution["result"] != "written" {
		t.Errorf("todo after accept: %+v", got)
	}
}
