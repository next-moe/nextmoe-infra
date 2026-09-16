package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
)

// Before the read faces carried a count, a site that wanted to render one had
// to mirror every like in a table of its own and dual-write it on each toggle.
func TestReadFacesCarryReactionCounts(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "42")
	posts := replyN(t, s, ctx, th, 700, 2)
	liked, untouched := posts[0], posts[1]

	for _, user := range []int64{801, 802, 803} {
		if _, err := s.toggleReaction(ctx, &toggleReactionInput{
			ID: liked, Body: dto.ReactionToggleRequest{UserID: user, Kind: model.ReactionKindLike},
		}); err != nil {
			t.Fatalf("toggle by %d: %v", user, err)
		}
	}

	byID := func(views []dto.PostView) map[int64]dto.PostView {
		out := make(map[int64]dto.PostView, len(views))
		for _, v := range views {
			out[v.ID] = v
		}
		return out
	}

	page, err := s.getComments(ctx, &commentsPageInput{
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "42", ViewerID: 802,
	})
	if err != nil {
		t.Fatalf("getComments: %v", err)
	}
	got := byID(page.Body.Data.Posts)
	if got[liked].ReactionCount != 3 {
		t.Fatalf("liked post: want reaction_count 3, got %d", got[liked].ReactionCount)
	}
	if !got[liked].ViewerReacted {
		t.Fatal("viewer 802 liked this post; viewer_reacted must be true")
	}
	if got[untouched].ReactionCount != 0 || got[untouched].ViewerReacted {
		t.Fatalf("untouched post: want 0/false, got %d/%v",
			got[untouched].ReactionCount, got[untouched].ViewerReacted)
	}

	// A request that names no viewer still gets the count, and never a true.
	anon, err := s.getComments(ctx, &commentsPageInput{
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "42",
	})
	if err != nil {
		t.Fatalf("getComments (anonymous): %v", err)
	}
	if a := byID(anon.Body.Data.Posts)[liked]; a.ReactionCount != 3 || a.ViewerReacted {
		t.Fatalf("anonymous read: want 3/false, got %d/%v", a.ReactionCount, a.ViewerReacted)
	}

	// A viewer who liked nothing.
	stranger, err := s.getComments(ctx, &commentsPageInput{
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "42", ViewerID: 999,
	})
	if err != nil {
		t.Fatalf("getComments (stranger): %v", err)
	}
	if st := byID(stranger.Body.Data.Posts)[liked]; st.ViewerReacted {
		t.Fatal("viewer 999 has liked nothing; viewer_reacted must be false")
	}

	feed, err := s.listSitePosts(ctx, &sitePostsInput{Kind: model.ThreadKindComments, AnchorKind: -1, ViewerID: 801})
	if err != nil {
		t.Fatalf("listSitePosts: %v", err)
	}
	for _, row := range feed.Body.Data.Posts {
		if row.Post.ID != liked {
			continue
		}
		if row.Post.ReactionCount != 3 || !row.Post.ViewerReacted {
			t.Fatalf("site feed: want 3/true, got %d/%v", row.Post.ReactionCount, row.Post.ViewerReacted)
		}
	}

	resolved := resolvePosts(t, s, ctx, []int64{liked, untouched})
	if len(resolved) != 2 || resolved[0].Post.ReactionCount != 3 {
		t.Fatalf("posts/resolve must carry the count, got %+v", resolved)
	}

	// Un-toggling takes the count back down — the mirror this replaces could not
	// be trusted to, because its two writes are not one transaction.
	if _, err := s.toggleReaction(ctx, &toggleReactionInput{
		ID: liked, Body: dto.ReactionToggleRequest{UserID: 803, Kind: model.ReactionKindLike},
	}); err != nil {
		t.Fatalf("un-toggle: %v", err)
	}
	after, err := s.getComments(ctx, &commentsPageInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "42"})
	if err != nil {
		t.Fatalf("getComments after un-toggle: %v", err)
	}
	if n := byID(after.Body.Data.Posts)[liked].ReactionCount; n != 2 {
		t.Fatalf("want reaction_count 2 after un-toggle, got %d", n)
	}
}

func TestTopAuthorsRanksThisSiteOnly(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	moyu := clientCtx("moyu")
	forum := clientCtx("kungal")
	for _, u := range []int64{901, 902, 903} {
		seedTL1(t, u)
	}

	th := resolve(t, s, moyu, model.AnchorKindSiteGame, "7")
	replyN(t, s, moyu, th, 901, 3)
	replyN(t, s, moyu, th, 902, 1)

	// Same author, a wall on the other tenant. A ranking that answered the
	// caller with this would put the forum's commenters on moyu's board.
	other := resolve(t, s, forum, model.AnchorKindSiteGame, "7")
	replyN(t, s, forum, other, 903, 9)

	out, err := s.topAuthors(moyu, &topAuthorsInput{Kind: -1, AnchorKind: -1, Limit: 10})
	if err != nil {
		t.Fatalf("topAuthors: %v", err)
	}
	stats := out.Body.Data.Stats
	if len(stats) != 2 {
		t.Fatalf("want 2 authors on moyu, got %d (%+v)", len(stats), stats)
	}
	if stats[0].AuthorID != 901 || stats[0].VisiblePosts != 3 {
		t.Fatalf("want 901 with 3 posts first, got %+v", stats[0])
	}
	if stats[1].AuthorID != 902 || stats[1].VisiblePosts != 1 {
		t.Fatalf("want 902 with 1 post second, got %+v", stats[1])
	}
}

// The edit face returns the post, and it is not a read face: moyu reported it
// answering reaction_count 0 on a post with likes, and was refilling the count
// from a resolve it had done before the edit.
func TestEditPostCarriesReactionCount(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "77")
	postID := replyN(t, s, ctx, th, 700, 1)[0]

	for _, user := range []int64{700, 801} {
		if _, err := s.toggleReaction(ctx, &toggleReactionInput{
			ID: postID, Body: dto.ReactionToggleRequest{UserID: user, Kind: model.ReactionKindLike},
		}); err != nil {
			t.Fatalf("toggle by %d: %v", user, err)
		}
	}

	out, err := s.editPost(ctx, &editPostInput{
		ID: postID, Body: dto.EditPostRequest{AuthorID: 700, Body: "edited"},
	})
	if err != nil {
		t.Fatalf("editPost: %v", err)
	}
	if got := out.Body.Data.Post.ReactionCount; got != 2 {
		t.Fatalf("an edit must not zero the likes: want reaction_count 2, got %d", got)
	}
	if !out.Body.Data.Post.ViewerReacted {
		t.Fatal("the acting user liked this post; viewer_reacted must be true for them")
	}

	// A moderator editing someone else's post is the viewer of the response.
	modOut, err := s.editPost(ctx, &editPostInput{
		ID: postID, Body: dto.EditPostRequest{AuthorID: 909, Body: "moderated", AsModerator: true},
	})
	if err != nil {
		t.Fatalf("editPost as moderator: %v", err)
	}
	if got := modOut.Body.Data.Post.ReactionCount; got != 2 {
		t.Fatalf("moderator edit: want reaction_count 2, got %d", got)
	}
	if modOut.Body.Data.Post.ViewerReacted {
		t.Fatal("the moderator never liked this post; viewer_reacted must be false")
	}
}
