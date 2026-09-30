package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"api/internal/platform/telemetry/dto"
	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/symbols"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) LookupBySymbolsTokenHash(ctx context.Context, hash string) (*symbols.UploadApp, error) {
	var row model.App
	err := s.db.WithContext(ctx).Where("symbols_token_hash = ?", hash).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &symbols.UploadApp{ID: row.ID, ServiceName: row.ServiceName, Enabled: row.Enabled}, nil
}

func (s *Store) RotateSymbolsToken(ctx context.Context, id int64) (string, error) {
	var row model.App
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	tok, err := randomSymbolsToken()
	if err != nil {
		return "", err
	}
	hash := symbols.HashToken(tok)
	if err := s.db.WithContext(ctx).Model(&row).Update("symbols_token_hash", hash).Error; err != nil {
		return "", err
	}
	return tok, nil
}

func randomSymbolsToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Store) AppByID(ctx context.Context, id int64) (*model.App, error) {
	var row model.App
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) IngestSymbolUpload(ctx context.Context, appID int64, version, engineRev string, files []symbols.IncomingFile) (int64, bool, error) {
	id, known, err := s.knownSymbolUpload(ctx, appID, version, engineRev, files)
	if err != nil {
		return 0, false, err
	}
	if known {
		return id, false, nil
	}
	id, err = s.persistSymbolUpload(ctx, appID, version, engineRev, files)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *Store) knownSymbolUpload(ctx context.Context, appID int64, version, engineRev string, files []symbols.IncomingFile) (int64, bool, error) {
	for _, f := range files {
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.SymbolFile{}).
			Where("app_id = ? AND service_version = ? AND file_name = ? AND sha256 = ?",
				appID, version, f.FileName, f.SHA256).
			Count(&n).Error; err != nil {
			return 0, false, err
		}
		if n == 0 {
			return 0, false, nil
		}
	}
	q := s.db.WithContext(ctx).Model(&model.SymbolUpload{}).
		Where("app_id = ? AND service_version = ?", appID, version)
	if engineRev != "" {
		var n int64
		if err := q.Where("engine_revision = ?", engineRev).Count(&n).Error; err != nil {
			return 0, false, err
		}
		if n == 0 {
			return 0, false, nil
		}
	}
	var up model.SymbolUpload
	take := s.db.WithContext(ctx).Where("app_id = ? AND service_version = ?", appID, version)
	if engineRev != "" {
		take = take.Where("engine_revision = ?", engineRev)
	}
	if err := take.Order("id DESC").Take(&up).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return up.ID, true, nil
}

func (s *Store) persistSymbolUpload(ctx context.Context, appID int64, version, engineRev string, files []symbols.IncomingFile) (int64, error) {
	if s.blobs == nil {
		return 0, fmt.Errorf("blob store not configured")
	}
	for _, f := range files {
		if err := s.putBlobIfAbsent(ctx, f.SHA256, f.Size, f.Path); err != nil {
			return 0, err
		}
	}
	var uploadID int64
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		up := model.SymbolUpload{
			AppID:          appID,
			ServiceVersion: version,
			EngineRevision: engineRev,
			CreatedAt:      now,
		}
		if err := tx.Create(&up).Error; err != nil {
			return err
		}
		uploadID = up.ID
		for _, f := range files {
			row := model.SymbolFile{
				UploadID:       up.ID,
				AppID:          appID,
				ServiceVersion: version,
				FileName:       f.FileName,
				Kind:           f.Kind,
				Arch:           f.Arch,
				BuildID:        f.BuildID,
				SHA256:         f.SHA256,
				Size:           f.Size,
				CreatedAt:      now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		if engineRev == "" {
			return nil
		}
		for _, variant := range []string{symbols.VariantARM64Release, symbols.VariantARMRelease} {
			if err := tx.Exec(`
				INSERT INTO telemetry_engine_symbol
					(engine_revision, variant, status, build_id, sha256, size, attempts, next_attempt_at, last_error, updated_at)
				VALUES (?, ?, ?, '', '', 0, 0, ?, '', ?)
				ON CONFLICT (engine_revision, variant) DO NOTHING`,
				engineRev, variant, symbols.StatusPending, now, now).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return uploadID, err
}

func (s *Store) putBlobIfAbsent(ctx context.Context, sha string, size int64, path string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.Blob{}).Where("sha256 = ?", sha).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := s.blobs.Put(ctx, symbols.BlobKey(sha), f); err != nil {
		return err
	}
	row := model.Blob{SHA256: sha, Size: size, CreatedAt: time.Now().UTC()}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (s *Store) EnsureBlobRow(ctx context.Context, sha256 string, size int64) error {
	row := model.Blob{SHA256: sha256, Size: size, CreatedAt: time.Now().UTC()}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (s *Store) DartSymbolsByBuildID(ctx context.Context, appID int64, buildID string) (*model.SymbolFile, error) {
	var row model.SymbolFile
	err := s.db.WithContext(ctx).
		Where("app_id = ? AND kind = ? AND build_id = ?", appID, symbols.KindDartSymbols, buildID).
		Order("id DESC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ObfuscationMapForUpload(ctx context.Context, uploadID int64) (*model.SymbolFile, error) {
	var row model.SymbolFile
	err := s.db.WithContext(ctx).
		Where("upload_id = ? AND kind = ?", uploadID, symbols.KindDartObfuscationMap).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) R8MappingForVersion(ctx context.Context, appID int64, version string) (*model.SymbolFile, error) {
	var row model.SymbolFile
	err := s.db.WithContext(ctx).
		Where("app_id = ? AND service_version = ? AND kind = ?", appID, version, symbols.KindR8Mapping).
		Order("id DESC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) EngineSymbolByBuildID(ctx context.Context, buildID string) (*model.EngineSymbol, error) {
	var row model.EngineSymbol
	err := s.db.WithContext(ctx).
		Where("build_id = ? AND status = ?", buildID, symbols.StatusReady).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) OpenBlob(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if s.blobs == nil {
		return nil, fmt.Errorf("blob store not configured")
	}
	rc, err := s.blobs.Open(ctx, symbols.BlobKey(sha256))
	if err != nil {
		return nil, err
	}
	return rc, nil
}

func (s *Store) NextPendingEngine(ctx context.Context, now time.Time) (*model.EngineSymbol, error) {
	var row model.EngineSymbol
	err := s.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", symbols.StatusPending, now).
		Order("next_attempt_at ASC, engine_revision ASC, variant ASC").
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) SaveEngineSymbol(ctx context.Context, row *model.EngineSymbol) error {
	return s.db.WithContext(ctx).Model(&model.EngineSymbol{}).
		Where("engine_revision = ? AND variant = ?", row.EngineRevision, row.Variant).
		Updates(map[string]any{
			"status":          row.Status,
			"build_id":        row.BuildID,
			"sha256":          row.SHA256,
			"size":            row.Size,
			"attempts":        row.Attempts,
			"next_attempt_at": row.NextAttemptAt,
			"last_error":      row.LastError,
			"updated_at":      row.UpdatedAt,
		}).Error
}

func (s *Store) ListSymbolUploads(ctx context.Context, appID int64, limit int) ([]dto.SymbolUploadView, error) {
	var ups []model.SymbolUpload
	if err := s.db.WithContext(ctx).
		Where("app_id = ?", appID).
		Order("id DESC").
		Limit(limit).
		Find(&ups).Error; err != nil {
		return nil, err
	}
	if len(ups) == 0 {
		return []dto.SymbolUploadView{}, nil
	}
	ids := make([]int64, len(ups))
	revs := map[string]struct{}{}
	for i, u := range ups {
		ids[i] = u.ID
		if u.EngineRevision != "" {
			revs[u.EngineRevision] = struct{}{}
		}
	}
	var files []model.SymbolFile
	if err := s.db.WithContext(ctx).Where("upload_id IN ?", ids).Order("id").Find(&files).Error; err != nil {
		return nil, err
	}
	byUpload := map[int64][]dto.SymbolFileView{}
	for _, f := range files {
		byUpload[f.UploadID] = append(byUpload[f.UploadID], dto.SymbolFileView{
			FileName: f.FileName,
			Kind:     f.Kind,
			Arch:     f.Arch,
			BuildID:  f.BuildID,
			SHA256:   f.SHA256,
			Size:     f.Size,
		})
	}
	engByRev := map[string][]dto.EngineSymbolView{}
	if len(revs) > 0 {
		revList := make([]string, 0, len(revs))
		for r := range revs {
			revList = append(revList, r)
		}
		var engs []model.EngineSymbol
		if err := s.db.WithContext(ctx).Where("engine_revision IN ?", revList).Find(&engs).Error; err != nil {
			return nil, err
		}
		for _, e := range engs {
			engByRev[e.EngineRevision] = append(engByRev[e.EngineRevision], dto.EngineSymbolView{
				Variant:   e.Variant,
				Status:    e.Status,
				BuildID:   e.BuildID,
				LastError: e.LastError,
			})
		}
	}
	out := make([]dto.SymbolUploadView, 0, len(ups))
	for _, u := range ups {
		files := byUpload[u.ID]
		if files == nil {
			files = []dto.SymbolFileView{}
		}
		view := dto.SymbolUploadView{
			ID:             u.ID,
			AppID:          u.AppID,
			ServiceVersion: u.ServiceVersion,
			EngineRevision: u.EngineRevision,
			CreatedAt:      u.CreatedAt.UTC().Format(time.RFC3339),
			Files:          files,
		}
		if u.EngineRevision != "" {
			view.EngineSymbols = engByRev[u.EngineRevision]
			if view.EngineSymbols == nil {
				view.EngineSymbols = []dto.EngineSymbolView{}
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Store) PurgeExpiredSymbols(ctx context.Context, now time.Time) error {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -365)
	metricFloor := today.AddDate(0, 0, -365).Format("2006-01-02")
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT u.id FROM telemetry_symbol_upload u
		 WHERE u.created_at < ?
		   AND NOT EXISTS (
		         SELECT 1 FROM telemetry_daily_metric m
		          WHERE m.app_id = u.app_id
		            AND m.service_version = u.service_version
		            AND m.day >= ?::date
		       )`, cutoff, metricFloor).Scan(&ids).Error; err != nil {
		return fmt.Errorf("select expired symbol uploads: %w", err)
	}
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("upload_id IN ?", ids).Delete(&model.SymbolFile{}).Error; err != nil {
			return fmt.Errorf("delete expired symbol files: %w", err)
		}
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.SymbolUpload{}).Error; err != nil {
			return fmt.Errorf("delete expired symbol uploads: %w", err)
		}
	}
	if err := s.db.WithContext(ctx).Exec(`
		DELETE FROM telemetry_engine_symbol e
		 WHERE NOT EXISTS (
		         SELECT 1 FROM telemetry_symbol_upload u
		          WHERE u.engine_revision = e.engine_revision
		            AND u.engine_revision <> ''
		       )`).Error; err != nil {
		return fmt.Errorf("delete orphan engine symbols: %w", err)
	}
	var shas []string
	if err := s.db.WithContext(ctx).Raw(`
		SELECT b.sha256 FROM telemetry_blob b
		 WHERE NOT EXISTS (SELECT 1 FROM telemetry_symbol_file f WHERE f.sha256 = b.sha256)
		   AND NOT EXISTS (SELECT 1 FROM telemetry_engine_symbol e WHERE e.sha256 = b.sha256 AND e.sha256 <> '')
	`).Scan(&shas).Error; err != nil {
		return fmt.Errorf("select orphan blobs: %w", err)
	}
	if s.blobs == nil && len(shas) > 0 {
		return fmt.Errorf("blob store not configured")
	}
	for _, sha := range shas {
		if err := s.blobs.Delete(ctx, symbols.BlobKey(sha)); err != nil {
			return fmt.Errorf("delete blob object %s: %w", sha, err)
		}
		if err := s.db.WithContext(ctx).Where("sha256 = ?", sha).Delete(&model.Blob{}).Error; err != nil {
			return fmt.Errorf("delete blob row %s: %w", sha, err)
		}
	}
	return nil
}
