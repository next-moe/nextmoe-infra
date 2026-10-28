package handler

import (
	"context"
	"encoding/base64"
	"net/netip"
	"strings"
	"sync"
	"time"

	siteRepo "api/internal/platform/site/repository"
	"api/pkg/oidctoken"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type caller struct {
	UserID   int64
	SiteID   int64
	SiteName string
	ClientID string
	IP       string
}

const localCaller = "agent_caller"

func callerOf(c fiber.Ctx) *caller {
	v, _ := c.Locals(localCaller).(*caller)
	return v
}

type siteInfo struct {
	id   int64
	name string
	at   time.Time
}

type SiteResolver struct {
	db    *gorm.DB
	mu    sync.Mutex
	cache map[string]siteInfo
}

func NewSiteResolver(mainDB *gorm.DB) *SiteResolver {
	return &SiteResolver{db: mainDB, cache: map[string]siteInfo{}}
}

func (r *SiteResolver) lookup(ctx context.Context, clientID string) (siteInfo, bool) {
	r.mu.Lock()
	if s, ok := r.cache[clientID]; ok && time.Since(s.at) < 5*time.Minute {
		r.mu.Unlock()
		return s, s.id != 0
	}
	r.mu.Unlock()
	var row struct {
		SiteID int64
		Name   string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT coalesce(c.site_id, 0) AS site_id, coalesce(s.name, '') AS name
		FROM oauth_clients c LEFT JOIN sites s ON s.id = c.site_id WHERE c.id = ?`, clientID).Scan(&row).Error
	if err != nil {
		return siteInfo{}, false
	}
	s := siteInfo{id: row.SiteID, name: row.Name, at: time.Now()}
	r.mu.Lock()
	r.cache[strings.Clone(clientID)] = s
	r.mu.Unlock()
	return s, s.id != 0
}

func clientIP(c fiber.Ctx) string {
	peer, err := netip.ParseAddr(c.IP())
	if err == nil && (peer.IsPrivate() || peer.IsLoopback()) {
		if fwd := strings.TrimSpace(c.Get("CF-Connecting-IP")); fwd != "" {
			if a, err := netip.ParseAddr(fwd); err == nil {
				return a.String()
			}
		}
	}
	return strings.Clone(c.IP())
}

func authenticate(verifier *oidctoken.Verifier, clients *siteRepo.OAuthClientRepository, sites *SiteResolver, requireUser bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		auth := c.Get("Authorization")
		who := &caller{IP: clientIP(c)}
		switch {
		case strings.HasPrefix(auth, "Bearer "):
			claims, err := verifier.Parse(c.Context(), strings.TrimPrefix(auth, "Bearer "))
			if err != nil || claims.ID == 0 || claims.ClientID == "" {
				return problem(c, fiber.StatusUnauthorized, "invalid or expired access token")
			}
			who.UserID, who.ClientID = int64(claims.ID), strings.Clone(claims.ClientID)
		case strings.HasPrefix(auth, "Basic ") && !requireUser:
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
			id, secret, ok := strings.Cut(string(raw), ":")
			if err != nil || !ok {
				return problem(c, fiber.StatusUnauthorized, "malformed client credentials")
			}
			client, err := clients.FindByClientID(c.Context(), id)
			if err != nil || client == nil || !client.VerifySecret(secret) {
				return problem(c, fiber.StatusUnauthorized, "invalid client credentials")
			}
			who.ClientID = strings.Clone(client.ID)
		default:
			if requireUser {
				return problem(c, fiber.StatusUnauthorized, "a user access token is required")
			}
			return problem(c, fiber.StatusUnauthorized, "a user access token or client credentials are required")
		}
		site, ok := sites.lookup(c.Context(), who.ClientID)
		if !ok {
			return problem(c, fiber.StatusForbidden, "this client is not bound to a site")
		}
		who.SiteID, who.SiteName = site.id, site.name
		c.Locals(localCaller, who)
		return c.Next()
	}
}

func problem(c fiber.Ctx, status int, detail string) error {
	c.Set("Content-Type", "application/problem+json")
	return c.Status(status).JSON(fiber.Map{"status": status, "title": strings.ToLower(httpStatusText(status)), "detail": detail})
}

func httpStatusText(status int) string {
	switch status {
	case fiber.StatusUnauthorized:
		return "Unauthorized"
	case fiber.StatusForbidden:
		return "Forbidden"
	case fiber.StatusTooManyRequests:
		return "Too Many Requests"
	case fiber.StatusPaymentRequired:
		return "Payment Required"
	case fiber.StatusConflict:
		return "Conflict"
	case fiber.StatusNotFound:
		return "Not Found"
	default:
		return "Bad Request"
	}
}
