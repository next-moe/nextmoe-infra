package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func TestReplyPreviewHidesWhatTheViewerHid(t *testing.T) {
	for _, how := range []string{"hide", "clear"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t, 1, 2)
			conv := r.direct(t, 1, 2)
			ctx := context.Background()
			secret := r.send(t, 1, conv, "top secret")
			if how == "hide" {
				if _, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{secret.Seq}, false); err != nil {
					t.Fatal(err)
				}
			} else if err := r.svc.ClearHistory(ctx, actor(2), conv, false); err != nil {
				t.Fatal(err)
			}
			r.pub.take()
			if _, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "about that", ReplyToSeq: &secret.Seq}); err != nil {
				t.Fatal(err)
			}
			for _, d := range r.pub.take() {
				e := d.Data.(*Event)
				if e.Message == nil || e.Message.ReplyTo == nil {
					t.Fatalf("push to %d lacks a reply header", d.UserID)
				}
				leaked := e.Message.ReplyTo.Text != ""
				if leaked == (d.UserID == 2) {
					t.Fatalf("push to %d: reply text %q", d.UserID, e.Message.ReplyTo.Text)
				}
			}
			page, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{})
			last := page.Messages[len(page.Messages)-1]
			if last.ReplyTo == nil || last.ReplyTo.Text != "" || !last.ReplyTo.Deleted {
				t.Fatalf("B's page shows the hidden text: %+v", last.ReplyTo)
			}
			list, _ := r.svc.ListConversations(ctx, actor(2), FolderInbox, "", 10)
			if lm := list.Conversations[0].LastMessage; lm.ReplyTo.Text != "" {
				t.Fatalf("the list shows the hidden text: %+v", lm.ReplyTo)
			}
			own, _ := r.svc.Messages(ctx, actor(1), conv, MessageQuery{})
			if own.Messages[len(own.Messages)-1].ReplyTo.Text != "top secret" {
				t.Fatal("the sender still sees what they replied to")
			}
		})
	}
}

func TestEditIsNotPushedToWhoHidTheMessage(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m := r.send(t, 1, conv, "original")
	if _, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{m.Seq}, false); err != nil {
		t.Fatal(err)
	}
	r.pub.take()
	if _, err := r.svc.Edit(ctx, actor(1), mustID(t, m.ID), "edited after you hid it", nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range r.pub.take() {
		e := d.Data.(*Event)
		if d.UserID == 2 && e.Message != nil {
			t.Fatalf("the edit reached the member who hid it: %q", e.Message.Text)
		}
		if d.UserID == 1 && e.Message == nil {
			t.Fatal("the sender's own devices get the edit")
		}
	}
}

func TestPinningObeysRequestsBlocksAndLimits(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	pending, _ := dto.ParseID(res.Conversation.ID)
	m := r.send(t, 1, pending, "hello")
	wantErr(t, r.svc.SetPinned(ctx, actor(1), pending, m.Seq, true), ErrRequestLimit)
	wantErr(t, r.svc.SetPinned(ctx, actor(2), pending, m.Seq, true), ErrRequestLimit)

	open := r.direct(t, 1, 3)
	msg := r.send(t, 1, open, "pin me")
	r.block(3, 1)
	wantErr(t, r.svc.SetPinned(ctx, actor(1), open, msg.Seq, true), ErrBlocked)
	r.rel.blocks = map[[2]int64]bool{}
	r.advance(time.Minute)
	for i := 0; i < pinsPerMinute/2; i++ {
		if err := r.svc.SetPinned(ctx, actor(1), open, msg.Seq, true); err != nil {
			t.Fatal(err)
		}
		if err := r.svc.SetPinned(ctx, actor(1), open, msg.Seq, false); err != nil {
			t.Fatal(err)
		}
	}
	var rl *RateLimitError
	if err := r.svc.SetPinned(ctx, actor(1), open, msg.Seq, true); !errors.As(err, &rl) {
		t.Fatalf("pins are rate limited, got %v", err)
	}
}

func TestErasedWordsLeaveQuotesToo(t *testing.T) {
	for _, how := range []string{"delete", "purge"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t, 1, 2)
			conv := r.direct(t, 1, 2)
			ctx := context.Background()
			orig := r.send(t, 1, conv, "call me at 555-1234")
			reply, err := r.svc.Send(ctx, actor(2), conv, SendInput{Text: "ok", ReplyToSeq: &orig.Seq, ReplyQuote: &QuoteInput{Text: "555-1234", Offset: 11}})
			if err != nil {
				t.Fatal(err)
			}
			if how == "delete" {
				if _, err := r.svc.DeleteMessages(ctx, actor(1), conv, []int64{orig.Seq}, true); err != nil {
					t.Fatal(err)
				}
			} else if err := r.svc.PurgeAccount(ctx, 1); err != nil {
				t.Fatal(err)
			}
			var row model.ChatMessage
			testDB.Take(&row, mustID(t, reply.Message.ID))
			if len(row.ReplyQuote) != 0 && string(row.ReplyQuote) != "null" {
				t.Fatalf("the quote of erased words survived: %s", row.ReplyQuote)
			}
			found := false
			for _, u := range updatesOf(t, 2) {
				if u.Kind == model.UpdateEditMessage && decodeData(t, u)["seq"] == float64(reply.Message.Seq) {
					found = true
				}
			}
			if !found {
				t.Fatal("clients holding the reply must learn it changed")
			}
		})
	}
}

func TestPurgeScrubsTheAccountFromEveryReport(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	ctx := context.Background()
	c12 := r.direct(t, 1, 2)
	r.send(t, 1, c12, "the leaver's context line")
	bad := r.send(t, 2, c12, "reported line")
	if _, err := r.svc.Report(ctx, actor(1), mustID(t, bad.ID), "spam", ptr("the leaver's note")); err != nil {
		t.Fatal(err)
	}
	c23 := r.direct(t, 2, 3)
	bad3 := r.send(t, 3, c23, "another")
	r.send(t, 2, c23, "not about the leaver")
	if _, err := r.svc.Report(ctx, actor(2), mustID(t, bad3.ID), "spam", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.PurgeAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var reports []model.ChatReport
	testDB.Order("id").Find(&reports)
	if strings.Contains(string(reports[0].Snapshot), "context line") || reports[0].Note != nil {
		t.Fatalf("the leaver's own report keeps their words: %s %v", reports[0].Snapshot, reports[0].Note)
	}
	if !strings.Contains(string(reports[1].Snapshot), "another") {
		t.Fatal("a report the leaver had nothing to do with is untouched")
	}
}

func TestReportSnapshotHasTheTargetEvenIfHidden(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "before")
	bad := r.send(t, 1, conv, "abuse")
	if _, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{bad.Seq}, false); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Report(ctx, actor(2), mustID(t, bad.ID), "harassment", nil)
	if err != nil {
		t.Fatal(err)
	}
	var row model.ChatReport
	testDB.Take(&row, res.ReportID)
	if !strings.Contains(string(row.Snapshot), "abuse") || !strings.Contains(string(row.Snapshot), `"reported": true`) {
		t.Fatalf("snapshot must hold the reported message: %s", row.Snapshot)
	}
}

func TestReadingAPurgedAccountsMessagesDoesNotRevive(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "a")
	if err := r.svc.PurgeAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Read(ctx, actor(2), conv, 99); err != nil {
		t.Fatal(err)
	}
	var n int64
	testDB.Model(&model.ChatUser{}).Where("user_id = 1").Count(&n)
	if n != 0 || len(updatesOf(t, 1)) != 0 {
		t.Fatal("a read must not recreate the purged account's rows")
	}
}

func TestPinnedDialogCapHoldsUnderConcurrency(t *testing.T) {
	r := newRig(t, 1)
	ctx := context.Background()
	var convs []int64
	for peer := int64(2); peer <= 16; peer++ {
		r.users.m[peer] = Profile{ID: peer, CreatedAt: old}
		convs = append(convs, r.direct(t, 1, peer))
	}
	for _, c := range convs[:4] {
		if _, err := r.svc.UpdateDialog(ctx, actor(1), c, DialogPatch{Pinned: ptr(true)}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, c := range convs[4:] {
		wg.Add(1)
		go func(c int64) {
			defer wg.Done()
			_, _ = r.svc.UpdateDialog(ctx, actor(1), c, DialogPatch{Pinned: ptr(true)})
		}(c)
	}
	wg.Wait()
	var ranks []int16
	testDB.Model(&model.ChatMember{}).Where("user_id = 1 AND pinned_rank IS NOT NULL").Pluck("pinned_rank", &ranks)
	seen := map[int16]bool{}
	for _, rk := range ranks {
		if seen[rk] {
			t.Fatalf("duplicate rank %d in %v", rk, ranks)
		}
		seen[rk] = true
	}
	if len(ranks) != maxPinnedDialogs {
		t.Fatalf("want exactly %d pinned, got %d", maxPinnedDialogs, len(ranks))
	}
}

func TestTypingCarriesStringIDsAndStopsAtABlock(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.pub.take()
	if err := r.svc.Typing(ctx, actor(1), conv); err != nil {
		t.Fatal(err)
	}
	got := r.pub.take()
	ev := got[0].Data.(map[string]any)
	if ev["conversation_id"] != dto.ID(conv) || ev["user_id"] != "1" {
		t.Fatalf("typing ids must be strings: %#v", ev)
	}
	r.block(2, 1)
	r.advance(typingWindow)
	if err := r.svc.Typing(ctx, actor(1), conv); err != nil {
		t.Fatal(err)
	}
	if len(r.pub.take()) != 0 {
		t.Fatal("no typing across a block")
	}
}

func TestBlocksStopEditsAndReactions(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	mine := r.send(t, 1, conv, "mine")
	theirs := r.send(t, 2, conv, "theirs")
	r.block(2, 1)
	_, err := r.svc.Edit(ctx, actor(1), mustID(t, mine.ID), "edited", nil)
	wantErr(t, err, ErrBlocked)
	_, err = r.svc.React(ctx, actor(1), mustID(t, theirs.ID), ptr("heart"))
	wantErr(t, err, ErrBlocked)
}

func TestPendingRequestTakesNoContextCard(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	_, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "hi", Context: &ContextInput{Kind: "patch", ID: "1", Title: "t", URL: "https://letmoe.example/p"}})
	wantErr(t, err, ErrRequestLimit)
}

func TestPinnedSeqsSkipWhatTheViewerHid(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m := r.send(t, 1, conv, "pinned")
	if err := r.svc.SetPinned(ctx, actor(1), conv, m.Seq, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{m.Seq}, false); err != nil {
		t.Fatal(err)
	}
	view, _ := r.svc.Conversation(ctx, actor(2), conv)
	if len(view.Conversation.PinnedSeqs) != 0 {
		t.Fatalf("pinned_seqs lists a message the viewer hid: %v", view.Conversation.PinnedSeqs)
	}
	if len(view.Conversation.PinnedMessages) != 0 {
		t.Fatalf("pinned_messages shows a message the viewer hid: %+v", view.Conversation.PinnedMessages)
	}
}
