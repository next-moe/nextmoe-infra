package main

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type legacyRoom struct {
	Source  string
	ID      int64
	Created time.Time
	Users   []int64
}

type legacyReaction struct {
	UserID  int64
	Emoji   string
	Created time.Time
}

type legacyMessage struct {
	Source    string
	ID        int64
	RoomID    int64
	SenderID  int64
	Content   string
	Deleted   bool
	EditedAt  *time.Time
	ReplyToID *int64
	Created   time.Time
	Reactions []legacyReaction
	// Users the old site recorded as having read this far.
	ReadBy []int64
}

type legacySource struct {
	Rooms    []legacyRoom
	Messages []legacyMessage
	// Every message counts as read by both people: the old site kept no read
	// state at all.
	AllRead bool
}

func readKungal(ctx context.Context, db *gorm.DB) (*legacySource, error) {
	src := &legacySource{}
	var rooms []struct {
		ID      int64
		Created time.Time
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, created FROM chat_room ORDER BY id`).Scan(&rooms).Error; err != nil {
		return nil, err
	}
	var parts []struct {
		ChatRoomID int64
		UserID     int64
	}
	if err := db.WithContext(ctx).Raw(`SELECT chat_room_id, user_id FROM chat_room_participant ORDER BY chat_room_id, user_id`).Scan(&parts).Error; err != nil {
		return nil, err
	}
	users := map[int64][]int64{}
	for _, p := range parts {
		users[p.ChatRoomID] = append(users[p.ChatRoomID], p.UserID)
	}
	for _, r := range rooms {
		src.Rooms = append(src.Rooms, legacyRoom{Source: "kungal", ID: r.ID, Created: r.Created, Users: users[r.ID]})
	}
	var msgs []struct {
		ID         int64
		ChatRoomID int64
		SenderID   int64
		Content    string
		IsRecall   bool
		EditTime   *time.Time
		Created    time.Time
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT id, chat_room_id, sender_id, coalesce(content, '') AS content, is_recall, edit_time, created
		  FROM chat_message ORDER BY id`).Scan(&msgs).Error; err != nil {
		return nil, err
	}
	var reads []struct {
		ChatRoomID int64
		UserID     int64
		MaxID      int64
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT m.chat_room_id, rb.user_id, max(m.id) AS max_id
		  FROM chat_message_read_by rb JOIN chat_message m ON m.id = rb.chat_message_id
		 GROUP BY m.chat_room_id, rb.user_id`).Scan(&reads).Error; err != nil {
		return nil, err
	}
	readAt := map[int64][]int64{}
	for _, r := range reads {
		readAt[r.MaxID] = append(readAt[r.MaxID], r.UserID)
	}
	for _, m := range msgs {
		src.Messages = append(src.Messages, legacyMessage{
			Source: "kungal", ID: m.ID, RoomID: m.ChatRoomID, SenderID: m.SenderID, Content: m.Content,
			Deleted: m.IsRecall, EditedAt: m.EditTime, Created: m.Created, ReadBy: readAt[m.ID],
		})
	}
	return src, nil
}

func readMoyu(ctx context.Context, db *gorm.DB) (*legacySource, error) {
	src := &legacySource{AllRead: true}
	var rooms []struct {
		ID      int64
		Created time.Time
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, created FROM chat_room WHERE type::text = 'PRIVATE' ORDER BY id`).Scan(&rooms).Error; err != nil {
		return nil, err
	}
	var members []struct {
		ChatRoomID int64
		UserID     int64
	}
	if err := db.WithContext(ctx).Raw(`SELECT chat_room_id, user_id FROM chat_member ORDER BY chat_room_id, user_id`).Scan(&members).Error; err != nil {
		return nil, err
	}
	users := map[int64][]int64{}
	for _, m := range members {
		users[m.ChatRoomID] = append(users[m.ChatRoomID], m.UserID)
	}
	private := map[int64]bool{}
	for _, r := range rooms {
		private[r.ID] = true
		src.Rooms = append(src.Rooms, legacyRoom{Source: "moyu", ID: r.ID, Created: r.Created, Users: users[r.ID]})
	}
	var msgs []struct {
		ID         int64
		ChatRoomID int64
		SenderID   int64
		Content    string
		Status     string
		DeletedAt  *time.Time
		ReplyToID  *int64
		Created    time.Time
		Updated    time.Time
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT id, chat_room_id, sender_id, coalesce(content, '') AS content, status::text AS status,
		       deleted_at, reply_to_id, created, updated
		  FROM chat_message ORDER BY id`).Scan(&msgs).Error; err != nil {
		return nil, err
	}
	var reactions []struct {
		ChatMessageID int64
		UserID        int64
		Emoji         string
		Created       time.Time
	}
	if err := db.WithContext(ctx).Raw(`SELECT chat_message_id, user_id, emoji, created FROM chat_message_reaction ORDER BY id`).Scan(&reactions).Error; err != nil {
		return nil, err
	}
	byMessage := map[int64][]legacyReaction{}
	for _, r := range reactions {
		byMessage[r.ChatMessageID] = append(byMessage[r.ChatMessageID], legacyReaction{UserID: r.UserID, Emoji: r.Emoji, Created: r.Created})
	}
	for _, m := range msgs {
		if !private[m.ChatRoomID] {
			continue
		}
		lm := legacyMessage{
			Source: "moyu", ID: m.ID, RoomID: m.ChatRoomID, SenderID: m.SenderID, Content: m.Content,
			Deleted: m.DeletedAt != nil || m.Status == "DELETED", ReplyToID: m.ReplyToID, Created: m.Created,
			Reactions: byMessage[m.ID],
		}
		if m.Status == "EDITED" {
			at := m.Updated
			lm.EditedAt = &at
		}
		src.Messages = append(src.Messages, lm)
	}
	return src, nil
}
