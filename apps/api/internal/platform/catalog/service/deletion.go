package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/editing"
	"api/internal/platform/editorigin"
	"api/internal/platform/settings/keys"

	"gorm.io/gorm"
)

var (
	ErrDeletionReason     = errors.New("catalog deletion: unknown reason")
	ErrDeletionDuplicate  = errors.New("catalog deletion: a duplicate entry is merged into the one it duplicates, not deleted")
	ErrDeletionPending    = errors.New("catalog deletion: the entry already has an open or approved deletion request")
	ErrDeletionFamily     = errors.New("catalog deletion: entries of this family cannot be deleted")
	ErrDeletionNotDeleted = errors.New("catalog deletion: the entry is not deleted by this request")
)

const DeletionReasonDuplicate = "duplicate"

type DeletionService struct {
	db      *gorm.DB
	resolve *ResolveService
}

func NewDeletionService(db *gorm.DB, resolve *ResolveService) *DeletionService {
	return &DeletionService{db: db, resolve: resolve}
}

type DeletionRequestFilter struct {
	ProposedBy *int64
	Status     *int16
	EntityType *int16
	EntityID   *int64
	OriginSite string
	BeforeID   int64
	Limit      int
}

func (s *DeletionService) Request(ctx context.Context, entityType int16, entityID, proposedBy int64, reason, note string) (*model.CatalogDeletionRequest, error) {
	if entityType != model.EntityTypeWork {
		return nil, ErrDeletionFamily
	}
	if reason == DeletionReasonDuplicate {
		return nil, ErrDeletionDuplicate
	}
	if !slices.Contains(model.DeletionReasons, reason) {
		return nil, fmt.Errorf("%w: %q", ErrDeletionReason, reason)
	}
	if s.resolve != nil {
		current, moved, err := s.resolve.Resolve(ctx, entityType, entityID)
		if err != nil {
			return nil, err
		}
		if moved {
			return nil, fmt.Errorf("%w: work %d was merged into %d", ErrEntityNotLive, entityID, current)
		}
	}
	if err := assertEntityAlive(s.db.WithContext(ctx), entityType, entityID); err != nil {
		return nil, err
	}
	r := &model.CatalogDeletionRequest{
		EntityType: entityType,
		EntityID:   entityID,
		Reason:     reason,
		Note:       note,
		Status:     model.ProposalStatusOpen,
		ProposedBy: proposedBy,
		OriginSite: editorigin.Ptr(ctx),
	}
	if err := s.db.WithContext(ctx).Create(r).Error; err != nil {
		if isUniqueViolation(err, "uq_catalog_deletion_request_pending") {
			return nil, ErrDeletionPending
		}
		return nil, err
	}
	return r, nil
}

func (s *DeletionService) List(ctx context.Context, f DeletionRequestFilter) ([]model.CatalogDeletionRequest, error) {
	q := s.db.WithContext(ctx).Model(&model.CatalogDeletionRequest{})
	if f.ProposedBy != nil {
		q = q.Where("proposed_by = ?", *f.ProposedBy)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.EntityType != nil {
		q = q.Where("entity_type = ?", *f.EntityType)
	}
	if f.EntityID != nil {
		q = q.Where("entity_id = ?", *f.EntityID)
	}
	if f.OriginSite != "" {
		q = q.Where("origin_site = ?", f.OriginSite)
	}
	if f.BeforeID > 0 {
		q = q.Where("id < ?", f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	var rows []model.CatalogDeletionRequest
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *DeletionService) Get(ctx context.Context, id int64) (*model.CatalogDeletionRequest, error) {
	var r model.CatalogDeletionRequest
	err := s.db.WithContext(ctx).First(&r, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: deletion request %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *DeletionService) CountSince(ctx context.Context, proposedBy int64, since time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogDeletionRequest{}).
		Where("proposed_by = ? AND proposed_at >= ?", proposedBy, since).Count(&n).Error
	return n, err
}

func (s *DeletionService) Approve(ctx context.Context, id, by int64) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen}, func(tx *gorm.DB, r *model.CatalogDeletionRequest) error {
		if err := assertEntityAlive(tx, r.EntityType, r.EntityID); err != nil {
			return err
		}
		now := time.Now()
		after := now.Add(time.Duration(keys.CatalogDeletionCoolingOffHours.Get()) * time.Hour)
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusApproved, "execute_after": after,
			"decided_by": by, "decided_at": now,
		}).Error
	})
}

func (s *DeletionService) Reject(ctx context.Context, id, by int64, reason string) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved}, func(tx *gorm.DB, r *model.CatalogDeletionRequest) error {
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusRejected, "decided_by": by, "decided_at": time.Now(),
			"decision_note": reason,
		}).Error
	})
}

func (s *DeletionService) Withdraw(ctx context.Context, id int64) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen}, func(tx *gorm.DB, r *model.CatalogDeletionRequest) error {
		return tx.Model(r).Update("status", model.ProposalStatusWithdrawn).Error
	})
}

// Execute soft-deletes the work of an approved request whose cooling-off has
// ended. closed reports a request it closed instead because its work was
// merged or deleted meanwhile.
func (s *DeletionService) Execute(ctx context.Context, id int64, by *int64) (closed bool, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockDeletionRequest(tx, id)
		if err != nil {
			return err
		}
		if r.Status != model.ProposalStatusApproved {
			return fmt.Errorf("%w: deletion request %d is in status %d, want approved", ErrProposalState, id, r.Status)
		}
		if r.ExecuteAfter == nil || time.Now().Before(*r.ExecuteAfter) {
			return fmt.Errorf("%w: deletion request %d executable after %v", ErrCoolingOff, id, r.ExecuteAfter)
		}
		w, err := lockWork(tx, r.EntityID)
		if err != nil {
			return err
		}
		if w == nil || w.DeletedAt != nil {
			closed = true
			return tx.Model(r).Updates(map[string]any{
				"status": model.ProposalStatusRejected, "decided_at": time.Now(),
				"decision_note": "条目已被合并或删除，此请求随之关闭",
			}).Error
		}
		snap, err := takeSnapshot(tx, model.EntityTypeWork, w.ID)
		if err != nil {
			return fmt.Errorf("snapshot work: %w", err)
		}
		if err := tx.Exec(`UPDATE catalog_work SET status = ?, deleted_at = now(), updated_at = now() WHERE id = ?`,
			model.WorkStatusDeleted, w.ID).Error; err != nil {
			return err
		}
		if err := editing.DeclineOpenProposals(tx, editspec.TypeWork, w.ID, by,
			fmt.Sprintf("作品已删除（删除请求 #%d），此提案随之关闭", r.ID)); err != nil {
			return fmt.Errorf("close proposals: %w", err)
		}
		changed, err := marshalChangedFields(map[string]any{"status": model.WorkStatusDeleted, "deleted": true})
		if err != nil {
			return err
		}
		if err := writeRevision(tx, model.EntityTypeWork, w.ID, model.RevisionActionDeleted, snap, changed, by,
			fmt.Sprintf("deletion request %d: %s", r.ID, r.Reason)); err != nil {
			return err
		}
		if err := refreshHiddenWorkUserData(ctx, tx, w.ID); err != nil {
			return err
		}
		prior := w.Status
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusExecuted, "executed_at": time.Now(), "executed_by": by,
			"prior_status": prior,
		}).Error
	})
	return closed, err
}

func (s *DeletionService) Restore(ctx context.Context, id, by int64, note string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockDeletionRequest(tx, id)
		if err != nil {
			return err
		}
		if r.Status != model.ProposalStatusExecuted {
			return fmt.Errorf("%w: deletion request %d is in status %d, want executed", ErrProposalState, id, r.Status)
		}
		w, err := lockWork(tx, r.EntityID)
		if err != nil {
			return err
		}
		if w == nil || w.DeletedAt == nil || w.Status != model.WorkStatusDeleted {
			return fmt.Errorf("%w: work %d", ErrDeletionNotDeleted, r.EntityID)
		}
		prior := model.WorkStatusLive
		if r.PriorStatus != nil {
			prior = *r.PriorStatus
		}
		if err := tx.Exec(`UPDATE catalog_work SET status = ?, deleted_at = NULL, updated_at = now() WHERE id = ?`,
			prior, w.ID).Error; err != nil {
			return err
		}
		snap, err := takeSnapshot(tx, model.EntityTypeWork, w.ID)
		if err != nil {
			return fmt.Errorf("snapshot work: %w", err)
		}
		changed, err := marshalChangedFields(map[string]any{"status": prior, "deleted": false})
		if err != nil {
			return err
		}
		if err := writeRevision(tx, model.EntityTypeWork, w.ID, model.RevisionActionRestored, snap, changed, &by,
			fmt.Sprintf("deletion request %d restored", r.ID)); err != nil {
			return err
		}
		if err := refreshHiddenWorkUserData(ctx, tx, w.ID); err != nil {
			return err
		}
		return tx.Model(r).Updates(map[string]any{
			"status": model.DeletionStatusRestored, "restored_at": time.Now(), "restored_by": by,
			"restore_note": note,
		}).Error
	})
}

type DueDeletionReport struct {
	Due, Executed, Closed, Failed int
}

func (s *DeletionService) ExecuteDue(ctx context.Context) (DueDeletionReport, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Model(&model.CatalogDeletionRequest{}).
		Where("status = ? AND execute_after <= ?", model.ProposalStatusApproved, time.Now()).
		Order("id").Pluck("id", &ids).Error; err != nil {
		return DueDeletionReport{}, err
	}
	rep := DueDeletionReport{Due: len(ids)}
	var firstErr error
	for _, id := range ids {
		closed, err := s.Execute(ctx, id, nil)
		switch {
		case err != nil:
			rep.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("execute deletion request %d: %w", id, err)
			}
		case closed:
			rep.Closed++
		default:
			rep.Executed++
		}
	}
	return rep, firstErr
}

func (s *DeletionService) WorkNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          int64
		DisplayName string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.DisplayName
	}
	return out, nil
}

type DeletedWork struct {
	RequestID int64
	Reason    string
	DeletedAt time.Time
}

// DeletedWorkInfo reports a work removed by a deletion request; nil for any
// other work, including one that was merged away.
func (s *DeletionService) DeletedWorkInfo(ctx context.Context, workID int64) (*DeletedWork, error) {
	var row struct {
		ID         int64
		Reason     string
		ExecutedAt *time.Time
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT r.id, r.reason, r.executed_at
		  FROM catalog_work w
		  JOIN catalog_deletion_request r ON r.entity_type = ? AND r.entity_id = w.id AND r.status = ?
		 WHERE w.id = ? AND w.status = ? AND w.deleted_at IS NOT NULL
		 ORDER BY r.id DESC LIMIT 1`,
		model.EntityTypeWork, model.ProposalStatusExecuted, workID, model.WorkStatusDeleted).Scan(&row).Error
	if err != nil || row.ID == 0 {
		return nil, err
	}
	out := &DeletedWork{RequestID: row.ID, Reason: row.Reason}
	if row.ExecutedAt != nil {
		out.DeletedAt = *row.ExecutedAt
	}
	return out, nil
}

func (s *DeletionService) transition(ctx context.Context, id int64, allowed []int16, fn func(*gorm.DB, *model.CatalogDeletionRequest) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockDeletionRequest(tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(allowed, r.Status) {
			return fmt.Errorf("%w: deletion request %d is in status %d", ErrProposalState, id, r.Status)
		}
		return fn(tx, r)
	})
}

func lockDeletionRequest(tx *gorm.DB, id int64) (*model.CatalogDeletionRequest, error) {
	var r model.CatalogDeletionRequest
	if err := tx.Raw(`SELECT * FROM catalog_deletion_request WHERE id = ? FOR UPDATE`, id).Scan(&r).Error; err != nil {
		return nil, err
	}
	if r.ID == 0 {
		return nil, fmt.Errorf("%w: deletion request %d", ErrNotFound, id)
	}
	return &r, nil
}

type lockedWork struct {
	ID        int64
	Status    int16
	DeletedAt *time.Time
}

func lockWork(tx *gorm.DB, id int64) (*lockedWork, error) {
	var w lockedWork
	if err := tx.Raw(`SELECT id, status, deleted_at FROM catalog_work WHERE id = ? FOR UPDATE`, id).Scan(&w).Error; err != nil {
		return nil, err
	}
	if w.ID == 0 {
		return nil, nil
	}
	return &w, nil
}

func refreshHiddenWorkUserData(ctx context.Context, tx *gorm.DB, workID int64) error {
	if err := tx.Exec(`
		UPDATE catalog_user_folder f
		   SET item_count = (SELECT count(*) FROM catalog_user_folder_item i
		                      WHERE i.folder_id = f.id AND `+liveWorkCond("i.work_id")+`),
		       updated_at = now()
		 WHERE f.id IN (SELECT folder_id FROM catalog_user_folder_item WHERE work_id = ?)`, workID).Error; err != nil {
		return fmt.Errorf("recount folders: %w", err)
	}
	if err := tx.Exec(`UPDATE catalog_user_work_rating SET changed_at = now() WHERE work_id = ?`, workID).Error; err != nil {
		return fmt.Errorf("resurface ratings: %w", err)
	}
	var related []int64
	if err := tx.Raw(`
		SELECT b_work_id FROM catalog_work_relation WHERE a_work_id = ?
		UNION
		SELECT a_work_id FROM catalog_work_relation WHERE b_work_id = ?`, workID, workID).Scan(&related).Error; err != nil {
		return err
	}
	return repository.TouchWorks(ctx, tx, related)
}
