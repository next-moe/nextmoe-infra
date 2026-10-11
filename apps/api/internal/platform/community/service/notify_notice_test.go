package service

import (
	"context"
	"encoding/json"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/notice"

	"gorm.io/gorm"
)

func resetNotices(t *testing.T) {
	t.Helper()
	if err := testDB.AutoMigrate(&notice.Notice{}); err != nil {
		t.Fatalf("migrate platform_notices: %v", err)
	}
	truncate := func() error {
		return testDB.Exec("TRUNCATE platform_notices, platform_notice_cursor RESTART IDENTITY").Error
	}
	if err := truncate(); err != nil {
		t.Fatalf("truncate notices: %v", err)
	}
	t.Cleanup(func() { _ = truncate() })
}

func raiseNotice(t *testing.T, site string, userID int64, ref, title string) {
	t.Helper()
	if err := testDB.Transaction(func(tx *gorm.DB) error {
		return notice.RaiseTx(tx, []notice.Notice{{
			Site: site, UserID: userID, Topic: "shop_restock", Ref: ref, Title: title, URL: "https://www.kungal.com/shop",
		}})
	}); err != nil {
		t.Fatalf("raise notice: %v", err)
	}
}

func deliverNotices(t *testing.T) int {
	t.Helper()
	n, err := NewNotificationService(testDB).WithNoticeSource(testDB).DeliverNotices(context.Background())
	if err != nil {
		t.Fatalf("DeliverNotices: %v", err)
	}
	return n
}

func noticeTitle(t *testing.T, n model.CommunityNotification) string {
	t.Helper()
	var p model.NoticePayload
	if err := json.Unmarshal(n.Notice, &p); err != nil {
		t.Fatalf("notice payload %s: %v", n.Notice, err)
	}
	return p.Title
}

func TestNoticesFoldPerTopicAndRefUntilRead(t *testing.T) {
	cleanTables(t)
	resetNotices(t)
	const user int64 = 7

	if n := deliverNotices(t); n != 0 {
		t.Fatalf("an empty outbox delivered %d", n)
	}
	raiseNotice(t, "kungal", user, "offer:1", "first")
	raiseNotice(t, "kungal", user, "offer:2", "other offer")
	raiseNotice(t, "moyu", user, "offer:1", "elsewhere")
	if n, err := NewNotificationService(testDB).DeliverNotices(context.Background()); err != nil || n != 0 {
		t.Fatalf("a dispatcher with no source delivered %d: %v", n, err)
	}
	if n := deliverNotices(t); n != 3 {
		t.Fatalf("delivered %d, want 3", n)
	}
	rows := notifsOfSite(t, "kungal", user)
	if len(rows) != 2 {
		t.Fatalf("kungal rows: %+v", rows)
	}
	first := rows[0]
	if first.Kind != model.NotificationKindNotice || first.ThreadID != 0 || first.AnchorKind != 0 || first.AnchorID != "" ||
		first.ActorID != nil || first.ItemCount != 1 || noticeTitle(t, first) != "first" {
		t.Fatalf("kind-11 row: %+v", first)
	}

	raiseNotice(t, "kungal", user, "offer:1", "second")
	if n := deliverNotices(t); n != 1 {
		t.Fatalf("delivered %d, want the one new notice", n)
	}
	if n := deliverNotices(t); n != 0 {
		t.Fatalf("the cursor did not move: delivered %d again", n)
	}
	rows = notifsOfSite(t, "kungal", user)
	if len(rows) != 2 || rows[0].ItemCount != 2 || noticeTitle(t, rows[0]) != "second" || rows[0].Seq <= first.Seq {
		t.Fatalf("an unread row of the same ref must fold: %+v", rows)
	}

	if _, _, err := NewNotificationService(testDB).MarkRead("kungal", user, []int64{rows[0].ID}, false); err != nil {
		t.Fatal(err)
	}
	raiseNotice(t, "kungal", user, "offer:1", "third")
	deliverNotices(t)
	if rows = notifsOfSite(t, "kungal", user); len(rows) != 3 || noticeTitle(t, rows[2]) != "third" || rows[2].ItemCount != 1 {
		t.Fatalf("a read row must not fold the next notice: %+v", rows)
	}
	if rows = notifsOfSite(t, "moyu", user); len(rows) != 1 || noticeTitle(t, rows[0]) != "elsewhere" {
		t.Fatalf("moyu rows: %+v", rows)
	}
}
