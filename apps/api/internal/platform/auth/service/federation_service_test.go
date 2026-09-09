package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/federation"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/utils"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: cannot open the assigned test database: %v\n", err)
			os.Exit(1)
		}
		if err := db.AutoMigrate(
			&model.User{},
			&model.Session{},
			&model.OAuthAccount{},
			&siteModel.Role{},
		); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: migrate: %v\n", err)
			os.Exit(1)
		}
		if err := db.Exec(`INSERT INTO roles (name, description) VALUES ('admin', ''), ('ren', '') ON CONFLICT (name) DO NOTHING`).Error; err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: seed roles: %v\n", err)
			os.Exit(1)
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	return testDB
}

type memItem struct {
	val []byte
	exp time.Time
}

type memKV struct {
	mu   sync.Mutex
	data map[string]memItem
}

func newMemKV() *memKV {
	return &memKV{data: make(map[string]memItem)}
}

func (m *memKV) Set(key string, value []byte, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	exp := time.Time{}
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	m.data[key] = memItem{val: cp, exp: exp}
	return nil
}

func (m *memKV) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	if !item.exp.IsZero() && time.Now().After(item.exp) {
		delete(m.data, key)
		return nil, nil
	}
	cp := make([]byte, len(item.val))
	copy(cp, item.val)
	return cp, nil
}

func (m *memKV) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

type fakeProvider struct {
	name  string
	ident *federation.Identity
	err   error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) AuthorizeURL(state, nonce, redirectURI string) string {
	return "https://example.invalid/authorize?state=" + state + "&nonce=" + nonce
}

func (f *fakeProvider) Exchange(ctx context.Context, code, redirectURI, nonce string) (*federation.Identity, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.ident == nil {
		return nil, fmt.Errorf("missing identity")
	}
	cp := *f.ident
	if cp.Provider == "" {
		cp.Provider = f.name
	}
	return &cp, nil
}

type fedHarness struct {
	db       *gorm.DB
	svc      *FederationService
	kv       *memKV
	reg      *federation.Registry
	users    *repository.UserRepository
	sessions *repository.SessionRepository
	oauth    *repository.OAuthAccountRepository
	cfg      *config.Config
}

func newFedHarness(t *testing.T) *fedHarness {
	t.Helper()
	db := requireDB(t)
	userRepo := repository.NewUserRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	oauthRepo := repository.NewOAuthAccountRepository(db)
	cfg := &config.Config{
		JWT: config.JWTConfig{Secret: "test-secret-key-for-testing-only"},
		Server: config.ServerConfig{
			SiteURL:     "http://127.0.0.1:9277",
			FrontendURL: "http://127.0.0.1:9420",
		},
	}
	authSvc := NewAuthService(userRepo, sessionRepo, cfg.JWT)
	reg := federation.NewRegistry(&config.Config{})
	kv := newMemKV()
	svc := NewFederationService(authSvc, userRepo, oauthRepo, sessionRepo, nil, cfg, reg)
	svc.kv = kv
	settings.Override(t, keys.AuthFederationProviders, []string{"google"})
	return &fedHarness{
		db: db, svc: svc, kv: kv, reg: reg,
		users: userRepo, sessions: sessionRepo, oauth: oauthRepo, cfg: cfg,
	}
}

var uniqN atomic.Int64

// Salted with startup time: rows can survive a failed cleanup, and a bare
// restart-at-1 counter regenerated the same emails and hit idx_users_email.
var uniqSalt = time.Now().UnixNano() % 1_000_000

func uniq(prefix string) string {
	return fmt.Sprintf("%s%06d%04d", prefix, uniqSalt, uniqN.Add(1))
}

func (h *fedHarness) createUser(t *testing.T, name, email string, banned bool, roles ...string) *model.User {
	t.Helper()
	hash, err := utils.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	u := &model.User{Name: name, Email: model.NormalizeEmail(email), Password: &hash}
	if banned {
		u.Status = 1
	}
	ctx := context.Background()
	if err := h.users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.db.Where("user_id = ?", u.ID).Delete(&model.OAuthAccount{})
		h.db.Where("user_id = ?", u.ID).Delete(&model.Session{})
		// user_roles rows block the user delete (no cascade); remove them first.
		h.db.Exec("DELETE FROM user_roles WHERE user_id = ?", u.ID)
		if err := h.db.Unscoped().Where("id = ?", u.ID).Delete(&model.User{}).Error; err != nil {
			t.Errorf("cleanup: delete user %d: %v", u.ID, err)
		}
	})
	for _, role := range roles {
		if err := h.users.AddRole(ctx, u.ID, role); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := h.users.FindByIDWithRoles(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func (h *fedHarness) link(t *testing.T, userID uint, provider, subject string) {
	t.Helper()
	if err := h.oauth.Create(context.Background(), &model.OAuthAccount{
		UserID: userID, Provider: provider, ProviderAccountID: subject,
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *fedHarness) callback(t *testing.T, ident *federation.Identity, exchangeErr error) (*CallbackResult, error) {
	t.Helper()
	h.reg.Register(&fakeProvider{name: "google", ident: ident, err: exchangeErr})
	_, state, err := h.svc.Start(context.Background(), "google", "")
	if err != nil {
		t.Fatal(err)
	}
	return h.svc.Callback(context.Background(), "google", "code", state, state, SessionMeta{
		UserAgent: "test-ua",
		IPAddress: "127.0.0.1",
		BrowserID: "browser-1",
	})
}

func linkCount(t *testing.T, db *gorm.DB, userID uint, provider string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.OAuthAccount{}).
		Where("user_id = ? AND provider = ?", userID, provider).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestResolveFederationRedirect(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{
		FrontendURL: "http://127.0.0.1:9420",
		SiteURL:     "http://127.0.0.1:9277",
	}}
	cases := []struct {
		in, want string
	}{
		{"", "http://127.0.0.1:9420/profile"},
		{"/x", "http://127.0.0.1:9420/x"},
		{"//evil", "http://127.0.0.1:9420/profile"},
		{"https://evil.com/x", "http://127.0.0.1:9420/profile"},
		{"http://127.0.0.1:9420/foo", "http://127.0.0.1:9420/foo"},
		{"http://127.0.0.1:9277/bar", "http://127.0.0.1:9277/bar"},
	}
	for _, tc := range cases {
		if got := resolveFederationRedirect(cfg, tc.in); got != tc.want {
			t.Errorf("resolveFederationRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFederationStart_disabled(t *testing.T) {
	settings.Override(t, keys.AuthFederationProviders, []string{})
	reg := federation.NewRegistry(&config.Config{})
	reg.Register(&fakeProvider{name: "google", ident: &federation.Identity{Subject: "1"}})
	svc := NewFederationService(nil, nil, nil, nil, nil, &config.Config{
		Server: config.ServerConfig{SiteURL: "http://127.0.0.1:9277", FrontendURL: "http://127.0.0.1:9420"},
	}, reg)
	svc.kv = newMemKV()
	_, _, err := svc.Start(context.Background(), "google", "")
	if !errors.Is(err, errors.ErrAuthFederationDisabled) {
		t.Fatalf("Start disabled: err = %v, want ErrAuthFederationDisabled", err)
	}
}

func TestFederationStart_setsStateAndAuthorizeURL(t *testing.T) {
	settings.Override(t, keys.AuthFederationProviders, []string{"google"})
	reg := federation.NewRegistry(&config.Config{})
	reg.Register(&fakeProvider{name: "google", ident: &federation.Identity{Subject: "1"}})
	svc := NewFederationService(nil, nil, nil, nil, nil, &config.Config{
		Server: config.ServerConfig{SiteURL: "http://127.0.0.1:9277", FrontendURL: "http://127.0.0.1:9420"},
	}, reg)
	kv := newMemKV()
	svc.kv = kv
	authURL, state, err := svc.Start(context.Background(), "google", "/x")
	if err != nil {
		t.Fatal(err)
	}
	if state == "" {
		t.Fatal("expected state")
	}
	if !strings.HasPrefix(authURL, "https://example.invalid/authorize?state=") {
		t.Fatalf("authorize URL = %q", authURL)
	}
	raw, err := kv.Get(federationStateKeyPrefix + state)
	if err != nil || len(raw) == 0 {
		t.Fatal("expected federation_state in cache")
	}
}

func TestCallback_linkHitLogin(t *testing.T) {
	h := newFedHarness(t)
	sub := uniq("s")
	u := h.createUser(t, uniq("n"), uniq("f")+"@gmail.com", false)
	h.link(t, u.ID, "google", sub)

	result, err := h.callback(t, &federation.Identity{Subject: sub, Email: u.Email, EmailVerified: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CallbackOutcomeLogin || result.Tokens == nil {
		t.Fatalf("outcome = %+v", result)
	}
	if _, err := h.sessions.FindByRefreshToken(context.Background(), result.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
}

func TestCallback_linkHitBanned(t *testing.T) {
	h := newFedHarness(t)
	sub := uniq("s")
	u := h.createUser(t, uniq("n"), uniq("f")+"@gmail.com", true)
	h.link(t, u.ID, "google", sub)

	_, err := h.callback(t, &federation.Identity{Subject: sub}, nil)
	if CallbackRedirectCode(err) != "federation_banned" {
		t.Fatalf("err = %v, want federation_banned", err)
	}
}

func TestCallback_linkHitAdminRoleStepup(t *testing.T) {
	h := newFedHarness(t)
	sub := uniq("s")
	u := h.createUser(t, uniq("n"), uniq("f")+"@gmail.com", false, "admin")
	h.link(t, u.ID, "google", sub)

	_, err := h.callback(t, &federation.Identity{Subject: sub}, nil)
	if CallbackRedirectCode(err) != "federation_stepup" {
		t.Fatalf("err = %v, want federation_stepup", err)
	}
}

func TestCallback_verifiedMatchingEmailAutoLinksAndLogsIn(t *testing.T) {
	h := newFedHarness(t)
	email := uniq("f") + "@gmail.com"
	u := h.createUser(t, uniq("n"), email, false)
	sub := uniq("s")

	result, err := h.callback(t, &federation.Identity{
		Subject: sub, Email: email, EmailVerified: true, Name: "FromGoogle",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CallbackOutcomeLogin {
		t.Fatalf("outcome = %s", result.Outcome)
	}
	if linkCount(t, h.db, u.ID, "google") != 1 {
		t.Fatal("expected auto-created oauth_accounts row")
	}
	acc, err := h.oauth.FindByProviderSubject(context.Background(), "google", sub)
	if err != nil {
		t.Fatal(err)
	}
	if acc.AccessToken != nil || acc.RefreshToken != nil {
		t.Fatal("upstream tokens must be left NULL")
	}
}

func TestCallbackDoesNotAutoLinkUnverifiedEmailMatchingExistingUser(t *testing.T) {
	h := newFedHarness(t)
	email := uniq("f") + "@gmail.com"
	u := h.createUser(t, uniq("n"), email, false)
	sub := uniq("s")

	result, err := h.callback(t, &federation.Identity{
		Subject: sub, Email: email, EmailVerified: false,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CallbackOutcomePending {
		t.Fatalf("outcome = %s, want pending (account-takeover guard)", result.Outcome)
	}
	if linkCount(t, h.db, u.ID, "google") != 0 {
		t.Fatal("unverified email must not create a link row")
	}
}

func TestCallback_verifiedEmailSameProviderAlreadyLinked_conflict(t *testing.T) {
	h := newFedHarness(t)
	email := uniq("f") + "@gmail.com"
	u := h.createUser(t, uniq("n"), email, false)
	h.link(t, u.ID, "google", uniq("old"))

	_, err := h.callback(t, &federation.Identity{
		Subject: uniq("new"), Email: email, EmailVerified: true,
	}, nil)
	if CallbackRedirectCode(err) != "federation_conflict" {
		t.Fatalf("err = %v, want federation_conflict", err)
	}
}

func TestCallback_verifiedMatchingEmailBanned(t *testing.T) {
	h := newFedHarness(t)
	email := uniq("f") + "@gmail.com"
	h.createUser(t, uniq("n"), email, true)

	_, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: email, EmailVerified: true,
	}, nil)
	if CallbackRedirectCode(err) != "federation_banned" {
		t.Fatalf("err = %v, want federation_banned", err)
	}
}

func TestCallback_verifiedMatchingEmailAdminStepup(t *testing.T) {
	h := newFedHarness(t)
	email := uniq("f") + "@gmail.com"
	h.createUser(t, uniq("n"), email, false, "admin")

	_, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: email, EmailVerified: true,
	}, nil)
	if CallbackRedirectCode(err) != "federation_stepup" {
		t.Fatalf("err = %v, want federation_stepup", err)
	}
}

func TestCallback_noEmail_pending(t *testing.T) {
	h := newFedHarness(t)
	result, err := h.callback(t, &federation.Identity{Subject: uniq("s"), Name: "N"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CallbackOutcomePending {
		t.Fatalf("outcome = %s, want pending", result.Outcome)
	}
}

func TestCallback_verifiedEmailNoMatchingUser_pending(t *testing.T) {
	h := newFedHarness(t)
	result, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: uniq("f") + "@gmail.com", EmailVerified: true, Name: "New",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CallbackOutcomePending || result.PendingToken == "" {
		t.Fatalf("outcome = %+v", result)
	}
}

func TestCallback_exchangeFail(t *testing.T) {
	h := newFedHarness(t)
	_, err := h.callback(t, &federation.Identity{Subject: "x"}, fmt.Errorf("upstream"))
	if CallbackRedirectCode(err) != "federation_failed" {
		t.Fatalf("err = %v, want federation_failed", err)
	}
}

func TestCallback_stateMismatch(t *testing.T) {
	h := newFedHarness(t)
	h.reg.Register(&fakeProvider{name: "google", ident: &federation.Identity{Subject: "1"}})
	_, state, err := h.svc.Start(context.Background(), "google", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.Callback(context.Background(), "google", "code", state, "other", SessionMeta{})
	if CallbackRedirectCode(err) != "federation_state" {
		t.Fatalf("err = %v, want federation_state", err)
	}
}

func TestComplete_manualEmailRequiresRegisterCode(t *testing.T) {
	h := newFedHarness(t)
	result, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: uniq("f") + "@gmail.com", EmailVerified: false, Name: "N",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := &dto.FederationCompleteRequest{
		Token:    result.PendingToken,
		Name:     uniq("n"),
		Password: "secret12",
		Email:    uniq("f") + "@gmail.com",
		Code:     "000000",
	}
	_, _, err = h.svc.Complete(context.Background(), req)
	if !errors.Is(err, errors.ErrAuthCodeExpired) {
		t.Fatalf("without stored code: err = %v, want ErrAuthCodeExpired", err)
	}

	email := uniq("f") + "@gmail.com"
	code := "123456"
	if err := h.kv.Set("register_code:"+email, []byte(code), time.Minute); err != nil {
		t.Fatal(err)
	}
	req.Email = email
	req.Code = "999999"
	_, _, err = h.svc.Complete(context.Background(), req)
	if !errors.Is(err, errors.ErrAuthCodeInvalid) {
		t.Fatalf("wrong code: err = %v, want ErrAuthCodeInvalid", err)
	}

	req.Code = code
	tokens, user, err := h.svc.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tokens == nil || user == nil {
		t.Fatal("expected user and tokens")
	}
	t.Cleanup(func() {
		h.db.Where("user_id = ?", user.ID).Delete(&model.OAuthAccount{})
		h.db.Where("user_id = ?", user.ID).Delete(&model.Session{})
		h.db.Unscoped().Where("id = ?", user.ID).Delete(&model.User{})
	})
}

func TestComplete_emailLockedIgnoresBogusReqEmail(t *testing.T) {
	h := newFedHarness(t)
	pendingEmail := uniq("f") + "@gmail.com"
	result, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: pendingEmail, EmailVerified: true, Name: "N",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := h.svc.Pending(context.Background(), result.PendingToken)
	if err != nil {
		t.Fatal(err)
	}
	if !info.EmailLocked {
		t.Fatal("expected EmailLocked")
	}

	bogus := uniq("b") + "@gmail.com"
	tokens, user, err := h.svc.Complete(context.Background(), &dto.FederationCompleteRequest{
		Token:    result.PendingToken,
		Name:     uniq("n"),
		Password: "secret12",
		Email:    bogus,
		Code:     "000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.db.Where("user_id = ?", user.ID).Delete(&model.OAuthAccount{})
		h.db.Where("user_id = ?", user.ID).Delete(&model.Session{})
		h.db.Unscoped().Where("id = ?", user.ID).Delete(&model.User{})
	})
	if user.Email != model.NormalizeEmail(pendingEmail) {
		t.Fatalf("email = %q, want pending %q", user.Email, pendingEmail)
	}
	if tokens == nil {
		t.Fatal("expected tokens")
	}
}

func TestComplete_successCreatesHashedPasswordLinkAndSession(t *testing.T) {
	h := newFedHarness(t)
	pendingEmail := uniq("f") + "@gmail.com"
	sub := uniq("s")
	result, err := h.callback(t, &federation.Identity{
		Subject: sub, Email: pendingEmail, EmailVerified: true, Name: "N",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	password := "secret12"
	name := uniq("n")
	tokens, user, err := h.svc.Complete(context.Background(), &dto.FederationCompleteRequest{
		Token:     result.PendingToken,
		Name:      name,
		Password:  password,
		UserAgent: "ua",
		IPAddress: "127.0.0.1",
		BrowserID: "b1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.db.Where("user_id = ?", user.ID).Delete(&model.OAuthAccount{})
		h.db.Where("user_id = ?", user.ID).Delete(&model.Session{})
		h.db.Unscoped().Where("id = ?", user.ID).Delete(&model.User{})
	})
	if user.Password == nil {
		t.Fatal("password must be set")
	}
	ok, err := utils.VerifyPassword(password, *user.Password)
	if err != nil || !ok {
		t.Fatal("password hash did not verify")
	}
	acc, err := h.oauth.FindByProviderSubject(context.Background(), "google", sub)
	if err != nil {
		t.Fatal(err)
	}
	if acc.UserID != user.ID {
		t.Fatalf("link user_id = %d, want %d", acc.UserID, user.ID)
	}
	if _, err := h.sessions.FindByRefreshToken(context.Background(), tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.Pending(context.Background(), result.PendingToken)
	if !errors.Is(err, errors.ErrAuthFederationExpired) {
		t.Fatalf("pending should be consumed, err = %v", err)
	}
}
