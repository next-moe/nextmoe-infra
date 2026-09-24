package service

import (
	"context"
	stderrors "errors"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/pkg/errors"

	"gorm.io/gorm"
)

func seedDeletable(t *testing.T, db *gorm.DB, tag string) *model.User {
	t.Helper()
	pw := "hash"
	u := &model.User{Name: "dl" + tag, Email: "dl-" + tag + "@test.local", Password: &pw, Bio: "about me"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM oauth_accounts WHERE user_id = ?`, `DELETE FROM user_preferences WHERE user_id = ?`,
			`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM user_roles WHERE user_id = ?`,
			`DELETE FROM users WHERE id = ?`,
		} {
			db.Exec(q, u.ID)
		}
	})
	return u
}

func deletionServices(db *gorm.DB) (*AuthService, *AdminService, *memKV) {
	repo := repository.NewUserRepository(db)
	kv := newMemKV()
	auth := &AuthService{userRepo: repo, codes: kv}
	admin := &AdminService{userRepo: repo, sessionRepo: repository.NewSessionRepository(db)}
	return auth, admin, kv
}

func rowsOf(t *testing.T, db *gorm.DB, table string, userID uint) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestAccountDeletionWaitsOutTheGraceAndErasesTheAccount(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := seedDeletable(t, db, tag)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Create(&model.OAuthAccount{UserID: u.ID, Provider: "github", ProviderAccountID: "gh-" + tag}).Error)
	must(db.Create(&model.UserPreference{UserID: u.ID, Namespace: "kungal", Doc: []byte(`{}`), Version: 1}).Error)
	must(db.Create(&model.Session{UserID: u.ID, SessionToken: "s-" + tag, RefreshToken: "r-" + tag,
		ExpiresAt: time.Now().Add(time.Hour)}).Error)

	auth, admin, kv := deletionServices(db)
	must(auth.SendDeletionCode(ctx, u.UUID))
	code, _ := kv.Get(deletionCodeKey(u.UUID))
	if len(code) != 6 {
		t.Fatalf("no code stored: %q", code)
	}

	wrong := "000000"
	if string(code) == wrong {
		wrong = "111111"
	}
	if _, err := auth.RequestDeletion(ctx, u.UUID, wrong); !isCode(err, errors.ErrAuthCodeInvalid) {
		t.Fatalf("a wrong code must be refused, got %v", err)
	}
	due, err := auth.RequestDeletion(ctx, u.UUID, string(code))
	must(err)
	if d := time.Until(due); d < AccountDeletionGrace-time.Minute || d > AccountDeletionGrace {
		t.Fatalf("due in %v, want the %v grace", d, AccountDeletionGrace)
	}
	if _, err := auth.RequestDeletion(ctx, u.UUID, string(code)); !isCode(err, errors.ErrAuthCodeExpired) {
		t.Fatalf("a used code must not work twice, got %v", err)
	}

	n, err := admin.ExecuteDueDeletions(ctx, time.Now())
	must(err)
	var still model.User
	must(db.First(&still, u.ID).Error)
	if still.AnonymizedAt != nil {
		t.Fatalf("erased before the grace ran out (%d run)", n)
	}

	n, err = admin.ExecuteDueDeletions(ctx, due.Add(time.Second))
	must(err)
	if n < 1 {
		t.Fatal("a due deletion was not executed")
	}
	var gone model.User
	must(db.First(&gone, u.ID).Error)
	if gone.Name != "已注销#"+strconv.FormatUint(uint64(u.ID), 10) || gone.Email == u.Email ||
		gone.Password != nil || gone.Bio != "" || gone.OriginalEmail != nil ||
		gone.AnonymizedAt == nil || gone.DeletionDueAt != nil || gone.Status != 1 {
		t.Fatalf("not erased: %+v", gone)
	}
	for _, table := range []string{"oauth_accounts", "user_preferences", "sessions"} {
		if c := rowsOf(t, db, table, u.ID); c != 0 {
			t.Fatalf("%s kept %d row(s) of a deleted account", table, c)
		}
	}
	if n, _ := admin.ExecuteDueDeletions(ctx, due.Add(time.Hour)); n != 0 {
		t.Fatalf("a finished deletion ran again: %d", n)
	}
}

func TestACancelledDeletionIsNeverExecuted(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	u := seedDeletable(t, db, strconv.FormatInt(time.Now().UnixNano(), 36))
	auth, admin, kv := deletionServices(db)
	if err := auth.SendDeletionCode(ctx, u.UUID); err != nil {
		t.Fatal(err)
	}
	code, _ := kv.Get(deletionCodeKey(u.UUID))
	due, err := auth.RequestDeletion(ctx, u.UUID, string(code))
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.CancelDeletion(ctx, u.UUID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecuteDueDeletions(ctx, due.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var kept model.User
	if err := db.First(&kept, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.AnonymizedAt != nil || kept.Name != u.Name || kept.DeletionDueAt != nil {
		t.Fatalf("a cancelled deletion touched the account: %+v", kept)
	}
}

func TestAnAdminCannotStartItsOwnDeletion(t *testing.T) {
	db := requireDB(t)
	u := seedDeletable(t, db, strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := repository.NewUserRepository(db).AddRole(context.Background(), u.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	auth, _, _ := deletionServices(db)
	if err := auth.SendDeletionCode(context.Background(), u.UUID); !isCode(err, errors.ErrAuthDeletionProtected) {
		t.Fatalf("want ErrAuthDeletionProtected, got %v", err)
	}
}

// An old account deleted late must still reach a consumer that has already
// paged past its id.
func TestTheDeletedFeedPagesInDeletionOrder(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	old := seedDeletable(t, db, tag+"a")
	young := seedDeletable(t, db, tag+"b")
	repo := repository.NewUserRepository(db)
	base := time.Now().Add(24 * time.Hour).Truncate(time.Microsecond)
	for _, step := range []struct {
		u  *model.User
		at time.Time
	}{{young, base}, {old, base.Add(time.Minute)}} {
		if _, err := repo.EraseAccount(ctx, step.u.ID, step.at); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewUserBatchService(repo, nil)
	cursor := strconv.FormatInt(base.Add(-time.Second).UnixNano(), 10) + ".0"
	first, err := svc.ListDeleted(ctx, cursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListDeleted(ctx, first.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Users) != 1 || first.Users[0].ID != young.ID || len(second.Users) != 1 || second.Users[0].ID != old.ID {
		t.Fatalf("want %d then %d, got %+v then %+v", young.ID, old.ID, first.Users, second.Users)
	}
	last, err := svc.ListDeleted(ctx, second.NextCursor, 1)
	if err != nil || len(last.Users) != 0 || last.NextCursor != second.NextCursor {
		t.Fatalf("an exhausted feed must hand the same cursor back: %+v %v", last, err)
	}

	batch, err := svc.GetBriefs(ctx, []uint{old.ID}, 0)
	if err != nil || len(batch.Users) != 1 || batch.Users[0].AnonymizedAt == nil {
		t.Fatalf("/users/batch must mark the deleted account: %+v %v", batch, err)
	}
	if _, err := svc.ListDeleted(ctx, "not-a-cursor", 1); !stderrors.Is(err, ErrBadDeletedCursor) {
		t.Fatalf("a malformed cursor must be refused, got %v", err)
	}
}

func isCode(err error, code int) bool {
	var appErr *errors.AppError
	return stderrors.As(err, &appErr) && appErr.Code == code
}
