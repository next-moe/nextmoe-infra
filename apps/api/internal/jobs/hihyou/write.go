package hihyou

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
	"api/internal/platform/news/service"
	"api/pkg/config"
	"api/pkg/imageclient"
	"api/pkg/imageshrink"

	"gorm.io/gorm"
)

const (
	bannerPreset = "news_banner"
	uploaderSub  = "system:hihyou-weekly"
	// What we are willing to DOWNLOAD, which is not what the service accepts:
	// the largest picture in the corpus is 19.7 MB and imageshrink.Shrink refits it before
	// upload. A 10 MiB ceiling here would drop 546 pictures at the fetch.
	maxImageB = 24 << 20
)

type writer struct {
	db     *gorm.DB
	opts   Opts
	images *imageclient.Client
	http   *http.Client

	// uploaded caches origin URL -> hash within one run. The weekly repeats the
	// same header image across issues, and the corpus holds 8,125 pictures: a
	// cold re-download per occurrence is the difference between minutes and hours.
	uploaded map[string]string
	// URLs warm already tried and lost. Without this the serial pass retries
	// each one immediately, doubling both the wait and the reported failures
	// for a picture that is simply gone upstream.
	failed map[string]bool
}

func newWriter(cfg *config.Config, db *gorm.DB, opts Opts) *writer {
	w := &writer{db: db, opts: opts, uploaded: map[string]string{},
		failed: map[string]bool{}, http: &http.Client{Timeout: 60 * time.Second}}
	if opts.NoImages {
		return w
	}
	ic := cfg.NewsImageClient
	if ic.ClientID == "" || ic.ClientSecret == "" {
		slog.Warn("hihyou: news image client not configured — pictures will be skipped (set KUN_NEWS_IMAGE_CLIENT_ID/SECRET)")
		return w
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
	return w
}

// ExternalID is the item's identity. The ordinal comes from paragraph order, so
// the segmentation must stay deterministic — a predicate change that reorders
// items rewrites identities rather than updating them.
func ExternalID(cv int64, ordinal int) string {
	return fmt.Sprintf("cv%d#%d", cv, ordinal)
}

func SourceURL(cv int64) string {
	return fmt.Sprintf("https://www.bilibili.com/read/cv%d", cv)
}

// preview joins the item's body and truncates in runes. Unlike 月幕, the body
// text here IS authorised — the ceiling is the schema's, kept so both partners'
// rows carry the same promise on the read face.
func preview(body []string) (string, bool) {
	r := []rune(strings.TrimSpace(strings.Join(body, "\n")))
	if len(r) <= model.PreviewMaxRunes {
		return string(r), false
	}
	return string(r[:model.PreviewMaxRunes]), true
}

func (w *writer) applyItem(ctx context.Context, cv int64, published time.Time, it Item, st *stats) error {
	text, cut := preview(it.Body)
	if text == "" {
		// A heading whose block holds only an image has no preview to publish.
		// The gate already refused issues where this happens more than twice.
		st.droppedEmpty++
		return nil
	}
	if cut {
		st.truncated++
	}

	extID := ExternalID(cv, it.Ordinal)
	var found []model.NewsItem
	if err := w.db.WithContext(ctx).
		Where("source_key = ? AND external_id = ?", model.SourceKeyHihyou, extID).
		Limit(1).Find(&found).Error; err != nil {
		return err
	}

	if len(found) == 0 {
		if !w.opts.Apply {
			st.wouldCreate++
			st.wouldImages += len(it.Pictures)
			return nil
		}
		banner, origin := w.picture(ctx, first(it.Pictures), st)
		row := model.NewsItem{
			SourceKey:        model.SourceKeyHihyou,
			Lane:             model.LaneNews,
			ExternalID:       extID,
			Title:            it.Title,
			Preview:          text,
			UpstreamCategory: it.Section,
			SourceURL:        SourceURL(cv),
			BannerHash:       banner,
			BannerOriginURL:  origin,
			PublishedAt:      published,
			Status:           model.StatusPending,
		}
		if err := w.db.WithContext(ctx).Create(&row).Error; err != nil {
			return err
		}
		st.created++
		return w.pictures(ctx, row.ID, it.Pictures[minInt(1, len(it.Pictures)):], st)
	}

	existing := found[0]
	changed := existing.Title != it.Title ||
		existing.Preview != text ||
		existing.UpstreamCategory != it.Section ||
		!existing.PublishedAt.Equal(published)
	if !changed {
		st.unchanged++
		if !w.opts.Apply {
			return nil
		}
		// The only place a row created without its pictures can still get them:
		// text that never changes never reaches the update branch. 04b wrote 4,425
		// rows with --no-images, and picture() tells the log a failed upload is
		// "retried next run" — without this, both are permanent.
		return w.backfillImages(ctx, existing, it, st)
	}
	if !w.opts.Apply {
		st.wouldUpdate++
		return nil
	}
	updates := map[string]any{
		"title":             it.Title,
		"preview":           text,
		"upstream_category": it.Section,
		"source_url":        SourceURL(cv),
		"published_at":      published,
		"last_seen_at":      time.Now(),
		"updated_at":        time.Now(),
	}
	// Same rule as the 月幕 path: an edit to a published row sends it back to the
	// queue. Here the text can only change because OUR segmentation changed,
	// which is exactly the case a human needs to see again.
	if existing.Status == model.StatusPublished {
		updates["status"] = model.StatusPending
	}
	if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return err
	}
	st.updated++
	// Pictures are reconciled on update too, not only on create: a segmentation
	// change moves paragraph boundaries, so the block's images can change with
	// its text.
	return w.backfillImages(ctx, existing, it, st)
}

// backfillImages brings a stored row up to the pictures its item currently has.
// It is a no-op for a row that already has them, and it never re-fetches: what
// to download is decided by the origin URLs already on disk, not by re-uploading
// and letting the unique key absorb the duplicate.
func (w *writer) backfillImages(ctx context.Context, row model.NewsItem, it Item, st *stats) error {
	if w.images == nil {
		return nil
	}
	if row.BannerHash == "" {
		if banner, origin := w.picture(ctx, first(it.Pictures), st); banner != "" {
			if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).Where("id = ?", row.ID).
				Updates(map[string]any{"banner_hash": banner, "banner_origin_url": origin}).Error; err != nil {
				return err
			}
		}
	}
	return w.pictures(ctx, row.ID, it.Pictures[minInt(1, len(it.Pictures)):], st)
}

func (w *writer) pictures(ctx context.Context, itemID int64, urls []string, st *stats) error {
	if len(urls) == 0 || w.images == nil {
		return nil
	}
	// Skipping by origin URL is what keeps a re-run cheap: deduplicating on the
	// hash instead would mean downloading and uploading all 8,080 pictures again
	// to discover that every one of them is already there.
	var have []string
	if err := w.db.WithContext(ctx).Model(&model.NewsItemImage{}).
		Where("item_id = ?", itemID).Pluck("origin_url", &have).Error; err != nil {
		return err
	}
	stored := make(map[string]bool, len(have))
	for _, u := range have {
		stored[u] = true
	}

	for i, u := range urls {
		if stored[u] {
			continue
		}
		hash, _ := w.picture(ctx, u, st)
		if hash == "" {
			continue
		}
		row := model.NewsItemImage{ItemID: itemID, ImageHash: hash, OriginURL: u, Position: i}
		// The unique key is (item_id, image_hash): the weekly repeats a picture
		// inside one item often enough that a plain insert would abort the row.
		if err := w.db.WithContext(ctx).
			Where("item_id = ? AND image_hash = ?", itemID, hash).
			FirstOrCreate(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) picture(ctx context.Context, src string, st *stats) (hash, origin string) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", src
	}
	if h, ok := w.uploaded[src]; ok {
		return h, src
	}
	if w.images == nil || w.failed[src] {
		return "", src
	}
	h, err := w.upload(ctx, src)
	if err != nil {
		st.imagesFail++
		slog.Warn("hihyou: picture fetch/upload failed — row kept, retried next run", "url", src, "err", err)
		return "", src
	}
	w.uploaded[src] = h
	st.imagesUp++
	return h, src
}

func (w *writer) upload(ctx context.Context, src string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	// No Referer. The charter warned of hotlink protection on i0.hdslb.com by
	// analogy with getchu; a probe showed a bare GET returns the same bytes, so
	// sending one would be a header that never did anything.
	req.Header.Set("User-Agent", "NextMoe-news-ingest/1.0 (+https://www.nextmoe.dev)")
	resp, err := w.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch picture: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageB+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxImageB {
		return "", fmt.Errorf("picture exceeds %d bytes", maxImageB)
	}
	name := path.Base(src)
	if name == "" || name == "." || name == "/" {
		name = "news.webp"
	}
	body, name, err = imageshrink.Shrink(body, name)
	if err != nil {
		return "", err
	}
	res, err := w.images.UploadWithSub(ctx, bytes.NewReader(body), name, bannerPreset, uploaderSub)
	if err != nil {
		return "", err
	}
	return res.Hash, nil
}

const releaseReason = "hihyou standing release: column already moderated by bilibili (user adjudication 2026-09-05)"

func (w *writer) release(ctx context.Context, actorUID int64) (int, error) {
	return service.ReleasePendingImports(ctx, w.db, model.SourceKeyHihyou, actorUID, releaseReason)
}

// applyIssue writes one issue that passed the gate.
func (w *writer) applyIssue(ctx context.Context, seg Segmentation, published time.Time, st *stats) error {
	if w.opts.Apply {
		var urls []string
		for _, it := range seg.Items {
			urls = append(urls, it.Pictures...)
		}
		w.warm(ctx, urls, st)
	}
	for _, it := range seg.Items {
		if err := w.applyItem(ctx, seg.CV, published, it, st); err != nil {
			return fmt.Errorf("issue %d item %d: %w", seg.IssueNo, it.Ordinal, err)
		}
	}
	return nil
}

// seedSource writes 批评's news_source row. 01 波 deliberately left it out: the
// column URL it must carry was not known until this backfill, and a placeholder
// homepage_url would put a wrong link under the one condition she set.
func (w *writer) seedSource(ctx context.Context) error {
	const stmt = `
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES (?, ?, ?, ?, ?, ?, true)
		ON CONFLICT (key) DO NOTHING`
	return w.db.WithContext(ctx).Exec(stmt,
		model.SourceKeyHihyou,
		"Galgame 批评",
		fmt.Sprintf("https://space.bilibili.com/%d", SpaceMID),
		"本条情报转载自 Galgame 批评的《Gal周报》专栏,点击标题可跳转至原文",
		115235,
		fmt.Sprintf("https://space.bilibili.com/%d/article", SpaceMID),
	).Error
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
