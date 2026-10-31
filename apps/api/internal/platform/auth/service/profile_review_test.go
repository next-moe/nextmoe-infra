package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteModel "api/internal/platform/site/model"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

type fakeProfileTrust struct {
	decision string
	checkErr error
	checked  []string
	scans    chan trustclient.ScanRequest
}

func newFakeProfileTrust() *fakeProfileTrust {
	return &fakeProfileTrust{decision: "allow", scans: make(chan trustclient.ScanRequest, 8)}
}

func (f *fakeProfileTrust) Check(_ context.Context, req trustclient.CheckRequest) (string, []string, error) {
	f.checked = append(f.checked, req.Text)
	return f.decision, nil, f.checkErr
}

func (f *fakeProfileTrust) Scan(_ context.Context, req trustclient.ScanRequest) (int64, bool, error) {
	f.scans <- req
	return 1, false, nil
}

func (f *fakeProfileTrust) nextScan(t *testing.T) (trustclient.ScanRequest, bool) {
	t.Helper()
	select {
	case r := <-f.scans:
		return r, true
	case <-time.After(300 * time.Millisecond):
		return trustclient.ScanRequest{}, false
	}
}

func TestSolicitationRuleOnTheBannedShapes(t *testing.T) {
	refused := []string{
		"台灣學生妹TGxs9805",
		"朵朵外約TG搜mt1689",
		"【 全 台 喝 茶 找 小 姐 資 訊 總 預 覽 】約 會 G l e e z y ： l g 5 2 21或 私 約 TG 搜 l g 5 2 2 1",
		"东京大阪外送 Telegram搜索 @tokysak8 外約流程：@tokysak88",
		"安琪外約-本土高端外約茶莊 賴043861",
	}
	for _, s := range refused {
		if !solicitsWithContact(s) {
			t.Errorf("not refused: %q", s)
		}
	}
	kept := []string{
		"前萤之光站长，导航站auroralili.cloudns.ch",
		"不玩NTR 我发布的资源不是开袋即食，需要打补丁 QQ:183097749",
		"周末喝茶看书",
		"Google搜索“全游戏无码化”",
		"1291490086",
	}
	for _, s := range kept {
		if solicitsWithContact(s) {
			t.Errorf("refused: %q", s)
		}
	}
}

func TestProfileGateAppliesToOrdinaryUsersOnly(t *testing.T) {
	ctx := context.Background()
	trust := newFakeProfileTrust()
	p := NewProfileReview(trust)
	ad := "學生妹 TG xs9805"

	settings.Override(t, keys.AuthProfileCheckEnabled, false)
	if err := p.Gate(ctx, 1, nil, ad); err != nil {
		t.Fatalf("switched off: %v", err)
	}

	settings.Override(t, keys.AuthProfileCheckEnabled, true)
	if err := p.Gate(ctx, 1, nil, ad); !errors.Is(err, errors.ErrAuthProfileRejected) {
		t.Fatalf("ordinary user, ad: %v", err)
	}
	if err := p.Gate(ctx, 1, []string{"user"}, ad); !errors.Is(err, errors.ErrAuthProfileRejected) {
		t.Fatalf("explicit user role, ad: %v", err)
	}
	if err := p.Gate(ctx, 1, []string{"creator"}, ad); err != nil {
		t.Fatalf("creator was gated: %v", err)
	}

	trust.decision = "deny"
	if err := p.Gate(ctx, 1, nil, "普通的名字"); !errors.Is(err, errors.ErrAuthProfileRejected) {
		t.Fatalf("trust deny not honoured: %v", err)
	}
	trust.decision = "hold"
	if err := p.Gate(ctx, 1, nil, "普通的名字"); err != nil {
		t.Fatalf("hold refused: %v", err)
	}
	trust.checkErr = stderrors.New("trust down")
	trust.decision = "deny"
	if err := p.Gate(ctx, 1, nil, "普通的名字"); err != nil {
		t.Fatalf("trust error did not fail open: %v", err)
	}
}

func seedProfileUser(t *testing.T, db *gorm.DB, role string) *model.User {
	t.Helper()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := &model.User{Name: "pr" + tag[len(tag)-8:], Email: "pr-" + tag + "@test.local", Bio: "旧签名"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM user_roles WHERE user_id = ?`, u.ID)
		db.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})
	if role != "" {
		r := siteModel.Role{Name: role}
		if err := db.Where("name = ?", role).FirstOrCreate(&r).Error; err != nil {
			t.Fatalf("role: %v", err)
		}
		if err := db.Exec(`INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, u.ID, r.ID).Error; err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	return u
}

func TestUpdateProfileRefusesAdsAndScansOrdinaryUsers(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	settings.Override(t, keys.AuthProfileCheckEnabled, true)
	settings.Override(t, keys.AuthProfileScanEnabled, true)
	trust := newFakeProfileTrust()
	svc := NewAuthService(repository.NewUserRepository(db), nil, config.JWTConfig{}).
		WithProfileReview(NewProfileReview(trust))

	plain := seedProfileUser(t, db, "")
	ad := "外約 TG：xs9805"
	if _, err := svc.UpdateProfile(ctx, plain.UUID, &dto.UpdateProfileRequest{Bio: &ad}); !errors.Is(err, errors.ErrAuthProfileRejected) {
		t.Fatalf("ad bio accepted: %v", err)
	}
	var kept model.User
	db.First(&kept, plain.ID)
	if kept.Bio != "旧签名" {
		t.Fatalf("a refused edit was written: %q", kept.Bio)
	}
	if _, ok := trust.nextScan(t); ok {
		t.Fatal("a refused edit was sent to trust")
	}

	clean := "喜欢纯爱"
	if _, err := svc.UpdateProfile(ctx, plain.UUID, &dto.UpdateProfileRequest{Bio: &clean}); err != nil {
		t.Fatalf("clean bio: %v", err)
	}
	scan, ok := trust.nextScan(t)
	if !ok {
		t.Fatal("clean edit was not scanned")
	}
	if scan.SubjectKind != ProfileSubjectKind || scan.SubjectID != fmt.Sprint(plain.ID) || scan.Text != "用户名: "+plain.Name+"\n签名: "+clean {
		t.Fatalf("scan = %+v", scan)
	}

	creator := seedProfileUser(t, db, "creator")
	if _, err := svc.UpdateProfile(ctx, creator.UUID, &dto.UpdateProfileRequest{Bio: &ad}); err != nil {
		t.Fatalf("creator refused: %v", err)
	}
	if _, ok := trust.nextScan(t); ok {
		t.Fatal("a creator's profile was sent to trust")
	}
}

func TestProfileEnforcerResetsOrdinaryUsersOnly(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	e := NewProfileEnforcer(repository.NewUserRepository(db))
	hide := func(u *model.User, action int16) ProfileEnforceResult {
		t.Helper()
		res, err := e.Handle(ctx, trustclient.Callback{SubjectKind: ProfileSubjectKind, SubjectID: fmt.Sprint(u.ID), Action: action})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		return res
	}
	reload := func(u *model.User) model.User {
		var got model.User
		db.First(&got, u.ID)
		return got
	}

	plain := seedProfileUser(t, db, "")
	if res := hide(plain, trustclient.ActionNone); res != ProfileEnforceSkipped || reload(plain).Name != plain.Name {
		t.Fatalf("restore changed the profile: %v", res)
	}
	if res := hide(plain, trustclient.ActionHide); res != ProfileEnforced {
		t.Fatalf("hide: %v", res)
	}
	got := reload(plain)
	if got.Name != fmt.Sprintf("用户%d", plain.ID) || got.Bio != "" || got.About != "" {
		t.Fatalf("after reset: name=%q bio=%q about=%q", got.Name, got.Bio, got.About)
	}

	creator := seedProfileUser(t, db, "creator")
	if res := hide(creator, trustclient.ActionRemove); res != ProfileEnforceSkipped || reload(creator).Bio != "旧签名" {
		t.Fatalf("creator was reset: %v", res)
	}

	res, err := e.Handle(ctx, trustclient.Callback{SubjectKind: "forum_topic", SubjectID: fmt.Sprint(plain.ID), Action: trustclient.ActionHide})
	if err != nil || res != ProfileEnforceUnsupported {
		t.Fatalf("other kind: %v %v", res, err)
	}
}
