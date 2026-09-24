package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
	"api/pkg/errors"
)

type commentsPageInput struct {
	AnchorKind int16  `query:"anchor_kind" doc:"1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID   string `query:"anchor_id"`
	After      int32  `query:"after" doc:"post_number to read after (0 = from the top)"`
	Limit      int    `query:"limit" doc:"page size (max 100, default 50)"`
	ViewerID   int64  `query:"viewer_id" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}

type commentsPageOutput struct {
	Body Envelope[dto.CommentsPage]
}

func (s *Server) getComments(ctx context.Context, in *commentsPageInput) (*commentsPageOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	if he := checkEntityAnchor(in.AnchorKind, in.AnchorID); he != nil {
		return nil, he
	}
	thread, err := s.threads.FindCommentsThread(site, in.AnchorKind, in.AnchorID)
	if err != nil {
		return nil, mapErr("find comments thread", err)
	}
	if thread == nil {
		return &commentsPageOutput{Body: okEnvelope(dto.CommentsPage{Posts: []dto.PostView{}})}, nil
	}
	limit := clampLimit(in.Limit)
	posts, err := s.posts.ListPosts(thread.ID, in.After, limit)
	if err != nil {
		return nil, mapErr("list comments", err)
	}
	views := toPostViews(posts)
	if err := s.hydratePostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate comment reactions", err)
	}
	view := toThreadView(thread)
	return &commentsPageOutput{Body: okEnvelope(dto.CommentsPage{
		Thread: &view, Posts: views, NextCursor: postsPageCursor(views, limit),
	})}, nil
}

type commentInput struct {
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"255" doc:"Makes the call safe to retry: a later call in the same site with the same key and the same body answers with the comment the first one wrote (header Idempotency-Replayed: true) instead of writing another; the same key with a different body is 409. Keys are kept for 24 hours."`
	Body           dto.CommentRequest
}
type commentOutput struct {
	Replayed string `header:"Idempotency-Replayed" doc:"true when this answer replays an earlier call with the same Idempotency-Key"`
	Body     Envelope[dto.ThreadResponse]
}

func (s *Server) comment(ctx context.Context, in *commentInput) (*commentOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	if he := checkEntityAnchor(in.Body.AnchorKind, in.Body.AnchorID); he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	res, err := s.posts.WriteComment(ctx, service.CommentParams{
		Site: site, AnchorKind: in.Body.AnchorKind, AnchorID: in.Body.AnchorID,
		ContentRating: in.Body.ContentRating, AuthorID: in.Body.AuthorID, BodyRaw: in.Body.Body,
		RootPostID: in.Body.RootPostID, ReplyToPostID: in.Body.ReplyToPostID, TargetUserID: in.Body.TargetUserID,
		MentionUserIDs: in.Body.MentionUserIDs,
	}, writeKey(in.IdempotencyKey, "comment", in.Body))
	if err != nil {
		return nil, mapErr("comment", err)
	}
	views := []dto.PostView{toPostView(res.Post)}
	if res.Replayed {
		if err := s.hydratePostReactions(in.Body.AuthorID, views); err != nil {
			return nil, mapErr("hydrate replayed comment reactions", err)
		}
	}
	return &commentOutput{
		Replayed: replayedHeader(res.Replayed),
		Body:     okEnvelope(dto.ThreadResponse{Thread: toThreadView(res.Thread), Post: &views[0]}),
	}, nil
}

func writeKey(key string, op string, request ...any) service.WriteKey {
	if key == "" {
		return service.WriteKey{}
	}
	b, _ := json.Marshal(append([]any{op}, request...))
	sum := sha256.Sum256(b)
	return service.WriteKey{Key: key, RequestHash: hex.EncodeToString(sum[:])}
}

func replayedHeader(replayed bool) string {
	if replayed {
		return "true"
	}
	return ""
}

func checkEntityAnchor(anchorKind int16, anchorID string) *houseError {
	if anchorID == "" {
		return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "anchor_id is required")
	}
	switch anchorKind {
	case model.AnchorKindSiteGame, model.AnchorKindSiteResource, model.AnchorKindCatalogWork, model.AnchorKindCatalogPerson:
		return nil
	}
	return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed,
		"anchor_kind must be 1..4 — a board hosts topics only")
}
