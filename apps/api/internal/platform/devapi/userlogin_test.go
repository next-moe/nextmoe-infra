package devapi

import (
	"testing"

	siteModel "api/internal/platform/site/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRedirectURI(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		ok   bool
	}{
		{"an https callback", "https://manager.example.com/oauth/callback", true},
		{"an https callback with a query", "https://example.com/cb?app=1", true},
		{"a native loopback callback", "http://127.0.0.1:53682/callback", true},
		{"the IPv6 loopback", "http://[::1]:53682/callback", true},
		{"a loopback with no port yet", "http://127.0.0.1/callback", true},

		{"plain http to a real host", "http://manager.example.com/cb", false},
		{"localhost by name", "http://localhost:53682/callback", false},
		{"a fragment", "https://example.com/cb#token", false},
		{"a wildcard host", "https://*.example.com/cb", false},
		{"a wildcard path", "https://example.com/*", false},
		{"userinfo impersonating a host", "https://example.com@evil.com/cb", false},
		{"a relative URI", "/callback", false},
		{"a non-http scheme", "javascript:alert(1)", false},
		{"empty", "", false},
		{"whitespace padding", " https://example.com/cb ", false},
		{"https to a bare IP", "https://93.184.216.34/cb", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRedirectURI(c.uri)
			if c.ok {
				assert.NoError(t, err, "%q must be accepted", c.uri)
				return
			}
			assert.ErrorIs(t, err, ErrRedirectURIInvalid, "%q must be refused", c.uri)
		})
	}
}

func TestValidateUserLogin(t *testing.T) {
	good := []string{"http://127.0.0.1:1/cb"}

	t.Run("openid is added for an app that forgot it", func(t *testing.T) {
		scopes, err := validateUserLogin(UserLoginRequest{
			RedirectURIs: good, Scopes: []string{ScopePlaytimeWrite},
		})
		require.NoError(t, err)
		assert.Contains(t, scopes, "openid")
		assert.Contains(t, scopes, ScopePlaytimeWrite)
	})

	t.Run("catalog:edit is accepted as a user scope", func(t *testing.T) {
		scopes, err := validateUserLogin(UserLoginRequest{
			RedirectURIs: good, Scopes: []string{"catalog:edit"},
		})
		require.NoError(t, err)
		assert.Contains(t, scopes, "openid")
		assert.Contains(t, scopes, "catalog:edit")
	})

	t.Run("catalog:read is accepted as a user scope", func(t *testing.T) {
		scopes, err := validateUserLogin(UserLoginRequest{
			RedirectURIs: good, Scopes: []string{ScopeCatalogRead},
		})
		require.NoError(t, err)
		assert.Contains(t, scopes, ScopeCatalogRead)
	})

	t.Run("a scope off the allow-list is refused", func(t *testing.T) {
		for _, scope := range []string{"image:upload", "artifact:upload", "galgame:nsfw"} {
			_, err := validateUserLogin(UserLoginRequest{RedirectURIs: good, Scopes: []string{scope}})
			assert.ErrorIs(t, err, ErrUserScopeNotAllowed, "scope %q", scope)
		}
	})

	t.Run("no callback at all", func(t *testing.T) {
		_, err := validateUserLogin(UserLoginRequest{Scopes: []string{ScopePlaytimeRead}})
		assert.ErrorIs(t, err, ErrRedirectURIRequired)
	})

	t.Run("over the callback cap", func(t *testing.T) {
		many := make([]string, MaxRedirectURIsPerApp+1)
		for i := range many {
			many[i] = "https://example.com/cb"
		}
		_, err := validateUserLogin(UserLoginRequest{RedirectURIs: many})
		assert.ErrorIs(t, err, ErrTooManyRedirectURIs)
	})

	t.Run("one bad callback poisons the set", func(t *testing.T) {
		_, err := validateUserLogin(UserLoginRequest{
			RedirectURIs: []string{"https://example.com/cb", "http://evil.example.com/cb"},
		})
		assert.ErrorIs(t, err, ErrRedirectURIInvalid)
	})

	t.Run("duplicate scopes collapse", func(t *testing.T) {
		scopes, err := validateUserLogin(UserLoginRequest{
			RedirectURIs: good,
			Scopes:       []string{"openid", "openid", ScopePlaytimeRead, ScopePlaytimeRead},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"openid", ScopePlaytimeRead}, scopes)
	})
}

func TestValidateAppName(t *testing.T) {
	for _, name := range []string{
		"NextMoe", "nextmoe helper", "NextMoe 官方助手", "未萌启动器",
		"KunGal Manager", "官方管理器", "Official Sync", "ADMIN tools",
		"Next Moe Sync",
	} {
		assert.ErrorIs(t, validateAppName(name), ErrAppNameReserved, "%q", name)
	}
	for _, name := range []string{
		"Kurumi", "GalGame Manager", "我的游戏库", "VN Launcher",
	} {
		assert.NoError(t, validateAppName(name), "%q", name)
	}
}

// The fixture keeps galgame:read on purpose: it is the shape of every app
// registered before 2026-08-18, and those rows are exactly what the filter in
// toUserLoginView still has to strip.
func TestToUserLoginViewKeepsCatalogEdit(t *testing.T) {
	view := toUserLoginView(&siteModel.OAuthClient{
		RedirectURIs:  []byte(`["https://example.com/cb"]`),
		AllowedScopes: []byte(`["catalog:read","galgame:read","openid","catalog:edit"]`),
	})
	require.NotNil(t, view)
	assert.Equal(t, []string{"openid", "catalog:edit"}, view.Scopes,
		"catalog:read / legacy galgame:read are stripped from the consent view; catalog:edit must stay")
}

func TestAppAllowedScopesKeyOnly(t *testing.T) {
	assert.JSONEq(t, `["catalog:read"]`, string(appAllowedScopes("")))
}

func TestAppAllowedScopesWithConsent(t *testing.T) {
	assert.JSONEq(t,
		`["catalog:read","openid","playtime:write"]`,
		string(appAllowedScopes("openid playtime:write")))
}

// catalog:read is both the machine scope this function injects and, since it
// became user-requestable, something the app may also list. Two copies in
// allowed_scopes would be harmless to CheckScope and confusing to every reader
// of the row.
func TestAppAllowedScopesDoesNotRepeatCatalogRead(t *testing.T) {
	assert.JSONEq(t,
		`["catalog:read","openid"]`,
		string(appAllowedScopes("openid catalog:read")))
}
