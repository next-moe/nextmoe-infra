package ymgalnews

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"api/internal/platform/news/model"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const (
	bannerPreset = "news_banner"
	uploaderSub  = "system:ymgal-news"
	maxBannerB   = 10 << 20
)

type outcome int

const (
	outcomeCreated outcome = iota
	outcomeUpdated
	outcomeUnchanged
	outcomeSkipped
)

type writer struct {
	db     *gorm.DB
	opts   Opts
	images *imageclient.Client
	http   *http.Client
}

func newWriter(cfg *config.Config, db *gorm.DB, opts Opts) (*writer, error) {
	w := &writer{db: db, opts: opts, http: &http.Client{Timeout: 60 * time.Second}}
	if opts.NoImages {
		return w, nil
	}
	ic := cfg.NewsImageClient
	if ic.ClientID == "" || ic.ClientSecret == "" {
		slog.Warn("ymgal: news image client not configured — banners will be skipped (set KUN_NEWS_IMAGE_CLIENT_ID/SECRET)")
		return w, nil
	}
	base := ic.BaseURL
	if opts.ImageBase != "" {
		base = opts.ImageBase
	}
	w.images = imageclient.New(imageclient.Config{
		BaseURL:      base,
		CDNBase:      cfg.ImageService.CDNBase,
		ClientID:     ic.ClientID,
		ClientSecret: ic.ClientSecret,
	})
	return w, nil
}

// truncatePreview enforces 月幕's ceiling in runes, not bytes: the previews are
// Chinese, and a byte-counted cut would both under-fill the allowance and split
// a character in half.
func truncatePreview(s string) (string, bool) {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= model.PreviewMaxRunes {
		return string(r), false
	}
	return string(r[:model.PreviewMaxRunes]), true
}

func (w *writer) apply(ctx context.Context, lane string, t Topic, st *stats) (outcome, error) {
	if strings.TrimSpace(t.TopicID) == "" || strings.TrimSpace(t.TopicURL) == "" || strings.TrimSpace(t.Title) == "" {
		// source_url carries the only route to the full article, which is the
		// entire shape of the permission we were given. A row without one is not
		// a degraded row, it is a row we are not allowed to publish.
		st.skipped++
		slog.Warn("ymgal: topic missing id, url or title — skipped", "topic", t.TopicID, "lane", lane)
		return outcomeSkipped, nil
	}
	published, err := t.PublishedAt()
	if err != nil {
		st.skipped++
		slog.Warn("ymgal: unparsable publishTime — skipped", "topic", t.TopicID, "err", err)
		return outcomeSkipped, nil
	}

	preview, cut := truncatePreview(t.Introduction)
	if cut {
		st.truncated++
	}

	// Find into a slice rather than Take: "this topic is new" is the common case
	// on a first import, and Take reports it as ErrRecordNotFound, which GORM
	// logs as an error line per row.
	var found []model.NewsItem
	if err := w.db.WithContext(ctx).
		Where("source_key = ? AND external_id = ?", model.SourceKeyYmgal, t.TopicID).
		Limit(1).Find(&found).Error; err != nil {
		return outcomeSkipped, err
	}
	var existing model.NewsItem
	if len(found) > 0 {
		existing = found[0]
	}

	if existing.ID == 0 {
		if !w.opts.Apply {
			st.wouldCreate++
			return outcomeCreated, nil
		}
		hash, origin := w.banner(ctx, t.MainImg, "", "", st)
		row := model.NewsItem{
			SourceKey:        model.SourceKeyYmgal,
			Lane:             lane,
			ExternalID:       t.TopicID,
			Title:            t.Title,
			Preview:          preview,
			UpstreamCategory: t.TopicCategory,
			SourceURL:        t.TopicURL,
			BannerHash:       hash,
			BannerOriginURL:  origin,
			PublishedAt:      published,
			Status:           model.StatusPending,
		}
		if err := w.db.WithContext(ctx).Create(&row).Error; err != nil {
			return outcomeSkipped, err
		}
		st.created++
		return outcomeCreated, nil
	}

	if existing.Lane != lane {
		st.laneFlip++
		slog.Warn("ymgal: topic changed lane — the shared-id-space assumption behind the unique key may be wrong",
			"topic", t.TopicID, "from", existing.Lane, "to", lane)
	}

	changed := existing.Title != t.Title ||
		existing.Preview != preview ||
		existing.SourceURL != t.TopicURL ||
		existing.Lane != lane ||
		existing.UpstreamCategory != t.TopicCategory ||
		!existing.PublishedAt.Equal(published)

	if !changed {
		if w.opts.Apply {
			// last_seen_at only. Touching updated_at here would make every row in
			// the table look freshly modified on every ten-minute poll and bury the
			// signal that something actually changed.
			if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).
				Where("id = ?", existing.ID).
				UpdateColumn("last_seen_at", time.Now()).Error; err != nil {
				return outcomeSkipped, err
			}
		}
		st.unchanged++
		return outcomeUnchanged, nil
	}

	if !w.opts.Apply {
		st.wouldUpdate++
		return outcomeUpdated, nil
	}

	hash, origin := w.banner(ctx, t.MainImg, existing.BannerOriginURL, existing.BannerHash, st)
	updates := map[string]any{
		"lane":              lane,
		"title":             t.Title,
		"preview":           preview,
		"upstream_category": t.TopicCategory,
		"source_url":        t.TopicURL,
		"banner_hash":       hash,
		"banner_origin_url": origin,
		"published_at":      published,
		"last_seen_at":      time.Now(),
		"updated_at":        time.Now(),
	}
	// A published item whose text changed goes back to pending, and the run's
	// standing release puts it out again: that release's decision row is the only
	// record that the text changed after it was published. rejected and withdrawn
	// are left alone — a human decided those, and re-seeing the article upstream
	// is not new information about that decision.
	if existing.Status == model.StatusPublished {
		updates["status"] = model.StatusPending
	}
	if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return outcomeSkipped, err
	}
	st.updated++
	return outcomeUpdated, nil
}

// banner returns the hash to store and the origin it came from. A failed upload
// is never fatal: the text row is the deliverable, and the next sweep retries.
func (w *writer) banner(ctx context.Context, mainImg, haveOrigin, haveHash string, st *stats) (hash, origin string) {
	mainImg = strings.TrimSpace(mainImg)
	if mainImg == "" {
		return "", ""
	}
	if w.images == nil {
		return haveHash, haveOrigin
	}
	if mainImg == haveOrigin && haveHash != "" {
		return haveHash, haveOrigin
	}
	res, err := w.upload(ctx, mainImg)
	if err != nil {
		st.imagesFail++
		slog.Warn("ymgal: banner fetch/upload failed — row kept, retried next sweep", "url", mainImg, "err", err)
		return haveHash, haveOrigin
	}
	st.imagesUp++
	return res, mainImg
}

func (w *writer) upload(ctx context.Context, src string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "NextMoe-news-ingest/1.0 (+https://www.nextmoe.dev)")
	req.Header.Set("Referer", "https://www.ymgal.games/")

	resp, err := w.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch banner: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBannerB+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxBannerB {
		return "", fmt.Errorf("banner exceeds %d bytes", maxBannerB)
	}

	name := path.Base(src)
	if name == "" || name == "." || name == "/" {
		name = "banner.webp"
	}
	res, err := w.images.UploadWithSub(ctx, bytes.NewReader(body), name, bannerPreset, uploaderSub)
	if err != nil {
		return "", err
	}
	return res.Hash, nil
}

// livenessCandidates lists rows we hold that the scan should have seen and did
// not. 苍麟 removes ad spam by hand after the fact, so an append-only mirror
// would keep republishing what he has already deleted.
func (w *writer) livenessCandidates(ctx context.Context, complete bool, oldest time.Time, seen map[string]struct{}) ([]string, error) {
	if !complete || oldest.IsZero() || len(seen) == 0 {
		return nil, nil
	}
	var held []string
	if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("source_key = ? AND dead_at IS NULL AND published_at >= ?", model.SourceKeyYmgal, oldest).
		Pluck("external_id", &held).Error; err != nil {
		return nil, err
	}
	var missing []string
	for _, id := range held {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func (w *writer) markDead(ctx context.Context, externalIDs []string) (int, error) {
	res := w.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("source_key = ? AND external_id IN ? AND dead_at IS NULL", model.SourceKeyYmgal, externalIDs).
		Updates(map[string]any{"dead_at": time.Now(), "updated_at": time.Now()})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
