package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/migrate"
	"api/internal/platform/chat/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var dsn string

func TestMain(m *testing.M) {
	d, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/import-chat")
	}
	dsn = d
	os.Exit(m.Run())
}

func openSchema(t *testing.T, schema string) *gorm.DB {
	t.Helper()
	d := dsn
	if schema != "" {
		d += " search_path=" + schema
	}
	db, err := gorm.Open(postgres.Open(d), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func must(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", strings.SplitN(sql, "\n", 2)[0], err)
	}
}

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hashS = "5555555555555555555555555555555555555555555555555555555555555555"

// Legacy shapes cut down to the columns the importer reads.
func seedLegacy(t *testing.T) (chat, main, kungal, moyu *gorm.DB) {
	t.Helper()
	root := openSchema(t, "")
	for _, s := range []string{"legacy_kg", "legacy_my", "legacy_main"} {
		must(t, root, "DROP SCHEMA IF EXISTS "+s+" CASCADE")
		must(t, root, "CREATE SCHEMA "+s)
	}
	chat = openSchema(t, "public")
	if err := migrate.Run(chat); err != nil {
		t.Fatal(err)
	}
	must(t, chat, `TRUNCATE chat_import_message, chat_report, chat_update, chat_hidden_message, chat_reaction, chat_message,
		chat_member, chat_conversation, chat_user RESTART IDENTITY CASCADE`)

	main = openSchema(t, "legacy_main")
	must(t, main, `CREATE TABLE users (id int PRIMARY KEY, anonymized_at timestamptz, deleted_at timestamptz)`)
	must(t, main, `INSERT INTO users (id, anonymized_at) VALUES (1, NULL), (2, NULL), (3, NULL), (4, now())`)

	kungal = openSchema(t, "legacy_kg")
	must(t, kungal, `CREATE TABLE chat_room (id int PRIMARY KEY, created timestamptz)`)
	must(t, kungal, `CREATE TABLE chat_room_participant (chat_room_id int, user_id int)`)
	must(t, kungal, `CREATE TABLE chat_message (id int PRIMARY KEY, chat_room_id int, sender_id int, content varchar,
		is_recall boolean, edit_time timestamptz, created timestamptz)`)
	must(t, kungal, `CREATE TABLE chat_message_read_by (chat_message_id int, user_id int)`)
	must(t, kungal, `INSERT INTO chat_room VALUES (10, '2025-01-01'), (11, '2025-01-01'), (12, '2025-01-01')`)
	must(t, kungal, `INSERT INTO chat_room_participant VALUES (10, 1), (10, 2), (11, 1), (11, 4), (12, 3)`)
	must(t, kungal, `INSERT INTO chat_message VALUES
		(100, 10, 1, 'hi **2**', false, NULL, '2025-01-01 10:00'),
		(101, 10, 2, '![image.png](/image/`+hashA+`) look ![sticker](/image/`+hashS+`)', false, NULL, '2025-01-01 10:02'),
		(102, 10, 1, 'regret', true, NULL, '2025-01-01 10:04'),
		(103, 10, 2, 'unread for 1', false, NULL, '2025-01-01 10:06'),
		(110, 11, 1, 'to a deleted account', false, NULL, '2025-01-01 10:00')`)
	must(t, kungal, `INSERT INTO chat_message_read_by VALUES (100, 2), (101, 1)`)

	moyu = openSchema(t, "legacy_my")
	must(t, moyu, `CREATE TABLE chat_room (id int PRIMARY KEY, created timestamptz, type text)`)
	must(t, moyu, `CREATE TABLE chat_member (chat_room_id int, user_id int)`)
	must(t, moyu, `CREATE TABLE chat_message (id int PRIMARY KEY, chat_room_id int, sender_id int, content varchar, status text,
		deleted_at timestamptz, reply_to_id int, created timestamptz, updated timestamptz)`)
	must(t, moyu, `CREATE TABLE chat_message_reaction (id serial, chat_message_id int, user_id int, emoji text, created timestamptz)`)
	must(t, moyu, `INSERT INTO chat_room VALUES (20, '2024-12-31', 'PRIVATE'), (21, '2024-12-31', 'GROUP')`)
	must(t, moyu, `INSERT INTO chat_member VALUES (20, 2), (20, 1), (21, 1), (21, 2), (21, 3)`)
	must(t, moyu, `INSERT INTO chat_message VALUES
		(200, 20, 2, 'from moyu, between the kungal ones', 'EDITED', NULL, NULL, '2025-01-01 10:01', '2025-01-01 10:05'),
		(201, 20, 1, 'replying', 'SENT', NULL, 200, '2025-01-01 10:03', '2025-01-01 10:03'),
		(210, 21, 1, 'group message, not imported yet', 'SENT', NULL, NULL, '2025-01-01 10:00', '2025-01-01 10:00')`)
	must(t, moyu, `INSERT INTO chat_message_reaction (chat_message_id, user_id, emoji, created) VALUES (201, 2, '❤️', now()), (201, 2, '🦄', now())`)
	return chat, main, kungal, moyu
}

func fakeRehost(refs map[imageRef]bool) (resolvedImages, int) {
	out := resolvedImages{}
	for ref := range refs {
		h := ref.Hash
		if !ref.Sticker {
			h = strings.Repeat("c", 64)
		}
		out[ref] = dto.Media{Type: "photo", ImageHash: h, Width: 8, Height: 6}
	}
	return out, 0
}

func TestRunImportsBothSitesIntoOneConversation(t *testing.T) {
	chat, main, kungal, moyu := seedLegacy(t)
	ctx := context.Background()
	d := deps{Chat: chat, Main: main, Kungal: kungal, Moyu: moyu, Rehost: fakeRehost}

	dry, err := run(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	chat.Model(&model.ChatMessage{}).Count(&n)
	if n != 0 {
		t.Fatal("a dry run writes nothing")
	}
	if dry.Stats.Pairs != 1 || dry.Stats.MergedPairs != 1 || dry.Stats.SkippedDeletedUsers != 1 || dry.Stats.SkippedRooms != 1 || dry.Images != 2 {
		t.Fatalf("plan: %+v images %d", dry.Stats, dry.Images)
	}

	d.Apply = true
	rep, err := run(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 1 || rep.Failed != 0 {
		t.Fatalf("report: %+v", *rep)
	}
	var msgs []model.ChatMessage
	chat.Order("seq").Find(&msgs)
	var texts []string
	for _, m := range msgs {
		switch {
		case m.DeletedAt != nil:
			texts = append(texts, "<deleted>")
		case len(m.Media) > 0:
			var md dto.Media
			if err := json.Unmarshal(m.Media, &md); err != nil {
				t.Fatal(err)
			}
			texts = append(texts, "<photo:"+md.ImageHash[:1]+">"+m.Text)
		default:
			texts = append(texts, m.Text)
		}
	}
	want := []string{"hi 2", "from moyu, between the kungal ones", "<photo:c>look", "<photo:5>", "replying", "<deleted>", "unread for 1"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("interleaved history:\n  got  %q\n  want %q", texts, want)
	}
	if msgs[2].MediaGroupID == nil || msgs[3].MediaGroupID == nil || *msgs[2].MediaGroupID != *msgs[3].MediaGroupID {
		t.Fatal("two photos from one old message form an album")
	}
	if msgs[4].ReplyToSeq == nil || *msgs[4].ReplyToSeq != 2 {
		t.Fatalf("moyu reply maps to the new seq: %v", msgs[4].ReplyToSeq)
	}
	if msgs[1].EditedAt == nil {
		t.Fatal("moyu EDITED keeps its edited mark")
	}
	var reactions []model.ChatReaction
	chat.Find(&reactions)
	if len(reactions) != 1 || reactions[0].Reaction != "heart" {
		t.Fatalf("reactions map to keys, unknown ones drop: %+v", reactions)
	}
	var m1, m2 model.ChatMember
	chat.Where("user_id = 1").Take(&m1)
	chat.Where("user_id = 2").Take(&m2)
	if m1.LastReadSeq != 6 || m1.UnreadCount != 1 {
		t.Fatalf("user 1: read %d unread %d", m1.LastReadSeq, m1.UnreadCount)
	}
	if m2.LastReadSeq != 7 || m2.UnreadCount != 0 {
		t.Fatalf("user 2: read %d unread %d", m2.LastReadSeq, m2.UnreadCount)
	}
	var ups int64
	chat.Model(&model.ChatUpdate{}).Count(&ups)
	if ups != 0 {
		t.Fatal("a first import writes no updates")
	}

	must(t, kungal, `INSERT INTO chat_message VALUES (104, 10, 1, 'sent on kungal after the import', false, NULL, '2025-01-02 09:00')`)
	again, err := run(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created != 0 || again.Imported != 1 || again.Skipped != len(want) {
		t.Fatalf("sweep: %+v", *again)
	}
	chat.Model(&model.ChatUpdate{}).Count(&ups)
	if ups != 2 {
		t.Fatalf("the swept message is announced to both people, got %d updates", ups)
	}
}
