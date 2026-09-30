package alert

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/telemetry/model"
)

const digestAge = 15 * time.Minute

type DispatchRepo interface {
	ListEnabledAlertChannels(ctx context.Context) ([]model.AlertChannel, error)
	SuppressQueuedAlerts(ctx context.Context, reason string) error
	ListQueuedImmediateAlerts(ctx context.Context) ([]model.Alert, error)
	ListReadyDigestAlerts(ctx context.Context, now time.Time, age time.Duration) ([]model.Alert, error)
	MarkAlertsSent(ctx context.Context, ids []int64, at time.Time) error
	RecordAlertFailure(ctx context.Context, ids []int64, errMsg string) error
	AppServiceNames(ctx context.Context, ids []int64) (map[int64]string, error)
}

type Dispatcher struct {
	repo           DispatchRepo
	notifier       Notifier
	mailConfigured bool
	adminBase      string
}

func NewDispatcher(repo DispatchRepo, notifier Notifier, mailConfigured bool, adminBase string) *Dispatcher {
	return &Dispatcher{repo: repo, notifier: notifier, mailConfigured: mailConfigured, adminBase: trimAdminBase(adminBase)}
}

func (d *Dispatcher) Dispatch(ctx context.Context, now time.Time) error {
	if d == nil || d.repo == nil {
		return nil
	}
	now = now.UTC()
	channels, err := d.repo.ListEnabledAlertChannels(ctx)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return d.repo.SuppressQueuedAlerts(ctx, "no channel")
	}
	if !d.mailConfigured {
		return d.repo.SuppressQueuedAlerts(ctx, "mail not configured")
	}
	var targets []string
	for _, c := range channels {
		if c.Kind == model.AlertKindEmail && c.Target != "" {
			targets = append(targets, c.Target)
		}
	}
	immediates, err := d.repo.ListQueuedImmediateAlerts(ctx)
	if err != nil {
		return err
	}
	digests, err := d.repo.ListReadyDigestAlerts(ctx, now, digestAge)
	if err != nil {
		return err
	}
	names, err := d.namesFor(ctx, immediates, digests)
	if err != nil {
		return err
	}
	for _, a := range immediates {
		n := RenderImmediate(a, names[a.AppID], d.adminBase)
		if err := d.sendAll(ctx, targets, n); err != nil {
			if rerr := d.repo.RecordAlertFailure(ctx, []int64{a.ID}, err.Error()); rerr != nil {
				return rerr
			}
			continue
		}
		if err := d.repo.MarkAlertsSent(ctx, []int64{a.ID}, now); err != nil {
			return err
		}
	}
	byApp := map[int64][]model.Alert{}
	var order []int64
	for _, a := range digests {
		if _, ok := byApp[a.AppID]; !ok {
			order = append(order, a.AppID)
		}
		byApp[a.AppID] = append(byApp[a.AppID], a)
	}
	for _, appID := range order {
		group := byApp[appID]
		n := RenderDigest(names[appID], d.adminBase, group)
		ids := make([]int64, len(group))
		for i, a := range group {
			ids[i] = a.ID
		}
		if err := d.sendAll(ctx, targets, n); err != nil {
			if rerr := d.repo.RecordAlertFailure(ctx, ids, err.Error()); rerr != nil {
				return rerr
			}
			continue
		}
		if err := d.repo.MarkAlertsSent(ctx, ids, now); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) sendAll(ctx context.Context, targets []string, n Notification) error {
	if len(targets) == 0 {
		return fmt.Errorf("no channel")
	}
	if d.notifier == nil {
		return fmt.Errorf("mail not configured")
	}
	var first error
	for _, to := range targets {
		n.To = to
		if err := d.notifier.Send(ctx, n); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (d *Dispatcher) namesFor(ctx context.Context, groups ...[]model.Alert) (map[int64]string, error) {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, g := range groups {
		for _, a := range g {
			if a.AppID == 0 {
				continue
			}
			if _, ok := seen[a.AppID]; ok {
				continue
			}
			seen[a.AppID] = struct{}{}
			ids = append(ids, a.AppID)
		}
	}
	return d.repo.AppServiceNames(ctx, ids)
}
