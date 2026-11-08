package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	stickerSetTTL     = 30 * time.Minute
	stickerMissReload = 5 * time.Minute
)

type OfficialStickers struct {
	base string
	key  string
	http *http.Client
	now  func() time.Time

	mu      sync.Mutex
	hashes  map[string]bool
	fetched time.Time
}

func NewOfficialStickers(baseURL, apiKey string) *OfficialStickers {
	return &OfficialStickers{
		base: strings.TrimRight(baseURL, "/"), key: apiKey,
		http: &http.Client{Timeout: 15 * time.Second}, now: time.Now,
	}
}

func (s *OfficialStickers) IsOfficial(ctx context.Context, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	age := s.now().Sub(s.fetched)
	if s.hashes == nil || age > stickerSetTTL || (!s.hashes[hash] && age > stickerMissReload) {
		set, err := s.fetch(ctx)
		switch {
		case err == nil:
			s.hashes = set
		case s.hashes == nil:
			return false, err
		}
		s.fetched = s.now()
	}
	return s.hashes[hash], nil
}

func (s *OfficialStickers) Hashes(ctx context.Context) (map[string]bool, error) { return s.fetch(ctx) }

func (s *OfficialStickers) fetch(ctx context.Context) (map[string]bool, error) {
	var ids []string
	q := url.Values{"official": {"true"}, "limit": {"100"}}
	for {
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err := s.get(ctx, "/v2/sticker/packs?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		for _, p := range page.Items {
			ids = append(ids, p.ID)
		}
		if page.NextCursor == "" {
			break
		}
		q.Set("cursor", page.NextCursor)
	}
	set := map[string]bool{}
	for _, id := range ids {
		var pack struct {
			Stickers []struct {
				Image struct {
					Hash string `json:"hash"`
				} `json:"image"`
			} `json:"stickers"`
		}
		if err := s.get(ctx, "/v2/sticker/packs/"+url.PathEscape(id), &pack); err != nil {
			return nil, err
		}
		for _, st := range pack.Stickers {
			if st.Image.Hash != "" {
				set[st.Image.Hash] = true
			}
		}
	}
	return set, nil
}

func (s *OfficialStickers) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("sticker face %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("sticker face %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sticker face %s: status %d: %.200s", path, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("sticker face %s: %w", path, err)
	}
	return nil
}
