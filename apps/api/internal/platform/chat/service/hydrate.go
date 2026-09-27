package service

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func decodeEntities(j datatypes.JSON) []content.Entity {
	if len(j) == 0 {
		return nil
	}
	var es []content.Entity
	if err := json.Unmarshal(j, &es); err != nil || len(es) == 0 {
		return nil
	}
	return es
}

func decodeJSON[T any](j datatypes.JSON) *T {
	if len(j) == 0 || string(j) == "null" {
		return nil
	}
	var v T
	if err := json.Unmarshal(j, &v); err != nil {
		return nil
	}
	return &v
}

type storedQuote struct {
	Text     string           `json:"text"`
	Entities []content.Entity `json:"entities"`
	Offset   int              `json:"offset"`
}

type storedAction struct {
	Type    string  `json:"type"`
	UserIDs []int64 `json:"user_ids,omitempty"`
	UserID  *int64  `json:"user_id,omitempty"`
	Title   *string `json:"title,omitempty"`
	Seq     *int64  `json:"seq,omitempty"`
}

func actionView(a *storedAction) *dto.ServiceAction {
	if a == nil {
		return nil
	}
	v := &dto.ServiceAction{Type: a.Type, UserID: dto.IDPtr(a.UserID), Title: a.Title, Seq: a.Seq}
	for _, id := range a.UserIDs {
		v.UserIDs = append(v.UserIDs, dto.ID(id))
	}
	return v
}

type storedDraft struct {
	Text       string           `json:"text"`
	Entities   []content.Entity `json:"entities"`
	ReplyToSeq *int64           `json:"reply_to_seq"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

type reactionAgg struct {
	MessageID int64     `gorm:"column:message_id"`
	Reaction  string    `gorm:"column:reaction"`
	N         int       `gorm:"column:n"`
	Reacted   bool      `gorm:"column:reacted"`
	First     time.Time `gorm:"column:first"`
}

func loadReactions(db *gorm.DB, viewer int64, ids []int64) (map[int64][]dto.ReactionCount, error) {
	out := map[int64][]dto.ReactionCount{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []reactionAgg
	if err := db.Raw(`
		SELECT message_id, reaction, count(*) AS n, bool_or(user_id = ?) AS reacted, min(created_at) AS first
		  FROM chat_reaction WHERE message_id IN ?
		 GROUP BY message_id, reaction`, viewer, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].N != rows[j].N {
			return rows[i].N > rows[j].N
		}
		return rows[i].First.Before(rows[j].First)
	})
	for _, r := range rows {
		out[r.MessageID] = append(out[r.MessageID], dto.ReactionCount{Reaction: r.Reaction, Count: r.N, Reacted: r.Reacted})
	}
	return out, nil
}

type seqKey struct{ conversationID, seq int64 }

func loadReplyTargets(db *gorm.DB, msgs []model.ChatMessage) (map[seqKey]model.ChatMessage, error) {
	byConv := map[int64][]int64{}
	for _, m := range msgs {
		if m.ReplyToSeq != nil {
			byConv[m.ConversationID] = append(byConv[m.ConversationID], *m.ReplyToSeq)
		}
	}
	out := map[seqKey]model.ChatMessage{}
	for conv, seqs := range byConv {
		var rows []model.ChatMessage
		if err := db.Where("conversation_id = ? AND seq IN ?", conv, seqs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[seqKey{conv, r.Seq}] = r
		}
	}
	return out, nil
}

type visibility struct {
	members map[[2]int64]model.ChatMember
	hidden  map[[2]int64]bool
}

func loadVisibility(db *gorm.DB, conversationIDs, users, messageIDs []int64) (*visibility, error) {
	v := &visibility{members: map[[2]int64]model.ChatMember{}, hidden: map[[2]int64]bool{}}
	if len(conversationIDs) == 0 || len(users) == 0 {
		return v, nil
	}
	var ms []model.ChatMember
	if err := db.Where("conversation_id IN ? AND user_id IN ?", conversationIDs, users).Find(&ms).Error; err != nil {
		return nil, err
	}
	for _, m := range ms {
		v.members[[2]int64{m.ConversationID, m.UserID}] = m
	}
	if len(messageIDs) > 0 {
		var hs []model.ChatHiddenMessage
		if err := db.Where("user_id IN ? AND message_id IN ?", users, messageIDs).Find(&hs).Error; err != nil {
			return nil, err
		}
		for _, h := range hs {
			v.hidden[[2]int64{h.UserID, h.MessageID}] = true
		}
	}
	return v, nil
}

func (v *visibility) sees(uid int64, m model.ChatMessage) bool {
	mem, ok := v.members[[2]int64{m.ConversationID, uid}]
	return ok && mem.LeftAt == nil && m.DeletedAt == nil &&
		m.Seq >= mem.VisibleFromSeq && m.Seq > mem.ClearedThroughSeq && !v.hidden[[2]int64{uid, m.ID}]
}

func unavailablePreview(r model.ChatMessage) *dto.ReplyPreview {
	return &dto.ReplyPreview{Seq: r.Seq, SenderID: dto.ID(r.SenderID), Deleted: true, Entities: []dto.Entity{}}
}

func replyPreview(r model.ChatMessage) *dto.ReplyPreview {
	p := &dto.ReplyPreview{Seq: r.Seq, SenderID: dto.ID(r.SenderID), Deleted: r.DeletedAt != nil, Entities: []dto.Entity{}}
	if r.DeletedAt != nil {
		return p
	}
	text, ents := content.Preview(r.Text, decodeEntities(r.Entities))
	p.Text = text
	p.Entities = dto.EntitiesOut(ents)
	if media := decodeJSON[dto.Media](r.Media); media != nil {
		t := media.Type
		p.MediaType = &t
	}
	return p
}

func messageView(m model.ChatMessage, viewer int64, reactions []dto.ReactionCount, reply *dto.ReplyPreview) dto.Message {
	v := dto.Message{
		Object: "message", ID: dto.ID(m.ID), ConversationID: dto.ID(m.ConversationID), Seq: m.Seq,
		SenderID: dto.ID(m.SenderID), Kind: m.Kind, Text: m.Text, Entities: dto.EntitiesOut(decodeEntities(m.Entities)),
		Media: decodeJSON[dto.Media](m.Media), MediaGroupID: dto.IDPtr(m.MediaGroupID),
		ServiceAction: actionView(decodeJSON[storedAction](m.ServiceAction)),
		Context:       decodeJSON[dto.ContextCard](m.Context), Reactions: reactions,
		Silent: m.Silent, PinnedAt: m.PinnedAt, EditedAt: m.EditedAt, CreatedAt: m.CreatedAt,
	}
	if v.Reactions == nil {
		v.Reactions = []dto.ReactionCount{}
	}
	if q := decodeJSON[storedQuote](m.ReplyQuote); q != nil {
		v.ReplyQuote = &dto.Quote{Text: q.Text, Entities: dto.EntitiesOut(q.Entities), Offset: q.Offset}
	}
	v.ReplyTo = reply
	if m.SenderID == viewer && m.ClientMessageID != nil {
		id := *m.ClientMessageID
		v.ClientMessageID = &id
	}
	return v
}

func hydrate(db *gorm.DB, viewer int64, msgs []model.ChatMessage) ([]dto.Message, error) {
	out := make([]dto.Message, 0, len(msgs))
	if len(msgs) == 0 {
		return out, nil
	}
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	reactions, err := loadReactions(db, viewer, ids)
	if err != nil {
		return nil, err
	}
	replies, err := loadReplyTargets(db, msgs)
	if err != nil {
		return nil, err
	}
	var convIDs, targetIDs []int64
	for _, r := range replies {
		convIDs = append(convIDs, r.ConversationID)
		targetIDs = append(targetIDs, r.ID)
	}
	vis, err := loadVisibility(db, convIDs, []int64{viewer}, targetIDs)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		var reply *dto.ReplyPreview
		if m.ReplyToSeq != nil {
			if r, ok := replies[seqKey{m.ConversationID, *m.ReplyToSeq}]; ok {
				if vis.sees(viewer, r) {
					reply = replyPreview(r)
				} else {
					reply = unavailablePreview(r)
				}
			}
		}
		out = append(out, messageView(m, viewer, reactions[m.ID], reply))
	}
	return out, nil
}

func draftView(j datatypes.JSON) *dto.Draft {
	d := decodeJSON[storedDraft](j)
	if d == nil {
		return nil
	}
	return &dto.Draft{Text: d.Text, Entities: dto.EntitiesOut(d.Entities), ReplyToSeq: d.ReplyToSeq, UpdatedAt: d.UpdatedAt}
}

func dialogView(m model.ChatMember) dto.DialogState {
	return dto.DialogState{
		Role: m.Role, Accepted: m.AcceptedAt != nil, LastReadSeq: m.LastReadSeq, UnreadCount: m.UnreadCount,
		MarkedUnread: m.MarkedUnread, ClearedThroughSeq: m.ClearedThroughSeq, VisibleFromSeq: m.VisibleFromSeq,
		MutedUntil: m.MutedUntil, Archived: m.ArchivedAt != nil, PinnedRank: m.PinnedRank, Draft: draftView(m.Draft),
	}
}

func peerOf(c model.ChatConversation, viewer int64) *int64 {
	if c.Kind != model.KindDirect || c.DirectUserLowID == nil || c.DirectUserHighID == nil {
		return nil
	}
	peer := *c.DirectUserLowID
	if peer == viewer {
		peer = *c.DirectUserHighID
	}
	return &peer
}

type peerRead struct {
	ConversationID int64 `gorm:"column:conversation_id"`
	MaxRead        int64 `gorm:"column:max_read"`
	AllAccepted    bool  `gorm:"column:all_accepted"`
}

func peerReadSeqs(db *gorm.DB, viewer int64, convs []model.ChatConversation) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(convs) == 0 {
		return out, nil
	}
	ids := make([]int64, len(convs))
	kind := map[int64]string{}
	for i, c := range convs {
		ids[i] = c.ID
		kind[c.ID] = c.Kind
	}
	var rows []peerRead
	if err := db.Raw(`
		SELECT conversation_id,
		       coalesce(max(last_read_seq), 0) AS max_read,
		       coalesce(bool_and(accepted_at IS NOT NULL), true) AS all_accepted
		  FROM chat_member
		 WHERE conversation_id IN ? AND user_id <> ? AND left_at IS NULL
		 GROUP BY conversation_id`, ids, viewer).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if kind[r.ConversationID] == model.KindDirect && !r.AllAccepted {
			out[r.ConversationID] = 0
			continue
		}
		out[r.ConversationID] = r.MaxRead
	}
	return out, nil
}

func lastVisible(db *gorm.DB, viewer int64, ids []int64) (map[int64]model.ChatMessage, error) {
	out := map[int64]model.ChatMessage{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.ChatMessage
	if err := db.Raw(`
		SELECT x.*
		  FROM chat_member m
		  JOIN LATERAL (
		        SELECT * FROM chat_message x
		         WHERE x.conversation_id = m.conversation_id
		           AND x.seq > m.cleared_through_seq AND x.seq >= m.visible_from_seq
		           AND x.deleted_at IS NULL
		           AND NOT EXISTS (SELECT 1 FROM chat_hidden_message h WHERE h.user_id = m.user_id AND h.message_id = x.id)
		         ORDER BY x.seq DESC LIMIT 1) x ON true
		 WHERE m.user_id = ? AND m.conversation_id IN ?`, viewer, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ConversationID] = r
	}
	return out, nil
}

type dialogRow struct {
	Member model.ChatMember
	Conv   model.ChatConversation
}

func conversationViews(db *gorm.DB, viewer int64, rows []dialogRow) ([]dto.Conversation, []int64, error) {
	convs := make([]model.ChatConversation, len(rows))
	ids := make([]int64, len(rows))
	for i, r := range rows {
		convs[i] = r.Conv
		ids[i] = r.Conv.ID
	}
	reads, err := peerReadSeqs(db, viewer, convs)
	if err != nil {
		return nil, nil, err
	}
	lasts, err := lastVisible(db, viewer, ids)
	if err != nil {
		return nil, nil, err
	}
	lastMsgs := make([]model.ChatMessage, 0, len(lasts))
	for _, id := range ids {
		if m, ok := lasts[id]; ok {
			lastMsgs = append(lastMsgs, m)
		}
	}
	views, err := hydrate(db, viewer, lastMsgs)
	if err != nil {
		return nil, nil, err
	}
	byConv := map[int64]*dto.Message{}
	for i := range views {
		id, _ := dto.ParseID(views[i].ConversationID)
		byConv[id] = &views[i]
	}
	var userIDs []int64
	out := make([]dto.Conversation, len(rows))
	for i, r := range rows {
		c := r.Conv
		peer := peerOf(c, viewer)
		out[i] = dto.Conversation{
			Object: "conversation", ID: dto.ID(c.ID), Kind: c.Kind, Title: c.Title, About: c.About,
			PhotoImageHash: c.PhotoImageHash, PeerID: dto.IDPtr(peer), MemberCount: c.MemberCount, LastSeq: c.LastSeq,
			PeerReadSeq: reads[c.ID], CreatedAt: c.CreatedAt, Me: dialogView(r.Member),
			LastMessage: byConv[c.ID],
		}
		if peer != nil {
			userIDs = append(userIDs, *peer)
		}
		if lm := byConv[c.ID]; lm != nil {
			userIDs = append(userIDs, messageUserIDs([]dto.Message{*lm})...)
		}
	}
	return out, userIDs, nil
}

func (s *Service) userViews(ctx context.Context, ids []int64) ([]dto.User, error) {
	uniq := map[int64]bool{}
	var list []int64
	for _, id := range ids {
		if id > 0 && !uniq[id] {
			uniq[id] = true
			list = append(list, id)
		}
	}
	out := make([]dto.User, 0, len(list))
	if len(list) == 0 || s.users == nil {
		return out, nil
	}
	profiles, err := s.users.Profiles(ctx, list)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	for _, id := range list {
		p, ok := profiles[id]
		if !ok || p.Deleted {
			out = append(out, dto.User{Object: "user", ID: dto.ID(id), Deleted: true})
			continue
		}
		out = append(out, dto.User{Object: "user", ID: dto.ID(id), Name: p.Name, Avatar: p.Avatar})
	}
	return out, nil
}

func messageUserIDs(ms []dto.Message) []int64 {
	var raw []string
	for _, m := range ms {
		raw = append(raw, m.SenderID)
		if m.ReplyTo != nil {
			raw = append(raw, m.ReplyTo.SenderID)
		}
		if m.ServiceAction != nil {
			raw = append(raw, m.ServiceAction.UserIDs...)
			if m.ServiceAction.UserID != nil {
				raw = append(raw, *m.ServiceAction.UserID)
			}
		}
		for _, e := range m.Entities {
			if e.UserID != nil {
				raw = append(raw, *e.UserID)
			}
		}
	}
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		if id, ok := dto.ParseID(s); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func updateView(u model.ChatUpdate) dto.Update {
	return dto.Update{Object: "update", UpdateSeq: u.UpdateSeq, Kind: u.Kind, ConversationID: dto.ID(u.ConversationID),
		Data: json.RawMessage(u.Data), CreatedAt: u.CreatedAt}
}
