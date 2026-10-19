package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings/keys"
	"api/pkg/errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func creatorReapplyCooldown() time.Duration {
	return time.Duration(keys.AuthCreatorApplicationCooldownHours.Get()) * time.Hour
}

const CreatorRoleName = "creator"

type CreatorApplicationService struct {
	repo         *repository.CreatorApplicationRepository
	userRepo     *repository.UserRepository
	userBatchSvc *UserBatchService
}

func NewCreatorApplicationService(repo *repository.CreatorApplicationRepository, userRepo *repository.UserRepository, userBatchSvc *UserBatchService) *CreatorApplicationService {
	return &CreatorApplicationService{repo: repo, userRepo: userRepo, userBatchSvc: userBatchSvc}
}

func (s *CreatorApplicationService) Apply(ctx context.Context, userID uint, source, message string, evidence datatypes.JSON) (*model.CreatorApplication, error) {
	user, err := s.userRepo.FindByIDWithRoles(ctx, userID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	if slices.Contains(user.RoleNames(), CreatorRoleName) {
		return nil, errors.NewWithCode(errors.ErrCreatorAlreadyHas)
	}

	latest, err := s.repo.FindLatestByUser(ctx, userID)
	switch {
	case err == nil:
		switch latest.Status {
		case model.CreatorAppPending:
			return nil, errors.NewWithCode(errors.ErrCreatorAppPending)
		case model.CreatorAppDeclined:
			if latest.ReviewedAt != nil && time.Since(*latest.ReviewedAt) < creatorReapplyCooldown() {
				return nil, errors.NewWithCode(errors.ErrCreatorAppCooldown)
			}
		}
	case err == gorm.ErrRecordNotFound:
	default:
		return nil, err
	}

	app := &model.CreatorApplication{
		UserID:   userID,
		Source:   source,
		Status:   model.CreatorAppPending,
		Message:  message,
		Evidence: evidence,
	}
	if err := s.repo.Create(ctx, app); err != nil {
		if strings.Contains(err.Error(), "uq_creator_app_pending") {
			return nil, errors.NewWithCode(errors.ErrCreatorAppPending)
		}
		return nil, err
	}
	return app, nil
}

func (s *CreatorApplicationService) MyApplication(ctx context.Context, userID uint) (*model.CreatorApplication, error) {
	app, err := s.repo.FindLatestByUser(ctx, userID)
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return app, err
}

type AdminApplicationItem struct {
	*model.CreatorApplication
	User *dto.UserBrief `json:"user"`
}

func (s *CreatorApplicationService) List(ctx context.Context, status string, page, limit int) ([]AdminApplicationItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	apps, total, err := s.repo.ListByStatus(ctx, status, page, limit)
	if err != nil {
		return nil, 0, err
	}

	ids := make([]uint, 0, len(apps))
	for i := range apps {
		ids = append(ids, apps[i].UserID)
	}
	briefByID := make(map[uint]*dto.UserBrief, len(ids))
	if len(ids) > 0 {
		res, err := s.userBatchSvc.GetBriefs(ctx, ids, 0)
		if err != nil {
			return nil, 0, err
		}
		for i := range res.Users {
			briefByID[res.Users[i].ID] = &res.Users[i]
		}
	}

	items := make([]AdminApplicationItem, len(apps))
	for i := range apps {
		items[i] = AdminApplicationItem{
			CreatorApplication: &apps[i],
			User:               briefByID[apps[i].UserID],
		}
	}
	return items, total, nil
}

func (s *CreatorApplicationService) Approve(ctx context.Context, id, reviewerID uint) error {
	app, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return errors.NewWithCode(errors.ErrCreatorAppNotFound)
	}
	if app.Status != model.CreatorAppPending {
		return errors.NewWithCode(errors.ErrCreatorAppNotPending)
	}
	if err := s.userRepo.AddRole(ctx, app.UserID, CreatorRoleName); err != nil {
		return err
	}
	n, err := s.repo.Review(ctx, id, model.CreatorAppApproved, reviewerID, "")
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.NewWithCode(errors.ErrCreatorAppNotPending)
	}
	return nil
}

func (s *CreatorApplicationService) Decline(ctx context.Context, id, reviewerID uint, reason string) error {
	app, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return errors.NewWithCode(errors.ErrCreatorAppNotFound)
	}
	if app.Status != model.CreatorAppPending {
		return errors.NewWithCode(errors.ErrCreatorAppNotPending)
	}
	n, err := s.repo.Review(ctx, id, model.CreatorAppDeclined, reviewerID, reason)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.NewWithCode(errors.ErrCreatorAppNotPending)
	}
	return nil
}
