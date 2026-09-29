package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGradeWithQuotaWaitsForTheAllocationToFree(t *testing.T) {
	g := &quotaGate{wait: time.Minute, probe: time.Millisecond}
	calls := 0
	err := gradeWithQuota(t.Context(), g, func() error {
		calls++
		if calls < 3 {
			return errDailyQuota
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err = %v calls = %d, want success on the third ask", err, calls)
	}
	if g.exhausted() {
		t.Fatal("a freed allocation must not leave the gate exhausted")
	}
}

func TestGradeWithQuotaCarriesOnceTheWaitRunsOut(t *testing.T) {
	g := &quotaGate{wait: 20 * time.Millisecond, probe: 5 * time.Millisecond}
	err := gradeWithQuota(t.Context(), g, func() error { return errDailyQuota })
	if !errors.Is(err, errDailyQuota) || !g.exhausted() {
		t.Fatalf("err = %v exhausted = %v, want the quota error and an exhausted gate", err, g.exhausted())
	}
	calls := 0
	err = gradeWithQuota(t.Context(), g, func() error { calls++; return errDailyQuota })
	if !errors.Is(err, errDailyQuota) || calls != 1 {
		t.Fatalf("after giving up: err = %v calls = %d, want one ask and no wait", err, calls)
	}
}

func TestGradeWithQuotaPassesOtherErrorsThrough(t *testing.T) {
	g := &quotaGate{wait: time.Minute, probe: time.Millisecond}
	boom := errors.New("fetch 404")
	if err := gradeWithQuota(t.Context(), g, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestMoondreamTellsTheDailyAllocationFromThrottling(t *testing.T) {
	status, body := http.StatusTooManyRequests, `{"errors":[{"message":"AiError: AiError: you have used up your daily free allocation of 10,000 neurons, please upgrade"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := newMoondreamClient("acct", "tok", "m")
	c.apiRoot = srv.URL

	if _, err := c.ask(t.Context(), "data:,", "q"); !errors.Is(err, errDailyQuota) {
		t.Fatalf("daily allocation 429: err = %v, want errDailyQuota", err)
	}
	body = `{"errors":[{"message":"Capacity temporarily exceeded"}]}`
	_, err := c.ask(t.Context(), "data:,", "q")
	if _, isRetry := err.(retryable); !isRetry || errors.Is(err, errDailyQuota) {
		t.Fatalf("plain 429: err = %v, want a retryable throttle", err)
	}
}
