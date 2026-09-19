package model

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const secretHashPrefix = "sha256:"

func HashOAuthClientSecret(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return secretHashPrefix + hex.EncodeToString(sum[:])
}

func (c *OAuthClient) VerifySecret(presented string) bool {
	stored := c.Secret
	if h, ok := strings.CutPrefix(stored, secretHashPrefix); ok {
		sum := sha256.Sum256([]byte(presented))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(h)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(presented)) == 1
}

type OAuthClient struct {
	ID           string         `gorm:"size:50;primaryKey" json:"id"`
	SiteID       *uint          `gorm:"index" json:"site_id,omitempty"`
	Name         string         `gorm:"size:100;not null" json:"name"`
	Secret       string         `gorm:"size:255;not null" json:"-"`
	RedirectURIs datatypes.JSON `gorm:"type:jsonb;not null" json:"redirect_uris"`
	Grants       datatypes.JSON `gorm:"type:jsonb;not null" json:"grants"`
	CreatedAt    time.Time      `json:"created_at"`

	CreatedByUserID *uint `gorm:"index" json:"created_by_user_id,omitempty"`

	IsPublic bool `gorm:"not null;default:false" json:"is_public"`

	AutoConsent bool `gorm:"not null;default:false" json:"auto_consent"`

	RefreshTokenTTLSeconds int `gorm:"not null;default:7776000" json:"refresh_token_ttl_seconds"`

	AllowedScopes datatypes.JSON `gorm:"type:jsonb" json:"allowed_scopes,omitempty"`

	MoemoepointAwarder bool `gorm:"not null;default:false" json:"moemoepoint_awarder"`

	Listed       bool   `gorm:"not null;default:false" json:"listed"`
	LogoURL      string `gorm:"size:255" json:"logo_url,omitempty"`
	Tagline      string `gorm:"size:100" json:"tagline,omitempty"`
	DisplayOrder int    `gorm:"not null;default:0" json:"display_order"`

	ImageEnabled bool   `gorm:"not null;default:false" json:"image_enabled"`
	ImageSiteKey string `gorm:"size:32" json:"image_site_key,omitempty"`

	ImageCDNBase         string         `gorm:"size:255" json:"image_cdn_base,omitempty"`
	ImageQuotaDaily      int            `gorm:"default:10000" json:"image_quota_daily"`
	ImageQuotaBytesDaily int64          `gorm:"default:10737418240" json:"image_quota_bytes_daily"`
	ImageMaxFileSize     int64          `gorm:"default:10485760" json:"image_max_file_size"`
	ImageAllowedPresets  datatypes.JSON `gorm:"type:jsonb" json:"image_allowed_presets,omitempty"`

	ArtifactEnabled         bool           `gorm:"not null;default:false" json:"artifact_enabled"`
	ArtifactSiteKey         string         `gorm:"size:32" json:"artifact_site_key,omitempty"`
	ArtifactCDNBase         string         `gorm:"size:255" json:"artifact_cdn_base,omitempty"`
	ArtifactQuotaDaily      int            `gorm:"default:1000" json:"artifact_quota_daily"`
	ArtifactQuotaBytesDaily int64          `gorm:"default:107374182400" json:"artifact_quota_bytes_daily"`
	ArtifactMaxFileSize     int64          `gorm:"default:21474836480" json:"artifact_max_file_size"`
	ArtifactAllowedMime     datatypes.JSON `gorm:"type:jsonb" json:"artifact_allowed_mime,omitempty"`

	OwnerUserID     *uint  `gorm:"index" json:"owner_user_id,omitempty"`
	DevEnabled      bool   `gorm:"not null" json:"dev_enabled"`
	DevTier         string `gorm:"size:20;not null" json:"dev_tier"`
	DevRatePerMin   int    `gorm:"not null" json:"dev_rate_per_min"`
	DevQuotaDaily   int    `gorm:"not null" json:"dev_quota_daily"`
	DevReviewStatus string `gorm:"size:20;not null" json:"dev_review_status"`
	DevReviewNote   string `gorm:"type:text" json:"dev_review_note,omitempty"`

	// Set when the owner deletes the application from the portal. The row and
	// every reference to it survive — sessions, authorization codes, store
	// links, usage history and the client_id itself all outlive the owner's
	// decision — but an archived application leaves the owner's list AND their
	// five-slot count, which a plain dev_enabled=false never did.
	DevArchivedAt *time.Time `json:"dev_archived_at,omitempty"`

	// Whether this application's clicks count toward its owner's share of the
	// DLsite coupon pool. New applications join by default (2026-09-19); the
	// operator takes one off here, and archiving takes it off too.
	StoreSettlementEligible bool `gorm:"not null;default:true" json:"store_settlement_eligible"`

	CatalogSite string `gorm:"size:64" json:"catalog_site,omitempty"`

	// The community tenant, when it is not the catalog one. catalog_site names
	// the site a client FILES CATALOG CLAIMS UNDER, and two properties can share
	// that identity while being separate communities: moyu and the kungal forum
	// are both catalog_site=kungal, which put their comment walls in one tenant
	// where anchor ids are the only separation — 1,992 of moyu's 2,040 commented
	// page ids already existed as a forum anchor and 490 carried forum posts.
	// Empty means "same tenant as catalog_site", which is what every site that
	// is its own property wants and what every existing client had.
	CommunitySite string `gorm:"size:64" json:"community_site,omitempty"`

	Site *Site `gorm:"foreignKey:SiteID" json:"site,omitempty"`
}

func (OAuthClient) TableName() string {
	return "oauth_clients"
}

func (c *OAuthClient) IsActive() bool {
	return true
}

func (c *OAuthClient) HasAnyRedirectURI() bool {
	var uris []string
	if err := json.Unmarshal(c.RedirectURIs, &uris); err != nil {
		return false
	}
	return len(uris) > 0
}

// CanSignInUsers answers whether this client was ever configured to complete an
// authorization-code flow — not whether a session exists now. Sessions and
// authorization codes are hard-deleted once they expire, so their absence says
// nothing about what the client once did; this configuration is written at
// creation and never cleaned up. The devapi delete guard and the console's
// application view both read it, and they must read the same one.
func (c *OAuthClient) CanSignInUsers() bool {
	return c.IsPublic || len(c.AllowedGrants()) > 0 || c.HasAnyRedirectURI()
}

func (c *OAuthClient) HasRedirectURI(uri string) bool {
	var uris []string
	if err := json.Unmarshal(c.RedirectURIs, &uris); err != nil {
		return false
	}
	if slices.Contains(uris, uri) {
		return true
	}
	got, ok := parseLoopbackRedirect(uri)
	if !ok {
		return false
	}
	for _, registered := range uris {
		if want, ok := parseLoopbackRedirect(registered); ok && want == got {
			return true
		}
	}
	return false
}

func parseLoopbackRedirect(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return "", false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", false
	}
	return u.Hostname() + "|" + u.EscapedPath() + "|" + u.RawQuery, true
}

func (c *OAuthClient) AllowedPresets() []string {
	if len(c.ImageAllowedPresets) == 0 {
		return nil
	}
	var presets []string
	if err := json.Unmarshal(c.ImageAllowedPresets, &presets); err != nil {
		return nil
	}
	return presets
}

func (c *OAuthClient) IsPresetAllowed(preset string) bool {
	return slices.Contains(c.AllowedPresets(), preset)
}

func (c *OAuthClient) ArtifactAllowedMimes() []string {
	if len(c.ArtifactAllowedMime) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(c.ArtifactAllowedMime, &list); err != nil {
		return nil
	}
	return list
}

var oidcCoreScopes = []string{"openid", "profile", "email"}

func (c *OAuthClient) allowedScopeList() []string {
	if len(c.AllowedScopes) == 0 {
		return oidcCoreScopes
	}
	var scopes []string
	if err := json.Unmarshal(c.AllowedScopes, &scopes); err != nil || len(scopes) == 0 {
		return oidcCoreScopes
	}
	return scopes
}

func (c *OAuthClient) CheckScope(scope string) (disallowed string, ok bool) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", true
	}
	allowed := c.allowedScopeList()
	for _, tok := range strings.Fields(scope) {
		if !slices.Contains(allowed, tok) {
			return tok, false
		}
	}
	return "", true
}

func (c *OAuthClient) AllowedGrants() []string {
	if len(c.Grants) == 0 {
		return nil
	}
	var grants []string
	if err := json.Unmarshal(c.Grants, &grants); err != nil {
		return nil
	}
	return grants
}

func (c *OAuthClient) IsGrantAllowed(grantType string) bool {
	return slices.Contains(c.AllowedGrants(), grantType)
}

const defaultRefreshTokenTTL = 90 * 24 * time.Hour

func (c *OAuthClient) RefreshTokenTTL() time.Duration {
	if c.RefreshTokenTTLSeconds <= 0 {
		return defaultRefreshTokenTTL
	}
	return time.Duration(c.RefreshTokenTTLSeconds) * time.Second
}
