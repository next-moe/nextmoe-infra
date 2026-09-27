package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/model"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

const (
	reportContextMessages = 10
	maxForwardAttempts    = 20
	maxContextNote        = 4000
	SubjectKind           = "chat_message"
)

var reportReasons = map[string]bool{"spam": true, "harassment": true, "sexual": true, "violence": true, "illegal": true, "other": true}

type snapshotMessage struct {
	Seq       int64            `json:"seq"`
	SenderID  int64            `json:"sender_id"`
	Text      string           `json:"text"`
	Entities  []content.Entity `json:"entities,omitempty"`
	MediaHash string           `json:"media_hash,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	Reported  bool             `json:"reported,omitempty"`
}

type ReportResult struct {
	ReportID int64
	Created  bool
}

func (s *Service) Report(ctx context.Context, a Actor, messageID int64, reason string, note *string) (*ReportResult, error) {
	if !reportReasons[reason] {
		return nil, &InvalidError{Field: "reason", Reason: "must be spam, harassment, sexual, violence, illegal or other"}
	}
	if note != nil {
		n := strings.TrimSpace(*note)
		if content.UTF16Len(n) > 500 {
			return nil, &InvalidError{Field: "note", Reason: "at most 500 characters"}
		}
		note = &n
	}
	db := s.db.WithContext(ctx)
	var target model.ChatMessage
	if err := db.Where("id = ? AND deleted_at IS NULL", messageID).Take(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	me, err := s.activeMember(db, target.ConversationID, a.UserID)
	if err != nil {
		return nil, err
	}
	if target.Seq < me.VisibleFromSeq || target.Seq <= me.ClearedThroughSeq {
		return nil, ErrNotFound
	}
	if target.SenderID == a.UserID || target.Kind != model.MessageKindMessage {
		return nil, &InvalidError{Field: "message", Reason: "only someone else's message can be reported"}
	}
	var context_ []model.ChatMessage
	if err := visibleTo(db, me).Where("seq < ?", target.Seq).Order("seq DESC").Limit(reportContextMessages).Find(&context_).Error; err != nil {
		return nil, err
	}
	context_ = append([]model.ChatMessage{target}, context_...)
	snap := make([]snapshotMessage, 0, len(context_))
	for i := len(context_) - 1; i >= 0; i-- {
		m := context_[i]
		sm := snapshotMessage{Seq: m.Seq, SenderID: m.SenderID, Text: m.Text, Entities: decodeEntities(m.Entities), CreatedAt: m.CreatedAt, Reported: m.ID == target.ID}
		if media := decodeJSON[struct {
			ImageHash string `json:"image_hash"`
		}](m.Media); media != nil {
			sm.MediaHash = media.ImageHash
		}
		snap = append(snap, sm)
	}
	row := model.ChatReport{
		MessageID: target.ID, ConversationID: target.ConversationID, ReporterID: a.UserID, ReportedUserID: target.SenderID,
		Reason: reason, Note: note, Snapshot: jsonOf(map[string]any{"messages": snap}), OriginSite: a.Site,
		Status: model.ReportPending, CreatedAt: s.now(),
	}
	res := db.Clauses(onConflictNothing).Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		var existing model.ChatReport
		if err := db.Where("reporter_id = ? AND message_id = ?", a.UserID, target.ID).Take(&existing).Error; err != nil {
			return nil, err
		}
		return &ReportResult{ReportID: existing.ID, Created: false}, nil
	}
	return &ReportResult{ReportID: row.ID, Created: true}, nil
}

type Forwarder interface {
	Forward(ctx context.Context, req trustclient.ForwardRequest) (int64, bool, error)
}

// A report trust refuses outright is marked failed, not retried every sweep:
// moyu's review items once looped each minute against a permanent 422
// because their subject kind was never registered.
func (s *Service) ForwardReports(ctx context.Context, fwd Forwarder) (int, error) {
	if fwd == nil {
		return 0, nil
	}
	var pending []model.ChatReport
	if err := s.db.WithContext(ctx).Where("status = ? AND resolution IS NULL", model.ReportPending).Order("id").Limit(50).Find(&pending).Error; err != nil {
		return 0, err
	}
	sent := 0
	for _, r := range pending {
		noteText := reportNote(r)
		ref := fmt.Sprintf("chat_report:%d", r.ID)
		itemID, _, err := fwd.Forward(ctx, trustclient.ForwardRequest{
			Site: r.OriginSite, SubjectKind: SubjectKind, SubjectID: fmt.Sprint(r.MessageID),
			ContextNote: &noteText, ForwarderRef: &ref,
		})
		now := s.now()
		if err == nil {
			sent++
			if uerr := s.db.WithContext(ctx).Model(&model.ChatReport{}).Where("id = ?", r.ID).
				Updates(map[string]any{"status": model.ReportForwarded, "trust_review_item_id": itemID, "forwarded_at": now, "forward_error": nil}).Error; uerr != nil {
				return sent, uerr
			}
			continue
		}
		msg := err.Error()
		status := model.ReportPending
		if isPermanent(err) || r.ForwardAttempts+1 >= maxForwardAttempts {
			status = model.ReportFailed
			slog.Error("chat report forward gave up", "report_id", r.ID, "site", r.OriginSite, "err", err)
		}
		if uerr := s.db.WithContext(ctx).Model(&model.ChatReport{}).Where("id = ?", r.ID).
			Updates(map[string]any{"status": status, "forward_attempts": gorm.Expr("forward_attempts + 1"), "forward_error": msg}).Error; uerr != nil {
			return sent, uerr
		}
	}
	return sent, nil
}

func isPermanent(err error) bool {
	type statusCoder interface{ StatusCode() int }
	var sc statusCoder
	if errors.As(err, &sc) {
		code := sc.StatusCode()
		return code >= 400 && code < 500 && code != 408 && code != 429
	}
	return false
}

func reportNote(r model.ChatReport) string {
	snap := decodeJSON[struct {
		Messages []snapshotMessage `json:"messages"`
	}](r.Snapshot)
	var b strings.Builder
	fmt.Fprintf(&b, "chat report %d, reason %s, reporter %d, reported user %d\n", r.ID, r.Reason, r.ReporterID, r.ReportedUserID)
	if r.Note != nil && *r.Note != "" {
		fmt.Fprintf(&b, "note: %s\n", *r.Note)
	}
	if snap != nil {
		for _, m := range snap.Messages {
			mark := " "
			if m.Reported {
				mark = ">"
			}
			text := m.Text
			if m.MediaHash != "" {
				text = "[image " + m.MediaHash + "] " + text
			}
			fmt.Fprintf(&b, "%s #%d u%d %s: %s\n", mark, m.Seq, m.SenderID, m.CreatedAt.UTC().Format(time.RFC3339), text)
		}
	}
	out := b.String()
	if len(out) > maxContextNote {
		out = out[:maxContextNote]
		for !strings.HasSuffix(out, "\n") && len(out) > 0 && !validUTF8Tail(out) {
			out = out[:len(out)-1]
		}
	}
	return out
}

func validUTF8Tail(s string) bool {
	return strings.ToValidUTF8(s, "") == s
}
