package service

import (
	"context"
	stderrors "errors"
	"log/slog"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrHistoryActorRequired   = stderrors.New("catalog: browsing history requires a user and an app")
	ErrHistoryWorkUnavailable = stderrors.New("catalog: work not available for browsing history")
)

const (
	BrowsingHistoryRetention  = 90 * 24 * time.Hour
	BrowsingHistoryPerApp     = 500
	browsingHistoryRevisitGap = time.Minute
	browsingHistoryLockClass  = 0x7669_6577
	browsingHistorySweepBatch = 5000
)

type BrowsingHistoryService struct{ db *gorm.DB }

func NewBrowsingHistoryService(db *gorm.DB) *BrowsingHistoryService {
	return &BrowsingHistoryService{db: db}
}

type BrowsingHistorySettings struct {
	Off    bool
	Shared bool
}

type BrowsingHistoryEntry struct {
	WorkID        int64
	FirstViewedAt time.Time
	LastViewedAt  time.Time
	ViewCount     int64
}

// ClientID "" reads every app's rows merged per work.
type BrowsingHistoryFilter struct {
	ClientID      string
	DisplayLimits []string
}

// Recording and switching off serialize per user, so no view recorded
// concurrently with "off" survives the delete that "off" runs.
func lockBrowsingHistoryTx(tx *gorm.DB, uid int64) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(?, hashint8(?))`, browsingHistoryLockClass, uid).Error
}

func browsingHistorySettingsTx(tx *gorm.DB, uid int64) (BrowsingHistorySettings, error) {
	var rows []model.CatalogUserPrivacy
	if err := tx.Where("actor_uid = ?", uid).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return BrowsingHistorySettings{}, err
	}
	return BrowsingHistorySettings{Off: rows[0].BrowsingHistoryOff, Shared: rows[0].BrowsingHistoryShared}, nil
}

func (s *BrowsingHistoryService) Settings(ctx context.Context, uid int64) (BrowsingHistorySettings, error) {
	if uid <= 0 {
		return BrowsingHistorySettings{}, ErrHistoryActorRequired
	}
	return browsingHistorySettingsTx(s.db.WithContext(ctx), uid)
}

// SetSettings changes the switches that are non-nil. Turning history off
// erases every app's rows in the same transaction.
func (s *BrowsingHistoryService) SetSettings(ctx context.Context, uid int64, off, shared *bool) (BrowsingHistorySettings, error) {
	if uid <= 0 {
		return BrowsingHistorySettings{}, ErrHistoryActorRequired
	}
	if off == nil && shared == nil {
		return s.Settings(ctx, uid)
	}
	var out BrowsingHistorySettings
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockBrowsingHistoryTx(tx, uid); err != nil {
			return err
		}
		row := model.CatalogUserPrivacy{ActorUID: uid}
		set := map[string]any{"updated_at": time.Now()}
		var changed []clause.Expression
		if off != nil {
			row.BrowsingHistoryOff = *off
			set["browsing_history_off"] = *off
			changed = append(changed, clause.Expr{SQL: "catalog_user_privacy.browsing_history_off IS DISTINCT FROM excluded.browsing_history_off"})
		}
		if shared != nil {
			row.BrowsingHistoryShared = *shared
			set["browsing_history_shared"] = *shared
			changed = append(changed, clause.Expr{SQL: "catalog_user_privacy.browsing_history_shared IS DISTINCT FROM excluded.browsing_history_shared"})
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "actor_uid"}},
			DoUpdates: clause.Assignments(set),
			Where:     clause.Where{Exprs: []clause.Expression{clause.Or(changed...)}},
		}).Create(&row).Error; err != nil {
			return err
		}
		if off != nil && *off {
			if err := tx.Where("actor_uid = ?", uid).Delete(&model.CatalogUserWorkView{}).Error; err != nil {
				return err
			}
		}
		var err error
		out, err = browsingHistorySettingsTx(tx, uid)
		return err
	})
	return out, err
}

// Record notes that the user opened the work in this app now. With history
// off it stores nothing and still succeeds.
func (s *BrowsingHistoryService) Record(ctx context.Context, uid int64, clientID string, workID int64) error {
	if uid <= 0 || clientID == "" {
		return ErrHistoryActorRequired
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertReportableWork(tx, workID); err != nil {
			if err == ErrPlaytimeWorkUnavailable {
				return ErrHistoryWorkUnavailable
			}
			return err
		}
		if err := lockBrowsingHistoryTx(tx, uid); err != nil {
			return err
		}
		settings, err := browsingHistorySettingsTx(tx, uid)
		if err != nil || settings.Off {
			return err
		}
		now := time.Now()
		var rows []struct{ Inserted bool }
		if err := tx.Raw(`
			INSERT INTO catalog_user_work_view AS v (actor_uid, client_id, work_id, first_viewed_at, last_viewed_at, view_count)
			VALUES (?, ?, ?, ?, ?, 1)
			ON CONFLICT (actor_uid, client_id, work_id) DO UPDATE
			SET last_viewed_at = excluded.last_viewed_at, view_count = v.view_count + 1
			WHERE v.last_viewed_at < ?
			RETURNING (xmax = 0) AS inserted`,
			uid, clientID, workID, now, now, now.Add(-browsingHistoryRevisitGap)).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 || !rows[0].Inserted {
			return nil
		}
		return tx.Exec(`
			DELETE FROM catalog_user_work_view WHERE id IN (
				SELECT id FROM catalog_user_work_view WHERE actor_uid = ? AND client_id = ?
				ORDER BY last_viewed_at DESC, id DESC OFFSET ?)`,
			uid, clientID, BrowsingHistoryPerApp).Error
	})
}

func (s *BrowsingHistoryService) entries(ctx context.Context, uid int64, f BrowsingHistoryFilter) *gorm.DB {
	q := s.db.WithContext(ctx).Table("catalog_user_work_view AS v").
		Joins("JOIN catalog_work w ON w.id = v.work_id AND w.status = ? AND w.deleted_at IS NULL", model.WorkStatusLive).
		Where("v.actor_uid = ? AND v.last_viewed_at > ?", uid, time.Now().Add(-BrowsingHistoryRetention))
	if f.ClientID != "" {
		q = q.Where("v.client_id = ?", f.ClientID)
	}
	if pred, args := displayLimitWhere(f.DisplayLimits); pred != "" {
		q = q.Where(pred, args...)
	}
	return q
}

// List is newest first. before/beforeWorkID is the keyset of the last entry
// of the previous page; zero starts from the top.
func (s *BrowsingHistoryService) List(ctx context.Context, uid int64, f BrowsingHistoryFilter, before time.Time, beforeWorkID int64, limit int) ([]BrowsingHistoryEntry, error) {
	if uid <= 0 {
		return nil, ErrHistoryActorRequired
	}
	q := s.entries(ctx, uid, f).
		Select(`v.work_id, min(v.first_viewed_at) AS first_viewed_at,
			max(v.last_viewed_at) AS last_viewed_at, sum(v.view_count)::bigint AS view_count`).
		Group("v.work_id")
	if !before.IsZero() {
		q = q.Having("(max(v.last_viewed_at), v.work_id) < (?, ?)", before, beforeWorkID)
	}
	var out []BrowsingHistoryEntry
	err := q.Order("last_viewed_at DESC, v.work_id DESC").Limit(limit).Scan(&out).Error
	return out, err
}

func (s *BrowsingHistoryService) Count(ctx context.Context, uid int64, f BrowsingHistoryFilter) (int64, error) {
	if uid <= 0 {
		return 0, ErrHistoryActorRequired
	}
	var n int64
	err := s.entries(ctx, uid, f).Distinct("v.work_id").Count(&n).Error
	return n, err
}

// Forget deletes the user's entries for one work, or every work when workID
// is 0, in one app or, with clientID "", in every app.
func (s *BrowsingHistoryService) Forget(ctx context.Context, uid int64, clientID string, workID int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrHistoryActorRequired
	}
	q := s.db.WithContext(ctx).Where("actor_uid = ?", uid)
	if clientID != "" {
		q = q.Where("client_id = ?", clientID)
	}
	if workID > 0 {
		q = q.Where("work_id = ?", workID)
	}
	res := q.Delete(&model.CatalogUserWorkView{})
	return res.RowsAffected, res.Error
}

// SweepExpired deletes entries past the retention window in batches.
func (s *BrowsingHistoryService) SweepExpired(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-BrowsingHistoryRetention)
	var total int64
	for {
		res := s.db.WithContext(ctx).Exec(`
			DELETE FROM catalog_user_work_view WHERE id IN (
				SELECT id FROM catalog_user_work_view WHERE last_viewed_at <= ? LIMIT ?)`,
			cutoff, browsingHistorySweepBatch)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < browsingHistorySweepBatch {
			return total, nil
		}
	}
}

func (s *BrowsingHistoryService) StartSweeper(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			if n, err := s.SweepExpired(ctx); err != nil {
				slog.Warn("browsing history sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("browsing history sweep", "deleted", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
