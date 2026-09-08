package service

import (
	"context"
	stderrors "errors"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWorkStateActorRequired    = stderrors.New("catalog: a work state requires a reporting user")
	ErrWorkStateWorkUnavailable  = stderrors.New("catalog: work not available for work states")
	ErrWorkStateBadState         = stderrors.New("catalog: unknown work state")
	ErrWorkStateCompletionOnWish = stderrors.New("catalog: completion cannot accompany wish")
	ErrWorkStateBadCompletion    = stderrors.New("catalog: unknown work completion")
)

type UserWorkStateService struct{ db *gorm.DB }

func NewUserWorkStateService(db *gorm.DB) *UserWorkStateService {
	return &UserWorkStateService{db: db}
}

type WorkStateRecord struct {
	WorkID     int64
	State      int16
	Completion *int16
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s *UserWorkStateService) Set(ctx context.Context, uid, workID int64, state int16, completion *int16) (*WorkStateRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkStateActorRequired
	}
	switch state {
	case model.WorkStateWish, model.WorkStateDoing, model.WorkStateDone,
		model.WorkStateOnHold, model.WorkStateDropped:
	default:
		return nil, ErrWorkStateBadState
	}
	if completion != nil {
		switch *completion {
		case model.WorkCompletionOneRoute, model.WorkCompletionMain, model.WorkCompletionAll:
		default:
			return nil, ErrWorkStateBadCompletion
		}
		if state == model.WorkStateWish {
			return nil, ErrWorkStateCompletionOnWish
		}
	}
	row := model.CatalogUserWorkState{
		ActorUID: uid, WorkID: workID, State: state, Completion: completion,
	}
	var completionVal any
	if completion != nil {
		completionVal = *completion
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertReportableWork(tx, workID); err != nil {
			if err == ErrPlaytimeWorkUnavailable {
				return ErrWorkStateWorkUnavailable
			}
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "actor_uid"}, {Name: "work_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"state":      state,
				"completion": completionVal,
				"updated_at": time.Now(),
			}),
		}).Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &WorkStateRecord{
		WorkID: row.WorkID, State: row.State, Completion: row.Completion,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *UserWorkStateService) GetMine(ctx context.Context, uid, workID int64) (*WorkStateRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkStateActorRequired
	}
	var rows []model.CatalogUserWorkState
	if err := s.db.WithContext(ctx).
		Where("actor_uid = ? AND work_id = ?", uid, workID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &WorkStateRecord{
		WorkID: r.WorkID, State: r.State, Completion: r.Completion,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func (s *UserWorkStateService) ListMine(ctx context.Context, uid int64, since time.Time, sinceWorkID int64, limit int) ([]WorkStateRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkStateActorRequired
	}
	q := s.db.WithContext(ctx).Model(&model.CatalogUserWorkState{}).Where("actor_uid = ?", uid)
	if !since.IsZero() {
		if sinceWorkID > 0 {
			q = q.Where("updated_at > ? OR (updated_at = ? AND work_id > ?)", since, since, sinceWorkID)
		} else {
			q = q.Where("updated_at > ?", since)
		}
	}
	var rows []model.CatalogUserWorkState
	if err := q.Order("updated_at ASC, work_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WorkStateRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, WorkStateRecord{
			WorkID: r.WorkID, State: r.State, Completion: r.Completion,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *UserWorkStateService) CountMine(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrWorkStateActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserWorkState{}).
		Where("actor_uid = ?", uid).Count(&n).Error
	return n, err
}

func (s *UserWorkStateService) DeleteMine(ctx context.Context, uid, workID int64) error {
	if uid <= 0 {
		return ErrWorkStateActorRequired
	}
	if workID <= 0 {
		return ErrWorkStateWorkUnavailable
	}
	return s.db.WithContext(ctx).
		Where("actor_uid = ? AND work_id = ?", uid, workID).
		Delete(&model.CatalogUserWorkState{}).Error
}
