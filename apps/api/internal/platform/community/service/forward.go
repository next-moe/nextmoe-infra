package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

const (
	forwardSubjectKind    = "community_post"
	forwardBatchSize      = 50
	forwardErrorThreshold = 5
	contextNoteMaxRunes   = 500

	outcomeApproved = "approved"
	outcomeRejected = "rejected"
)

type Forwarder interface {
	Forward(ctx context.Context, req trustclient.ForwardRequest) (trustItemID int64, created bool, err error)
	Resolve(ctx context.Context, trustItemID int64, outcome, actorRef string) (closed bool, err error)
}

type ForwardService struct {
	db *gorm.DB
	fw Forwarder
}

func NewForwardService(db *gorm.DB, fw Forwarder) *ForwardService {
	return &ForwardService{db: db, fw: fw}
}

func (s *ForwardService) Enabled() bool { return s.fw != nil }

func (s *ForwardService) ForwardItemBg(itemID int64) {
	if err := s.forwardItem(context.Background(), itemID); err != nil {
		slog.Warn("community trust-forward (immediate)", "item_id", itemID, "err", err)
	}
}

func (s *ForwardService) forwardItem(ctx context.Context, itemID int64) error {
	if s.fw == nil {
		return nil
	}
	ft, found, err := repository.LoadForwardTargetTx(s.db.WithContext(ctx), itemID)
	if err != nil {
		return err
	}
	if !found || ft.Forwarded {
		return nil
	}
	trustID, _, err := s.fw.Forward(ctx, s.buildRequest(ft))
	if err != nil {
		return err
	}
	return repository.SetTrustReviewItemIDTx(s.db.WithContext(ctx), itemID, trustID)
}

func (s *ForwardService) Sweep(ctx context.Context) (int, error) {
	if s.fw == nil {
		return 0, nil
	}
	forwarded := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := repository.LockUnforwardedTx(tx, forwardBatchSize)
		if err != nil {
			return err
		}
		for i := range rows {
			row := &rows[i]
			if err := repository.BumpForwardAttemptsTx(tx, row.ID); err != nil {
				return err
			}
			attempts := row.ForwardAttempts + 1
			author, content, ok, err := repository.PostBodyTx(tx, *row.PostID)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			ft := &repository.ForwardTarget{
				ItemID: row.ID, Site: *row.Site, PostID: *row.PostID, Source: row.Source,
				AuthorID: author, ContentRaw: content,
			}
			trustID, _, ferr := s.fw.Forward(ctx, s.buildRequest(ft))
			if ferr != nil {
				if int(attempts) >= forwardErrorThreshold {
					slog.Error("community trust-forward sweep", "item_id", row.ID, "attempts", attempts, "err", ferr)
				} else {
					slog.Warn("community trust-forward sweep", "item_id", row.ID, "attempts", attempts, "err", ferr)
				}
				continue
			}
			if err := repository.SetTrustReviewItemIDTx(tx, row.ID, trustID); err != nil {
				return err
			}
			forwarded++
		}
		return nil
	})
	return forwarded, err
}

func (s *ForwardService) ResolveItemBg(itemID int64, outcome string, actorID int64) {
	if err := s.resolveItem(context.Background(), itemID, outcome, actorID); err != nil {
		slog.Warn("community trust-resolve", "item_id", itemID, "outcome", outcome, "err", err)
	}
}

func (s *ForwardService) resolveItem(ctx context.Context, itemID int64, outcome string, actorID int64) error {
	if s.fw == nil {
		return nil
	}
	row, err := repository.GetReviewItemTx(s.db.WithContext(ctx), itemID)
	if err != nil {
		return err
	}
	if row == nil || row.TrustReviewItemID == nil {
		return nil
	}
	_, err = s.fw.Resolve(ctx, *row.TrustReviewItemID, outcome, strconv.FormatInt(actorID, 10))
	return err
}

func (s *ForwardService) buildRequest(ft *repository.ForwardTarget) trustclient.ForwardRequest {
	note := forwardContextNote(ft)
	ref := strconv.FormatInt(ft.ItemID, 10)
	return trustclient.ForwardRequest{
		Site: ft.Site, SubjectKind: forwardSubjectKind, SubjectID: strconv.FormatInt(ft.PostID, 10),
		AuthorID: &ft.AuthorID, ContextNote: &note, ForwarderRef: &ref,
	}
}

func forwardContextNote(ft *repository.ForwardTarget) string {
	return fmt.Sprintf("[%s] post #%d by user %d: %s",
		forwardSourceLabel(ft.Source), ft.PostID, ft.AuthorID, truncateRunes(ft.ContentRaw, contextNoteMaxRunes))
}

func forwardSourceLabel(source *int16) string {
	if source == nil {
		return "unknown"
	}
	switch *source {
	case model.ReviewSourceFlags:
		return "flags"
	case model.ReviewSourceFirstPostHold:
		return "first_post_hold"
	case model.ReviewSourceSuspectWords:
		return "suspect_words"
	case model.ReviewSourceExternal:
		return "external"
	default:
		return "unknown"
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

type ForwardingSink struct {
	inner EventSink
	fwd   *ForwardService
}

func NewForwardingSink(inner EventSink, fwd *ForwardService) ForwardingSink {
	return ForwardingSink{inner: inner, fwd: fwd}
}

func (s ForwardingSink) Emit(e Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	if s.fwd == nil || !s.fwd.Enabled() {
		return
	}
	switch e.Kind {
	case EventReviewEnqueued:
		go s.fwd.ForwardItemBg(e.ReviewItemID)
	case EventReviewApproved:
		go s.fwd.ResolveItemBg(e.ReviewItemID, outcomeApproved, e.ActorID)
	case EventReviewRejected:
		go s.fwd.ResolveItemBg(e.ReviewItemID, outcomeRejected, e.ActorID)
	}
}
