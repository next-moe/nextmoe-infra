// Package migrate owns the kun_chat schema: the AutoMigrate table list plus the
// idempotent raw-SQL section for the indexes AutoMigrate cannot express
// (partial and DESC-ordered). Called by `cmd/migrate chat` and by the
// migrate integration test.
package migrate

import (
	"fmt"

	"api/internal/platform/accountpurge"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// 2026-09-27: the chat service's first schema (plan 17 W2). Every table
		// is new and empty.
		&model.ChatUser{},
		&model.ChatConversation{},
		&model.ChatMember{},
		&model.ChatMessage{},
		&model.ChatReaction{},
		&model.ChatHiddenMessage{},
		&model.ChatUpdate{},
		&model.ChatReport{},
		&accountpurge.Cursor{},
		// The import ledger of cmd/import-chat (plan 17 W4); new and empty.
		&model.ChatImportMessage{},
	); err != nil {
		return fmt.Errorf("chat automigrate: %w", err)
	}
	for _, stmt := range rawIndexes {
		if err := db.Exec(stmt.sql).Error; err != nil {
			return fmt.Errorf("chat index %s: %w", stmt.name, err)
		}
	}
	return nil
}

var rawIndexes = []struct{ name, sql string }{
	// One direct conversation per pair of users: "a conversation belongs to
	// the people in it" (plan 17 §0).
	{"uq_chat_conversation_direct", `
		CREATE UNIQUE INDEX IF NOT EXISTS uq_chat_conversation_direct
		    ON chat_conversation(direct_user_low_id, direct_user_high_id) WHERE kind = 'direct'`},
	// A user's conversation list, most recent first.
	{"idx_chat_member_dialogs", `
		CREATE INDEX IF NOT EXISTS idx_chat_member_dialogs
		    ON chat_member(user_id, last_message_at DESC, conversation_id DESC) WHERE left_at IS NULL`},
	{"idx_chat_message_pinned", `
		CREATE INDEX IF NOT EXISTS idx_chat_message_pinned
		    ON chat_message(conversation_id, pinned_at DESC) WHERE pinned_at IS NOT NULL`},
	{"idx_chat_report_pending", `
		CREATE INDEX IF NOT EXISTS idx_chat_report_pending
		    ON chat_report(id) WHERE status = 'pending'`},
}
