package discussion

import (
	"context"

	catalogPerm "api/internal/platform/catalog/perm"

	"gorm.io/gorm"
)

type Viewer struct {
	UID   int64
	Roles []string
}

type Kind struct {
	Name         string
	Public       bool
	Exists       func(ctx context.Context, db *gorm.DB, id int64) (bool, error)
	CanSee       func(ctx context.Context, db *gorm.DB, v Viewer, id int64) (bool, error)
	Owner        func(ctx context.Context, db *gorm.DB, id int64) (int64, error)
	Participants func(ctx context.Context, db *gorm.DB, id int64) ([]int64, error)
	Reviewer     func(roles []string) bool
}

const (
	KindProposal        = "proposal"
	KindMergeRequest    = "merge_request"
	KindDeletionRequest = "deletion_request"
)

func uidsOf(ctx context.Context, db *gorm.DB, sql string, args ...any) ([]int64, error) {
	var out []int64
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func exists(ctx context.Context, db *gorm.DB, sql string, args ...any) (bool, error) {
	var ok bool
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&ok).Error; err != nil {
		return false, err
	}
	return ok, nil
}

func ownerOf(ctx context.Context, db *gorm.DB, sql string, id int64) (int64, error) {
	var uid int64
	if err := db.WithContext(ctx).Raw(sql, id).Scan(&uid).Error; err != nil {
		return 0, err
	}
	return uid, nil
}

func ProposalKind() Kind {
	return Kind{
		Name:   KindProposal,
		Public: true,
		Exists: func(ctx context.Context, db *gorm.DB, id int64) (bool, error) {
			return exists(ctx, db, `SELECT EXISTS (SELECT 1 FROM edit_proposal WHERE id = ?)`, id)
		},
		Owner: func(ctx context.Context, db *gorm.DB, id int64) (int64, error) {
			return ownerOf(ctx, db, `SELECT proposer_uid FROM edit_proposal WHERE id = ?`, id)
		},
		Participants: func(ctx context.Context, db *gorm.DB, id int64) ([]int64, error) {
			return uidsOf(ctx, db, `
				SELECT proposer_uid FROM edit_proposal WHERE id = ?
				UNION SELECT decided_by_uid FROM edit_proposal WHERE id = ? AND decided_by_uid IS NOT NULL
				UNION SELECT amender_uid FROM edit_proposal_amendment WHERE proposal_id = ?`, id, id, id)
		},
		Reviewer: catalogPerm.ReviewsEdits,
	}
}

func MergeRequestKind() Kind {
	canReview := func(roles []string) bool { return catalogPerm.Resolver.Can(roles, catalogPerm.MergeReview) }
	return Kind{
		Name: KindMergeRequest,
		Exists: func(ctx context.Context, db *gorm.DB, id int64) (bool, error) {
			return exists(ctx, db, `SELECT EXISTS (SELECT 1 FROM catalog_merge_proposal WHERE id = ? AND origin = 'editor')`, id)
		},
		CanSee: func(ctx context.Context, db *gorm.DB, v Viewer, id int64) (bool, error) {
			if canReview(v.Roles) {
				return true, nil
			}
			if v.UID <= 0 {
				return false, nil
			}
			return exists(ctx, db, `SELECT EXISTS (SELECT 1 FROM catalog_merge_proposal WHERE id = ? AND proposed_by = ?)`, id, v.UID)
		},
		Owner: func(ctx context.Context, db *gorm.DB, id int64) (int64, error) {
			return ownerOf(ctx, db, `SELECT proposed_by FROM catalog_merge_proposal WHERE id = ?`, id)
		},
		Participants: func(ctx context.Context, db *gorm.DB, id int64) ([]int64, error) {
			return uidsOf(ctx, db, `
				SELECT uid FROM catalog_merge_proposal,
					unnest(ARRAY[proposed_by, amended_by, undone_by]) AS uid
				WHERE id = ? AND uid IS NOT NULL`, id)
		},
		Reviewer: canReview,
	}
}

func DeletionRequestKind() Kind {
	canReview := func(roles []string) bool { return catalogPerm.Resolver.Can(roles, catalogPerm.DeleteReview) }
	return Kind{
		Name: KindDeletionRequest,
		Exists: func(ctx context.Context, db *gorm.DB, id int64) (bool, error) {
			return exists(ctx, db, `SELECT EXISTS (SELECT 1 FROM catalog_deletion_request WHERE id = ?)`, id)
		},
		CanSee: func(ctx context.Context, db *gorm.DB, v Viewer, id int64) (bool, error) {
			if canReview(v.Roles) {
				return true, nil
			}
			if v.UID <= 0 {
				return false, nil
			}
			return exists(ctx, db, `SELECT EXISTS (SELECT 1 FROM catalog_deletion_request WHERE id = ? AND proposed_by = ?)`, id, v.UID)
		},
		Owner: func(ctx context.Context, db *gorm.DB, id int64) (int64, error) {
			return ownerOf(ctx, db, `SELECT proposed_by FROM catalog_deletion_request WHERE id = ?`, id)
		},
		Participants: func(ctx context.Context, db *gorm.DB, id int64) ([]int64, error) {
			return uidsOf(ctx, db, `
				SELECT uid FROM catalog_deletion_request,
					unnest(ARRAY[proposed_by, decided_by, executed_by, restored_by]) AS uid
				WHERE id = ? AND uid IS NOT NULL`, id)
		},
		Reviewer: canReview,
	}
}
