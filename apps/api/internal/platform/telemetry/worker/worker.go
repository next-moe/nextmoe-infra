package worker

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbolicate"

	"gorm.io/gorm"
)

const (
	ProcessBudget = 60 * time.Second
	IdleWait      = 5 * time.Second
	RequeueEvery  = 5 * time.Minute
)

type Worker struct {
	Store  *store.Store
	Tools  symbolicate.Tools
	Lookup Lookup
	Log    *slog.Logger
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Log == nil {
		w.Log = slog.Default()
	}
	requeue := time.NewTicker(RequeueEvery)
	defer requeue.Stop()
	if err := w.Store.RequeueReadyCrashes(ctx); err != nil {
		w.Log.Error("telemetry requeue", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-requeue.C:
			if err := w.Store.RequeueReadyCrashes(ctx); err != nil {
				w.Log.Error("telemetry requeue", "err", err)
			}
		default:
		}
		claimed, err := w.step(ctx)
		if err != nil {
			w.Log.Error("telemetry crash step", "err", err)
		}
		if claimed {
			continue
		}
		timer := time.NewTimer(IdleWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-requeue.C:
			timer.Stop()
			if err := w.Store.RequeueReadyCrashes(ctx); err != nil {
				w.Log.Error("telemetry requeue", "err", err)
			}
		case <-timer.C:
		}
	}
}

func (w *Worker) step(ctx context.Context) (bool, error) {
	return w.Store.ClaimOne(ctx, func(tx *gorm.DB, job *store.ClaimedCrash) error {
		return w.handle(ctx, tx, job)
	})
}

func (w *Worker) handle(ctx context.Context, tx *gorm.DB, job *store.ClaimedCrash) error {
	start := time.Now()
	pctx, cancel := context.WithTimeout(ctx, ProcessBudget)
	defer cancel()
	res := Process(pctx, job, w.Tools, w.Lookup)
	if err := store.FinishCrash(tx, job, res); err != nil {
		return err
	}
	w.Log.Info("telemetry crash processed",
		"app_id", job.Crash.AppID,
		"kind", job.Crash.Kind,
		"status", res.Status,
		"elapsed", time.Since(start),
	)
	return nil
}
