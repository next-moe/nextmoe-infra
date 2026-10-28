package officialcovers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent = "nextmoe-infra/backfill-official-covers (+https://www.kungal.com)"

	defaultVNDBBase = "https://api.vndb.org/kana"
	defaultBgmBase  = "https://api.bgm.tv"

	releaseFields = "id,vns.id,images.type,images.url,images.dims,images.sexual,images.violence,images.vn"

	vnBatch      = 10
	apiGap       = time.Second
	apiRetries   = 5
	maxImageSize = 16 << 20
)

type relImage struct {
	Type     string  `json:"type"`
	URL      string  `json:"url"`
	Dims     []int   `json:"dims"`
	Sexual   float64 `json:"sexual"`
	Violence float64 `json:"violence"`
	VN       *string `json:"vn"`
}

func (i relImage) area() int64 {
	if len(i.Dims) != 2 {
		return 0
	}
	return int64(i.Dims[0]) * int64(i.Dims[1])
}

func (i relImage) portrait() bool {
	return len(i.Dims) == 2 && i.Dims[0] > 0 && isPortrait(i.Dims[0], i.Dims[1])
}

type release struct {
	ID  string `json:"id"`
	VNs []struct {
		ID string `json:"id"`
	} `json:"vns"`
	Images []relImage `json:"images"`
}

func isPortrait(w, h int) bool { return int64(h)*20 > int64(w)*21 }

func bestDigital(rels []release, vn string) *relImage {
	var best *relImage
	for _, r := range rels {
		if !releaseOf(r, vn) {
			continue
		}
		for i := range r.Images {
			img := &r.Images[i]
			if img.Type != "dig" || strings.TrimSpace(img.URL) == "" || (img.VN != nil && *img.VN != vn) {
				continue
			}
			if best == nil || betterDigital(img, best) {
				best = img
			}
		}
	}
	return best
}

func releaseOf(r release, vn string) bool {
	for _, v := range r.VNs {
		if v.ID == vn {
			return true
		}
	}
	return false
}

func betterDigital(a, b *relImage) bool {
	if a.portrait() != b.portrait() {
		return a.portrait()
	}
	if a.area() != b.area() {
		return a.area() > b.area()
	}
	if a.Sexual != b.Sexual {
		return a.Sexual < b.Sexual
	}
	return a.URL < b.URL
}

func ratingLevel(v float64) int16 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	return int16(min(math.Floor(v+0.5), 2))
}

type upstream struct {
	vndbBase, bgmBase, bgmToken string
	http                        *http.Client
	last                        time.Time
}

func (u *upstream) digitalCovers(ctx context.Context, vns []string) (map[string]*relImage, error) {
	out := make(map[string]*relImage, len(vns))
	for i := 0; i < len(vns); i += vnBatch {
		batch := vns[i:min(i+vnBatch, len(vns))]
		var rels []release
		for page := 1; ; page++ {
			body, _ := json.Marshal(map[string]any{
				"filters": vnFilter(batch), "fields": releaseFields, "results": 100, "page": page,
			})
			var resp struct {
				Results []release `json:"results"`
				More    bool      `json:"more"`
			}
			if err := u.call(ctx, http.MethodPost, u.vndbBase+"/release", body, "", &resp); err != nil {
				return nil, fmt.Errorf("vndb releases of %v: %w", batch, err)
			}
			rels = append(rels, resp.Results...)
			if !resp.More {
				break
			}
		}
		for _, vn := range batch {
			out[vn] = bestDigital(rels, vn)
		}
	}
	return out, nil
}

func vnFilter(vns []string) any {
	one := func(v string) any { return []any{"vn", "=", []any{"id", "=", v}} }
	if len(vns) == 1 {
		return one(vns[0])
	}
	out := []any{"or"}
	for _, v := range vns {
		out = append(out, one(v))
	}
	return out
}

func (u *upstream) subjectCover(ctx context.Context, id string) (string, error) {
	var resp struct {
		Images struct {
			Large string `json:"large"`
		} `json:"images"`
	}
	err := u.call(ctx, http.MethodGet, u.bgmBase+"/v0/subjects/"+id, nil, u.bgmToken, &resp)
	if err == errNotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("bangumi subject %s: %w", id, err)
	}
	large := strings.TrimSpace(resp.Images.Large)
	if strings.HasPrefix(large, "//") {
		large = "https:" + large
	}
	return large, nil
}

var errNotFound = fmt.Errorf("not found")

func (u *upstream) call(ctx context.Context, method, url string, body []byte, token string, out any) error {
	var lastErr error
	for attempt := 0; attempt < apiRetries; attempt++ {
		if wait := apiGap - time.Since(u.last); wait > 0 && !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
		u.last = time.Now()
		wait, err := u.once(ctx, method, url, body, token, out)
		if err == nil || wait <= 0 {
			return err
		}
		lastErr = err
		if !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("gave up after %d attempts: %w", apiRetries, lastErr)
}

func (u *upstream) once(ctx context.Context, method, url string, body []byte, token string, out any) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return 5 * time.Second, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return 0, errNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return time.Duration(max(secs, 30)) * time.Second, fmt.Errorf("429")
	case resp.StatusCode >= 500:
		return 5 * time.Second, fmt.Errorf("status %d", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	return 0, json.NewDecoder(resp.Body).Decode(out)
}

func (u *upstream) download(ctx context.Context, src string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 && !sleepCtx(ctx, time.Duration(5<<attempt)*time.Second) {
			return nil, ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := u.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
		resp.Body.Close()
		switch {
		case err != nil:
			lastErr = err
		case resp.StatusCode != http.StatusOK:
			lastErr = fmt.Errorf("image %d", resp.StatusCode)
		case len(body) == 0 || len(body) > maxImageSize:
			return nil, fmt.Errorf("image body of %d bytes", len(body))
		default:
			return body, nil
		}
	}
	return nil, lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
