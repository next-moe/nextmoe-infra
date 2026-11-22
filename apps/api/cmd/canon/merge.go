package main

import (
	"context"
	"flag"
	"fmt"

	"api/internal/canon/ent"
	"api/internal/canon/merge"
)

func runMerge(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	entity := fs.String("entity", "", "work, work_part, release, person or org")
	loser := fs.Int64("loser", 0, "id that will redirect")
	survivor := fs.Int64("survivor", 0, "id that stays")
	by := fs.String("by", "", "who merges, e.g. user:<id>")
	reason := fs.String("reason", "", "why they are one entity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *by == "" || *reason == "" {
		return fmt.Errorf("merge needs -by and -reason")
	}
	id, err := merge.Merge(ctx, client, *entity, *loser, *survivor, *by, *reason)
	if err != nil {
		return err
	}
	fmt.Printf("merge %d: %s %d → %d\n", id, *entity, *loser, *survivor)
	return nil
}

func runUnmerge(ctx context.Context, client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("unmerge", flag.ExitOnError)
	id := fs.Int("merge", 0, "merge id")
	by := fs.String("by", "", "who undoes it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return merge.Unmerge(ctx, client, *id, *by)
}
