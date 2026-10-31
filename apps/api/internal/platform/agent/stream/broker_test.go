package stream

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func collect(t *testing.T, b *Broker, id string, after int) []map[string]any {
	t.Helper()
	var out []map[string]any
	err := b.Subscribe(context.Background(), id, after, func(f Frame) error {
		var m map[string]any
		if err := json.Unmarshal(f.Data, &m); err != nil {
			return err
		}
		out = append(out, m)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBrokerCoalescesDeltasAndReplaysFromAnySeq(t *testing.T) {
	b := NewBroker()
	clock := time.Unix(100, 0)
	b.now = func() time.Time { return clock }
	w := b.Open("r1")
	w.Emit(map[string]any{"type": "start"})
	w.Delta("text-delta", "t1", "he")
	w.Delta("text-delta", "t1", "llo")
	clock = clock.Add(coalesceEvery)
	w.Delta("text-delta", "t1", "!")
	w.Emit(map[string]any{"type": "finish"})
	w.Close()

	all := collect(t, b, "r1", 0)
	var text strings.Builder
	for _, f := range all {
		if f["type"] == "text-delta" {
			text.WriteString(f["delta"].(string))
		}
	}
	if text.String() != "hello!" || all[0]["type"] != "start" || all[len(all)-1]["type"] != "finish" {
		t.Fatalf("frames = %v", all)
	}
	if len(all) > 4 {
		t.Fatalf("deltas inside one coalescing window must merge, got %d frames", len(all))
	}
	if tail := collect(t, b, "r1", len(all)-1); len(tail) != 1 || tail[0]["type"] != "finish" {
		t.Fatalf("Last-Event-ID resume = %v", tail)
	}
}

func TestBrokerFollowsALiveRunUntilItCloses(t *testing.T) {
	b := NewBroker()
	w := b.Open("r2")
	got := make(chan int, 1)
	go func() {
		n := 0
		_ = b.Subscribe(context.Background(), "r2", 0, func(Frame) error { n++; return nil })
		got <- n
	}()
	time.Sleep(10 * time.Millisecond)
	w.Emit(map[string]any{"type": "start"})
	w.Emit(map[string]any{"type": "finish"})
	w.Close()
	select {
	case n := <-got:
		if n != 2 {
			t.Fatalf("subscriber saw %d frames, want 2", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not return after the run closed")
	}
	if b.Active("r2") {
		t.Fatal("a closed run is not active")
	}
	if err := b.Subscribe(context.Background(), "nope", 0, func(Frame) error { return nil }); err != ErrUnknownRun {
		t.Fatalf("unknown run err = %v", err)
	}
}
