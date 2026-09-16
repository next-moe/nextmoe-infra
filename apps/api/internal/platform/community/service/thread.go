package service

import (
	"context"
	"strconv"
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
	boards  *repository.BoardRepository
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
		boards:  repository.NewBoardRepository(db),
		sink:    sink,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

type OpenTopicParams struct {
	Site              string
	AuthorID          int64
	BoardID           int64
	LegacyBoardKey    string
	AsModerator       bool
	Title             string
	ContentRating     int16
	BodyRaw           string
	HeaderImageHashes datatypes.JSON
}

type OpenFeedbackParams struct {
	Site          string
	AuthorID      int64
	AnchorKind    int16
	AnchorID      string
	Title         string
	ContentRating int16
	BodyRaw       string
}

func (s *ThreadService) OpenTopic(ctx context.Context, p OpenTopicParams) (*model.CommunityThread, *model.CommunityPost, error) {
	board, err := s.findBoard(p.Site, p.BoardID, p.LegacyBoardKey)
	if err != nil {
		return nil, nil, err
	}
	level, err := trustLevel(s.trusts, p.AuthorID)
	if err != nil {
		return nil, nil, err
	}
	if err := topicGate(board, level, p.AsModerator); err != nil {
		return nil, nil, err
	}
	return s.openWithFirstPost(ctx, openThread{
		kind: model.ThreadKindTopic, site: p.Site, authorID: p.AuthorID, level: level,
		anchorKind: model.AnchorKindBoard, anchorID: model.BoardAnchorID(board.ID),
		title: p.Title, contentRating: max(p.ContentRating, board.ContentRating),
		bodyRaw: p.BodyRaw, headerImageHashes: p.HeaderImageHashes,
		lockAnchorTx: func(tx *gorm.DB) error {
			locked, err := repository.LockBoardTx(tx, p.Site, board.ID, repository.LockShare)
			if err != nil {
				return err
			}
			if locked == nil {
				return ErrBoardNotFound
			}
			return topicGate(locked, level, p.AsModerator)
		},
	})
}

func (s *ThreadService) OpenFeedback(ctx context.Context, p OpenFeedbackParams) (*model.CommunityThread, *model.CommunityPost, error) {
	level, err := trustLevel(s.trusts, p.AuthorID)
	if err != nil {
		return nil, nil, err
	}
	return s.openWithFirstPost(ctx, openThread{
		kind: model.ThreadKindFeedback, site: p.Site, authorID: p.AuthorID, level: level,
		anchorKind: p.AnchorKind, anchorID: p.AnchorID,
		title: p.Title, contentRating: p.ContentRating, bodyRaw: p.BodyRaw,
	})
}

func (s *ThreadService) findBoard(site string, id int64, key string) (*model.CommunityBoard, error) {
	var b *model.CommunityBoard
	var err error
	switch {
	case id > 0:
		b, err = s.boards.Get(site, id)
	case key == "":
		return nil, &InvalidError{Reason: "board_id is required"}
	default:
		if n, perr := strconv.ParseInt(key, 10, 64); perr == nil {
			b, err = s.boards.Get(site, n)
		} else {
			b, err = s.boards.GetBySlug(site, key)
		}
	}
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrBoardNotFound
	}
	return b, nil
}

type openThread struct {
	kind              int16
	site              string
	authorID          int64
	level             int16
	anchorKind        int16
	anchorID          string
	title             string
	contentRating     int16
	bodyRaw           string
	headerImageHashes datatypes.JSON
	lockAnchorTx      func(tx *gorm.DB) error
}

func (s *ThreadService) openWithFirstPost(ctx context.Context, p openThread) (*model.CommunityThread, *model.CommunityPost, error) {
	cooked := sanitize.Cook(p.bodyRaw)
	if err := checkContentSandbox(p.level, cooked); err != nil {
		return nil, nil, err
	}
	if isSandboxed(p.level) {
		n, err := s.threads.CountOpenedByCreatorSince(p.authorID, time.Now().Add(-time.Duration(keys.CommunitySandboxWindowHours.Get())*time.Hour))
		if err != nil {
			return nil, nil, err
		}
		if n >= keys.CommunitySandboxMaxTopicsPerDay.Get() {
			return nil, nil, &SandboxError{Reason: "daily topic limit"}
		}
	}

	checkText := p.bodyRaw
	if p.title != "" {
		checkText = p.title + "\n\n" + p.bodyRaw
	}
	suspectHold := false
	switch s.check.Decision(ctx, p.site, checkText, &p.authorID) {
	case checkDeny:
		return nil, nil, ErrContentBlocked
	case checkHold:
		suspectHold = true
	}

	now := time.Now()
	var thread model.CommunityThread
	var post model.CommunityPost
	var enqueuedItemID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if p.lockAnchorTx != nil {
			if err := p.lockAnchorTx(tx); err != nil {
				return err
			}
		}
		trust, err := repository.GetOrCreateTrustTx(tx, p.authorID)
		if err != nil {
			return err
		}
		held := trust.FirstPostsHeldRemaining > 0

		var title *string
		if p.title != "" {
			title = &p.title
		}
		thread = model.CommunityThread{
			Site: p.site, Kind: p.kind, AnchorKind: p.anchorKind, AnchorID: p.anchorID,
			Title: title, HeaderImageHashes: p.headerImageHashes,
			ContentRating: p.contentRating, Status: model.ThreadStatusOpen,
			PostsCount: 1, ParticipantsCount: 1, HighestPostNumber: 1, LastPostedAt: &now,
			CreatedBy: p.authorID,
		}
		if p.kind == model.ThreadKindFeedback {
			open := model.FeedbackStatusOpen
			thread.FbStatus = &open
		}
		if err := repository.CreateThreadTx(tx, &thread, false); err != nil {
			return err
		}

		post = model.CommunityPost{
			ThreadID: thread.ID, PostNumber: 1, AuthorID: p.authorID,
			ContentRaw: p.bodyRaw, ContentHTML: cooked.HTML, SanitizerVersion: int32(cooked.Version),
			ContentRating: p.contentRating, Status: postStatus(held),
		}
		if err := repository.CreatePostTx(tx, &post); err != nil {
			return err
		}
		if err := repository.EnsureSubscribedTx(tx, thread.ID, p.authorID, post.PostNumber); err != nil {
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
			return repository.DecrementHoldTx(tx, p.authorID)
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
	s.sink.Emit(Event{Kind: EventPostCreated, ThreadID: thread.ID, PostID: post.ID, ActorID: p.authorID})
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
