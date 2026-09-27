package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const messagesPerMinute = 30

type MediaInput struct {
	Type      string
	ImageHash string
}

type QuoteInput struct {
	Text   string
	Offset int
}

type ContextInput struct {
	Kind  string
	ID    string
	Title string
	URL   string
}

type SendInput struct {
	ClientMessageID *string
	Text            string
	Entities        []content.Entity
	Media           *MediaInput
	MediaGroupID    *int64
	ReplyToSeq      *int64
	ReplyQuote      *QuoteInput
	Context         *ContextInput
	Silent          bool
}

type MessageResult struct {
	Message dto.Message
	Users   []dto.User
}

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	imageHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	contextKey       = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func contentErr(err error) error {
	if ce, ok := content.IsError(err); ok {
		return &InvalidError{Field: ce.Field, Reason: ce.Reason}
	}
	return err
}

func hostAllowed(host string, hosts []string) bool {
	host = strings.ToLower(host)
	for _, h := range hosts {
		h = strings.ToLower(h)
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func (s *Service) buildContext(a Actor, in *ContextInput) (*dto.ContextCard, error) {
	if in == nil {
		return nil, nil
	}
	if !contextKey.MatchString(in.Kind) || !contextKey.MatchString(in.ID) {
		return nil, &InvalidError{Field: "context", Reason: "kind and id must be 1-64 characters of [A-Za-z0-9_.:-]"}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || content.UTF16Len(title) > 200 {
		return nil, &InvalidError{Field: "context.title", Reason: "must be 1-200 characters"}
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(in.URL) > content.MaxURLLength {
		return nil, &InvalidError{Field: "context.url", Reason: "must be an https URL"}
	}
	if !hostAllowed(u.Hostname(), a.Hosts) {
		return nil, &InvalidError{Field: "context.url", Reason: "must point at the calling site"}
	}
	return &dto.ContextCard{Site: a.Site, Kind: in.Kind, ID: in.ID, Title: title, URL: u.String()}, nil
}

func (s *Service) buildMedia(ctx context.Context, in *MediaInput) (*dto.Media, error) {
	if in == nil {
		return nil, nil
	}
	if in.Type != "photo" {
		return nil, &InvalidError{Field: "media.type", Reason: "only photo is supported"}
	}
	if !imageHashPattern.MatchString(in.ImageHash) {
		return nil, &InvalidError{Field: "media.image_hash", Reason: "must be a 64-character lowercase hex hash"}
	}
	if s.images == nil {
		return nil, ErrImagesDisabled
	}
	metas, err := s.images.Meta(ctx, []string{in.ImageHash})
	if err != nil {
		return nil, err
	}
	meta, ok := metas[in.ImageHash]
	if !ok {
		return nil, &InvalidError{Field: "media.image_hash", Reason: "no such image"}
	}
	return &dto.Media{Type: "photo", ImageHash: in.ImageHash, Width: meta.Width, Height: meta.Height, Thumbhash: meta.Thumbhash}, nil
}

func checkMentions(entities []content.Entity, members []model.ChatMember) error {
	for _, id := range content.MentionedUserIDs(entities) {
		if findMember(members, id) == nil {
			return &InvalidError{Field: "entities", Reason: "a mention names someone outside the conversation"}
		}
	}
	return nil
}

func (s *Service) existingByClientID(db *gorm.DB, sender int64, clientID *string) (*model.ChatMessage, error) {
	if clientID == nil {
		return nil, nil
	}
	var m model.ChatMessage
	err := db.Where("sender_id = ? AND client_message_id = ?", sender, *clientID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}

func (s *Service) Send(ctx context.Context, a Actor, conversationID int64, in SendInput) (*MessageResult, error) {
	if in.ClientMessageID != nil {
		id := strings.ToLower(*in.ClientMessageID)
		if !uuidPattern.MatchString(id) {
			return nil, &InvalidError{Field: "client_message_id", Reason: "must be a UUID"}
		}
		in.ClientMessageID = &id
		if m, err := s.existingByClientID(s.db.WithContext(ctx), a.UserID, in.ClientMessageID); err != nil {
			return nil, err
		} else if m != nil {
			if m.ConversationID != conversationID {
				return nil, &InvalidError{Field: "client_message_id", Reason: "already used in another conversation"}
			}
			return s.messageResult(ctx, a.UserID, *m)
		}
	}
	text, entities, err := content.Normalize(in.Text, in.Entities)
	if err != nil {
		return nil, contentErr(err)
	}
	media, err := s.buildMedia(ctx, in.Media)
	if err != nil {
		return nil, err
	}
	if text == "" && media == nil {
		return nil, &InvalidError{Field: "text", Reason: "a message needs text or media"}
	}
	card, err := s.buildContext(a, in.Context)
	if err != nil {
		return nil, err
	}
	if in.MediaGroupID != nil && (media == nil || *in.MediaGroupID == 0) {
		return nil, &InvalidError{Field: "media_group_id", Reason: "only a media message may join a group, and the id must not be 0"}
	}
	if in.ReplyQuote != nil && in.ReplyToSeq == nil {
		return nil, &InvalidError{Field: "reply_quote", Reason: "needs reply_to_seq"}
	}
	if err := s.allow(ctx, "send:"+itoa(a.UserID), messagesPerMinute, time.Minute); err != nil {
		return nil, err
	}

	var (
		msg        *model.ChatMessage
		updates    []model.ChatUpdate
		recipients []int64
	)
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		extra, err := s.admitSend(ctx, tx, conv, members, me, entities, media)
		if err != nil {
			return err
		}
		if err := checkMentions(entities, members); err != nil {
			return err
		}
		row := &model.ChatMessage{
			SenderID: a.UserID, Kind: model.MessageKindMessage, Text: text,
			MediaGroupID: in.MediaGroupID, ReplyToSeq: in.ReplyToSeq,
			ClientMessageID: in.ClientMessageID, OriginSite: a.Site, Silent: in.Silent,
		}
		if entities != nil {
			row.Entities = jsonOf(entities)
		}
		if media != nil {
			row.Media = jsonOf(media)
		}
		if card != nil {
			row.Context = jsonOf(card)
		}
		if in.ReplyToSeq != nil {
			var target model.ChatMessage
			err := tx.Where("conversation_id = ? AND seq = ? AND deleted_at IS NULL AND seq >= ?", conv.ID, *in.ReplyToSeq, me.VisibleFromSeq).
				Take(&target).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &InvalidError{Field: "reply_to_seq", Reason: "no such message"}
			}
			if err != nil {
				return err
			}
			if in.ReplyQuote != nil {
				ents, err := content.QuoteAt(target.Text, decodeEntities(target.Entities), in.ReplyQuote.Text, in.ReplyQuote.Offset)
				if err != nil {
					return contentErr(err)
				}
				row.ReplyQuote = jsonOf(storedQuote{Text: in.ReplyQuote.Text, Entities: ents, Offset: in.ReplyQuote.Offset})
			}
		}
		msg, updates, err = s.appendMessage(tx, conv, members, row, extra)
		if err != nil {
			if isUniqueViolation(err) && in.ClientMessageID != nil {
				return errDuplicateClientID
			}
			return err
		}
		recipients = memberIDs(members)
		return nil
	})
	if errors.Is(err, errDuplicateClientID) {
		m, ferr := s.existingByClientID(s.db.WithContext(ctx), a.UserID, in.ClientMessageID)
		if ferr != nil || m == nil {
			return nil, ferr
		}
		return s.messageResult(ctx, a.UserID, *m)
	}
	if err != nil {
		return nil, err
	}
	s.publishMessageUpdates(ctx, updates, *msg, recipients)
	return s.messageResult(ctx, a.UserID, *msg)
}

var errDuplicateClientID = errors.New("chat: duplicate client_message_id")

func isUniqueViolation(err error) bool {
	type sqlState interface{ SQLState() string }
	var st sqlState
	return errors.As(err, &st) && st.SQLState() == "23505"
}

func memberIDs(ms []model.ChatMember) []int64 {
	ids := make([]int64, len(ms))
	for i, m := range ms {
		ids[i] = m.UserID
	}
	return ids
}

func (s *Service) admitSend(ctx context.Context, tx *gorm.DB, conv *model.ChatConversation, members []model.ChatMember, me *model.ChatMember,
	entities []content.Entity, media *dto.Media) ([]pendingUpdate, error) {
	if conv.Kind != model.KindDirect {
		return nil, nil
	}
	peerID := peerOf(*conv, me.UserID)
	peer := findMember(members, *peerID)
	if peer == nil {
		return nil, &NotAcceptingError{Reason: "the other account no longer exists"}
	}
	if s.rel != nil {
		blocked, err := s.rel.BlockedEitherWay(ctx, me.UserID, peer.UserID)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, ErrBlocked
		}
	}
	if peer.AcceptedAt == nil {
		if media != nil || content.HasLink(entities) {
			return nil, ErrRequestLimit
		}
		var sent int64
		if err := tx.Model(&model.ChatMessage{}).
			Where("conversation_id = ? AND sender_id = ? AND kind = ?", conv.ID, me.UserID, model.MessageKindMessage).
			Count(&sent).Error; err != nil {
			return nil, err
		}
		if sent >= pendingMessageLimit {
			return nil, ErrRequestLimit
		}
	}
	if me.AcceptedAt == nil {
		now := s.now()
		if err := tx.Model(&model.ChatMember{}).
			Where("conversation_id = ? AND user_id = ?", conv.ID, me.UserID).
			Updates(map[string]any{"accepted_at": now, "updated_at": now}).Error; err != nil {
			return nil, err
		}
		me.AcceptedAt = &now
		return []pendingUpdate{{userID: me.UserID, kind: model.UpdateDialog, conversationID: conv.ID, data: map[string]any{"accepted": true}}}, nil
	}
	return nil, nil
}

// The caller must already hold the conversation and member locks.
func (s *Service) appendMessage(tx *gorm.DB, conv *model.ChatConversation, members []model.ChatMember, row *model.ChatMessage, extra []pendingUpdate) (*model.ChatMessage, []model.ChatUpdate, error) {
	now := s.now()
	seq := conv.LastSeq + 1
	if err := tx.Model(&model.ChatConversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{"last_seq": seq, "updated_at": now}).Error; err != nil {
		return nil, nil, err
	}
	conv.LastSeq = seq
	row.ConversationID = conv.ID
	row.Seq = seq
	row.CreatedAt = now
	if err := tx.Create(row).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Exec(`
		UPDATE chat_member SET
		       unread_count    = CASE WHEN user_id = ? THEN 0 ELSE unread_count + 1 END,
		       last_read_seq   = CASE WHEN user_id = ? THEN ? ELSE last_read_seq END,
		       marked_unread   = CASE WHEN user_id = ? THEN false ELSE marked_unread END,
		       archived_at     = CASE WHEN user_id <> ? AND archived_at IS NOT NULL
		                                   AND (muted_until IS NULL OR muted_until <= ?) THEN NULL
		                              ELSE archived_at END,
		       last_message_at = ?,
		       updated_at      = ?
		 WHERE conversation_id = ? AND left_at IS NULL`,
		row.SenderID, row.SenderID, seq, row.SenderID, row.SenderID, now, now, now, conv.ID).Error; err != nil {
		return nil, nil, err
	}
	ups := append([]pendingUpdate{}, extra...)
	for _, m := range members {
		ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateNewMessage, conversationID: conv.ID, data: map[string]any{"seq": seq}})
	}
	written, err := appendUpdates(tx, now, ups)
	if err != nil {
		return nil, nil, err
	}
	return row, written, nil
}

func (s *Service) messageResult(ctx context.Context, viewer int64, m model.ChatMessage) (*MessageResult, error) {
	views, err := hydrate(s.db.WithContext(ctx), viewer, []model.ChatMessage{m})
	if err != nil {
		return nil, err
	}
	users, err := s.userViews(ctx, messageUserIDs(views))
	if err != nil {
		return nil, err
	}
	return &MessageResult{Message: views[0], Users: users}, nil
}

func (s *Service) Edit(ctx context.Context, a Actor, messageID int64, text string, entities []content.Entity) (*MessageResult, error) {
	text, entities, err := content.Normalize(text, entities)
	if err != nil {
		return nil, contentErr(err)
	}
	conversationID, err := s.conversationOfMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	var (
		msg        model.ChatMessage
		updates    []model.ChatUpdate
		recipients []int64
	)
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND deleted_at IS NULL", messageID).Take(&msg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if msg.SenderID != a.UserID || msg.Kind != model.MessageKindMessage {
			return ErrNotPermitted
		}
		now := s.now()
		if now.Sub(msg.CreatedAt) > editWindow {
			return ErrEditWindow
		}
		if text == "" && len(msg.Media) == 0 {
			return &InvalidError{Field: "text", Reason: "a message needs text or media"}
		}
		if conv.Kind == model.KindDirect {
			if peer := findMember(members, *peerOf(*conv, me.UserID)); peer != nil && peer.AcceptedAt == nil && content.HasLink(entities) {
				return ErrRequestLimit
			}
		}
		if err := checkMentions(entities, members); err != nil {
			return err
		}
		msg.Text = text
		msg.Entities = nil
		if entities != nil {
			msg.Entities = jsonOf(entities)
		}
		msg.EditedAt = &now
		if err := tx.Model(&model.ChatMessage{}).Where("id = ?", msg.ID).
			Updates(map[string]any{"text": msg.Text, "entities": msg.Entities, "edited_at": now}).Error; err != nil {
			return err
		}
		ups := make([]pendingUpdate, 0, len(members))
		for _, m := range members {
			ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateEditMessage, conversationID: conv.ID, data: map[string]any{"seq": msg.Seq}})
		}
		updates, err = appendUpdates(tx, now, ups)
		recipients = memberIDs(members)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publishMessageUpdates(ctx, updates, msg, recipients)
	return s.messageResult(ctx, a.UserID, msg)
}

func (s *Service) conversationOfMessage(ctx context.Context, messageID int64) (int64, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Model(&model.ChatMessage{}).Where("id = ?", messageID).Pluck("conversation_id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, ErrNotFound
	}
	return ids[0], nil
}
