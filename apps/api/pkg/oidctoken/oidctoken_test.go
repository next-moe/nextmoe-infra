package oidctoken

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"api/pkg/oidckeys"
	"api/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
)

type stubResolver struct{ keys map[string]crypto.PublicKey }

func (r stubResolver) Key(_ context.Context, kid string) (crypto.PublicKey, error) {
	if k := r.keys[kid]; k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("no key for kid %q", kid)
}

func pubFromMaterial(t *testing.T, km *oidckeys.KeyMaterial) crypto.PublicKey {
	t.Helper()
	raw, err := json.Marshal(km.PublicJWK)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	pub, err := oidckeys.PublicKeyFromJWK(m)
	if err != nil {
		t.Fatalf("PublicKeyFromJWK: %v", err)
	}
	return pub
}

func TestAsymmetricSignVerifyRoundtrip(t *testing.T) {
	esKM, err := oidckeys.Generate(oidckeys.AlgES256)
	if err != nil {
		t.Fatal(err)
	}
	rsKM, err := oidckeys.Generate(oidckeys.AlgRS256)
	if err != nil {
		t.Fatal(err)
	}
	resolver := stubResolver{keys: map[string]crypto.PublicKey{
		esKM.Kid: pubFromMaterial(t, esKM),
		rsKM.Kid: pubFromMaterial(t, rsKM),
	}}
	v := NewVerifier("", resolver)

	claims := utils.TokenClaims{
		UserUUID: "u-1", ID: 42, Roles: []string{"admin"}, Scope: "openid profile",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"www.example.com"}},
	}

	esPriv, err := oidckeys.ParsePrivate(esKM.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	esTok, err := NewES256Signer(esKM.Kid, esPriv, "https://id.example").SignAccess(claims, time.Minute)
	if err != nil {
		t.Fatalf("ES256 sign: %v", err)
	}
	got, err := v.Parse(context.Background(), esTok)
	if err != nil {
		t.Fatalf("ES256 verify: %v", err)
	}
	if got.UserUUID != "u-1" || got.ID != 42 || len(got.Roles) != 1 || got.Roles[0] != "admin" || got.Scope != "openid profile" {
		t.Fatalf("ES256 claims mismatch: %+v", got)
	}
	if got.Issuer != "https://id.example" {
		t.Fatalf("access token iss mismatch: %q", got.Issuer)
	}
	if len(got.Audience) != 1 || got.Audience[0] != "www.example.com" {
		t.Fatalf("access token aud mismatch: %v", got.Audience)
	}
	hdr, _, err := jwt.NewParser().ParseUnverified(esTok, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Header["typ"] != "at+jwt" {
		t.Fatalf("access token typ header mismatch: %v", hdr.Header["typ"])
	}

	rsPriv, err := oidckeys.ParsePrivate(rsKM.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	rsClaims := claims
	rsClaims.RegisteredClaims = jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}
	rsJWT := jwt.NewWithClaims(jwt.SigningMethodRS256, rsClaims)
	rsJWT.Header["kid"] = rsKM.Kid
	rsJWT.Header["typ"] = utils.AccessTokenType
	rsTok, err := rsJWT.SignedString(rsPriv)
	if err != nil {
		t.Fatalf("RS256 sign: %v", err)
	}
	if _, err := v.Parse(context.Background(), rsTok); err != nil {
		t.Fatalf("RS256 verify: %v", err)
	}
}

func TestAcceptBothHS256(t *testing.T) {
	const secret = "legacy-secret"
	claims := utils.TokenClaims{UserUUID: "u-2", ID: 7}
	tok, err := NewHS256Signer(secret, "https://id.example").SignAccess(claims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewVerifier(secret, stubResolver{}).Parse(context.Background(), tok); err != nil {
		t.Fatalf("accept-both should accept HS256: %v", err)
	}
	if _, err := NewVerifier("", stubResolver{}).Parse(context.Background(), tok); err == nil {
		t.Fatal("asymmetric-only verifier should reject HS256")
	}
	if _, err := NewVerifier("other-secret", stubResolver{}).Parse(context.Background(), tok); err == nil {
		t.Fatal("verifier should reject HS256 signed with a different secret")
	}
}

func TestJWKSResolverEndToEnd(t *testing.T) {
	km, err := oidckeys.Generate(oidckeys.AlgES256)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"keys": []any{km.PublicJWK}})
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	v := NewVerifierWithJWKS("", srv.URL)
	priv, err := oidckeys.ParsePrivate(km.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := NewES256Signer(km.Kid, priv, "https://id.example").SignAccess(utils.TokenClaims{UserUUID: "u-jwks"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Parse(context.Background(), tok)
	if err != nil {
		t.Fatalf("verify via JWKS resolver: %v", err)
	}
	if got.UserUUID != "u-jwks" {
		t.Fatalf("claims mismatch: %+v", got)
	}
	if hits == 0 {
		t.Fatal("expected the resolver to fetch the JWK Set")
	}

	before := hits
	if _, err := v.Parse(context.Background(), tok); err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if hits != before {
		t.Fatalf("expected cache hit, but resolver refetched (%d -> %d)", before, hits)
	}
}

func TestIDSigner(t *testing.T) {
	km, err := oidckeys.Generate(oidckeys.AlgES256)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := oidckeys.ParsePrivate(km.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	pub := pubFromMaterial(t, km)
	s := NewIDSigner(km.Kid, oidckeys.AlgES256, priv, "https://id.example")

	tok, err := s.Sign("user-uuid", "client-1", "nonce-xyz", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(tok, func(*jwt.Token) (any, error) { return pub, nil })
	if err != nil {
		t.Fatalf("verify id_token: %v", err)
	}
	if parsed.Header["kid"] != km.Kid {
		t.Fatalf("id_token kid mismatch: %v", parsed.Header["kid"])
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["iss"] != "https://id.example" || claims["sub"] != "user-uuid" || claims["aud"] != "client-1" || claims["nonce"] != "nonce-xyz" {
		t.Fatalf("id_token claims mismatch: %+v", claims)
	}

	tok2, err := s.Sign("u", "c", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := jwt.Parse(tok2, func(*jwt.Token) (any, error) { return pub, nil })
	if _, ok := p2.Claims.(jwt.MapClaims)["nonce"]; ok {
		t.Fatal("id_token should omit nonce when not provided")
	}
}

func TestRejectAlgNone(t *testing.T) {
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, utils.TokenClaims{UserUUID: "x"})
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier("secret", stubResolver{}).Parse(context.Background(), s); err == nil {
		t.Fatal("verifier must reject alg=none")
	}
}

// The keyfunc's type switch IS the algorithm pin: it hands an HMAC token the
// configured secret and an ECDSA/RSA token a JWK Set key, so "sign an RS256
// token's payload with HS256 using the public key as the secret" cannot work.
// Simplifying it to `return secret, nil` would open exactly that.
func TestRejectAlgorithmConfusion(t *testing.T) {
	km, err := oidckeys.Generate(oidckeys.AlgRS256)
	if err != nil {
		t.Fatal(err)
	}
	pub := pubFromMaterial(t, km)
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	claims := utils.TokenClaims{UserUUID: "attacker", ID: 1,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}}
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	forged.Header["kid"] = km.Kid
	tok, err := forged.SignedString(pubDER)
	if err != nil {
		t.Fatal(err)
	}

	resolver := stubResolver{keys: map[string]crypto.PublicKey{km.Kid: pub}}
	if _, err := NewVerifier("", resolver).Parse(context.Background(), tok); err == nil {
		t.Fatal("an HS256 token signed with the OP's public key must not verify")
	}

	priv, err := oidckeys.ParsePrivate(km.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	real := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	real.Header["kid"] = km.Kid
	real.Header["typ"] = utils.AccessTokenType
	realTok, err := real.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier("", resolver).Parse(context.Background(), realTok); err != nil {
		t.Fatalf("positive control: the genuinely signed token still verifies: %v", err)
	}
}

func TestRequiringIssuer(t *testing.T) {
	const secret = "issuer-secret"
	const want = "https://oauth.example"
	claims := utils.TokenClaims{UserUUID: "u-3", ID: 9}

	mine, err := NewHS256Signer(secret, want).SignAccess(claims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := NewHS256Signer(secret, "https://someone-else.example").SignAccess(claims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	noIss := jwt.NewWithClaims(jwt.SigningMethodHS256, utils.TokenClaims{
		UserUUID: "u-3", ID: 9,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
	})
	legacy, err := noIss.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	strict := NewVerifier(secret, stubResolver{}).RequiringIssuer(want)
	if _, err := strict.Parse(context.Background(), mine); err != nil {
		t.Fatalf("positive control: our own issuer still parses: %v", err)
	}
	if _, err := strict.Parse(context.Background(), elsewhere); err == nil {
		t.Error("the same secret signs every service's tokens; iss is what tells them apart")
	}
	if _, err := strict.Parse(context.Background(), legacy); err == nil {
		t.Error("a token with no iss must not satisfy a required issuer")
	}

	lax := NewVerifier(secret, stubResolver{})
	if _, err := lax.Parse(context.Background(), elsewhere); err != nil {
		t.Fatalf("positive control: a verifier with no expected issuer is unchanged: %v", err)
	}
}

func TestAnIDTokenIsNotAnAccessToken(t *testing.T) {
	km, err := oidckeys.Generate(oidckeys.AlgRS256)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := oidckeys.ParsePrivate(km.PrivateDER)
	if err != nil {
		t.Fatal(err)
	}
	v := NewVerifier("hs-secret", stubResolver{keys: map[string]crypto.PublicKey{km.Kid: pubFromMaterial(t, km)}})

	idt, err := NewIDSigner(km.Kid, oidckeys.AlgRS256, priv, "").Sign("user-uuid", "some-client", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Parse(context.Background(), idt); err == nil {
		t.Fatal("an id_token must not verify as an access token")
	}

	for name, sign := range map[string]func() (string, error){
		"hs256 signer": func() (string, error) {
			return NewHS256Signer("hs-secret", "").SignAccess(utils.TokenClaims{UserUUID: "u"}, time.Minute)
		},
		"legacy helper": func() (string, error) {
			return utils.GenerateAccessToken("hs-secret", utils.TokenClaims{UserUUID: "u"}, time.Minute)
		},
	} {
		tok, err := sign()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Parse(context.Background(), tok); err != nil {
			t.Fatalf("%s: a real access token must still verify: %v", name, err)
		}
	}
}
