package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/agent/tools"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
	pow "github.com/next-moe/nextmoe-pow/go"
	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb redis.UniversalClient
	now func() time.Time
}

func NewLimiter(rdb redis.UniversalClient) *Limiter { return &Limiter{rdb: rdb, now: time.Now} }

func (l *Limiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) bool {
	slot := l.now().UnixNano() / int64(window)
	k := fmt.Sprintf("agent:rl:%s:%d:%d", key, int64(window/time.Second), slot)
	n, err := l.rdb.Incr(ctx, k).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		l.rdb.Expire(ctx, k, 2*window)
	}
	return n <= limit
}

func ipBucket(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	if a = a.Unmap(); a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}

type Verifier interface {
	Challenge(siteID int64) (string, error)
	Verify(ctx context.Context, siteID int64, solution string) error
}

const powAction = "agent.search"

type PowVerifier struct {
	keys [][]byte
	rdb  redis.UniversalClient
	now  func() time.Time
}

func NewPowVerifier(rawKeys string, rdb redis.UniversalClient) (*PowVerifier, error) {
	v := &PowVerifier{rdb: rdb, now: time.Now}
	for _, k := range strings.Split(rawKeys, ",") {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) < 32 {
			return nil, errors.New("KUN_AGENT_POW_KEYS: each key must be base64 of at least 32 bytes")
		}
		v.keys = append(v.keys, b)
	}
	if len(v.keys) == 0 {
		return nil, nil
	}
	return v, nil
}

type redisReplay struct{ rdb redis.UniversalClient }

func (r redisReplay) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return r.rdb.SetNX(ctx, "pow:"+key, 1, ttl).Result()
}

func (v *PowVerifier) scope(siteID int64) pow.Scope {
	return pow.Scope{Site: strconv.FormatInt(siteID, 10), Action: powAction}
}

func (v *PowVerifier) Challenge(siteID int64) (string, error) {
	return pow.Issue(v.keys[0], pow.Params{
		Alg: pow.Argon2id, MemoryKiB: 512, Iterations: 1, K: 8,
		Difficulty: uint32(keys.AgentAnonPowDifficulty.Get()), TTL: 5 * time.Minute,
	}, v.scope(siteID), v.now())
}

func (v *PowVerifier) Verify(ctx context.Context, siteID int64, solution string) error {
	_, err := pow.VerifyOnce(ctx, redisReplay{v.rdb}, v.keys, solution, v.scope(siteID), v.now())
	return err
}

type Searcher struct {
	Catalog func(siteID int64) *tools.CatalogClient
}

type SearchHit struct {
	ID   string          `json:"id"`
	Card json.RawMessage `json:"card"`
}

func (s *Server) registerSearch(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getAgentPowChallenge", Method: http.MethodGet, Path: "/v2/agent/pow/challenge",
		Summary: "A proof-of-work challenge for a signed-out search", Tags: []string{"agent"}},
		func(ctx context.Context, _ *struct{}) (*struct {
			CacheControl string `header:"Cache-Control"`
			Body         struct {
				Token string `json:"token"`
			}
		}, error) {
			c := from(ctx)
			if err := siteEnabled(c); err != nil {
				return nil, err
			}
			if s.Verify == nil {
				return nil, huma.Error503ServiceUnavailable("proof of work is not configured")
			}
			tok, err := s.Verify.Challenge(c.SiteID)
			if err != nil {
				return nil, err
			}
			out := &struct {
				CacheControl string `header:"Cache-Control"`
				Body         struct {
					Token string `json:"token"`
				}
			}{CacheControl: "no-store"}
			out.Body.Token = tok
			return out, nil
		})

	type searchOut struct {
		Body struct {
			Items      []SearchHit `json:"items"`
			SearchMode string      `json:"search_mode" enum:"lexical,hybrid"`
		}
	}
	huma.Register(api, huma.Operation{OperationID: "agentSearch", Method: http.MethodPost, Path: "/v2/agent/search",
		Summary: "Search mode: find works from a description", Tags: []string{"agent"},
		Description: "Ranks catalog works for a title or a free-text description. Signed-out callers (client credentials, the visitor's address in CF-Connecting-IP) are limited per address and may have to solve a proof-of-work challenge from /v2/agent/pow/challenge."},
		func(ctx context.Context, in *struct {
			Body struct {
				Q   string `json:"q" minLength:"1" maxLength:"300"`
				Pow string `json:"pow,omitempty" maxLength:"4096" doc:"Solution to a challenge from /v2/agent/pow/challenge, when the site requires one."`
			}
		}) (*searchOut, error) {
			c := from(ctx)
			if err := siteEnabled(c); err != nil {
				return nil, err
			}
			if err := s.admitSearch(ctx, c, in.Body.Pow); err != nil {
				return nil, err
			}
			cat := s.Search.Catalog(c.SiteID)
			hits, mode, err := cat.SearchWorks(ctx, strings.TrimSpace(in.Body.Q), 20)
			if err != nil {
				return nil, huma.Error502BadGateway("catalog search failed")
			}
			out := &searchOut{}
			out.Body.SearchMode = mode
			out.Body.Items = []SearchHit{}
			for _, h := range hits {
				out.Body.Items = append(out.Body.Items, SearchHit{ID: h.ID, Card: h.Data})
			}
			return out, nil
		})
}

func (s *Server) admitSearch(ctx context.Context, c *caller, solution string) error {
	if c.UserID != 0 {
		if !s.Limits.Allow(ctx, "user:"+strconv.FormatInt(c.UserID, 10), keys.AgentUserRatePerMinute.Get(), time.Minute) {
			return huma.Error429TooManyRequests("too many searches; wait a minute")
		}
		return nil
	}
	if !keys.AgentAnonEnabled.ForSite(uint(c.SiteID)) {
		return huma.Error401Unauthorized("sign in to search")
	}
	if keys.AgentAnonVerifyMode.ForSite(uint(c.SiteID)) == "pow" {
		if s.Verify == nil {
			return huma.Error503ServiceUnavailable("proof of work is not configured")
		}
		if solution == "" {
			return huma.NewError(http.StatusForbidden, "pow_required")
		}
		if err := s.Verify.Verify(ctx, c.SiteID, solution); err != nil {
			return huma.NewError(http.StatusForbidden, "pow_rejected: "+err.Error())
		}
	}
	ip := "ip:" + ipBucket(c.IP)
	day := 24 * time.Hour
	switch {
	case !s.Limits.Allow(ctx, ip, keys.AgentAnonRatePerMinute.Get(), time.Minute):
		return huma.Error429TooManyRequests("too many searches from this address; wait a minute")
	case !s.Limits.Allow(ctx, ip, keys.AgentAnonQuotaPerDay.Get(), day):
		return huma.Error429TooManyRequests("this address used up today's searches; sign in for more")
	case !s.Limits.Allow(ctx, "anon-global", keys.AgentAnonGlobalQuotaPerDay.Get(), day):
		return huma.Error429TooManyRequests("signed-out search is busy today; sign in to continue")
	}
	return nil
}
