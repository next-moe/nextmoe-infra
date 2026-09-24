package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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

	s.attachCosmetics(ctx, briefs, siteID)

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

func (s *UserBatchService) SearchByName(ctx context.Context, query string, limit int, siteID uint) (*dto.SearchUsersResponse, error) {
	users, err := s.userRepo.SearchByName(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	briefs := make([]dto.UserBrief, 0, len(users))
	for i := range users {
		briefs = append(briefs, toBrief(&users[i]))
	}
	s.attachCosmetics(ctx, briefs, siteID)
	return &dto.SearchUsersResponse{Users: briefs}, nil
}

func (s *UserBatchService) attachCosmetics(ctx context.Context, briefs []dto.UserBrief, siteID uint) {
	if s.cosmetics == nil || len(briefs) == 0 {
		return
	}
	ids := make([]uint, len(briefs))
	for i := range briefs {
		ids[i] = briefs[i].ID
	}
	worn, err := s.cosmetics.CosmeticsFor(ctx, ids, siteID)
	if err != nil {
		slog.Warn("users: cosmetics lookup failed; answering without them", "err", err)
		return
	}
	for i := range briefs {
		if briefs[i].AnonymizedAt == nil {
			briefs[i].Cosmetics = worn[briefs[i].ID]
		}
	}
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
		AnonymizedAt:    formatUTC(u.AnonymizedAt),
	}
}

func formatUTC(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.UTC().Format(time.RFC3339Nano)
	return &v
}

func (s *UserBatchService) ListDeleted(ctx context.Context, cursor string, limit int) (*dto.DeletedUsersResponse, error) {
	at, id, err := parseDeletedCursor(cursor)
	if err != nil {
		return nil, err
	}
	rows, err := s.userRepo.ListDeletedAfter(ctx, at, id, limit)
	if err != nil {
		return nil, err
	}
	out := &dto.DeletedUsersResponse{Users: make([]dto.DeletedUser, 0, len(rows)), NextCursor: cursor}
	for _, r := range rows {
		out.Users = append(out.Users, dto.DeletedUser{ID: r.ID, UUID: r.UUID, DeletedAt: r.AnonymizedAt.UTC().Format(time.RFC3339Nano)})
		out.NextCursor = fmt.Sprintf("%d.%d", r.AnonymizedAt.UnixNano(), r.ID)
	}
	return out, nil
}

var ErrBadDeletedCursor = stderrors.New("users: malformed deleted-users cursor")

func parseDeletedCursor(cursor string) (time.Time, uint, error) {
	if cursor == "" {
		return time.Unix(0, 0), 0, nil
	}
	ns, id, ok := strings.Cut(cursor, ".")
	n, err1 := strconv.ParseInt(ns, 10, 64)
	u, err2 := strconv.ParseUint(id, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return time.Time{}, 0, ErrBadDeletedCursor
	}
	return time.Unix(0, n), uint(u), nil
}
