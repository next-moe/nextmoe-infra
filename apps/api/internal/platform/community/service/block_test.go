package service

import (
	"context"
	"errors"
	"testing"

	"api/internal/platform/community/model"
)

func followEdges(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Count(&n).Error; err != nil {
		t.Fatalf("count follows: %v", err)
	}
	return n
}

func TestBlockRemovesFollowsBothWays(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	const A, B, C int64 = 1, 2, 3

	for _, p := range [][2]int64{{A, B}, {B, A}, {A, C}, {C, B}} {
		if _, err := s.Follow(ctx, "kungal", p[0], p[1]); err != nil {
			t.Fatalf("follow %v: %v", p, err)
		}
	}
	created, err := s.Block(ctx, "moyu", A, B)
	if err != nil || !created {
		t.Fatalf("first block: created=%v err=%v", created, err)
	}
	if n := followEdges(t); n != 2 {
		t.Fatalf("only A<->B edges go; want 2 left (A->C, C->B), got %d", n)
	}
	var row model.CommunityUserBlock
	if err := testDB.Where("blocker_id = ? AND blocked_id = ?", A, B).Take(&row).Error; err != nil {
		t.Fatalf("reload block: %v", err)
	}
	if row.OriginSite != "moyu" || row.CreatedAt.IsZero() {
		t.Fatalf("block row: %+v", row)
	}

	created, err = s.Block(ctx, "kungal", A, B)
	if err != nil || created {
		t.Fatalf("repeat block must be a no-op: created=%v err=%v", created, err)
	}
}

func TestFollowRefusedWhileEitherBlocks(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	const A, B int64 = 1, 2

	if _, err := s.Block(ctx, "kungal", A, B); err != nil {
		t.Fatalf("block: %v", err)
	}
	var forbidden *ForbiddenError
	for _, p := range [][2]int64{{A, B}, {B, A}} {
		_, err := s.Follow(ctx, "kungal", p[0], p[1])
		if !errors.As(err, &forbidden) {
			t.Fatalf("follow %v across a block: want ForbiddenError, got %v", p, err)
		}
	}
	if n := followEdges(t); n != 0 {
		t.Fatalf("no edge may be written across a block, got %d", n)
	}

	deleted, err := s.Unblock(ctx, A, B)
	if err != nil || !deleted {
		t.Fatalf("unblock: deleted=%v err=%v", deleted, err)
	}
	deleted, err = s.Unblock(ctx, A, B)
	if err != nil || deleted {
		t.Fatalf("repeat unblock must be a no-op: deleted=%v err=%v", deleted, err)
	}
	if _, err := s.Follow(ctx, "kungal", B, A); err != nil {
		t.Fatalf("follow after unblock: %v", err)
	}
}

func TestBlockRejectsSelfAndNonPositiveIDs(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	_, err := s.Block(ctx, "kungal", 1, 1)
	wantInvalid(t, err, "cannot block yourself")
	_, err = s.Block(ctx, "kungal", 0, 1)
	wantInvalid(t, err, "user ids must be positive")
	_, err = s.Unblock(ctx, 1, -2)
	wantInvalid(t, err, "user ids must be positive")
}

func TestListBlockingNewestFirstWithCursor(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	for _, target := range []int64{11, 12, 13} {
		if _, err := s.Block(ctx, "kungal", 1, target); err != nil {
			t.Fatalf("block %d: %v", target, err)
		}
	}
	if _, err := s.Block(ctx, "kungal", 2, 1); err != nil {
		t.Fatalf("someone blocks 1: %v", err)
	}
	page, err := s.ListBlocking(1, 0, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page) != 2 || page[0].BlockedID != 13 || page[1].BlockedID != 12 {
		t.Fatalf("page 1: %+v", page)
	}
	rest, err := s.ListBlocking(1, page[1].ID, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(rest) != 1 || rest[0].BlockedID != 11 {
		t.Fatalf("page 2 must hold only 1's own oldest block: %+v", rest)
	}
}

func TestFollowStatesReportBlocksBothWays(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	const viewer, blockedByViewer, blocksViewer, stranger int64 = 1, 2, 3, 4

	if _, err := s.Block(ctx, "kungal", viewer, blockedByViewer); err != nil {
		t.Fatalf("viewer blocks: %v", err)
	}
	if _, err := s.Block(ctx, "kungal", blocksViewer, viewer); err != nil {
		t.Fatalf("blocks viewer: %v", err)
	}
	if _, err := s.Block(ctx, "kungal", stranger, blockedByViewer); err != nil {
		t.Fatalf("unrelated block: %v", err)
	}
	states, err := s.States(viewer, []int64{blockedByViewer, blocksViewer, stranger})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	want := map[int64][2]bool{
		blockedByViewer: {true, false},
		blocksViewer:    {false, true},
		stranger:        {false, false},
	}
	for _, st := range states {
		w := want[st.UserID]
		if st.ViewerBlocks != w[0] || st.BlocksViewer != w[1] {
			t.Errorf("user %d: viewer_blocks=%v blocks_viewer=%v, want %v", st.UserID, st.ViewerBlocks, st.BlocksViewer, w)
		}
	}
	anon, err := s.States(0, []int64{blockedByViewer})
	if err != nil {
		t.Fatalf("anonymous states: %v", err)
	}
	if anon[0].ViewerBlocks || anon[0].BlocksViewer {
		t.Fatalf("anonymous viewer has no blocks: %+v", anon[0])
	}
}

func TestNotificationsSkipWhoeverBlockedTheActor(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	fs := NewFollowService(testDB)
	ctx := letmoeCtx()
	const opener, actor, mentioned, bystander int64 = 100, 200, 300, 400

	th := openTopic(t, ts, "letmoe", opener, "b1", "opening")
	if _, err := fs.Block(ctx, "letmoe", opener, actor); err != nil {
		t.Fatalf("opener blocks actor: %v", err)
	}
	if _, err := fs.Block(ctx, "letmoe", mentioned, actor); err != nil {
		t.Fatalf("mentioned blocks actor: %v", err)
	}
	if _, err := fs.Block(ctx, "letmoe", actor, bystander); err != nil {
		t.Fatalf("actor blocks bystander: %v", err)
	}
	target := opener
	replyTo(t, ps, ctx, th.ID, actor, &target, []int64{mentioned, bystander}, "hi")
	processBatch(t)

	if n := notifsOf(t, opener); len(n) != 0 {
		t.Fatalf("the opener blocked the replier and must hear nothing, got kinds %v", kindsOf(n))
	}
	if n := notifsOf(t, mentioned); len(n) != 0 {
		t.Fatalf("a mention from a blocked user must not notify, got kinds %v", kindsOf(n))
	}
	if n := notifsOf(t, bystander); len(n) != 1 || n[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("the actor's own block does not silence the actor's mention of them, got kinds %v", kindsOf(n))
	}
}
