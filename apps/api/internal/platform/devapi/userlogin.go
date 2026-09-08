package devapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"

	siteModel "api/internal/platform/site/model"
)

func appAllowedScopes(userScopes string) []byte {
	all := []string{ScopeCatalogRead}
	for _, s := range strings.Fields(userScopes) {
		if !slices.Contains(all, s) {
			all = append(all, s)
		}
	}
	encoded, err := json.Marshal(all)
	if err != nil {
		return []byte(`["catalog:read"]`)
	}
	return encoded
}

const MaxRedirectURIsPerApp = 5

var selfServiceUserScopes = []string{
	"openid", "profile", "email",
	ScopePlaytimeRead, ScopePlaytimeWrite,
	ScopeFolderRead, ScopeFolderWrite,
	ScopeCatalogEdit,
	ScopeCatalogRead,
}

const (
	ScopePlaytimeRead  = "playtime:read"
	ScopePlaytimeWrite = "playtime:write"
	// Unlike the playtime pair these two are enforced: folders hold private
	// collections, so /v2/me/folders breaks the "any app may call /v2/me"
	// convention and demands an explicit consent.
	ScopeFolderRead  = "folder:read"
	ScopeFolderWrite = "folder:write"
	// Enforced too, over the whole editing plane. v1's UserGate demanded it
	// across /api/v1/user/catalog; wave R3 deleted that surface and carried
	// nothing over, so from that day /v2/me/proposals accepted an
	// `openid profile` token and merged the edit.
	ScopeCatalogEdit = "catalog:edit"
)

var (
	ErrRedirectURIRequired = errors.New("devapi: user login needs at least one redirect URI")
	ErrTooManyRedirectURIs = errors.New("devapi: too many redirect URIs (max 5)")
	ErrRedirectURIInvalid  = errors.New("devapi: redirect URI must be https://, or http:// on the 127.0.0.1 / [::1] loopback for a native app")
	ErrUserScopeNotAllowed = errors.New("devapi: scope not permitted for a self-service app (want openid/profile/email/playtime:read/playtime:write/folder:read/folder:write/catalog:edit/catalog:read)")
	ErrAppNameReserved     = errors.New("devapi: application name may not claim to be NextMoe or an official application")
)

var reservedNameFragments = []string{
	"nextmoe", "next moe", "未萌", "kungal", "kun galgame",
	"官方", "official", "admin", "管理员",
}

type UserLoginRequest struct {
	RedirectURIs []string
	Scopes       []string
}

func validateUserLogin(req UserLoginRequest) ([]string, error) {
	if len(req.RedirectURIs) == 0 {
		return nil, ErrRedirectURIRequired
	}
	if len(req.RedirectURIs) > MaxRedirectURIsPerApp {
		return nil, ErrTooManyRedirectURIs
	}
	for _, raw := range req.RedirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			return nil, err
		}
	}
	scopes := []string{"openid"}
	for _, s := range req.Scopes {
		if !slices.Contains(selfServiceUserScopes, s) {
			return nil, ErrUserScopeNotAllowed
		}
		if !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	return scopes, nil
}

func validateRedirectURI(raw string) error {
	if strings.ContainsAny(raw, "*") || strings.TrimSpace(raw) != raw || raw == "" {
		return ErrRedirectURIInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return ErrRedirectURIInvalid
	}
	host := u.Hostname()
	if host == "" {
		return ErrRedirectURIInvalid
	}
	switch u.Scheme {
	case "https":
		if net.ParseIP(host) != nil {
			return ErrRedirectURIInvalid
		}
		return nil
	case "http":
		if isLoopbackHost(host) {
			return nil
		}
		return ErrRedirectURIInvalid
	}
	return ErrRedirectURIInvalid
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func toUserLoginView(app *siteModel.OAuthClient) *userLoginView {
	var uris []string
	if err := json.Unmarshal(app.RedirectURIs, &uris); err != nil || len(uris) == 0 {
		return nil
	}
	var all []string
	_ = json.Unmarshal(app.AllowedScopes, &all)
	consent := make([]string, 0, len(all))
	for _, s := range all {
		// galgame:read is still filtered although appAllowedScopes stopped
		// writing it on 2026-08-18: every app registered before that date has it
		// in allowed_scopes, and dropping the filter would make the consent page
		// suddenly list a retired machine scope as something the app asks a user for.
		//
		// catalog:read stays filtered for the same reason after it became a
		// user-requestable scope: appAllowedScopes injects it into every app, so
		// allowed_scopes cannot say whether this app asked for it. Nothing is lost
		// — the injection means every self-service app may request it at
		// /oauth/authorize whether or not it registered it.
		if s != ScopeCatalogRead && s != ScopeGalgameRead {
			consent = append(consent, s)
		}
	}
	return &userLoginView{RedirectURIs: uris, Scopes: consent, PKCERequired: true}
}

func validateAppName(name string) error {
	folded := strings.ToLower(strings.ReplaceAll(name, " ", ""))
	for _, frag := range reservedNameFragments {
		if strings.Contains(folded, strings.ReplaceAll(frag, " ", "")) {
			return ErrAppNameReserved
		}
	}
	return nil
}
