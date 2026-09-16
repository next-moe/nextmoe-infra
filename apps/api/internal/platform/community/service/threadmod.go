package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func (s *ThreadService) Move(ctx context.Context, threadID, boardID, actorID int64) (*model.CommunityThread, error) {
	var from string
	out, err := s.moderateTx(ctx, threadID, func(tx *gorm.DB, th *model.CommunityThread) error {
		if !isBoardTopic(th) {
			return &InvalidError{Reason: "only board topics can be moved"}
		}
		target, err := repository.LockBoardTx(tx, th.Site, boardID, repository.LockShare)
		if err != nil {
			return err
		}
		if target == nil {
			return ErrBoardNotFound
		}
		from = th.AnchorID
		if th.AnchorID == model.BoardAnchorID(target.ID) {
			return nil
		}
		updates := map[string]any{"anchor_id": model.BoardAnchorID(target.ID)}
		if th.PinScope == model.PinScopeBoard {
			updates["pin_scope"], updates["pinned_at"], updates["pinned_until"] = model.PinScopeNone, nil, nil
		}
		if target.ContentRating > th.ContentRating {
			updates["content_rating"] = target.ContentRating
			if err := repository.RaisePostRatingTx(tx, th.ID, target.ContentRating); err != nil {
				return err
			}
		}
		return repository.UpdateThreadTx(tx, th.ID, updates)
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community topic moved", "thread_id", threadID, "from_board", from, "to_board", boardID, "actor_id", actorID)
	return out, nil
}

func (s *ThreadService) Pin(ctx context.Context, threadID int64, scope int16, until *time.Time, actorID int64) (*model.CommunityThread, error) {
	if scope < model.PinScopeNone || scope > model.PinScopeSite {
		return nil, &InvalidError{Reason: "scope must be 0 (unpin), 1 (board) or 2 (site)"}
	}
	now := time.Now()
	if until != nil && (scope == model.PinScopeNone || !until.After(now)) {
		return nil, &InvalidError{Reason: "until must be a future time on a pin"}
	}
	out, err := s.moderateTx(ctx, threadID, func(tx *gorm.DB, th *model.CommunityThread) error {
		if !isBoardTopic(th) {
			return &InvalidError{Reason: "only board topics can be pinned"}
		}
		updates := map[string]any{"pin_scope": scope, "pinned_at": nil, "pinned_until": nil}
		if scope != model.PinScopeNone {
			updates["pinned_at"], updates["pinned_until"] = now, until
		}
		return repository.UpdateThreadTx(tx, th.ID, updates)
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community topic pinned", "thread_id", threadID, "scope", scope, "until", until, "actor_id", actorID)
	return out, nil
}

func (s *ThreadService) SetClosed(ctx context.Context, threadID int64, closed bool, actorID int64) (*model.CommunityThread, error) {
	out, err := s.moderateTx(ctx, threadID, func(tx *gorm.DB, th *model.CommunityThread) error {
		if th.Status != model.ThreadStatusOpen && th.Status != model.ThreadStatusClosed {
			return &ConflictError{Reason: "only an open or closed thread can be closed or reopened"}
		}
		if !closed && th.MergedIntoID != nil {
			return &ConflictError{Reason: "a merged feedback thread stays closed"}
		}
		status := model.ThreadStatusOpen
		if closed {
			status = model.ThreadStatusClosed
		}
		if th.Status == status {
			return nil
		}
		return repository.UpdateThreadTx(tx, th.ID, map[string]any{"status": status})
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community thread closed", "thread_id", threadID, "closed", closed, "actor_id", actorID)
	return out, nil
}

func (s *ThreadService) SetAnswer(ctx context.Context, threadID, postID, actorID int64, asModerator bool) (*model.CommunityThread, error) {
	out, err := s.moderateTx(ctx, threadID, func(tx *gorm.DB, th *model.CommunityThread) error {
		if err := answerable(tx, th); err != nil {
			return err
		}
		if th.CreatedBy != actorID && !asModerator {
			return &ForbiddenError{Reason: "only the thread's author or a moderator marks its answer"}
		}
		var answer *int64
		if postID != 0 {
			post, err := repository.GetPostTx(tx, postID)
			if err != nil {
				return err
			}
			switch {
			case post == nil || post.ThreadID != th.ID:
				return &InvalidError{Reason: "the answer must be a reply in this thread"}
			case post.PostNumber == 1:
				return &InvalidError{Reason: "the opening post cannot answer itself"}
			case post.Status != model.PostStatusVisible:
				return &InvalidError{Reason: "the answer must be a visible reply"}
			}
			answer = &postID
		}
		if err := repository.UpdateThreadTx(tx, th.ID, map[string]any{"answer_post_id": answer}); err != nil {
			return err
		}
		if postID == 0 || (th.AnswerPostID != nil && *th.AnswerPostID == postID) {
			return nil
		}
		return repository.EnqueueEventTx(tx, &model.CommunityEvent{
			Site: th.Site, Kind: model.EventKindAnswerAccepted,
			ThreadID: th.ID, PostID: &postID, ActorID: actorID,
			AttemptAfter: time.Now(),
		})
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community answer marked", "thread_id", threadID, "post_id", postID,
		"actor_id", actorID, "as_moderator", asModerator)
	return out, nil
}

func answerable(tx *gorm.DB, th *model.CommunityThread) error {
	if th.Kind == model.ThreadKindFeedback {
		return nil
	}
	if isBoardTopic(th) {
		board, err := repository.TopicBoardTx(tx, th)
		if err != nil {
			return err
		}
		if board != nil && board.Format == model.BoardFormatQA {
			return nil
		}
	}
	return &InvalidError{Reason: "answers are marked on Q&A board topics and on feedback threads"}
}

func (s *ThreadService) moderateTx(ctx context.Context, threadID int64, change func(tx *gorm.DB, th *model.CommunityThread) error) (*model.CommunityThread, error) {
	var out *model.CommunityThread
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		th, err := repository.LockThreadTx(tx, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.Status == model.ThreadStatusDeleted || !ownSite(ctx, th.Site) {
			return ErrThreadNotFound
		}
		if err := change(tx, th); err != nil {
			return err
		}
		out, err = repository.GetThreadTx(tx, threadID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Not crossTenantCtx: that admits every site to a catalog-anchored thread, and
// here it let any site close a comment wall shared by the whole network (caught
// in review before it shipped). Only the site that opened a thread moderates it.
func ownSite(ctx context.Context, threadSite string) bool {
	site := callerSite(ctx)
	return site == "" || site == threadSite
}

func isBoardTopic(th *model.CommunityThread) bool {
	return th.Kind == model.ThreadKindTopic && th.AnchorKind == model.AnchorKindBoard
}
