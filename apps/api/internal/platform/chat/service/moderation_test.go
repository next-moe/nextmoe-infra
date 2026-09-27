package service

import (
	"context"
	"slices"
	"testing"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
	"api/pkg/trustclient"
)

func disposition(messageID int64, action int16) trustclient.Callback {
	return trustclient.Callback{DispositionID: 77, SubjectKind: SubjectKind, SubjectID: dto.ID(messageID), Action: action, ReasonCode: "harassment"}
}

func reportRow(t *testing.T, id int64) model.ChatReport {
	t.Helper()
	var row model.ChatReport
	if err := testDB.Take(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestDispositionRemoveErasesForEveryone(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	bad := r.send(t, 1, conv, "offensive")
	badID := mustID(t, bad.ID)
	reply, err := r.svc.Send(ctx, actor(2), conv, SendInput{Text: "what?", ReplyToSeq: ptr(bad.Seq), ReplyQuote: &QuoteInput{Offset: 0, Text: "offensive"}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := r.svc.Report(ctx, actor(2), badID, "harassment", nil)
	r.pub.take()

	for _, action := range []int16{trustclient.ActionRemove, trustclient.ActionHide} {
		res, err := r.svc.ApplyDisposition(ctx, disposition(badID, action))
		if err != nil || res != DispositionApplied {
			t.Fatalf("action %d: %v %v", action, res, err)
		}
	}
	var msg model.ChatMessage
	testDB.Take(&msg, badID)
	if msg.DeletedAt == nil || msg.Text != "" {
		t.Fatalf("the message must be erased: %+v", msg)
	}
	var quoted model.ChatMessage
	testDB.Where("conversation_id = ? AND seq = ?", conv, reply.Message.Seq).Take(&quoted)
	if quoted.ReplyQuote != nil {
		t.Fatal("the quote of the erased message must go with it")
	}
	for _, uid := range []int64{1, 2} {
		got := kinds(updatesOf(t, uid))
		if slices.Index(got, model.UpdateDeleteMessages) < 0 || slices.Index(got, model.UpdateEditMessage) < 0 {
			t.Fatalf("user %d updates %v", uid, got)
		}
		if n := len(slices.DeleteFunc(slices.Clone(got), func(k string) bool { return k != model.UpdateDeleteMessages })); n != 1 {
			t.Fatalf("user %d: the second disposition must not erase again, updates %v", uid, got)
		}
	}
	if len(r.pub.take()) == 0 {
		t.Fatal("the erase must be pushed")
	}
	row := reportRow(t, first.ReportID)
	if row.Resolution == nil || *row.Resolution != model.ResolutionRemoved || row.TrustDispositionID == nil || *row.TrustDispositionID != 77 || row.ResolvedAt == nil {
		t.Fatalf("report: %+v", row)
	}
	if m := member(t, conv, 2); m.UnreadCount != 0 {
		t.Fatalf("an erased message is not unread: %d", m.UnreadCount)
	}
}

func TestDispositionNoneKeepsTheMessage(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	m := r.send(t, 1, conv, "fine")
	id := mustID(t, m.ID)
	rep, _ := r.svc.Report(ctx, actor(2), id, "spam", nil)

	if res, err := r.svc.ApplyDisposition(ctx, disposition(id, trustclient.ActionNone)); err != nil || res != DispositionApplied {
		t.Fatalf("%v %v", res, err)
	}
	var msg model.ChatMessage
	testDB.Take(&msg, id)
	if msg.DeletedAt != nil || msg.Text != "fine" {
		t.Fatalf("a dismissed report leaves the message: %+v", msg)
	}
	row := reportRow(t, rep.ReportID)
	if row.Resolution == nil || *row.Resolution != model.ResolutionDismissed {
		t.Fatalf("report: %+v", row)
	}
	if n, err := r.svc.ForwardReports(ctx, &fakeForwarder{}); err != nil || n != 0 {
		t.Fatalf("a resolved report is never forwarded: %d %v", n, err)
	}
}

func TestDispositionUnsupported(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	id := mustID(t, r.send(t, 1, conv, "x").ID)
	for name, cb := range map[string]trustclient.Callback{
		"other kind":    {SubjectKind: "community_post", SubjectID: dto.ID(id), Action: trustclient.ActionRemove},
		"other action":  {SubjectKind: SubjectKind, SubjectID: dto.ID(id), Action: 9},
		"non-numeric":   {SubjectKind: SubjectKind, SubjectID: "abc", Action: trustclient.ActionRemove},
		"unknown id ok": {SubjectKind: SubjectKind, SubjectID: "999999999", Action: trustclient.ActionRemove},
	} {
		res, err := r.svc.ApplyDisposition(ctx, cb)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := DispositionUnsupported; name == "unknown id ok" {
			if res != DispositionApplied {
				t.Fatalf("%s: a message that is gone has nothing left to do: %v", name, res)
			}
		} else if res != want {
			t.Fatalf("%s: %v", name, res)
		}
	}
	var msg model.ChatMessage
	testDB.Take(&msg, id)
	if msg.DeletedAt != nil {
		t.Fatal("an unsupported disposition must not touch the message")
	}
}

func TestDispositionAfterTheSenderDeleted(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	m := r.send(t, 1, conv, "gone soon")
	id := mustID(t, m.ID)
	rep, _ := r.svc.Report(ctx, actor(2), id, "spam", nil)
	if _, err := r.svc.DeleteMessages(ctx, actor(1), conv, []int64{m.Seq}, true); err != nil {
		t.Fatal(err)
	}
	before := len(updatesOf(t, 2))
	if res, err := r.svc.ApplyDisposition(ctx, disposition(id, trustclient.ActionRemove)); err != nil || res != DispositionApplied {
		t.Fatalf("%v %v", res, err)
	}
	if after := len(updatesOf(t, 2)); after != before {
		t.Fatalf("nothing left to erase, yet %d updates were added", after-before)
	}
	if row := reportRow(t, rep.ReportID); row.Resolution == nil || *row.Resolution != model.ResolutionRemoved {
		t.Fatalf("report: %+v", row)
	}
}

func TestRemovalOutranksAnEarlierOrLaterDismissal(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	m := r.send(t, 1, conv, "borderline")
	id := mustID(t, m.ID)
	rep, _ := r.svc.Report(ctx, actor(2), id, "spam", nil)

	steps := []struct {
		action int16
		want   string
	}{
		{trustclient.ActionNone, model.ResolutionDismissed},
		{trustclient.ActionRemove, model.ResolutionRemoved},
		{trustclient.ActionNone, model.ResolutionRemoved},
	}
	for i, st := range steps {
		cb := disposition(id, st.action)
		cb.DispositionID = int64(100 + i)
		if _, err := r.svc.ApplyDisposition(ctx, cb); err != nil {
			t.Fatal(err)
		}
		if row := reportRow(t, rep.ReportID); row.Resolution == nil || *row.Resolution != st.want {
			t.Fatalf("step %d: want %s, got %+v", i, st.want, row.Resolution)
		}
	}
	if row := reportRow(t, rep.ReportID); *row.TrustDispositionID != 101 {
		t.Fatalf("the removal's disposition is the one on record: %d", *row.TrustDispositionID)
	}
}
