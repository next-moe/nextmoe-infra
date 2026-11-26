package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func (s *PostService) ListWall(q repository.WallQuery) ([]repository.WallRow, error) {
	q.Limit = clampLimit(q.Limit)
	return s.posts.ListWall(q)
}

func (s *PostService) VisibleReplyCounts(threadID int64, rootIDs []int64) (map[int64]int32, error) {
	return s.posts.VisibleReplyCounts(threadID, rootIDs)
}

func (s *PostService) ReplyPreviews(threadID int64, rootIDs []int64, n int) ([]model.CommunityPost, error) {
	return s.posts.ReplyPreviews(threadID, rootIDs, n)
}

func (s *PostService) PinnedPosts(site string, threadID int64) ([]repository.PinnedPost, error) {
	return repository.PinnedPosts(s.db, site, threadID)
}

func (s *PostService) Replies(ctx context.Context, rootID int64, afterNumber int32, limit int) ([]model.CommunityPost, error) {
	root, thread, err := s.reachablePost(ctx, s.db, rootID)
	if err != nil {
		return nil, err
	}
	if root.RootPostID != nil {
		return nil, &InvalidError{Reason: "replies are listed under a top-level post"}
	}
	return s.posts.ListReplies(thread.ID, root.ID, afterNumber, clampLimit(limit))
}

func (s *PostService) reachablePost(ctx context.Context, tx *gorm.DB, postID int64) (*model.CommunityPost, *model.CommunityThread, error) {
	post, err := repository.GetPostTx(tx, postID)
	if err != nil {
		return nil, nil, err
	}
	if post == nil {
		return nil, nil, ErrPostNotFound
	}
	thread, err := repository.GetThreadTx(tx, post.ThreadID)
	if err != nil {
		return nil, nil, err
	}
	if thread == nil || crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
		return nil, nil, ErrPostNotFound
	}
	return post, thread, nil
}

type PinResult struct {
	Post     *model.CommunityPost
	PinnedAt *time.Time
	Changed  bool
}

func (s *PostService) Pin(ctx context.Context, postID, actorID int64) (PinResult, error) {
	var out PinResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		post, thread, err := s.reachablePost(ctx, tx, postID)
		if err != nil {
			return err
		}
		if thread.Kind != model.ThreadKindComments {
			return &InvalidError{Reason: "only comments can be pinned"}
		}
		if post.RootPostID != nil {
			return &InvalidError{Reason: "only a top-level comment can be pinned"}
		}
		if post.Status != model.PostStatusVisible {
			return &ConflictError{Reason: "only a visible comment can be pinned"}
		}
		site := deliverySite(ctx, thread.Site)
		if _, err := repository.LockThreadTx(tx, thread.ID); err != nil {
			return err
		}
		out.Post = post
		existing, err := repository.GetPostPinTx(tx, site, post.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			out.PinnedAt = &existing.PinnedAt
			return nil
		}
		pinned, err := repository.PinnedPosts(tx, site, thread.ID)
		if err != nil {
			return err
		}
		if len(pinned) >= model.PostPinLimit {
			return &ConflictError{Reason: fmt.Sprintf("pin limit reached (max %d per wall)", model.PostPinLimit)}
		}
		pin := model.CommunityPostPin{Site: site, PostID: post.ID, PinnedBy: actorID, PinnedAt: time.Now()}
		if err := repository.CreatePostPinTx(tx, &pin); err != nil {
			return err
		}
		out.PinnedAt, out.Changed = &pin.PinnedAt, true
		return nil
	})
	if err == nil && out.Changed {
		slog.Info("community comment pinned", "post_id", postID, "thread_id", out.Post.ThreadID, "actor_id", actorID)
	}
	return out, err
}

func (s *PostService) Unpin(ctx context.Context, postID, actorID int64) (PinResult, error) {
	var out PinResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		post, thread, err := s.reachablePost(ctx, tx, postID)
		if err != nil {
			return err
		}
		out.Post = post
		out.Changed, err = repository.DeletePostPinTx(tx, deliverySite(ctx, thread.Site), post.ID)
		return err
	})
	if err == nil && out.Changed {
		slog.Info("community comment unpinned", "post_id", postID, "thread_id", out.Post.ThreadID, "actor_id", actorID)
	}
	return out, err
}

func (s *PostService) Restore(ctx context.Context, postID, actorID int64) (*model.CommunityPost, bool, error) {
	var post *model.CommunityPost
	changed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := s.reachablePost(ctx, tx, postID); err != nil {
			return err
		}
		p, err := repository.LockPostTx(tx, postID)
		if err != nil {
			return err
		}
		post = p
		switch {
		case p.Status == model.PostStatusVisible:
			return nil
		case p.Status == model.PostStatusHidden:
			return &ConflictError{Reason: "a hidden post is released through the review queue"}
		case !model.DeleteRestorable(p.DeleteSource):
			return &ConflictError{Reason: "only a post removed by a moderator or by review can be restored"}
		}
		if err := repository.SetPostStatusTx(tx, postID, model.PostStatusVisible); err != nil {
			return err
		}
		p.Status, p.DeleteSource, p.DeletedBy, changed = model.PostStatusVisible, model.DeleteSourceUnknown, nil, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		slog.Info("community mod restore", "post_id", postID, "thread_id", post.ThreadID,
			"post_author_id", post.AuthorID, "moderator_id", actorID)
	}
	return post, changed, nil
}
