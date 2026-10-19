package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/auth/service"
	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const silentFrontend = "https://account.test"

type silentRig struct {
	app      *fiber.App
	db       *gorm.DB
	oauth    *service.OAuthService
	user     *model.User
	auto     *siteModel.OAuthClient
	manual   *siteModel.OAuthClient
	redirect string
	browser  string
	tag      string
}

func newSilentRig(t *testing.T) *silentRig {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		dbtest.Skipf(t, "pgcrypto: %v", err)
	}
	if err := model.AddUserContentPreferenceColumns(db); err != nil {
		dbtest.Skipf(t, "content preference columns: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Session{}, &model.AuthorizationCode{},
		&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tag := randomTail()
	user := &model.User{Name: "silent_" + tag, Email: "silent_" + tag + "@example.test"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.First(user, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	redirect := "https://editor-" + tag + ".test/auth/callback"
	mkClient := func(id string, autoConsent bool) *siteModel.OAuthClient {
		uris, _ := json.Marshal([]string{redirect})
		cl := &siteModel.OAuthClient{
			ID: id, Name: id, Secret: siteModel.HashOAuthClientSecret("unused"),
			RedirectURIs: uris, Grants: []byte(`["authorization_code","refresh_token"]`),
			IsPublic: true, AutoConsent: autoConsent,
		}
		if err := db.Create(cl).Error; err != nil {
			t.Fatalf("create client %s: %v", id, err)
		}
		return cl
	}
	auto := mkClient("silent-auto-"+tag, true)
	manual := mkClient("silent-manual-"+tag, false)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM sessions WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM authorization_codes WHERE user_id = ?`, user.ID)
		db.Exec(`DELETE FROM oauth_clients WHERE id IN (?, ?)`, auto.ID, manual.ID)
		db.Unscoped().Delete(&model.User{}, user.ID)
	})

	cfg := &config.Config{JWT: config.JWTConfig{Secret: "silent-test-secret"}}
	cfg.Server.FrontendURL = silentFrontend
	users := repository.NewUserRepository(db)
	oauth := service.NewOAuthService(users, repository.NewAuthorizationCodeRepository(db),
		repository.NewSessionRepository(db), siteRepo.NewOAuthClientRepository(db), nil, cfg)
	h := NewOAuthHandler(oauth, cfg)

	app := fiber.New()
	app.Get("/api/v1/oauth/authorize", h.Authorize)
	app.Get("/api/v1/auth/oauth/silent-authorize", h.SilentAuthorize)

	return &silentRig{app: app, db: db, oauth: oauth, user: user, auto: auto, manual: manual,
		redirect: redirect, browser: "browser-" + tag, tag: tag}
}

func (r *silentRig) signIn(t *testing.T, authAt, expiresAt time.Time) *model.Session {
	t.Helper()
	s := &model.Session{
		UserID: r.user.ID, SessionToken: "at-" + randomTail(), RefreshToken: "rt-" + randomTail(),
		BrowserID: r.browser, AuthTime: &authAt, ExpiresAt: expiresAt,
	}
	if err := r.db.Create(s).Error; err != nil {
		t.Fatalf("seed sign-in: %v", err)
	}
	return s
}

func silentVerifier(tag string) string { return "verifier-" + tag + "-long-enough-for-pkce-rules-0000" }

func silentChallenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (r *silentRig) query(client string, extra map[string]string) url.Values {
	q := url.Values{}
	q.Set("client_id", client)
	q.Set("redirect_uri", r.redirect)
	q.Set("response_type", "code")
	q.Set("state", "st-"+r.tag)
	q.Set("code_challenge", silentChallenge(silentVerifier(r.tag)))
	q.Set("code_challenge_method", "S256")
	for k, v := range extra {
		q.Set(k, v)
	}
	return q
}

type silentResp struct {
	status   int
	location *url.URL
	body     string
	header   http.Header
	cookies  []*http.Cookie
}

func (r *silentRig) get(t *testing.T, path string, q url.Values, refreshToken string) silentResp {
	t.Helper()
	req := httptest.NewRequest("GET", path+"?"+q.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: browserCookieName, Value: r.browser})
	if refreshToken != "" {
		req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: refreshToken})
	}
	resp, err := r.app.Test(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	out := silentResp{status: resp.StatusCode, body: string(raw), header: resp.Header, cookies: resp.Cookies()}
	if loc := resp.Header.Get("Location"); loc != "" {
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("bad Location %q: %v", loc, err)
		}
		out.location = u
	}
	return out
}

func (r *silentRig) silent(t *testing.T, client string, extra map[string]string, refreshToken string) silentResp {
	return r.get(t, "/api/v1/auth/oauth/silent-authorize", r.query(client, extra), refreshToken)
}

func wantFrontend(t *testing.T, res silentResp, rig *silentRig) {
	t.Helper()
	if res.status != fiber.StatusSeeOther && res.status != fiber.StatusFound {
		t.Fatalf("status %d, want a redirect", res.status)
	}
	if res.location == nil || !strings.HasPrefix(res.location.String(), silentFrontend+"/oauth/authorize?") {
		t.Fatalf("Location %v, want the frontend consent page", res.location)
	}
	if res.location.Query().Get("state") != "st-"+rig.tag {
		t.Fatalf("frontend hop lost the state: %v", res.location)
	}
	var codes int64
	rig.db.Model(&model.AuthorizationCode{}).Where("user_id = ?", rig.user.ID).Count(&codes)
	if codes != 0 {
		t.Fatalf("a fallback minted %d authorization code(s)", codes)
	}
}

func TestAuthorizeHopsAutoConsentClientsToTheSilentRoute(t *testing.T) {
	r := newSilentRig(t)

	res := r.get(t, "/api/v1/oauth/authorize", r.query(r.auto.ID, nil), "")
	if res.location == nil || res.location.Path != "/api/v1/auth/oauth/silent-authorize" {
		t.Fatalf("auto-consent client went to %v, want the silent route", res.location)
	}
	if res.location.Query().Get("code_challenge") == "" || res.location.Query().Get("state") != "st-"+r.tag {
		t.Fatalf("the hop dropped request parameters: %v", res.location)
	}

	for _, prompt := range []string{"login", "select_account"} {
		res := r.get(t, "/api/v1/oauth/authorize", r.query(r.auto.ID, map[string]string{"prompt": prompt}), "")
		wantFrontend(t, res, r)
	}
	wantFrontend(t, r.get(t, "/api/v1/oauth/authorize", r.query(r.manual.ID, nil), ""), r)
}

func TestSilentAuthorizeAnswersTheClientWithNoPage(t *testing.T) {
	r := newSilentRig(t)
	now := time.Now()
	authAt := now.Add(-8 * 24 * time.Hour)
	op := r.signIn(t, authAt, now.Add(time.Hour))

	res := r.silent(t, r.auto.ID, map[string]string{"nonce": "n-" + r.tag}, op.RefreshToken)
	if res.location == nil || res.location.Scheme+"://"+res.location.Host+res.location.Path != r.redirect {
		t.Fatalf("Location %v, want the client's redirect_uri", res.location)
	}
	if strings.Contains(res.body, "<") || strings.Contains(res.header.Get("Content-Type"), "text/html") {
		t.Fatalf("the fast path rendered a page: %q", res.body)
	}
	if res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control %q, want no-store", res.header.Get("Cache-Control"))
	}
	code := res.location.Query().Get("code")
	if code == "" || res.location.Query().Get("state") != "st-"+r.tag {
		t.Fatalf("redirect lacks code or state: %v", res.location)
	}

	var ac model.AuthorizationCode
	if err := r.db.Where("code = ?", code).First(&ac).Error; err != nil {
		t.Fatalf("load code: %v", err)
	}
	if ac.UserID != r.user.ID || ac.BrowserID != r.browser || ac.Nonce != "n-"+r.tag {
		t.Fatalf("code carries user %d browser %q nonce %q", ac.UserID, ac.BrowserID, ac.Nonce)
	}

	var reloaded model.Session
	r.db.First(&reloaded, op.ID)
	if reloaded.ExpiresAt.Before(now.Add(29 * 24 * time.Hour)) {
		t.Fatalf("sign-in expires %v; a silent authorize must slide it like an account refresh", reloaded.ExpiresAt)
	}
	var cookie *http.Cookie
	for _, c := range res.cookies {
		if c.Name == refreshTokenCookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != op.RefreshToken || cookie.Path != "/api/v1/auth" || !cookie.HttpOnly {
		t.Fatalf("refresh cookie not re-issued as the account refresh issues it: %+v", cookie)
	}
	wantAge := authAt.Add(365 * 24 * time.Hour).Sub(now).Seconds()
	if d := float64(cookie.MaxAge) - wantAge; d > 60 || d < -60 {
		t.Fatalf("cookie Max-Age %d, want about %.0f (time left to the cap)", cookie.MaxAge, wantAge)
	}

	tokens, err := r.oauth.ExchangeCode(context.Background(), &dto.TokenRequest{
		GrantType: "authorization_code", Code: code, ClientID: r.auto.ID,
		RedirectURI: r.redirect, CodeVerifier: silentVerifier(r.tag),
	})
	if err != nil {
		t.Fatalf("exchange the silent code: %v", err)
	}
	var clientSession model.Session
	if err := r.db.Where("refresh_token = ?", tokens.RefreshToken).First(&clientSession).Error; err != nil {
		t.Fatalf("find client session: %v", err)
	}
	if clientSession.SSOBrowserID != r.browser {
		t.Fatalf("client session sso_browser_id %q, want %q", clientSession.SSOBrowserID, r.browser)
	}
}

func TestSilentAuthorizeHandsEverythingElseToTheFrontend(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		setup func(t *testing.T, r *silentRig) (client string, extra map[string]string, cookie string)
	}{
		{"no sign-in", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			return r.auto.ID, nil, ""
		}},
		{"expired sign-in", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now.Add(-40*24*time.Hour), now.Add(-time.Minute))
			return r.auto.ID, nil, op.RefreshToken
		}},
		{"prompt=login", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			return r.auto.ID, map[string]string{"prompt": "login"}, op.RefreshToken
		}},
		{"prompt=select_account", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			return r.auto.ID, map[string]string{"prompt": "select_account"}, op.RefreshToken
		}},
		{"client without auto consent", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			return r.manual.ID, nil, op.RefreshToken
		}},
		{"login_hint names someone else", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			return r.auto.ID, map[string]string{"login_hint": "someone-else"}, op.RefreshToken
		}},
		{"banned user", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			r.db.Model(&model.User{}).Where("id = ?", r.user.ID).Update("status", 1)
			return r.auto.ID, nil, op.RefreshToken
		}},
		{"a client session's refresh token", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			s := &model.Session{UserID: r.user.ID, ClientID: r.auto.ID, SessionToken: "cat-" + randomTail(),
				RefreshToken: "crt-" + randomTail(), ExpiresAt: now.Add(time.Hour)}
			if err := r.db.Create(s).Error; err != nil {
				t.Fatalf("seed client session: %v", err)
			}
			return r.auto.ID, nil, s.RefreshToken
		}},
		{"a rotated-away refresh token past the grace window", func(t *testing.T, r *silentRig) (string, map[string]string, string) {
			op := r.signIn(t, now, now.Add(time.Hour))
			rotated := now.Add(-time.Hour)
			r.db.Model(&model.Session{}).Where("id = ?", op.ID).
				Updates(map[string]any{"prev_refresh_token": "old-" + op.RefreshToken, "refresh_token": "new-" + op.RefreshToken, "rotated_at": rotated})
			return r.auto.ID, nil, "old-" + op.RefreshToken
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSilentRig(t)
			client, extra, cookie := tc.setup(t, r)
			wantFrontend(t, r.silent(t, client, extra, cookie), r)
		})
	}
}

func TestSilentAuthorizeHonoursALoginHintThatNamesTheSignedInUser(t *testing.T) {
	r := newSilentRig(t)
	op := r.signIn(t, time.Now(), time.Now().Add(time.Hour))
	for _, hint := range []string{r.user.UUID, r.user.Email} {
		res := r.silent(t, r.auto.ID, map[string]string{"login_hint": hint}, op.RefreshToken)
		if res.location == nil || res.location.Query().Get("code") == "" {
			t.Fatalf("login_hint %q: got %v, want a code", hint, res.location)
		}
	}
}

// The account frontend decides the account from the refresh cookie when no
// login_hint is given, whatever else the browser holds; the fast path must
// pick the same one rather than send that case through the page.
func TestSilentAuthorizeUsesTheCookiesAccountAmongSeveral(t *testing.T) {
	r := newSilentRig(t)
	other := &model.User{Name: "other_" + r.tag, Email: "silent_other_" + r.tag + "@example.test"}
	if err := r.db.Create(other).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() {
		r.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, other.ID)
		r.db.Unscoped().Delete(&model.User{}, other.ID)
	})
	now := time.Now()
	r.db.Create(&model.Session{UserID: other.ID, SessionToken: "oat-" + randomTail(), RefreshToken: "ort-" + randomTail(),
		BrowserID: r.browser, AuthTime: &now, ExpiresAt: now.Add(time.Hour)})
	op := r.signIn(t, now, now.Add(time.Hour))

	res := r.silent(t, r.auto.ID, nil, op.RefreshToken)
	code := res.location.Query().Get("code")
	var ac model.AuthorizationCode
	if err := r.db.Where("code = ?", code).First(&ac).Error; err != nil {
		t.Fatalf("load code: %v", err)
	}
	if ac.UserID != r.user.ID {
		t.Fatalf("code went to user %d, want the cookie's account %d", ac.UserID, r.user.ID)
	}
}

func TestSilentPromptNoneWithoutASignInIsLoginRequired(t *testing.T) {
	r := newSilentRig(t)
	res := r.silent(t, r.auto.ID, map[string]string{"prompt": "none"}, "")
	if res.location == nil || res.location.Scheme+"://"+res.location.Host+res.location.Path != r.redirect {
		t.Fatalf("Location %v, want the client's redirect_uri", res.location)
	}
	if res.location.Query().Get("error") != "login_required" || res.location.Query().Get("state") != "st-"+r.tag {
		t.Fatalf("got %v, want error=login_required with the state", res.location)
	}

	op := r.signIn(t, time.Now(), time.Now().Add(time.Hour))
	res = r.silent(t, r.auto.ID, map[string]string{"prompt": "none"}, op.RefreshToken)
	if res.location.Query().Get("code") == "" {
		t.Fatalf("prompt=none with a sign-in: got %v, want a code", res.location)
	}
}
