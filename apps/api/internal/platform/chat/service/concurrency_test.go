package service

import (
	"context"
	"sort"
	"sync"
	"testing"

	"api/internal/platform/chat/model"
)

func TestConcurrentSendsKeepEveryStreamGapless(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	r.svc.counter = nil
	ctx := context.Background()
	c12 := r.direct(t, 1, 2)
	c13 := r.direct(t, 1, 3)
	c23 := r.direct(t, 2, 3)
	convs := []struct {
		id     int64
		sender int64
	}{{c12, 1}, {c12, 2}, {c13, 3}, {c13, 1}, {c23, 2}, {c23, 3}}

	const perSender = 15
	var wg sync.WaitGroup
	errs := make(chan error, len(convs)*perSender)
	for _, c := range convs {
		wg.Add(1)
		go func(conv, sender int64) {
			defer wg.Done()
			for i := 0; i < perSender; i++ {
				if _, err := r.svc.Send(ctx, actor(sender), conv, SendInput{Text: "x"}); err != nil {
					errs <- err
				}
			}
		}(c.id, c.sender)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}

	for _, conv := range []int64{c12, c13, c23} {
		var seqs []int64
		testDB.Model(&model.ChatMessage{}).Where("conversation_id = ?", conv).Order("seq").Pluck("seq", &seqs)
		if len(seqs) != 2*perSender {
			t.Fatalf("conversation %d: %d messages", conv, len(seqs))
		}
		for i, s := range seqs {
			if s != int64(i+1) {
				t.Fatalf("conversation %d: seqs not gapless: %v", conv, seqs)
			}
		}
	}
	for _, uid := range []int64{1, 2, 3} {
		ups := updatesOf(t, uid)
		if len(ups) != 4*perSender {
			t.Fatalf("user %d: %d updates", uid, len(ups))
		}
		got := make([]int64, len(ups))
		for i, u := range ups {
			got[i] = u.UpdateSeq
		}
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		for i, s := range got {
			if s != int64(i+1) {
				t.Fatalf("user %d: update stream not gapless: %v", uid, got)
			}
		}
		var cu model.ChatUser
		testDB.Where("user_id = ?", uid).Take(&cu)
		if cu.LastUpdateSeq != int64(len(ups)) {
			t.Fatalf("user %d: last_update_seq %d for %d updates", uid, cu.LastUpdateSeq, len(ups))
		}
	}
}
