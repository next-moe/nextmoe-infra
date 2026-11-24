package commit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"api/internal/canon/canontest"
	"api/internal/canon/ent/identitykey"
	"api/internal/canon/ent/programstate"
	"api/internal/canon/ent/revision"
	"api/internal/canon/ent/work"
	"api/internal/canon/registry"
)

var person = Actor{Kind: ActorHuman, Key: "user:1"}

func TestUndoOfAnEditRestoresAndLocks(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	made, err := s.Apply(ctx, book(isbn()))
	if err != nil {
		t.Fatal(err)
	}
	w := made.IDs["w"]
	set := func(a Actor, status string) (Result, error) {
		return s.Apply(ctx, Changeset{Actor: a, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"status": official(status)}}}})
	}
	first, err := set(agent, "complete")
	if err != nil {
		t.Fatal(err)
	}
	second, err := set(agent, "hiatus")
	if err != nil {
		t.Fatal(err)
	}
	var c Conflicts
	if err := s.Undo(ctx, first.Batch, person, "wrong"); !errors.As(err, &c) || !strings.Contains(err.Error(), "written again") {
		t.Errorf("undoing under a later edit: %v", err)
	}
	if err := s.Undo(ctx, second.Batch, person, "wrong"); err != nil {
		t.Fatal(err)
	}
	got, _ := client.Work.Get(ctx, w)
	if got.Status != "complete" || got.FieldState["status"].(map[string]any)["by"] != "human" {
		t.Errorf("after the undo: %s %v", got.Status, got.FieldState["status"])
	}
	if _, err := set(agent, "hiatus"); refusal(t, err).Code != "HUMAN_LOCKED" {
		t.Errorf("an agent may not write back what a person undid: %v", err)
	}
	if err := s.Undo(ctx, second.Batch, person, "again"); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("undoing twice: %v", err)
	}
	if err := s.Undo(ctx, first.Batch, Actor{Kind: ActorSystem, Key: "op"}, "rollback"); err != nil {
		t.Fatalf("with the later edit undone, the earlier one is free: %v", err)
	}
	if got, _ = client.Work.Get(ctx, w); got.Status != "ongoing" {
		t.Errorf("after both undos: %s", got.Status)
	}
}

func TestUndoOfACreateRemovesTheRowsAndTheirKeys(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	cs := book(isbn())
	cs.Key = "test:" + isbn()
	made, err := s.Apply(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}
	w := made.IDs["w"]
	if err := s.Undo(ctx, made.Batch, person, "not a light novel"); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.Work.Query().Where(work.ID(w)).Count(ctx); n != 0 {
		t.Error("the work is still there")
	}
	if n, _ := client.IdentityKey.Query().Where(identitykey.EntityID(w), identitykey.Entity("work")).Count(ctx); n != 0 {
		t.Errorf("%d identity keys of a removed work", n)
	}
	removed, err := client.Revision.Query().Where(revision.Entity("work"), revision.EntityID(w), revision.Action("remove")).Only(ctx)
	if err != nil || removed.Changes["row"] == nil {
		t.Errorf("the removal keeps the row: %+v %v", removed, err)
	}
	if _, ok, _ := s.Prior(ctx, cs.Key); ok {
		t.Error("the commit key would answer a replay with removed ids")
	}
	if _, err := s.Apply(ctx, cs); err != nil {
		t.Errorf("the same changeset may come back: %v", err)
	}
}

func TestUndoOfACreateRefusesWhatOthersBuiltOn(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	made, err := s.Apply(ctx, book(isbn()))
	if err != nil {
		t.Fatal(err)
	}
	w := made.IDs["w"]
	if _, err := s.Apply(ctx, Changeset{Actor: agent, Changes: []Change{{Entity: "work_part", Values: map[string]Value{"work_id": official(w), "kind": official("volume"), "ordinal": official(29)}}}}); err != nil {
		t.Fatal(err)
	}
	var c Conflicts
	if err := s.Undo(ctx, made.Batch, person, "x"); !errors.As(err, &c) {
		t.Errorf("a later volume points at the work: %v", err)
	}
	if n, _ := client.Work.Query().Where(work.ID(w)).Count(ctx); n != 1 {
		t.Error("a refused undo removed something")
	}
}

func TestRollbackTakesBackAProgramsRunAndLocksNothing(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	program := Actor{Kind: ActorAgent, Key: "program:t-" + isbn()}
	since := time.Now()
	kept := book(isbn())
	kept.Actor = program
	base, err := s.Apply(ctx, kept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, Changeset{Actor: person, Changes: []Change{{Entity: "work", ID: base.IDs["w"], Values: map[string]Value{"content_rating": {Raw: raw("r15")}}}}}); err != nil {
		t.Fatal(err)
	}
	var loose []int64
	for range 3 {
		cs := title(unique("の本"))
		cs.Actor = program
		res, err := s.Apply(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		loose = append(loose, res.IDs["w"])
	}
	undone, conflicts, err := s.Rollback(ctx, program.Key, since, "ops", "bad run")
	if err != nil || len(undone) != 3 || len(conflicts) != 1 {
		t.Fatalf("rollback: %v %v %v", undone, conflicts, err)
	}
	if n, _ := client.Work.Query().Where(work.IDIn(loose...)).Count(ctx); n != 0 {
		t.Errorf("%d works of the bad run are left", n)
	}
	if _, err := client.Work.Get(ctx, base.IDs["w"]); err != nil {
		t.Errorf("the work a person edited stays: %v", err)
	}
}

func TestAProgramPeopleKeepUndoingStops(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	key := "t-" + isbn()
	var batches []string
	for i := range 25 {
		cs := title(fmt.Sprintf("%s第%d話の題", unique(""), i))
		cs.Actor = Actor{Kind: ActorAgent, Key: "program:" + key}
		res, err := s.Apply(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, res.Batch)
	}
	mode := func() string {
		p, err := client.ProgramState.Query().Where(programstate.ID(key)).Only(ctx)
		if err != nil {
			return ""
		}
		return p.Mode
	}
	if err := s.Undo(ctx, batches[0], person, "wrong"); err != nil {
		t.Fatal(err)
	}
	if m := mode(); m != "" {
		t.Errorf("one undo stopped it: %s", m)
	}
	if err := s.Undo(ctx, batches[1], person, "wrong"); err != nil {
		t.Fatal(err)
	}
	if m := mode(); m != registry.ModeStopped {
		t.Errorf("two of 25 is over 2%%: %q", m)
	}
}
