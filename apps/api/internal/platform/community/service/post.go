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
	ThreadID       int64
	AuthorID       int64
	BodyRaw        string
	RootPostID     *int64
	ReplyToPostID  *int64
	TargetUserID   *int64
	MentionUserIDs []int64
}

func (s *PostService) Reply(ctx context.Context, p ReplyParams) (*model.CommunityPost, error) {
	draft, err := s.draftPost(ctx, callerSite(ctx), p.AuthorID, p.BodyRaw)
	if err != nil {
		return nil, err
	}
	mentions, err := normalizeMentionIDs(p.AuthorID, p.MentionUserIDs)
	if err != nil {
		return nil, err
	}
	draft.rootPostID, draft.replyToPostID, draft.targetUserID = p.RootPostID, p.ReplyToPostID, p.TargetUserID
	draft.mentionUserIDs = mentions

	var written writtenPost
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		thread, err := repository.GetThreadTx(tx, p.ThreadID)
		if err != nil {
			return err
		}
		if thread == nil || crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
			return ErrThreadNotFound
		}
		written, err = appendPostTx(tx, thread, draft)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.emitWrite(p.ThreadID, p.AuthorID, written)
	return &written.post, nil
}

// postDraft is what a write face settles BEFORE it opens a transaction: the
// cooking, the author's trust level, the sandbox quota and the content check.
type postDraft struct {
	authorID       int64
	site           string
	bodyRaw        string
	cooked         sanitize.Cooked
	rootPostID     *int64
	replyToPostID  *int64
	targetUserID   *int64
	mentionUserIDs []int64
	suspectHold    bool
	now            time.Time
}

type writtenPost struct {
	post           model.CommunityPost
	enqueuedItemID int64
	targetUserID   *int64
}

func (s *PostService) draftPost(ctx context.Context, site string, authorID int64, bodyRaw string) (postDraft, error) {
	cooked := sanitize.Cook(bodyRaw)
	level, err := trustLevel(s.trusts, authorID)
	if err != nil {
		return postDraft{}, err
	}
	if err := checkContentSandbox(level, cooked); err != nil {
		return postDraft{}, err
	}
	if isSandboxed(level) {
		n, err := s.posts.CountByAuthorSince(authorID, time.Now().Add(-time.Duration(keys.CommunitySandboxWindowHours.Get())*time.Hour))
		if err != nil {
			return postDraft{}, err
		}
		if n >= keys.CommunitySandboxMaxRepliesPerDay.Get() {
			return postDraft{}, &SandboxError{Reason: "daily reply limit"}
		}
	}
	draft := postDraft{authorID: authorID, site: site, bodyRaw: bodyRaw, cooked: cooked, now: time.Now()}
	switch s.check.Decision(ctx, site, bodyRaw, &authorID) {
	case checkDeny:
		return postDraft{}, ErrContentBlocked
	case checkHold:
		draft.suspectHold = true
	}
	return draft, nil
}

func appendPostTx(tx *gorm.DB, thread *model.CommunityThread, d postDraft) (writtenPost, error) {
	var out writtenPost
	if thread.Status != model.ThreadStatusOpen {
		return out, ErrThreadNotOpen
	}
	trust, err := repository.GetOrCreateTrustTx(tx, d.authorID)
	if err != nil {
		return out, err
	}
	held := trust.FirstPostsHeldRemaining > 0
	if thread.Kind == model.ThreadKindTopic && thread.AnchorKind == model.AnchorKindBoard {
		board, err := repository.TopicBoardTx(tx, thread)
		if err != nil {
			return out, err
		}
		if board == nil {
			return out, ErrBoardNotFound
		}
		if err := replyGate(board, trust.Level); err != nil {
			return out, err
		}
	}

	posted, err := repository.AuthorHasPostedTx(tx, thread.ID, d.authorID)
	if err != nil {
		return out, err
	}
	number, err := repository.AllocateReplyTx(tx, thread.ID, d.now, !posted)
	if err != nil {
		return out, err
	}
	rootPostID, targetUserID := d.rootPostID, d.targetUserID
	if d.replyToPostID != nil && (rootPostID == nil || targetUserID == nil) {
		parent, err := repository.GetPostTx(tx, *d.replyToPostID)
		if err != nil {
			return out, err
		}
		if parent != nil && parent.ThreadID == thread.ID {
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
	out.targetUserID = targetUserID
	out.post = model.CommunityPost{
		ThreadID: thread.ID, PostNumber: number,
		RootPostID: rootPostID, ReplyToPostID: d.replyToPostID, TargetUserID: targetUserID,
		AuthorID:   d.authorID,
		ContentRaw: d.bodyRaw, ContentHTML: d.cooked.HTML, SanitizerVersion: int32(d.cooked.Version),
		ContentRating: thread.ContentRating,
		Status:        postStatus(held),
	}
	if err := repository.CreatePostTx(tx, &out.post); err != nil {
		return out, err
	}
	site := d.site
	if site == "" {
		site = thread.Site
	}
	if err := enqueuePostCreatedTx(tx, site, thread.ID, out.post.ID, d.authorID, out.targetUserID, d.mentionUserIDs); err != nil {
		return out, err
	}
	if err := repository.EnsureSubscribedTx(tx, thread.ID, d.authorID, out.post.PostNumber, site); err != nil {
		return out, err
	}
	if err := repository.MarkThreadNotificationsReadTx(tx, d.authorID, thread.ID, out.post.PostNumber); err != nil {
		return out, err
	}
	if held {
		itemID, created, err := repository.EnqueueReviewIfAbsentTx(tx, thread.Site, out.post.ID, model.ReviewSourceFirstPostHold)
		if err != nil {
			return out, err
		}
		if created {
			out.enqueuedItemID = itemID
		}
		return out, repository.DecrementHoldTx(tx, d.authorID)
	}
	if d.suspectHold {
		itemID, created, err := repository.EnqueueReviewIfAbsentTx(tx, thread.Site, out.post.ID, model.ReviewSourceSuspectWords)
		if err != nil {
			return out, err
		}
		if created {
			out.enqueuedItemID = itemID
		}
	}
	return out, nil
}

func (s *PostService) emitWrite(threadID, authorID int64, w writtenPost) {
	s.sink.Emit(Event{Kind: EventPostCreated, ThreadID: threadID, PostID: w.post.ID, ActorID: authorID})
	if w.enqueuedItemID != 0 {
		s.sink.Emit(Event{Kind: EventReviewEnqueued, ThreadID: threadID, PostID: w.post.ID, ReviewItemID: w.enqueuedItemID})
	}
	if w.targetUserID != nil && *w.targetUserID != authorID {
		s.sink.Emit(Event{Kind: EventReplyToYou, ThreadID: threadID, PostID: w.post.ID, ActorID: authorID, TargetID: *w.targetUserID})
	}
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
