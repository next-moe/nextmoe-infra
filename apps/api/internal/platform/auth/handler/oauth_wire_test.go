package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"slices"
	"testing"

	"api/pkg/config"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
)

func callWire(t *testing.T, fn fiber.Handler) (int, map[string]any) {
	t.Helper()
	app := fiber.New()
	app.Get("/x", fn)
	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("body %q is not JSON: %v", raw, err)
		}
	}
	return resp.StatusCode, body
}

// TestProtocolErrorsAreRFC6749 pins the wire contract of the OAuth protocol
// endpoints: an RFC 6749 §5.2 error object, and never the house
// {code,message,data} envelope. This is the regression that made every standard
// OIDC library unable to talk to this server — the envelope hid access_token
// one level down, so a conforming client read nothing and retried forever.
func TestProtocolErrorsAreRFC6749(t *testing.T) {
	t.Run("error object has error + error_description and no envelope keys", func(t *testing.T) {
		status, body := callWire(t, func(c fiber.Ctx) error {
			return protoErr(c, errors.ErrOAuthInvalidCode)
		})
		if status != fiber.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
		if body["error"] != "invalid_grant" {
			t.Fatalf("error = %v, want invalid_grant", body["error"])
		}
		if _, ok := body["error_description"]; !ok {
			t.Fatal("error_description is required")
		}
		for _, envelopeKey := range []string{"code", "message", "data"} {
			if _, ok := body[envelopeKey]; ok {
				t.Fatalf("house envelope key %q leaked onto a protocol endpoint", envelopeKey)
			}
		}
	})

	t.Run("invalid_client is 401, everything else is 400", func(t *testing.T) {
		status, body := callWire(t, func(c fiber.Ctx) error {
			return protoErr(c, errors.ErrOAuthInvalidClientSecret)
		})
		if status != fiber.StatusUnauthorized || body["error"] != "invalid_client" {
			t.Fatalf("got %d %v, want 401 invalid_client", status, body["error"])
		}
	})

	t.Run("internal faults are 500 server_error, never 4xx", func(t *testing.T) {
		status, body := callWire(t, protoServerError)
		if status != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", status)
		}
		if body["error"] != "server_error" {
			t.Fatalf("error = %v, want server_error", body["error"])
		}
	})

	t.Run("invalid_request carries the caller-facing detail", func(t *testing.T) {
		status, body := callWire(t, func(c fiber.Ctx) error {
			return protoInvalidRequest(c, "token is required")
		})
		if status != fiber.StatusBadRequest || body["error"] != "invalid_request" {
			t.Fatalf("got %d %v", status, body["error"])
		}
		if body["error_description"] != "token is required" {
			t.Fatalf("error_description = %v", body["error_description"])
		}
	})
}

func TestOAuthErrString(t *testing.T) {
	cases := []struct {
		name    string
		appCode int
		want    string
	}{
		{"bad client secret", errors.ErrOAuthInvalidClientSecret, "invalid_client"},
		{"unknown client", errors.ErrOAuthInvalidClient, "invalid_client"},
		{"bad auth code", errors.ErrOAuthInvalidCode, "invalid_grant"},
		{"bad PKCE verifier", errors.ErrOAuthInvalidCodeVerifier, "invalid_grant"},
		{"redirect_uri mismatch", errors.ErrOAuthInvalidRedirectURI, "invalid_grant"},
		{"dead refresh token", errors.ErrAuthTokenExpired, "invalid_grant"},
		{"banned user", errors.ErrAuthUserBanned, "invalid_grant"},
		{"grant not allow-listed", errors.ErrOAuthInvalidGrant, "unauthorized_client"},
		{"bad scope", errors.ErrOAuthInvalidScope, "invalid_scope"},
		{"unsupported grant_type", errors.ErrOAuthUnsupportedGrantType, "unsupported_grant_type"},
		{"anything else", errors.ErrBadRequest, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oauthErrString(tc.appCode); got != tc.want {
				t.Fatalf("oauthErrString(%d) = %q, want %q", tc.appCode, got, tc.want)
			}
		})
	}
}

func TestDiscoveryDocument(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.SiteURL = "https://oauth.example.com/"
	meta := (&OIDCHandler{cfg: cfg}).metadata()

	if meta["issuer"] != "https://oauth.example.com" {
		t.Fatalf("issuer = %v — the trailing slash must be trimmed, RPs compare it byte-for-byte", meta["issuer"])
	}

	required := []string{
		"issuer", "authorization_endpoint", "token_endpoint", "userinfo_endpoint",
		"jwks_uri", "response_types_supported", "response_modes_supported",
		"grant_types_supported", "scopes_supported", "subject_types_supported",
		"id_token_signing_alg_values_supported", "token_endpoint_auth_methods_supported",
		"code_challenge_methods_supported",
	}
	for _, k := range required {
		if _, ok := meta[k]; !ok {
			t.Errorf("discovery is missing %q", k)
		}
	}

	if modes, _ := meta["response_modes_supported"].([]string); len(modes) != 1 || modes[0] != "query" {
		t.Errorf("response_modes_supported = %v, want [query]", meta["response_modes_supported"])
	}

	if algs, _ := meta["id_token_signing_alg_values_supported"].([]string); len(algs) != 1 || algs[0] != "RS256" {
		t.Errorf("id_token_signing_alg_values_supported = %v, want [RS256]", meta["id_token_signing_alg_values_supported"])
	}

	scopes, _ := meta["scopes_supported"].([]string)
	if !slices.Contains(scopes, PreferencesScope) {
		t.Errorf("scopes_supported = %v, want it to advertise %q", scopes, PreferencesScope)
	}

	claims, _ := meta["claims_supported"].([]string)
	for _, want := range []string{"adult_confirmed", "nsfw_display"} {
		if !slices.Contains(claims, want) {
			t.Errorf("claims_supported = %v, missing %q", claims, want)
		}
	}
}
