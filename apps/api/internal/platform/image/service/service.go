package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"api/internal/platform/image/model"
	"api/internal/platform/image/moderation"
	"api/internal/platform/image/preset"
	"api/internal/platform/image/processor"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/storage"

	"github.com/gabriel-vasile/mimetype"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPresetNotFound = errors.New("service: preset not defined")
	ErrMIMENotAllowed = errors.New("service: mime not allowed by preset")
)

type UploadRequest struct {
	Body           []byte
	Preset         string
	Site           string
	UploaderSub    string
	UploaderClient string
	UploaderIP     string
	CDNBase        string
}

type UploadResult struct {
	Hash         string            `json:"hash"`
	URL          string            `json:"url"`
	VariantURLs  map[string]string `json:"variant_urls"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	Thumbhash    string            `json:"thumbhash,omitempty"`
	SizeBytes    int64             `json:"size_bytes"`
	Deduplicated bool              `json:"deduplicated"`
}

type Service struct {
	presets   *preset.Config
	storage   *storage.Client
	imgRepo   *repository.ImageRepository
	usageRepo *repository.SiteUsageRepository
	db        *gorm.DB
	mod       moderation.Provider
	syncTO    time.Duration
	cdnBase   string
}

type Options struct {
	Moderation  moderation.Provider
	SyncTimeout time.Duration
	DB          *gorm.DB
}

func New(
	presets *preset.Config,
	storage *storage.Client,
	imgRepo *repository.ImageRepository,
	usageRepo *repository.SiteUsageRepository,
	cdnBase string,
	opts ...Options,
) *Service {
	s := &Service{
		presets:   presets,
		storage:   storage,
		imgRepo:   imgRepo,
		usageRepo: usageRepo,
		cdnBase:   strings.TrimRight(cdnBase, "/"),
		mod:       moderation.NewNoop(),
		syncTO:    300 * time.Millisecond,
	}
	if len(opts) > 0 {
		o := opts[0]
		if o.Moderation != nil {
			s.mod = o.Moderation
		}
		if o.SyncTimeout > 0 {
			s.syncTO = o.SyncTimeout
		}
		if o.DB != nil {
			s.db = o.DB
		}
	}
	return s
}

var ErrModerationRejected = errors.New("service: rejected by moderation")

func (s *Service) Upload(ctx context.Context, req UploadRequest) (*UploadResult, error) {
	ps, ok := s.presets.Get(req.Preset)
	if !ok {
		return nil, ErrPresetNotFound
	}

	mt := mimetype.Detect(req.Body).String()
	if !ps.IsMIMEAllowed(mt) {
		return nil, fmt.Errorf("%w: %s", ErrMIMENotAllowed, mt)
	}

	sum := sha256.Sum256(req.Body)
	hash := hex.EncodeToString(sum[:])

	existing, err := s.imgRepo.FindByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("lookup hash: %w", err)
	}
	if existing != nil {
		result, err := s.handleExisting(ctx, existing, ps, req)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	if deleted, err := s.imgRepo.FindByHashIncludingDeleted(ctx, hash); err != nil {
		return nil, fmt.Errorf("lookup hash (incl deleted): %w", err)
	} else if deleted != nil {
		if err := s.imgRepo.Resurrect(ctx, hash); err != nil {
			return nil, fmt.Errorf("resurrect hash: %w", err)
		}
		deleted.DeletedAt = nil
		return s.handleExisting(ctx, deleted, ps, req)
	}

	syncCtx, cancel := context.WithTimeout(ctx, s.syncTO)
	defer cancel()
	syncDecision, syncErr := s.mod.SyncCheck(syncCtx, req.Body, mt)
	if syncErr != nil {
		slog.Warn("moderation sync errored; treating as undecided",
			"provider", s.mod.Name(), "err", syncErr)
	}
	if syncDecision != nil && syncDecision.Verdict == moderation.VerdictReject {
		return nil, fmt.Errorf("%w: %s", ErrModerationRejected, syncDecision.Reason)
	}

	return s.handleNew(ctx, hash, mt, ps, req, syncDecision)
}

func (s *Service) handleExisting(ctx context.Context, img *model.Image, ps preset.Preset, req UploadRequest) (*UploadResult, error) {
	haveVariants := img.VariantList()
	var missing []preset.VariantSpec
	for _, v := range ps.Variants {
		if !slices.Contains(haveVariants, v.Name) {
			missing = append(missing, v)
		}
	}

	if len(missing) > 0 {
		rc, err := s.storage.Get(ctx, img.StorageKey)
		if err != nil {
			return nil, fmt.Errorf("fetch main for variant backfill: %w", err)
		}
		mainBytes, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read main: %w", err)
		}
		srcImg, _, err := processor.DecodeFromBytes(mainBytes)
		if err != nil {
			return nil, fmt.Errorf("decode main for backfill: %w", err)
		}
		for _, v := range missing {
			out, err := processor.ProcessVariant(srcImg, v)
			if err != nil {
				return nil, fmt.Errorf("variant %s: %w", v.Name, err)
			}
			variantKey := variantStorageKey(img.Hash, out.VariantName, out.Ext)
			if err := s.storage.Put(ctx, variantKey, out.Data, out.MIME); err != nil {
				return nil, fmt.Errorf("store variant %s: %w", v.Name, err)
			}
			haveVariants = append(haveVariants, v.Name)
		}
		if err := s.imgRepo.UpdateVariants(ctx, img.Hash, haveVariants); err != nil {
			return nil, fmt.Errorf("persist variants: %w", err)
		}
	}

	if _, err := s.imgRepo.TouchReferenced(ctx, []string{img.Hash}); err != nil {
		return nil, fmt.Errorf("touch: %w", err)
	}
	if err := s.usageRepo.RecordUpload(ctx, img.Hash, req.Site, req.UploaderSub, req.UploaderClient); err != nil {
		return nil, fmt.Errorf("record usage: %w", err)
	}

	return s.buildResult(req.CDNBase, img.Hash, img.Ext, img.Thumbhash, img.Width, img.Height, img.SizeBytes, ps, true), nil
}

func (s *Service) handleNew(ctx context.Context, hash, originMIME string, ps preset.Preset, req UploadRequest, syncDec *moderation.Decision) (*UploadResult, error) {
	srcImg, _, err := processor.DecodeFromBytes(req.Body)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	mainOut, err := processor.ProcessMain(srcImg, s.presets.MainPipeline)
	if err != nil {
		return nil, fmt.Errorf("process main: %w", err)
	}

	mainKey := mainStorageKey(hash, mainOut.Ext)
	if err := s.storage.Put(ctx, mainKey, mainOut.Data, mainOut.MIME); err != nil {
		return nil, fmt.Errorf("store main: %w", err)
	}

	variantNames := make([]string, 0, len(ps.Variants))
	for _, v := range ps.Variants {
		out, err := processor.ProcessVariant(srcImg, v)
		if err != nil {
			return nil, fmt.Errorf("variant %s: %w", v.Name, err)
		}
		key := variantStorageKey(hash, out.VariantName, out.Ext)
		if err := s.storage.Put(ctx, key, out.Data, out.MIME); err != nil {
			return nil, fmt.Errorf("store variant %s: %w", v.Name, err)
		}
		variantNames = append(variantNames, v.Name)
	}

	reviewStatus := model.ReviewApproved
	enqueueAsync := false
	var reviewLabels datatypes.JSON
	if syncDec != nil {
		switch syncDec.Verdict {
		case moderation.VerdictApprove:
			reviewStatus = model.ReviewApproved
		case moderation.VerdictReview:
			reviewStatus = model.ReviewManual
		case moderation.VerdictUndecided:
			if s.db != nil && s.mod.Name() != "noop" {
				reviewStatus = model.ReviewPending
				enqueueAsync = true
			}
		}
		if syncDec.Labels != nil {
			if raw, err := json.Marshal(syncDec.Labels); err == nil {
				reviewLabels = datatypes.JSON(raw)
			}
		}
	}

	now := time.Now()
	img := &model.Image{
		Hash:                hash,
		StorageKey:          mainKey,
		MIME:                mainOut.MIME,
		Ext:                 mainOut.Ext,
		Width:               mainOut.Width,
		Height:              mainOut.Height,
		Thumbhash:           mainOut.Thumbhash,
		SizeBytes:           int64(len(mainOut.Data)),
		OriginMIME:          originMIME,
		OriginSize:          int64(len(req.Body)),
		ReviewStatus:        reviewStatus,
		ReviewLabels:        reviewLabels,
		FirstUploaderSub:    req.UploaderSub,
		FirstUploaderClient: req.UploaderClient,
		FirstUploaderIP:     req.UploaderIP,
	}
	if reviewStatus != model.ReviewPending {
		img.ReviewedAt = &now
	}
	img.SetVariants(variantNames)
	inserted, err := s.imgRepo.Create(ctx, img)
	if err != nil {
		return nil, fmt.Errorf("persist image: %w", err)
	}
	if !inserted {
		winner, err := s.imgRepo.FindByHash(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("lookup after conflict: %w", err)
		}
		if winner == nil {
			return nil, fmt.Errorf("persist image: conflict but row not found")
		}
		return s.handleExisting(ctx, winner, ps, req)
	}

	if err := s.usageRepo.RecordUpload(ctx, hash, req.Site, req.UploaderSub, req.UploaderClient); err != nil {
		return nil, fmt.Errorf("record usage: %w", err)
	}

	if enqueueAsync {
		qe := &model.ModerationQueue{Hash: hash, Site: req.Site}
		if err := s.db.WithContext(ctx).Create(qe).Error; err != nil {
			slog.Warn("enqueue moderation failed", "hash", hash, "err", err)
		}
	}

	return s.buildResult(req.CDNBase, hash, mainOut.Ext, mainOut.Thumbhash, mainOut.Width, mainOut.Height, int64(len(mainOut.Data)), ps, false), nil
}

func (s *Service) SoftDelete(ctx context.Context, hash, site string) (bool, error) {
	if s.db == nil {
		return false, errors.New("service: SoftDelete requires a DB handle")
	}
	deleted := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var img model.Image
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("hash = ?", hash).First(&img).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		res := tx.Where("hash = ? AND site = ?", hash, site).
			Delete(&model.ImageSiteUsage{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		deleted = true

		var remaining int64
		if err := tx.Model(&model.ImageSiteUsage{}).
			Where("hash = ?", hash).Count(&remaining).Error; err != nil {
			return err
		}
		if remaining > 0 {
			return nil
		}
		return tx.Model(&model.Image{}).
			Where("hash = ?", hash).
			Update("deleted_at", time.Now()).Error
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) buildResult(clientBase, hash, ext, thumbhash string, w, h int, size int64, ps preset.Preset, dedup bool) *UploadResult {
	base := s.resolveCDNBase(clientBase)
	variants := make(map[string]string, len(ps.Variants))
	for _, v := range ps.Variants {
		variants[v.Name] = joinURL(base, variantStorageKey(hash, v.Name, "webp"))
	}
	return &UploadResult{
		Hash:         hash,
		URL:          joinURL(base, mainStorageKey(hash, ext)),
		VariantURLs:  variants,
		Width:        w,
		Height:       h,
		Thumbhash:    thumbhash,
		SizeBytes:    size,
		Deduplicated: dedup,
	}
}

func (s *Service) resolveCDNBase(clientBase string) string {
	if b := strings.TrimRight(clientBase, "/"); b != "" {
		return b
	}
	return s.cdnBase
}

func (s *Service) MainURL(hash, ext string) string {
	return s.MainURLFor("", hash, ext)
}

func (s *Service) VariantURL(hash, variant, ext string) string {
	return s.VariantURLFor("", hash, variant, ext)
}

func (s *Service) MainURLFor(clientBase, hash, ext string) string {
	return joinURL(s.resolveCDNBase(clientBase), mainStorageKey(hash, ext))
}

func (s *Service) VariantURLFor(clientBase, hash, variant, ext string) string {
	return joinURL(s.resolveCDNBase(clientBase), variantStorageKey(hash, variant, ext))
}

func joinURL(base, key string) string {
	return fmt.Sprintf("%s/%s", base, key)
}

func mainStorageKey(hash, ext string) string {
	return fmt.Sprintf("%s/%s/%s.%s", hash[:2], hash[2:4], hash, ext)
}

func variantStorageKey(hash, variant, ext string) string {
	return fmt.Sprintf("%s/%s/%s_%s.%s", hash[:2], hash[2:4], hash, variant, ext)
}

func (s *Service) GetByHash(ctx context.Context, hash string) (*model.Image, []string, error) {
	img, err := s.imgRepo.FindByHash(ctx, hash)
	if err != nil || img == nil {
		return nil, nil, err
	}
	sites, err := s.usageRepo.SitesForHash(ctx, hash)
	if err != nil {
		return img, nil, err
	}
	return img, sites, nil
}

func (s *Service) MetaBatch(ctx context.Context, hashes []string) (map[string]repository.ImageMeta, error) {
	return s.imgRepo.MetaByHashes(ctx, hashes)
}

func (s *Service) ReferencePing(ctx context.Context, site string, hashes []string) (int64, []string, error) {
	if len(hashes) == 0 {
		return 0, nil, nil
	}
	existing, err := s.usageRepo.ExistingHashesForSite(ctx, site, hashes)
	if err != nil {
		return 0, nil, err
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, h := range existing {
		existingSet[h] = struct{}{}
	}
	notFound := make([]string, 0)
	for _, h := range hashes {
		if _, ok := existingSet[h]; !ok {
			notFound = append(notFound, h)
		}
	}
	if _, err := s.imgRepo.TouchReferenced(ctx, existing); err != nil {
		return 0, nil, err
	}
	return int64(len(existing)), notFound, nil
}

var _ = url.PathEscape
