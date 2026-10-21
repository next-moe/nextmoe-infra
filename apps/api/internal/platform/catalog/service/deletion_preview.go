package service

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

type DeletionAnchor struct {
	Source     string
	ExternalID string
	LinkKind   int16
}

type DeletionPreview struct {
	WorkID           int64
	DisplayName      string
	Deleted          bool
	MergedInto       *int64
	Anchors          []DeletionAnchor
	Releases         int64
	Ratings          int64
	FolderItems      int64
	Playtimes        int64
	WorkStates       int64
	OpenProposals    int64
	ClaimSite        string
	ClaimState       *int16
	PendingRequestID *int64
}

func (s *DeletionService) Preview(ctx context.Context, workID int64) (*DeletionPreview, error) {
	db := s.db.WithContext(ctx)
	var w struct {
		ID          int64
		DisplayName string
		Status      int16
		DeletedAt   *time.Time
		Site        *string
		ClaimState  *int16
	}
	if err := db.Raw(`SELECT id, display_name, status, deleted_at, site, claim_state FROM catalog_work WHERE id = ?`,
		workID).Scan(&w).Error; err != nil {
		return nil, err
	}
	if w.ID == 0 {
		return nil, fmt.Errorf("%w: work %d", ErrNotFound, workID)
	}
	p := &DeletionPreview{WorkID: w.ID, DisplayName: w.DisplayName, ClaimState: w.ClaimState}
	if w.Site != nil {
		p.ClaimSite = *w.Site
	}
	if w.Status == model.WorkStatusMerged && s.resolve != nil {
		if current, moved, err := s.resolve.Resolve(ctx, model.EntityTypeWork, workID); err == nil && moved {
			p.MergedInto = &current
		}
		return p, nil
	}
	p.Deleted = w.DeletedAt != nil

	if err := db.Raw(`
		SELECT s.key AS source, r.external_id, r.link_kind
		  FROM catalog_external_ref r
		  JOIN catalog_source s ON s.id = r.source_id
		 WHERE r.entity_type = ? AND r.entity_id = ? AND r.dead_at IS NULL
		 ORDER BY r.link_kind, s.key, r.external_id`, model.EntityTypeWork, workID).Scan(&p.Anchors).Error; err != nil {
		return nil, err
	}
	counts := []struct {
		dst *int64
		sql string
	}{
		{&p.Releases, `SELECT count(*) FROM catalog_release WHERE work_id = ? AND deleted_at IS NULL`},
		{&p.Ratings, `SELECT count(*) FROM catalog_user_work_rating WHERE work_id = ?`},
		{&p.FolderItems, `SELECT count(*) FROM catalog_user_folder_item WHERE work_id = ?`},
		{&p.Playtimes, `SELECT count(*) FROM catalog_user_playtime WHERE work_id = ?`},
		{&p.WorkStates, `SELECT count(*) FROM catalog_user_work_state WHERE work_id = ?`},
	}
	for _, c := range counts {
		if err := db.Raw(c.sql, workID).Scan(c.dst).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&editing.Proposal{}).
		Where("entity_type = ? AND entity_id = ? AND status = ?", editspec.TypeWork, workID, editing.StatusOpen).
		Count(&p.OpenProposals).Error; err != nil {
		return nil, err
	}
	var pending int64
	if err := db.Raw(`SELECT id FROM catalog_deletion_request
		WHERE entity_type = ? AND entity_id = ? AND status IN (?, ?) ORDER BY id DESC LIMIT 1`,
		model.EntityTypeWork, workID, model.ProposalStatusOpen, model.ProposalStatusApproved).Scan(&pending).Error; err != nil {
		return nil, err
	}
	if pending > 0 {
		p.PendingRequestID = &pending
	}
	return p, nil
}
