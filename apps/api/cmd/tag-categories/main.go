package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/jobs/tagcategory"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; hosts src_vndb)")
	curated := flag.String("curated", "", "JSONL of reviewed {tag_id, tag, category}; absent = derive categories for uncategorized tags from VNDB")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" {
		fail(fmt.Errorf("catalog DSN is required (--dsn)"))
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		fail(fmt.Errorf("connect catalog db: %w", err))
	}
	ctx := context.Background()

	if *curated == "" {
		st, err := tagcategory.Derive(ctx, db, *apply)
		if err != nil {
			fail(err)
		}
		fmt.Printf("\n=== tag-categories derive %s ===\n", mode(*apply))
		fmt.Printf("candidates=%d derived=%d by_category=%v\n", st.Candidates, st.Derived, st.ByCategory)
		return
	}

	recs, err := readRecords(*curated)
	if err != nil {
		fail(err)
	}
	st, err := tagcategory.ApplyCurated(ctx, db, recs, *apply)
	if st != nil {
		fmt.Printf("\n=== tag-categories curated %s ===\n", mode(*apply))
		fmt.Printf("records=%d changed=%d stamped=%d unchanged=%d\n", st.Records, st.Changed, st.Stamped, st.Unchanged)
	}
	if err != nil {
		fail(err)
	}
}

func readRecords(path string) ([]tagcategory.CuratedRec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []tagcategory.CuratedRec
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var r tagcategory.CuratedRec
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func fail(err error) {
	slog.Error("tag-categories failed", "error", err)
	os.Exit(1)
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}
