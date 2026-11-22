package main

import (
	"context"
	"flag"
	"fmt"

	"api/internal/canon/ent"
	"api/internal/canon/takedown"
)

func runTakedown(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("takedown", flag.ExitOnError)
	var n takedown.Notice
	fs.StringVar(&n.NoticeID, "notice", "", "trust inbox notice id")
	fs.StringVar(&n.Scope, "scope", takedown.ScopeEntity, "asset | media_set | synopsis | entity")
	fs.StringVar(&n.Entity, "entity", "", "work, release, person, org …")
	fs.Int64Var(&n.EntityID, "id", 0, "entity id")
	fs.StringVar(&n.Asset, "asset", "", "asset hash for scope=asset")
	fs.StringVar(&n.Reason, "reason", "", "why, as the notice states it")
	fs.StringVar(&n.By, "by", "", "who takes it down, e.g. user:<id>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	td, err := takedown.Take(ctx, client, n)
	if err != nil {
		return err
	}
	fmt.Printf("takedown %d: %s %s %d held and hidden\n", td.ID, td.Scope, td.Entity, td.EntityID)
	return nil
}

func runRestore(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	id := fs.Int("takedown", 0, "takedown id")
	by := fs.String("by", "", "who restores it")
	note := fs.String("note", "", "why, e.g. the counter-notice")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == 0 || *by == "" || *note == "" {
		return fmt.Errorf("restore needs -takedown, -by and -note")
	}
	return takedown.Restore(ctx, client, *id, *by, *note)
}
