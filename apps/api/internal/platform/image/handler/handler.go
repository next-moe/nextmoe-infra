package handler

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"time"

	"api/internal/platform/image/metrics"
	imgMW "api/internal/platform/image/middleware"
	"api/internal/platform/image/processor"
	"api/internal/platform/image/quota"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/service"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	svc       *service.Service
	quota     *quota.Checker
	statsRepo *repository.StatsRepository
}

func New(svc *service.Service, q *quota.Checker, statsRepo *repository.StatsRepository) *Handler {
	return &Handler{svc: svc, quota: q, statsRepo: statsRepo}
}

func (h *Handler) Upload(c fiber.Ctx) error {
	client := imgMW.ClientFromCtx(c)
	site := imgMW.SiteKeyFromCtx(c)
	if client == nil || site == "" {
		metrics.UploadTotal.WithLabelValues("", "", "unauthorized").Inc()
		return response.Unauthorized(c, errors.ErrImageUnauthorized)
	}
	start := time.Now()

	presetName := c.FormValue("preset")
	if presetName == "" {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}
	if !client.IsPresetAllowed(presetName) {
		return response.Forbidden(c, errors.ErrImagePresetDenied)
	}

	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}

	if client.ImageMaxFileSize > 0 && fileHeader.Size > client.ImageMaxFileSize {
		return response.Error(c, fiber.StatusRequestEntityTooLarge, errors.ErrImageFileTooLarge, errors.GetMessage(errors.ErrImageFileTooLarge))
	}

	fh, err := fileHeader.Open()
	if err != nil {
		return response.InternalError(c, errors.ErrImageBadRequest)
	}
	defer fh.Close()
	body, err := io.ReadAll(fh)
	if err != nil {
		return response.InternalError(c, errors.ErrImageBadRequest)
	}

	if h.quota != nil {
		usage, qerr := h.quota.Reserve(c.Context(), site, int64(len(body)), client.ImageQuotaDaily, client.ImageQuotaBytesDaily)
		if qerr != nil {
			if stderrors.Is(qerr, quota.ErrCountExceeded) || stderrors.Is(qerr, quota.ErrBytesExceeded) {
				metrics.UploadTotal.WithLabelValues(site, presetName, "rejected_quota").Inc()
				details := fiber.Map{
					"quota_count": usage.LimitCount,
					"quota_bytes": usage.LimitBytes,
					"used_count":  usage.Count,
					"used_bytes":  usage.Bytes,
					"reset_at":    usage.ResetAt,
				}
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"code":    errors.ErrImageQuotaExceeded,
					"message": errors.GetMessage(errors.ErrImageQuotaExceeded),
					"details": details,
				})
			}
			if !stderrors.Is(qerr, quota.ErrNotConfigured) {
				slog.Error("quota reserve", "err", qerr)
				return response.InternalError(c, errors.ErrImageStoreFailed)
			}
		}
	}

	uploaderSub := imgMW.UserSubFromCtx(c)
	if uploaderSub == "" {
		uploaderSub = c.FormValue("uploader_sub")
	}
	req := service.UploadRequest{
		Body:           body,
		Preset:         presetName,
		Site:           site,
		UploaderSub:    uploaderSub,
		UploaderClient: client.ID,
		UploaderIP:     c.IP(),
		CDNBase:        client.ImageCDNBase,
	}
	result, err := h.svc.Upload(c.Context(), req)
	if err != nil {
		return respondUploadError(c, site, presetName, client.ID, err)
	}

	resultLabel := "success"
	if result.Deduplicated {
		resultLabel = "dedup"
		metrics.DedupHits.WithLabelValues(site).Inc()
	}
	metrics.UploadTotal.WithLabelValues(site, presetName, resultLabel).Inc()
	metrics.UploadDuration.WithLabelValues(site, presetName).Observe(time.Since(start).Seconds())

	return response.Success(c, result)
}

func respondUploadError(c fiber.Ctx, site, presetName, clientID string, err error) error {
	var resultLabel string
	switch {
	case stderrors.Is(err, service.ErrPresetNotFound):
		resultLabel = "preset_not_found"
		metrics.UploadTotal.WithLabelValues(site, presetName, resultLabel).Inc()
		return response.BadRequest(c, errors.ErrImagePresetNotFound)
	case stderrors.Is(err, service.ErrMIMENotAllowed):
		resultLabel = "mime_denied"
		metrics.UploadTotal.WithLabelValues(site, presetName, resultLabel).Inc()
		return response.BadRequest(c, errors.ErrImageMIMEDenied)
	case stderrors.Is(err, service.ErrModerationRejected):
		resultLabel = "rejected_moderation"
		metrics.UploadTotal.WithLabelValues(site, presetName, resultLabel).Inc()
		return response.Error(c, fiber.StatusUnprocessableEntity, errors.ErrModerationRejected, errors.GetMessage(errors.ErrModerationRejected))
	case stderrors.Is(err, processor.ErrInvalidInput):
		resultLabel = "decode_failed"
		metrics.UploadTotal.WithLabelValues(site, presetName, resultLabel).Inc()
		return response.BadRequest(c, errors.ErrImageDecodeFailed)
	default:
		metrics.UploadTotal.WithLabelValues(site, presetName, "error").Inc()
		slog.Error("image upload failed",
			"site", site, "client_id", clientID, "preset", presetName, "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
}

func (h *Handler) SoftDelete(c fiber.Ctx) error {
	site := imgMW.SiteKeyFromCtx(c)
	if site == "" {
		return response.Unauthorized(c, errors.ErrAuthUnauthorized)
	}
	hash := c.Params("hash")
	if len(hash) != 64 {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}

	ok, err := h.svc.SoftDelete(c.Context(), hash, site)
	if err != nil {
		slog.Error("image soft-delete", "hash", hash, "site", site, "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
	if !ok {
		return response.NotFound(c, errors.ErrImageNotFound)
	}
	return response.Success(c, fiber.Map{"hash": hash, "soft_deleted": true})
}

func (h *Handler) Meta(c fiber.Ctx) error {
	hash := c.Params("hash")
	if len(hash) != 64 {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}

	img, sites, err := h.svc.GetByHash(c.Context(), hash)
	if err != nil {
		slog.Error("meta lookup failed", "hash", hash, "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
	if img == nil {
		return response.NotFound(c, errors.ErrImageNotFound)
	}

	callerSite := imgMW.SiteKeyFromCtx(c)
	scoped := sites[:0]
	for _, s := range sites {
		if s == callerSite {
			scoped = append(scoped, s)
		}
	}
	sites = scoped

	clientBase := ""
	if cl := imgMW.ClientFromCtx(c); cl != nil {
		clientBase = cl.ImageCDNBase
	}
	variantURLs := make(map[string]string, len(img.VariantList()))
	for _, v := range img.VariantList() {
		variantURLs[v] = h.svc.VariantURLFor(clientBase, img.Hash, v, img.Ext)
	}

	return response.Success(c, fiber.Map{
		"hash":               img.Hash,
		"url":                h.svc.MainURLFor(clientBase, img.Hash, img.Ext),
		"variant_urls":       variantURLs,
		"width":              img.Width,
		"height":             img.Height,
		"thumbhash":          img.Thumbhash,
		"size_bytes":         img.SizeBytes,
		"mime":               img.MIME,
		"review_status":      reviewStatusLabel(img.ReviewStatus),
		"review_labels":      decodeReviewLabels(img.ReviewLabels),
		"created_at":         img.CreatedAt,
		"last_referenced_at": img.LastReferencedAt,
		"sites":              sites,
	})
}

type metaBatchRequest struct {
	Hashes []string `json:"hashes"`
}

func (h *Handler) MetaBatch(c fiber.Ctx) error {
	var req metaBatchRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}
	if len(req.Hashes) > 1000 {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}
	cleaned := make([]string, 0, len(req.Hashes))
	for _, hh := range req.Hashes {
		if len(hh) == 64 {
			cleaned = append(cleaned, hh)
		}
	}
	metas, err := h.svc.MetaBatch(c.Context(), cleaned)
	if err != nil {
		slog.Error("meta-batch lookup failed", "count", len(cleaned), "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
	return response.Success(c, fiber.Map{"metas": metas})
}

type referencePingRequest struct {
	Hashes []string `json:"hashes"`
}

func (h *Handler) Ping(c fiber.Ctx) error {
	var req referencePingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}
	if len(req.Hashes) == 0 {
		return response.Success(c, fiber.Map{"updated": 0, "not_found": []string{}})
	}
	if len(req.Hashes) > 1000 {
		return response.BadRequest(c, errors.ErrImageBadRequest)
	}
	cleaned := make([]string, 0, len(req.Hashes))
	for _, h := range req.Hashes {
		if len(h) == 64 {
			cleaned = append(cleaned, h)
		}
	}

	updated, notFound, err := h.svc.ReferencePing(c.Context(), imgMW.SiteKeyFromCtx(c), cleaned)
	if err != nil {
		slog.Error("reference-ping failed", "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
	return response.Success(c, fiber.Map{
		"updated":   updated,
		"not_found": notFound,
	})
}

func (h *Handler) Stats(c fiber.Ctx) error {
	site := imgMW.SiteKeyFromCtx(c)
	if site == "" {
		return response.Unauthorized(c, errors.ErrImageUnauthorized)
	}
	res, err := h.statsRepo.Stats(c.Context(), repository.ScopeFilter{Site: site})
	if err != nil {
		slog.Error("stats failed", "site", site, "err", err)
		return response.InternalError(c, errors.ErrImageStoreFailed)
	}
	return response.Success(c, res)
}

func reviewStatusLabel(s int16) string {
	switch s {
	case 0:
		return "pending"
	case 1:
		return "approved"
	case 2:
		return "rejected"
	case 3:
		return "manual_review"
	default:
		return "unknown"
	}
}

func decodeReviewLabels(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
