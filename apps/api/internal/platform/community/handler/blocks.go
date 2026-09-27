package handler

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerBlocks(api huma.API) {
	tags := []string{"community-blocks"}
	huma.Register(api, huma.Operation{OperationID: "blockUser", Method: http.MethodPut, Path: "/api/v1/community/users/{id}/blocking/{target_id}",
		Summary: "Block a user network-wide (idempotent); also removes the follows between the two, and neither can follow the other while it stands", Tags: tags}, s.blockUser)
	huma.Register(api, huma.Operation{OperationID: "unblockUser", Method: http.MethodDelete, Path: "/api/v1/community/users/{id}/blocking/{target_id}",
		Summary: "Remove a block (idempotent; deleted says whether a block was removed); removed follows are not restored", Tags: tags}, s.unblockUser)
	huma.Register(api, huma.Operation{OperationID: "listBlocking", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/blocking",
		Summary: "List who a user has blocked, newest block first; only the blocker's own list exists (who blocked me is never listed)", Tags: tags}, s.listBlocking)
}

type blockUserOutput struct {
	Body Envelope[dto.BlockResult]
}

func (s *Server) blockUser(ctx context.Context, in *followUserInput) (*blockUserOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	created, err := s.follows.Block(ctx, site, in.ID, in.TargetID)
	if err != nil {
		return nil, mapErr("block user", err)
	}
	return &blockUserOutput{Body: okEnvelope(dto.BlockResult{
		BlockerID: in.ID, BlockedID: in.TargetID, Blocking: true, Created: created,
	})}, nil
}

type unblockUserOutput struct {
	Body Envelope[dto.UnblockResult]
}

func (s *Server) unblockUser(ctx context.Context, in *followUserInput) (*unblockUserOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	deleted, err := s.follows.Unblock(ctx, in.ID, in.TargetID)
	if err != nil {
		return nil, mapErr("unblock user", err)
	}
	return &unblockUserOutput{Body: okEnvelope(dto.UnblockResult{
		BlockerID: in.ID, BlockedID: in.TargetID, Blocking: false, Deleted: deleted,
	})}, nil
}

type listBlockingOutput struct {
	Body Envelope[dto.BlockListResponse]
}

func (s *Server) listBlocking(ctx context.Context, in *listFollowsInput) (*listBlockingOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	beforeID, he := parseAnchorCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	limit := clampLimit(in.Limit)
	rows, err := s.follows.ListBlocking(in.ID, beforeID, limit)
	if err != nil {
		return nil, mapErr("list blocking", err)
	}
	users := make([]dto.BlockView, len(rows))
	for i := range rows {
		users[i] = dto.BlockView{UserID: rows[i].BlockedID, BlockedAt: rows[i].CreatedAt}
	}
	return &listBlockingOutput{Body: okEnvelope(dto.BlockListResponse{
		Users: users, NextCursor: blockCursor(rows, limit),
	})}, nil
}

func blockCursor(rows []model.CommunityUserBlock, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return strconv.FormatInt(rows[len(rows)-1].ID, 10)
}
