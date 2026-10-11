package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

// Grading is two modes because the tagger must not run on the production host:
// on 2026-10-11 it ran there through the evening peak (1-minute load 9-10 on
// eight cores) and the owner ruled that compute stays off it. `tag` needs only
// the public CDN and a tagger; `apply` needs only the images database and is
// where the level is computed, so wd.go's rule has one implementation.

type gradeLabels struct {
	Provider string             `json:"provider"`
	Model    string             `json:"model"`
	Level    int                `json:"level"`
	Answers  map[string]bool    `json:"answers,omitempty"`
	Scores   map[string]float64 `json:"scores,omitempty"`
	At       time.Time          `json:"at"`
}

const (
	providerWD        = "wd-tagger"
	providerMoondream = "cloudflare-workers-ai"
)

type tagRecord struct {
	Hash string `json:"hash"`
	wdScores
}

type tagOptions struct {
	In          string
	Out         string
	BaseURL     string
	Concurrency int
	ReadyWait   time.Duration
	Tagger      *wdClient
}

func runTag(ctx context.Context, o tagOptions, w io.Writer) error {
	if o.In == "" || o.Out == "" {
		return fmt.Errorf("--in (hash,ext CSV) and --out (JSONL) are required")
	}
	if o.BaseURL == "" {
		return fmt.Errorf("--base-url is required")
	}
	if o.Tagger == nil || o.Tagger.base == "" {
		return fmt.Errorf("--tagger-url is required")
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 1
	}
	rows, err := readTagInput(o.In)
	if err != nil {
		return err
	}
	done, err := taggedHashes(o.Out)
	if err != nil {
		return err
	}
	todo := rows[:0]
	for _, r := range rows {
		if !done[r.Hash] {
			todo = append(todo, r)
		}
	}
	model, err := o.Tagger.waitReady(ctx, o.ReadyWait)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "listed=%d already_tagged=%d todo=%d concurrency=%d tagger=%s\n",
		len(rows), len(rows)-len(todo), len(todo), o.Concurrency, model)

	out, err := os.OpenFile(o.Out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	fetch := &http.Client{Timeout: 60 * time.Second}
	var (
		mu                   sync.Mutex
		ok, bad, undecodable int
		started              = time.Now()
		undecodableSample    []string
		writeErr             error
	)
	work := make(chan imageRow)
	var wg sync.WaitGroup
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range work {
				var line []byte
				body, err := fetchImage(ctx, fetch, publicURL(o.BaseURL, r, ""))
				var scores wdScores
				if err == nil {
					scores, err = o.Tagger.tag(ctx, body)
				}
				if err == nil {
					line, err = json.Marshal(tagRecord{Hash: r.Hash, wdScores: scores})
				}
				mu.Lock()
				switch {
				case errors.Is(err, errUndecodable):
					undecodable++
					if len(undecodableSample) < 10 {
						undecodableSample = append(undecodableSample, r.Hash)
					}
				case err != nil:
					bad++
					if bad%20 == 1 {
						fmt.Fprintf(w, "error hash=%s: %v\n", r.Hash, err)
					}
				default:
					if _, err := out.Write(append(line, '\n')); err != nil && writeErr == nil {
						writeErr = err
					}
					ok++
				}
				if n := ok + bad + undecodable; n%2000 == 0 {
					fmt.Fprintf(w, "progress done=%d errors=%d rate=%.2f img/s\n",
						ok, bad, float64(n)/time.Since(started).Seconds())
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, r := range todo {
		select {
		case work <- r:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()

	fmt.Fprintf(w, "tag complete done=%d errors=%d undecodable=%d elapsed=%s\n",
		ok, bad, undecodable, time.Since(started).Truncate(time.Second))
	if undecodable > 0 {
		fmt.Fprintf(w, "undecodable images stay ungraded and are listed again next run: %v\n", undecodableSample)
	}
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", o.Out, writeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if bad > 0 {
		return fmt.Errorf("%d images failed to tag", bad)
	}
	return nil
}

func readTagInput(path string) ([]imageRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = 2
	var rows []imageRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(rec[0]) != 64 || rec[1] == "" {
			return nil, fmt.Errorf("%s: want hash,ext, got %q", path, rec)
		}
		rows = append(rows, imageRow{Hash: rec[0], Ext: rec[1]})
	}
}

func taggedHashes(path string) (map[string]bool, error) {
	done := map[string]bool{}
	recs, err := readTagRecords(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	for _, r := range recs {
		done[r.Hash] = true
	}
	return done, err
}

func readTagRecords(path string) ([]tagRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []tagRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var r tagRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if len(r.Hash) != 64 || r.Model == "" {
			return nil, fmt.Errorf("%s line %d: a record needs a 64-character hash and a model", path, line)
		}
		recs = append(recs, r)
	}
	return recs, sc.Err()
}

type applyOptions struct {
	DSN            string
	In             string
	ReplaceMachine bool
	Apply          bool
}

type gradeMove struct {
	from string
	to   int
}

type applyStats struct {
	written, missing, keptHuman, keptWD, keptMachine int
	moves                                            map[gradeMove]int
	shown, hidden                                    int
}

func runApply(ctx context.Context, o applyOptions, w io.Writer) error {
	if o.DSN == "" || o.In == "" {
		return fmt.Errorf("--dsn and --in are required")
	}
	recs, err := readTagRecords(o.In)
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
	st, err := applyGrades(ctx, db, recs, o)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "apply=%v replace_machine=%v records=%d written=%d missing=%d kept_human=%d kept_wd=%d kept_machine=%d\n",
		o.Apply, o.ReplaceMachine, len(recs), st.written, st.missing, st.keptHuman, st.keptWD, st.keptMachine)
	fmt.Fprintf(w, "visibility for SFW readers: newly hidden=%d newly shown=%d\n", st.hidden, st.shown)
	moves := make([]gradeMove, 0, len(st.moves))
	for m := range st.moves {
		moves = append(moves, m)
	}
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].from != moves[j].from {
			return moves[i].from < moves[j].from
		}
		return moves[i].to < moves[j].to
	})
	for _, m := range moves {
		fmt.Fprintf(w, "  level %-4s -> %d  %7d\n", m.from, m.to, st.moves[m])
	}
	return nil
}

func applyGrades(ctx context.Context, db *gorm.DB, recs []tagRecord, o applyOptions) (*applyStats, error) {
	st := &applyStats{moves: map[gradeMove]int{}}
	at := time.Now().UTC()
	for start := 0; start < len(recs); start += 1000 {
		batch := recs[start:min(start+1000, len(recs))]
		hashes := make([]string, len(batch))
		for i, r := range batch {
			hashes[i] = r.Hash
		}
		var current []struct {
			Hash     string  `gorm:"column:hash"`
			Provider *string `gorm:"column:provider"`
			Level    *int    `gorm:"column:level"`
		}
		if err := db.WithContext(ctx).Raw(
			`SELECT hash, review_labels->'grade'->>'provider' AS provider,
			        (review_labels->'grade'->>'level')::int AS level
			   FROM images WHERE hash IN ? AND deleted_at IS NULL`, hashes).
			Scan(&current).Error; err != nil {
			return st, fmt.Errorf("read current grades: %w", err)
		}
		byHash := make(map[string]int, len(current))
		for i, c := range current {
			byHash[strings.TrimSpace(c.Hash)] = i
		}

		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, r := range batch {
				i, ok := byHash[r.Hash]
				if !ok {
					st.missing++
					continue
				}
				c := current[i]
				from := "none"
				if c.Provider != nil {
					switch *c.Provider {
					case providerHuman:
						st.keptHuman++
						continue
					case providerWD:
						st.keptWD++
						continue
					case providerMoondream:
						if !o.ReplaceMachine {
							st.keptMachine++
							continue
						}
					}
					if c.Level != nil {
						from = fmt.Sprint(*c.Level)
					}
				}
				level, err := wdLevel(r.wdScores)
				if err != nil {
					return fmt.Errorf("%s: %w", r.Hash, err)
				}
				st.written++
				st.moves[gradeMove{from: from, to: level}]++
				if c.Level != nil {
					switch was, now := *c.Level >= 2, level >= 2; {
					case was && !now:
						st.shown++
					case !was && now:
						st.hidden++
					}
				}
				if !o.Apply {
					continue
				}
				scores := make(map[string]float64, len(r.Rating)+len(r.Tags))
				for k, v := range r.Rating {
					scores[k] = v
				}
				for k, v := range r.Tags {
					scores[k] = v
				}
				raw, err := json.Marshal(gradeLabels{Provider: providerWD, Model: r.Model, Level: level, Scores: scores, At: at})
				if err != nil {
					return err
				}
				// The guard re-checks what the read saw, so a human regrade that
				// lands between the read and this write is never overwritten.
				if err := tx.Exec(`
					UPDATE images
					   SET review_labels = jsonb_set(
					         CASE WHEN review_labels->'grade'->>'provider' = ?
					                   AND review_labels->'grade_moondream' IS NULL
					              THEN jsonb_set(review_labels, '{grade_moondream}', review_labels->'grade')
					              ELSE coalesce(review_labels, '{}'::jsonb) END,
					         '{grade}', ?::jsonb)
					 WHERE hash = ?
					   AND (review_labels->'grade' IS NULL
					        OR (? AND review_labels->'grade'->>'provider' = ?))`,
					providerMoondream, string(raw), r.Hash, o.ReplaceMachine, providerMoondream).Error; err != nil {
					return fmt.Errorf("write %s: %w", r.Hash, err)
				}
			}
			return nil
		})
		if err != nil {
			return st, err
		}
	}
	return st, nil
}

func fetchImage(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusOK && readErr == nil:
				return body, nil
			case resp.StatusCode == http.StatusNotFound:
				return nil, fmt.Errorf("fetch %s: http 404", url)
			case readErr != nil:
				lastErr = readErr
			default:
				lastErr = fmt.Errorf("fetch %s: http %d", url, resp.StatusCode)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return nil, lastErr
}
