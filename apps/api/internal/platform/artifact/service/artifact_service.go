package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/quota"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/storage"
	"api/internal/platform/settings/keys"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrTooBig       = stderrors.New("artifact: file exceeds per-site max size")
	ErrMIMEDenied   = stderrors.New("artifact: file type not allowed for this site")
	ErrQuotaCount   = stderrors.New("artifact: daily file-count quota exceeded")
	ErrQuotaBytes   = stderrors.New("artifact: daily byte quota exceeded")
	ErrNotFound     = stderrors.New("artifact: not found")
	ErrSizeMismatch = stderrors.New("artifact: uploaded size does not match declared size")
	ErrBadRequest   = stderrors.New("artifact: bad request")
	ErrNotResumable = stderrors.New("artifact: upload is not resumable (already completed or failed)")
)

const maxS3Parts = 10000

type Service struct {
	repo  *repository.ArtifactRepository
	store *storage.Client
	quota *quota.Checker
}

func New(repo *repository.ArtifactRepository, store *storage.Client, q *quota.Checker) *Service {
	return &Service{repo: repo, store: store, quota: q}
}

type InitParams struct {
	Site           string
	UploaderSub    string
	UploaderClient string
	MaxFileSize    int64
	QuotaCount     int
	QuotaBytes     int64
	AllowedMime    []string
}

func (s *Service) InitUpload(ctx context.Context, req dto.InitUploadRequest, p InitParams) (*dto.InitUploadResponse, error) {
	if p.MaxFileSize > 0 && req.FileSize > p.MaxFileSize {
		return nil, ErrTooBig
	}
	if !mimeAllowed(req.MimeType, req.Name, p.AllowedMime) {
		return nil, ErrMIMEDenied
	}

	if s.quota != nil {
		if _, qerr := s.quota.Reserve(ctx, p.Site, req.FileSize, p.QuotaCount, p.QuotaBytes); qerr != nil {
			switch {
			case stderrors.Is(qerr, quota.ErrCountExceeded):
				return nil, ErrQuotaCount
			case stderrors.Is(qerr, quota.ErrBytesExceeded):
				return nil, ErrQuotaBytes
			case stderrors.Is(qerr, quota.ErrNotConfigured):
			default:
				return nil, qerr
			}
		}
	}

	id := uuid.NewString()
	fileKey := p.Site + "/" + id + extForKey(req.Name)

	a := &model.Artifact{
		UUID:           id,
		SiteKey:        p.Site,
		UploaderSub:    p.UploaderSub,
		UploaderClient: p.UploaderClient,
		Name:           req.Name,
		Description:    req.Description,
		FileKey:        fileKey,
		ReportedSize:   req.FileSize,
		MimeType:       req.MimeType,
		Checksum:       req.Checksum,
		Status:         model.StatusUploading,
		Public:         req.Public,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, err
	}

	uploadTTL := time.Duration(keys.ArtifactPresignUploadTTLSeconds.Get()) * time.Second
	partSize := keys.ArtifactPartSizeBytes.Get()
	threshold := keys.ArtifactMultipartThresholdBytes.Get()

	expiresAt := time.Now().Add(uploadTTL).UTC().Format(time.RFC3339)
	resp := &dto.InitUploadResponse{UUID: id, ExpiresAt: expiresAt}

	if req.FileSize >= threshold {
		numParts := (req.FileSize + partSize - 1) / partSize
		if numParts > maxS3Parts {
			return nil, ErrTooBig
		}
		uploadID, err := s.store.CreateMultipart(ctx, fileKey, a.Name, a.MimeType)
		if err != nil {
			return nil, err
		}
		a.UploadID = uploadID
		a.PartSize = partSize
		if err := s.repo.Update(ctx, a); err != nil {
			return nil, err
		}
		parts := make([]dto.PartURL, 0, numParts)
		for n := int32(1); int64(n) <= numParts; n++ {
			url, err := s.store.PresignUploadPart(ctx, fileKey, uploadID, n, uploadTTL)
			if err != nil {
				return nil, err
			}
			parts = append(parts, dto.PartURL{PartNumber: n, URL: url})
		}
		resp.Multipart = true
		resp.UploadID = uploadID
		resp.PartSize = partSize
		resp.PartURLs = parts
		return resp, nil
	}

	url, err := s.store.PresignPut(ctx, fileKey, uploadTTL)
	if err != nil {
		return nil, err
	}
	resp.UploadURL = url
	return resp, nil
}

func (s *Service) ResumeUpload(ctx context.Context, uuidStr, site, owner string) (*dto.ResumeUploadResponse, error) {
	a, err := s.repo.FindByUUID(ctx, uuidStr)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.SiteKey != site || !uploadedBy(a, owner) {
		return nil, ErrNotFound
	}
	if a.Status != model.StatusUploading {
		return nil, ErrNotResumable
	}

	if err := s.repo.Touch(ctx, a.ID); err != nil {
		slog.Warn("artifact: touch on resume", "uuid", a.UUID, "err", err)
	}

	uploadTTL := time.Duration(keys.ArtifactPresignUploadTTLSeconds.Get()) * time.Second
	expiresAt := time.Now().Add(uploadTTL).UTC().Format(time.RFC3339)
	resp := &dto.ResumeUploadResponse{UUID: a.UUID, ExpiresAt: expiresAt}

	if !a.IsMultipart() {
		url, err := s.store.PresignPut(ctx, a.FileKey, uploadTTL)
		if err != nil {
			return nil, err
		}
		resp.UploadURL = url
		return resp, nil
	}

	if a.PartSize <= 0 {
		return nil, ErrBadRequest
	}

	uploaded, err := s.store.ListParts(ctx, a.FileKey, a.UploadID)
	if err != nil {
		return nil, err
	}
	have := make(map[int32]struct{}, len(uploaded))
	for _, p := range uploaded {
		have[p.PartNumber] = struct{}{}
		resp.UploadedParts = append(resp.UploadedParts, dto.UploadedPart{
			PartNumber: p.PartNumber, ETag: p.ETag, Size: p.Size,
		})
	}

	numParts := (a.ReportedSize + a.PartSize - 1) / a.PartSize
	resp.Multipart = true
	resp.PartSize = a.PartSize
	for n := int32(1); int64(n) <= numParts; n++ {
		if _, ok := have[n]; ok {
			continue
		}
		url, err := s.store.PresignUploadPart(ctx, a.FileKey, a.UploadID, n, uploadTTL)
		if err != nil {
			return nil, err
		}
		resp.PartURLs = append(resp.PartURLs, dto.PartURL{PartNumber: n, URL: url})
	}
	return resp, nil
}

func (s *Service) CompleteUpload(ctx context.Context, uuidStr, site, owner string, req dto.CompleteUploadRequest) (*dto.ArtifactResponse, error) {
	a, err := s.repo.FindByUUID(ctx, uuidStr)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.SiteKey != site || !uploadedBy(a, owner) {
		return nil, ErrNotFound
	}
	if a.IsReady() {
		return s.persistManifestAndRespond(ctx, a, req.Manifest)
	}

	if a.IsMultipart() {
		if len(req.Parts) == 0 {
			return nil, ErrBadRequest
		}
		parts := make([]storage.CompletedPart, 0, len(req.Parts))
		for _, p := range req.Parts {
			parts = append(parts, storage.CompletedPart{PartNumber: p.PartNumber, ETag: p.ETag})
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
		if err := s.store.CompleteMultipart(ctx, a.FileKey, a.UploadID, parts); err != nil {
			if abErr := s.store.AbortMultipart(ctx, a.FileKey, a.UploadID); abErr != nil {
				slog.Warn("artifact: abort multipart on complete-failure", "uuid", a.UUID, "err", abErr)
			}
			s.markFailed(ctx, a)
			return nil, err
		}
	}

	actual, err := s.store.HeadSize(ctx, a.FileKey)
	if err != nil {
		s.markFailed(ctx, a)
		return nil, err
	}
	if actual != a.ReportedSize {
		if delErr := s.store.Delete(ctx, a.FileKey); delErr != nil {
			slog.Warn("artifact: delete on size mismatch failed", "uuid", a.UUID, "err", delErr)
		}
		s.markFailed(ctx, a)
		return nil, ErrSizeMismatch
	}

	if !a.IsMultipart() {
		if err := s.store.SetContentDisposition(ctx, a.FileKey, a.Name, a.MimeType); err != nil {
			slog.Warn("artifact: bake content-disposition", "uuid", a.UUID, "err", err)
		}
	}

	a.FileSize = actual
	a.Status = model.StatusReady
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, err
	}
	return s.persistManifestAndRespond(ctx, a, req.Manifest)
}

func (s *Service) Download(ctx context.Context, uuidStr, site, cdnBase string) (*dto.DownloadResponse, error) {
	a, err := s.repo.FindByUUID(ctx, uuidStr)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.SiteKey != site || !a.IsReady() {
		return nil, ErrNotFound
	}

	if a.Public && cdnBase != "" {
		return &dto.DownloadResponse{URL: strings.TrimRight(cdnBase, "/") + "/" + a.FileKey}, nil
	}

	downloadTTL := time.Duration(keys.ArtifactPresignDownloadTTLSeconds.Get()) * time.Second
	url, err := s.store.PresignGet(ctx, a.FileKey, a.Name, downloadTTL)
	if err != nil {
		return nil, err
	}
	return &dto.DownloadResponse{
		URL:       url,
		ExpiresAt: time.Now().Add(downloadTTL).UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) Delete(ctx context.Context, uuidStr, site string) (bool, error) {
	return s.repo.SoftDeleteByUUID(ctx, uuidStr, site)
}

func (s *Service) Get(ctx context.Context, uuidStr, site string) (*dto.ArtifactResponse, error) {
	a, err := s.repo.FindByUUID(ctx, uuidStr)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.SiteKey != site {
		return nil, ErrNotFound
	}
	return toResponse(a), nil
}

func (s *Service) List(ctx context.Context, site string, offset, limit int) ([]dto.ArtifactResponse, int64, error) {
	items, total, err := s.repo.ListBySite(ctx, site, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.ArtifactResponse, 0, len(items))
	for i := range items {
		out = append(out, *toResponse(&items[i]))
	}
	return out, total, nil
}

func (s *Service) markFailed(ctx context.Context, a *model.Artifact) {
	a.Status = model.StatusFailed
	if err := s.repo.Update(ctx, a); err != nil {
		slog.Error("artifact: mark failed", "uuid", a.UUID, "err", err)
	}
}

func (s *Service) persistManifestAndRespond(ctx context.Context, a *model.Artifact, mi *dto.ManifestInput) (*dto.ArtifactResponse, error) {
	if mi != nil {
		m := &model.Manifest{
			ArtifactID: a.ID,
			Executable: mi.Executable,
			Arguments:  mi.Arguments,
			WorkingDir: mi.WorkingDir,
			SavePath:   mi.SavePath,
		}
		if mi.Requirements != nil {
			if raw, err := json.Marshal(mi.Requirements); err == nil {
				m.Requirements = datatypes.JSON(raw)
			}
		}
		if err := s.repo.SaveManifest(ctx, m); err != nil {
			return nil, err
		}
	}
	return toResponse(a), nil
}

func uploadedBy(a *model.Artifact, owner string) bool {
	return owner == "" || a.UploaderSub == owner
}

func toResponse(a *model.Artifact) *dto.ArtifactResponse {
	return &dto.ArtifactResponse{
		UUID:        a.UUID,
		SiteKey:     a.SiteKey,
		Name:        a.Name,
		Description: a.Description,
		FileSize:    a.FileSize,
		MimeType:    a.MimeType,
		Checksum:    a.Checksum,
		Status:      a.Status,
		Public:      a.Public,
		CreatedAt:   a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func extForKey(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) < 2 {
		return ""
	}
	for _, r := range ext[1:] {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return ext
}

func mimeAllowed(mime, name string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if mime != "" && a == strings.ToLower(mime) {
			return true
		}
		if ext != "" && (a == ext || a == strings.TrimPrefix(ext, ".")) {
			return true
		}
	}
	return false
}
