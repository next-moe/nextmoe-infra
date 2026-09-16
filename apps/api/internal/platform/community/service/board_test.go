package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func createBoard(t *testing.T, bs *BoardService, site, slug string, parent int64, mutate ...func(*BoardFields)) *Board {
	t.Helper()
	f := BoardFields{Slug: slug, Name: slug}
	if parent > 0 {
		f.ParentID = &parent
	}
	for _, m := range mutate {
		m(&f)
	}
	b, err := bs.Create(context.Background(), site, 1, f)
	if err != nil {
		t.Fatalf("create board %s: %v", slug, err)
	}
	return b
}

func wantErr[T error](t *testing.T, err error, what string) {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Fatalf("%s: want %T, got %v", what, target, err)
	}
}

func boardSlugs(boards []Board) []string {
	out := make([]string, len(boards))
	for i, b := range boards {
		out[i] = b.Slug
	}
	return out
}

func TestBoardCreateValidatesAndNests(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ctx := context.Background()

	for _, slug := range []string{"", "General", "a--b", "-a", "a_b", "123"} {
		_, err := bs.Create(ctx, "letmoe", 1, BoardFields{Slug: slug, Name: "x"})
		wantErr[*InvalidError](t, err, "slug "+slug)
	}
	_, err := bs.Create(ctx, "letmoe", 1, BoardFields{Slug: "blank", Name: "   "})
	wantErr[*InvalidError](t, err, "blank name")
	_, err = bs.Create(ctx, "letmoe", 1, BoardFields{Slug: "gated", Name: "x", TopicMinTrustLevel: 4})
	wantErr[*InvalidError](t, err, "a trust gate above the staff floor")

	general := createBoard(t, bs, "letmoe", "general", 0)
	_, err = bs.Create(ctx, "letmoe", 1, BoardFields{Slug: "general", Name: "again"})
	wantErr[*ConflictError](t, err, "duplicate slug")
	createBoard(t, bs, "nextmanga", "general", 0)

	child := createBoard(t, bs, "letmoe", "guides", general.ID)
	_, err = bs.Create(ctx, "letmoe", 1, BoardFields{Slug: "deeper", Name: "x", ParentID: &child.ID})
	wantErr[*InvalidError](t, err, "a sub-board as parent")
	foreign := createBoard(t, bs, "other", "foreign", 0)
	_, err = bs.Create(ctx, "letmoe", 1, BoardFields{Slug: "stray", Name: "x", ParentID: &foreign.ID})
	wantErr[*InvalidError](t, err, "another site's board as parent")

	desc := "  talk about anything  "
	createBoard(t, bs, "letmoe", "offtopic", 0, func(f *BoardFields) { f.Description = &desc })
	createBoard(t, bs, "letmoe", "faq", general.ID)

	list, err := bs.List("letmoe")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got, want := boardSlugs(list), []string{"general", "guides", "faq", "offtopic"}; !slices.Equal(got, want) {
		t.Fatalf("tree order: want %v, got %v", want, got)
	}
	if list[3].Description == nil || *list[3].Description != "talk about anything" {
		t.Fatalf("description should be trimmed, got %v", list[3].Description)
	}
	if list[1].Position != 0 || list[2].Position != 1 || list[3].Position != 1 {
		t.Fatalf("new boards append to their siblings: %d %d %d", list[1].Position, list[2].Position, list[3].Position)
	}
}

func TestBoardUpdateReparentsAndClears(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ctx := context.Background()
	general := createBoard(t, bs, "letmoe", "general", 0)
	news := createBoard(t, bs, "letmoe", "news", 0)
	icon := "📢"
	guides := createBoard(t, bs, "letmoe", "guides", general.ID, func(f *BoardFields) { f.Icon = &icon })

	top := int64(0)
	empty := ""
	archived := model.BoardStatusArchived
	got, err := bs.Update(ctx, "letmoe", guides.ID, 1, BoardPatch{ParentID: &top, Icon: &empty, Status: &archived})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.ParentID != nil || got.Icon != nil || got.Status != model.BoardStatusArchived || got.Position != 2 {
		t.Fatalf("want a top-level archived board at position 2 with no icon, got parent=%v icon=%v status=%d pos=%d",
			got.ParentID, got.Icon, got.Status, got.Position)
	}
	reread := getBoard(t, guides.ID)
	if reread.ParentID != nil || reread.Icon != nil {
		t.Fatalf("the clear must reach the row: %+v", reread)
	}
	if !got.UpdatedAt.Equal(reread.UpdatedAt) || !got.UpdatedAt.After(guides.UpdatedAt) {
		t.Fatalf("the response carries the written updated_at: got %v, row %v, before %v", got.UpdatedAt, reread.UpdatedAt, guides.UpdatedAt)
	}

	if _, err := bs.Update(ctx, "letmoe", news.ID, 1, BoardPatch{ParentID: &general.ID}); err != nil {
		t.Fatalf("nest news: %v", err)
	}
	_, err = bs.Update(ctx, "letmoe", general.ID, 1, BoardPatch{ParentID: &guides.ID})
	wantErr[*ConflictError](t, err, "a board with sub-boards becoming a sub-board")
	_, err = bs.Update(ctx, "letmoe", guides.ID, 1, BoardPatch{ParentID: &guides.ID})
	wantErr[*InvalidError](t, err, "its own parent")

	taken := "general"
	_, err = bs.Update(ctx, "letmoe", guides.ID, 1, BoardPatch{Slug: &taken})
	wantErr[*ConflictError](t, err, "renaming onto a taken slug")
	if _, err := bs.Update(ctx, "other", guides.ID, 1, BoardPatch{Slug: &taken}); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("another site's board must be invisible, got %v", err)
	}
}

func TestBoardDeleteNeedsAnEmptyBoard(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ctx := context.Background()
	general := createBoard(t, bs, "letmoe", "general", 0)
	child := createBoard(t, bs, "letmoe", "child", general.ID)

	wantErr[*ConflictError](t, bs.Delete(ctx, "letmoe", general.ID, 1), "delete with a sub-board")
	if err := bs.Delete(ctx, "letmoe", child.ID, 1); err != nil {
		t.Fatalf("delete an empty sub-board: %v", err)
	}

	th := openTopic(t, ts, "letmoe", 100, "general", "x")
	if err := testDB.Model(&model.CommunityThread{}).Where("id = ?", th.ID).
		Update("status", model.ThreadStatusDeleted).Error; err != nil {
		t.Fatalf("tombstone thread: %v", err)
	}
	wantErr[*ConflictError](t, bs.Delete(ctx, "letmoe", general.ID, 1), "a board still named by a deleted topic")
	if err := bs.Delete(ctx, "other", general.ID, 1); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("another site's board must be invisible, got %v", err)
	}
}

func TestBoardDeleteWaitsForATopicBeingOpened(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	board := createBoard(t, bs, "letmoe", "general", 0)

	opener := testDB.Begin()
	if _, err := repository.LockBoardTx(opener, "letmoe", board.ID, repository.LockShare); err != nil {
		t.Fatalf("share-lock board: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- bs.Delete(context.Background(), "letmoe", board.ID, 1) }()

	select {
	case err := <-done:
		opener.Rollback()
		t.Fatalf("delete must wait for the opener, returned %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := opener.Create(&model.CommunityThread{
		Site: "letmoe", Kind: model.ThreadKindTopic, AnchorKind: model.AnchorKindBoard,
		AnchorID: model.BoardAnchorID(board.ID), Status: model.ThreadStatusOpen, CreatedBy: 1,
	}).Error; err != nil {
		opener.Rollback()
		t.Fatalf("insert topic: %v", err)
	}
	if err := opener.Commit().Error; err != nil {
		t.Fatalf("commit opener: %v", err)
	}
	select {
	case err := <-done:
		wantErr[*ConflictError](t, err, "delete after the topic landed")
	case <-time.After(5 * time.Second):
		t.Fatal("delete never returned")
	}
}

func TestTopicOpenWaitsForABoardBeingDeleted(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	board := createBoard(t, bs, "letmoe", "general", 0)
	seedTrust(t, 100, model.TrustLevelBasic, 0)

	deleter := testDB.Begin()
	if _, err := repository.LockBoardTx(deleter, "letmoe", board.ID, repository.LockUpdate); err != nil {
		t.Fatalf("lock board: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := openOn(ts, board.ID, 100, false)
		done <- err
	}()

	select {
	case err := <-done:
		deleter.Rollback()
		t.Fatalf("opening must wait for the delete, returned %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := repository.DeleteBoardTx(deleter, board.ID); err != nil {
		deleter.Rollback()
		t.Fatalf("delete board: %v", err)
	}
	if err := deleter.Commit().Error; err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrBoardNotFound) {
			t.Fatalf("opening on a board deleted meanwhile: want ErrBoardNotFound, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening never returned")
	}
	var orphans int64
	if err := testDB.Model(&model.CommunityThread{}).Where("anchor_kind = ?", model.AnchorKindBoard).Count(&orphans).Error; err != nil {
		t.Fatalf("count topics: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("no topic may land on the deleted board, found %d", orphans)
	}
}

func TestBoardReorder(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ctx := context.Background()
	a := createBoard(t, bs, "letmoe", "a", 0)
	b := createBoard(t, bs, "letmoe", "b", 0)
	c := createBoard(t, bs, "letmoe", "c", 0)
	a1 := createBoard(t, bs, "letmoe", "a1", a.ID)
	a2 := createBoard(t, bs, "letmoe", "a2", a.ID)

	wantErr[*InvalidError](t, bs.Reorder(ctx, "letmoe", 0, []int64{c.ID, a.ID}, 1), "a partial order")
	wantErr[*InvalidError](t, bs.Reorder(ctx, "letmoe", 0, []int64{c.ID, a.ID, a.ID}, 1), "a repeated id")
	wantErr[*InvalidError](t, bs.Reorder(ctx, "letmoe", 0, []int64{c.ID, a.ID, a1.ID}, 1), "a child in the top level")
	if err := bs.Reorder(ctx, "letmoe", 99999, []int64{}, 1); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("unknown parent: want ErrBoardNotFound, got %v", err)
	}

	if err := bs.Reorder(ctx, "letmoe", 0, []int64{c.ID, a.ID, b.ID}, 1); err != nil {
		t.Fatalf("reorder top: %v", err)
	}
	if err := bs.Reorder(ctx, "letmoe", a.ID, []int64{a2.ID, a1.ID}, 1); err != nil {
		t.Fatalf("reorder children: %v", err)
	}
	list, err := bs.List("letmoe")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got, want := boardSlugs(list), []string{"c", "a", "a2", "a1", "b"}; !slices.Equal(got, want) {
		t.Fatalf("order: want %v, got %v", want, got)
	}
}

func TestBoardStatsCountListedTopics(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()
	general := createBoard(t, bs, "letmoe", "general", 0)
	quiet := createBoard(t, bs, "letmoe", "quiet", 0)

	first := openTopic(t, ts, "letmoe", 100, "general", "first")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	if _, err := ps.Reply(ctx, ReplyParams{ThreadID: first.ID, AuthorID: 200, BodyRaw: "r1"}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	latest := openTopic(t, ts, "letmoe", 100, "general", "latest")

	seedTrust(t, 300, model.TrustLevelNew, 1)
	if _, _, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 300, BoardID: general.ID, Title: "held", BodyRaw: "held",
	}); err != nil {
		t.Fatalf("held topic: %v", err)
	}
	gone := openTopic(t, ts, "letmoe", 100, "general", "gone")
	if err := testDB.Model(&model.CommunityPost{}).Where("thread_id = ? AND post_number = 1", gone.ID).
		Update("status", model.PostStatusDeleted).Error; err != nil {
		t.Fatalf("tombstone opening: %v", err)
	}

	got, err := bs.Get("letmoe", general.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	st := got.Stats
	if st.TopicsCount != 2 || st.PostsCount != 3 {
		t.Fatalf("want 2 listed topics holding 3 posts, got %d/%d", st.TopicsCount, st.PostsCount)
	}
	if st.LastThreadID == nil || *st.LastThreadID != latest.ID || st.LastPostedAt == nil {
		t.Fatalf("latest topic should be %d, got %v", latest.ID, st.LastThreadID)
	}

	list, err := bs.List("letmoe")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, b := range list {
		switch b.ID {
		case general.ID:
			if b.Stats.TopicsCount != st.TopicsCount || b.Stats.PostsCount != st.PostsCount ||
				*b.Stats.LastThreadID != *st.LastThreadID || !b.Stats.LastPostedAt.Equal(*st.LastPostedAt) {
				t.Fatalf("the listing and the single read disagree: %+v vs %+v", b.Stats, st)
			}
		case quiet.ID:
			if b.Stats.TopicsCount != 0 || b.Stats.LastThreadID != nil {
				t.Fatalf("an empty board has no stats, got %+v", b.Stats)
			}
		}
	}
}

func getBoard(t *testing.T, id int64) *model.CommunityBoard {
	t.Helper()
	var b model.CommunityBoard
	if err := testDB.First(&b, id).Error; err != nil {
		t.Fatalf("reload board %d: %v", id, err)
	}
	return &b
}
