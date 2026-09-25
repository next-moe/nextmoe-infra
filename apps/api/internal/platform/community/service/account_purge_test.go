package service

import (
	"context"
	"testing"

	"api/internal/platform/community/model"
)

func TestPurgeAccountReachesEveryTenant(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()
	kungal, moyu := WithCallerSite(ctx, "kungal"), WithCallerSite(ctx, "moyu")
	const opener, gone, bystander int64 = 1, 2, 3

	seedTrust(t, opener, model.TrustLevelBasic, 0)
	kungalWall, _, err := ps.Comment(kungal, CommentParams{
		Site: "kungal", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w7", AuthorID: opener, BodyRaw: "wall",
	})
	if err != nil {
		t.Fatalf("open kungal wall: %v", err)
	}
	moyuWall, _, err := ps.Comment(moyu, CommentParams{
		Site: "moyu", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w8", AuthorID: opener, BodyRaw: "wall",
	})
	if err != nil {
		t.Fatalf("open moyu wall: %v", err)
	}
	replyTo(t, ps, kungal, kungalWall.ID, gone, nil, nil, "on kungal")
	replyTo(t, ps, moyu, moyuWall.ID, gone, nil, nil, "on moyu")
	replyTo(t, ps, letmoeCtx(), kungalWall.ID, gone, nil, nil, "through letmoe, which owns no thread")
	replyTo(t, ps, kungal, kungalWall.ID, bystander, nil, nil, "stays")
	fs := NewFollowService(testDB)
	if _, err := fs.Follow(kungal, "kungal", gone, opener); err != nil {
		t.Fatalf("gone follows opener: %v", err)
	}
	if _, err := fs.Follow(kungal, "kungal", opener, gone); err != nil {
		t.Fatalf("opener follows gone: %v", err)
	}
	if _, err := fs.Follow(kungal, "kungal", bystander, opener); err != nil {
		t.Fatalf("bystander follows opener: %v", err)
	}
	processBatch(t)
	if got := threadUserSite(t, kungalWall.ID, gone); got != "letmoe" {
		t.Fatalf("setup: the letmoe reply must leave a letmoe row, got %q", got)
	}

	if err := ps.PurgeAccount(ctx, gone); err != nil {
		t.Fatalf("purge account: %v", err)
	}

	count := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := testDB.Raw(q, args...).Scan(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM community_post WHERE author_id = ? AND (status <> ? OR content_raw <> '')`,
		gone, model.PostStatusDeleted); n != 0 {
		t.Fatalf("%d of the account's posts survived", n)
	}
	if n := count(`SELECT count(*) FROM community_thread_user WHERE user_id = ?`, gone); n != 0 {
		t.Fatalf("%d thread rows survived; the letmoe-delivered one needs letmoe's purge", n)
	}
	if n := count(`SELECT count(*) FROM community_trust WHERE user_id = ?`, gone); n != 0 {
		t.Fatal("the account's trust row survived")
	}
	if n := count(`SELECT count(*) FROM community_post WHERE author_id = ? AND status <> ? AND content_raw = 'stays'`,
		bystander, model.PostStatusDeleted); n != 1 {
		t.Fatal("the bystander's post was touched")
	}
	if n := count(`SELECT count(*) FROM community_trust WHERE user_id = ?`, bystander); n != 1 {
		t.Fatal("the bystander's trust row was touched")
	}
	if n := count(`SELECT count(*) FROM community_user_follow WHERE follower_id = ? OR followee_id = ?`, gone, gone); n != 0 {
		t.Fatalf("%d follow edges naming the account survived", n)
	}
	if n := count(`SELECT count(*) FROM community_user_follow`); n != 1 {
		t.Fatal("the bystander's follow was touched")
	}

	if err := ps.PurgeAccount(ctx, gone); err != nil {
		t.Fatalf("a second purge must be a no-op: %v", err)
	}
}

func TestSitePurgeLeavesFollows(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	fs := NewFollowService(testDB)
	ctx := context.Background()
	kungal := WithCallerSite(ctx, "kungal")
	const author, other int64 = 2, 3

	seedTrust(t, author, model.TrustLevelBasic, 0)
	if _, _, err := ps.Comment(kungal, CommentParams{
		Site: "kungal", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w9", AuthorID: author, BodyRaw: "wall",
	}); err != nil {
		t.Fatalf("open wall: %v", err)
	}
	if _, err := fs.Follow(kungal, "kungal", author, other); err != nil {
		t.Fatalf("author follows other: %v", err)
	}
	if _, err := fs.Follow(kungal, "kungal", other, author); err != nil {
		t.Fatalf("other follows author: %v", err)
	}

	if _, err := ps.PurgeAuthor(kungal, "kungal", author); err != nil {
		t.Fatalf("purge author: %v", err)
	}
	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Count(&n).Error; err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 2 {
		t.Fatalf("site author purge must leave follow edges, got %d", n)
	}
}
