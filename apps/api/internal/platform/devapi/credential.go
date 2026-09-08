package devapi

import "slices"

const (
	TierFree     = "free"
	TierTrusted  = "trusted"
	TierInternal = "internal"
)

const (
	ScopeCatalogRead  = "catalog:read"
	ScopeGalgameRead  = "galgame:read"
	ScopeGalgameNSFW  = "galgame:nsfw"
	ScopeGalgameWrite = "galgame:write"
	ScopeStoreRead    = "store:read"
	// Operator-granted, never self-service: claim events carry decline reasons
	// and the moderator uid behind every decision.
	ScopeClaimEventsRead = "claim_events:read"
	// Retired 2026-08-25 with the grant-only application machinery: no route
	// checks it any more. Kept because live keys still carry the string in
	// their scopes jsonb and readers of that history need the name.
	ScopeNewsRead = "news:read"
)

func TierLimits(tier string) (ratePerMin, quotaDaily int, unlimited bool) {
	switch tier {
	case TierInternal:
		return 0, 0, true
	case TierTrusted:
		return 600, 1_000_000, false
	default:
		return 60, 50_000, false
	}
}

type Credential struct {
	KeyID         uint     `json:"key_id"`
	ClientID      string   `json:"client_id"`
	AppName       string   `json:"app_name"`
	Tier          string   `json:"tier"`
	Scopes        []string `json:"scopes"`
	RateOverride  int      `json:"rate_override"`
	QuotaOverride int      `json:"quota_override"`
}

func (c *Credential) EffectiveRate() (limit int, unlimited bool) {
	def, _, unl := TierLimits(c.Tier)
	if unl {
		return 0, true
	}
	if c.RateOverride > 0 {
		return c.RateOverride, false
	}
	return def, false
}

func (c *Credential) EffectiveQuota() (limit int, unlimited bool) {
	_, def, unl := TierLimits(c.Tier)
	if unl {
		return 0, true
	}
	if c.QuotaOverride > 0 {
		return c.QuotaOverride, false
	}
	return def, false
}

func (c *Credential) HasScope(s string) bool {
	return slices.Contains(c.Scopes, s)
}
