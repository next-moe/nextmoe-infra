package service

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PublicWorkRecordFilter struct {
	States        []int16
	DisplayLimits []string
}

type PlayRecordsPrivacy struct {
	Private   bool
	UpdatedAt *time.Time
}

func (s *UserWorkRecordService) Privacy(ctx context.Context, uid int64) (PlayRecordsPrivacy, error) {
	if uid <= 0 {
		return PlayRecordsPrivacy{}, ErrWorkRecordActorRequired
	}
	var rows []model.CatalogUserPrivacy
	if err := s.db.WithContext(ctx).Where("actor_uid = ?", uid).Limit(1).Find(&rows).Error; err != nil {
		return PlayRecordsPrivacy{}, err
	}
	if len(rows) == 0 {
		return PlayRecordsPrivacy{}, nil
	}
	at := rows[0].UpdatedAt
	return PlayRecordsPrivacy{Private: rows[0].PlayRecordsPrivate, UpdatedAt: &at}, nil
}

func (s *UserWorkRecordService) SetPrivacy(ctx context.Context, uid int64, private bool) (PlayRecordsPrivacy, error) {
	if uid <= 0 {
		return PlayRecordsPrivacy{}, ErrWorkRecordActorRequired
	}
	row := model.CatalogUserPrivacy{ActorUID: uid, PlayRecordsPrivate: private}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "actor_uid"}},
		DoUpdates: clause.Assignments(map[string]any{
			"play_records_private": private,
			"updated_at":           time.Now(),
		}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{
			SQL: "catalog_user_privacy.play_records_private IS DISTINCT FROM excluded.play_records_private",
		}}},
	}).Create(&row).Error
	if err != nil {
		return PlayRecordsPrivacy{}, err
	}
	return s.Privacy(ctx, uid)
}

func (s *UserWorkRecordService) publicStates(ctx context.Context, uid int64, f PublicWorkRecordFilter) *gorm.DB {
	q := s.db.WithContext(ctx).Table("catalog_user_work_state AS s").
		Joins("JOIN catalog_work w ON w.id = s.work_id AND w.status = ?", model.WorkStatusLive).
		Where("s.actor_uid = ?", uid)
	if len(f.States) > 0 {
		q = q.Where("s.state IN ?", f.States)
	}
	if pred, args := displayLimitWhere(f.DisplayLimits); pred != "" {
		q = q.Where(pred, args...)
	}
	return q
}

func (s *UserWorkRecordService) ListPublic(ctx context.Context, uid int64, f PublicWorkRecordFilter, before time.Time, beforeWorkID int64, limit int) ([]WorkRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkRecordActorRequired
	}
	q := s.publicStates(ctx, uid, f)
	if !before.IsZero() {
		q = q.Where("(s.updated_at, s.work_id) < (?, ?)", before, beforeWorkID)
	}
	var states []model.CatalogUserWorkState
	if err := q.Select("s.*").Order("s.updated_at DESC, s.work_id DESC").Limit(limit).Find(&states).Error; err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(states))
	for i, st := range states {
		ids[i] = st.WorkID
	}
	playtimes, err := NewUserPlaytimeService(s.db).ListMineFor(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := NewUserRatingService(s.db).ListMineFor(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	playtime := make(map[int64]*UserWorkPlaytime, len(playtimes))
	for i := range playtimes {
		playtime[playtimes[i].WorkID] = &playtimes[i]
	}
	rating := make(map[int64]*RatingRecord, len(ratings))
	for i := range ratings {
		rating[ratings[i].WorkID] = &ratings[i]
	}
	out := make([]WorkRecord, 0, len(states))
	for _, st := range states {
		out = append(out, WorkRecord{
			WorkID: st.WorkID,
			WorkState: &WorkStateRecord{
				WorkID: st.WorkID, State: st.State, Completion: st.Completion,
				CreatedAt: st.CreatedAt, UpdatedAt: st.UpdatedAt,
			},
			Playtime: playtime[st.WorkID],
			Rating:   rating[st.WorkID],
		})
	}
	return out, nil
}

func (s *UserWorkRecordService) CountPublicByState(ctx context.Context, uid int64, f PublicWorkRecordFilter) (map[int16]int64, error) {
	if uid <= 0 {
		return nil, ErrWorkRecordActorRequired
	}
	var rows []struct {
		State int16
		N     int64
	}
	if err := s.publicStates(ctx, uid, f).Select("s.state, count(*) AS n").Group("s.state").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int16]int64, len(rows))
	for _, r := range rows {
		out[r.State] = r.N
	}
	return out, nil
}
