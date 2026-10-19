package service

import (
	"context"
	stderrors "errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	"api/pkg/errors"
)

func newEditorAppRig(t *testing.T) (*EditorApplicationService, *model.User, *repository.UserRepository) {
	t.Helper()
	db := requireDB(t)
	if err := db.AutoMigrate(&model.EditorApplication{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_editor_app_pending
		ON editor_applications (user_id) WHERE status = 'pending'`).Error; err != nil {
		t.Fatalf("pending index: %v", err)
	}
	if err := db.Exec(`INSERT INTO roles (name, description) VALUES ('editor', ''), ('creator', ''), ('moderator', '')
		ON CONFLICT (name) DO NOTHING`).Error; err != nil {
		t.Fatalf("seed roles: %v", err)
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &model.User{Name: "ed" + tag, Email: "ed-" + tag + "@test.local"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM editor_applications WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM user_roles WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})
	users := repository.NewUserRepository(db)
	svc := NewEditorApplicationService(repository.NewEditorApplicationRepository(db), users, NewUserBatchService(users, nil, ""))
	return svc, user, users
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var appErr *errors.AppError
	if !stderrors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err = %v, want code %d", err, code)
	}
}

func TestEditorApplicationApprovalGrantsTheRole(t *testing.T) {
	svc, user, users := newEditorAppRig(t)
	ctx := context.Background()

	app, err := svc.Apply(ctx, user.ID, "  我常在论坛修正作品资料,想直接编辑  ")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if app.Status != model.EditorAppPending || app.Reason != "我常在论坛修正作品资料,想直接编辑" {
		t.Errorf("application = %+v", app)
	}
	_, err = svc.Apply(ctx, user.ID, "第二份申请的理由也足够长")
	wantCode(t, err, errors.ErrEditorAppPending)

	if err := svc.Approve(ctx, app.ID, 1); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, err := users.FindByIDWithRoles(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.RoleNames(), EditorRoleName) {
		t.Errorf("roles after approval = %v, want editor", got.RoleNames())
	}
	wantCode(t, svc.Approve(ctx, app.ID, 1), errors.ErrEditorAppNotPending)
	_, err = svc.Apply(ctx, user.ID, "已经是编辑者了还想再申请")
	wantCode(t, err, errors.ErrEditorAlreadyHas)
}

func TestEditorApplicationCooldownFollowsTheSetting(t *testing.T) {
	svc, user, _ := newEditorAppRig(t)
	ctx := context.Background()

	app, err := svc.Apply(ctx, user.ID, "第一次申请的理由足够长")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := svc.Decline(ctx, app.ID, 1, "资料不足"); err != nil {
		t.Fatalf("decline: %v", err)
	}

	settings.Override(t, keys.AuthEditorApplicationCooldownHours, 24)
	_, err = svc.Apply(ctx, user.ID, "被拒后立刻再申请一次")
	wantCode(t, err, errors.ErrEditorAppCooldown)

	settings.Override(t, keys.AuthEditorApplicationCooldownHours, 0)
	if _, err := svc.Apply(ctx, user.ID, "冷却期设为零后可以再申请"); err != nil {
		t.Errorf("apply with a zero cooldown: %v", err)
	}
}

func TestCreatorApplicationCooldownFollowsTheSetting(t *testing.T) {
	_, user, users := newEditorAppRig(t)
	db := requireDB(t)
	if err := db.AutoMigrate(&model.CreatorApplication{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM creator_applications WHERE user_id = ?`, user.ID) })
	svc := NewCreatorApplicationService(repository.NewCreatorApplicationRepository(db), users, NewUserBatchService(users, nil, ""))
	ctx := context.Background()

	app, err := svc.Apply(ctx, user.ID, "forum", "", nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := svc.Decline(ctx, app.ID, 1, ""); err != nil {
		t.Fatalf("decline: %v", err)
	}
	settings.Override(t, keys.AuthCreatorApplicationCooldownHours, 24)
	_, err = svc.Apply(ctx, user.ID, "forum", "", nil)
	wantCode(t, err, errors.ErrCreatorAppCooldown)

	settings.Override(t, keys.AuthCreatorApplicationCooldownHours, 0)
	if _, err := svc.Apply(ctx, user.ID, "forum", "", nil); err != nil {
		t.Errorf("apply with a zero cooldown: %v", err)
	}
}

func TestEditorApplicationRefusesPeopleWhoAlreadyEdit(t *testing.T) {
	svc, user, users := newEditorAppRig(t)
	ctx := context.Background()
	if err := users.AddRole(ctx, user.ID, "moderator"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Apply(ctx, user.ID, "版主不需要再申请编辑者")
	wantCode(t, err, errors.ErrEditorAlreadyHas)
}
