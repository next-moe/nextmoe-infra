package federation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"api/internal/platform/settings/keys"
	"api/pkg/config"
)

// CheckRedirect refuses redirects: the token POST carries the client secret,
// and the default client would replay it against whatever Location says.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type Identity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

type AuthRequest struct {
	State         string
	Nonce         string
	RedirectURI   string
	CodeChallenge string
}

type ExchangeRequest struct {
	Code         string
	RedirectURI  string
	Nonce        string
	CodeVerifier string
}

type Provider interface {
	Name() string
	AuthorizeURL(req AuthRequest) string
	Exchange(ctx context.Context, req ExchangeRequest) (*Identity, error)
}

// S256Challenge is RFC 7636 §4.2. Callers pass the verifier they stored with the
// federation state; a provider that does not do PKCE simply drops the challenge.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type Registry struct {
	providers map[string]Provider
	order     []string
}

func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{providers: make(map[string]Provider)}
	if cfg == nil {
		return r
	}
	if cfg.Federation.Google.ClientID != "" && cfg.Federation.Google.ClientSecret != "" {
		r.Register(newGoogleProvider(cfg.Federation.Google))
	}
	if cfg.Federation.GitHub.ClientID != "" && cfg.Federation.GitHub.ClientSecret != "" {
		r.Register(newGitHubProvider(cfg.Federation.GitHub))
	}
	if cfg.Federation.Hikarinagi.ClientID != "" && cfg.Federation.Hikarinagi.ClientSecret != "" {
		r.Register(newHikarinagiProvider(cfg.Federation.Hikarinagi))
	}
	return r
}

func (r *Registry) Register(p Provider) {
	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	name := p.Name()
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

func (r *Registry) Get(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) Enabled() []string {
	if r == nil {
		return nil
	}
	wanted := keys.AuthFederationProviders.Get()
	out := make([]string, 0, len(wanted))
	seen := make(map[string]struct{}, len(wanted))
	for _, name := range wanted {
		if _, ok := r.providers[name]; !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (r *Registry) ConfiguredCount() int {
	if r == nil {
		return 0
	}
	return len(r.providers)
}

func postFormJSON(ctx context.Context, rawURL string, form url.Values, headers map[string]string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doJSON(req, dest)
}

func getJSON(ctx context.Context, rawURL string, headers map[string]string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doJSON(req, dest)
}

func doJSON(req *http.Request, dest any) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("federation: %s %s: status %d", req.Method, req.URL.Host, resp.StatusCode)
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}
