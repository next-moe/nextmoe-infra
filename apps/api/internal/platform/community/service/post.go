package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/sanitize"
	"api/internal/platform/settings/keys"

	"gorm.io/gorm"
)

type PostService struct {
	db     *gorm.DB
	posts  *repository.PostRepository
	trusts *repository.TrustRepository
	sink   EventSink
	check  *CheckService
}

type PostOption func(*PostService)

func WithPostChecker(c *CheckService) PostOption {
	return func(s *PostService) { s.check = c }
}

func NewPostService(db *gorm.DB, sink EventSink, opts ...PostOption) *PostService {
	s := &PostService{
		db:     db,
		posts:  repository.NewPostRepository(db),
		trusts: repository.NewTrustRepository(db),
		sink:   sink,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

type ReplyParams struct {
	ThreadID      int64
	AuthorID      int64
	BodyRaw       string
	RootPostID    *int64
	ReplyToPostID *int64
	TargetUserID  *int64
}

func (s *PostService) Reply(ctx context.Context, p ReplyParams) (*model.CommunityPost, error) {
	cooked := sanitize.Cook(p.BodyRaw)
	level, err := trustLevel(s.trusts, p.AuthorID)
	if err != nil {
		return nil, err
	}
	if err := checkContentSandbox(level, cooked); err != nil {
		return nil, err
	}
	if isSandboxed(level) {
		n, err := s.posts.CountByAuthorSince(p.AuthorID, time.Now().Add(-time.Duration(keys.CommunitySandboxWindowHours.Get())*time.Hour))
		if err != nil {
			return nil, err
		}
		if n >= keys.CommunitySandboxMaxRepliesPerDay.Get() {
			return nil, &SandboxError{Reason: "daily reply limit"}
		}
	}

	suspectHold := false
	switch s.check.Decision(ctx, callerSite(ctx), p.BodyRaw, &p.AuthorID) {
	case checkDeny:
		return nil, ErrContentBlocked
	case checkHold:
		suspectHold = true
	}

	now := time.Now()
	var post model.CommunityPost
	var enqueuedItemID int64
	rootPostID, targetUserID := p.RootPostID, p.TargetUserID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		thread, err := repository.GetThreadTx(tx, p.ThreadID)
		if err != nil {
			return err
		}
		if thread == nil {
			return ErrThreadNotFound
		}
		if crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
			return ErrThreadNotFound
		}
		if thread.Status != model.ThreadStatusOpen {
			return ErrThreadNotOpen
		}
		trust, err := repository.GetOrCreateTrustTx(tx, p.AuthorID)
		if err != nil {
			return err
		}
		held := trust.FirstPostsHeldRemaining > 0

		posted, err := repository.AuthorHasPostedTx(tx, p.ThreadID, p.AuthorID)
		if err != nil {
			return err
		}
		number, err := repository.AllocateReplyTx(tx, p.ThreadID, now, !posted)
		if err != nil {
			return err
		}
		if p.ReplyToPostID != nil && (rootPostID == nil || targetUserID == nil) {
			parent, perr := repository.GetPostTx(tx, *p.ReplyToPostID)
			if perr != nil {
				return perr
			}
			if parent != nil && parent.ThreadID == p.ThreadID {
				if targetUserID == nil {
					author := parent.AuthorID
					targetUserID = &author
				}
				if rootPostID == nil {
					if parent.RootPostID != nil {
						rootPostID = parent.RootPostID
					} else {
						top := parent.ID
						rootPostID = &top
					}
				}
			}
		}
		post = model.CommunityPost{
			ThreadID: p.ThreadID, PostNumber: number,
			RootPostID: rootPostID, ReplyToPostID: p.ReplyToPostID, TargetUserID: targetUserID,
			AuthorID:   p.AuthorID,
			ContentRaw: p.BodyRaw, ContentHTML: cooked.HTML, SanitizerVersion: int32(cooked.Version),
			ContentRating: thread.ContentRating,
			Status:        postStatus(held),
		}
		if err := repository.CreatePostTx(tx, &post); err != nil {
			return err
		}
		if err := repository.EnsureSubscribedTx(tx, p.ThreadID, p.AuthorID, post.PostNumber); err != nil {
			return err
		}
		if held {
			itemID, created, err := repository.EnqueueReviewIfAbsentTx(tx, thread.Site, post.ID, model.ReviewSourceFirstPostHold)
			if err != nil {
				return err
			}
			if created {
				enqueuedItemID = itemID
			}
			return repository.DecrementHoldTx(tx, p.AuthorID)
		}
		if suspectHold {
			itemID, created, err := repository.EnqueueReviewIfAbsentTx(tx, thread.Site, post.ID, model.ReviewSourceSuspectWords)
			if err != nil {
				return err
			}
			if created {
				enqueuedItemID = itemID
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.sink.Emit(Event{Kind: EventPostCreated, ThreadID: p.ThreadID, PostID: post.ID, ActorID: p.AuthorID})
	if enqueuedItemID != 0 {
		s.sink.Emit(Event{Kind: EventReviewEnqueued, ThreadID: p.ThreadID, PostID: post.ID, ReviewItemID: enqueuedItemID})
	}
	if targetUserID != nil && *targetUserID != p.AuthorID {
		s.sink.Emit(Event{Kind: EventReplyToYou, ThreadID: p.ThreadID, PostID: post.ID, ActorID: p.AuthorID, TargetID: *targetUserID})
	}
	return &post, nil
}

func (s *PostService) ListPosts(threadID int64, afterNumber int32, limit int) ([]model.CommunityPost, error) {
	return s.posts.ListByThread(threadID, afterNumber, clampLimit(limit))
}

type EditParams struct {
	PostID      int64
	AuthorID    int64
	BodyRaw     string
	AsModerator bool
}

func (s *PostService) Edit(ctx context.Context, p EditParams) (*model.CommunityPost, error) {
	cooked := sanitize.Cook(p.BodyRaw)
	level, err := trustLevel(s.trusts, p.AuthorID)
	if err != nil {
		return nil, err
	}
	if err := checkContentSandbox(level, cooked); err != nil {
		return nil, err
	}

	suspectHold := false
	switch s.check.Decision(ctx, callerSite(ctx), p.BodyRaw, &p.AuthorID) {
	case checkDeny:
		return nil, ErrContentBlocked
	case checkHold:
		suspectHold = true
	}

	now := time.Now()
	var post model.CommunityPost
	var enqueuedItemID int64
	modActed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := repository.GetPostTx(tx, p.PostID)
		if err != nil {
			return err
		}
		if existing == nil {
			return ErrPostNotFound
		}
		thread, err := repository.GetThreadTx(tx, existing.ThreadID)
		if err != nil {
			return err
		}
		if thread == nil || crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
			return ErrPostNotFound
		}
		if existing.AuthorID != p.AuthorID {
			if !p.AsModerator {
				return ErrNotAuthor
			}
			modActed = true
		}
		if existing.Status != model.PostStatusVisible {
			return ErrPostNotEditable
		}
		if err := repository.UpdatePostContentTx(tx, p.PostID, p.BodyRaw, cooked.HTML, int32(cooked.Version), now, modActed); err != nil {
			return err
		}
		existing.ContentRaw = p.BodyRaw
		existing.ContentHTML = cooked.HTML
		existing.SanitizerVersion = int32(cooked.Version)
		existing.EditedAt = &now
		existing.EditedByModerator = modActed
		if suspectHold {
			itemID, created, eqErr := repository.EnqueueReviewIfAbsentTx(tx, thread.Site, existing.ID, model.ReviewSourceSuspectWords)
			if eqErr != nil {
				return eqErr
			}
			if created {
				enqueuedItemID = itemID
			}
		}
		post = *existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	if modActed {
		slog.Info("community mod edit", "post_id", post.ID, "thread_id", post.ThreadID,
			"post_author_id", post.AuthorID, "moderator_id", p.AuthorID)
	}
	s.sink.Emit(Event{Kind: EventPostEdited, ThreadID: post.ThreadID, PostID: post.ID, ActorID: p.AuthorID})
	if enqueuedItemID != 0 {
		s.sink.Emit(Event{Kind: EventReviewEnqueued, ThreadID: post.ThreadID, PostID: post.ID, ReviewItemID: enqueuedItemID})
	}
	return &post, nil
}

func (s *PostService) Delete(ctx context.Context, postID, actorID int64, asModerator bool) error {
	modActed := false
	var threadID, authorID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := repository.GetPostTx(tx, postID)
		if err != nil {
			return err
		}
		if existing == nil {
			return ErrPostNotFound
		}
		thread, err := repository.GetThreadTx(tx, existing.ThreadID)
		if err != nil {
			return err
		}
		if thread == nil || crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
			return ErrPostNotFound
		}
		if existing.AuthorID != actorID {
			if !asModerator {
				return ErrNotAuthor
			}
			modActed = true
		}
		threadID, authorID = existing.ThreadID, existing.AuthorID
		if existing.Status == model.PostStatusDeleted {
			return nil
		}
		return repository.SetPostStatusTx(tx, postID, model.PostStatusDeleted)
	})
	if err != nil {
		return err
	}
	if modActed {
		slog.Info("community mod delete", "post_id", postID, "thread_id", threadID,
			"post_author_id", authorID, "moderator_id", actorID)
	}
	return nil
}
