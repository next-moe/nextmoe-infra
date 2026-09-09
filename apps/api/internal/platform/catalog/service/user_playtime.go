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
	ErrPlaytimeActorRequired   = stderrors.New("catalog: a playtime report requires a reporting user")
	ErrPlaytimeClientRequired  = stderrors.New("catalog: a playtime report requires a client id")
	ErrPlaytimeWorkUnavailable = stderrors.New("catalog: work not available for playtime reporting")
	ErrPlaytimeMinutesRange    = stderrors.New("catalog: minutes must be between 0 and 60000")
	ErrPlaytimeUnknownSource   = stderrors.New("catalog: unknown external source key")
	ErrPlaytimeRefUnresolved   = stderrors.New("catalog: no work is anchored to that external id")
)

type UserPlaytimeService struct{ db *gorm.DB }

func NewUserPlaytimeService(db *gorm.DB) *UserPlaytimeService {
	return &UserPlaytimeService{db: db}
}

type PlaytimeReport struct {
	ActorUID     int64
	WorkID       int64
	ClientID     string
	Minutes      int
	LastPlayedAt *time.Time
}

type PlaytimeRecord struct {
	WorkID       int64
	Minutes      int
	LastPlayedAt *time.Time
	ClientID     string
	UpdatedAt    time.Time
}

func (s *UserPlaytimeService) Report(ctx context.Context, r PlaytimeReport) (*PlaytimeRecord, error) {
	if err := validateReport(r); err != nil {
		return nil, err
	}
	row := model.CatalogUserPlaytime{
		ActorUID: r.ActorUID, WorkID: r.WorkID, ClientID: r.ClientID,
		Minutes: r.Minutes, LastPlayedAt: r.LastPlayedAt,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertReportableWork(tx, r.WorkID); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "actor_uid"}, {Name: "work_id"}, {Name: "client_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"minutes":        r.Minutes,
				"last_played_at": r.LastPlayedAt,
				"updated_at":     time.Now(),
			}),
		}).Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &PlaytimeRecord{
		WorkID: row.WorkID, Minutes: row.Minutes,
		LastPlayedAt: row.LastPlayedAt, ClientID: row.ClientID, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *UserPlaytimeService) ResolveRef(ctx context.Context, sourceKey, externalID string) (int64, error) {
	var sourceID int16
	err := s.db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_source WHERE key = ?`, sourceKey).Scan(&sourceID).Error
	if err != nil {
		return 0, err
	}
	if sourceID == 0 {
		return 0, ErrPlaytimeUnknownSource
	}
	var workID int64
	err = s.db.WithContext(ctx).Raw(`
		SELECT entity_id FROM catalog_external_ref
		 WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = 0
		 ORDER BY id DESC LIMIT 1`,
		model.EntityTypeWork, sourceID, externalID).Scan(&workID).Error
	if err != nil {
		return 0, err
	}
	if workID == 0 {
		return 0, ErrPlaytimeRefUnresolved
	}
	return workID, nil
}

func (s *UserPlaytimeService) ListMine(ctx context.Context, uid int64, since time.Time, sinceWorkID int64, limit int) ([]PlaytimeRecord, error) {
	if uid <= 0 {
		return nil, ErrPlaytimeActorRequired
	}
	q := s.db.WithContext(ctx).Model(&model.CatalogUserPlaytime{}).Where("actor_uid = ?", uid)
	if !since.IsZero() {
		// sinceWorkID <= 0 keeps the strictly-after semantics: the v1 face's
		// updated_since is a client-remembered resync watermark, and adding the
		// equal-timestamp arm there would re-deliver rows on every pull.
		if sinceWorkID > 0 {
			q = q.Where("updated_at > ? OR (updated_at = ? AND work_id > ?)", since, since, sinceWorkID)
		} else {
			q = q.Where("updated_at > ?", since)
		}
	}
	var rows []model.CatalogUserPlaytime
	if err := q.Order("updated_at ASC, work_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PlaytimeRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, PlaytimeRecord{
			WorkID: r.WorkID, Minutes: r.Minutes,
			LastPlayedAt: r.LastPlayedAt, ClientID: r.ClientID, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *UserPlaytimeService) CountMine(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrPlaytimeActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserPlaytime{}).
		Where("actor_uid = ?", uid).Count(&n).Error
	return n, err
}

type UserWorkPlaytime struct {
	WorkID       int64
	Minutes      int
	LastPlayedAt *time.Time
	Clients      int
}

func (s *UserPlaytimeService) DeleteMine(ctx context.Context, uid, workID int64) error {
	if uid <= 0 {
		return ErrPlaytimeActorRequired
	}
	if workID <= 0 {
		return ErrPlaytimeWorkUnavailable
	}
	return s.db.WithContext(ctx).
		Where("actor_uid = ? AND work_id = ?", uid, workID).
		Delete(&model.CatalogUserPlaytime{}).Error
}

func (s *UserPlaytimeService) GetMine(ctx context.Context, uid, workID int64) (*UserWorkPlaytime, error) {
	if uid <= 0 {
		return nil, ErrPlaytimeActorRequired
	}
	var rows []model.CatalogUserPlaytime
	if err := s.db.WithContext(ctx).
		Where("actor_uid = ? AND work_id = ?", uid, workID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := UserWorkPlaytime{WorkID: workID, Clients: len(rows)}
	best := 0
	for i, r := range rows {
		if r.Minutes > rows[best].Minutes {
			best = i
		}
		if r.LastPlayedAt != nil && (out.LastPlayedAt == nil || r.LastPlayedAt.After(*out.LastPlayedAt)) {
			out.LastPlayedAt = r.LastPlayedAt
		}
	}
	out.Minutes = rows[best].Minutes
	return &out, nil
}

func validateReport(r PlaytimeReport) error {
	if r.ActorUID <= 0 {
		return ErrPlaytimeActorRequired
	}
	if r.ClientID == "" {
		return ErrPlaytimeClientRequired
	}
	if r.Minutes < 0 || r.Minutes > model.PlaytimeMinutesMax {
		return ErrPlaytimeMinutesRange
	}
	return nil
}

func assertReportableWork(tx *gorm.DB, workID int64) error {
	var work model.CatalogWork
	err := tx.Select("status").Where("id = ?", workID).Take(&work).Error
	switch {
	case stderrors.Is(err, gorm.ErrRecordNotFound):
		return ErrPlaytimeWorkUnavailable
	case err != nil:
		return err
	case work.Status != model.WorkStatusLive:
		return ErrPlaytimeWorkUnavailable
	}
	return nil
}
