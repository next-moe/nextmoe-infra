package app

import (
	"context"
	"log/slog"
	"time"
)

type dueDeletions interface {
	ExecuteDueDeletions(ctx context.Context, now time.Time) (int, error)
}

func StartAccountDeletion(ctx context.Context, svc dueDeletions) {
	run := func() {
		n, err := svc.ExecuteDueDeletions(ctx, time.Now())
		if err != nil {
			slog.Error("account deletion run failed", "deleted", n, "err", err)
			return
		}
		if n > 0 {
			slog.Info("account deletion run", "deleted", n)
		}
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
