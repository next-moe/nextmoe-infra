package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
	"api/pkg/trustclient"
)

func TestPurgeAccountErasesTheirMessagesOnly(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	ctx := context.Background()
	c12 := r.direct(t, 1, 2)
	c13 := r.direct(t, 1, 3)
	c23 := r.direct(t, 2, 3)
	r.send(t, 1, c12, "from the leaver")
	keep := r.send(t, 2, c12, "from the one who stays")
	r.send(t, 1, c13, "also leaving")
	r.send(t, 2, c23, "untouched")
	if _, err := r.svc.React(ctx, actor(1), mustID(t, keep.ID), ptr("heart")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Report(ctx, actor(2), mustID(t, r.send(t, 1, c12, "reportable").ID), "spam", nil); err != nil {
		t.Fatal(err)
	}

	if err := r.svc.PurgeAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var live int64
	testDB.Model(&model.ChatMessage{}).Where("sender_id = 1 AND deleted_at IS NULL").Count(&live)
	if live != 0 {
		t.Fatalf("%d of the account's messages survived", live)
	}
	var kept model.ChatMessage
	testDB.Take(&kept, mustID(t, keep.ID))
	if kept.DeletedAt != nil || kept.Text != "from the one who stays" {
		t.Fatal("the other person's messages stay")
	}
	var reactions int64
	testDB.Model(&model.ChatReaction{}).Where("user_id = 1").Count(&reactions)
	if reactions != 0 {
		t.Fatal("the account's reactions go")
	}
	if member(t, c12, 1).LeftAt == nil {
		t.Fatal("the account leaves its conversations")
	}
	view, err := r.svc.Conversation(ctx, actor(2), c12)
	if err != nil {
		t.Fatalf("the conversation stays for the other side: %v", err)
	}
	if view.Conversation.MemberCount != 1 {
		t.Fatalf("member_count = %d", view.Conversation.MemberCount)
	}
	ups := updatesOf(t, 2)
	got := kinds(ups)
	if !strings.Contains(strings.Join(got, ","), model.UpdateDeleteMessages) || !strings.Contains(strings.Join(got, ","), model.UpdateMember) {
		t.Fatalf("the other side hears the deletes and the departure: %v", got)
	}
	var cu, own int64
	testDB.Model(&model.ChatUser{}).Where("user_id = 1").Count(&cu)
	testDB.Model(&model.ChatUpdate{}).Where("user_id = 1").Count(&own)
	if cu != 0 || own != 0 {
		t.Fatal("the account's chat_user and update rows go")
	}
	var report model.ChatReport
	testDB.Take(&report)
	if !strings.Contains(string(report.Snapshot), "erased") {
		t.Fatalf("a report's snapshot of the account's words is erased: %s", report.Snapshot)
	}
	untouched := member(t, c23, 2)
	if untouched.LeftAt != nil {
		t.Fatal("a conversation the account was never in is untouched")
	}
	_, err = r.svc.Send(ctx, actor(2), c12, SendInput{Text: "hello?"})
	var na *NotAcceptingError
	if !errors.As(err, &na) {
		t.Fatalf("messaging an erased account: want NotAcceptingError, got %v", err)
	}
	before := len(updatesOf(t, 2))
	if err := r.svc.PurgeAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(updatesOf(t, 2)) != before {
		t.Fatal("a second purge changes nothing")
	}
}

func TestPurgeRemovesConversationsLeftEmpty(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	r.send(t, 1, conv, "x")
	if err := r.svc.PurgeAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.PurgeAccount(ctx, 2); err != nil {
		t.Fatal(err)
	}
	var n int64
	testDB.Model(&model.ChatConversation{}).Count(&n)
	if n != 0 {
		t.Fatal("a conversation nobody is left in is removed")
	}
}

func TestReportSnapshotsWhatTheReporterSaw(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	for i := 0; i < 12; i++ {
		r.send(t, 1, conv, fmt.Sprintf("m%d", i))
	}
	target := r.send(t, 1, conv, "the bad one")
	r.send(t, 1, conv, "after")
	res, err := r.svc.Report(ctx, actor(2), mustID(t, target.ID), "harassment", ptr("  context please  "))
	if err != nil || !res.Created {
		t.Fatalf("report: %+v %v", res, err)
	}
	var row model.ChatReport
	testDB.Take(&row, res.ReportID)
	snap := decodeJSON[struct {
		Messages []snapshotMessage `json:"messages"`
	}](row.Snapshot)
	if len(snap.Messages) != reportContextMessages+1 {
		t.Fatalf("want the message and %d before it, got %d", reportContextMessages, len(snap.Messages))
	}
	lastSnap := snap.Messages[len(snap.Messages)-1]
	if !lastSnap.Reported || lastSnap.Text != "the bad one" {
		t.Fatalf("the reported message closes the snapshot: %+v", lastSnap)
	}
	if row.Note == nil || *row.Note != "context please" || row.OriginSite != "letmoe" || row.ReportedUserID != 1 {
		t.Fatalf("row: %+v", row)
	}
	again, err := r.svc.Report(ctx, actor(2), mustID(t, target.ID), "spam", nil)
	if err != nil || again.Created || again.ReportID != res.ReportID {
		t.Fatalf("a second report on the same message: %+v %v", again, err)
	}
	_, err = r.svc.Report(ctx, actor(1), mustID(t, target.ID), "spam", nil)
	wantInvalid(t, err)
	_, err = r.svc.Report(ctx, actor(2), mustID(t, target.ID), "boring", nil)
	wantInvalid(t, err)
	if err := r.svc.ClearHistory(ctx, actor(2), conv, false); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.Report(ctx, actor(2), mustID(t, r.send(t, 2, conv, "own").ID)-1, "spam", nil)
	wantErr(t, err, ErrNotFound)
}

type fakeForwarder struct {
	mu    sync.Mutex
	calls []trustclient.ForwardRequest
	err   error
}

func (f *fakeForwarder) Forward(_ context.Context, req trustclient.ForwardRequest) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.err != nil {
		return 0, false, f.err
	}
	return 900 + int64(len(f.calls)), true, nil
}

func TestForwardReports(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	m := r.send(t, 1, conv, "offensive")
	res, _ := r.svc.Report(ctx, actor(2), mustID(t, m.ID), "harassment", nil)

	transient := &fakeForwarder{err: errors.New("connection refused")}
	if n, err := r.svc.ForwardReports(ctx, transient); err != nil || n != 0 {
		t.Fatalf("transient: %d %v", n, err)
	}
	var row model.ChatReport
	testDB.Take(&row, res.ReportID)
	if row.Status != model.ReportPending || row.ForwardAttempts != 1 {
		t.Fatalf("a transient failure stays pending: %+v", row)
	}

	ok := &fakeForwarder{}
	if n, err := r.svc.ForwardReports(ctx, ok); err != nil || n != 1 {
		t.Fatalf("forward: %d %v", n, err)
	}
	call := ok.calls[0]
	if call.Site != "letmoe" || call.SubjectKind != SubjectKind || call.SubjectID != dto.ID(mustID(t, m.ID)) ||
		call.ContextNote == nil || !strings.Contains(*call.ContextNote, "offensive") {
		t.Fatalf("forward request: %+v", call)
	}
	var forwarded model.ChatReport
	testDB.Take(&forwarded, res.ReportID)
	if forwarded.Status != model.ReportForwarded || forwarded.TrustReviewItemID == nil {
		t.Fatalf("forwarded: %+v", forwarded)
	}

	m2 := r.send(t, 1, conv, "again")
	res2, _ := r.svc.Report(ctx, actor(2), mustID(t, m2.ID), "spam", nil)
	permanent := &fakeForwarder{err: &trustclient.StatusError{Path: "/api/v1/trust/forward", Status: 422, Body: "subject kind not registered"}}
	if _, err := r.svc.ForwardReports(ctx, permanent); err != nil {
		t.Fatal(err)
	}
	var failed model.ChatReport
	testDB.Take(&failed, res2.ReportID)
	if failed.Status != model.ReportFailed || failed.ForwardError == nil {
		t.Fatalf("a permanent refusal is not retried: %+v", failed)
	}
	if _, err := r.svc.ForwardReports(ctx, permanent); err != nil {
		t.Fatal(err)
	}
	if len(permanent.calls) != 1 {
		t.Fatal("a failed report is not sent again")
	}
}
