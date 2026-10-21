package main

import (
	"io"
	"testing"
	"time"

	catmodel "api/internal/platform/catalog/model"
	commodel "api/internal/platform/community/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnUndoneMergeGetsItsConversationBack(t *testing.T) {
	if testDB == nil {
		t.Skip("no test DB")
	}
	for _, tbl := range []string{"community_post", "community_thread", "community_thread_user", "community_anchor_user",
		"community_notification", "community_rehome_log", "catalog_redirect", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	work := func(id int64, deleted bool) {
		require.NoError(t, testDB.Create(&catmodel.CatalogWork{ID: id, MediumID: 1, OLang: "ja", DisplayName: "w",
			Extra: []byte(`{}`), FieldProvenance: []byte(`{}`)}).Error)
		if deleted {
			require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, id).Error)
		}
	}
	redirect := func(old, current int64) {
		now := time.Now()
		require.NoError(t, testDB.Create(&catmodel.CatalogRedirect{
			EntityType: entityTypeWork, OldID: old, CurrentID: current, MergedAt: &now}).Error)
	}
	thread := func(kind int16, anchor string, posts int) int64 {
		th := commodel.CommunityThread{Site: testSite, Kind: threadKindComments, AnchorKind: kind, AnchorID: anchor, CreatedBy: 1}
		require.NoError(t, testDB.Create(&th).Error)
		for i := 1; i <= posts; i++ {
			require.NoError(t, testDB.Create(&commodel.CommunityPost{ThreadID: th.ID, PostNumber: int32(i), AuthorID: int64(i),
				ContentRaw: "hi", ContentHTML: "<p>hi</p>", SanitizerVersion: 1}).Error)
		}
		require.NoError(t, testDB.Exec(`UPDATE community_thread SET posts_count = ?, highest_post_number = ?, participants_count = ? WHERE id = ?`,
			posts, posts, posts, th.ID).Error)
		return th.ID
	}
	postsOn := func(threadID int64) []int32 {
		var nums []int32
		require.NoError(t, testDB.Raw(`SELECT post_number FROM community_post WHERE thread_id = ? ORDER BY post_number`, threadID).Scan(&nums).Error)
		return nums
	}

	work(930001, true)
	work(930002, false)
	redirect(930001, 930002)
	moved := thread(3, "930001", 2)

	work(930011, true)
	work(930012, false)
	redirect(930011, 930012)
	folded := thread(3, "930011", 3)
	survivor := thread(3, "930012", 2)
	require.NoError(t, testDB.Create(&commodel.CommunityThreadUser{ThreadID: folded, UserID: 7, LastReadPostNumber: 3, NotificationLevel: 2}).Error)
	require.NoError(t, testDB.Create(&commodel.CommunityAnchorUser{Site: testSite, UserID: 7, AnchorKind: 3, AnchorID: "930011", NotificationLevel: 2}).Error)
	pn := int32(2)
	require.NoError(t, testDB.Create(&commodel.CommunityNotification{Site: testSite, UserID: 7, Kind: 1, ThreadID: folded,
		AnchorKind: 3, AnchorID: "930011", PostNumber: &pn, FirstPostNumber: &pn, ActorCount: 1, ItemCount: 1}).Error)

	work(930021, true)
	work(930022, false)
	redirect(930021, 930022)
	retired := thread(1, "930021", 1)

	_, _, err := run(testDB, testDB, []string{testSite}, 3, 0, true, io.Discard)
	require.NoError(t, err)
	_, _, err = run(testDB, testDB, []string{testSite}, 1, 0, true, io.Discard)
	require.NoError(t, err)
	assertStatus(t, map[int64]int16{folded: threadStatusDeleted, retired: threadStatusDeleted})
	assert.Equal(t, []int32{1, 2, 3, 4, 5}, postsOn(survivor), "the folded conversation joined the survivor's")

	require.NoError(t, testDB.Create(&commodel.CommunityPost{ThreadID: survivor, PostNumber: 6, AuthorID: 9,
		ContentRaw: "after", ContentHTML: "<p>after</p>", SanitizerVersion: 1}).Error)

	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = NULL WHERE id IN (930001, 930011, 930021)`).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_redirect SET revoked_at = now() WHERE old_id IN (930001, 930011, 930021)`).Error)

	_, _, err = run(testDB, testDB, []string{testSite}, 3, 0, true, io.Discard)
	require.NoError(t, err)
	_, _, err = run(testDB, testDB, []string{testSite}, 1, 0, true, io.Discard)
	require.NoError(t, err)

	var anchor string
	require.NoError(t, testDB.Raw(`SELECT anchor_id FROM community_thread WHERE id = ?`, moved).Scan(&anchor).Error)
	assert.Equal(t, "930001", anchor, "a moved conversation goes back to its own page")
	assertStatus(t, map[int64]int16{folded: 0, retired: 0, survivor: 0})
	assert.Equal(t, []int32{1, 2, 3}, postsOn(folded), "folded posts come back with their own numbers")
	assert.Equal(t, []int32{1, 2, 6}, postsOn(survivor), "the survivor keeps its own posts and what was written there later")

	var subs, reads int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM community_anchor_user WHERE anchor_id = '930011' AND user_id = 7`).Scan(&subs).Error)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM community_thread_user WHERE thread_id = ? AND user_id = 7`, folded).Scan(&reads).Error)
	assert.EqualValues(t, 1, subs, "the subscription to the restored page is back")
	assert.EqualValues(t, 1, reads, "read state is back")
	var note struct {
		ThreadID   int64
		AnchorID   string
		PostNumber int32
	}
	require.NoError(t, testDB.Raw(`SELECT thread_id, anchor_id, post_number FROM community_notification WHERE user_id = 7`).Scan(&note).Error)
	assert.Equal(t, folded, note.ThreadID)
	assert.Equal(t, "930011", note.AnchorID)
	assert.EqualValues(t, 2, note.PostNumber)

	var open int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM community_rehome_log WHERE reverted_at IS NULL`).Scan(&open).Error)
	assert.Zero(t, open, "every logged action was reversed")
}
