package personadj

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type BatchOpts struct {
	PacketsPath  string
	VerdictsPath string
	ErrorsPath   string
	Bucket       Bucket
	Workers      int
	Limit        int
	Chunk        int
}

type BatchStats struct {
	Packets  int
	Skipped  int
	Judged   int
	Errors   int
	Merge    int
	Distinct int
	Unsure   int
	Elapsed  time.Duration
}

type batchError struct {
	Key    string `json:"key"`
	Bucket Bucket `json:"bucket"`
	Error  string `json:"error"`
	At     string `json:"at"`
}

func RunBatch(ctx context.Context, j Judge, opts BatchOpts) (*BatchStats, error) {
	packets, err := LoadPackets(opts.PacketsPath, opts.Bucket)
	if err != nil {
		return nil, err
	}
	done, err := LoadVerdictKeys(opts.VerdictsPath)
	if err != nil {
		return nil, fmt.Errorf("read existing verdicts: %w", err)
	}
	st := &BatchStats{Packets: len(packets)}
	todo := make([]Packet, 0, len(packets))
	for _, p := range packets {
		if done[p.Key] {
			st.Skipped++
			continue
		}
		todo = append(todo, p)
	}
	if opts.Limit > 0 && opts.Limit < len(todo) {
		todo = todo[:opts.Limit]
	}

	vf, err := os.OpenFile(opts.VerdictsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer vf.Close()
	var ef *os.File
	if opts.ErrorsPath != "" {
		if ef, err = os.OpenFile(opts.ErrorsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			return nil, err
		}
		defer ef.Close()
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 8
	}
	var mu sync.Mutex
	var progressed int64
	start := time.Now()

	chunks := chunkPackets(todo, opts.Chunk)
	in := make(chan []Packet)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range in {
				if ctx.Err() != nil {
					continue
				}
				vs, err := judgeChunk(ctx, j, c)
				mu.Lock()
				if err != nil {
					for _, p := range c {
						st.Errors++
						writeLine(ef, batchError{Key: p.Key, Bucket: p.Bucket, Error: err.Error(), At: time.Now().UTC().Format(time.RFC3339)})
					}
				} else {
					for _, v := range vs {
						st.Judged++
						switch v.Verdict {
						case VerdictMerge:
							st.Merge++
						case VerdictDistinct:
							st.Distinct++
						default:
							st.Unsure++
						}
						writeLine(vf, v)
					}
				}
				mu.Unlock()
				if n := atomic.AddInt64(&progressed, int64(len(c))); n%100 < int64(len(c)) {
					slog.Info("adjudicate progress", "done", n, "of", len(todo),
						"elapsed", time.Since(start).Round(time.Second))
				}
			}
		}()
	}
	for _, c := range chunks {
		select {
		case <-ctx.Done():
			close(in)
			wg.Wait()
			st.Elapsed = time.Since(start)
			return st, ctx.Err()
		case in <- c:
		}
	}
	close(in)
	wg.Wait()
	st.Elapsed = time.Since(start)
	return st, nil
}

func judgeChunk(ctx context.Context, j Judge, c []Packet) ([]Verdict, error) {
	if bj, ok := j.(BatchJudge); ok && len(c) > 1 {
		return bj.JudgeBatch(ctx, c)
	}
	out := make([]Verdict, 0, len(c))
	for _, p := range c {
		v, err := j.Judge(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func chunkPackets(ps []Packet, size int) [][]Packet {
	if size < 1 {
		size = 1
	}
	var out [][]Packet
	var cur []Packet
	for _, p := range ps {
		if len(cur) > 0 && (len(cur) == size || cur[0].Bucket != p.Bucket) {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, p)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func writeLine(w io.Writer, v any) {
	if w == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		slog.Error("adjudicate: marshal record", "error", err)
		return
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		slog.Error("adjudicate: append record", "error", err)
	}
	if f, ok := w.(*os.File); ok {
		_ = f.Sync()
	}
}
