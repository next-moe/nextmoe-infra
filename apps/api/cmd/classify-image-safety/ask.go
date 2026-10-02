package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type askOptions struct {
	In          string
	Out         string
	Question    string
	BaseURL     string
	Concurrency int
	GuardDSN    string
	GuardShare  float64
	Client      *moondreamClient
}

type askRecord struct {
	Hash   string `json:"hash"`
	Answer bool   `json:"answer"`
}

// runAsk puts one yes/no question to a list of images and writes the answers
// down. It reads no database and writes none: it is the instrument a candidate
// ladder question is measured with before the ladder changes.
func runAsk(ctx context.Context, o askOptions, w io.Writer) error {
	if o.In == "" || o.Out == "" || o.Question == "" {
		return fmt.Errorf("--in, --out and --question are required")
	}
	if o.BaseURL == "" {
		return fmt.Errorf("--base-url is required")
	}
	if !o.Client.configured() {
		return fmt.Errorf("moondream client not configured (need --cf-account and --cf-token)")
	}
	rows, err := readAskInput(o.In)
	if err != nil {
		return err
	}
	done, err := loadDone(o.Out)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(o.Out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	ctx, stop := context.WithCancelCause(ctx)
	stopGuard, err := startUsageGuard(ctx, gradeOptions{GuardDSN: o.GuardDSN, GuardShare: o.GuardShare}, stop, w)
	if err != nil {
		stop(nil)
		return err
	}
	defer func() {
		stop(nil)
		stopGuard()
	}()
	fetch := &http.Client{Timeout: 60 * time.Second}
	var (
		mu           sync.Mutex
		yes, no, bad int
	)
	enc := json.NewEncoder(out)
	work := make(chan imageRow)
	var wg sync.WaitGroup
	for range max(o.Concurrency, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range work {
				rec := askRecord{Hash: r.Hash}
				body, err := fetchImage(ctx, fetch, publicURL(o.BaseURL, r, ""))
				if err == nil {
					rec.Answer, err = o.Client.askYesNo(ctx, dataURI("image/"+r.Ext, body), o.Question)
				}
				if errors.Is(err, errDailyQuota) {
					stop(err)
					continue
				}
				mu.Lock()
				switch {
				case err != nil:
					bad++
				case rec.Answer:
					yes++
				default:
					no++
				}
				// A failed image is not written, so the next run asks it again.
				if err == nil {
					_ = enc.Encode(rec)
				}
				if n := yes + no + bad; n%500 == 0 {
					fmt.Fprintf(w, "progress yes=%d no=%d errors=%d neurons=%.0f\n", yes, no, bad, o.Client.neurons())
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, r := range rows {
		if done[r.Hash] {
			continue
		}
		select {
		case work <- r:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	fmt.Fprintf(w, "ask complete yes=%d no=%d errors=%d skipped=%d neurons=%.0f\n", yes, no, bad, len(done), o.Client.neurons())
	if cause := context.Cause(ctx); cause != nil && cause != context.Canceled {
		return cause
	}
	if bad > 0 {
		return fmt.Errorf("%d images got no answer; run again to retry them", bad)
	}
	return nil
}

func readAskInput(path string) ([]imageRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []imageRow
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var rec struct {
			Hash string `json:"hash"`
			Ext  string `json:"ext"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if len(rec.Hash) < 4 || rec.Ext == "" {
			return nil, fmt.Errorf("%s line %d: need hash and ext", path, line)
		}
		rows = append(rows, imageRow{Hash: rec.Hash, Ext: rec.Ext})
	}
	return rows, sc.Err()
}
