package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"api/internal/platform/catalog/repository"
)

var (
	ErrRedirectChain         = errors.New("catalog: redirect chain detected (flatten invariant broken)")
	ErrSameEntity            = errors.New("catalog: source and target resolve to the same entity")
	ErrProposalState         = errors.New("catalog: proposal is not in the required state")
	ErrCoolingOff            = errors.New("catalog: cooling-off window has not elapsed")
	ErrDuplicateOpenProposal = errors.New("catalog: an open proposal for this pair already exists")
	ErrClaimConflict         = errors.New("catalog: work already claimed by another product work")
	ErrNotFound              = errors.New("catalog: not found")
)

type ResolveService struct {
	redirects *repository.RedirectRepository
}

func NewResolveService(redirects *repository.RedirectRepository) *ResolveService {
	return &ResolveService{redirects: redirects}
}

func (s *ResolveService) Resolve(ctx context.Context, entityType int16, id int64) (int64, bool, error) {
	row, err := s.redirects.Get(ctx, entityType, id)
	if err != nil {
		return 0, false, err
	}
	if row == nil {
		return id, false, nil
	}
	chained, err := s.redirects.Get(ctx, entityType, row.CurrentID)
	if err != nil {
		return 0, false, err
	}
	if chained != nil {
		slog.Error("catalog redirect chain detected",
			"entity_type", entityType, "old_id", id,
			"current_id", row.CurrentID, "chained_to", chained.CurrentID)
		return 0, false, fmt.Errorf("%w: %d -> %d -> %d", ErrRedirectChain, id, row.CurrentID, chained.CurrentID)
	}
	return row.CurrentID, true, nil
}

func (s *ResolveService) ResolveBatch(ctx context.Context, entityType int16, ids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(ids))
	for _, id := range ids {
		out[id] = id
	}
	if len(ids) == 0 {
		return out, nil
	}
	redirected, err := s.redirects.GetBatch(ctx, entityType, ids)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, redirected)
	return out, nil
}

func (s *ResolveService) RedirectsSince(ctx context.Context, entityType *int16, cursor repository.RedirectCursor, limit int) ([]RedirectFeedItem, repository.RedirectCursor, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.redirects.Since(ctx, entityType, cursor, limit)
	if err != nil {
		return nil, cursor, err
	}
	items := make([]RedirectFeedItem, len(rows))
	next := cursor
	for i, row := range rows {
		// The cursor advances on every row, including one with no merge time:
		// skipping those left the cursor where it was and the crawl never
		// terminated once the NULL rows became visible.
		merged := time.Unix(0, 0).UTC()
		if row.MergedAt != nil {
			merged = *row.MergedAt
		}
		items[i] = RedirectFeedItem{
			EntityType: row.EntityType,
			OldID:      row.OldID,
			CurrentID:  row.CurrentID,
			MergedAt:   merged,
		}
		next = repository.RedirectCursor{MergedAt: merged, EntityType: row.EntityType, OldID: row.OldID}
	}
	return items, next, nil
}

func (s *ResolveService) RedirectsTotal(ctx context.Context, entityType *int16) (int64, error) {
	return s.redirects.Count(ctx, entityType)
}

type RedirectFeedItem struct {
	EntityType int16     `json:"entity_type"`
	OldID      int64     `json:"old_id"`
	CurrentID  int64     `json:"current_id"`
	MergedAt   time.Time `json:"merged_at"`
}
