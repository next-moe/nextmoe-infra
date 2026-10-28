package stream

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	retention     = 2 * time.Minute
	coalesceEvery = 50 * time.Millisecond
	maxFrames     = 20000
)

var ErrUnknownRun = errors.New("agent stream: unknown run")

type Frame struct {
	Seq  int
	Data []byte
}

type run struct {
	mu       sync.Mutex
	frames   []Frame
	done     bool
	notify   chan struct{}
	finished time.Time
}

type Broker struct {
	mu   sync.Mutex
	runs map[string]*run
	now  func() time.Time
}

func NewBroker() *Broker {
	return &Broker{runs: map[string]*run{}, now: time.Now}
}

func (b *Broker) Open(runID string) *Writer {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	r := &run{notify: make(chan struct{})}
	b.runs[runID] = r
	return &Writer{run: r, now: b.now}
}

func (b *Broker) Known(runID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runs[runID] != nil
}

func (b *Broker) Active(runID string) bool {
	b.mu.Lock()
	r := b.runs[runID]
	b.mu.Unlock()
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.done
}

func (b *Broker) sweepLocked() {
	cutoff := b.now().Add(-retention)
	for id, r := range b.runs {
		r.mu.Lock()
		stale := r.done && r.finished.Before(cutoff)
		r.mu.Unlock()
		if stale {
			delete(b.runs, id)
		}
	}
}

func (b *Broker) Subscribe(ctx context.Context, runID string, after int, send func(Frame) error) error {
	b.mu.Lock()
	r := b.runs[runID]
	b.mu.Unlock()
	if r == nil {
		return ErrUnknownRun
	}
	next := after
	for {
		r.mu.Lock()
		pending := make([]Frame, 0, len(r.frames))
		for _, f := range r.frames {
			if f.Seq > next {
				pending = append(pending, f)
			}
		}
		done, wait := r.done, r.notify
		r.mu.Unlock()
		for _, f := range pending {
			if err := send(f); err != nil {
				return err
			}
			next = f.Seq
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
}

type Writer struct {
	run     *run
	now     func() time.Time
	mu      sync.Mutex
	pending map[string]*delta
	order   []string
	flushed time.Time
}

type delta struct {
	kind, id string
	text     []byte
}

func (w *Writer) append(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	r := w.run
	r.mu.Lock()
	if !r.done && len(r.frames) < maxFrames {
		r.frames = append(r.frames, Frame{Seq: len(r.frames) + 1, Data: data})
		close(r.notify)
		r.notify = make(chan struct{})
	}
	r.mu.Unlock()
}

func (w *Writer) flushLocked() {
	for _, key := range w.order {
		d := w.pending[key]
		w.append(map[string]any{"type": d.kind, "id": d.id, "delta": string(d.text)})
	}
	w.pending, w.order = nil, nil
	w.flushed = w.now()
}

func (w *Writer) Delta(kind, id, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil {
		w.pending = map[string]*delta{}
	}
	key := kind + "\x00" + id
	d := w.pending[key]
	if d == nil {
		d = &delta{kind: kind, id: id}
		w.pending[key] = d
		w.order = append(w.order, key)
	}
	d.text = append(d.text, text...)
	if w.now().Sub(w.flushed) >= coalesceEvery {
		w.flushLocked()
	}
}

func (w *Writer) Emit(v any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
	w.append(v)
}

func (w *Writer) Close() {
	w.mu.Lock()
	w.flushLocked()
	w.mu.Unlock()
	r := w.run
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.finished = w.now()
	close(r.notify)
	r.notify = make(chan struct{})
}
