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
	ErrVoteActorRequired   = stderrors.New("catalog: a cover vote requires a voting user")
	ErrVoteWorkUnavailable = stderrors.New("catalog: work not available for voting")
	ErrVoteCoverNotOnWork  = stderrors.New("catalog: cover does not belong to this work")
)

type CoverVoteService struct{ db *gorm.DB }

func NewCoverVoteService(db *gorm.DB) *CoverVoteService { return &CoverVoteService{db: db} }

type CoverVoteParams struct {
	WorkID   int64
	CoverID  int64
	ActorUID int64
	Site     string
}

func (s *CoverVoteService) Vote(ctx context.Context, p CoverVoteParams) (int64, error) {
	if p.ActorUID <= 0 {
		return 0, ErrVoteActorRequired
	}
	var count int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertVotableWork(tx, p.WorkID); err != nil {
			return err
		}
		if err := assertCoverOnWork(tx, p.WorkID, p.CoverID); err != nil {
			return err
		}
		vote := model.CatalogCoverVote{
			WorkID: p.WorkID, CoverID: p.CoverID, ActorUID: p.ActorUID, Site: p.Site,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "work_id"}, {Name: "actor_uid"}},
			DoUpdates: clause.Assignments(map[string]any{"cover_id": p.CoverID, "site": p.Site, "updated_at": time.Now()}),
		}).Create(&vote).Error; err != nil {
			return err
		}
		return tx.Model(&model.CatalogCoverVote{}).Where("cover_id = ?", p.CoverID).Count(&count).Error
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *CoverVoteService) Unvote(ctx context.Context, workID, actorUID int64) error {
	if actorUID <= 0 {
		return ErrVoteActorRequired
	}
	return s.db.WithContext(ctx).
		Where("work_id = ? AND actor_uid = ?", workID, actorUID).
		Delete(&model.CatalogCoverVote{}).Error
}

func (s *CoverVoteService) WorkIDForCover(ctx context.Context, coverID int64) (int64, error) {
	var workID int64
	err := s.db.WithContext(ctx).Raw(
		`SELECT work_id FROM catalog_work_cover WHERE id = ? LIMIT 1`, coverID).Scan(&workID).Error
	if err != nil {
		return 0, err
	}
	return workID, nil
}

func (s *CoverVoteService) ListMine(ctx context.Context, uid int64) ([]CoverVoteParams, error) {
	if uid <= 0 {
		return nil, ErrVoteActorRequired
	}
	var rows []model.CatalogCoverVote
	if err := s.db.WithContext(ctx).Where("actor_uid = ?", uid).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CoverVoteParams, 0, len(rows))
	for _, r := range rows {
		out = append(out, CoverVoteParams{WorkID: r.WorkID, CoverID: r.CoverID, ActorUID: r.ActorUID, Site: r.Site})
	}
	return out, nil
}

func assertVotableWork(tx *gorm.DB, workID int64) error {
	var work model.CatalogWork
	err := tx.Select("status").Where("id = ?", workID).Take(&work).Error
	switch {
	case stderrors.Is(err, gorm.ErrRecordNotFound):
		return ErrVoteWorkUnavailable
	case err != nil:
		return err
	case work.Status != model.WorkStatusLive:
		return ErrVoteWorkUnavailable
	}
	return nil
}

func assertCoverOnWork(tx *gorm.DB, workID, coverID int64) error {
	var exists bool
	if err := tx.Raw(
		`SELECT EXISTS (SELECT 1 FROM catalog_work_cover WHERE id = ? AND work_id = ?)`,
		coverID, workID,
	).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return ErrVoteCoverNotOnWork
	}
	return nil
}

type CoverVoteTally struct {
	Count int
	Voted bool
}

func (s *ReadService) CoverVotes(ctx context.Context, coverIDs []int64, viewerUID int64) (map[int64]CoverVoteTally, error) {
	out := make(map[int64]CoverVoteTally, len(coverIDs))
	if len(coverIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		CoverID int64 `gorm:"column:cover_id"`
		Votes   int   `gorm:"column:votes"`
		Voted   bool  `gorm:"column:voted"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT cover_id,
		       COUNT(*) AS votes,
		       COUNT(*) FILTER (WHERE actor_uid = ?) > 0 AS voted
		  FROM catalog_cover_vote
		 WHERE cover_id IN ?
		 GROUP BY cover_id`, viewerUID, coverIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CoverID] = CoverVoteTally{Count: r.Votes, Voted: r.Voted}
	}
	return out, nil
}
