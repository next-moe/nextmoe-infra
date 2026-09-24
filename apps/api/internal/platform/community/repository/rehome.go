package repository

import (
	"fmt"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RehomeOutcome struct {
	IntoThreadID int64
	MovedPosts   int64
}

// RehomeCommentsThreadTx moves a comments thread's conversation to another
// anchor of the same kind. With no live comments thread there, the thread itself
// takes the new anchor. Otherwise its posts are appended to that thread,
// numbered after its last post, and the emptied thread is retired as merged into
// it; an anchor holds one live comments thread (uq_community_thread_anchor_*).
func RehomeCommentsThreadTx(tx *gorm.DB, threadID int64, toAnchorID string) (RehomeOutcome, error) {
	from, err := LockThreadTx(tx, threadID)
	if err != nil {
		return RehomeOutcome{}, err
	}
	if from == nil || from.Kind != model.ThreadKindComments {
		return RehomeOutcome{}, fmt.Errorf("thread %d is not a comments thread", threadID)
	}
	if from.AnchorID == toAnchorID {
		return RehomeOutcome{IntoThreadID: from.ID}, nil
	}
	into, err := getLiveCommentsThread(tx.Clauses(clause.Locking{Strength: LockUpdate}), from.Site, from.AnchorKind, toAnchorID)
	if err != nil {
		return RehomeOutcome{}, err
	}
	if err := rehomeAnchorSubscriptionsTx(tx, from, toAnchorID); err != nil {
		return RehomeOutcome{}, err
	}
	if into == nil {
		if err := tx.Exec(`UPDATE community_thread SET anchor_id = ?, updated_at = now() WHERE id = ?`,
			toAnchorID, from.ID).Error; err != nil {
			return RehomeOutcome{}, err
		}
		err := tx.Exec(`UPDATE community_notification SET anchor_id = ?, updated_at = now() WHERE thread_id = ?`,
			toAnchorID, from.ID).Error
		return RehomeOutcome{IntoThreadID: from.ID}, err
	}

	base := into.HighestPostNumber
	var joining int64
	if err := tx.Raw(`
		SELECT count(DISTINCT p.author_id) FROM community_post p
		WHERE p.thread_id = ?
		  AND NOT EXISTS (SELECT 1 FROM community_post q WHERE q.thread_id = ? AND q.author_id = p.author_id)`,
		from.ID, into.ID).Scan(&joining).Error; err != nil {
		return RehomeOutcome{}, err
	}
	moved := tx.Exec(`
		UPDATE community_post
		   SET thread_id = ?, post_number = post_number + ?,
		       content_rating = GREATEST(content_rating, ?)
		 WHERE thread_id = ?`, into.ID, base, into.ContentRating, from.ID)
	if moved.Error != nil {
		return RehomeOutcome{}, moved.Error
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`UPDATE community_thread
		     SET posts_count = posts_count + ?, highest_post_number = ?,
		         participants_count = participants_count + ?,
		         last_posted_at = GREATEST(last_posted_at, ?::timestamptz), updated_at = now()
		   WHERE id = ?`,
			[]any{from.PostsCount, base + from.HighestPostNumber, joining, from.LastPostedAt, into.ID}},
		{`INSERT INTO community_thread_user (thread_id, user_id, site, last_read_post_number, notification_level, last_visited_at)
		  SELECT ?, user_id, site,
		         CASE WHEN last_read_post_number > 0 THEN last_read_post_number + ? ELSE 0 END,
		         notification_level, last_visited_at
		    FROM community_thread_user WHERE thread_id = ?
		  ON CONFLICT (thread_id, user_id) DO NOTHING`,
			[]any{into.ID, base, from.ID}},
		{`DELETE FROM community_thread_user WHERE thread_id = ?`, []any{from.ID}},
		{`UPDATE community_notification
		     SET thread_id = ?, anchor_id = ?, post_number = post_number + ?,
		         first_post_number = first_post_number + ?, updated_at = now()
		   WHERE thread_id = ?`,
			[]any{into.ID, toAnchorID, base, base, from.ID}},
		{`UPDATE community_thread
		     SET status = ?, merged_into_id = ?, posts_count = 0, highest_post_number = 0, updated_at = now()
		   WHERE id = ?`,
			[]any{model.ThreadStatusDeleted, into.ID, from.ID}},
	}
	for _, s := range steps {
		if err := tx.Exec(s.sql, s.args...).Error; err != nil {
			return RehomeOutcome{}, err
		}
	}
	return RehomeOutcome{IntoThreadID: into.ID, MovedPosts: moved.RowsAffected}, nil
}

func rehomeAnchorSubscriptionsTx(tx *gorm.DB, from *model.CommunityThread, toAnchorID string) error {
	if err := tx.Exec(`
		INSERT INTO community_anchor_user (site, user_id, anchor_kind, anchor_id, notification_level, created_at, updated_at)
		SELECT site, user_id, anchor_kind, ?, notification_level, created_at, now()
		  FROM community_anchor_user
		 WHERE site = ? AND anchor_kind = ? AND anchor_id = ?
		ON CONFLICT (site, user_id, anchor_kind, anchor_id) DO NOTHING`,
		toAnchorID, from.Site, from.AnchorKind, from.AnchorID).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM community_anchor_user WHERE site = ? AND anchor_kind = ? AND anchor_id = ?`,
		from.Site, from.AnchorKind, from.AnchorID).Error
}
