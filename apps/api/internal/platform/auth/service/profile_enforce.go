package service

import (
	"context"
	"fmt"
	"strconv"

	"api/internal/platform/auth/repository"
	"api/pkg/trustclient"
)

type ProfileEnforceResult int

const (
	ProfileEnforced ProfileEnforceResult = iota
	ProfileEnforceSkipped
	ProfileEnforceUnsupported
)

type ProfileEnforcer struct {
	userRepo *repository.UserRepository
}

func NewProfileEnforcer(userRepo *repository.UserRepository) *ProfileEnforcer {
	return &ProfileEnforcer{userRepo: userRepo}
}

// Handle carries out a Trust disposition on a user_profile subject: hide and
// remove both reset the name and clear the bio and about. Nothing is kept to
// put back, so a later "none" (restore) changes nothing.
func (e *ProfileEnforcer) Handle(ctx context.Context, cb trustclient.Callback) (ProfileEnforceResult, error) {
	if cb.SubjectKind != ProfileSubjectKind {
		return ProfileEnforceUnsupported, nil
	}
	switch cb.Action {
	case trustclient.ActionHide, trustclient.ActionRemove:
	case trustclient.ActionNone:
		return ProfileEnforceSkipped, nil
	default:
		return ProfileEnforceUnsupported, nil
	}
	id, err := strconv.ParseUint(cb.SubjectID, 10, 64)
	if err != nil {
		return ProfileEnforceUnsupported, nil
	}
	user, err := e.userRepo.FindByIDWithRoles(ctx, uint(id))
	if err != nil {
		return ProfileEnforceSkipped, nil
	}
	if user.IsAnonymized() || !isOrdinary(user.RoleNames()) {
		return ProfileEnforceSkipped, nil
	}

	base := fmt.Sprintf("用户%d", user.ID)
	for i := 0; i < 10; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		err := e.userRepo.UpdateProfile(ctx, user.UUID, map[string]any{"name": name, "bio": "", "about": "", "about_html": ""})
		if err == nil {
			return ProfileEnforced, nil
		}
		if !isUniqueViolation(err) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("no free reset name for user %d", user.ID)
}
