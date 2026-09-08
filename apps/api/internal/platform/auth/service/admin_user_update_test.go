package service

import (
	"testing"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	siteModel "api/internal/platform/site/model"
	"api/pkg/errors"
)

func userWithRoles(names ...string) *model.User {
	roles := make([]siteModel.Role, len(names))
	for i, n := range names {
		roles[i] = siteModel.Role{Name: n}
	}
	return &model.User{Roles: roles}
}

func TestAdminProtectedCoversRen(t *testing.T) {
	cases := []struct {
		roles []string
		want  bool
	}{
		{nil, false},
		{[]string{"user"}, false},
		{[]string{"creator", "moderator"}, false},
		{[]string{"admin"}, true},
		{[]string{"ren"}, true},
	}
	for _, c := range cases {
		if got := adminProtected(userWithRoles(c.roles...)); got != c.want {
			t.Errorf("adminProtected(%v) = %v, want %v", c.roles, got, c.want)
		}
	}
}

func TestAuthorizeUserUpdate(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }

	plainAdmin := UserUpdateActor{}
	pIIAdmin := UserUpdateActor{CanSeePII: true}
	ren := UserUpdateActor{CanSeePII: true, CanManageAdmins: true}

	cases := []struct {
		name    string
		target  *model.User
		req     *dto.UpdateUserRequest
		actor   UserUpdateActor
		wantErr bool
	}{
		{"admin edits an ordinary user's name", userWithRoles("user"), &dto.UpdateUserRequest{Name: str("kun")}, plainAdmin, false},
		{"admin edits an ordinary user's bio", userWithRoles(), &dto.UpdateUserRequest{Bio: str("hi")}, plainAdmin, false},
		{"admin without PII writes an email", userWithRoles("user"), &dto.UpdateUserRequest{Email: str("a@b.com")}, plainAdmin, true},
		{"admin with PII writes an email", userWithRoles("user"), &dto.UpdateUserRequest{Email: str("a@b.com")}, pIIAdmin, false},
		{"admin edits another admin", userWithRoles("admin"), &dto.UpdateUserRequest{Name: str("kun")}, pIIAdmin, true},
		{"admin edits ren", userWithRoles("ren"), &dto.UpdateUserRequest{Bio: str("hi")}, pIIAdmin, true},
		{"ren edits an admin", userWithRoles("admin"), &dto.UpdateUserRequest{Name: str("kun")}, ren, false},
		{"ren bans an admin", userWithRoles("admin"), &dto.UpdateUserRequest{Status: num(1)}, ren, true},
		{"ren unbans an admin", userWithRoles("admin"), &dto.UpdateUserRequest{Status: num(0)}, ren, false},
		{"admin bans an ordinary user", userWithRoles("user"), &dto.UpdateUserRequest{Status: num(1)}, plainAdmin, false},
	}

	for _, c := range cases {
		err := authorizeUserUpdate(c.target, c.req, c.actor)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: authorizeUserUpdate = %v, wantErr %v", c.name, err, c.wantErr)
			continue
		}
		if err != nil && !errors.Is(err, errors.ErrForbidden) {
			t.Errorf("%s: got code %v, want ErrForbidden", c.name, err)
		}
	}
}
