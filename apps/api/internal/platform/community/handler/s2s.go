package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

const defaultPageLimit = 50

type Server struct {
	threads    *service.ThreadService
	posts      *service.PostService
	reactions  *service.ReactionService
	feedback   *service.FeedbackService
	flags      *service.FlagService
	trust      *service.TrustService
	review     *service.ReviewService
	engagement *service.EngagementService
	search     *service.SearchService
}

// Services is what the S2S face is wired from; the spec generator passes an
// empty one, since registering the routes never touches a service.
type Services struct {
	Threads    *service.ThreadService
	Posts      *service.PostService
	Reactions  *service.ReactionService
	Feedback   *service.FeedbackService
	Flags      *service.FlagService
	Trust      *service.TrustService
	Review     *service.ReviewService
	Engagement *service.EngagementService
	Search     *service.SearchService
}

func Setup(app *fiber.App, svc Services) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Community Service", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(S2SBridge)

	s := &Server{
		threads: svc.Threads, posts: svc.Posts, reactions: svc.Reactions, feedback: svc.Feedback,
		flags: svc.Flags, trust: svc.Trust, review: svc.Review, engagement: svc.Engagement, search: svc.Search,
	}
	s.register(api)
	return api
}

func (s *Server) register(api huma.API) {
	read := []string{"community-read"}
	write := []string{"community-write"}

	huma.Register(api, huma.Operation{OperationID: "getComments", Method: http.MethodGet, Path: "/api/v1/community/comments",
		Summary: "Read an anchor's comments and its thread (both absent until the first comment)", Tags: read}, s.getComments)
	huma.Register(api, huma.Operation{OperationID: "resolveComments", Method: http.MethodPost, Path: "/api/v1/community/comments/resolve",
		Summary: "Deprecated: get-or-create the comments thread for an anchor. Read with GET /comments and write with POST /comments — this face mints a thread on a read",
		Tags:    read, Deprecated: true}, s.resolveComments)
	huma.Register(api, huma.Operation{OperationID: "listThreads", Method: http.MethodGet, Path: "/api/v1/community/threads",
		Summary: "List a site's threads of a kind (keyset, newest activity first)", Tags: read}, s.listThreads)
	huma.Register(api, huma.Operation{OperationID: "getThread", Method: http.MethodGet, Path: "/api/v1/community/threads/{id}",
		Summary: "Get a thread with a page of posts", Tags: read}, s.getThread)
	huma.Register(api, huma.Operation{OperationID: "listPosts", Method: http.MethodGet, Path: "/api/v1/community/threads/{id}/posts",
		Summary: "List a thread's posts (keyset by post_number)", Tags: read}, s.listPosts)
	huma.Register(api, huma.Operation{OperationID: "listSitePosts", Method: http.MethodGet, Path: "/api/v1/community/posts",
		Summary: "List the site's newest posts across every thread (keyset by post creation time)", Tags: read}, s.listSitePosts)
	huma.Register(api, huma.Operation{OperationID: "listAuthorPosts", Method: http.MethodGet, Path: "/api/v1/community/authors/{id}/posts",
		Summary: "List a site author's visible posts across threads (keyset by post id, newest first) with thread context", Tags: read}, s.listAuthorPosts)
	huma.Register(api, huma.Operation{OperationID: "authorStats", Method: http.MethodGet, Path: "/api/v1/community/authors/stats",
		Summary: "Batch visible-post counts for a site's authors", Tags: read}, s.authorStats)
	huma.Register(api, huma.Operation{OperationID: "topAuthors", Method: http.MethodGet, Path: "/api/v1/community/authors/top",
		Summary: "The site's most-posted-in authors, most first", Tags: read}, s.topAuthors)
	huma.Register(api, huma.Operation{OperationID: "searchPosts", Method: http.MethodGet, Path: "/api/v1/community/search/posts",
		Summary: "Search the site's visible posts by substring of their markdown source", Tags: read}, s.searchPosts)
	huma.Register(api, huma.Operation{OperationID: "searchThreads", Method: http.MethodGet, Path: "/api/v1/community/search/threads",
		Summary: "Search the site's threads by substring of their title", Tags: read}, s.searchThreads)
	huma.Register(api, huma.Operation{OperationID: "resolvePosts", Method: http.MethodPost, Path: "/api/v1/community/posts/resolve",
		Summary: "Resolve a batch of posts by id (visible only, request order, deduped) with thread context", Tags: read}, s.resolvePosts)

	huma.Register(api, huma.Operation{OperationID: "openTopic", Method: http.MethodPost, Path: "/api/v1/community/topics",
		Summary: "Open a board topic with its opening post", Tags: write}, s.openTopic)
	huma.Register(api, huma.Operation{OperationID: "openFeedback", Method: http.MethodPost, Path: "/api/v1/community/feedback",
		Summary: "Open a feedback thread with its opening post", Tags: write}, s.openFeedback)
	huma.Register(api, huma.Operation{OperationID: "comment", Method: http.MethodPost, Path: "/api/v1/community/comments",
		Summary: "Comment on an anchor; the first comment is what creates the comments thread", Tags: write}, s.comment)
	huma.Register(api, huma.Operation{OperationID: "reply", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/posts",
		Summary: "Reply to a thread", Tags: write}, s.reply)
	huma.Register(api, huma.Operation{OperationID: "editPost", Method: http.MethodPatch, Path: "/api/v1/community/posts/{id}",
		Summary: "Edit a post (author, or a site moderator via as_moderator; re-sanitized, stamps edited_at)", Tags: write}, s.editPost)
	huma.Register(api, huma.Operation{OperationID: "deletePost", Method: http.MethodDelete, Path: "/api/v1/community/posts/{id}",
		Summary: "Delete a post (author self-delete, or a site moderator via as_moderator; tombstone, post_number preserved)", Tags: write}, s.deletePost)
	huma.Register(api, huma.Operation{OperationID: "toggleReaction", Method: http.MethodPost, Path: "/api/v1/community/posts/{id}/reaction",
		Summary: "Toggle a reaction on a post", Tags: write}, s.toggleReaction)
	huma.Register(api, huma.Operation{OperationID: "submitFlag", Method: http.MethodPost, Path: "/api/v1/community/posts/{id}/flag",
		Summary: "Report a post", Tags: write}, s.submitFlag)
	huma.Register(api, huma.Operation{OperationID: "setFeedbackStatus", Method: http.MethodPost, Path: "/api/v1/community/feedback/{id}/status",
		Summary: "Set a feedback thread's status and official response", Tags: write}, s.setFeedbackStatus)
	huma.Register(api, huma.Operation{OperationID: "mergeFeedback", Method: http.MethodPost, Path: "/api/v1/community/feedback/{id}/merge",
		Summary: "Merge a duplicate feedback thread into another (reversible)", Tags: write}, s.mergeFeedback)
	huma.Register(api, huma.Operation{OperationID: "purgeAuthor", Method: http.MethodPost, Path: "/api/v1/community/authors/{id}/purge",
		Summary: "Compliance purge: tombstone + scrub all of a site author's posts and delete their reactions (idempotent)", Tags: write}, s.purgeAuthor)

	engagement := []string{"community-engagement"}
	huma.Register(api, huma.Operation{OperationID: "markThreadRead", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/read",
		Summary: "Report how far a user has read a thread (monotonic; creates the sparse thread_user row)", Tags: engagement}, s.markThreadRead)
	huma.Register(api, huma.Operation{OperationID: "setThreadNotification", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/notification",
		Summary: "Set a user's notification level for a thread (0=muted 1=normal 2=tracking 3=watching)", Tags: engagement}, s.setThreadNotification)
	huma.Register(api, huma.Operation{OperationID: "threadStates", Method: http.MethodPost, Path: "/api/v1/community/threads/states",
		Summary: "Batch read/subscription state for a user over a set of threads", Tags: engagement}, s.threadStates)
	huma.Register(api, huma.Operation{OperationID: "listUnread", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/unread",
		Summary: "List a user's threads carrying unread posts (muted excluded), newest activity first", Tags: engagement}, s.listUnread)

	trust := []string{"community-trust"}
	review := []string{"community-review"}
	huma.Register(api, huma.Operation{OperationID: "recordActivity", Method: http.MethodPost, Path: "/api/v1/community/trust/activity",
		Summary: "Report a user's reading-behavior activity (metering + promotion)", Tags: trust}, s.recordActivity)
	huma.Register(api, huma.Operation{OperationID: "setBoost", Method: http.MethodPost, Path: "/api/v1/community/trust/boost",
		Summary: "Declare a starter boost (veteran/creator/staff) for a user", Tags: trust}, s.setBoost)
	huma.Register(api, huma.Operation{OperationID: "listReview", Method: http.MethodGet, Path: "/api/v1/community/review",
		Summary: "List the site's pending moderation-queue items", Tags: review}, s.listReview)
	huma.Register(api, huma.Operation{OperationID: "approveReview", Method: http.MethodPost, Path: "/api/v1/community/review/{id}/approve",
		Summary: "Approve a queue item (keep the content; restore the post)", Tags: review}, s.approveReview)
	huma.Register(api, huma.Operation{OperationID: "rejectReview", Method: http.MethodPost, Path: "/api/v1/community/review/{id}/reject",
		Summary: "Reject a queue item (remove the content; tombstone the post)", Tags: review}, s.rejectReview)
}

type resolveCommentsInput struct{ Body dto.CommentsResolveRequest }
type threadWithPostsOutput struct {
	Body Envelope[dto.ThreadWithPosts]
}

func (s *Server) resolveComments(ctx context.Context, in *resolveCommentsInput) (*threadWithPostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, err := s.threads.GetOrCreateCommentsThread(ctx, service.CommentsThreadParams{
		Site: site, AnchorKind: in.Body.AnchorKind, AnchorID: in.Body.AnchorID,
		ContentRating: in.Body.ContentRating, ActorID: 0,
	})
	if err != nil {
		return nil, mapErr("resolve comments", err)
	}
	posts, err := s.posts.ListPosts(thread.ID, 0, defaultPageLimit)
	if err != nil {
		return nil, mapErr("list posts", err)
	}
	views := toPostViews(posts)
	// No viewer_reacted here: this face is the retiring get-or-create shape and
	// takes no viewer. GET /comments is the one that renders a wall.
	if err := s.hydratePostReactions(0, views); err != nil {
		return nil, mapErr("hydrate resolved comment reactions", err)
	}
	return &threadWithPostsOutput{Body: okEnvelope(dto.ThreadWithPosts{
		Thread: toThreadView(thread), Posts: views, NextCursor: postsPageCursor(views, defaultPageLimit),
	})}, nil
}

type listThreadsInput struct {
	Kind       int16  `query:"kind" doc:"0=topic 1=comments 2=feedback"`
	AnchorKind int16  `query:"anchor_kind" doc:"anchor kind for the optional anchor filter (only used when anchor_id is set)"`
	AnchorID   string `query:"anchor_id" doc:"optional: narrow to a single anchor (e.g. a resource's feedback wall); empty = the whole site"`
	Sort       string `query:"sort" default:"activity" enum:"activity,created,posts" doc:"activity (newest activity) | created (newest thread) | posts (most replies; a mutable key, so a row can move between pages)"`
	HasPosts   bool   `query:"has_posts" doc:"only threads that hold at least one post; a comments thread is created on first view, so most carry none"`
	Cursor     string `query:"cursor" doc:"opaque cursor from the previous page; it is bound to the sort that minted it"`
	Limit      int    `query:"limit" doc:"page size (max 100, default 50)"`
}
type threadListOutput struct {
	Body Envelope[dto.ThreadListResponse]
}

func (s *Server) listThreads(ctx context.Context, in *listThreadsInput) (*threadListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	sort, ok := parseThreadSort(in.Sort)
	if !ok {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "unknown sort")
	}
	cursor, err := decodeThreadCursor(in.Cursor)
	if err != nil || (cursor.ID != 0 && cursor.Sort != sort) {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	limit := clampLimit(in.Limit)
	threads, err := s.threads.List(repository.ThreadListQuery{
		Site: site, Kind: in.Kind, AnchorKind: in.AnchorKind, AnchorID: in.AnchorID,
		Sort: sort, HasPosts: in.HasPosts, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return nil, mapErr("list threads", err)
	}
	ids := make([]int64, len(threads))
	for i := range threads {
		ids[i] = threads[i].ID
	}
	metas, err := s.threads.OpeningPostMeta(ids)
	if err != nil {
		return nil, mapErr("list threads openings", err)
	}
	return &threadListOutput{Body: okEnvelope(dto.ThreadListResponse{
		Threads: toThreadViewsWithOpening(threads, metas), NextCursor: threadsPageCursor(threads, sort, limit),
	})}, nil
}

type threadPostsInput struct {
	ID       int64 `path:"id"`
	After    int32 `query:"after" doc:"post_number to read after (0 = from the top)"`
	Limit    int   `query:"limit"`
	ViewerID int64 `query:"viewer_id" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}

func (s *Server) getThread(ctx context.Context, in *threadPostsInput) (*threadWithPostsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, err := s.threads.Get(in.ID)
	if err != nil {
		return nil, mapErr("get thread", err)
	}
	if thread == nil || service.CrossTenant(site, thread.Site, thread.AnchorKind) {
		return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
	}
	limit := clampLimit(in.Limit)
	posts, err := s.posts.ListPosts(in.ID, in.After, limit)
	if err != nil {
		return nil, mapErr("list posts", err)
	}
	views := toPostViews(posts)
	if err := s.hydratePostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate thread reactions", err)
	}
	return &threadWithPostsOutput{Body: okEnvelope(dto.ThreadWithPosts{
		Thread: toThreadView(thread), Posts: views, NextCursor: postsPageCursor(views, limit),
	})}, nil
}

type postListOutput struct {
	Body Envelope[dto.PostListResponse]
}

func (s *Server) listPosts(ctx context.Context, in *threadPostsInput) (*postListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, err := s.threads.Get(in.ID)
	if err != nil {
		return nil, mapErr("get thread", err)
	}
	if thread == nil || service.CrossTenant(site, thread.Site, thread.AnchorKind) {
		return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
	}
	limit := clampLimit(in.Limit)
	posts, err := s.posts.ListPosts(in.ID, in.After, limit)
	if err != nil {
		return nil, mapErr("list posts", err)
	}
	views := toPostViews(posts)
	if err := s.hydratePostReactions(in.ViewerID, views); err != nil {
		return nil, mapErr("hydrate post reactions", err)
	}
	return &postListOutput{Body: okEnvelope(dto.PostListResponse{
		Posts: views, NextCursor: postsPageCursor(views, limit),
	})}, nil
}

type openTopicInput struct{ Body dto.OpenTopicRequest }
type threadOutput struct {
	Body Envelope[dto.ThreadResponse]
}

func (s *Server) openTopic(ctx context.Context, in *openTopicInput) (*threadOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, post, err := s.threads.OpenTopic(ctx, service.OpenThreadParams{
		Site: site, AuthorID: in.Body.AuthorID, AnchorKind: model.AnchorKindBoard, AnchorID: in.Body.AnchorID,
		Title: in.Body.Title, ContentRating: in.Body.ContentRating, BodyRaw: in.Body.Body,
		HeaderImageHashes: hashesJSON(in.Body.HeaderImageHashes),
	})
	if err != nil {
		return nil, mapErr("open topic", err)
	}
	pv := toPostView(post)
	return &threadOutput{Body: okEnvelope(dto.ThreadResponse{Thread: toThreadView(thread), Post: &pv})}, nil
}

type openFeedbackInput struct{ Body dto.OpenFeedbackRequest }

func (s *Server) openFeedback(ctx context.Context, in *openFeedbackInput) (*threadOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, post, err := s.threads.OpenFeedback(ctx, service.OpenThreadParams{
		Site: site, AuthorID: in.Body.AuthorID, AnchorKind: in.Body.AnchorKind, AnchorID: in.Body.AnchorID,
		Title: in.Body.Title, ContentRating: in.Body.ContentRating, BodyRaw: in.Body.Body,
	})
	if err != nil {
		return nil, mapErr("open feedback", err)
	}
	pv := toPostView(post)
	return &threadOutput{Body: okEnvelope(dto.ThreadResponse{Thread: toThreadView(thread), Post: &pv})}, nil
}

type replyInput struct {
	ID   int64 `path:"id"`
	Body dto.ReplyRequest
}
type postOutput struct {
	Body Envelope[dto.PostResponse]
}

func (s *Server) reply(ctx context.Context, in *replyInput) (*postOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	post, err := s.posts.Reply(ctx, service.ReplyParams{
		ThreadID: in.ID, AuthorID: in.Body.AuthorID, BodyRaw: in.Body.Body,
		RootPostID: in.Body.RootPostID, ReplyToPostID: in.Body.ReplyToPostID, TargetUserID: in.Body.TargetUserID,
	})
	if err != nil {
		return nil, mapErr("reply", err)
	}
	// No hydration: the post was inserted by this call, so nobody can have
	// reacted to it and 0 is the true count. Every OTHER face returning a
	// PostView has to fill it -- see editPost.
	return &postOutput{Body: okEnvelope(dto.PostResponse{Post: toPostView(post)})}, nil
}

type editPostInput struct {
	ID   int64 `path:"id"`
	Body dto.EditPostRequest
}

func (s *Server) editPost(ctx context.Context, in *editPostInput) (*postOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	post, err := s.posts.Edit(ctx, service.EditParams{
		PostID: in.ID, AuthorID: in.Body.AuthorID, BodyRaw: in.Body.Body, AsModerator: in.Body.AsModerator,
	})
	if err != nil {
		return nil, mapErr("edit post", err)
	}
	// An edit does not change the likes, but it returns the post, and a consumer
	// that renders the response in place showed the count as 0 until moyu
	// reported it. The acting user is the viewer: this response is for them.
	views := []dto.PostView{toPostView(post)}
	if err := s.hydratePostReactions(in.Body.AuthorID, views); err != nil {
		return nil, mapErr("hydrate edited post reactions", err)
	}
	return &postOutput{Body: okEnvelope(dto.PostResponse{Post: views[0]})}, nil
}

type deletePostInput struct {
	ID          int64 `path:"id"`
	AuthorID    int64 `query:"author_id" doc:"the acting user (the post author, or the moderator when as_moderator)"`
	AsModerator bool  `query:"as_moderator" doc:"mod-actor variant: skip the author match; the site vouches author_id is its moderator"`
}

func (s *Server) deletePost(ctx context.Context, in *deletePostInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.posts.Delete(ctx, in.ID, in.AuthorID, in.AsModerator); err != nil {
		return nil, mapErr("delete post", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type toggleReactionInput struct {
	ID   int64 `path:"id"`
	Body dto.ReactionToggleRequest
}
type reactionOutput struct {
	Body Envelope[dto.ReactionToggleResponse]
}

func (s *Server) toggleReaction(ctx context.Context, in *toggleReactionInput) (*reactionOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	added, pc, err := s.reactions.Toggle(ctx, in.ID, in.Body.UserID, in.Body.Kind)
	if err != nil {
		return nil, mapErr("toggle reaction", err)
	}
	return &reactionOutput{Body: okEnvelope(dto.ReactionToggleResponse{
		Added: added, AuthorID: pc.AuthorID, ThreadID: pc.ThreadID,
		AnchorKind: pc.AnchorKind, AnchorID: pc.AnchorID,
	})}, nil
}

type flagInput struct {
	ID   int64 `path:"id"`
	Body dto.FlagRequest
}
type okOutput struct {
	Body Envelope[dto.OKResponse]
}

func (s *Server) submitFlag(ctx context.Context, in *flagInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.flags.Submit(ctx, in.ID, in.Body.FlaggerID, in.Body.Reason, in.Body.Note); err != nil {
		return nil, mapErr("submit flag", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type feedbackStatusInput struct {
	ID   int64 `path:"id"`
	Body dto.FeedbackStatusRequest
}

func (s *Server) setFeedbackStatus(ctx context.Context, in *feedbackStatusInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.feedback.SetStatus(ctx, in.ID, in.Body.FbStatus, in.Body.ResponderID, in.Body.Response); err != nil {
		return nil, mapErr("set feedback status", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type feedbackMergeInput struct {
	ID   int64 `path:"id"`
	Body dto.FeedbackMergeRequest
}

func (s *Server) mergeFeedback(ctx context.Context, in *feedbackMergeInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.feedback.Merge(ctx, in.ID, in.Body.IntoID); err != nil {
		return nil, mapErr("merge feedback", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type recordActivityInput struct{ Body dto.ActivityReceiptRequest }
type trustOutput struct {
	Body Envelope[dto.TrustView]
}

func (s *Server) recordActivity(ctx context.Context, in *recordActivityInput) (*trustOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	trust, err := s.trust.RecordActivity(ctx, service.ActivityReceipt{
		UserID: in.Body.UserID, TopicsEntered: in.Body.TopicsEntered, PostsRead: in.Body.PostsRead,
		ReadTimeS: in.Body.ReadTimeS, DaysVisited: in.Body.DaysVisited, WindowActiveDays: in.Body.WindowActiveDays,
	})
	if err != nil {
		return nil, mapErr("record activity", err)
	}
	return &trustOutput{Body: okEnvelope(toTrustView(trust))}, nil
}

type setBoostInput struct{ Body dto.SetBoostRequest }

func (s *Server) setBoost(ctx context.Context, in *setBoostInput) (*trustOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	trust, err := s.trust.SetBoost(ctx, in.Body.UserID, in.Body.Boost)
	if err != nil {
		return nil, mapErr("set boost", err)
	}
	return &trustOutput{Body: okEnvelope(toTrustView(trust))}, nil
}

type listReviewInput struct {
	Source int16 `query:"source" default:"-1" doc:"filter to one source; -1 = all"`
	Limit  int   `query:"limit"`
}
type reviewListOutput struct {
	Body Envelope[dto.ReviewListResponse]
}

func (s *Server) listReview(ctx context.Context, in *listReviewInput) (*reviewListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	items, err := s.review.List(site, in.Source, clampLimit(in.Limit))
	if err != nil {
		return nil, mapErr("list review", err)
	}
	return &reviewListOutput{Body: okEnvelope(dto.ReviewListResponse{Items: toReviewItemViews(items)})}, nil
}

type reviewDecisionInput struct {
	ID   int64 `path:"id"`
	Body dto.ReviewDecisionRequest
}

func (s *Server) approveReview(ctx context.Context, in *reviewDecisionInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.review.Approve(ctx, in.ID, in.Body.DecidedBy); err != nil {
		return nil, mapErr("approve review", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

func (s *Server) rejectReview(ctx context.Context, in *reviewDecisionInput) (*okOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	if err := s.review.Reject(ctx, in.ID, in.Body.DecidedBy); err != nil {
		return nil, mapErr("reject review", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return defaultPageLimit
	}
	return limit
}

func hashesJSON(hashes []string) datatypes.JSON {
	if len(hashes) == 0 {
		return nil
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

func mapErr(op string, err error) *houseError {
	var sandbox *service.SandboxError
	switch {
	case stderrors.As(err, &sandbox):
		return apiErrMsg(http.StatusTooManyRequests, errors.ErrOperationFailed, "sandbox limit: "+sandbox.Reason)
	case stderrors.Is(err, service.ErrThreadNotFound),
		stderrors.Is(err, service.ErrPostNotFound),
		stderrors.Is(err, service.ErrReviewNotFound):
		return apiErr(http.StatusNotFound, errors.ErrNotFound)
	case stderrors.Is(err, service.ErrThreadNotOpen):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, "thread is not open")
	case stderrors.Is(err, service.ErrInvalidSearchQuery):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "search query must be 2-100 characters")
	case stderrors.Is(err, service.ErrInvalidNotificationLevel):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "notification level out of range")
	case stderrors.Is(err, service.ErrNotFeedback):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "thread is not a feedback thread")
	case stderrors.Is(err, service.ErrNotAuthor):
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden, "not the post author")
	case stderrors.Is(err, service.ErrPostNotEditable):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, "post is not editable")
	case stderrors.Is(err, service.ErrContentBlocked):
		return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "content blocked by word list")
	default:
		slog.Error("community "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}
