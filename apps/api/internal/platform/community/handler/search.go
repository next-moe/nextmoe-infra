package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/pkg/errors"
)

type searchInput struct {
	Q      string `query:"q" doc:"substring to look for, 2-100 characters; matched case-insensitively against the markdown source (posts) or the title (threads)"`
	Kind   int16  `query:"kind" default:"-1" doc:"thread kind filter (0=topic 1=comments 2=feedback); -1 = every kind"`
	Cursor string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit  int    `query:"limit" doc:"page size (max 100, default 50)"`
}

func (s *Server) searchPosts(ctx context.Context, in *searchInput) (*sitePostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	cursor, err := decodePostCursor(in.Cursor)
	if err != nil {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	limit := clampLimit(in.Limit)
	rows, err := s.search.Posts(repository.SearchQuery{
		Site: site, Q: in.Q, Kind: in.Kind, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return nil, mapErr("search posts", err)
	}
	return &sitePostsOutput{Body: okEnvelope(dto.PostFeedResponse{
		Posts: toAuthorPostViews(rows), NextCursor: postFeedPageCursor(rows, limit),
	})}, nil
}

func (s *Server) searchThreads(ctx context.Context, in *searchInput) (*threadListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	cursor, err := decodeThreadCursor(in.Cursor)
	if err != nil || (cursor.ID != 0 && cursor.Sort != repository.ThreadSortCreated) {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	limit := clampLimit(in.Limit)
	threads, err := s.search.Threads(repository.SearchQuery{
		Site: site, Q: in.Q, Kind: in.Kind,
		Cursor: repository.TimeCursor{CreatedAt: cursor.Created, ID: cursor.ID}, Limit: limit,
	})
	if err != nil {
		return nil, mapErr("search threads", err)
	}
	ids := make([]int64, len(threads))
	for i := range threads {
		ids[i] = threads[i].ID
	}
	// A held opening post must not leak its title here either: the caller hides
	// the row the same way it does on the thread listing.
	metas, err := s.threads.OpeningPostMeta(ids)
	if err != nil {
		return nil, mapErr("search threads openings", err)
	}
	return &threadListOutput{Body: okEnvelope(dto.ThreadListResponse{
		Threads:    toThreadViewsWithOpening(threads, metas),
		NextCursor: threadsPageCursor(threads, repository.ThreadSortCreated, limit),
	})}, nil
}
