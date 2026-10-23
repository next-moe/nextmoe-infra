package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/discussion"
	catalogPerm "api/internal/platform/catalog/perm"
	"api/internal/platform/settings/keys"
	"api/pkg/trustclient"

	"github.com/danielgtaylor/huma/v2"
)

const DiscussionTrustSubjectKind = "edit_discussion"

const (
	discussionPageDefault = 50
	discussionNoticeLimit = 50
	discussionNoticeSince = 14 * 24 * time.Hour
)

type discussionAnchorInput struct {
	Object string `path:"object" pattern:"^[a-z_]{1,32}$" maxLength:"32" doc:"Object discriminant of the anchor: proposal, merge_request, or one a later request type registers."`
	ID     string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Id of the proposal or request."`
	Cursor string `query:"cursor" pattern:"^cur_[0-9]+$" maxLength:"32" doc:"Opaque cursor from next_cursor."`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" doc:"Page size, 1-100. Default 50."`
}

type discussionPageOutput struct {
	Body repr.DiscussionPage
}

type createDiscussionInput struct {
	Body struct {
		AnchorKind  string   `json:"anchor_kind" pattern:"^[a-z_]{1,32}$" maxLength:"32" doc:"proposal, merge_request, or a kind a later request type registers."`
		AnchorID    string   `json:"anchor_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Id of the proposal or request."`
		Body        string   `json:"body" minLength:"1" maxLength:"8000" doc:"Plain text, at most 2000 characters after trimming. Must not be used as a discriminant."`
		ReplyToID   string   `json:"reply_to_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"A post in the same thread this one quotes."`
		MentionUIDs []string `json:"mention_uids,omitempty" maxItems:"100" doc:"Central account ids to notify; the author is dropped. More than 10 people is 422."`
	}
}

type discussionPostIDInput struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Post id."`
}

type editDiscussionInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Post id."`
	Body struct {
		Body string `json:"body" minLength:"1" maxLength:"8000" doc:"The new text. Must not be used as a discriminant."`
	}
}

type moderateDiscussionInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Post id."`
	Body struct {
		Hidden bool   `json:"hidden" doc:"true hides the post from everyone but discussion moderators; false restores it."`
		Note   string `json:"note,omitempty" maxLength:"500" doc:"Why. Shown to discussion moderators only. Must not be used as a discriminant."`
	}
}

type discussionNoticesInput struct {
	Since string `query:"since" format:"date-time" maxLength:"40" doc:"RFC 3339. Only posts newer than this. Default: 14 days ago."`
	Limit int    `query:"limit" minimum:"1" maximum:"100" doc:"At most this many, newest first. Default 50."`
}

type watchDiscussionInput struct {
	Body struct {
		AnchorKind string `json:"anchor_kind" pattern:"^[a-z_]{1,32}$" maxLength:"32" doc:"Anchor kind."`
		AnchorID   string `json:"anchor_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Anchor id."`
		Muted      bool   `json:"muted" doc:"true mutes the thread (mentions still arrive); false watches it."`
	}
}

type discussionPostOutput struct {
	Body repr.DiscussionPost
}

type discussionNoticesOutput struct {
	Body repr.List[repr.DiscussionNotice]
}

type discussionWatchOutput struct {
	Body repr.DiscussionWatch
}

func registerDiscussions(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusUnprocessableEntity, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogDiscussion", Method: http.MethodGet, Path: "/v2/catalog/discussions/{object}/{id}",
		Summary: "Discussion under a proposal or request",
		Description: "Posts oldest first; follow next_cursor for newer ones. A proposal's thread is public like the proposal; a merge request's is readable by its requester and by catalog.merge.review holders. " +
			"With a user token the page carries viewer: whether the caller may post or moderate here, and whether the thread reaches their notices. Anonymous calls are allowed and limited per IP address; they read public threads only.",
		Tags: []string{"catalog"}, Errors: errs, SkipValidateParams: true,
	}, listCatalogDiscussion(cat))
	me, mod := []string{"me"}, []string{"moderation"}
	writeErrs := append(errs, http.StatusConflict, http.StatusTooManyRequests)
	huma.Register(api, huma.Operation{
		OperationID: "createMyDiscussionPost", Method: http.MethodPost, Path: "/v2/me/discussions",
		Summary: "Post in a discussion",
		Description: "Adds a post under a proposal or request the caller can read. The author, everyone mentioned, and the anchor's participants (proposer or requester, decider, amenders) start watching the thread; their notices pick up later posts. " +
			"Limited per account by catalog.discussion_posts_per_hour; past it 429 QUOTA_EXCEEDED. Text refused by the content check is 422. Requires a user access token from a first-party site with the catalog:edit scope.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyDiscussionPost(cat))
	huma.Register(api, huma.Operation{
		OperationID: "editMyDiscussionPost", Method: http.MethodPatch, Path: "/v2/me/discussions/{id}",
		Summary:     "Edit my post",
		Description: "Replaces the text of a visible post the caller wrote and stamps edited_at. Requires a user access token from a first-party site with the catalog:edit scope.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, editMyDiscussionPost(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyDiscussionPost", Method: http.MethodDelete, Path: "/v2/me/discussions/{id}",
		Summary:     "Delete my post",
		Description: "The post stays in the thread as deleted, with its text removed. Requires a user access token from a first-party site with the catalog:edit scope.",
		Tags:        me, Errors: writeErrs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyDiscussionPost(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyDiscussionNotices", Method: http.MethodGet, Path: "/v2/me/discussion-notices",
		Summary:     "New posts for me",
		Description: "Posts by others, newer than since, in threads the caller watches or that mention them; a muted thread reaches the caller only through mentions. Requires a user access token from a first-party site with the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyDiscussionNotices(cat))
	huma.Register(api, huma.Operation{
		OperationID: "watchMyDiscussion", Method: http.MethodPut, Path: "/v2/me/discussion-watches",
		Summary:     "Watch or mute a discussion",
		Description: "Requires a user access token from a first-party site with the catalog:edit scope.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, watchMyDiscussion(cat))
	huma.Register(api, huma.Operation{
		OperationID: "moderateDiscussionPost", Method: http.MethodPost, Path: "/v2/moderation/discussions/{id}/visibility",
		Summary:     "Hide or restore a post",
		Description: "Requires the catalog.discussion.moderate permission.",
		Tags:        mod, Errors: writeErrs, SkipValidateParams: true,
	}, moderateDiscussionPost(cat))
}

func (c *Catalog) discussionsBound() error {
	if c == nil || c.Discussions == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "discussions are not bound.")
	}
	return nil
}

func discussionErr(err error) error {
	switch {
	case errors.Is(err, discussion.ErrUnknownKind), errors.Is(err, discussion.ErrAnchorNotFound),
		errors.Is(err, discussion.ErrForbidden):
		return problem.New(problem.CodeNotFound, "", "", "no discussion with that anchor is visible to the caller.")
	case errors.Is(err, discussion.ErrPostNotFound):
		return problem.New(problem.CodeNotFound, "", "", "no post with that id.")
	case errors.Is(err, discussion.ErrNotAuthor):
		return problem.New(problem.CodePermissionRequired, "", "", "only the author may change this post.")
	case errors.Is(err, discussion.ErrPostClosed):
		return problem.New(problem.CodeInvalidStateTransition, "", "", "the post is hidden or deleted.")
	case errors.Is(err, discussion.ErrEmptyBody):
		return fieldProblem("/body", problem.ReasonRequired, "the text is empty after trimming.")
	case errors.Is(err, discussion.ErrBodyTooLong):
		return fieldProblem("/body", problem.ReasonTooLong, "at most 2000 characters.")
	case errors.Is(err, discussion.ErrTooManyMentions):
		return fieldProblem("/mention_uids", problem.ReasonTooManyItems, "at most 10 people.")
	case errors.Is(err, discussion.ErrBadReplyTo):
		return fieldProblem("/reply_to_id", problem.ReasonUnknownReference, "not a post in this thread.")
	}
	return err
}

func fieldProblem(pointer, reason, detail string) *problem.Problem {
	p := problem.New(problem.CodeValidationFailed, "", "", detail)
	p.Errors = []problem.FieldError{{Pointer: pointer, Reason: reason, Detail: detail}}
	return p
}

func requireDiscussionUser(ctx context.Context) (int64, error) {
	uid, _, err := requireUser(ctx)
	if err != nil {
		return 0, err
	}
	if _, err := requireSite(ctx); err != nil {
		return 0, err
	}
	if thirdPartyFrom(ctx) {
		return 0, problem.New(problem.CodePermissionRequired, "", "", "discussions are open to first-party sites only.")
	}
	return uid, nil
}

func discussionViewer(ctx context.Context) discussion.Viewer {
	return discussion.Viewer{UID: userIDFrom(ctx), Roles: rolesFrom(ctx)}
}

func canModerateDiscussions(ctx context.Context) bool {
	return userIDFrom(ctx) > 0 && catalogPerm.Resolver.Can(rolesFrom(ctx), catalogPerm.DiscussionModerate)
}

func discussionPostFrom(p *discussion.Post, moderator bool) repr.DiscussionPost {
	out := repr.DiscussionPost{
		Object: "discussion_post", ID: repr.ID(p.ID), AnchorKind: p.AnchorKind, AnchorID: repr.ID(p.AnchorID),
		AuthorUID: repr.ID(p.AuthorUID), AuthorReviewer: p.AuthorReviewer, Body: p.Body,
		MentionUIDs: []string{}, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	switch p.Status {
	case discussion.StatusHidden:
		out.State = "hidden"
		if !moderator {
			out.Body = ""
		}
	case discussion.StatusDeleted:
		out.State, out.Body = "deleted", ""
	default:
		out.State = "visible"
	}
	if p.ReplyToID != nil {
		id := repr.ID(*p.ReplyToID)
		out.ReplyToID = &id
	}
	for _, uid := range discussion.MentionsOf(p) {
		out.MentionUIDs = append(out.MentionUIDs, repr.ID(uid))
	}
	if moderator && p.ModeratedAt != nil {
		note := p.ModerationNote
		out.ModerationNote = &note
	}
	if p.EditedAt != nil {
		at := p.EditedAt.UTC().Format(time.RFC3339)
		out.EditedAt = &at
	}
	return out
}

func (c *Catalog) attachDiscussionAuthors(ctx context.Context, posts []*repr.DiscussionPost) {
	if c.UsersOf == nil || len(posts) == 0 {
		return
	}
	seen := map[int64]bool{}
	var ids []int64
	for _, p := range posts {
		if id, ok := repr.ParseID(p.AuthorUID); ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	users, err := c.UsersOf(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "v2: account lookup for discussion posts failed; author objects stay null", "ids", len(ids), "err", err)
		return
	}
	for _, p := range posts {
		if id, ok := repr.ParseID(p.AuthorUID); ok {
			if u, found := users[id]; found {
				p.Author = &u
			}
		}
	}
}

func listCatalogDiscussion(cat *Catalog) func(context.Context, *discussionAnchorInput) (*discussionPageOutput, error) {
	return func(ctx context.Context, in *discussionAnchorInput) (*discussionPageOutput, error) {
		if in.Limit > 100 {
			return nil, catalogErr(ctx, problem.New(problem.CodeLimitTooLarge, "", "", "limit is at most 100."))
		}
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		limit := in.Limit
		if limit <= 0 {
			limit = discussionPageDefault
		}
		anchorID, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		kind := in.Object
		svc := cat.Discussions
		if err := svc.CheckVisible(ctx, kind, anchorID, discussionViewer(ctx)); err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		var after int64
		if in.Cursor != "" {
			n, err := strconv.ParseInt(strings.TrimPrefix(in.Cursor, "cur_"), 10, 64)
			if err != nil || n <= 0 {
				return nil, catalogErr(ctx, problem.New(problem.CodeInvalidCursor, "", "", "cursor is not one this face issued."))
			}
			after = n
		}
		rows, err := svc.List(ctx, kind, anchorID, after, limit+1)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		total, err := svc.Count(ctx, kind, anchorID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		moderator := canModerateDiscussions(ctx)
		page := repr.DiscussionPage{Object: "list", Items: []repr.DiscussionPost{}, Total: total}
		if len(rows) > limit {
			rows = rows[:limit]
			next := "cur_" + strconv.FormatInt(rows[len(rows)-1].ID, 10)
			page.NextCursor = &next
		}
		ptrs := make([]*repr.DiscussionPost, 0, len(rows))
		for i := range rows {
			page.Items = append(page.Items, discussionPostFrom(&rows[i], moderator))
		}
		for i := range page.Items {
			ptrs = append(ptrs, &page.Items[i])
		}
		cat.attachDiscussionAuthors(ctx, ptrs)
		if owner, err := svc.Owner(ctx, kind, anchorID); err == nil && owner > 0 {
			id := repr.ID(owner)
			page.OwnerUID = &id
		}
		if uid := userIDFrom(ctx); uid > 0 {
			state, err := svc.WatchState(ctx, kind, anchorID, uid)
			if err != nil {
				return nil, catalogErr(ctx, err)
			}
			page.Viewer = &repr.DiscussionViewer{
				CanPost: true, CanModerate: moderator,
				Watching: state.Watching, Muted: state.Muted,
			}
		}
		return &discussionPageOutput{Body: page}, nil
	}
}

func (c *Catalog) checkDiscussionText(ctx context.Context, uid int64, text string) (checkSkipped bool, err error) {
	if c.DiscussionTrust == nil {
		return false, nil
	}
	cctx, cancel := context.WithTimeout(ctx, ratingCheckTimeout)
	defer cancel()
	decision, matched, cerr := c.DiscussionTrust.Check(cctx, trustclient.CheckRequest{Site: RatingTrustSite, Text: text, AuthorID: &uid})
	switch {
	case cerr != nil:
		slog.Warn("discussion trust-check fail-open", "uid", uid, "err", cerr)
		return true, nil
	case decision == "deny":
		return false, fieldProblem("/body", problem.ReasonNotAllowedValue, "the text was refused by the content check; nothing was written.")
	case decision == "hold":
		slog.Info("discussion post held by trust", "uid", uid, "matched", matched)
	}
	return false, nil
}

func (c *Catalog) scanDiscussionPost(postID, uid int64, text string, checkSkipped bool) {
	if c.DiscussionTrust == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ratingScanTimeout)
		defer cancel()
		if _, _, err := c.DiscussionTrust.Scan(ctx, trustclient.ScanRequest{
			Site: RatingTrustSite, SubjectKind: DiscussionTrustSubjectKind,
			SubjectID: strconv.FormatInt(postID, 10), Text: text, AuthorID: &uid,
			CheckSkipped: checkSkipped,
		}); err != nil {
			slog.Warn("discussion trust-scan", "post_id", postID, "err", err)
		}
	}()
}

func createMyDiscussionPost(cat *Catalog) func(context.Context, *createDiscussionInput) (*discussionPostOutput, error) {
	return func(ctx context.Context, in *createDiscussionInput) (*discussionPostOutput, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		anchorID, ok := repr.ParseID(in.Body.AnchorID)
		if !ok {
			return nil, catalogErr(ctx, fieldProblem("/anchor_id", problem.ReasonInvalidFormat, "anchor_id must be a catalog id."))
		}
		svc := cat.Discussions
		if err := svc.CheckVisible(ctx, in.Body.AnchorKind, anchorID, discussionViewer(ctx)); err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		body, err := discussion.NormalizeBody(in.Body.Body)
		if err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		since := time.Now().Add(-time.Hour)
		used, err := svc.CountByAuthorSince(ctx, uid, since)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if limit := keys.CatalogDiscussionPostsPerHour.Get(); used >= limit {
			return nil, catalogErr(ctx, quotaExceeded("the discussion post limit for this account is exhausted", used, limit, time.Hour))
		}
		var replyTo *int64
		if in.Body.ReplyToID != "" {
			id, ok := repr.ParseID(in.Body.ReplyToID)
			if !ok {
				return nil, catalogErr(ctx, fieldProblem("/reply_to_id", problem.ReasonInvalidFormat, "reply_to_id must be a post id."))
			}
			replyTo = &id
		}
		mentions := make([]int64, 0, len(in.Body.MentionUIDs))
		for i, raw := range in.Body.MentionUIDs {
			id, ok := repr.ParseID(raw)
			if !ok {
				return nil, catalogErr(ctx, fieldProblem("/mention_uids/"+strconv.Itoa(i), problem.ReasonInvalidFormat, "mention_uids must be account ids."))
			}
			mentions = append(mentions, id)
		}
		checkSkipped, err := cat.checkDiscussionText(ctx, uid, body)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		post, err := svc.Create(ctx, discussion.CreateInput{
			Kind: in.Body.AnchorKind, AnchorID: anchorID, AuthorUID: uid, Roles: rolesFrom(ctx),
			Body: body, ReplyToID: replyTo, MentionUIDs: mentions,
		})
		if err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		cat.scanDiscussionPost(post.ID, uid, post.Body, checkSkipped)
		out := discussionPostFrom(post, canModerateDiscussions(ctx))
		cat.attachDiscussionAuthors(ctx, []*repr.DiscussionPost{&out})
		return &discussionPostOutput{Body: out}, nil
	}
}

func editMyDiscussionPost(cat *Catalog) func(context.Context, *editDiscussionInput) (*discussionPostOutput, error) {
	return func(ctx context.Context, in *editDiscussionInput) (*discussionPostOutput, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		body, err := discussion.NormalizeBody(in.Body.Body)
		if err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		checkSkipped, err := cat.checkDiscussionText(ctx, uid, body)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		post, err := cat.Discussions.Edit(ctx, id, uid, body)
		if err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		cat.scanDiscussionPost(post.ID, uid, post.Body, checkSkipped)
		out := discussionPostFrom(post, canModerateDiscussions(ctx))
		cat.attachDiscussionAuthors(ctx, []*repr.DiscussionPost{&out})
		return &discussionPostOutput{Body: out}, nil
	}
}

func deleteMyDiscussionPost(cat *Catalog) func(context.Context, *discussionPostIDInput) (*struct{}, error) {
	return func(ctx context.Context, in *discussionPostIDInput) (*struct{}, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		if err := cat.Discussions.Delete(ctx, id, uid); err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		return nil, nil
	}
}

func listMyDiscussionNotices(cat *Catalog) func(context.Context, *discussionNoticesInput) (*discussionNoticesOutput, error) {
	return func(ctx context.Context, in *discussionNoticesInput) (*discussionNoticesOutput, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		since := time.Now().Add(-discussionNoticeSince)
		if in.Since != "" {
			t, err := time.Parse(time.RFC3339, in.Since)
			if err != nil {
				return nil, catalogErr(ctx, problem.New(problem.CodeInvalidParameter, "", "", "since must be an RFC 3339 timestamp."))
			}
			since = t
		}
		limit := in.Limit
		if limit <= 0 || limit > 100 {
			limit = discussionNoticeLimit
		}
		svc := cat.Discussions
		rows, err := svc.Notices(ctx, uid, since, limit)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		owners := map[string]int64{}
		out := repr.List[repr.DiscussionNotice]{Object: "list", Items: make([]repr.DiscussionNotice, 0, len(rows))}
		for i := range rows {
			key := rows[i].AnchorKind + ":" + strconv.FormatInt(rows[i].AnchorID, 10)
			owner, seen := owners[key]
			if !seen {
				owner, _ = svc.Owner(ctx, rows[i].AnchorKind, rows[i].AnchorID)
				owners[key] = owner
			}
			out.Items = append(out.Items, repr.DiscussionNotice{
				Object: "discussion_notice", Reason: rows[i].Reason, ViewerIsOwner: owner == uid,
				Post: discussionPostFrom(&rows[i].Post, false),
			})
		}
		ptrs := make([]*repr.DiscussionPost, 0, len(out.Items))
		for i := range out.Items {
			ptrs = append(ptrs, &out.Items[i].Post)
		}
		cat.attachDiscussionAuthors(ctx, ptrs)
		return &discussionNoticesOutput{Body: out}, nil
	}
}

func watchMyDiscussion(cat *Catalog) func(context.Context, *watchDiscussionInput) (*discussionWatchOutput, error) {
	return func(ctx context.Context, in *watchDiscussionInput) (*discussionWatchOutput, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		anchorID, ok := repr.ParseID(in.Body.AnchorID)
		if !ok {
			return nil, catalogErr(ctx, fieldProblem("/anchor_id", problem.ReasonInvalidFormat, "anchor_id must be a catalog id."))
		}
		svc := cat.Discussions
		if err := svc.CheckVisible(ctx, in.Body.AnchorKind, anchorID, discussionViewer(ctx)); err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		if err := svc.SetMuted(ctx, in.Body.AnchorKind, anchorID, uid, in.Body.Muted); err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		return &discussionWatchOutput{Body: repr.DiscussionWatch{
			Object: "discussion_watch", AnchorKind: in.Body.AnchorKind, AnchorID: repr.ID(anchorID),
			Watching: !in.Body.Muted, Muted: in.Body.Muted,
		}}, nil
	}
}

func moderateDiscussionPost(cat *Catalog) func(context.Context, *moderateDiscussionInput) (*discussionPostOutput, error) {
	return func(ctx context.Context, in *moderateDiscussionInput) (*discussionPostOutput, error) {
		if err := cat.discussionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireDiscussionUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireModerationKey(ctx, catalogPerm.DiscussionModerate, rolesFrom(ctx)); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		post, err := cat.Discussions.Moderate(ctx, id, uid, in.Body.Hidden, strings.TrimSpace(in.Body.Note))
		if err != nil {
			return nil, catalogErr(ctx, discussionErr(err))
		}
		out := discussionPostFrom(post, true)
		cat.attachDiscussionAuthors(ctx, []*repr.DiscussionPost{&out})
		return &discussionPostOutput{Body: out}, nil
	}
}
