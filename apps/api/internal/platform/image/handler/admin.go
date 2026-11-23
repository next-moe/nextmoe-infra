package handler

import (
	stderrors "errors"
	"log/slog"
	"strconv"
	"time"

	"api/internal/platform/image/cdn"
	"api/internal/platform/image/model"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/service"
	"api/internal/platform/image/storage"
	errs "api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type AdminHandler struct {
	db        *gorm.DB
	svc       *service.Service
	statsRepo *repository.StatsRepository
	storage   *storage.Client
	purger    cdn.Purger
	bases     []string
}

// WithPurger makes a hard delete purge the edge too; a takedown needs both.
func (h *AdminHandler) WithPurger(p cdn.Purger, bases []string) *AdminHandler {
	h.purger, h.bases = p, bases
	return h
}

func NewAdmin(db *gorm.DB, svc *service.Service, statsRepo *repository.StatsRepository, s *storage.Client) *AdminHandler {
	return &AdminHandler{db: db, svc: svc, statsRepo: statsRepo, storage: s}
}

func (h *AdminHandler) List(c fiber.Ctx) error {
	q := h.db.WithContext(c.Context()).Model(&model.Image{})

	if site := c.Query("site"); site != "" {
		q = q.Where("hash IN (?)",
			h.db.Model(&model.ImageSiteUsage{}).Select("hash").Where("site = ?", site))
	}
	if status := c.Query("review_status"); status != "" {
		switch status {
		case "pending":
			q = q.Where("review_status = ?", model.ReviewPending)
		case "rejected":
			q = q.Where("review_status = ?", model.ReviewRejected)
		case "manual_review":
			q = q.Where("review_status = ?", model.ReviewManual)
		case "approved":
			q = q.Where("review_status = ?", model.ReviewApproved)
		default:
			return response.BadRequest(c, errs.ErrImageBadRequest)
		}
	}
	if sub := c.Query("uploader_sub"); sub != "" {
		q = q.Where("first_uploader_sub = ?", sub)
	}
	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return response.BadRequest(c, errs.ErrImageBadRequest)
		}
		q = q.Where("created_at >= ?", t)
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return response.BadRequest(c, errs.ErrImageBadRequest)
		}
		q = q.Where("created_at <= ?", t)
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		slog.Error("admin list count", "err", err)
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}

	var rows []model.Image
	if err := q.Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		slog.Error("admin list query", "err", err)
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}

	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, h.toAdminRow(&r))
	}
	return response.Success(c, fiber.Map{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AdminHandler) toAdminRow(r *model.Image) map[string]any {
	variantURLs := make(map[string]string, len(r.VariantList()))
	for _, v := range r.VariantList() {
		variantURLs[v] = h.svc.VariantURL(r.Hash, v, r.Ext)
	}
	return map[string]any{
		"hash":                  r.Hash,
		"url":                   h.svc.MainURL(r.Hash, r.Ext),
		"variant_urls":          variantURLs,
		"width":                 r.Width,
		"height":                r.Height,
		"size_bytes":            r.SizeBytes,
		"mime":                  r.MIME,
		"review_status":         reviewStatusLabel(r.ReviewStatus),
		"review_labels":         decodeReviewLabels(r.ReviewLabels),
		"first_uploader_sub":    r.FirstUploaderSub,
		"first_uploader_client": r.FirstUploaderClient,
		"created_at":            r.CreatedAt,
		"last_referenced_at":    r.LastReferencedAt,
		"deleted_at":            r.DeletedAt,
	}
}

type reviewRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h *AdminHandler) Review(c fiber.Ctx) error {
	hash := c.Params("hash")
	if len(hash) != 64 {
		return response.BadRequest(c, errs.ErrImageBadRequest)
	}
	var req reviewRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errs.ErrImageBadRequest)
	}

	var status int16
	switch req.Status {
	case "approved":
		status = model.ReviewApproved
	case "rejected":
		status = model.ReviewRejected
	case "manual_review":
		status = model.ReviewManual
	case "pending":
		status = model.ReviewPending
	default:
		return response.BadRequest(c, errs.ErrImageBadRequest)
	}

	now := time.Now()
	updates := map[string]any{
		"review_status": status,
		"reviewed_at":   now,
	}
	if req.Reason != "" {
		updates["review_labels"] = gorm.Expr(
			"jsonb_set(coalesce(review_labels, '{}'::jsonb), '{manual_reason}', to_jsonb(?::text))",
			req.Reason)
	}

	res := h.db.WithContext(c.Context()).
		Model(&model.Image{}).
		Where("hash = ?", hash).
		Updates(updates)
	if res.Error != nil {
		slog.Error("admin review update", "err", res.Error)
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}
	if res.RowsAffected == 0 {
		return response.NotFound(c, errs.ErrImageNotFound)
	}
	return response.Success(c, fiber.Map{"hash": hash, "status": req.Status})
}

func (h *AdminHandler) Delete(c fiber.Ctx) error {
	hash := c.Params("hash")
	if len(hash) != 64 {
		return response.BadRequest(c, errs.ErrImageBadRequest)
	}
	force := c.Query("force") == "true"

	var img model.Image
	if err := h.db.WithContext(c.Context()).
		Unscoped().
		Where("hash = ?", hash).
		First(&img).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, errs.ErrImageNotFound)
		}
		slog.Error("admin delete fetch", "err", err)
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}

	if !force {
		if err := h.db.WithContext(c.Context()).
			Model(&model.Image{}).
			Where("hash = ?", hash).
			Update("deleted_at", time.Now()).Error; err != nil {
			return response.InternalError(c, errs.ErrImageStoreFailed)
		}
		return response.Success(c, fiber.Map{"hash": hash, "soft_deleted": true})
	}

	if err := h.storage.Delete(c.Context(), img.StorageKey); err != nil {
		slog.Warn("hard-delete main s3", "hash", hash, "err", err)
	}
	keys := []string{img.StorageKey}
	for _, v := range img.VariantList() {
		key := img.Hash[:2] + "/" + img.Hash[2:4] + "/" + img.Hash + "_" + v + "." + img.Ext
		keys = append(keys, key)
		if err := h.storage.Delete(c.Context(), key); err != nil {
			slog.Warn("hard-delete variant s3", "hash", hash, "variant", v, "err", err)
		}
	}
	if h.purger != nil {
		if err := h.purger.Purge(c.Context(), cdn.URLs(h.bases, keys)); err != nil {
			slog.Error("hard-delete cdn purge", "hash", hash, "err", err)
			return response.InternalError(c, errs.ErrImageStoreFailed)
		}
	}
	if err := h.db.WithContext(c.Context()).Unscoped().
		Where("hash = ?", hash).Delete(&model.Image{}).Error; err != nil {
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}
	_ = h.db.WithContext(c.Context()).
		Where("hash = ?", hash).Delete(&model.ImageSiteUsage{}).Error

	return response.Success(c, fiber.Map{"hash": hash, "hard_deleted": true})
}

func (h *AdminHandler) Stats(c fiber.Ctx) error {
	res, err := h.statsRepo.Stats(c.Context(), repository.ScopeFilter{})
	if err != nil {
		slog.Error("admin stats", "err", err)
		return response.InternalError(c, errs.ErrImageStoreFailed)
	}
	return response.Success(c, res)
}
