package handler

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/settings/keys"
)

func quotaWindow() time.Duration {
	return time.Duration(keys.CatalogWriteQuotaWindowHours.Get()) * time.Hour
}

func quotaExceeded(lane string, used, limit int64, window time.Duration) *problem.Problem {
	return problem.New(problem.CodeQuotaExceeded, "", "",
		fmt.Sprintf("%s: %d of %d in the last %d hours. The window is a sliding one, so the oldest write ages out %d hours after it was made.",
			lane, used, limit, int(window.Hours()), int(window.Hours())))
}

// The claim lane is capped for everyone, not for a role. Minting needs no
// permission — only a user token bound to a catalog site — and a mint that lands
// `pending` is already in the default public works list and search, both of
// which filter `hidden` and nothing else. catalog.claim.trusted only chooses
// `live` over `pending`; without it the owner still reaches `live` by
// withdrawing to draft and publishing. A cap that asked for the permission would
// have capped nobody.
func (c *Catalog) checkClaimQuota(ctx context.Context, actorUID int64) error {
	if c == nil || c.Claims == nil || actorUID <= 0 {
		return nil
	}
	limit := keys.CatalogClaimWritesPerDay.Get()
	if actsAsTrustedClaimant(ctx) {
		limit = keys.CatalogClaimWritesPerDayTrusted.Get()
	}
	window := quotaWindow()
	used, err := c.Claims.CountActorWritesSince(ctx, actorUID, time.Now().Add(-window))
	if err != nil {
		return err
	}
	if used >= limit {
		return quotaExceeded("the claim write limit for this account is exhausted", used, limit, window)
	}
	return nil
}

func (c *Catalog) checkProposalQuota(ctx context.Context, actorUID int64) error {
	if c == nil || c.Engine == nil || actorUID <= 0 {
		return nil
	}
	limit := keys.CatalogProposalsPerDay.Get()
	if actsAsTrustedEditor(ctx) {
		limit = keys.CatalogProposalsPerDayTrusted.Get()
	}
	window := quotaWindow()
	used, err := c.Engine.CountProposalsSince(ctx, actorUID, time.Now().Add(-window))
	if err != nil {
		return err
	}
	if used >= limit {
		return quotaExceeded("the proposal limit for this account is exhausted", used, limit, window)
	}
	return nil
}
