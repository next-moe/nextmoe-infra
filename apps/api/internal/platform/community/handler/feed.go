package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/pkg/errors"
)

type sitePostsInput struct {
	Kind        int16  `query:"kind" default:"-1" doc:"thread kind filter (0=topic 1=comments 2=feedback); -1 = every kind"`
	AnchorKind  int16  `query:"anchor_kind" default:"-1" doc:"anchor kind filter (0-4); -1 = every anchor kind"`
	AnchorID    string `query:"anchor_id" doc:"optional: narrow to a single anchor (requires anchor_kind)"`
	RepliesOnly bool   `query:"replies_only" doc:"skip opening posts (post_number = 1), leaving only replies"`
	Cursor      string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit       int    `query:"limit" doc:"page size (max 100, default 50)"`
	ViewerID    int64  `query:"viewer_id" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}

type sitePostsOutput struct {
	Body Envelope[dto.PostFeedResponse]
}

func (s *Server) listSitePosts(ctx context.Context, in *sitePostsInput) (*sitePostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	cursor, err := decodePostCursor(in.Cursor)
	if err != nil {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	limit := clampLimit(in.Limit)
	rows, err := s.posts.SiteFeed(repository.PostFeedQuery{
		Site: site, Kind: in.Kind, AnchorKind: in.AnchorKind, AnchorID: in.AnchorID,
		RepliesOnly: in.RepliesOnly, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return nil, mapErr("list site posts", err)
	}
	views := toAuthorPostViews(rows)
	if err := s.hydrateAuthorPostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate feed reactions", err)
	}
	return &sitePostsOutput{Body: okEnvelope(dto.PostFeedResponse{
		Posts: views, NextCursor: postFeedPageCursor(rows, limit),
	})}, nil
}
