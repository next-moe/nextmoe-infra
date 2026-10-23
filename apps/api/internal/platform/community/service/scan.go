package service

import (
	"context"
	"log/slog"
	"strconv"

	"api/internal/platform/community/repository"
	"api/internal/platform/settings/keys"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

const scanSubjectKind = forwardSubjectKind

type Scanner interface {
	Scan(ctx context.Context, req trustclient.ScanRequest) (scanID int64, truncated bool, err error)
}

type ScanService struct {
	db *gorm.DB
	sc Scanner
}

func NewScanService(db *gorm.DB, sc Scanner) *ScanService {
	return &ScanService{db: db, sc: sc}
}

func (s *ScanService) Enabled() bool { return s.sc != nil && keys.TrustScanEnabled.Get() }

func (s *ScanService) ScanPostBg(postID int64, checkSkipped bool) {
	if err := s.scanPost(context.Background(), postID, checkSkipped); err != nil {
		slog.Warn("community trust-scan", "post_id", postID, "err", err)
	}
}

func (s *ScanService) scanPost(ctx context.Context, postID int64, checkSkipped bool) error {
	if s.sc == nil {
		return nil
	}
	tgt, found, err := repository.LoadScanTargetTx(s.db.WithContext(ctx), postID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	text := tgt.ContentRaw
	if tgt.PostNumber == 1 && tgt.Title != "" {
		text = tgt.Title + "\n\n" + tgt.ContentRaw
	}
	author := tgt.AuthorID
	_, _, err = s.sc.Scan(ctx, trustclient.ScanRequest{
		Site: tgt.Site, SubjectKind: scanSubjectKind, SubjectID: strconv.FormatInt(tgt.PostID, 10),
		Text: text, AuthorID: &author, CheckSkipped: checkSkipped,
	})
	return err
}

type ScanningSink struct {
	inner EventSink
	scan  *ScanService
}

func NewScanningSink(inner EventSink, scan *ScanService) ScanningSink {
	return ScanningSink{inner: inner, scan: scan}
}

func (s ScanningSink) Emit(e Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	if s.scan == nil || !s.scan.Enabled() {
		return
	}
	switch e.Kind {
	case EventPostCreated, EventPostEdited:
		go s.scan.ScanPostBg(e.PostID, e.CheckSkipped)
	}
}
