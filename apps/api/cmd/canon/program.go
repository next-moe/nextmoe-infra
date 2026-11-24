package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/registry"
	"api/internal/canon/seed"
)

func runProgram(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("program", flag.ExitOnError)
	key := fs.String("key", "", "registered program; empty lists them all")
	mode := fs.String("mode", "", strings.Join(registry.Modes, " | "))
	by := fs.String("by", "", "who sets it")
	note := fs.String("note", "", "why")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		for _, p := range registry.Programs {
			m, set := registry.ModeReview, "default"
			if s, err := client.ProgramState.Get(ctx, p.Key); err == nil {
				m, set = s.Mode, s.SetBy+" "+s.UpdatedAt.Format(time.RFC3339)+" "+s.Note
			} else if !ent.IsNotFound(err) {
				return err
			}
			fmt.Printf("%s\t%s\t%s\n", p.Key, m, set)
		}
		return nil
	}
	if _, ok := registry.ProgramByKey(*key); !ok {
		return fmt.Errorf("program %q is not registered", *key)
	}
	if !slices.Contains(registry.Modes, *mode) || *by == "" {
		return errors.New("program -key needs -mode and -by")
	}
	return client.ProgramState.Create().SetID(*key).SetMode(*mode).SetSetBy(*by).SetNote(*note).
		OnConflictColumns("id").UpdateNewValues().Exec(ctx)
}

func runUndo(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("undo", flag.ExitOnError)
	batch := fs.String("batch", "", "commit to take back (revision.batch)")
	by := fs.String("by", "", "person who decides it, e.g. user:<id>")
	note := fs.String("note", "", "why")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *batch == "" || *by == "" || *note == "" {
		return errors.New("undo needs -batch, -by and -note")
	}
	return commit.New(client).Undo(ctx, *batch, commit.Actor{Kind: commit.ActorHuman, Key: *by}, *note)
}

func runRollback(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	key := fs.String("program", "", "program whose commits to take back")
	since := fs.String("since", "", "RFC 3339; commits from then on")
	by := fs.String("by", "", "who runs it")
	note := fs.String("note", "", "why")
	if err := fs.Parse(args); err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339, *since)
	if *key == "" || *by == "" || *note == "" || err != nil {
		return errors.New("rollback needs -program, -since (RFC 3339), -by and -note")
	}
	undone, conflicts, err := commit.New(client).Rollback(ctx, "program:"+*key, at, *by, *note)
	for b, c := range conflicts {
		fmt.Printf("kept %s: %v\n", b, c)
	}
	fmt.Printf("%d commits undone, %d kept\n", len(undone), len(conflicts))
	return err
}

func runOrgSeed(ctx context.Context, client *ent.Client) error {
	ids, err := seed.Orgs(ctx, client)
	fmt.Printf("%d organizations: %v\n", len(ids), ids)
	return err
}
