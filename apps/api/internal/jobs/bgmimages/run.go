package bgmimages

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type Kind string

const (
	KindCovers  Kind = "covers"
	KindPersons Kind = "persons"
)

const manifestName = "dims.jsonl"

type Opts struct {
	Kind        Kind
	Out         string
	APIBase     string
	UserAgent   string
	Token       string
	Rate        float64
	Concurrency int
	MaxRetries  int
}

type Stats struct {
	Kind         Kind
	IDs          int
	Downloaded   int64
	SkippedExist int64
	NoImage      int64
	NotFound     int64
	Errors       int64
}

func (s *Stats) Line() string {
	return fmt.Sprintf("fetch-bangumi-images: done — kind=%s ids=%d downloaded=%d skipped_exist=%d no_image=%d not_found=%d errors=%d",
		s.Kind, s.IDs, s.Downloaded, s.SkippedExist, s.NoImage, s.NotFound, s.Errors)
}

type manifestRecord struct {
	SubjectID int    `json:"subject_id,omitempty"`
	ID        string `json:"id,omitempty"`
	File      string `json:"file"`
	W         int    `json:"w"`
	H         int    `json:"h"`
	URL       string `json:"url,omitempty"`
}

func (r manifestRecord) key() string {
	if r.ID != "" {
		return r.ID
	}
	if r.SubjectID > 0 {
		return strconv.Itoa(r.SubjectID)
	}
	return ""
}

type runner struct {
	opts     Opts
	http     *httpClient
	stats    *Stats
	recorded map[string]bool

	mu       sync.Mutex
	manifest *os.File
}

func Run(ctx context.Context, opts Opts, ids []int) (*Stats, error) {
	st := &Stats{Kind: opts.Kind, IDs: len(ids)}
	if opts.Kind != KindCovers && opts.Kind != KindPersons {
		return st, fmt.Errorf("unknown kind %q (want covers or persons)", opts.Kind)
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return st, err
	}
	path := filepath.Join(opts.Out, manifestName)
	recorded, err := loadRecorded(path)
	if err != nil {
		return st, err
	}
	mf, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return st, err
	}
	defer mf.Close()

	r := &runner{
		opts:     opts,
		http:     newHTTPClient(opts.Rate, opts.UserAgent, opts.Token, opts.MaxRetries),
		stats:    st,
		recorded: recorded,
		manifest: mf,
	}
	one := r.cover
	if opts.Kind == KindPersons {
		one = r.person
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, max(opts.Concurrency, 1))
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-runCtx.Done():
		}
		if runCtx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := one(runCtx, id); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(id)
	}
	wg.Wait()
	if firstErr != nil {
		return st, firstErr
	}
	return st, ctx.Err()
}

// apiFailure sorts a failed API hop: nil when the id was counted and the run
// goes on, the context error when the run is being cancelled.
func (r *runner) apiFailure(ctx context.Context, id int, status int, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if status == http.StatusNotFound {
		atomic.AddInt64(&r.stats.NotFound, 1)
		return nil
	}
	r.fail(id, "api", err)
	return nil
}

func (r *runner) fail(id int, what string, err error) {
	atomic.AddInt64(&r.stats.Errors, 1)
	log.Printf("fetch-bangumi-images: %s %d: %s: %v", r.opts.Kind, id, what, err)
}

func (r *runner) append(rec manifestRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = r.manifest.Write(append(line, '\n'))
	return err
}

func loadRecorded(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	set := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec manifestRecord
		if json.Unmarshal([]byte(strings.TrimSpace(sc.Text())), &rec) != nil {
			continue
		}
		if k := rec.key(); k != "" {
			set[k] = true
		}
	}
	return set, sc.Err()
}

func LoadIDs(path string) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []int
	seen := map[int]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := strconv.Atoi(line)
		if err != nil || id <= 0 {
			log.Printf("fetch-bangumi-images: skipping non-integer id %q", line)
			continue
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, sc.Err()
}

func apiURL(base, kind string, id int) string {
	return fmt.Sprintf("%s/v0/%s/%d", strings.TrimRight(base, "/"), kind, id)
}

func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}
