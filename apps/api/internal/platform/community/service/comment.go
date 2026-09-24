package service

import (
	"context"
	"errors"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

// CommentParams addresses the conversation by its ANCHOR rather than by a
// thread id, because the comments thread is created by the first comment. It
// used to be created by the first *view* (the resolve read face), which left
// 110,918 of kungal's 114,070 comments threads holding nothing at all.
type CommentParams struct {
	Site           string
	AnchorKind     int16
	AnchorID       string
	ContentRating  int16
	AuthorID       int64
	BodyRaw        string
	RootPostID     *int64
	ReplyToPostID  *int64
	TargetUserID   *int64
	MentionUserIDs []int64
}

type CommentResult struct {
	Thread   *model.CommunityThread
	Post     *model.CommunityPost
	Replayed bool
}

func (s *PostService) Comment(ctx context.Context, p CommentParams) (*model.CommunityThread, *model.CommunityPost, error) {
	r, err := s.WriteComment(ctx, p, WriteKey{})
	return r.Thread, r.Post, err
}

func (s *PostService) WriteComment(ctx context.Context, p CommentParams, k WriteKey) (CommentResult, error) {
	if r, err := s.replayComment(ctx, p.Site, k); r.Replayed || err != nil {
		return r, err
	}
	draft, err := s.draftPost(ctx, p.Site, p.AuthorID, p.BodyRaw)
	if err != nil {
		return CommentResult{}, err
	}
	mentions, err := normalizeMentionIDs(p.AuthorID, p.MentionUserIDs)
	if err != nil {
		return CommentResult{}, err
	}
	draft.rootPostID, draft.replyToPostID, draft.targetUserID = p.RootPostID, p.ReplyToPostID, p.TargetUserID
	draft.mentionUserIDs = mentions

	var thread *model.CommunityThread
	var written writtenPost
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		requestID, err := claimWriteKeyTx(tx, p.Site, k)
		if err != nil {
			return err
		}
		found, err := commentsThreadForWriteTx(tx, p)
		if err != nil {
			return err
		}
		if written, err = appendPostTx(tx, found, draft); err != nil {
			return err
		}
		if err := bindWriteKeyTx(tx, requestID, written.post.ID); err != nil {
			return err
		}
		// The counters moved in SQL, so the row found (or just inserted) above
		// is already stale; report the thread as it now stands.
		if thread, err = repository.GetThreadTx(tx, found.ID); err != nil {
			return err
		}
		if thread == nil {
			return ErrThreadNotFound
		}
		return nil
	})
	if errors.Is(err, errWriteKeyTaken) {
		r, err := s.replayComment(ctx, p.Site, k)
		if err == nil && !r.Replayed {
			err = ErrPostNotFound
		}
		return r, err
	}
	if err != nil {
		return CommentResult{}, err
	}
	s.emitWrite(thread.ID, p.AuthorID, written)
	return CommentResult{Thread: thread, Post: &written.post}, nil
}

func (s *PostService) replayComment(ctx context.Context, site string, k WriteKey) (CommentResult, error) {
	post, err := s.keyedPost(ctx, site, k)
	if post == nil || err != nil {
		return CommentResult{}, err
	}
	thread, err := repository.GetThreadTx(s.db.WithContext(ctx), post.ThreadID)
	if err != nil {
		return CommentResult{}, err
	}
	if thread == nil {
		return CommentResult{}, ErrThreadNotFound
	}
	return CommentResult{Thread: thread, Post: post, Replayed: true}, nil
}

func commentsThreadForWriteTx(tx *gorm.DB, p CommentParams) (*model.CommunityThread, error) {
	existing, err := repository.GetLiveCommentsThreadTx(tx, p.Site, p.AnchorKind, p.AnchorID)
	if err != nil || existing != nil {
		return existing, err
	}
	thread := &model.CommunityThread{
		Site: p.Site, Kind: model.ThreadKindComments, AnchorKind: p.AnchorKind, AnchorID: p.AnchorID,
		ContentRating: p.ContentRating, Status: model.ThreadStatusOpen,
		CreatedBy: p.AuthorID,
	}
	created, err := repository.CreateCommentsThreadIfAbsentTx(tx, thread)
	if err != nil {
		return nil, err
	}
	if created {
		return thread, nil
	}
	winner, err := repository.GetLiveCommentsThreadTx(tx, p.Site, p.AnchorKind, p.AnchorID)
	if err != nil {
		return nil, err
	}
	if winner == nil {
		return nil, ErrThreadNotFound
	}
	return winner, nil
}
