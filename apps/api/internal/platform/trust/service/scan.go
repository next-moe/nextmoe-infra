package service

import (
	"context"

	"api/internal/platform/trust/model"

	"gorm.io/gorm"
)

type ScanService struct {
	db        *gorm.DB
	allowlist map[string]bool
}

func NewScanService(db *gorm.DB, allowlist map[string]bool) *ScanService {
	return &ScanService{db: db, allowlist: allowlist}
}

func (s *ScanService) allowed(clientID string) bool {
	return clientID != "" && s.allowlist[clientID]
}

type ScanParams struct {
	CallerClientID string
	Site           string
	WireSite       string
	SubjectKind    string
	SubjectID      string
	Text           string
	AuthorID       *int64
	SubjectReach   *int64
	CheckSkipped   bool
}

type ScanResult struct {
	ScanID    int64
	Truncated bool
}

func (s *ScanService) Ingest(ctx context.Context, p ScanParams) (ScanResult, error) {
	site := p.Site
	if p.WireSite != "" {
		if !s.allowed(p.CallerClientID) {
			return ScanResult{}, ErrForwarderNotAllowed
		}
		site = p.WireSite
	}

	var kindCount int64
	if err := s.db.WithContext(ctx).Model(&model.TrustSubjectKind{}).
		Where("site = ? AND key = ? AND is_deprecated = false", site, p.SubjectKind).
		Count(&kindCount).Error; err != nil {
		return ScanResult{}, err
	}
	if kindCount == 0 {
		return ScanResult{}, ErrSubjectKindNotRegistered
	}

	text, truncated := capRunes(p.Text, maxScanTextRunes)

	row := model.TrustScanResult{
		Site: site, SubjectKind: p.SubjectKind, SubjectID: p.SubjectID,
		AuthorID: p.AuthorID, ContentText: text, SubjectReach: p.SubjectReach, CheckSkipped: p.CheckSkipped,
		Status: model.ScanStatusPending, Mode: model.ScanModeShadow,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ScanResult{}, err
	}
	return ScanResult{ScanID: row.ID, Truncated: truncated}, nil
}

func capRunes(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return string(r[:n]), true
}
