package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerBoards(api huma.API) {
	tags := []string{"community-boards"}
	huma.Register(api, huma.Operation{OperationID: "listBoards", Method: http.MethodGet, Path: "/api/v1/community/boards",
		Summary: "List the site's boards in display order, with their topic stats", Tags: tags}, s.listBoards)
	huma.Register(api, huma.Operation{OperationID: "getBoard", Method: http.MethodGet, Path: "/api/v1/community/boards/{id}",
		Summary: "Get a board by id, with its topic stats", Tags: tags}, s.getBoard)
	huma.Register(api, huma.Operation{OperationID: "getBoardBySlug", Method: http.MethodGet, Path: "/api/v1/community/boards/by-slug/{slug}",
		Summary: "Get a board by its slug, with its topic stats", Tags: tags}, s.getBoardBySlug)
	huma.Register(api, huma.Operation{OperationID: "createBoard", Method: http.MethodPost, Path: "/api/v1/community/boards",
		Summary: "Create a board (top level, or under a top-level board)", Tags: tags}, s.createBoard)
	huma.Register(api, huma.Operation{OperationID: "updateBoard", Method: http.MethodPatch, Path: "/api/v1/community/boards/{id}",
		Summary: "Change a board's settings, parent or status", Tags: tags}, s.updateBoard)
	huma.Register(api, huma.Operation{OperationID: "deleteBoard", Method: http.MethodDelete, Path: "/api/v1/community/boards/{id}",
		Summary: "Delete a board that holds no topics and no sub-boards", Tags: tags}, s.deleteBoard)
	huma.Register(api, huma.Operation{OperationID: "reorderBoards", Method: http.MethodPost, Path: "/api/v1/community/boards/reorder",
		Summary: "Set the order of one parent's boards", Tags: tags}, s.reorderBoards)
}

type boardListOutput struct {
	Body Envelope[dto.BoardListResponse]
}

func (s *Server) listBoards(ctx context.Context, _ *struct{}) (*boardListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	boards, err := s.boards.List(site)
	if err != nil {
		return nil, mapErr("list boards", err)
	}
	views := make([]dto.BoardView, len(boards))
	for i := range boards {
		views[i] = toBoardView(&boards[i])
	}
	return &boardListOutput{Body: okEnvelope(dto.BoardListResponse{Boards: views})}, nil
}

type boardIDInput struct {
	ID int64 `path:"id"`
}
type boardOutput struct {
	Body Envelope[dto.BoardResponse]
}

func (s *Server) getBoard(ctx context.Context, in *boardIDInput) (*boardOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	return boardResult("get board", func() (*service.Board, error) { return s.boards.Get(site, in.ID) })
}

type boardSlugInput struct {
	Slug string `path:"slug"`
}

func (s *Server) getBoardBySlug(ctx context.Context, in *boardSlugInput) (*boardOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	return boardResult("get board by slug", func() (*service.Board, error) { return s.boards.GetBySlug(site, in.Slug) })
}

type createBoardInput struct{ Body dto.CreateBoardRequest }

func (s *Server) createBoard(ctx context.Context, in *createBoardInput) (*boardOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	b := in.Body
	var parent *int64
	if b.ParentID > 0 {
		parent = &b.ParentID
	}
	return boardResult("create board", func() (*service.Board, error) {
		return s.boards.Create(ctx, site, b.ActorID, service.BoardFields{
			ParentID: parent, Slug: b.Slug, Name: b.Name,
			Description: b.Description, Icon: b.Icon, Color: b.Color, TopicTemplate: b.TopicTemplate,
			Format: b.Format, ContentRating: b.ContentRating,
			TopicMinTrustLevel: b.TopicMinTrustLevel, ReplyMinTrustLevel: b.ReplyMinTrustLevel,
		})
	})
}

type updateBoardInput struct {
	ID   int64 `path:"id"`
	Body dto.UpdateBoardRequest
}

func (s *Server) updateBoard(ctx context.Context, in *updateBoardInput) (*boardOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	b := in.Body
	return boardResult("update board", func() (*service.Board, error) {
		return s.boards.Update(ctx, site, in.ID, b.ActorID, service.BoardPatch{
			ParentID: b.ParentID, Slug: b.Slug, Name: b.Name,
			Description: b.Description, Icon: b.Icon, Color: b.Color, TopicTemplate: b.TopicTemplate,
			Format: b.Format, Status: b.Status, ContentRating: b.ContentRating,
			TopicMinTrustLevel: b.TopicMinTrustLevel, ReplyMinTrustLevel: b.ReplyMinTrustLevel,
		})
	})
}

type deleteBoardInput struct {
	ID      int64 `path:"id"`
	ActorID int64 `query:"actor_id" doc:"the site admin making the change; recorded in the audit log"`
}

func (s *Server) deleteBoard(ctx context.Context, in *deleteBoardInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	if err := s.boards.Delete(ctx, site, in.ID, in.ActorID); err != nil {
		return nil, mapErr("delete board", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type reorderBoardsInput struct{ Body dto.ReorderBoardsRequest }

func (s *Server) reorderBoards(ctx context.Context, in *reorderBoardsInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	if err := s.boards.Reorder(ctx, site, in.Body.ParentID, in.Body.BoardIDs, in.Body.ActorID); err != nil {
		return nil, mapErr("reorder boards", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

func boardResult(op string, load func() (*service.Board, error)) (*boardOutput, error) {
	b, err := load()
	if err != nil {
		return nil, mapErr(op, err)
	}
	return &boardOutput{Body: okEnvelope(dto.BoardResponse{Board: toBoardView(b)})}, nil
}

func toBoardView(b *service.Board) dto.BoardView {
	return dto.BoardView{
		ID: b.ID, Site: b.Site, ParentID: b.ParentID, Slug: b.Slug, Name: b.Name,
		Description: b.Description, Icon: b.Icon, Color: b.Color, Position: b.Position,
		Format: b.Format, Status: b.Status, ContentRating: b.ContentRating,
		TopicMinTrustLevel: b.TopicMinTrustLevel, ReplyMinTrustLevel: b.ReplyMinTrustLevel,
		TopicTemplate: b.TopicTemplate, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
		Stats: dto.BoardStats{
			TopicsCount: b.Stats.TopicsCount, PostsCount: b.Stats.PostsCount, LastPostedAt: b.Stats.LastPostedAt,
			LastThreadID: b.Stats.LastThreadID, LastThreadTitle: b.Stats.LastThreadTitle,
		},
	}
}
