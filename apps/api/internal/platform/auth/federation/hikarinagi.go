package federation

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"api/pkg/config"

	"github.com/golang-jwt/jwt/v5"
)

const (
	hikarinagiIssuer       = "https://id.hikarinagi.org/oidc"
	hikarinagiAuthorizeURL = "https://id.hikarinagi.org/oidc/auth"
	hikarinagiTokenURL     = "https://id.hikarinagi.org/oidc/token"
)

type hikarinagiProvider struct {
	clientID     string
	clientSecret string
}

func newHikarinagiProvider(cfg config.FederationProviderConfig) *hikarinagiProvider {
	return &hikarinagiProvider{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret}
}

func (p *hikarinagiProvider) Name() string { return "hikarinagi" }

func (p *hikarinagiProvider) AuthorizeURL(req AuthRequest) string {
	q := url.Values{}
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", req.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	q.Set("state", req.State)
	q.Set("nonce", req.Nonce)
	// Hikarinagi ID rejects an authorization request with no code_challenge, and
	// advertises S256 as the only method. No `prompt`: consent is only required
	// for offline_access, which login does not request.
	q.Set("code_challenge", req.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	return hikarinagiAuthorizeURL + "?" + q.Encode()
}

func (p *hikarinagiProvider) Exchange(ctx context.Context, req ExchangeRequest) (*Identity, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("redirect_uri", req.RedirectURI)
	form.Set("code_verifier", req.CodeVerifier)

	var tok struct {
		IDToken string `json:"id_token"`
	}
	// client_secret_basic, the registered auth method for this client: the
	// credentials go in the header, never in the form. RFC 6749 §2.3.1 wants both
	// halves form-urlencoded before base64.
	basic := base64.StdEncoding.EncodeToString([]byte(
		url.QueryEscape(p.clientID) + ":" + url.QueryEscape(p.clientSecret),
	))
	if err := postFormJSON(ctx, hikarinagiTokenURL, form, map[string]string{
		"Authorization": "Basic " + basic,
		"Accept":        "application/json",
	}, &tok); err != nil {
		return nil, err
	}
	if tok.IDToken == "" {
		return nil, fmt.Errorf("federation: hikarinagi token response missing id_token")
	}

	// Signature check deliberately skipped: OIDC Core §3.1.3.7.6 allows it when the ID token comes straight from the token endpoint over TLS; iss/aud/exp/nonce are still enforced.
	parsed, _, err := jwt.NewParser().ParseUnverified(tok.IDToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("federation: hikarinagi id_token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("federation: hikarinagi id_token claims")
	}

	iss, err := claims.GetIssuer()
	if err != nil || iss != hikarinagiIssuer {
		return nil, fmt.Errorf("federation: hikarinagi id_token iss")
	}
	aud, err := claims.GetAudience()
	if err != nil || !audienceHas(aud, p.clientID) {
		return nil, fmt.Errorf("federation: hikarinagi id_token aud")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil || !exp.After(time.Now()) {
		return nil, fmt.Errorf("federation: hikarinagi id_token exp")
	}
	nonceClaim, _ := claims["nonce"].(string)
	if nonceClaim != req.Nonce {
		return nil, fmt.Errorf("federation: hikarinagi id_token nonce")
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return nil, fmt.Errorf("federation: hikarinagi id_token sub")
	}

	email, _ := claims["email"].(string)
	verified, _ := claims["email_verified"].(bool)
	picture, _ := claims["picture"].(string)
	preferred, _ := claims["preferred_username"].(string)
	nickname, _ := claims["nickname"].(string)
	name, _ := claims["name"].(string)

	return &Identity{
		Provider:      p.Name(),
		Subject:       sub,
		Email:         email,
		EmailVerified: verified,
		Name:          firstNonEmpty(preferred, nickname, name),
		AvatarURL:     picture,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
