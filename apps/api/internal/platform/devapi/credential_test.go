package devapi

import (
	"slices"
	"strings"
	"testing"
)

// news:read used to be grant-only: an owner filed an application, we approved
// it, and only then could the scope be ticked. 2026-08-25 retired that whole
// machinery — /v1/news takes any valid key — so the scope is now exactly as
// mintable as any other string nobody offers, which is not at all. The constant
// stays because live keys still carry the literal in their scopes jsonb.
func TestScopeNewsReadRetired(t *testing.T) {
	if ScopeNewsRead != "news:read" {
		t.Errorf("ScopeNewsRead = %q, want %q", ScopeNewsRead, "news:read")
	}
	if slices.Contains(selfServiceScopes, ScopeNewsRead) {
		t.Errorf("selfServiceScopes must NOT contain %q — no route reads it any more", ScopeNewsRead)
	}
	if err := checkMintScopes([]string{ScopeNewsRead}); err != ErrScopeNotAllowed {
		t.Errorf("minting news:read = %v, want ErrScopeNotAllowed", err)
	}
}

// galgame:read outlived its face: /v1/galgame is a 410 tombstone since wave 146,
// so the portal was still offering a permission over nothing.
func TestScopeGalgameReadRetired(t *testing.T) {
	if slices.Contains(selfServiceScopes, ScopeGalgameRead) {
		t.Errorf("selfServiceScopes must NOT contain %q — the galgame face retired at wave 146", ScopeGalgameRead)
	}
	if err := checkMintScopes([]string{ScopeGalgameRead}); err != ErrScopeNotAllowed {
		t.Errorf("minting galgame:read = %v, want ErrScopeNotAllowed", err)
	}
	if want := []string{ScopeCatalogRead, ScopeStoreRead}; !slices.Equal(selfServiceScopes, want) {
		t.Errorf("selfServiceScopes = %v, want %v", selfServiceScopes, want)
	}
}

// store:read was born grant-only beside news:read, and the machinery that
// handed it out retired the day after it shipped. Owner decision 2026-08-26:
// the scope is self-service — /v1/store still checks it on every request, but
// holding it is the caller's own choice, not an approval.
func TestScopeStoreReadSelfService(t *testing.T) {
	if ScopeStoreRead != "store:read" {
		t.Errorf("ScopeStoreRead = %q, want %q", ScopeStoreRead, "store:read")
	}
	if err := checkMintScopes([]string{ScopeStoreRead}); err != nil {
		t.Errorf("minting store:read = %v, want it accepted", err)
	}
}

// moyu:read and sticker:read were mintable for less than a day (2026-09-08):
// the sticker face's first smoke call was 403'd by the scope its own owner had
// not ticked, and the ruling followed — a free read-only downstream face takes
// any valid key. Unlike news:read the constants are deleted outright, because
// no key was ever minted carrying either string (verified against production
// before the removal). The literals below are deliberate: a stale portal build
// still offering the tick-boxes must get a clean refusal, not a minted scope
// nothing reads.
func TestScopeMoyuAndStickerReadRetired(t *testing.T) {
	for _, sc := range []string{"moyu:read", "sticker:read"} {
		if slices.Contains(selfServiceScopes, sc) {
			t.Errorf("selfServiceScopes must NOT contain %q — the downstream faces take any valid key", sc)
		}
		if err := checkMintScopes([]string{sc}); err != ErrScopeNotAllowed {
			t.Errorf("minting %s = %v, want ErrScopeNotAllowed", sc, err)
		}
	}
	msg := ErrScopeNotAllowed.Error()
	for _, sc := range []string{ScopeCatalogRead, ScopeStoreRead} {
		if !strings.Contains(msg, sc) {
			t.Errorf("ErrScopeNotAllowed message %q does not name %q", msg, sc)
		}
	}
}

// claim_events:read is operator-granted by design: /v2/catalog/claim-events
// carries decline reasons and the moderator uid behind every decision, which is
// a different disclosure than the registry rows catalog:read buys. Nothing in
// the portal may offer it as a tick-box.
func TestScopeClaimEventsReadIsNotSelfService(t *testing.T) {
	if ScopeClaimEventsRead != "claim_events:read" {
		t.Errorf("ScopeClaimEventsRead = %q, want %q", ScopeClaimEventsRead, "claim_events:read")
	}
	if slices.Contains(selfServiceScopes, ScopeClaimEventsRead) {
		t.Errorf("selfServiceScopes must NOT contain %q — it is granted by an operator", ScopeClaimEventsRead)
	}
	if err := checkMintScopes([]string{ScopeClaimEventsRead}); err != ErrScopeNotAllowed {
		t.Errorf("minting claim_events:read = %v, want ErrScopeNotAllowed", err)
	}
}

// folder_holders:read answers who keeps a work in a favorite folder, private
// folders included. That is somebody else's collection rather than a catalog
// row, so it is granted the claim_events:read way — by an operator, with no
// tick-box in the portal.
func TestScopeFolderHoldersReadIsNotSelfService(t *testing.T) {
	if ScopeFolderHoldersRead != "folder_holders:read" {
		t.Errorf("ScopeFolderHoldersRead = %q, want %q", ScopeFolderHoldersRead, "folder_holders:read")
	}
	if slices.Contains(selfServiceScopes, ScopeFolderHoldersRead) {
		t.Errorf("selfServiceScopes must NOT contain %q — it is granted by an operator", ScopeFolderHoldersRead)
	}
	if err := checkMintScopes([]string{ScopeFolderHoldersRead}); err != ErrScopeNotAllowed {
		t.Errorf("minting folder_holders:read = %v, want ErrScopeNotAllowed", err)
	}
}

func TestScopeGalgameWriteSelfServiceExcluded(t *testing.T) {
	if ScopeGalgameWrite != "galgame:write" {
		t.Errorf("ScopeGalgameWrite = %q, want %q", ScopeGalgameWrite, "galgame:write")
	}
	if slices.Contains(selfServiceScopes, ScopeGalgameWrite) {
		t.Errorf("selfServiceScopes must NOT contain %q (D3: write is never self-service)", ScopeGalgameWrite)
	}
	if err := checkMintScopes([]string{ScopeGalgameWrite}); err != ErrScopeNotAllowed {
		t.Errorf("minting galgame:write = %v, want ErrScopeNotAllowed", err)
	}
	if err := checkMintScopes([]string{ScopeCatalogRead}); err != nil {
		t.Errorf("minting catalog:read = %v, want it accepted", err)
	}
}

func TestTierLimits(t *testing.T) {
	cases := []struct {
		tier      string
		rate      int
		quota     int
		unlimited bool
	}{
		{TierFree, 60, 50_000, false},
		{TierTrusted, 600, 1_000_000, false},
		{TierInternal, 0, 0, true},
		{"garbage", 60, 50_000, false},
	}
	for _, c := range cases {
		r, q, u := TierLimits(c.tier)
		if r != c.rate || q != c.quota || u != c.unlimited {
			t.Errorf("TierLimits(%q) = (%d,%d,%v), want (%d,%d,%v)", c.tier, r, q, u, c.rate, c.quota, c.unlimited)
		}
	}
}

func TestEffectiveRateQuota(t *testing.T) {
	free := &Credential{Tier: TierFree}
	if lim, unl := free.EffectiveRate(); lim != 60 || unl {
		t.Errorf("free rate = (%d,%v), want (60,false)", lim, unl)
	}
	if lim, unl := free.EffectiveQuota(); lim != 50_000 || unl {
		t.Errorf("free quota = (%d,%v), want (50000,false)", lim, unl)
	}

	over := &Credential{Tier: TierFree, RateOverride: 5, QuotaOverride: 999}
	if lim, _ := over.EffectiveRate(); lim != 5 {
		t.Errorf("override rate = %d, want 5", lim)
	}
	if lim, _ := over.EffectiveQuota(); lim != 999 {
		t.Errorf("override quota = %d, want 999", lim)
	}

	internal := &Credential{Tier: TierInternal, RateOverride: 5}
	if _, unl := internal.EffectiveRate(); !unl {
		t.Errorf("internal rate should be unlimited")
	}
	if _, unl := internal.EffectiveQuota(); !unl {
		t.Errorf("internal quota should be unlimited")
	}
}

func TestHasScope(t *testing.T) {
	c := &Credential{Scopes: []string{ScopeCatalogRead, ScopeGalgameRead}}
	if !c.HasScope(ScopeCatalogRead) {
		t.Errorf("expected catalog:read present")
	}
	if c.HasScope(ScopeGalgameNSFW) {
		t.Errorf("did not expect galgame:nsfw present")
	}
}
