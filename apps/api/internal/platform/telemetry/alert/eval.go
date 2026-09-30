package alert

import (
	"context"
	"time"

	"api/internal/platform/telemetry/store"
)

type Evaluator struct {
	st *store.Store
}

func NewEvaluator(st *store.Store) *Evaluator { return &Evaluator{st: st} }

func (e *Evaluator) Evaluate(ctx context.Context, now time.Time) error {
	if e == nil || e.st == nil {
		return nil
	}
	now = now.UTC()
	rows, err := e.st.CollectAlertCandidates(ctx, now)
	if err != nil {
		return err
	}
	return e.st.InsertAlertCandidates(ctx, rows)
}
