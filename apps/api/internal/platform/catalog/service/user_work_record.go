package service

import (
	"context"
	stderrors "errors"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

var (
	ErrWorkRecordActorRequired   = stderrors.New("catalog: a work record requires a reporting user")
	ErrWorkRecordWorkUnavailable = stderrors.New("catalog: work not available for records")
)

type WorkStateInput struct {
	State      int16
	Completion *int16
}

type WorkRecordInput struct {
	WorkState  *WorkStateInput
	Minutes    *int
	Rating     *RatingInput
	KeepRating bool
}

type WorkRecord struct {
	WorkID    int64
	WorkState *WorkStateRecord
	Playtime  *UserWorkPlaytime
	Rating    *RatingRecord
}

type UserWorkRecordService struct{ db *gorm.DB }

func NewUserWorkRecordService(db *gorm.DB) *UserWorkRecordService {
	return &UserWorkRecordService{db: db}
}

func (in WorkRecordInput) Validate() error {
	if in.WorkState != nil {
		if err := ValidateWorkState(in.WorkState.State, in.WorkState.Completion); err != nil {
			return err
		}
	}
	if in.Minutes != nil && (*in.Minutes < 0 || *in.Minutes > model.PlaytimeMinutesMax) {
		return ErrPlaytimeMinutesRange
	}
	if in.Rating != nil {
		return in.Rating.Validate()
	}
	return nil
}

func (s *UserWorkRecordService) Put(ctx context.Context, uid, workID int64, clientID, site string, in WorkRecordInput) (*WorkRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkRecordActorRequired
	}
	if clientID == "" {
		return nil, ErrPlaytimeClientRequired
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertReportableWork(tx, workID); err != nil {
			if err == ErrPlaytimeWorkUnavailable {
				return ErrWorkRecordWorkUnavailable
			}
			return err
		}
		if in.WorkState != nil {
			if _, err := setWorkStateTx(tx, uid, workID, in.WorkState.State, in.WorkState.Completion); err != nil {
				return err
			}
		} else if err := deleteWorkStateTx(tx, uid, workID); err != nil {
			return err
		}
		if in.Minutes != nil {
			if _, err := reportPlaytimeTx(tx, PlaytimeReport{
				ActorUID: uid, WorkID: workID, ClientID: clientID, Minutes: *in.Minutes,
			}); err != nil {
				return err
			}
		} else if err := tx.Where("actor_uid = ? AND work_id = ? AND client_id = ?", uid, workID, clientID).
			Delete(&model.CatalogUserPlaytime{}).Error; err != nil {
			return err
		}
		switch {
		case in.Rating != nil:
			_, err := setRatingTx(tx, uid, workID, clientID, site, *in.Rating)
			return err
		case in.KeepRating:
			return nil
		}
		_, err := deleteRatingsTx(tx, "actor_uid = ? AND work_id = ?", uid, workID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, uid, workID)
}

func (s *UserWorkRecordService) Get(ctx context.Context, uid, workID int64) (*WorkRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkRecordActorRequired
	}
	out := &WorkRecord{WorkID: workID}
	var err error
	if out.WorkState, err = NewUserWorkStateService(s.db).GetMine(ctx, uid, workID); err != nil {
		return nil, err
	}
	if out.Playtime, err = NewUserPlaytimeService(s.db).GetMine(ctx, uid, workID); err != nil {
		return nil, err
	}
	if out.Rating, err = NewUserRatingService(s.db).GetMine(ctx, uid, workID); err != nil {
		return nil, err
	}
	return out, nil
}
