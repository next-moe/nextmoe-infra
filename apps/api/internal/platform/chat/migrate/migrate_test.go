package migrate

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

const suiteLockKey = 0x63686174

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("chat/migrate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("chat/migrate", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	conn, err := sqlDB.Conn(context.Background())
	if err == nil {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", suiteLockKey)
	}
	if err := Run(db); err != nil {
		dbtest.SkipMainf("chat/migrate", "chat migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	if conn != nil {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", suiteLockKey)
		_ = conn.Close()
	}
	os.Exit(code)
}

func columns(t *testing.T, table string) []string {
	t.Helper()
	var names []string
	if err := testDB.Raw(`SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = ? ORDER BY column_name`, table).Scan(&names).Error; err != nil {
		t.Fatal(err)
	}
	return names
}

// The full column list of every chat table. A column added to a model without
// a line here fails, so the contract doc and the importer get looked at too.
func TestColumnAudit(t *testing.T) {
	want := map[string][]string{
		"chat_user": {"user_id", "last_update_seq", "allow_incoming", "accept_requests", "allow_group_invites", "created_at", "updated_at"},
		"chat_conversation": {"id", "kind", "direct_user_low_id", "direct_user_high_id", "title", "about", "photo_image_hash",
			"creator_id", "origin_site", "last_seq", "member_count", "created_at", "updated_at", "deleted_at"},
		"chat_member": {"conversation_id", "user_id", "role", "joined_at", "visible_from_seq", "left_at", "accepted_at",
			"last_read_seq", "unread_count", "marked_unread", "cleared_through_seq", "last_message_at", "muted_until",
			"archived_at", "pinned_rank", "draft", "updated_at"},
		"chat_message": {"id", "conversation_id", "seq", "sender_id", "kind", "text", "entities", "media", "media_group_id",
			"reply_to_seq", "reply_quote", "forward_origin", "service_action", "context", "client_message_id", "origin_site",
			"silent", "pinned_at", "edited_at", "deleted_at", "created_at"},
		"chat_reaction":       {"message_id", "user_id", "reaction", "created_at"},
		"chat_hidden_message": {"user_id", "message_id", "created_at"},
		"chat_update":         {"user_id", "update_seq", "kind", "conversation_id", "data", "created_at"},
		"chat_import_message": {"source_key", "message_id", "conversation_id", "imported_at"},
		"chat_report": {"id", "message_id", "conversation_id", "reporter_id", "reported_user_id", "reason", "note", "snapshot",
			"origin_site", "status", "trust_review_item_id", "forward_attempts", "forward_error", "created_at", "forwarded_at",
			"resolution", "trust_disposition_id", "resolved_at"},
	}
	for table, cols := range want {
		sort.Strings(cols)
		if got := columns(t, table); strings.Join(got, ",") != strings.Join(cols, ",") {
			t.Errorf("%s columns\n  want %v\n  got  %v", table, cols, got)
		}
	}
}

func TestIndexDefinitions(t *testing.T) {
	for name, want := range map[string]string{
		"uq_chat_conversation_direct":          "(direct_user_low_id, direct_user_high_id) WHERE (kind = 'direct'::text)",
		"idx_chat_member_dialogs":              "(user_id, last_message_at DESC, conversation_id DESC) WHERE (left_at IS NULL)",
		"uq_chat_message_seq":                  "(conversation_id, seq)",
		"uq_chat_message_client":               "(sender_id, client_message_id)",
		"idx_chat_message_sender":              "(sender_id)",
		"idx_chat_message_pinned":              "(conversation_id, pinned_at DESC) WHERE (pinned_at IS NOT NULL)",
		"idx_chat_reaction_user":               "(user_id)",
		"idx_chat_update_created":              "(created_at)",
		"uq_chat_report":                       "(reporter_id, message_id)",
		"idx_chat_report_pending":              "(id) WHERE (status = 'pending'::text)",
		"idx_chat_import_message_conversation": "(conversation_id)",
	} {
		var def string
		testDB.Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = ?`, name).Scan(&def)
		if !strings.Contains(def, want) {
			t.Errorf("%s: want %s, got %q", name, want, def)
		}
	}
}

func TestConstraintsHold(t *testing.T) {
	tx := testDB.Begin()
	defer tx.Rollback()
	try := func(sql string) error {
		tx.SavePoint("probe")
		err := tx.Exec(sql).Error
		tx.RollbackTo("probe")
		return err
	}
	if err := tx.Exec(`TRUNCATE chat_conversation RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`INSERT INTO chat_conversation (id, kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
		VALUES (1, 'direct', 1, 2, 1, 's', 0, 2, now(), now())`).Error; err != nil {
		t.Fatal(err)
	}
	for name, probe := range map[string]string{
		"second direct for a pair": `INSERT INTO chat_conversation (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('direct', 1, 2, 2, 's', 0, 2, now(), now())`,
		"direct without users": `INSERT INTO chat_conversation (kind, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('direct', 1, 's', 0, 2, now(), now())`,
		"group with users": `INSERT INTO chat_conversation (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('group', 3, 4, 3, 's', 0, 2, now(), now())`,
		"users out of order": `INSERT INTO chat_conversation (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('direct', 9, 8, 9, 's', 0, 2, now(), now())`,
		"unknown kind": `INSERT INTO chat_conversation (kind, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('channel', 1, 's', 0, 0, now(), now())`,
		"deleted message with text": `INSERT INTO chat_message (conversation_id, seq, sender_id, kind, text, origin_site, silent, deleted_at, created_at)
			VALUES (1, 1, 1, 'message', 'x', 's', false, now(), now())`,
		"service without action": `INSERT INTO chat_message (conversation_id, seq, sender_id, kind, text, origin_site, silent, created_at)
			VALUES (1, 2, 1, 'service', '', 's', false, now())`,
		"message with action": `INSERT INTO chat_message (conversation_id, seq, sender_id, kind, text, service_action, origin_site, silent, created_at)
			VALUES (1, 3, 1, 'message', 'x', '{}', 's', false, now())`,
		"unknown update kind": `INSERT INTO chat_update (user_id, update_seq, kind, conversation_id, data, created_at)
			VALUES (1, 1, 'poke', 1, '{}', now())`,
		"unknown allow": `INSERT INTO chat_user (user_id, last_update_seq, allow_incoming, accept_requests, allow_group_invites, created_at, updated_at)
			VALUES (1, 0, 'friends', true, 'all', now(), now())`,
	} {
		if err := try(probe); err == nil {
			t.Errorf("%s: the schema must refuse it", name)
		}
	}
	if err := try(`INSERT INTO chat_conversation (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
		VALUES ('direct', 5, 5, 5, 's', 0, 1, now(), now())`); err != nil {
		t.Errorf("a conversation with oneself is allowed (saved messages): %v", err)
	}
}

// A conversation removed from under a member who left it earlier would cut
// holes in that member's update stream if the stream cascaded.
func TestUpdateStreamsDoNotCascade(t *testing.T) {
	var n int64
	testDB.Raw(`SELECT count(*) FROM information_schema.table_constraints
		WHERE table_name = 'chat_update' AND constraint_type = 'FOREIGN KEY'`).Scan(&n)
	if n != 0 {
		t.Fatalf("chat_update has %d foreign keys", n)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	if err := Run(testDB); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
