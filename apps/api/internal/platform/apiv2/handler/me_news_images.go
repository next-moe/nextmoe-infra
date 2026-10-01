package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/settings/keys"
	"api/pkg/imageclient"
)

// NewsImageStore must be the news site's image client. Reference-ping is
// site-scoped, so a banner stored under any other client is invisible to the
// daily news-image-refping and is collected about thirteen months later.
type NewsImageStore interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
}

const newsBannerPreset = "news_banner"

func (c *Catalog) UploadNewsImage(ctx context.Context, filename string, body io.Reader) (repr.NewsImage, error) {
	if c == nil || c.NewsImages == nil {
		return repr.NewsImage{}, problem.New(problem.CodeServiceUnavailable, "", "", "the news image upload leg is not configured.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.NewsImage{}, err
	}
	if qerr := c.checkNewsImageQuota(ctx, uid); qerr != nil {
		return repr.NewsImage{}, qerr
	}
	res, uerr := c.NewsImages.UploadWithSub(ctx, body, filename, newsBannerPreset, strconv.FormatInt(uid, 10))
	if uerr != nil {
		return repr.NewsImage{}, imageUploadErr(uerr)
	}
	return newsImageFrom(res), nil
}

func (c *Catalog) checkNewsImageQuota(ctx context.Context, uid int64) error {
	if c.Counters == nil {
		return nil
	}
	limit := keys.CatalogNewsImageUploadsPerDay.Get()
	key := "v2:news-images:u" + strconv.FormatInt(uid, 10) + ":" + time.Now().UTC().Format("2006-01-02")
	used, err := c.Counters.Incr(ctx, key, 25*time.Hour)
	if err != nil {
		slog.Warn("news image upload counter unavailable, not enforcing the per-account limit", "err", err)
		return nil
	}
	if used > limit {
		return problem.New(problem.CodeQuotaExceeded, "", "",
			fmt.Sprintf("the news image upload limit for this account is exhausted: %d per UTC day.", limit))
	}
	return nil
}

func (c *Catalog) newsBannerErrors(ctx context.Context, pointer, value string) ([]problem.FieldError, error) {
	if errs := newsBannerFormatErrors(pointer, value); errs != nil || value == "" {
		return errs, nil
	}
	if c.NewsImages == nil {
		return nil, problem.New(problem.CodeServiceUnavailable, "", "", "the news image leg is not configured, so a banner cannot be verified.")
	}
	res, err := c.NewsImages.ReferencePing(ctx, []string{value})
	if err != nil {
		slog.Error("news banner check failed", "err", err)
		return nil, problem.New(problem.CodeServiceUnavailable, "", "", "the image service did not answer the banner check.")
	}
	if slices.Contains(res.NotFound, value) {
		return []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonUnknownReference,
			Detail: "no such image under the news site; upload the banner through POST /v2/me/news-images and send the hash it returns"}}, nil
	}
	return nil, nil
}

func newsImageFrom(res *imageclient.UploadResult) repr.NewsImage {
	out := repr.NewsImage{
		Object: "news_image", URL: res.URL, Hash: res.Hash,
		SizeBytes: res.SizeBytes, IsDeduplicated: res.Deduplicated,
	}
	if res.Width > 0 {
		w := res.Width
		out.Width = &w
	}
	if res.Height > 0 {
		h := res.Height
		out.Height = &h
	}
	if res.Thumbhash != "" {
		t := res.Thumbhash
		out.Thumbhash = &t
	}
	return out
}
