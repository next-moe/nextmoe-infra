package handler

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerFollows(api huma.API) {
	tags := []string{"community-follows"}
	huma.Register(api, huma.Operation{OperationID: "followUser", Method: http.MethodPut, Path: "/api/v1/community/users/{id}/following/{target_id}",
		Summary: "Follow a user network-wide (idempotent; created says whether the follow is new)", Tags: tags}, s.followUser)
	huma.Register(api, huma.Operation{OperationID: "unfollowUser", Method: http.MethodDelete, Path: "/api/v1/community/users/{id}/following/{target_id}",
		Summary: "Stop following a user (idempotent; deleted says whether a follow was removed)", Tags: tags}, s.unfollowUser)
	huma.Register(api, huma.Operation{OperationID: "listFollowers", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/followers",
		Summary: "List who follows a user, newest follow first", Tags: tags}, s.listFollowers)
	huma.Register(api, huma.Operation{OperationID: "listFollowing", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/following",
		Summary: "List who a user follows, newest follow first", Tags: tags}, s.listFollowing)
	huma.Register(api, huma.Operation{OperationID: "followStates", Method: http.MethodPost, Path: "/api/v1/community/follows/states",
		Summary: "Batch follower/following counts and the viewer's relation to up to 100 users", Tags: tags}, s.followStates)
}

type followUserInput struct {
	ID       int64 `path:"id" minimum:"1"`
	TargetID int64 `path:"target_id" minimum:"1"`
}
type followUserOutput struct {
	Body Envelope[dto.FollowResult]
}

func (s *Server) followUser(ctx context.Context, in *followUserInput) (*followUserOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	created, err := s.follows.Follow(ctx, site, in.ID, in.TargetID)
	if err != nil {
		return nil, mapErr("follow user", err)
	}
	return &followUserOutput{Body: okEnvelope(dto.FollowResult{
		FollowerID: in.ID, FolloweeID: in.TargetID, Following: true, Created: created,
	})}, nil
}

type unfollowUserOutput struct {
	Body Envelope[dto.UnfollowResult]
}

func (s *Server) unfollowUser(ctx context.Context, in *followUserInput) (*unfollowUserOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	deleted, err := s.follows.Unfollow(ctx, in.ID, in.TargetID)
	if err != nil {
		return nil, mapErr("unfollow user", err)
	}
	return &unfollowUserOutput{Body: okEnvelope(dto.UnfollowResult{
		FollowerID: in.ID, FolloweeID: in.TargetID, Following: false, Deleted: deleted,
	})}, nil
}

type listFollowsInput struct {
	ID     int64  `path:"id" minimum:"1"`
	Cursor string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit  int    `query:"limit" doc:"page size (max 100, default 50)"`
}
type listFollowsOutput struct {
	Body Envelope[dto.FollowListResponse]
}

func (s *Server) listFollowers(ctx context.Context, in *listFollowsInput) (*listFollowsOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	beforeID, he := parseAnchorCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	limit := clampLimit(in.Limit)
	rows, err := s.follows.ListFollowers(in.ID, beforeID, limit)
	if err != nil {
		return nil, mapErr("list followers", err)
	}
	return &listFollowsOutput{Body: okEnvelope(dto.FollowListResponse{
		Users: toFollowViews(rows, true), NextCursor: followCursor(rows, limit),
	})}, nil
}

func (s *Server) listFollowing(ctx context.Context, in *listFollowsInput) (*listFollowsOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	beforeID, he := parseAnchorCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	limit := clampLimit(in.Limit)
	rows, err := s.follows.ListFollowing(in.ID, beforeID, limit)
	if err != nil {
		return nil, mapErr("list following", err)
	}
	return &listFollowsOutput{Body: okEnvelope(dto.FollowListResponse{
		Users: toFollowViews(rows, false), NextCursor: followCursor(rows, limit),
	})}, nil
}

type followStatesInput struct{ Body dto.FollowStatesRequest }
type followStatesOutput struct {
	Body Envelope[dto.FollowStatesResponse]
}

func (s *Server) followStates(ctx context.Context, in *followStatesInput) (*followStatesOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	rows, err := s.follows.States(in.Body.ViewerID, in.Body.UserIDs)
	if err != nil {
		return nil, mapErr("follow states", err)
	}
	return &followStatesOutput{Body: okEnvelope(dto.FollowStatesResponse{States: toFollowStateViews(rows)})}, nil
}

func toFollowViews(rows []model.CommunityUserFollow, followers bool) []dto.FollowView {
	out := make([]dto.FollowView, len(rows))
	for i := range rows {
		id := rows[i].FolloweeID
		if followers {
			id = rows[i].FollowerID
		}
		out[i] = dto.FollowView{UserID: id, FollowedAt: rows[i].CreatedAt}
	}
	return out
}

func toFollowStateViews(rows []service.FollowState) []dto.FollowStateView {
	out := make([]dto.FollowStateView, len(rows))
	for i, st := range rows {
		out[i] = dto.FollowStateView{
			UserID: st.UserID, FollowersCount: st.FollowersCount, FollowingCount: st.FollowingCount,
			ViewerFollows: st.ViewerFollows, FollowsViewer: st.FollowsViewer,
		}
	}
	return out
}

func followCursor(rows []model.CommunityUserFollow, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return strconv.FormatInt(rows[len(rows)-1].ID, 10)
}
