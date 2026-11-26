package handler

import (
	"context"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
)

func wallComment(t *testing.T, s *Server, ctx context.Context, anchorKind int16, anchorID string, author int64, replyTo *int64) dto.PostView {
	t.Helper()
	out, err := s.comment(ctx, &commentInput{Body: dto.CommentRequest{
		AnchorKind: anchorKind, AnchorID: anchorID, AuthorID: author, Body: "c", ReplyToPostID: replyTo,
	}})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	return *out.Body.Data.Post
}

func like(t *testing.T, s *Server, ctx context.Context, postID int64, users ...int64) {
	t.Helper()
	for _, u := range users {
		if _, err := s.setReaction(ctx, &toggleReactionInput{ID: postID, Body: dto.ReactionToggleRequest{UserID: u}}); err != nil {
			t.Fatalf("like: %v", err)
		}
	}
}

func readWall(t *testing.T, s *Server, ctx context.Context, in commentsPageInput) dto.CommentsPage {
	t.Helper()
	if in.AnchorKind == 0 {
		in.AnchorKind, in.AnchorID = model.AnchorKindSiteGame, "g1"
	}
	out, err := s.getComments(ctx, &in)
	if err != nil {
		t.Fatalf("getComments(%+v): %v", in, err)
	}
	return out.Body.Data
}

func numbers(posts []dto.PostView) []int32 {
	out := make([]int32, len(posts))
	for i, p := range posts {
		out[i] = p.PostNumber
	}
	return out
}

func sameNumbers(t *testing.T, what string, got []dto.PostView, want ...int32) {
	t.Helper()
	g := numbers(got)
	if len(g) != len(want) {
		t.Fatalf("%s: post numbers %v, want %v", what, g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("%s: post numbers %v, want %v", what, g, want)
		}
	}
}

// Wall g1: roots 1, 2, 4; replies 3 and 5 under 1. Likes: 2 has two, 4 has one.
func seedWall(t *testing.T, s *Server, ctx context.Context) (a, b, c dto.PostView) {
	t.Helper()
	a = wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 100, nil)
	b = wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 101, nil)
	wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 102, &a.ID)
	c = wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 103, nil)
	wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 104, &a.ID)
	like(t, s, ctx, b.ID, 201, 202)
	like(t, s, ctx, c.ID, 201)
	return a, b, c
}

func TestWall_SortsAndRoots(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("moyu")
	a, _, _ := seedWall(t, s, ctx)

	sameNumbers(t, "oldest flat", readWall(t, s, ctx, commentsPageInput{}).Posts, 1, 2, 3, 4, 5)

	p1 := readWall(t, s, ctx, commentsPageInput{Sort: "newest", RootsOnly: true, Limit: 2})
	sameNumbers(t, "newest roots page 1", p1.Posts, 4, 2)
	p2 := readWall(t, s, ctx, commentsPageInput{Sort: "newest", RootsOnly: true, Limit: 2, Cursor: p1.NextCursor})
	sameNumbers(t, "newest roots page 2", p2.Posts, 1)
	if *p2.Posts[0].ReplyCount != 2 || *p1.Posts[0].ReplyCount != 0 {
		t.Fatalf("reply counts: root 1 has two replies, root 4 none: %+v / %+v", p2.Posts[0], p1.Posts[0])
	}

	t1 := readWall(t, s, ctx, commentsPageInput{Sort: "top", RootsOnly: true, Limit: 2})
	sameNumbers(t, "top page 1", t1.Posts, 2, 4)
	t2 := readWall(t, s, ctx, commentsPageInput{Sort: "top", RootsOnly: true, Limit: 2, Cursor: t1.NextCursor})
	sameNumbers(t, "top page 2", t2.Posts, 1)
	if t1.Posts[0].ReactionCount != 2 {
		t.Fatalf("top rows carry their like count: %+v", t1.Posts[0])
	}

	pv := readWall(t, s, ctx, commentsPageInput{RootsOnly: true, ReplyPreview: 1})
	sameNumbers(t, "oldest roots", pv.Posts, 1, 2, 4)
	sameNumbers(t, "previews", pv.ReplyPreviews, 3)

	oldest := readWall(t, s, ctx, commentsPageInput{Limit: 2})
	if oldest.NextCursor != "2" {
		t.Fatalf("an oldest cursor stays a bare post_number for callers that pass it as after: %q", oldest.NextCursor)
	}
	sameNumbers(t, "oldest via after", readWall(t, s, ctx, commentsPageInput{After: 2, Limit: 2}).Posts, 3, 4)

	replies, err := s.listReplies(ctx, &listRepliesInput{ID: a.ID})
	if err != nil {
		t.Fatalf("listReplies: %v", err)
	}
	sameNumbers(t, "replies of 1", replies.Body.Data.Posts, 3, 5)

	for name, in := range map[string]commentsPageInput{
		"cursor of another sort": {Sort: "top", Cursor: p1.NextCursor},
		"after on newest":        {Sort: "newest", After: 2},
		"preview without roots":  {ReplyPreview: 1},
		"cursor and after":       {Cursor: "2", After: 2},
	} {
		in.AnchorKind, in.AnchorID = model.AnchorKindSiteGame, "g1"
		if _, err := s.getComments(ctx, &in); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestWall_Pins(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("moyu")
	a, b, c := seedWall(t, s, ctx)
	d := wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 105, nil)

	pin := func(id int64) (*postChangeOutput, error) {
		return s.pinPost(ctx, &pinPostInput{ID: id, Body: dto.PostActorRequest{ActorID: 9}})
	}
	for _, p := range []dto.PostView{c, a, b} {
		out, err := pin(p.ID)
		if err != nil || !out.Body.Data.Changed || out.Body.Data.Post.PinnedAt == nil {
			t.Fatalf("pin %d: %+v %v", p.ID, out, err)
		}
	}
	again, err := pin(a.ID)
	if err != nil || again.Body.Data.Changed {
		t.Fatalf("pinning a pinned comment changes nothing: %+v %v", again, err)
	}
	if _, err := pin(d.ID); err == nil {
		t.Fatalf("a fourth pin on one wall should be refused")
	}
	reply := readWall(t, s, ctx, commentsPageInput{}).Posts[2]
	if _, err := pin(reply.ID); err == nil {
		t.Fatalf("a reply cannot be pinned")
	}

	first := readWall(t, s, ctx, commentsPageInput{Limit: 2})
	sameNumbers(t, "pinned, newest pin first", first.PinnedPosts, b.PostNumber, a.PostNumber, c.PostNumber)
	if first.Posts[0].PinnedAt == nil || first.Posts[1].PinnedAt == nil {
		t.Fatalf("a pinned post in the page carries pinned_at: %+v", first.Posts)
	}
	if next := readWall(t, s, ctx, commentsPageInput{Limit: 2, Cursor: first.NextCursor}); len(next.PinnedPosts) != 0 {
		t.Fatalf("pins come on the first page only: %+v", next.PinnedPosts)
	}
	if other := readWall(t, s, clientCtx("kungal"), commentsPageInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1"}); other.Thread != nil {
		t.Fatalf("a site-local wall is not another site's")
	}

	if _, err := s.deletePost(ctx, &deletePostInput{ID: b.ID, AuthorID: 9, AsModerator: true}); err != nil {
		t.Fatalf("mod delete: %v", err)
	}
	sameNumbers(t, "a removed post leaves the pinned set", readWall(t, s, ctx, commentsPageInput{}).PinnedPosts, a.PostNumber, c.PostNumber)
	if out, err := pin(d.ID); err != nil || !out.Body.Data.Changed {
		t.Fatalf("the removed post's pin no longer counts toward the limit: %v", err)
	}

	un, err := s.unpinPost(ctx, &unpinPostInput{ID: a.ID, ActorID: 9})
	if err != nil || !un.Body.Data.Changed || un.Body.Data.Post.PinnedAt != nil {
		t.Fatalf("unpin: %+v %v", un, err)
	}
	if un, err := s.unpinPost(ctx, &unpinPostInput{ID: a.ID, ActorID: 9}); err != nil || un.Body.Data.Changed {
		t.Fatalf("unpinning again changes nothing: %+v %v", un, err)
	}
}

func TestWall_CatalogPinsArePerSite(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	moyu, kungal := clientCtx("moyu"), clientCtx("kungal")
	post := wallComment(t, s, moyu, model.AnchorKindCatalogWork, "42", 100, nil)
	if _, err := s.pinPost(kungal, &pinPostInput{ID: post.ID, Body: dto.PostActorRequest{ActorID: 9}}); err != nil {
		t.Fatalf("pin on a shared wall: %v", err)
	}
	in := commentsPageInput{AnchorKind: model.AnchorKindCatalogWork, AnchorID: "42"}
	if got := readWall(t, s, kungal, in); len(got.PinnedPosts) != 1 {
		t.Fatalf("the pinning site sees its pin: %+v", got.PinnedPosts)
	}
	if got := readWall(t, s, moyu, in); len(got.PinnedPosts) != 0 || got.Posts[0].PinnedAt != nil {
		t.Fatalf("another site's pin does not show: %+v", got)
	}
}

func TestWall_Restore(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("moyu")
	own := wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 100, nil)
	modded := wallComment(t, s, ctx, model.AnchorKindSiteGame, "g1", 101, nil)

	if _, err := s.deletePost(ctx, &deletePostInput{ID: own.ID, AuthorID: 100}); err != nil {
		t.Fatalf("self delete: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: modded.ID, AuthorID: 9, AsModerator: true}); err != nil {
		t.Fatalf("mod delete: %v", err)
	}
	posts := readWall(t, s, ctx, commentsPageInput{}).Posts
	if posts[0].DeleteSource != "author" || posts[1].DeleteSource != "moderator" {
		t.Fatalf("a tombstone says who removed it: %q %q", posts[0].DeleteSource, posts[1].DeleteSource)
	}

	restore := func(id int64) (*postChangeOutput, error) {
		return s.restorePost(ctx, &restorePostInput{ID: id, Body: dto.PostActorRequest{ActorID: 9}})
	}
	if _, err := restore(own.ID); err == nil {
		t.Fatalf("an author's own delete cannot be undone by a moderator")
	}
	out, err := restore(modded.ID)
	if err != nil || !out.Body.Data.Changed || out.Body.Data.Post.Status != model.PostStatusVisible || out.Body.Data.Post.DeleteSource != "" {
		t.Fatalf("restore a moderator's removal: %+v %v", out, err)
	}
	if out, err := restore(modded.ID); err != nil || out.Body.Data.Changed {
		t.Fatalf("restoring a visible post changes nothing: %+v %v", out, err)
	}
	if _, err := s.restorePost(clientCtx("kungal"), &restorePostInput{ID: modded.ID, Body: dto.PostActorRequest{ActorID: 9}}); err == nil {
		t.Fatalf("another site cannot reach a site-local post")
	}
}
