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
	"api/internal/jobs/tagparents"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; hosts src_vndb)")
	curated := flag.String("curated", "", "JSONL of reviewed edges to add or remove; absent = sync the VNDB-derived edges")
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
		st, err := tagparents.Sync(ctx, db, *apply)
		if err != nil {
			fail(err)
		}
		fmt.Printf("\n=== tag-parents sync %s ===\n", mode(*apply))
		fmt.Printf("vndb_tags=%d mapped=%d edges=%d added=%d deleted=%d cycles=%d\n",
			st.VNDBTags, st.Mapped, st.Edges, st.Added, st.Deleted, st.Cycles)
		return
	}

	recs, err := readRecords(*curated)
	if err != nil {
		fail(err)
	}
	st, err := tagparents.ApplyCurated(ctx, db, recs, *apply)
	if err != nil {
		fail(err)
	}
	fmt.Printf("\n=== tag-parents curated %s ===\n", mode(*apply))
	fmt.Printf("records=%d added=%d endorsed=%d removed=%d unchanged=%d errors=%d\n",
		st.Records, st.Added, st.Endorsed, st.Removed, st.Unchanged, st.Errors)
	if st.Errors > 0 {
		os.Exit(1)
	}
}

func readRecords(path string) ([]tagparents.CuratedRec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []tagparents.CuratedRec
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var r tagparents.CuratedRec
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func fail(err error) {
	slog.Error("tag-parents failed", "error", err)
	os.Exit(1)
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}
