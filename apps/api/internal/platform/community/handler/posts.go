package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/pkg/errors"
)

type resolvePostsInput struct{ Body dto.PostsResolveRequest }
type resolvePostsOutput struct {
	Body Envelope[dto.PostsResolveResponse]
}

func (s *Server) resolvePosts(ctx context.Context, in *resolvePostsInput) (*resolvePostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ids, he := dedupeIDs(in.Body.IDs)
	if he != nil {
		return nil, he
	}
	rows, err := s.posts.ResolvePosts(site, ids)
	if err != nil {
		return nil, mapErr("resolve posts", err)
	}
	views := orderResolvedPosts(rows, ids)
	if err := s.hydrateAuthorPostReactions(in.Body.ViewerID, views); err != nil {
		return nil, mapErr("hydrate resolved reactions", err)
	}
	return &resolvePostsOutput{Body: okEnvelope(dto.PostsResolveResponse{
		Posts: views,
	})}, nil
}

func dedupeIDs(ids []int64) ([]int64, *houseError) {
	if len(ids) > 100 {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "too many ids (max 100)")
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func orderResolvedPosts(rows []repository.AuthorPostRow, ids []int64) []dto.AuthorPostView {
	byID := make(map[int64]*repository.AuthorPostRow, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	out := make([]dto.AuthorPostView, 0, len(rows))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, authorPostView(row))
		}
	}
	return out
}
