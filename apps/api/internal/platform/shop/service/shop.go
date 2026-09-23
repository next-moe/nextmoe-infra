package service

import (
	"context"
	"strings"
	"time"

	ledgerService "api/internal/platform/ledger/service"

	"gorm.io/gorm"
)

type ObjectStore interface {
	PutWithCacheControl(ctx context.Context, key string, body []byte, contentType, cacheControl string) error
}

type Shop struct {
	db     *gorm.DB
	ledger *ledgerService.Ledger
	store  ObjectStore
	cdn    string
	now    func() time.Time
}

func New(db *gorm.DB, ledger *ledgerService.Ledger, store ObjectStore, cdnBase string) *Shop {
	return &Shop{db: db, ledger: ledger, store: store, cdn: strings.TrimRight(cdnBase, "/"), now: time.Now}
}

func (s *Shop) assetURL(key string) string {
	if key == "" {
		return ""
	}
	return s.cdn + "/" + key
}
