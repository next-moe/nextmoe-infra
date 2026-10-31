package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"time"

	"api/internal/platform/trust/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ScanWorker struct {
	db         *gorm.DB
	gateway    ScanGateway
	tier0      Tier0Matcher
	policy     *PolicyService
	mode       int16
	sampleRate float64
	rand       func() float64
	batchSize  int
	interval   time.Duration
	now        func() time.Time
}

type Tier0Matcher interface {
	Tier0Matches(ctx context.Context, site, text string) (decision string, matched []string, err error)
}

func NewScanWorker(db *gorm.DB, gateway ScanGateway, tier0 Tier0Matcher, opts ...ScanWorkerOption) *ScanWorker {
	w := &ScanWorker{
		db:         db,
		gateway:    gateway,
		tier0:      tier0,
		mode:       model.ScanModeShadow,
		sampleRate: 0,
		rand:       rand.Float64,
		batchSize:  scanBatchSize,
		interval:   scanInterval,
		now:        time.Now,
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

type ScanWorkerOption func(*ScanWorker)

func WithScanMode(name string) ScanWorkerOption {
	return func(w *ScanWorker) { w.mode = ScanModeFromName(name) }
}

func ScanModeFromName(name string) int16 {
	if name == "live" {
		return model.ScanModeLive
	}
	return model.ScanModeShadow
}

func WithSampleRate(rate float64) ScanWorkerOption {
	return func(w *ScanWorker) {
		if rate <= 0 || rate > maxScanSampleRate {
			w.sampleRate = 0
			return
		}
		w.sampleRate = rate
	}
}

func WithPolicy(p *PolicyService) ScanWorkerOption {
	return func(w *ScanWorker) { w.policy = p }
}

func (w *ScanWorker) policyFor(site string) ResolvedPolicy {
	if w.policy == nil {
		return ResolvedPolicy{
			ScanMode:        w.mode,
			SampleRate:      w.sampleRate,
			AutoHideEnabled: true,
		}
	}
	return w.policy.Resolve(site)
}

func (w *ScanWorker) Run(ctx context.Context) {
	mode, rate := w.mode, w.sampleRate
	if w.policy != nil {
		d := w.policy.Defaults()
		mode, rate = d.ScanMode, d.SampleRate
	}
	slog.Info("trust scan worker starting",
		"interval", w.interval.String(), "batch", w.batchSize,
		"gateway_configured", w.gateway.Configured(),
		"default_mode", scanModeName(mode), "default_sample_rate", rate,
		"per_site_policy", w.policy != nil)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		if _, err := w.ScorePending(ctx); err != nil {
			slog.Error("trust scan scoring", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *ScanWorker) ScorePending(ctx context.Context) (int, error) {
	processed := 0
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pending []model.TrustScanResult
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? OR (status = ? AND next_scan_at <= ?)",
				model.ScanStatusPending, model.ScanStatusDegraded, w.now()).
			Order("status ASC, id ASC").Limit(w.batchSize).Find(&pending).Error; err != nil {
			return err
		}
		for i := range pending {
			outage, err := w.scoreOne(ctx, tx, &pending[i])
			if err != nil {
				return err
			}
			processed++
			// One failed call is enough to tell the gateway is down. Dialing the
			// rest of the batch would only spend their attempts on the same outage.
			if outage {
				break
			}
		}
		return nil
	})
	return processed, err
}

func (w *ScanWorker) scoreOne(ctx context.Context, tx *gorm.DB, r *model.TrustScanResult) (outage bool, err error) {
	tier0, decision, matched := w.tier0Matched(ctx, r)
	pol := w.policyFor(r.Site)

	if r.CheckSkipped && (decision == DecisionDeny || decision == DecisionHold) {
		if err := w.enforceSkippedCheck(tx, r, decision, matched); err != nil {
			return false, err
		}
	}

	if !w.gateway.Configured() {
		return false, w.markDegraded(tx, r, tier0, pol, model.ScanDegradedGatewayUnconfigured)
	}
	verdict, err := w.gateway.Moderate(ctx, r.Site, r.ContentText, r.SubjectKind, r.AuthorID)
	if err != nil {
		slog.Warn("trust scan gateway call failed; draining to degraded",
			"scan_id", r.ID, "attempt", r.ScanAttempts+1, "err", err)
		return true, w.markDegraded(tx, r, tier0, pol, model.ScanDegradedGatewayCallFailed)
	}
	if verdict.Degraded {
		// The gateway reported its own fail-open path (upstream down / over budget /
		// a reply it could not parse). This branch logged NOTHING until 2026-08-07,
		// which is precisely why it drained 262 rows unnoticed — it is the most
		// common drain, and it was the only one that was silent. The gateway's own
		// ai_usage row holds the specific cause; this line is what makes anyone go
		// look for it.
		slog.Warn("trust scan gateway returned a degraded verdict; draining to degraded",
			"scan_id", r.ID, "site", r.Site, "attempt", r.ScanAttempts+1,
			"channel", verdict.Channel)
		return true, w.markDegraded(tx, r, tier0, pol, model.ScanDegradedGatewayDegraded)
	}
	return false, w.markScored(tx, r, verdict, tier0, pol)
}

func (w *ScanWorker) tier0Matched(ctx context.Context, r *model.TrustScanResult) (datatypes.JSON, string, []string) {
	if w.tier0 == nil {
		return nil, "", nil
	}
	decision, matched, err := w.tier0.Tier0Matches(ctx, r.Site, r.ContentText)
	if err != nil {
		slog.Warn("trust tier0 match failed; recording no tier0", "scan_id", r.ID, "err", err)
		return nil, "", nil
	}
	b, err := json.Marshal(matched)
	if err != nil {
		slog.Warn("trust tier0 marshal failed; recording no tier0", "scan_id", r.ID, "err", err)
		return nil, decision, matched
	}
	return datatypes.JSON(b), decision, matched
}

func (w *ScanWorker) markScored(tx *gorm.DB, r *model.TrustScanResult, v GatewayVerdict, tier0 datatypes.JSON, pol ResolvedPolicy) error {
	updates := map[string]any{
		"status":          model.ScanStatusScored,
		"mode":            pol.ScanMode,
		"flagged":         v.Flagged,
		"gateway_flagged": v.Flagged,
		"channel":         v.Channel,
		"scored_at":       w.now(),
		"scan_attempts":   r.ScanAttempts + 1,
		"degraded_reason": nil,
		"next_scan_at":    nil,
	}
	if tier0 != nil {
		updates["tier0_matched"] = tier0
	}
	if v.Score != nil {
		updates["score"] = *v.Score
	}
	if len(v.Categories) > 0 {
		b, err := json.Marshal(v.Categories)
		if err != nil {
			return err
		}
		updates["categories"] = datatypes.JSON(b)
	}
	if v.Flagged {
		slog.Info("trust scan flagged",
			"scan_id", r.ID, "site", r.Site, "subject_kind", r.SubjectKind, "subject_id", r.SubjectID,
			"channel", v.Channel, "categories", v.Categories, "enforced", pol.ScanMode == model.ScanModeLive)
	}
	if err := tx.Model(&model.TrustScanResult{}).Where("id = ?", r.ID).Updates(updates).Error; err != nil {
		return err
	}
	if v.Flagged {
		if pol.ScanMode == model.ScanModeLive {
			return w.enforceFlagged(tx, r, v, pol)
		}
		return nil
	}
	return w.maybeSampleClean(tx, r, v, pol)
}

func (w *ScanWorker) markDegraded(tx *gorm.DB, r *model.TrustScanResult, tier0 datatypes.JSON, pol ResolvedPolicy, reason int16) error {
	attempts := r.ScanAttempts + 1
	var next any
	if reason != model.ScanDegradedGatewayUnconfigured && attempts < maxScanAttempts {
		next = w.now().Add(scanRetryBackoff[attempts-1])
	}
	updates := map[string]any{
		"status":          model.ScanStatusDegraded,
		"mode":            pol.ScanMode,
		"degraded_reason": reason,
		"scan_attempts":   attempts,
		"next_scan_at":    next,
	}
	if tier0 != nil {
		updates["tier0_matched"] = tier0
	}
	return tx.Model(&model.TrustScanResult{}).Where("id = ?", r.ID).Updates(updates).Error
}
