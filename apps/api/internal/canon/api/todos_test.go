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
	"api/internal/canon/ent/credit"
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
	name := fmt.Sprintf("同名%d", rand.Int64())
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Ref: "p", Entity: "person", Values: map[string]commit.Value{}},
		{Ref: "a", Entity: "person_alias", Values: map[string]commit.Value{"person_id": v("ref:p"), "name": v(name)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	work, alias := res.IDs["w"], res.IDs["a"]
	row := fmt.Sprintf(`{"contract_version":"0.1.0","kind":"changeset","program":"ln-pilot","changeset":{"lead_id":"l","entities":[
		{"ref":"person.x","entity":"person","properties":{}},
		{"ref":"alias.x","entity":"person_alias","properties":{"person_id":{"value":"ref:person.x"},"name":{"value":%q,"tier":"registry"}}},
		{"ref":"credit.0","entity":"credit","properties":{"work_id":{"value":%d},"alias_id":{"value":"ref:alias.x"},"role":{"value":"author","tier":"registry"}}}]}}`, name, work)
	var suggestion map[string]any
	if err := json.Unmarshal([]byte(row), &suggestion); err != nil {
		t.Fatal(err)
	}
	td, err := client.Todo.Create().SetKind("identity").SetSubjectKey("identity:person_alias:" + name).SetSuggestion(suggestion).SetSource("pool:test:7").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}, "user": {"editor"}}})

	if c := call(t, h, http.MethodGet, "/v3/catalog/todos", "", "", nil); c != http.StatusUnauthorized {
		t.Errorf("no token: %d", c)
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos", "user", "", nil); c != http.StatusForbidden {
		t.Errorf("editor: %d", c)
	}
	var list TodoList
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos?kind=identity&limit=100", "mod", "", &list); c != http.StatusOK || len(list.Items) == 0 {
		t.Fatalf("list: %d %+v", c, list)
	}
	path := "/v3/catalog/todos/" + strconv.Itoa(td.ID) + "/accept"
	body := fmt.Sprintf(`{"use_ids":{"person.x":"%d","alias.x":"%d"}}`, res.IDs["p"], alias)
	var out struct {
		State string            `json:"state"`
		IDs   map[string]string `json:"ids"`
	}
	if c := call(t, h, http.MethodPost, path, "mod", body, &out); c != http.StatusOK || out.State != "done" {
		t.Fatalf("accept: %d %+v", c, out)
	}
	n, _ := client.Credit.Query().Where(credit.WorkID(work), credit.AliasID(alias)).Count(ctx)
	if n != 1 || out.IDs["alias.x"] != strconv.FormatInt(alias, 10) {
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
