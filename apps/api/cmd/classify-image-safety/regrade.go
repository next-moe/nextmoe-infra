package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const providerHuman = "human"

type regradeOptions struct {
	DSN   string
	In    string
	Apply bool
}

type regradeRow struct {
	Hash  string `json:"hash"`
	Level int    `json:"level"`
}

// runRegrade writes a person's verdict over the machine's. The grader misses
// what a mosaic covers: on 2026-10-02 four getchu screenshots showing sex acts
// or bare buttocks behind a mosaic all carried level 0, every rung answered no.
// A human grade is one the nightly grader never revisits (it only fills a
// missing grade) and sync-image-grades carries it into the catalog like any
// other; the machine's answer is kept beside it under grade_machine.
func runRegrade(ctx context.Context, o regradeOptions, w io.Writer) error {
	if o.DSN == "" || o.In == "" {
		return fmt.Errorf("--dsn and --in are required")
	}
	rows, err := readRegradeInput(o.In)
	if err != nil {
		return err
	}
	db, err := database.OpenJob(o.DSN)
	if err != nil {
		return err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	st, err := regrade(ctx, db, rows, o.Apply)
	fmt.Fprintf(w, "regrade apply=%v images=%d changed=%d unchanged=%d missing=%d\n",
		o.Apply, len(rows), st.changed, st.unchanged, st.missing)
	for _, c := range st.moves() {
		fmt.Fprintf(w, "  level %s -> %d   %d\n", c.from, c.to, c.n)
	}
	if err != nil {
		return err
	}
	if st.missing > 0 {
		return fmt.Errorf("%d hashes are not in the image service", st.missing)
	}
	return nil
}

type regradeStats struct {
	changed, unchanged, missing int
	byMove                      map[regradeMove]int
}

type regradeMove struct {
	from string
	to   int
	n    int
}

func (s *regradeStats) moves() []regradeMove {
	out := make([]regradeMove, 0, len(s.byMove))
	for m, n := range s.byMove {
		m.n = n
		out = append(out, m)
	}
	return out
}

func regrade(ctx context.Context, db *gorm.DB, rows []regradeRow, apply bool) (*regradeStats, error) {
	st := &regradeStats{byMove: map[regradeMove]int{}}
	for _, r := range rows {
		var cur struct {
			Found    bool    `gorm:"column:found"`
			Provider *string `gorm:"column:provider"`
			Level    *int    `gorm:"column:level"`
		}
		if err := db.WithContext(ctx).Raw(`
			SELECT true AS found, review_labels->'grade'->>'provider' AS provider,
			       (review_labels->'grade'->>'level')::int AS level
			  FROM images WHERE hash = ?`, r.Hash).Scan(&cur).Error; err != nil {
			return st, fmt.Errorf("read %s: %w", r.Hash, err)
		}
		if !cur.Found {
			st.missing++
			continue
		}
		if cur.Provider != nil && *cur.Provider == providerHuman && cur.Level != nil && *cur.Level == r.Level {
			st.unchanged++
			continue
		}
		from := "none"
		if cur.Level != nil {
			from = fmt.Sprint(*cur.Level)
		}
		st.changed++
		st.byMove[regradeMove{from: from, to: r.Level}]++
		if !apply {
			continue
		}
		raw, err := json.Marshal(gradeLabels{
			Provider: providerHuman,
			Level:    r.Level,
			Answers:  map[string]bool{"act": r.Level == 3, "nude": r.Level == 2, "underwear": r.Level == 1},
			At:       time.Now().UTC(),
		})
		if err != nil {
			return st, err
		}
		// grade_machine is written once: a second correction must not replace the
		// machine's answer with the first human one.
		if err := db.WithContext(ctx).Exec(`
			UPDATE images
			   SET review_labels = jsonb_set(
			         CASE WHEN review_labels->'grade' IS NOT NULL
			                   AND review_labels->'grade'->>'provider' IS DISTINCT FROM ?
			                   AND review_labels->'grade_machine' IS NULL
			              THEN jsonb_set(review_labels, '{grade_machine}', review_labels->'grade')
			              ELSE coalesce(review_labels, '{}'::jsonb) END,
			         '{grade}', ?::jsonb)
			 WHERE hash = ?`, providerHuman, string(raw), r.Hash).Error; err != nil {
			return st, fmt.Errorf("write %s: %w", r.Hash, err)
		}
	}
	return st, nil
}

func readRegradeInput(path string) ([]regradeRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []regradeRow
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		rec := regradeRow{Level: -1}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if len(rec.Hash) != 64 || rec.Level < 0 || rec.Level > 3 {
			return nil, fmt.Errorf("%s line %d: need a 64-character hash and a level 0-3", path, line)
		}
		if seen[rec.Hash] {
			return nil, fmt.Errorf("%s line %d: %s listed twice", path, line, rec.Hash)
		}
		seen[rec.Hash] = true
		rows = append(rows, rec)
	}
	return rows, sc.Err()
}
