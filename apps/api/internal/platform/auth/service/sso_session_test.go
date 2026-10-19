package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/utils"

	"gorm.io/gorm"
)

const ssoTestSecret = "sso-test-secret"

type ssoRig struct {
	db     *gorm.DB
	oauth  *OAuthService
	auth   *AuthService
	user   *model.User
	client *siteModel.OAuthClient
	tag    string
}

func newSSORig(t *testing.T, unionSiteRoles bool) *ssoRig {
	t.Helper()
	db := requireDB(t)
	if err := db.AutoMigrate(&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		t.Fatalf("migrate sites/clients: %v", err)
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &model.User{Name: "sso" + tag, Email: "sso-" + tag + "@test.local"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	site := &siteModel.Site{Name: "site " + tag, Domain: "own-" + tag + ".test"}
	if err := db.Create(site).Error; err != nil {
		t.Fatalf("seed site: %v", err)
	}
	redirects, _ := json.Marshal([]string{"https://own-" + tag + ".test/cb"})
	client := &siteModel.OAuthClient{
		ID:             "c-" + tag,
		SiteID:         &site.ID,
		Name:           "client " + tag,
		Secret:         siteModel.HashOAuthClientSecret("unused"),
		RedirectURIs:   redirects,
		Grants:         []byte(`["authorization_code","refresh_token"]`),
		IsPublic:       true,
		UnionSiteRoles: unionSiteRoles,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM sessions WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM authorization_codes WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM user_site_roles WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM oauth_clients WHERE id = ?`, client.ID)
		db.Exec(`DELETE FROM sites WHERE id = ?`, site.ID)
		db.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})

	cfg := &config.Config{JWT: config.JWTConfig{Secret: ssoTestSecret}}
	sessions := repository.NewSessionRepository(db)
	users := repository.NewUserRepository(db)
	oauth := NewOAuthService(users, repository.NewAuthorizationCodeRepository(db), sessions,
		siteRepo.NewOAuthClientRepository(db), repository.NewUserSiteRoleRepository(db), cfg)
	auth := NewAuthService(users, sessions, cfg.JWT)
	return &ssoRig{db: db, oauth: oauth, auth: auth, user: user, client: client, tag: tag}
}

func (r *ssoRig) opSession(t *testing.T, browserID string, authAt, expiresAt time.Time) *model.Session {
	t.Helper()
	s := &model.Session{
		UserID: r.user.ID, SessionToken: "op-at-" + browserID + r.tag, RefreshToken: "op-rt-" + browserID + r.tag,
		BrowserID: browserID, AuthTime: &authAt, ExpiresAt: expiresAt,
	}
	if err := r.db.Create(s).Error; err != nil {
		t.Fatalf("seed op session: %v", err)
	}
	return s
}

func (r *ssoRig) clientSession(t *testing.T, browserID string) *model.Session {
	t.Helper()
	s := &model.Session{
		UserID: r.user.ID, ClientID: r.client.ID, SessionToken: "cl-at-" + r.tag, RefreshToken: "cl-rt-" + r.tag,
		SSOBrowserID: browserID, ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	}
	if err := r.db.Create(s).Error; err != nil {
		t.Fatalf("seed client session: %v", err)
	}
	return s
}

func (r *ssoRig) reload(t *testing.T, id uint) *model.Session {
	t.Helper()
	var s model.Session
	if err := r.db.First(&s, id).Error; err != nil {
		t.Fatalf("reload session %d: %v", id, err)
	}
	return &s
}

func TestClientRefreshKeepsTheBrowserSignInAlive(t *testing.T) {
	r := newSSORig(t, false)
	now := time.Now()
	op := r.opSession(t, "browser-a-"+r.tag, now.Add(-8*24*time.Hour), now.Add(time.Hour))
	cl := r.clientSession(t, "browser-a-"+r.tag)

	if _, err := r.oauth.RefreshWithClient(context.Background(), cl.RefreshToken, r.client.ID, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := r.reload(t, op.ID).ExpiresAt
	if got.Before(now.Add(29 * 24 * time.Hour)) {
		t.Errorf("op session expires %v; a forum refresh must push it to about now+30d", got)
	}
}

func TestClientRefreshNeverPassesTheAbsoluteCap(t *testing.T) {
	r := newSSORig(t, false)
	now := time.Now()
	authAt := now.Add(-364 * 24 * time.Hour)
	op := r.opSession(t, "browser-a-"+r.tag, authAt, now.Add(time.Hour))
	cl := r.clientSession(t, "browser-a-"+r.tag)

	if _, err := r.oauth.RefreshWithClient(context.Background(), cl.RefreshToken, r.client.ID, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := r.reload(t, op.ID).ExpiresAt
	if cap := authAt.Add(365 * 24 * time.Hour); got.After(cap.Add(time.Second)) {
		t.Errorf("op session expires %v, past the 365-day cap %v", got, cap)
	}
	if !got.After(now.Add(time.Hour)) {
		t.Errorf("op session expires %v; it should still move up to the cap", got)
	}
}

func TestClientRefreshLeavesOtherBrowsersAndDeadSessionsAlone(t *testing.T) {
	r := newSSORig(t, false)
	now := time.Now()
	other := r.opSession(t, "browser-b-"+r.tag, now.Add(-time.Hour), now.Add(time.Hour))
	expired := r.opSession(t, "browser-a-"+r.tag, now.Add(-40*24*time.Hour), now.Add(-time.Minute))
	cl := r.clientSession(t, "browser-a-"+r.tag)

	if _, err := r.oauth.RefreshWithClient(context.Background(), cl.RefreshToken, r.client.ID, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := r.reload(t, other.ID).ExpiresAt; got.After(now.Add(2 * time.Hour)) {
		t.Errorf("another browser's sign-in was extended to %v", got)
	}
	if got := r.reload(t, expired.ID).ExpiresAt; got.After(now) {
		t.Errorf("an expired sign-in was brought back to %v", got)
	}
}

func TestLogoutStillEndsTheBrowserSignIn(t *testing.T) {
	r := newSSORig(t, false)
	now := time.Now()
	op := r.opSession(t, "browser-a-"+r.tag, now.Add(-time.Hour), now.Add(time.Hour))
	cl := r.clientSession(t, "browser-a-"+r.tag)

	if err := r.auth.Logout(context.Background(), op.SessionToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := r.oauth.RefreshWithClient(context.Background(), cl.RefreshToken, r.client.ID, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var n int64
	r.db.Model(&model.Session{}).Where("id = ?", op.ID).Count(&n)
	if n != 0 {
		t.Error("a client refresh recreated a signed-out account-center session")
	}
}

func TestCodeExchangeLinksTheClientSessionToTheBrowser(t *testing.T) {
	r := newSSORig(t, false)
	ctx := context.Background()
	verifier := "verifier-" + r.tag + "-padding-to-make-it-long-enough"
	code, err := r.oauth.CreateAuthorizationCode(ctx, r.user.ID, r.client.ID,
		"https://own-"+r.tag+".test/cb", "", s256(verifier), "S256", "", "browser-a-"+r.tag)
	if err != nil {
		t.Fatalf("create code: %v", err)
	}
	resp, err := r.oauth.ExchangeCode(ctx, &dto.TokenRequest{
		GrantType: "authorization_code", Code: code, ClientID: r.client.ID,
		RedirectURI: "https://own-" + r.tag + ".test/cb", CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	var s model.Session
	if err := r.db.Where("refresh_token = ?", resp.RefreshToken).First(&s).Error; err != nil {
		t.Fatalf("find client session: %v", err)
	}
	if s.SSOBrowserID != "browser-a-"+r.tag {
		t.Errorf("client session sso_browser_id = %q, want the consenting browser", s.SSOBrowserID)
	}
	if s.BrowserID != "" {
		t.Errorf("client session browser_id = %q; it must stay out of the account switcher", s.BrowserID)
	}
}

func TestAccountRefreshSetsExpiryAndCookieFromTheCap(t *testing.T) {
	r := newSSORig(t, false)
	now := time.Now()
	authAt := now.Add(-100 * 24 * time.Hour)
	op := r.opSession(t, "browser-a-"+r.tag, authAt, now.Add(time.Hour))

	tokens, err := r.auth.RefreshToken(context.Background(), op.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := r.reload(t, op.ID).ExpiresAt
	if got.Before(now.Add(29*24*time.Hour)) || got.After(now.Add(31*24*time.Hour)) {
		t.Errorf("op session expires %v; want about now+30d", got)
	}
	wantCookie := int(authAt.Add(365 * 24 * time.Hour).Sub(now).Seconds())
	if d := tokens.CookieMaxAge - wantCookie; d < -5 || d > 5 {
		t.Errorf("cookie max-age %d; want the time left to the cap, about %d", tokens.CookieMaxAge, wantCookie)
	}
}

func TestUnionClientTokensCarryEverySitesRoles(t *testing.T) {
	r := newSSORig(t, true)
	ctx := context.Background()
	other := &siteModel.Site{Name: "other " + r.tag, Domain: "other-" + r.tag + ".test"}
	if err := r.db.Create(other).Error; err != nil {
		t.Fatalf("seed site: %v", err)
	}
	t.Cleanup(func() { r.db.Exec(`DELETE FROM sites WHERE id = ?`, other.ID) })
	past := time.Now().Add(-time.Hour)
	for _, g := range []model.UserSiteRole{
		{UserID: r.user.ID, SiteID: other.ID, RoleName: "moderator", GrantedBy: 1},
		{UserID: r.user.ID, SiteID: *r.client.SiteID, RoleName: "creator", GrantedBy: 1},
		{UserID: r.user.ID, SiteID: other.ID, RoleName: "creator", GrantedBy: 1},
		{UserID: r.user.ID, SiteID: other.ID, RoleName: "lapsed", GrantedBy: 1, ExpiresAt: &past},
	} {
		if err := r.db.Create(&g).Error; err != nil {
			t.Fatalf("seed site role: %v", err)
		}
	}

	verifier := "verifier-" + r.tag + "-padding-to-make-it-long-enough"
	code, err := r.oauth.CreateAuthorizationCode(ctx, r.user.ID, r.client.ID,
		"https://own-"+r.tag+".test/cb", "", s256(verifier), "S256", "", "")
	if err != nil {
		t.Fatalf("create code: %v", err)
	}
	resp, err := r.oauth.ExchangeCode(ctx, &dto.TokenRequest{
		GrantType: "authorization_code", Code: code, ClientID: r.client.ID,
		RedirectURI: "https://own-" + r.tag + ".test/cb", CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	assertSiteRoles(t, "code exchange", resp.AccessToken, []string{"creator", "moderator"})

	refreshed, err := r.oauth.RefreshWithClient(ctx, resp.RefreshToken, r.client.ID, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertSiteRoles(t, "refresh", refreshed.AccessToken, []string{"creator", "moderator"})

	r.db.Model(&siteModel.OAuthClient{}).Where("id = ?", r.client.ID).Update("union_site_roles", false)
	single, err := r.oauth.RefreshWithClient(ctx, refreshed.RefreshToken, r.client.ID, "")
	if err != nil {
		t.Fatalf("refresh without union: %v", err)
	}
	assertSiteRoles(t, "own site only", single.AccessToken, []string{"creator"})
}

func assertSiteRoles(t *testing.T, label, token string, want []string) {
	t.Helper()
	claims, err := utils.ParseToken(token, ssoTestSecret)
	if err != nil {
		t.Fatalf("%s: parse token: %v", label, err)
	}
	if len(claims.SiteRoles) != len(want) {
		t.Fatalf("%s: site_roles = %v, want %v", label, claims.SiteRoles, want)
	}
	for i := range want {
		if claims.SiteRoles[i] != want[i] {
			t.Fatalf("%s: site_roles = %v, want %v", label, claims.SiteRoles, want)
		}
	}
}

func s256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
