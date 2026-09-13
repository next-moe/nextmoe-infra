package federation

import (
	"net/url"
	"testing"

	"api/pkg/config"
)

func TestHikarinagiAuthorizeURL(t *testing.T) {
	p := newHikarinagiProvider(config.FederationProviderConfig{ClientID: "cid", ClientSecret: "sec"})
	raw := p.AuthorizeURL(AuthRequest{
		State:         "st",
		Nonce:         "no",
		RedirectURI:   "https://account.nextmoe.com/api/v1/auth/federation/hikarinagi/callback",
		CodeChallenge: S256Challenge("verifier"),
	})

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.Scheme+"://"+u.Host+u.Path, hikarinagiAuthorizeURL; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":             "cid",
		"redirect_uri":          "https://account.nextmoe.com/api/v1/auth/federation/hikarinagi/callback",
		"response_type":         "code",
		"scope":                 "openid profile email",
		"state":                 "st",
		"nonce":                 "no",
		"code_challenge":        S256Challenge("verifier"),
		"code_challenge_method": "S256",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// offline_access is not requested, so `prompt=consent` would only add a
	// consent screen the login flow does not need.
	if q.Has("prompt") {
		t.Errorf("prompt = %q, want it absent", q.Get("prompt"))
	}
}

func TestS256Challenge(t *testing.T) {
	// RFC 7636 appendix B.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := S256Challenge(verifier); got != want {
		t.Fatalf("S256Challenge = %q, want %q", got, want)
	}
}
