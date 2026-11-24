package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"api/internal/canon/ent"
	"api/internal/canon/registry"
	"api/internal/canon/review"
)

// agentKinds is what a review agent may settle (D29); agent_on_human is a
// person's.
var agentKinds = []string{registry.TodoFirstWrite, registry.TodoIdentity, registry.TodoConflict}

func runReview(ctx context.Context, client *ent.Client, args []string) error {
	if len(args) == 0 {
		return errors.New("review needs export or apply")
	}
	fs := flag.NewFlagSet("review "+args[0], flag.ExitOnError)
	dir := fs.String("dir", "", "directory holding todos.jsonl and verdicts.jsonl")
	limit := fs.Int("limit", 50, "todos to export")
	kinds := fs.String("kinds", strings.Join(agentKinds, ","), "todo kinds to export")
	by := fs.String("by", "", "who reviewed, e.g. review:grok")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("review needs -dir")
	}
	switch args[0] {
	case "export":
		want := strings.Split(*kinds, ",")
		for _, k := range want {
			if !slices.Contains(agentKinds, k) {
				return fmt.Errorf("a review agent does not settle %s todos", k)
			}
		}
		sheets, err := review.Sheets(ctx, client, want, *limit)
		if err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(*dir, "todos.jsonl"))
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetEscapeHTML(false)
		for _, s := range sheets {
			if err := enc.Encode(s); err != nil {
				return err
			}
		}
		fmt.Printf("%d todos exported\n", len(sheets))
		return f.Close()
	case "apply":
		if !strings.HasPrefix(*by, "review:") {
			return errors.New("review apply needs -by review:<agent>")
		}
		return applyVerdicts(ctx, client, filepath.Join(*dir, "verdicts.jsonl"), *by)
	}
	return fmt.Errorf("unknown review command %s", args[0])
}

func applyVerdicts(ctx context.Context, client *ent.Client, path, by string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	counts := map[string]int{}
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var v review.Verdict
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return fmt.Errorf("%s: %w", sc.Text(), err)
		}
		if err := review.Settle(ctx, client, by, v); err != nil {
			counts["failed"]++
			fmt.Printf("todo %d %s: %v\n", v.TodoID, v.Verdict, err)
			continue
		}
		counts[v.Verdict]++
	}
	fmt.Println(counts)
	return sc.Err()
}
