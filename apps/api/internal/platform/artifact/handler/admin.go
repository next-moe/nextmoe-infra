package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/service"
	"api/internal/platform/artifact/storage"
	"api/internal/platform/settings/keys"

	"gorm.io/gorm"
)

type AdminHandler struct {
	db         *gorm.DB
	statsRepo  *repository.StatsRepository
	store      *storage.Client
	composites *service.CompositeService
}

func NewAdmin(db *gorm.DB, statsRepo *repository.StatsRepository, store *storage.Client) *AdminHandler {
	var cleanup service.ObjectStore
	if store != nil {
		cleanup = store
	}
	return &AdminHandler{
		db:         db,
		statsRepo:  statsRepo,
		store:      store,
		composites: service.NewCompositeService(repository.NewCompositeRepository(db), nil, cleanup),
	}
}

var (
	errAdminNotFound       = errors.New("artifact not found")
	errAdminBadRequest     = errors.New("bad request")
	errReclaimUnavailable  = errors.New("artifact storage not configured; reclaim unavailable")
	errReclaimNotUploading = errors.New("only in-progress (uploading) artifacts can be reclaimed")
	errReclaimActive       = errors.New("upload still active; refusing to interrupt")
	errReclaimRaced        = errors.New("upload changed state; not reclaimed")
)

var statusLabels = map[int]string{
	model.StatusUploading: "uploading",
	model.StatusReady:     "ready",
	model.StatusFailed:    "failed",
}

type AdminListFilters struct {
	Site        string
	Status      string
	UploaderSub string
	Search      string
	From        *time.Time
	To          *time.Time
	Page        int
	Limit       int
}

func (h *AdminHandler) List(ctx context.Context, f AdminListFilters) (dto.AdminArtifactList, error) {
	q := h.db.WithContext(ctx).Model(&model.Artifact{})

	if f.Site != "" {
		q = q.Where("site_key = ?", f.Site)
	}
	switch f.Status {
	case "":
	case "uploading":
		q = q.Where("status = ?", model.StatusUploading)
	case "ready":
		q = q.Where("status = ?", model.StatusReady)
	case "failed":
		q = q.Where("status = ?", model.StatusFailed)
	default:
		return dto.AdminArtifactList{}, errAdminBadRequest
	}
	if f.UploaderSub != "" {
		q = q.Where("uploader_sub = ?", f.UploaderSub)
	}
	if f.Search != "" {
		q = q.Where("name ILIKE ? OR uuid::text = ?", "%"+f.Search+"%", f.Search)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at <= ?", *f.To)
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	limit := f.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return dto.AdminArtifactList{}, fmt.Errorf("count artifacts: %w", err)
	}

	var rows []model.Artifact
	if err := q.Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return dto.AdminArtifactList{}, fmt.Errorf("query artifacts: %w", err)
	}

	items := make([]dto.AdminArtifactRow, 0, len(rows))
	for i := range rows {
		items = append(items, toAdminRow(&rows[i]))
	}
	return dto.AdminArtifactList{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func toAdminRow(a *model.Artifact) dto.AdminArtifactRow {
	return dto.AdminArtifactRow{
		UUID:           a.UUID,
		Name:           a.Name,
		FileKey:        a.FileKey,
		FileSize:       a.FileSize,
		MimeType:       a.MimeType,
		SiteKey:        a.SiteKey,
		Status:         statusLabels[a.Status],
		Public:         a.Public,
		UploaderSub:    a.UploaderSub,
		UploaderClient: a.UploaderClient,
		Checksum:       a.Checksum,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
}

func (h *AdminHandler) Stats(ctx context.Context) (dto.AdminArtifactStats, error) {
	res, err := h.statsRepo.Stats(ctx)
	if err != nil {
		return dto.AdminArtifactStats{}, err
	}
	out := dto.AdminArtifactStats{
		TotalCount:  res.TotalCount,
		TotalBytes:  res.TotalBytes,
		Uploading:   res.Uploading,
		Failed:      res.Failed,
		SoftDeleted: res.SoftDeleted,
	}
	if len(res.BySite) > 0 {
		out.BySite = make(map[string]dto.AdminArtifactSiteStats, len(res.BySite))
		for k, v := range res.BySite {
			out.BySite[k] = dto.AdminArtifactSiteStats{Count: v.Count, Bytes: v.Bytes}
		}
	}
	return out, nil
}

// Delete is the console's takedown, so it also deletes every composite that
// serves the artifact's bytes: a composite pins its sources against artifact-gc,
// and left alone it would keep a taken-down artifact downloadable for good —
// including one the site already soft-deleted, which is why the lookup is
// unscoped.
func (h *AdminHandler) Delete(ctx context.Context, uuid string) (int, error) {
	if uuid == "" {
		return 0, errAdminBadRequest
	}
	var a model.Artifact
	if err := h.db.WithContext(ctx).Unscoped().Where("uuid = ?", uuid).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errAdminNotFound
		}
		return 0, fmt.Errorf("load artifact: %w", err)
	}
	deleted, removed, err := h.composites.TakeDown(ctx, &a, "")
	if err != nil {
		return removed, fmt.Errorf("take down artifact: %w", err)
	}
	if !deleted && removed == 0 {
		return 0, errAdminNotFound
	}
	return removed, nil
}

func (h *AdminHandler) Reclaim(ctx context.Context, uuid string) error {
	if h.store == nil {
		return errReclaimUnavailable
	}
	if uuid == "" {
		return errAdminBadRequest
	}

	var a model.Artifact
	if err := h.db.WithContext(ctx).Where("uuid = ?", uuid).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errAdminNotFound
		}
		return fmt.Errorf("reclaim load: %w", err)
	}
	if a.Status != model.StatusUploading {
		return errReclaimNotUploading
	}
	minIdle := time.Duration(keys.ArtifactReclaimMinIdleSeconds.Get()) * time.Second
	if idle := time.Since(a.UpdatedAt); idle < minIdle {
		return fmt.Errorf("%w (idle %s < min %s)", errReclaimActive, idle.Round(time.Second), minIdle)
	}

	claim := h.db.WithContext(ctx).Unscoped().
		Where("uuid = ? AND status = ?", uuid, model.StatusUploading).
		Delete(&model.Artifact{})
	if claim.Error != nil {
		return fmt.Errorf("reclaim claim: %w", claim.Error)
	}
	if claim.RowsAffected == 0 {
		return errReclaimRaced
	}

	if a.UploadID != "" {
		if err := h.store.AbortMultipart(ctx, a.FileKey, a.UploadID); err != nil {
			slog.Warn("artifact reclaim: abort multipart", "uuid", uuid, "key", a.FileKey, "err", err)
		}
	}
	if err := h.store.Delete(ctx, a.FileKey); err != nil {
		slog.Warn("artifact reclaim: delete object", "uuid", uuid, "key", a.FileKey, "err", err)
	}
	return nil
}
