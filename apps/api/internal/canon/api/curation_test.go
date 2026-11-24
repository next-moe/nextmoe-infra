package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/perm"
	"api/internal/canon/registry"
	"api/internal/platform/authz"
)

func grant(t *testing.T, rows ...perm.Override) {
	t.Helper()
	perm.Holder.Swap(authz.NewResolver(perm.Merged(perm.Bundles, rows)))
	t.Cleanup(func() { perm.Holder.Swap(authz.NewResolver(perm.Bundles)) })
}

func TestTodoKeysSplitTheQueue(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	tag := strconv.FormatUint(rand.Uint64(), 36)
	identity, err := client.Todo.Create().SetKind(registry.TodoIdentity).SetSubjectKey("identity:person_alias:" + tag).
		SetSuggestion(map[string]any{}).SetSource("pool:perm:1").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Todo.Create().SetKind(registry.TodoAgentOnHuman).SetSubjectKey("agent_on_human:work:1:" + tag).
		SetSuggestion(map[string]any{}).SetSource("pool:perm:2").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := New(client, Options{Verifier: fakeVerifier{"ed": {"editor"}, "mod": {"moderator"}, "site": {"user"}}})
	grant(t, perm.Override{Role: "editor", Permission: string(perm.TodoValue), Effect: "grant"})

	var list TodoList
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos?limit=100", "ed", "", &list); c != http.StatusOK {
		t.Fatalf("editor with the value key lists: %d", c)
	}
	for _, it := range list.Items {
		if it.Kind == registry.TodoIdentity {
			t.Errorf("editor saw a %s todo", it.Kind)
		}
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos?kind=identity", "ed", "", nil); c != http.StatusForbidden {
		t.Errorf("editor filtering identity: %d", c)
	}
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos?kind=nope", "mod", "", nil); c != http.StatusUnprocessableEntity {
		t.Errorf("unknown kind: %d", c)
	}
	reject := func(n int, token string) int {
		return call(t, h, http.MethodPost, "/v3/catalog/todos/"+strconv.Itoa(n)+"/reject", token, `{"note":"no"}`, nil)
	}
	if c := reject(identity.ID, "ed"); c != http.StatusForbidden {
		t.Errorf("editor rejecting an identity todo: %d", c)
	}
	if c := reject(value.ID, "site"); c != http.StatusForbidden {
		t.Errorf("a plain user rejecting: %d", c)
	}
	if c := reject(value.ID, "ed"); c != http.StatusOK {
		t.Errorf("editor rejecting a value todo: %d", c)
	}
	if c := reject(identity.ID, "mod"); c != http.StatusOK {
		t.Errorf("moderator rejecting an identity todo: %d", c)
	}

	grant(t, perm.Override{Role: "moderator", Permission: string(perm.TodoIdentity), Effect: "deny"},
		perm.Override{Role: "moderator", Permission: string(perm.TodoValue), Effect: "deny"})
	if c := call(t, h, http.MethodGet, "/v3/catalog/todos", "mod", "", nil); c != http.StatusForbidden {
		t.Errorf("moderator denied both keys lists: %d", c)
	}
}

func TestMergeAndTakedownThroughV3(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "a", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Ref: "b", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := res.IDs["a"], res.IDs["b"]
	h, _ := New(client, Options{Verifier: fakeVerifier{"mod": {"moderator"}, "ed": {"editor"}}})

	body := fmt.Sprintf(`{"entity":"work","loser":"%d","survivor":"%d","reason":"same book"}`, a, b)
	if c := call(t, h, http.MethodPost, "/v3/catalog/merges", "ed", body, nil); c != http.StatusForbidden {
		t.Errorf("editor merging: %d", c)
	}
	var made struct {
		ID string `json:"id"`
	}
	if c := call(t, h, http.MethodPost, "/v3/catalog/merges", "mod", body, &made); c != http.StatusCreated || made.ID == "" {
		t.Fatalf("merge: %d %+v", c, made)
	}
	var p Problem
	if c := call(t, h, http.MethodGet, "/v3/catalog/works/"+id(a), "", "", &p); c != http.StatusNotFound || p.Code != "ENTITY_MERGED" || p.CurrentID != id(b) {
		t.Errorf("merged loser: %d %+v", c, p)
	}
	missing := fmt.Sprintf(`{"entity":"work","loser":"%d","survivor":"%d","reason":"x"}`, a, b)
	if c := call(t, h, http.MethodPost, "/v3/catalog/merges", "mod", missing, nil); c != http.StatusNotFound {
		t.Errorf("merging a loser that is gone: %d", c)
	}
	undo := "/v3/catalog/merges/" + made.ID + "/undo"
	if c := call(t, h, http.MethodPost, undo, "mod", "", nil); c != http.StatusOK {
		t.Fatalf("undo: %d", c)
	}
	if c := call(t, h, http.MethodPost, undo, "mod", "", nil); c != http.StatusConflict {
		t.Errorf("undo twice: %d", c)
	}
	if c := get(t, h, "/v3/catalog/works/"+id(a), nil); c != http.StatusOK {
		t.Errorf("loser after undo: %d", c)
	}

	td := fmt.Sprintf(`{"notice_id":"n-%d","entity":"work","entity_id":"%d","reason":"rights holder asked"}`, a, a)
	if c := call(t, h, http.MethodPost, "/v3/catalog/takedowns", "ed", td, nil); c != http.StatusForbidden {
		t.Errorf("editor taking down: %d", c)
	}
	var tdOut map[string]any
	if c := call(t, h, http.MethodPost, "/v3/catalog/takedowns", "mod", td, &tdOut); c != http.StatusCreated {
		t.Fatalf("takedown: %d %v", c, tdOut)
	}
	made.ID, _ = tdOut["id"].(string)
	if c := get(t, h, "/v3/catalog/works/"+id(a), nil); c != http.StatusGone {
		t.Errorf("taken-down work: %d", c)
	}
	gone := fmt.Sprintf(`{"notice_id":"n","entity":"work","entity_id":"%d","reason":"x"}`, b+1_000_000_000)
	if c := call(t, h, http.MethodPost, "/v3/catalog/takedowns", "mod", gone, nil); c != http.StatusNotFound {
		t.Errorf("taking down a missing work: %d", c)
	}
	restore := "/v3/catalog/takedowns/" + made.ID + "/restore"
	if c := call(t, h, http.MethodPost, restore, "mod", `{"note":"counter-notice"}`, nil); c != http.StatusOK {
		t.Fatalf("restore: %d", c)
	}
	if c := call(t, h, http.MethodPost, restore, "mod", `{"note":"again"}`, nil); c != http.StatusConflict {
		t.Errorf("restore twice: %d", c)
	}
	if c := get(t, h, "/v3/catalog/works/"+id(a), nil); c != http.StatusOK {
		t.Errorf("restored work: %d", c)
	}
}
