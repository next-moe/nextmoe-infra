package alert

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"api/internal/platform/telemetry/model"
)

type memRepo struct {
	mu       sync.Mutex
	channels []model.AlertChannel
	alerts   []model.Alert
	names    map[int64]string
}

func (m *memRepo) ListEnabledAlertChannels(context.Context) ([]model.AlertChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]model.AlertChannel(nil), m.channels...)
	return out, nil
}

func (m *memRepo) SuppressQueuedAlerts(_ context.Context, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.alerts {
		if m.alerts[i].Status == model.AlertQueued {
			m.alerts[i].Status = model.AlertSuppressed
			m.alerts[i].LastError = reason
		}
	}
	return nil
}

func (m *memRepo) ListQueuedImmediateAlerts(context.Context) ([]model.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Alert
	for _, a := range m.alerts {
		if a.Status == model.AlertQueued && a.Urgency == model.UrgencyImmediate {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memRepo) ListReadyDigestAlerts(_ context.Context, now time.Time, age time.Duration) ([]model.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldest := map[int64]time.Time{}
	for _, a := range m.alerts {
		if a.Status != model.AlertQueued || a.Urgency != model.UrgencyDigest {
			continue
		}
		if t, ok := oldest[a.AppID]; !ok || a.CreatedAt.Before(t) {
			oldest[a.AppID] = a.CreatedAt
		}
	}
	ready := map[int64]struct{}{}
	for id, t := range oldest {
		if now.Sub(t) >= age {
			ready[id] = struct{}{}
		}
	}
	var out []model.Alert
	for _, a := range m.alerts {
		if a.Status == model.AlertQueued && a.Urgency == model.UrgencyDigest {
			if _, ok := ready[a.AppID]; ok {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

func (m *memRepo) MarkAlertsSent(_ context.Context, ids []int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[int64]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	for i := range m.alerts {
		if _, ok := want[m.alerts[i].ID]; ok {
			m.alerts[i].Status = model.AlertSent
			ts := at
			m.alerts[i].SentAt = &ts
			m.alerts[i].LastError = ""
		}
	}
	return nil
}

func (m *memRepo) RecordAlertFailure(_ context.Context, ids []int64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[int64]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	for i := range m.alerts {
		if _, ok := want[m.alerts[i].ID]; !ok {
			continue
		}
		m.alerts[i].Attempts++
		m.alerts[i].LastError = errMsg
		if m.alerts[i].Attempts >= 5 {
			m.alerts[i].Status = model.AlertFailed
		}
	}
	return nil
}

func (m *memRepo) AppServiceNames(context.Context, []int64) (map[int64]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]string{}
	for k, v := range m.names {
		out[k] = v
	}
	return out, nil
}

func (m *memRepo) alert(id int64) model.Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.alerts {
		if a.ID == id {
			return a
		}
	}
	return model.Alert{}
}

type failNote struct {
	mu  sync.Mutex
	err error
	n   int
}

func (f *failNote) Send(context.Context, Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return f.err
}

type recNote struct {
	mu   sync.Mutex
	sent []Notification
}

func (r *recNote) Send(_ context.Context, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return nil
}

func TestDispatcherStatuses(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	ch := []model.AlertChannel{{ID: 1, Kind: model.AlertKindEmail, Target: "ops@example.com", Enabled: true}}
	base := model.Alert{
		ID:         1,
		AppID:      1,
		Rule:       model.RuleCrashRate,
		SubjectKey: "k",
		Urgency:    model.UrgencyImmediate,
		Title:      "崩溃率偏高",
		Facts:      factsJSON(t, map[string]any{"version": "1", "sessions": 200, "crashed": 10, "rate": 0.05, "threshold": 0.01}),
		Status:     model.AlertQueued,
		CreatedAt:  now.Add(-time.Minute),
	}

	t.Run("sent", func(t *testing.T) {
		repo := &memRepo{channels: ch, alerts: []model.Alert{base}, names: map[int64]string{1: "kungal-app"}}
		note := &recNote{}
		d := NewDispatcher(repo, note, true, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		got := repo.alert(1)
		if got.Status != model.AlertSent || got.SentAt == nil {
			t.Fatalf("got %+v", got)
		}
		if len(note.sent) != 1 || note.sent[0].To != "ops@example.com" {
			t.Fatalf("sent=%+v", note.sent)
		}
	})

	t.Run("failure increments", func(t *testing.T) {
		a := base
		repo := &memRepo{channels: ch, alerts: []model.Alert{a}, names: map[int64]string{1: "kungal-app"}}
		note := &failNote{err: errors.New("smtp down")}
		d := NewDispatcher(repo, note, true, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		got := repo.alert(1)
		if got.Attempts != 1 || got.Status != model.AlertQueued {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("fifth failure failed", func(t *testing.T) {
		a := base
		a.Attempts = 4
		repo := &memRepo{channels: ch, alerts: []model.Alert{a}, names: map[int64]string{1: "kungal-app"}}
		note := &failNote{err: errors.New("smtp down")}
		d := NewDispatcher(repo, note, true, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		got := repo.alert(1)
		if got.Attempts != 5 || got.Status != model.AlertFailed {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("no channel", func(t *testing.T) {
		a := base
		repo := &memRepo{channels: nil, alerts: []model.Alert{a}}
		d := NewDispatcher(repo, &recNote{}, true, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		got := repo.alert(1)
		if got.Status != model.AlertSuppressed || got.LastError != "no channel" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("mail not configured", func(t *testing.T) {
		a := base
		repo := &memRepo{channels: ch, alerts: []model.Alert{a}}
		d := NewDispatcher(repo, &recNote{}, false, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		got := repo.alert(1)
		if got.Status != model.AlertSuppressed || got.LastError != "mail not configured" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("digest age", func(t *testing.T) {
		young := model.Alert{
			ID:         2,
			AppID:      2,
			Rule:       model.RuleIssueNew,
			SubjectKey: "y",
			Urgency:    model.UrgencyDigest,
			Title:      "新问题",
			Facts:      factsJSON(t, map[string]any{"issue_id": 1, "kind": "java", "title": "t", "culprit": "c", "first_version": "1"}),
			Status:     model.AlertQueued,
			CreatedAt:  now.Add(-14 * time.Minute),
		}
		old := model.Alert{
			ID:         3,
			AppID:      3,
			Rule:       model.RuleIssueNew,
			SubjectKey: "o",
			Urgency:    model.UrgencyDigest,
			Title:      "新问题",
			Facts:      factsJSON(t, map[string]any{"issue_id": 2, "kind": "java", "title": "t", "culprit": "c", "first_version": "1"}),
			Status:     model.AlertQueued,
			CreatedAt:  now.Add(-15 * time.Minute),
		}
		repo := &memRepo{
			channels: ch,
			alerts:   []model.Alert{young, old},
			names:    map[int64]string{2: "a", 3: "b"},
		}
		note := &recNote{}
		d := NewDispatcher(repo, note, true, "https://admin.nextmoe.dev")
		if err := d.Dispatch(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		if repo.alert(2).Status != model.AlertQueued {
			t.Fatalf("young=%+v", repo.alert(2))
		}
		if repo.alert(3).Status != model.AlertSent {
			t.Fatalf("old=%+v", repo.alert(3))
		}
	})
}
