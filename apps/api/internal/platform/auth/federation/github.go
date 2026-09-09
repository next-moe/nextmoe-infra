package federation

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"api/pkg/config"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserURL      = "https://api.github.com/user"
	githubEmailsURL    = "https://api.github.com/user/emails"
)

type githubProvider struct {
	clientID     string
	clientSecret string
}

func newGitHubProvider(cfg config.FederationProviderConfig) *githubProvider {
	return &githubProvider{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret}
}

func (p *githubProvider) Name() string { return "github" }

func (p *githubProvider) AuthorizeURL(state, nonce, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "read:user user:email")
	q.Set("state", state)
	return githubAuthorizeURL + "?" + q.Encode()
}

func (p *githubProvider) Exchange(ctx context.Context, code, redirectURI, nonce string) (*Identity, error) {
	form := url.Values{}
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := postFormJSON(ctx, githubTokenURL, form, map[string]string{
		"Accept": "application/json",
	}, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("federation: github token response missing access_token")
	}

	apiHeaders := map[string]string{
		"Authorization":        "Bearer " + tok.AccessToken,
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getJSON(ctx, githubUserURL, apiHeaders, &user); err != nil {
		return nil, err
	}
	if user.ID == 0 {
		return nil, fmt.Errorf("federation: github user missing id")
	}

	name := user.Name
	if name == "" {
		name = user.Login
	}

	ident := &Identity{
		Provider:  p.Name(),
		Subject:   strconv.FormatInt(user.ID, 10),
		Name:      name,
		AvatarURL: user.AvatarURL,
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, githubEmailsURL, apiHeaders, &emails); err != nil {
		// A transient emails-API failure must fail the whole exchange: swallowing
		// it strips the verified email and routes an existing user into signup.
		return nil, err
	}

	var firstVerified string
	for _, e := range emails {
		if !e.Verified || e.Email == "" {
			continue
		}
		if e.Primary {
			ident.Email = e.Email
			ident.EmailVerified = true
			return ident, nil
		}
		if firstVerified == "" {
			firstVerified = e.Email
		}
	}
	if firstVerified != "" {
		ident.Email = firstVerified
		ident.EmailVerified = true
	}
	return ident, nil
}
