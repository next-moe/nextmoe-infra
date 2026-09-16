package handler

import (
	"net/http"
	"slices"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func boardServer() *Server {
	s := newTenantServer()
	s.boards = service.NewBoardService(testDB)
	return s
}

func TestBoardFacesAreSiteScoped(t *testing.T) {
	cleanTables(t)
	s := boardServer()
	mine, theirs := clientCtx("letmoe"), clientCtx("kungal")

	created, err := s.createBoard(mine, &createBoardInput{Body: dto.CreateBoardRequest{
		ActorID: 1, Slug: "guides", Name: "攻略", Format: model.BoardFormatQA,
	}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	board := created.Body.Data.Board
	if board.Site != "letmoe" || board.Format != model.BoardFormatQA || board.Stats.TopicsCount != 0 {
		t.Fatalf("created board: %+v", board)
	}
	_, err = s.createBoard(mine, &createBoardInput{Body: dto.CreateBoardRequest{ActorID: 1, Slug: "guides", Name: "again"}})
	wantStatus(t, err, http.StatusConflict)

	got, err := s.getBoardBySlug(mine, &boardSlugInput{Slug: "guides"})
	if err != nil || got.Body.Data.Board.ID != board.ID {
		t.Fatalf("by slug: %v", err)
	}
	_, err = s.getBoard(theirs, &boardIDInput{ID: board.ID})
	wantStatus(t, err, http.StatusNotFound)
	_, err = s.getBoardBySlug(theirs, &boardSlugInput{Slug: "guides"})
	wantStatus(t, err, http.StatusNotFound)
	name := "hijack"
	_, err = s.updateBoard(theirs, &updateBoardInput{ID: board.ID, Body: dto.UpdateBoardRequest{ActorID: 2, Name: &name}})
	wantStatus(t, err, http.StatusNotFound)
	_, err = s.deleteBoard(theirs, &deleteBoardInput{ID: board.ID, ActorID: 2})
	wantStatus(t, err, http.StatusNotFound)
	if list, err := s.listBoards(theirs, &struct{}{}); err != nil || len(list.Body.Data.Boards) != 0 {
		t.Fatalf("another site lists no boards: %v", err)
	}

	seedTL1(t, 100)
	_, err = s.openTopic(theirs, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: board.ID, Title: "t", Body: "x"}})
	wantStatus(t, err, http.StatusNotFound)
	_, err = s.listThreads(theirs, &listThreadsInput{BoardID: board.ID, Pinned: "any"})
	wantStatus(t, err, http.StatusNotFound)

	topic, err := s.openTopic(mine, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: board.ID, Title: "t", Body: "x"}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if v := topic.Body.Data.Thread; v.BoardID == nil || *v.BoardID != board.ID {
		t.Fatalf("a topic names its board: %+v", v)
	}
	theirBoard, err := s.createBoard(theirs, &createBoardInput{Body: dto.CreateBoardRequest{ActorID: 2, Slug: "guides", Name: "theirs"}})
	if err != nil {
		t.Fatalf("the same slug on another site: %v", err)
	}
	_, err = s.moveTopic(mine, &moveTopicInput{ID: topic.Body.Data.Thread.ID, Body: dto.MoveTopicRequest{ActorID: 9, BoardID: theirBoard.Body.Data.Board.ID}})
	wantStatus(t, err, http.StatusNotFound)
	_, err = s.pinTopic(theirs, &pinTopicInput{ID: topic.Body.Data.Thread.ID, Body: dto.PinTopicRequest{ActorID: 9, Scope: model.PinScopeSite}})
	wantStatus(t, err, http.StatusNotFound)
	_, err = s.deleteBoard(mine, &deleteBoardInput{ID: board.ID, ActorID: 1})
	wantStatus(t, err, http.StatusConflict)
}

func TestOpenTopicBoardReference(t *testing.T) {
	cleanTables(t)
	s := boardServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 100)
	mainID := testBoard(t, "letmoe", "main")

	legacy, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, AnchorID: "main", Title: "t", Body: "x"}})
	if err != nil {
		t.Fatalf("the deprecated slug anchor still opens: %v", err)
	}
	if v := legacy.Body.Data.Thread; v.AnchorID != model.BoardAnchorID(mainID) {
		t.Fatalf("a legacy anchor lands on the board id, got %q", v.AnchorID)
	}
	_, err = s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: mainID, AnchorID: "main", Title: "t", Body: "x"}})
	wantStatus(t, err, http.StatusUnprocessableEntity)
	_, err = s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, Title: "t", Body: "x"}})
	wantStatus(t, err, http.StatusUnprocessableEntity)
	_, err = s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, AnchorID: "nowhere", Title: "t", Body: "x"}})
	wantStatus(t, err, http.StatusNotFound)

	_, err = s.openFeedback(ctx, &openFeedbackInput{Body: dto.OpenFeedbackRequest{
		AuthorID: 100, AnchorKind: model.AnchorKindBoard, AnchorID: model.BoardAnchorID(mainID), Title: "t", Body: "x",
	}})
	wantStatus(t, err, http.StatusUnprocessableEntity)

	news, err := s.createBoard(ctx, &createBoardInput{Body: dto.CreateBoardRequest{
		ActorID: 1, Slug: "news", Name: "公告", Format: model.BoardFormatAnnouncement,
	}})
	if err != nil {
		t.Fatalf("create news: %v", err)
	}
	newsID := news.Body.Data.Board.ID
	_, err = s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: newsID, Title: "t", Body: "x"}})
	wantStatus(t, err, http.StatusForbidden)
	if _, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: newsID, AsModerator: true, Title: "t", Body: "x"}}); err != nil {
		t.Fatalf("a moderator posts an announcement: %v", err)
	}
}

func TestListThreadsByBoard(t *testing.T) {
	cleanTables(t)
	s := boardServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 100)
	parent, err := s.createBoard(ctx, &createBoardInput{Body: dto.CreateBoardRequest{ActorID: 1, Slug: "games", Name: "游戏"}})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	parentID := parent.Body.Data.Board.ID
	child, err := s.createBoard(ctx, &createBoardInput{Body: dto.CreateBoardRequest{ActorID: 1, ParentID: parentID, Slug: "rpg", Name: "RPG"}})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	childID := child.Body.Data.Board.ID
	open := func(boardID int64) int64 {
		t.Helper()
		out, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: boardID, Title: "t", Body: "x"}})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return out.Body.Data.Thread.ID
	}
	onParent, onChild := open(parentID), open(childID)
	if _, err := s.pinTopic(ctx, &pinTopicInput{ID: onChild, Body: dto.PinTopicRequest{ActorID: 9, Scope: model.PinScopeBoard}}); err != nil {
		t.Fatalf("pin: %v", err)
	}

	ids := func(in listThreadsInput) []int64 {
		t.Helper()
		if in.Pinned == "" {
			in.Pinned = "any"
		}
		out, err := s.listThreads(ctx, &in)
		if err != nil {
			t.Fatalf("list %+v: %v", in, err)
		}
		got := make([]int64, len(out.Body.Data.Threads))
		for i, th := range out.Body.Data.Threads {
			got[i] = th.ID
		}
		slices.Sort(got)
		return got
	}
	if got := ids(listThreadsInput{BoardID: parentID}); !slices.Equal(got, []int64{onParent}) {
		t.Fatalf("the parent alone: want [%d], got %v", onParent, got)
	}
	if got := ids(listThreadsInput{BoardID: parentID, Subboards: true}); !slices.Equal(got, []int64{onParent, onChild}) {
		t.Fatalf("the parent with its sub-board: want [%d %d], got %v", onParent, onChild, got)
	}
	pinned, err := s.listThreads(ctx, &listThreadsInput{BoardID: childID, Pinned: "only", Limit: 1})
	if err != nil {
		t.Fatalf("pinned: %v", err)
	}
	if len(pinned.Body.Data.Threads) != 1 || pinned.Body.Data.NextCursor != "" ||
		pinned.Body.Data.Threads[0].PinScope != model.PinScopeBoard || pinned.Body.Data.Threads[0].PinnedAt == nil {
		t.Fatalf("the pinned set is one page carrying its pin: %+v", pinned.Body.Data)
	}
	if got := ids(listThreadsInput{Pinned: "only"}); len(got) != 0 {
		t.Fatalf("a board pin is not a site pin, got %v", got)
	}
	if err := testDB.Exec(`UPDATE community_thread SET pinned_until = now() - interval '1 minute' WHERE id = ?`, onChild).Error; err != nil {
		t.Fatalf("lapse pin: %v", err)
	}
	lapsed, err := s.getThread(ctx, &threadPostsInput{ID: onChild})
	if err != nil {
		t.Fatalf("get lapsed: %v", err)
	}
	if v := lapsed.Body.Data.Thread; v.PinScope != model.PinScopeNone || v.PinnedAt != nil {
		t.Fatalf("a lapsed pin reads as unpinned: %+v", v)
	}

	for name, in := range map[string]listThreadsInput{
		"board and anchor together":   {BoardID: parentID, AnchorID: "x", Pinned: "any"},
		"a board listing of comments": {BoardID: parentID, Kind: model.ThreadKindComments, Pinned: "any"},
		"sub-boards without a board":  {Subboards: true, Pinned: "any"},
	} {
		_, err := s.listThreads(ctx, &in)
		if err == nil {
			t.Fatalf("%s must be refused", name)
		}
		wantStatus(t, err, http.StatusUnprocessableEntity)
	}
	_, err = s.listThreads(ctx, &listThreadsInput{Pinned: "only", Cursor: "MTox"})
	wantStatus(t, err, http.StatusBadRequest)
}

func TestModerationFacesReturnTheThread(t *testing.T) {
	cleanTables(t)
	s := boardServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 100)
	seedTL1(t, 200)
	qa, err := s.createBoard(ctx, &createBoardInput{Body: dto.CreateBoardRequest{ActorID: 1, Slug: "qa", Name: "问答", Format: model.BoardFormatQA}})
	if err != nil {
		t.Fatalf("qa board: %v", err)
	}
	topic, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: qa.Body.Data.Board.ID, Title: "how", Body: "?"}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	threadID := topic.Body.Data.Thread.ID
	reply, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "this way"}})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}

	answered, err := s.markAnswer(ctx, &markAnswerInput{ID: threadID, Body: dto.MarkAnswerRequest{ActorID: 100, PostID: reply.Body.Data.Post.ID}})
	if err != nil || answered.Body.Data.Thread.AnswerPostID == nil {
		t.Fatalf("mark answer: %v", err)
	}
	_, err = s.markAnswer(ctx, &markAnswerInput{ID: threadID, Body: dto.MarkAnswerRequest{ActorID: 200, PostID: 0}})
	wantStatus(t, err, http.StatusForbidden)

	closed, err := s.closeThread(ctx, &closeThreadInput{ID: threadID, Body: dto.CloseThreadRequest{ActorID: 9, Closed: true}})
	if err != nil || closed.Body.Data.Thread.Status != model.ThreadStatusClosed {
		t.Fatalf("close: %v", err)
	}
	_, err = s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "late"}})
	wantStatus(t, err, http.StatusConflict)

	_, err = s.pinTopic(ctx, &pinTopicInput{ID: threadID, Body: dto.PinTopicRequest{ActorID: 9, Scope: 5}})
	wantStatus(t, err, http.StatusUnprocessableEntity)
	_, err = s.moveTopic(ctx, &moveTopicInput{ID: 999999, Body: dto.MoveTopicRequest{ActorID: 9, BoardID: qa.Body.Data.Board.ID}})
	wantStatus(t, err, http.StatusNotFound)
}
