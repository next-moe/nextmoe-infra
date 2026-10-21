package service

import (
	"context"
	"errors"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

type EditHistoryService struct{ db *gorm.DB }

func NewEditHistoryService(db *gorm.DB) *EditHistoryService {
	return &EditHistoryService{db: db}
}

type RevisionPage struct {
	EntityType string
	EntityID   int64
	Site       string
	OriginSite string
	ActorUID   int64
	IDs        []int64
	Ascending  bool
	Cursor     int64
	Limit      int
	WithTotal  bool
}

// The snapshot column is a whole entity per row; the list face never renders it
// and selecting it turned a 100-row page into megabytes.
const revisionListColumns = `id, entity_family, entity_type, entity_id, seq, action,
	changed_fields, actor_uid, amender_uid, proposal_id, site, origin_site, created_at`

func (s *EditHistoryService) Revisions(ctx context.Context, p RevisionPage) ([]editing.Revision, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, nil
	}
	q := s.db.WithContext(ctx).Model(&editing.Revision{})
	if p.EntityType != "" {
		q = q.Where("entity_type = ?", p.EntityType)
	}
	if p.EntityID > 0 {
		q = q.Where("entity_id = ?", p.EntityID)
	}
	if p.Site != "" {
		q = q.Where("site = ?", p.Site)
	}
	if p.OriginSite != "" {
		q = q.Where("origin_site = ?", p.OriginSite)
	}
	if p.ActorUID > 0 {
		q = q.Where("actor_uid = ?", p.ActorUID)
	}
	if len(p.IDs) > 0 {
		q = q.Where("id IN ?", p.IDs)
	}

	var total int64
	if p.WithTotal {
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, 0, err
		}
	}

	page := q.Session(&gorm.Session{}).Select(revisionListColumns)
	if len(p.IDs) > 0 {
		var out []editing.Revision
		if err := page.Order("id DESC").Limit(len(p.IDs)).Find(&out).Error; err != nil {
			return nil, 0, err
		}
		return out, total, nil
	}
	if p.Cursor > 0 {
		if p.Ascending {
			page = page.Where("id > ?", p.Cursor)
		} else {
			page = page.Where("id < ?", p.Cursor)
		}
	}
	order := "id DESC"
	if p.Ascending {
		order = "id ASC"
	}
	limit := p.Limit
	if limit <= 0 || limit > 101 {
		limit = 21
	}
	var out []editing.Revision
	if err := page.Order(order).Limit(limit).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *EditHistoryService) RevisionByID(ctx context.Context, id int64) (*editing.Revision, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var rev editing.Revision
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&rev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (s *EditHistoryService) RevisionsWithBaseline(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if s == nil || s.db == nil || len(ids) == 0 {
		return out, nil
	}
	var hits []int64
	if err := s.db.WithContext(ctx).Model(&editing.Revision{}).
		Where("id IN ? AND seq = 1 AND baseline_snapshot IS NOT NULL", ids).
		Pluck("id", &hits).Error; err != nil {
		return nil, err
	}
	for _, id := range hits {
		out[id] = true
	}
	return out, nil
}

func (s *EditHistoryService) PreviousRevisionID(ctx context.Context, rev *editing.Revision) (int64, error) {
	if s == nil || s.db == nil || rev == nil || rev.Seq <= 1 {
		return 0, nil
	}
	var prior int64
	err := s.db.WithContext(ctx).Model(&editing.Revision{}).
		Select("id").
		Where("entity_family = ? AND entity_type = ? AND entity_id = ? AND seq < ?",
			rev.EntityFamily, rev.EntityType, rev.EntityID, rev.Seq).
		Order("seq DESC").Limit(1).Scan(&prior).Error
	if err != nil {
		return 0, err
	}
	return prior, nil
}

func (s *EditHistoryService) AmendmentsFor(ctx context.Context, proposalIDs []int64) (map[int64][]editing.ProposalAmendment, error) {
	out := map[int64][]editing.ProposalAmendment{}
	if s == nil || s.db == nil || len(proposalIDs) == 0 {
		return out, nil
	}
	var rows []editing.ProposalAmendment
	if err := s.db.WithContext(ctx).Model(&editing.ProposalAmendment{}).
		Select("id, proposal_id, seq, amender_uid, note, created_at").
		Where("proposal_id IN ?", proposalIDs).
		Order("proposal_id ASC, seq ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ProposalID] = append(out[rows[i].ProposalID], rows[i])
	}
	return out, nil
}

func (s *EditHistoryService) ProposalByID(ctx context.Context, id int64) (*editing.Proposal, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var p editing.Proposal
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *EditHistoryService) ProposalsByID(ctx context.Context, ids []int64) ([]editing.Proposal, error) {
	if s == nil || s.db == nil || len(ids) == 0 {
		return nil, nil
	}
	var out []editing.Proposal
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Order("id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
