package handler

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/service"

	"gorm.io/datatypes"
)

func purgeFixture(t *testing.T, s *Server, ctx context.Context) (posts []int64, other int64) {
	t.Helper()
	seedTL1(t, 500)
	seedTL1(t, 600)
	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	posts = replyN(t, s, ctx, th, 500, 3)
	other = replyN(t, s, ctx, th, 600, 1)[0]
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, posts[1]).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: posts[2], AuthorID: 500}); err != nil {
		t.Fatalf("self-delete: %v", err)
	}
	addReaction(t, posts[0], 500)
	addReaction(t, other, 500)
	addReaction(t, posts[0], 700)

	es := service.NewEngagementService(testDB)
	for _, site := range []string{"letmoe", "kungal"} {
		if _, err := es.SetAnchorLevel(ctx, site, 500, model.AnchorKindSiteGame, "g1", model.NotificationLevelWatching); err != nil {
			t.Fatalf("watch %s: %v", site, err)
		}
	}
	actor500 := int64(500)
	for i, n := range []model.CommunityNotification{
		{Site: "letmoe", UserID: 500, ActorID: &actor500},
		{Site: "kungal", UserID: 500, ActorID: &actor500},
		{Site: "letmoe", UserID: 600, ActorID: &actor500},
	} {
		n.Kind, n.ThreadID, n.AnchorKind, n.AnchorID = model.NotificationKindPosted, th, model.AnchorKindSiteGame, "g1"
		n.ActorCount, n.ItemCount, n.Seq = 1, 1, int64(100+i)
		if err := testDB.Create(&n).Error; err != nil {
			t.Fatalf("insert notification: %v", err)
		}
	}
	target500 := int64(500)
	for _, ev := range []model.CommunityEvent{
		{ActorID: 500},
		{ActorID: 600, TargetUserID: &target500, MentionUserIDs: datatypes.JSON(`[500, 700]`)},
	} {
		ev.Site, ev.Kind, ev.ThreadID, ev.AttemptAfter = "letmoe", model.EventKindPostCreated, th, time.Now().Add(time.Hour)
		if err := testDB.Create(&ev).Error; err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	return posts, other
}

// A notification's updated_at is left out: clearing and restoring its actor
// both stamp it.
func purgeableRows(t *testing.T) []string {
	t.Helper()
	var rows []string
	for _, q := range []string{
		`SELECT to_jsonb(x)::text FROM community_post x`,
		`SELECT to_jsonb(x)::text FROM community_reaction x`,
		`SELECT to_jsonb(x)::text FROM community_thread_user x`,
		`SELECT to_jsonb(x)::text FROM community_anchor_user x`,
		`SELECT (to_jsonb(x) - 'updated_at')::text FROM community_notification x`,
		`SELECT to_jsonb(x)::text FROM community_event x`,
	} {
		var part []string
		if err := testDB.Raw(q).Scan(&part).Error; err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		rows = append(rows, part...)
	}
	slices.Sort(rows)
	return rows
}

func TestAuthorRestoreUndoesThePurge(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	purgeFixture(t, s, ctx)
	before := purgeableRows(t)

	if _, err := s.purgeAuthor(ctx, &authorPurgeInput{ID: 500}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if slices.Equal(purgeableRows(t), before) {
		t.Fatal("the purge changed nothing, so the restore below proves nothing")
	}
	_, err := s.restoreAuthor(clientCtx("kungal"), &authorPurgeInput{ID: 500})
	wantStatus(t, err, http.StatusNotFound)

	res, err := s.restoreAuthor(ctx, &authorPurgeInput{ID: 500})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	got := res.Body.Data
	if got.PostsRestored != 3 || got.ReactionsRestored != 2 || got.ReadStatesRestored != 1 ||
		got.AnchorSubscriptionsRestored != 1 || got.NotificationsRestored != 1 {
		t.Fatalf("restore counts: want {3,2,1,1,1}, got %+v", got)
	}
	if after := purgeableRows(t); !slices.Equal(after, before) {
		t.Fatalf("restore did not give back what the purge took\n  before: %v\n  after:  %v", before, after)
	}

	_, err = s.restoreAuthor(ctx, &authorPurgeInput{ID: 500})
	wantStatus(t, err, http.StatusNotFound)
}

func TestAuthorRestoreKeepsWhatChangedSince(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	posts, _ := purgeFixture(t, s, ctx)
	if _, err := s.purgeAuthor(ctx, &authorPurgeInput{ID: 500}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusVisible, posts[1]).Error; err != nil {
		t.Fatalf("approve: %v", err)
	}
	addReaction(t, posts[0], 500)

	res, err := s.restoreAuthor(ctx, &authorPurgeInput{ID: 500})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if res.Body.Data.PostsRestored != 2 || res.Body.Data.ReactionsRestored != 1 {
		t.Fatalf("want 2 posts and 1 reaction restored, got %+v", res.Body.Data)
	}
	if p := getPostRow(t, posts[1]); p.Status != model.PostStatusVisible || p.ContentRaw != "" {
		t.Fatalf("a post changed since the purge must be left alone, got status=%d raw=%q", p.Status, p.ContentRaw)
	}
	if p := getPostRow(t, posts[0]); p.Status != model.PostStatusVisible || p.ContentRaw == "" {
		t.Fatalf("an untouched post must come back, got status=%d raw=%q", p.Status, p.ContentRaw)
	}
}

func TestPurgeArchivePrune(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	purgeFixture(t, s, ctx)
	if _, err := s.purgeAuthor(ctx, &authorPurgeInput{ID: 500}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := s.purgeAuthor(clientCtx("kungal"), &authorPurgeInput{ID: 500}); err != nil {
		t.Fatalf("purge kungal: %v", err)
	}
	expired := time.Now().Add(-service.PurgeArchiveRetain - time.Hour)
	if err := testDB.Exec("UPDATE community_purge_archive SET created_at = ? WHERE site = 'letmoe'", expired).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	pruned, err := s.posts.PrunePurgeArchive(context.Background())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	var left int64
	if err := testDB.Raw("SELECT count(*) FROM community_purge_archive WHERE site = 'kungal'").Scan(&left).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if pruned == 0 || left == 0 {
		t.Fatalf("want the expired letmoe rows pruned and the kungal rows kept, pruned=%d kept=%d", pruned, left)
	}
	_, err = s.restoreAuthor(ctx, &authorPurgeInput{ID: 500})
	wantStatus(t, err, http.StatusNotFound)
}

func TestAuthorRestoreStaysOnItsSite(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	purgeFixture(t, s, ctx)
	for _, site := range []string{"letmoe", "kungal"} {
		if _, err := s.purgeAuthor(clientCtx(site), &authorPurgeInput{ID: 500}); err != nil {
			t.Fatalf("purge %s: %v", site, err)
		}
	}
	if _, err := s.restoreAuthor(ctx, &authorPurgeInput{ID: 500}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := countAnchorSubs(t, "letmoe", 500); n != 1 {
		t.Fatalf("letmoe's anchor row must come back, got %d", n)
	}
	if n := countAnchorSubs(t, "kungal", 500); n != 0 {
		t.Fatalf("kungal's purge is kungal's to undo, but its anchor row came back")
	}
}
