package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/pkg/errors"
)

type authorPostsInput struct {
	ID         int64 `path:"id"`
	After      int64 `query:"after" doc:"post id to read after (descending; 0 = newest first)"`
	AnchorKind int16 `query:"anchor_kind" default:"-1" doc:"optional anchor-kind filter (0-4); -1 = all kinds"`
	Limit      int   `query:"limit" doc:"page size (default 20, max 100)"`
	ViewerID   int64 `query:"viewer_id" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}
type authorPostsOutput struct {
	Body Envelope[dto.AuthorPostsResponse]
}

func (s *Server) listAuthorPosts(ctx context.Context, in *authorPostsInput) (*authorPostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	limit := clampAuthorLimit(in.Limit)
	rows, err := s.posts.ListAuthorPosts(site, in.ID, in.After, in.AnchorKind, limit)
	if err != nil {
		return nil, mapErr("list author posts", err)
	}
	views := toAuthorPostViews(rows)
	if err := s.hydrateAuthorPostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate author reactions", err)
	}
	return &authorPostsOutput{Body: okEnvelope(dto.AuthorPostsResponse{
		Posts: views, NextCursor: authorPostsCursor(rows, limit),
	})}, nil
}

type authorStatsInput struct {
	IDs        string `query:"ids" doc:"comma-separated author ids (max 100)"`
	Kind       int16  `query:"kind" default:"-1" doc:"thread kind filter (0=topic 1=comments 2=feedback); -1 = every kind"`
	AnchorKind int16  `query:"anchor_kind" default:"-1" doc:"anchor kind filter (0-4); -1 = every anchor kind"`
}
type authorStatsOutput struct {
	Body Envelope[dto.AuthorStatsResponse]
}

func (s *Server) authorStats(ctx context.Context, in *authorStatsInput) (*authorStatsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ids, he := parseAuthorIDs(in.IDs)
	if he != nil {
		return nil, he
	}
	counts, err := s.posts.AuthorStats(site, ids, in.Kind, in.AnchorKind)
	if err != nil {
		return nil, mapErr("author stats", err)
	}
	stats := make([]dto.AuthorStat, 0, len(ids))
	for _, id := range ids {
		stats = append(stats, dto.AuthorStat{AuthorID: id, VisiblePosts: counts[id]})
	}
	return &authorStatsOutput{Body: okEnvelope(dto.AuthorStatsResponse{Stats: stats})}, nil
}

type topAuthorsInput struct {
	Kind       int16 `query:"kind" default:"-1" doc:"thread kind filter (0=topic 1=comments 2=feedback); -1 = every kind"`
	AnchorKind int16 `query:"anchor_kind" default:"-1" doc:"anchor kind filter (0-4); -1 = every anchor kind"`
	Limit      int   `query:"limit" doc:"how many authors (default 50, max 100)"`
}

func (s *Server) topAuthors(ctx context.Context, in *topAuthorsInput) (*authorStatsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	rows, err := s.posts.TopAuthors(site, in.Kind, in.AnchorKind, clampLimit(in.Limit))
	if err != nil {
		return nil, mapErr("top authors", err)
	}
	stats := make([]dto.AuthorStat, 0, len(rows))
	for _, row := range rows {
		stats = append(stats, dto.AuthorStat{AuthorID: row.AuthorID, VisiblePosts: row.VisiblePosts})
	}
	return &authorStatsOutput{Body: okEnvelope(dto.AuthorStatsResponse{Stats: stats})}, nil
}

type authorPurgeInput struct {
	ID int64 `path:"id"`
}
type authorPurgeOutput struct {
	Body Envelope[dto.PurgeResponse]
}

func (s *Server) purgeAuthor(ctx context.Context, in *authorPurgeInput) (*authorPurgeOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	res, err := s.posts.PurgeAuthor(ctx, site, in.ID)
	if err != nil {
		return nil, mapErr("purge author", err)
	}
	return &authorPurgeOutput{Body: okEnvelope(dto.PurgeResponse{
		PostsPurged: res.PostsPurged, ReactionsDeleted: res.ReactionsDeleted,
		ReadStatesDeleted:          res.ReadStatesDeleted,
		AnchorSubscriptionsDeleted: res.AnchorSubscriptionsDeleted,
		NotificationsDeleted:       res.NotificationsDeleted,
		ActivitiesDeleted:          res.ActivitiesDeleted,
	})}, nil
}

type authorRestoreOutput struct {
	Body Envelope[dto.RestoreResponse]
}

func (s *Server) restoreAuthor(ctx context.Context, in *authorPurgeInput) (*authorRestoreOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	res, err := s.posts.RestoreAuthor(ctx, site, in.ID)
	if err != nil {
		return nil, mapErr("restore author", err)
	}
	return &authorRestoreOutput{Body: okEnvelope(dto.RestoreResponse{
		PostsRestored: res.PostsRestored, ReactionsRestored: res.ReactionsRestored,
		ReadStatesRestored:          res.ReadStatesRestored,
		AnchorSubscriptionsRestored: res.AnchorSubscriptionsRestored,
		NotificationsRestored:       res.NotificationsRestored,
	})}, nil
}

func clampAuthorLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func authorPostsCursor(rows []repository.AuthorPostRow, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return strconv.FormatInt(rows[len(rows)-1].ID, 10)
}

func authorPostView(row *repository.AuthorPostRow) dto.AuthorPostView {
	return dto.AuthorPostView{
		Post: toPostView(&row.CommunityPost),
		Thread: dto.PostThreadContext{
			ThreadID:   row.ThreadID,
			Title:      row.ThreadTitle,
			AnchorKind: row.ThreadAnchorKind,
			AnchorID:   row.ThreadAnchorID,
			BoardID:    boardIDOf(row.ThreadAnchorKind, row.ThreadAnchorID),
		},
	}
}

func toAuthorPostViews(rows []repository.AuthorPostRow) []dto.AuthorPostView {
	out := make([]dto.AuthorPostView, len(rows))
	for i := range rows {
		out[i] = authorPostView(&rows[i])
	}
	return out
}

func parseAuthorIDs(raw string) ([]int64, *houseError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[int64]bool, len(parts))
	ids := make([]int64, 0, len(parts))
	total := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed author id: "+p)
		}
		total++
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if total > 100 {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "too many ids (max 100)")
	}
	return ids, nil
}
