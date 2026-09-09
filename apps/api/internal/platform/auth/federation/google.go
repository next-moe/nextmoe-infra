package federation

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"api/pkg/config"

	"github.com/golang-jwt/jwt/v5"
)

const (
	googleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL     = "https://oauth2.googleapis.com/token"
)

type googleProvider struct {
	clientID     string
	clientSecret string
}

func newGoogleProvider(cfg config.FederationProviderConfig) *googleProvider {
	return &googleProvider{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret}
}

func (p *googleProvider) Name() string { return "google" }

func (p *googleProvider) AuthorizeURL(state, nonce, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("prompt", "select_account")
	return googleAuthorizeURL + "?" + q.Encode()
}

func (p *googleProvider) Exchange(ctx context.Context, code, redirectURI, nonce string) (*Identity, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := postFormJSON(ctx, googleTokenURL, form, nil, &tok); err != nil {
		return nil, err
	}
	if tok.IDToken == "" {
		return nil, fmt.Errorf("federation: google token response missing id_token")
	}

	// Signature check deliberately skipped: OIDC Core §3.1.3.7.6 allows it when the ID token comes straight from the token endpoint over TLS; iss/aud/exp/nonce are still enforced.
	parsed, _, err := jwt.NewParser().ParseUnverified(tok.IDToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("federation: google id_token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("federation: google id_token claims")
	}

	iss, err := claims.GetIssuer()
	if err != nil || (iss != "https://accounts.google.com" && iss != "accounts.google.com") {
		return nil, fmt.Errorf("federation: google id_token iss")
	}
	aud, err := claims.GetAudience()
	if err != nil || !audienceHas(aud, p.clientID) {
		return nil, fmt.Errorf("federation: google id_token aud")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil || !exp.After(time.Now()) {
		return nil, fmt.Errorf("federation: google id_token exp")
	}
	nonceClaim, _ := claims["nonce"].(string)
	if nonceClaim != nonce {
		return nil, fmt.Errorf("federation: google id_token nonce")
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return nil, fmt.Errorf("federation: google id_token sub")
	}

	email, _ := claims["email"].(string)
	verified, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)
	picture, _ := claims["picture"].(string)

	return &Identity{
		Provider:      p.Name(),
		Subject:       sub,
		Email:         email,
		EmailVerified: verified,
		Name:          name,
		AvatarURL:     picture,
	}, nil
}

func audienceHas(aud jwt.ClaimStrings, clientID string) bool {
	for _, a := range aud {
		if a == clientID {
			return true
		}
	}
	return false
}
