package oidctoken

import (
	"context"
	"crypto"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"api/pkg/oidckeys"
	"api/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
)

var ErrKeyUnavailable = stderrors.New("oidctoken: signing keys unavailable")

type Resolver interface {
	Key(ctx context.Context, kid string) (crypto.PublicKey, error)
}

type Signer interface {
	SignAccess(claims utils.TokenClaims, ttl time.Duration) (string, error)
}

func finalizeAccess(claims utils.TokenClaims, ttl time.Duration, issuer string) (utils.TokenClaims, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return claims, err
	}
	now := time.Now()
	aud := claims.Audience
	claims.RegisteredClaims = jwt.RegisteredClaims{
		ID:        hex.EncodeToString(jti),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Issuer:    issuer,
		Audience:  aud,
	}
	return claims, nil
}

func signAccess(method jwt.SigningMethod, key any, kid, issuer string, claims utils.TokenClaims, ttl time.Duration) (string, error) {
	c, err := finalizeAccess(claims, ttl, issuer)
	if err != nil {
		return "", err
	}
	tok := jwt.NewWithClaims(method, c)
	tok.Header["typ"] = "at+jwt"
	if kid != "" {
		tok.Header["kid"] = kid
	}
	return tok.SignedString(key)
}

type hs256Signer struct {
	secret []byte
	issuer string
}

func NewHS256Signer(secret, issuer string) Signer {
	return &hs256Signer{secret: []byte(secret), issuer: issuer}
}

func (s *hs256Signer) SignAccess(claims utils.TokenClaims, ttl time.Duration) (string, error) {
	return signAccess(jwt.SigningMethodHS256, s.secret, "", s.issuer, claims, ttl)
}

type es256Signer struct {
	kid    string
	key    crypto.PrivateKey
	issuer string
}

func NewES256Signer(kid string, key crypto.PrivateKey, issuer string) Signer {
	return &es256Signer{kid: kid, key: key, issuer: issuer}
}

func (s *es256Signer) SignAccess(claims utils.TokenClaims, ttl time.Duration) (string, error) {
	return signAccess(jwt.SigningMethodES256, s.key, s.kid, s.issuer, claims, ttl)
}

type IDSigner struct {
	kid    string
	method jwt.SigningMethod
	key    crypto.PrivateKey
	issuer string
}

func NewIDSigner(kid, alg string, key crypto.PrivateKey, issuer string) *IDSigner {
	return &IDSigner{kid: kid, method: jwt.GetSigningMethod(alg), key: key, issuer: issuer}
}

func (s *IDSigner) Sign(sub, aud, nonce string, ttl time.Duration) (string, error) {
	if s.method == nil {
		return "", fmt.Errorf("oidctoken: unknown id_token signing alg")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.issuer,
		"sub": sub,
		"aud": aud,
		"exp": now.Add(ttl).Unix(),
		"iat": now.Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	tok := jwt.NewWithClaims(s.method, claims)
	tok.Header["kid"] = s.kid
	return tok.SignedString(s.key)
}

type Verifier struct {
	hs256Secret []byte
	resolver    Resolver
	issuer      string
}

// jwt.ParseWithClaims validates exp, nbf and iat by default and NOTHING else:
// iss and aud are only checked when the parser is handed the option. Every
// resource server here shares one HS256 secret and one JWK Set, so without
// this any token that OP ever signed was accepted anywhere.
func (v *Verifier) RequiringIssuer(want string) *Verifier {
	if v == nil || want == "" {
		return v
	}
	clone := *v
	clone.issuer = want
	return &clone
}

func NewVerifier(hs256Secret string, resolver Resolver) *Verifier {
	var s []byte
	if hs256Secret != "" {
		s = []byte(hs256Secret)
	}
	return &Verifier{hs256Secret: s, resolver: resolver}
}

func NewVerifierWithJWKS(hs256Secret, jwksURL string) *Verifier {
	var r Resolver
	if jwksURL != "" {
		jr := NewJWKSResolver(jwksURL)
		r = jr
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := jr.refetch(ctx); err != nil {
				slog.Error("oidctoken: JWKS warm-up failed; ES256 verification degraded until the OP is reachable",
					"url", jwksURL, "err", err)
			}
		}()
	}
	return NewVerifier(hs256Secret, r)
}

func (v *Verifier) Parse(ctx context.Context, tokenString string) (*utils.TokenClaims, error) {
	keyfunc := func(t *jwt.Token) (any, error) {
		switch t.Method.(type) {
		case *jwt.SigningMethodHMAC:
			if v.hs256Secret == nil {
				return nil, fmt.Errorf("oidctoken: HS256 tokens no longer accepted")
			}
			return v.hs256Secret, nil
		case *jwt.SigningMethodECDSA, *jwt.SigningMethodRSA:
			if v.resolver == nil {
				return nil, fmt.Errorf("oidctoken: asymmetric verification not configured")
			}
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, fmt.Errorf("oidctoken: token missing kid")
			}
			return v.resolver.Key(ctx, kid)
		default:
			return nil, fmt.Errorf("oidctoken: unexpected signing method: %v", t.Header["alg"])
		}
	}
	var opts []jwt.ParserOption
	if v.issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.issuer))
	}
	tok, err := jwt.ParseWithClaims(tokenString, &utils.TokenClaims{}, keyfunc, opts...)
	if err != nil {
		return nil, err
	}
	if !utils.IsAccessTokenType(tok) {
		return nil, utils.ErrNotAccessToken
	}
	if claims, ok := tok.Claims.(*utils.TokenClaims); ok && tok.Valid {
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

type JWKSResolver struct {
	url        string
	httpc      *http.Client
	minRefresh time.Duration

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey
	lastFetch time.Time
}

func NewJWKSResolver(url string) *JWKSResolver {
	return &JWKSResolver{
		url:        url,
		httpc:      &http.Client{Timeout: 5 * time.Second},
		minRefresh: 30 * time.Second,
	}
}

func (r *JWKSResolver) Key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	r.mu.RLock()
	k := r.keys[kid]
	r.mu.RUnlock()
	if k != nil {
		return k, nil
	}
	if err := r.refetch(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyUnavailable, err)
	}
	r.mu.RLock()
	k = r.keys[kid]
	r.mu.RUnlock()
	if k == nil {
		return nil, fmt.Errorf("oidctoken: unknown kid %q", kid)
	}
	return k, nil
}

func (r *JWKSResolver) refetch(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys != nil && time.Since(r.lastFetch) < r.minRefresh {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	resp, err := r.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidctoken: jwks fetch status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	keys, err := oidckeys.ParseJWKSet(body)
	if err != nil {
		return err
	}
	r.keys = keys
	r.lastFetch = time.Now()
	return nil
}
