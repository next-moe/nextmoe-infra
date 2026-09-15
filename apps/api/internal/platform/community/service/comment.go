package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

// CommentParams addresses the conversation by its ANCHOR rather than by a
// thread id, because the comments thread is created by the first comment. It
// used to be created by the first *view* (the resolve read face), which left
// 110,918 of kungal's 114,070 comments threads holding nothing at all.
type CommentParams struct {
	Site          string
	AnchorKind    int16
	AnchorID      string
	ContentRating int16
	AuthorID      int64
	BodyRaw       string
	RootPostID    *int64
	ReplyToPostID *int64
	TargetUserID  *int64
}

func (s *PostService) Comment(ctx context.Context, p CommentParams) (*model.CommunityThread, *model.CommunityPost, error) {
	draft, err := s.draftPost(ctx, p.Site, p.AuthorID, p.BodyRaw)
	if err != nil {
		return nil, nil, err
	}
	draft.rootPostID, draft.replyToPostID, draft.targetUserID = p.RootPostID, p.ReplyToPostID, p.TargetUserID

	var thread *model.CommunityThread
	var written writtenPost
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		found, err := commentsThreadForWriteTx(tx, p)
		if err != nil {
			return err
		}
		if written, err = appendPostTx(tx, found, draft); err != nil {
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
	if err != nil {
		return nil, nil, err
	}
	s.emitWrite(thread.ID, p.AuthorID, written)
	return thread, &written.post, nil
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
