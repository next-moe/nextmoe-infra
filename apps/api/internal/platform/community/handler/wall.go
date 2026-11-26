package handler

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerWall(api huma.API) {
	read := []string{"community-read"}
	moderation := []string{"community-moderation"}
	huma.Register(api, huma.Operation{OperationID: "listReplies", Method: http.MethodGet, Path: "/api/v1/community/posts/{id}/replies",
		Summary: "List a top-level post's replies (keyset by post_number, oldest first)", Tags: read}, s.listReplies)
	huma.Register(api, huma.Operation{OperationID: "pinPost", Method: http.MethodPut, Path: "/api/v1/community/posts/{id}/pin",
		Summary: "Pin a top-level comment on its wall for the caller's site (idempotent; at most 3 per wall)", Tags: moderation}, s.pinPost)
	huma.Register(api, huma.Operation{OperationID: "unpinPost", Method: http.MethodDelete, Path: "/api/v1/community/posts/{id}/pin",
		Summary: "Unpin a comment for the caller's site (idempotent)", Tags: moderation}, s.unpinPost)
	huma.Register(api, huma.Operation{OperationID: "restorePost", Method: http.MethodPost, Path: "/api/v1/community/posts/{id}/restore",
		Summary: "Undo a moderator's or the review queue's removal of a post (idempotent)", Tags: moderation}, s.restorePost)
}

func wallQuery(in *commentsPageInput) (repository.WallQuery, *houseError) {
	q := repository.WallQuery{Sort: repository.WallSort(in.Sort), RootsOnly: in.RootsOnly, Limit: in.Limit}
	if q.Sort == "" {
		q.Sort = repository.WallSortOldest
	}
	if in.ReplyPreview > 0 && !in.RootsOnly {
		return q, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "reply_preview needs roots_only")
	}
	switch {
	case in.Cursor != "" && in.After != 0:
		return q, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "send cursor or after, not both")
	case in.After != 0 && q.Sort != repository.WallSortOldest:
		return q, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "after pages sort=oldest only; send cursor")
	case in.After != 0:
		q.Cursor.PostNumber = in.After
		return q, nil
	}
	c, err := decodeWallCursor(in.Cursor, q.Sort)
	if err != nil {
		return q, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	q.Cursor = c
	return q, nil
}

// Under sort=oldest the cursor stays a bare post_number: callers that predate
// sort pass next_cursor back as after.
func encodeWallCursor(sort repository.WallSort, last repository.WallRow) string {
	switch sort {
	case repository.WallSortNewest:
		return base64.RawURLEncoding.EncodeToString([]byte("n:" + strconv.Itoa(int(last.PostNumber))))
	case repository.WallSortTop:
		return base64.RawURLEncoding.EncodeToString([]byte("t:" + strconv.Itoa(int(last.Likes)) + ":" + strconv.Itoa(int(last.PostNumber))))
	}
	return strconv.Itoa(int(last.PostNumber))
}

func decodeWallCursor(raw string, sort repository.WallSort) (repository.WallCursor, error) {
	var c repository.WallCursor
	if raw == "" {
		return c, nil
	}
	if sort == repository.WallSortOldest {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			return c, stderrors.New("bad oldest cursor")
		}
		c.PostNumber = int32(n)
		return c, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, err
	}
	parts := strings.Split(string(b), ":")
	nums := make([]int32, 0, 2)
	for _, p := range parts[1:] {
		n, err := strconv.ParseInt(p, 10, 32)
		if err != nil || n < 0 {
			return c, stderrors.New("bad cursor number")
		}
		nums = append(nums, int32(n))
	}
	switch {
	case sort == repository.WallSortNewest && parts[0] == "n" && len(nums) == 1 && nums[0] > 0:
		c.PostNumber = nums[0]
	case sort == repository.WallSortTop && parts[0] == "t" && len(nums) == 2 && nums[1] > 0:
		c.Likes, c.PostNumber = nums[0], nums[1]
	default:
		return c, stderrors.New("cursor of another sort")
	}
	return c, nil
}

func (s *Server) wallPage(site string, q repository.WallQuery, preview int, viewerID int64) (*dto.CommentsPage, error) {
	rows, err := s.posts.ListWall(q)
	if err != nil {
		return nil, err
	}
	page := &dto.CommentsPage{PinnedPosts: []dto.PostView{}, Posts: make([]dto.PostView, len(rows))}
	for i := range rows {
		page.Posts[i] = toPostView(&rows[i].CommunityPost)
	}
	if len(rows) == clampLimit(q.Limit) {
		page.NextCursor = encodeWallCursor(q.Sort, rows[len(rows)-1])
	}

	pinned, err := s.posts.PinnedPosts(site, q.ThreadID)
	if err != nil {
		return nil, err
	}
	pinnedAt := make(map[int64]time.Time, len(pinned))
	for i := range pinned {
		pinnedAt[pinned[i].ID] = pinned[i].PinnedAt
		if q.Cursor.PostNumber == 0 {
			v := toPostView(&pinned[i].CommunityPost)
			v.PinnedAt = &pinned[i].PinnedAt
			page.PinnedPosts = append(page.PinnedPosts, v)
		}
	}
	for i := range page.Posts {
		if t, ok := pinnedAt[page.Posts[i].ID]; ok {
			page.Posts[i].PinnedAt = &t
		}
	}

	if q.RootsOnly {
		roots := make([]int64, 0, len(page.PinnedPosts)+len(page.Posts))
		for _, list := range [][]dto.PostView{page.PinnedPosts, page.Posts} {
			for _, v := range list {
				roots = append(roots, v.ID)
			}
		}
		counts, err := s.posts.VisibleReplyCounts(q.ThreadID, roots)
		if err != nil {
			return nil, err
		}
		for _, list := range [][]dto.PostView{page.PinnedPosts, page.Posts} {
			for i := range list {
				n := counts[list[i].ID]
				list[i].ReplyCount = &n
			}
		}
		if preview > 0 {
			replies, err := s.posts.ReplyPreviews(q.ThreadID, dedupeOrdered(roots), preview)
			if err != nil {
				return nil, err
			}
			page.ReplyPreviews = toPostViews(replies)
		}
	}

	for _, list := range [][]dto.PostView{page.PinnedPosts, page.Posts, page.ReplyPreviews} {
		if err := s.hydratePostReactions(viewerID, list); err != nil {
			return nil, err
		}
	}
	return page, nil
}

func dedupeOrdered(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

type listRepliesInput struct {
	ID       int64 `path:"id"`
	After    int32 `query:"after" doc:"post_number to read after (0 = from the first reply)"`
	Limit    int   `query:"limit" doc:"page size (max 100, default 50)"`
	ViewerID int64 `query:"viewer_id" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}

func (s *Server) listReplies(ctx context.Context, in *listRepliesInput) (*postListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	rows, err := s.posts.Replies(ctx, in.ID, in.After, in.Limit)
	if err != nil {
		return nil, mapErr("list replies", err)
	}
	views := toPostViews(rows)
	if err := s.hydratePostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate reply reactions", err)
	}
	return &postListOutput{Body: okEnvelope(dto.PostListResponse{
		Posts: views, NextCursor: postsPageCursor(views, clampLimit(in.Limit)),
	})}, nil
}

type pinPostInput struct {
	ID   int64 `path:"id"`
	Body dto.PostActorRequest
}

type unpinPostInput struct {
	ID      int64 `path:"id"`
	ActorID int64 `query:"actor_id" doc:"the moderator; recorded in the audit log"`
}

type postChangeOutput struct {
	Body Envelope[dto.PostChangeResponse]
}

func (s *Server) pinPost(ctx context.Context, in *pinPostInput) (*postChangeOutput, error) {
	return s.pinChange(ctx, "pin post", in.ID, in.Body.ActorID, s.posts.Pin)
}

func (s *Server) unpinPost(ctx context.Context, in *unpinPostInput) (*postChangeOutput, error) {
	return s.pinChange(ctx, "unpin post", in.ID, in.ActorID, s.posts.Unpin)
}

func (s *Server) pinChange(ctx context.Context, op string, postID, actorID int64,
	do func(context.Context, int64, int64) (service.PinResult, error)) (*postChangeOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	res, err := do(service.WithCallerSite(ctx, site), postID, actorID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	views := []dto.PostView{toPostView(res.Post)}
	views[0].PinnedAt = res.PinnedAt
	if err := s.hydratePostReactions(actorID, views); err != nil {
		return nil, mapErr(op+" reactions", err)
	}
	return &postChangeOutput{Body: okEnvelope(dto.PostChangeResponse{Post: views[0], Changed: res.Changed})}, nil
}

type restorePostInput struct {
	ID   int64 `path:"id"`
	Body dto.PostActorRequest
}

func (s *Server) restorePost(ctx context.Context, in *restorePostInput) (*postChangeOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	post, changed, err := s.posts.Restore(service.WithCallerSite(ctx, site), in.ID, in.Body.ActorID)
	if err != nil {
		return nil, mapErr("restore post", err)
	}
	views := []dto.PostView{toPostView(post)}
	if err := s.hydratePostReactions(in.Body.ActorID, views); err != nil {
		return nil, mapErr("restore post reactions", err)
	}
	return &postChangeOutput{Body: okEnvelope(dto.PostChangeResponse{Post: views[0], Changed: changed})}, nil
}
