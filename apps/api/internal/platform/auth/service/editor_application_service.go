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

	"gorm.io/gorm"
)

const EditorRoleName = "editor"

var rolesThatAlreadyEdit = []string{EditorRoleName, CreatorRoleName, "moderator", "admin", "ren"}

func editorReapplyCooldown() time.Duration {
	return time.Duration(keys.AuthEditorApplicationCooldownHours.Get()) * time.Hour
}

type EditorApplicationService struct {
	repo         *repository.EditorApplicationRepository
	userRepo     *repository.UserRepository
	userBatchSvc *UserBatchService
}

func NewEditorApplicationService(repo *repository.EditorApplicationRepository, userRepo *repository.UserRepository, userBatchSvc *UserBatchService) *EditorApplicationService {
	return &EditorApplicationService{repo: repo, userRepo: userRepo, userBatchSvc: userBatchSvc}
}

func (s *EditorApplicationService) Apply(ctx context.Context, userID uint, reason string) (*model.EditorApplication, error) {
	user, err := s.userRepo.FindByIDWithRoles(ctx, userID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	if slices.ContainsFunc(user.RoleNames(), func(r string) bool { return slices.Contains(rolesThatAlreadyEdit, r) }) {
		return nil, errors.NewWithCode(errors.ErrEditorAlreadyHas)
	}

	latest, err := s.repo.FindLatestByUser(ctx, userID)
	switch {
	case err == nil:
		switch latest.Status {
		case model.EditorAppPending:
			return nil, errors.NewWithCode(errors.ErrEditorAppPending)
		case model.EditorAppDeclined:
			if latest.ReviewedAt != nil && time.Since(*latest.ReviewedAt) < editorReapplyCooldown() {
				return nil, errors.NewWithCode(errors.ErrEditorAppCooldown)
			}
		}
	case err == gorm.ErrRecordNotFound:
	default:
		return nil, err
	}

	app := &model.EditorApplication{
		UserID: userID,
		Status: model.EditorAppPending,
		Reason: strings.TrimSpace(reason),
	}
	if err := s.repo.Create(ctx, app); err != nil {
		if strings.Contains(err.Error(), "uq_editor_app_pending") {
			return nil, errors.NewWithCode(errors.ErrEditorAppPending)
		}
		return nil, err
	}
	return app, nil
}

func (s *EditorApplicationService) MyApplication(ctx context.Context, userID uint) (*model.EditorApplication, error) {
	app, err := s.repo.FindLatestByUser(ctx, userID)
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return app, err
}

type AdminEditorApplicationItem struct {
	*model.EditorApplication
	User *dto.UserBrief `json:"user"`
}

func (s *EditorApplicationService) List(ctx context.Context, status string, page, limit int) ([]AdminEditorApplicationItem, int64, error) {
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

	items := make([]AdminEditorApplicationItem, len(apps))
	for i := range apps {
		items[i] = AdminEditorApplicationItem{
			EditorApplication: &apps[i],
			User:              briefByID[apps[i].UserID],
		}
	}
	return items, total, nil
}

func (s *EditorApplicationService) Approve(ctx context.Context, id, reviewerID uint) error {
	app, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return errors.NewWithCode(errors.ErrEditorAppNotFound)
	}
	if app.Status != model.EditorAppPending {
		return errors.NewWithCode(errors.ErrEditorAppNotPending)
	}
	n, err := s.repo.Approve(ctx, id, app.UserID, reviewerID, EditorRoleName)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.NewWithCode(errors.ErrEditorAppNotPending)
	}
	return nil
}

func (s *EditorApplicationService) Decline(ctx context.Context, id, reviewerID uint, reason string) error {
	app, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return errors.NewWithCode(errors.ErrEditorAppNotFound)
	}
	if app.Status != model.EditorAppPending {
		return errors.NewWithCode(errors.ErrEditorAppNotPending)
	}
	n, err := s.repo.Decline(ctx, id, reviewerID, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.NewWithCode(errors.ErrEditorAppNotPending)
	}
	return nil
}
