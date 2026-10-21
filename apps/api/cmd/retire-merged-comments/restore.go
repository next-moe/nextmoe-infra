package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	commodel "api/internal/platform/community/model"

	"gorm.io/gorm"
)

type savedPost struct {
	ID            int64 `json:"id"`
	PostNumber    int32 `json:"post_number"`
	ContentRating int16 `json:"content_rating"`
}

type threadState struct {
	Status            int16             `json:"status"`
	PostsCount        int64             `json:"posts_count"`
	HighestPostNumber int64             `json:"highest_post_number"`
	ParticipantsCount int64             `json:"participants_count"`
	LastPostedAt      *time.Time        `json:"last_posted_at"`
	Posts             []savedPost       `json:"posts"`
	ThreadUsers       []json.RawMessage `json:"thread_users"`
	Notifications     []int64           `json:"notifications"`
	AnchorUsers       []json.RawMessage `json:"anchor_users"`
}

// captureState reads a thread and everything a rehome rewrites, before it does.
func captureState(tx *gorm.DB, threadID int64) (threadState, error) {
	var raw string
	err := tx.Raw(`
		SELECT jsonb_build_object(
		  'status', t.status, 'posts_count', t.posts_count,
		  'highest_post_number', t.highest_post_number,
		  'participants_count', t.participants_count, 'last_posted_at', t.last_posted_at,
		  'posts', COALESCE((SELECT jsonb_agg(jsonb_build_object('id', p.id, 'post_number', p.post_number,
		             'content_rating', p.content_rating) ORDER BY p.id) FROM community_post p WHERE p.thread_id = t.id), '[]'),
		  'thread_users', COALESCE((SELECT jsonb_agg(to_jsonb(u)) FROM community_thread_user u WHERE u.thread_id = t.id), '[]'),
		  'notifications', COALESCE((SELECT jsonb_agg(n.id ORDER BY n.id) FROM community_notification n WHERE n.thread_id = t.id), '[]'),
		  'anchor_users', COALESCE((SELECT jsonb_agg(to_jsonb(a)) FROM community_anchor_user a
		      WHERE a.site = t.site AND a.anchor_kind = t.anchor_kind AND a.anchor_id = t.anchor_id), '[]'))::text
		FROM community_thread t WHERE t.id = ?`, threadID).Scan(&raw).Error
	if err != nil {
		return threadState{}, err
	}
	var s threadState
	if raw == "" {
		return s, fmt.Errorf("thread %d not found", threadID)
	}
	return s, json.Unmarshal([]byte(raw), &s)
}

func writeLog(tx *gorm.DB, r stranded, anchorKind, action int16, into *int64, offset int32, state threadState) error {
	old, _ := strconv.ParseInt(r.AnchorID, 10, 64)
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return tx.Create(&commodel.CommunityRehomeLog{
		Site: r.Site, AnchorKind: anchorKind, OldAnchorID: r.AnchorID, NewAnchorID: r.SurvivorAnchor,
		CatalogOldID: old, Action: action, ThreadID: r.ThreadID, IntoThreadID: into, PostOffset: offset, State: body,
	}).Error
}

// restoreUndone puts back what earlier runs did to conversations on works whose
// catalog merge has since been undone: the work is live again under its own
// id and its redirect is revoked, so its page exists and its comments belong
// on it. A conversation is left on the survivor (and reported) only when a new
// live thread already holds the old anchor.
func restoreUndone(cat, com *gorm.DB, site string, anchorKind int16, apply bool, out io.Writer) (int, error) {
	var logs []commodel.CommunityRehomeLog
	if err := com.Where("site = ? AND anchor_kind = ? AND reverted_at IS NULL", site, anchorKind).
		Order("id").Find(&logs).Error; err != nil {
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(logs))
	for _, l := range logs {
		ids = append(ids, l.CatalogOldID)
	}
	var live []int64
	if err := cat.Raw(`SELECT w.id FROM catalog_work w
		WHERE w.id IN ? AND w.deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM catalog_redirect r
		                  WHERE r.entity_type = ? AND r.old_id = w.id AND r.revoked_at IS NULL)`,
		ids, entityTypeWork).Scan(&live).Error; err != nil {
		return 0, err
	}
	back := map[int64]bool{}
	for _, id := range live {
		back[id] = true
	}
	done := 0
	for _, l := range logs {
		if !back[l.CatalogOldID] {
			continue
		}
		if !apply {
			fmt.Fprintf(out, "%s: would put thread %d back on anchor %s (merge of work %d undone)\n", site, l.ThreadID, l.OldAnchorID, l.CatalogOldID)
			done++
			continue
		}
		var outcome string
		err := com.Transaction(func(tx *gorm.DB) error {
			var err error
			outcome, err = reverse(tx, l)
			if err != nil {
				return err
			}
			if outcome != "" {
				return nil
			}
			return tx.Model(&commodel.CommunityRehomeLog{}).Where("id = ?", l.ID).Update("reverted_at", time.Now()).Error
		})
		if err != nil {
			return done, fmt.Errorf("log %d: %w", l.ID, err)
		}
		if outcome != "" {
			fmt.Fprintf(out, "%s: thread %d stays on anchor %s: %s\n", site, l.ThreadID, l.NewAnchorID, outcome)
			continue
		}
		fmt.Fprintf(out, "%s: put thread %d back on anchor %s\n", site, l.ThreadID, l.OldAnchorID)
		done++
	}
	return done, nil
}

func liveCommentsThreadOn(tx *gorm.DB, l commodel.CommunityRehomeLog, except int64) (bool, error) {
	var n int64
	err := tx.Raw(`SELECT count(*) FROM community_thread
		WHERE site = ? AND anchor_kind = ? AND anchor_id = ? AND kind = ? AND status <> ? AND id <> ?`,
		l.Site, l.AnchorKind, l.OldAnchorID, threadKindComments, threadStatusDeleted, except).Scan(&n).Error
	return n > 0, err
}

// reverse returns a non-empty reason when it leaves the conversation where it is.
func reverse(tx *gorm.DB, l commodel.CommunityRehomeLog) (string, error) {
	var st threadState
	if err := json.Unmarshal(l.State, &st); err != nil {
		return "", err
	}
	occupied, err := liveCommentsThreadOn(tx, l, l.ThreadID)
	if err != nil {
		return "", err
	}
	if occupied {
		return "a new conversation already lives on the restored work's page", nil
	}
	switch l.Action {
	case commodel.RehomeRetired:
		return "", tx.Exec(`UPDATE community_thread SET status = ?, updated_at = now() WHERE id = ? AND status = ?`,
			st.Status, l.ThreadID, threadStatusDeleted).Error
	case commodel.RehomeMoved:
		var later int64
		if err := tx.Raw(`SELECT count(*) FROM community_post WHERE thread_id = ? AND id NOT IN ?`,
			l.ThreadID, postIDs(st)).Scan(&later).Error; err != nil {
			return "", err
		}
		if later > 0 {
			return fmt.Sprintf("%d post(s) were written on the survivor's page after the move", later), nil
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE community_thread SET anchor_id = ?, updated_at = now() WHERE id = ?`, []any{l.OldAnchorID, l.ThreadID}},
			{`UPDATE community_notification SET anchor_id = ?, updated_at = now() WHERE thread_id = ?`, []any{l.OldAnchorID, l.ThreadID}},
		} {
			if err := tx.Exec(q.sql, q.args...).Error; err != nil {
				return "", err
			}
		}
	case commodel.RehomeFolded:
		if l.IntoThreadID == nil {
			return "", fmt.Errorf("folded log %d names no thread", l.ID)
		}
		into := *l.IntoThreadID
		for _, p := range st.Posts {
			if err := tx.Exec(`UPDATE community_post SET thread_id = ?, post_number = ?, content_rating = ?
				WHERE id = ? AND thread_id = ?`, l.ThreadID, p.PostNumber, p.ContentRating, p.ID, into).Error; err != nil {
				return "", err
			}
		}
		if err := tx.Exec(`UPDATE community_thread
			SET status = ?, merged_into_id = NULL, posts_count = ?, highest_post_number = ?,
			    participants_count = ?, last_posted_at = ?, updated_at = now()
			WHERE id = ?`, st.Status, st.PostsCount, st.HighestPostNumber, st.ParticipantsCount, st.LastPostedAt, l.ThreadID).Error; err != nil {
			return "", err
		}
		if err := tx.Exec(`UPDATE community_thread t
			SET posts_count = (SELECT count(*) FROM community_post p WHERE p.thread_id = t.id),
			    participants_count = (SELECT count(DISTINCT p.author_id) FROM community_post p WHERE p.thread_id = t.id),
			    updated_at = now()
			WHERE t.id = ?`, into).Error; err != nil {
			return "", err
		}
		if len(st.Notifications) > 0 {
			if err := tx.Exec(`UPDATE community_notification
				SET thread_id = ?, anchor_id = ?, post_number = post_number - ?,
				    first_post_number = first_post_number - ?, updated_at = now()
				WHERE id IN ? AND thread_id = ?`,
				l.ThreadID, l.OldAnchorID, l.PostOffset, l.PostOffset, st.Notifications, into).Error; err != nil {
				return "", err
			}
		}
		for _, u := range st.ThreadUsers {
			if err := tx.Exec(`INSERT INTO community_thread_user SELECT * FROM jsonb_populate_record(NULL::community_thread_user, ?::jsonb)
				ON CONFLICT DO NOTHING`, string(u)).Error; err != nil {
				return "", err
			}
		}
	default:
		return "", fmt.Errorf("log %d: unknown action %d", l.ID, l.Action)
	}
	for _, a := range st.AnchorUsers {
		if err := tx.Exec(`INSERT INTO community_anchor_user SELECT * FROM jsonb_populate_record(NULL::community_anchor_user, ?::jsonb)
			ON CONFLICT DO NOTHING`, string(a)).Error; err != nil {
			return "", err
		}
	}
	return "", nil
}

func postIDs(st threadState) []int64 {
	ids := []int64{0}
	for _, p := range st.Posts {
		ids = append(ids, p.ID)
	}
	return ids
}
