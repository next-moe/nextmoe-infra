package service

import (
	"context"
	"strings"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/sanitize"
	"api/internal/platform/settings/keys"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ThreadService struct {
	db      *gorm.DB
	threads *repository.ThreadRepository
	trusts  *repository.TrustRepository
	sink    EventSink
	check   *CheckService
}

type ThreadOption func(*ThreadService)

func WithThreadChecker(c *CheckService) ThreadOption {
	return func(s *ThreadService) { s.check = c }
}

func NewThreadService(db *gorm.DB, sink EventSink, opts ...ThreadOption) *ThreadService {
	s := &ThreadService{
		db:      db,
		threads: repository.NewThreadRepository(db),
		trusts:  repository.NewTrustRepository(db),
		sink:    sink,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

type OpenThreadParams struct {
	Site              string
	AuthorID          int64
	AnchorKind        int16
	AnchorID          string
	Title             string
	ContentRating     int16
	BodyRaw           string
	HeaderImageHashes datatypes.JSON
}

func (s *ThreadService) OpenTopic(ctx context.Context, p OpenThreadParams) (*model.CommunityThread, *model.CommunityPost, error) {
	return s.openWithFirstPost(ctx, model.ThreadKindTopic, p)
}

func (s *ThreadService) OpenFeedback(ctx context.Context, p OpenThreadParams) (*model.CommunityThread, *model.CommunityPost, error) {
	return s.openWithFirstPost(ctx, model.ThreadKindFeedback, p)
}

func (s *ThreadService) openWithFirstPost(ctx context.Context, kind int16, p OpenThreadParams) (*model.CommunityThread, *model.CommunityPost, error) {
	cooked := sanitize.Cook(p.BodyRaw)
	level, err := trustLevel(s.trusts, p.AuthorID)
	if err != nil {
		return nil, nil, err
	}
	if err := checkContentSandbox(level, cooked); err != nil {
		return nil, nil, err
	}
	if isSandboxed(level) {
		n, err := s.threads.CountOpenedByCreatorSince(p.AuthorID, time.Now().Add(-time.Duration(keys.CommunitySandboxWindowHours.Get())*time.Hour))
		if err != nil {
			return nil, nil, err
		}
		if n >= keys.CommunitySandboxMaxTopicsPerDay.Get() {
			return nil, nil, &SandboxError{Reason: "daily topic limit"}
		}
	}

	checkText := p.BodyRaw
	if p.Title != "" {
		checkText = p.Title + "\n\n" + p.BodyRaw
	}
	suspectHold := false
	switch s.check.Decision(ctx, p.Site, checkText, &p.AuthorID) {
	case checkDeny:
		return nil, nil, ErrContentBlocked
	case checkHold:
		suspectHold = true
	}

	now := time.Now()
	var thread model.CommunityThread
	var post model.CommunityPost
	var enqueuedItemID int64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		trust, err := repository.GetOrCreateTrustTx(tx, p.AuthorID)
		if err != nil {
			return err
		}
		held := trust.FirstPostsHeldRemaining > 0

		var title *string
		if p.Title != "" {
			title = &p.Title
		}
		thread = model.CommunityThread{
			Site: p.Site, Kind: kind, AnchorKind: p.AnchorKind, AnchorID: p.AnchorID,
			Title: title, HeaderImageHashes: p.HeaderImageHashes,
			ContentRating: p.ContentRating, Status: model.ThreadStatusOpen,
			PostsCount: 1, ParticipantsCount: 1, HighestPostNumber: 1, LastPostedAt: &now,
			CreatedBy: p.AuthorID,
		}
		if kind == model.ThreadKindFeedback {
			open := model.FeedbackStatusOpen
			thread.FbStatus = &open
		}
		if err := repository.CreateThreadTx(tx, &thread, false); err != nil {
			return err
		}

		post = model.CommunityPost{
			ThreadID: thread.ID, PostNumber: 1, AuthorID: p.AuthorID,
			ContentRaw: p.BodyRaw, ContentHTML: cooked.HTML, SanitizerVersion: int32(cooked.Version),
			ContentRating: p.ContentRating, Status: postStatus(held),
		}
		if err := repository.CreatePostTx(tx, &post); err != nil {
			return err
		}
		if err := repository.EnsureSubscribedTx(tx, thread.ID, p.AuthorID, post.PostNumber); err != nil {
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
		return nil, nil, err
	}
	s.sink.Emit(Event{Kind: EventPostCreated, ThreadID: thread.ID, PostID: post.ID, ActorID: p.AuthorID})
	if enqueuedItemID != 0 {
		s.sink.Emit(Event{Kind: EventReviewEnqueued, ThreadID: thread.ID, PostID: post.ID, ReviewItemID: enqueuedItemID})
	}
	return &thread, &post, nil
}

type CommentsThreadParams struct {
	Site          string
	AnchorKind    int16
	AnchorID      string
	ContentRating int16
	ActorID       int64
}

// FindCommentsThread is the read half of an anchor's comment wall: nil until
// someone comments. Only the deprecated resolve face still mints a thread from
// a read (see GetOrCreateCommentsThread).
func (s *ThreadService) FindCommentsThread(site string, anchorKind int16, anchorID string) (*model.CommunityThread, error) {
	return s.threads.GetLiveCommentsThread(site, anchorKind, anchorID)
}

func (s *ThreadService) GetOrCreateCommentsThread(ctx context.Context, p CommentsThreadParams) (*model.CommunityThread, error) {
	if t, err := s.threads.GetLiveCommentsThread(p.Site, p.AnchorKind, p.AnchorID); err != nil {
		return nil, err
	} else if t != nil {
		return t, nil
	}
	thread := model.CommunityThread{
		Site: p.Site, Kind: model.ThreadKindComments, AnchorKind: p.AnchorKind, AnchorID: p.AnchorID,
		ContentRating: p.ContentRating, Status: model.ThreadStatusOpen,
		PostsCount: 0, ParticipantsCount: 0, HighestPostNumber: 0,
		CreatedBy: p.ActorID,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return repository.CreateThreadTx(tx, &thread, true)
	})
	if err == nil {
		return &thread, nil
	}
	if isDuplicate(err) {
		if t, e := s.threads.GetLiveCommentsThread(p.Site, p.AnchorKind, p.AnchorID); e == nil && t != nil {
			return t, nil
		}
	}
	return nil, err
}

func (s *ThreadService) Get(id int64) (*model.CommunityThread, error) {
	return s.threads.GetByID(id)
}

func (s *ThreadService) List(q repository.ThreadListQuery) ([]model.CommunityThread, error) {
	q.Limit = clampLimit(q.Limit)
	return s.threads.List(q)
}

func (s *ThreadService) ListByAnchor(site string, anchorKind int16, anchorID string, kind int16) ([]model.CommunityThread, error) {
	return s.threads.ListByAnchor(site, anchorKind, anchorID, kind)
}

func (s *ThreadService) OpeningPostMeta(threadIDs []int64) (map[int64]repository.OpeningPostMeta, error) {
	return s.threads.OpeningPostMetaByThreadIDs(threadIDs)
}

func postStatus(held bool) int16 {
	if held {
		return model.PostStatusHidden
	}
	return model.PostStatusVisible
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 50
	}
	return limit
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key")
}
