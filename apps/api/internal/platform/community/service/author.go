package service

import (
	"context"
	"log/slog"

	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type PurgeResult struct {
	PostsPurged       int64
	ReactionsDeleted  int64
	ReadStatesDeleted int64
}

func (s *PostService) ListAuthorPosts(site string, authorID, after int64, anchorKind int16, limit int) ([]repository.AuthorPostRow, error) {
	return s.posts.ListAuthorVisiblePosts(site, authorID, after, anchorKind, limit)
}

func (s *PostService) AuthorStats(site string, authorIDs []int64, kind, anchorKind int16) (map[int64]int64, error) {
	return s.posts.CountAuthorVisiblePosts(site, authorIDs, kind, anchorKind)
}

func (s *PostService) TopAuthors(site string, kind, anchorKind int16, limit int) ([]repository.AuthorStatRow, error) {
	return s.posts.TopAuthors(site, kind, anchorKind, limit)
}

func (s *PostService) ResolvePosts(site string, ids []int64) ([]repository.AuthorPostRow, error) {
	return s.posts.ResolveVisiblePosts(site, ids)
}

func (s *PostService) PurgeAuthor(ctx context.Context, site string, authorID int64) (PurgeResult, error) {
	var res PurgeResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		posts, err := repository.PurgeAuthorPostsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		reactions, err := repository.DeleteAuthorReactionsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		readStates, err := repository.DeleteAuthorThreadUsersTx(tx, site, authorID)
		if err != nil {
			return err
		}
		res = PurgeResult{PostsPurged: posts, ReactionsDeleted: reactions, ReadStatesDeleted: readStates}
		return nil
	})
	if err != nil {
		return PurgeResult{}, err
	}
	slog.Info("community author purge", "site", site, "author_id", authorID,
		"posts_purged", res.PostsPurged, "reactions_deleted", res.ReactionsDeleted,
		"read_states_deleted", res.ReadStatesDeleted)
	return res, nil
}
