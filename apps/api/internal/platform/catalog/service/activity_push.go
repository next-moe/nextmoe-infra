package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/communityclient"

	"gorm.io/gorm"
)

const (
	activityPushBatch     = 100
	activityPushGap       = 250 * time.Millisecond
	activityPushIdle      = 2 * time.Second
	activityPushBackoff   = 30 * time.Second
	activityTombstoneKeep = 30 * 24 * time.Hour
)

type ActivityWriter interface {
	WriteActivities(ctx context.Context, items []communityclient.ActivityItem) ([]communityclient.ActivityOutcome, error)
}

type ActivityPusher struct {
	db *gorm.DB
	w  ActivityWriter
}

func NewActivityPusher(db *gorm.DB, w ActivityWriter) *ActivityPusher {
	return &ActivityPusher{db: db, w: w}
}

// Run pushes at most one batch of 100 per activityPushGap, so a backlog such
// as the play-state backfill drains at 4 batches a second.
func (p *ActivityPusher) Run(ctx context.Context) {
	lastPrune := time.Time{}
	for {
		wait := activityPushIdle
		n, err := p.PushBatch(ctx)
		switch {
		case err != nil:
			slog.Error("catalog activity push", "err", err)
			wait = activityPushBackoff
		case n == activityPushBatch:
			wait = activityPushGap
		}
		if time.Since(lastPrune) > time.Hour {
			if _, err := p.Prune(ctx); err != nil {
				slog.Error("catalog activity prune", "err", err)
			}
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// PushBatch sends the oldest rows community has not acknowledged and returns
// how many it sent. A row written again while its batch was in flight keeps
// its newer revision pending.
func (p *ActivityPusher) PushBatch(ctx context.Context) (int, error) {
	sent := 0
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked bool
		if err := tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtext('catalog-activity-push'))`).Scan(&locked).Error; err != nil || !locked {
			return err
		}
		var rows []model.CatalogActivityItem
		if err := tx.Where("pushed_revision < revision").Order("revision ASC").Limit(activityPushBatch).
			Find(&rows).Error; err != nil || len(rows) == 0 {
			return err
		}
		items := make([]communityclient.ActivityItem, len(rows))
		for i, r := range rows {
			items[i] = communityclient.ActivityItem{Key: r.Key, ActorID: r.ActorUID, Revision: r.Revision, Removed: r.Removed}
			if r.Removed {
				continue
			}
			var body FeedItem
			if err := json.Unmarshal(r.Item, &body); err != nil {
				return err
			}
			at := body.OccurredAt
			items[i].Verb, items[i].ObjectKind, items[i].ObjectLabel = body.Verb, body.ObjectKind, body.ObjectLabel
			items[i].Title, items[i].Excerpt, items[i].WorkID = body.Title, body.Excerpt, body.WorkID
			items[i].ObjectRef, items[i].ContentLimit, items[i].OccurredAt = body.ObjectRef, body.ContentLimit, &at
		}
		results, err := p.w.WriteActivities(ctx, items)
		if err != nil {
			return err
		}
		for i, r := range rows {
			if results[i].Outcome == "invalid" {
				slog.Warn("catalog activity refused by community", "key", r.Key, "reason", results[i].Reason)
			}
			if err := tx.Model(&model.CatalogActivityItem{}).Where("key = ? AND revision = ?", r.Key, r.Revision).
				Update("pushed_revision", r.Revision).Error; err != nil {
				return err
			}
		}
		sent = len(rows)
		return nil
	})
	return sent, err
}

func (p *ActivityPusher) Prune(ctx context.Context) (int64, error) {
	res := p.db.WithContext(ctx).Where("removed AND pushed_revision >= revision AND updated_at < ?",
		time.Now().Add(-activityTombstoneKeep)).Delete(&model.CatalogActivityItem{})
	return res.RowsAffected, res.Error
}
