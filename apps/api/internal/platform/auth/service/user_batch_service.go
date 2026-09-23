package service

import (
	"context"
	"log/slog"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	shopModel "api/internal/platform/shop/model"
)

type CosmeticsSource interface {
	CosmeticsFor(ctx context.Context, userIDs []uint, siteID uint) (map[uint]shopModel.Cosmetics, error)
}

type UserBatchService struct {
	userRepo     *repository.UserRepository
	siteRoleRepo *repository.UserSiteRoleRepository
	cosmetics    CosmeticsSource
}

func NewUserBatchService(userRepo *repository.UserRepository, siteRoleRepo *repository.UserSiteRoleRepository) *UserBatchService {
	return &UserBatchService{userRepo: userRepo, siteRoleRepo: siteRoleRepo}
}

func (s *UserBatchService) WithCosmetics(src CosmeticsSource) *UserBatchService {
	s.cosmetics = src
	return s
}

func (s *UserBatchService) GetBriefs(ctx context.Context, ids []uint, siteID uint) (*dto.BatchGetUsersResponse, error) {
	users, err := s.userRepo.FindByIDsWithRoles(ctx, ids)
	if err != nil {
		return nil, err
	}

	found := make(map[uint]struct{}, len(users))
	briefs := make([]dto.UserBrief, 0, len(users))
	for _, u := range users {
		found[u.ID] = struct{}{}
		briefs = append(briefs, toBrief(&u))
	}

	if siteID != 0 && s.siteRoleRepo != nil {
		if byUser, err := s.siteRoleRepo.ActiveRoleNamesForUsers(ctx, ids, siteID); err == nil {
			for i := range briefs {
				briefs[i].SiteRoles = byUser[briefs[i].ID]
			}
		}
	}

	if s.cosmetics != nil {
		if worn, err := s.cosmetics.CosmeticsFor(ctx, ids, siteID); err != nil {
			slog.Warn("users batch: cosmetics lookup failed; answering without them", "err", err)
		} else {
			for i := range briefs {
				briefs[i].Cosmetics = worn[briefs[i].ID]
			}
		}
	}

	notFound := make([]uint, 0)
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			notFound = append(notFound, id)
		}
	}

	return &dto.BatchGetUsersResponse{
		Users:    briefs,
		NotFound: notFound,
	}, nil
}

func (s *UserBatchService) SearchByName(ctx context.Context, query string, limit int) (*dto.SearchUsersResponse, error) {
	users, err := s.userRepo.SearchByName(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	briefs := make([]dto.UserBrief, 0, len(users))
	for i := range users {
		briefs = append(briefs, toBrief(&users[i]))
	}
	return &dto.SearchUsersResponse{Users: briefs}, nil
}

func toBrief(u *model.User) dto.UserBrief {
	roles := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		roles = append(roles, r.Name)
	}
	return dto.UserBrief{
		ID:              u.ID,
		UUID:            u.UUID,
		Name:            u.Name,
		Avatar:          u.Avatar,
		AvatarImageHash: u.AvatarImageHash,
		Bio:             u.Bio,
		Status:          u.Status,
		Roles:           roles,
		CreatedAt:       u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
