package walkthroughdraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"api/internal/platform/catalog/llmsuggest"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/catalog/walkthrough/extract"

	"gorm.io/gorm"
)

const (
	PerDay                     = 20
	MaxRunes                   = 60000
	walkthroughDraftMaxTokens  = 16000
	walkthroughDraftWindow     = 24 * time.Hour
	walkthroughDraftSchemaName = "walkthrough"
)

var (
	ErrUnavailable = errors.New("walkthrough drafts: no model configured")
	ErrQuota       = errors.New("walkthrough drafts: daily quota used up")
)

type Service struct {
	db     *gorm.DB
	llm    *llmsuggest.Client
	schema map[string]any
}

func NewService(db *gorm.DB, baseURL, modelName string) *Service {
	s := &Service{db: db}
	if baseURL != "" {
		s.llm = llmsuggest.NewClient(baseURL, modelName)
		if err := json.Unmarshal([]byte(extract.SchemaJSON), &s.schema); err != nil {
			panic(err)
		}
	}
	return s
}

type Draft struct {
	Verdict   string
	Chart     *walkthrough.Chart
	Metrics   extract.Metrics
	Remaining int
}

func (s *Service) Draft(ctx context.Context, uid int64, text string) (Draft, error) {
	if s == nil || s.llm == nil {
		return Draft{}, ErrUnavailable
	}
	runes := utf8.RuneCountInString(text)
	var used int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('walkthrough-draft'), ?::int)`, uid).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CatalogWalkthroughDraftLog{}).
			Where("user_id = ? AND created_at > ?", uid, time.Now().Add(-walkthroughDraftWindow)).Count(&used).Error; err != nil {
			return err
		}
		if used >= PerDay {
			return ErrQuota
		}
		return tx.Create(&model.CatalogWalkthroughDraftLog{UserID: uid, InputRunes: runes, CreatedAt: time.Now()}).Error
	})
	if err != nil {
		return Draft{}, err
	}
	out := Draft{Remaining: PerDay - int(used) - 1}
	var got extract.Extracted
	for attempt := 0; ; attempt++ {
		res, err := s.llm.ChatJSON(ctx, extract.System, text, walkthroughDraftSchemaName, s.schema, walkthroughDraftMaxTokens)
		if err != nil {
			return out, fmt.Errorf("walkthrough draft: %w", err)
		}
		if res.FinishReason == "length" {
			return out, fmt.Errorf("walkthrough draft: the text is too long for one chart")
		}
		if err = json.Unmarshal([]byte(res.Content), &got); err == nil {
			break
		}
		if attempt >= 1 {
			return out, fmt.Errorf("walkthrough draft: %w", err)
		}
	}
	out.Verdict, out.Chart = extract.Settle(text, got, &out.Metrics)
	if out.Chart != nil {
		walkthrough.FillNil(out.Chart)
	}
	return out, nil
}
