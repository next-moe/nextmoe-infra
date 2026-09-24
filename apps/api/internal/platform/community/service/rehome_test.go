package service

import (
	"context"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func commentOn(t *testing.T, ps *PostService, anchorID string, author int64, body string) *model.CommunityPost {
	t.Helper()
	_, post, err := ps.Comment(context.Background(), CommentParams{
		Site: "kungal", AnchorKind: model.AnchorKindSiteGame, AnchorID: anchorID, AuthorID: author, BodyRaw: body,
	})
	if err != nil {
		t.Fatalf("comment on %s: %v", anchorID, err)
	}
	return post
}

func rehome(t *testing.T, threadID int64, to string) repository.RehomeOutcome {
	t.Helper()
	var out repository.RehomeOutcome
	err := testDB.Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = repository.RehomeCommentsThreadTx(tx, threadID, to)
		return err
	})
	if err != nil {
		t.Fatalf("rehome %d -> %s: %v", threadID, to, err)
	}
	return out
}

func TestRehome_NoThreadAtTheSurvivorMovesTheThreadWhole(t *testing.T) {
	cleanTables(t)
	seedTrust(t, 10, 1, 0)
	ps := NewPostService(testDB, NoopSink{})
	post := commentOn(t, ps, "900", 10, "on the merged-away work")
	if err := testDB.Create(&model.CommunityAnchorUser{Site: "kungal", UserID: 11,
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "900", NotificationLevel: model.NotificationLevelWatching}).Error; err != nil {
		t.Fatalf("seed anchor subscription: %v", err)
	}

	out := rehome(t, post.ThreadID, "100")

	th := getThread(t, post.ThreadID)
	if out.IntoThreadID != th.ID || th.AnchorID != "100" || th.Status != model.ThreadStatusOpen {
		t.Fatalf("the thread must take the survivor's anchor and stay open: out=%+v thread=%+v", out, th)
	}
	if got := getPost(t, post.ID); got.ThreadID != th.ID || got.PostNumber != 1 {
		t.Fatalf("the post must not move: %+v", got)
	}
	var subs []string
	testDB.Model(&model.CommunityAnchorUser{}).Where("user_id = 11").Pluck("anchor_id", &subs)
	if len(subs) != 1 || subs[0] != "100" {
		t.Fatalf("a watcher of the merged-away page watches the survivor, got %v", subs)
	}
}

func TestRehome_IntoALiveThreadAppendsAfterItsLastPost(t *testing.T) {
	cleanTables(t)
	for _, u := range []int64{10, 20, 30} {
		seedTrust(t, u, 1, 0)
	}
	ps := NewPostService(testDB, NoopSink{})
	kept1 := commentOn(t, ps, "100", 10, "survivor 1")
	commentOn(t, ps, "100", 20, "survivor 2")
	moved1 := commentOn(t, ps, "900", 30, "stranded 1")
	moved2 := commentOn(t, ps, "900", 10, "stranded 2")
	survivor, stranded := getThread(t, kept1.ThreadID), getThread(t, moved1.ThreadID)

	out := rehome(t, stranded.ID, "100")
	if out.IntoThreadID != survivor.ID || out.MovedPosts != 2 {
		t.Fatalf("want 2 posts into thread %d, got %+v", survivor.ID, out)
	}

	for want, id := range map[int32]int64{3: moved1.ID, 4: moved2.ID} {
		if got := getPost(t, id); got.ThreadID != survivor.ID || got.PostNumber != want {
			t.Fatalf("post %d: want thread %d number %d, got thread %d number %d",
				id, survivor.ID, want, got.ThreadID, got.PostNumber)
		}
	}
	after := getThread(t, survivor.ID)
	if after.PostsCount != 4 || after.HighestPostNumber != 4 {
		t.Fatalf("survivor counters: want 4/4, got posts=%d highest=%d", after.PostsCount, after.HighestPostNumber)
	}
	if after.ParticipantsCount != survivor.ParticipantsCount+1 {
		t.Fatalf("only author 30 is new to the survivor: participants %d -> %d",
			survivor.ParticipantsCount, after.ParticipantsCount)
	}
	gone := getThread(t, stranded.ID)
	if gone.Status != model.ThreadStatusDeleted || gone.MergedIntoID == nil || *gone.MergedIntoID != survivor.ID {
		t.Fatalf("the emptied thread must be retired as merged into the survivor: %+v", gone)
	}

	var last int32
	testDB.Model(&model.CommunityThreadUser{}).Where("thread_id = ? AND user_id = 30", survivor.ID).
		Pluck("last_read_post_number", &last)
	if last != 3 {
		t.Fatalf("author 30 had read the stranded thread to its post 1, now number 3; got %d", last)
	}

	// The survivor keeps taking comments after the fold, numbered after it.
	next := commentOn(t, ps, "100", 20, "after the fold")
	if next.ThreadID != survivor.ID || next.PostNumber != 5 {
		t.Fatalf("a new comment must land after the moved posts: %+v", next)
	}
}
