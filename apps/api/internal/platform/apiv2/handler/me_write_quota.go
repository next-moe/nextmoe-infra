package handler

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catalogPerm "api/internal/platform/catalog/perm"
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

const (
	tierUser     = "user"
	tierEditor   = "editor"
	tierReviewer = "reviewer"
)

func editTier(ctx context.Context) string {
	if thirdPartyFrom(ctx) {
		return tierUser
	}
	roles := rolesFrom(ctx)
	switch {
	case catalogPerm.ReviewsEdits(roles):
		return tierReviewer
	case catalogPerm.Resolver.Can(roles, catalogPerm.EditTrusted):
		return tierEditor
	default:
		return tierUser
	}
}

type quotaCounter struct {
	limit *int64
	used  int64
}

func (q quotaCounter) exhausted() bool { return q.limit != nil && q.used >= *q.limit }

func (q quotaCounter) repr() repr.QuotaCounter {
	out := repr.QuotaCounter{Limit: q.limit, Used: q.used}
	if q.limit != nil {
		left := max(*q.limit-q.used, 0)
		out.Remaining = &left
	}
	return out
}

type editQuota struct {
	tier          string
	window        time.Duration
	proposals     quotaCounter
	directEdits   quotaCounter
	openProposals quotaCounter
}

func (q editQuota) blocksFiling() bool {
	return q.proposals.exhausted() || q.openProposals.exhausted()
}

func (q editQuota) demotesEditor() bool {
	return q.tier == tierEditor && q.directEdits.exhausted()
}

func limitOf(v int64) *int64 { return &v }

func capOf(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func (c *Catalog) editQuota(ctx context.Context, uid int64) (editQuota, error) {
	q := editQuota{tier: editTier(ctx), window: quotaWindow()}
	since := time.Now().Add(-q.window)
	switch q.tier {
	case tierReviewer:
		q.proposals.limit = limitOf(keys.CatalogProposalsPerDayReviewer.Get())
		q.openProposals.limit = capOf(keys.CatalogOpenProposalsMaxReviewer.Get())
	case tierEditor:
		q.proposals.limit = limitOf(keys.CatalogProposalsPerDayEditor.Get())
		q.directEdits.limit = limitOf(keys.CatalogDirectEditsPerDayEditor.Get())
		q.openProposals.limit = capOf(keys.CatalogOpenProposalsMaxEditor.Get())
	default:
		q.proposals.limit = limitOf(keys.CatalogProposalsPerDay.Get())
		q.directEdits.limit = limitOf(0)
		q.openProposals.limit = capOf(keys.CatalogOpenProposalsMaxUser.Get())
	}
	if c == nil || c.Engine == nil || uid <= 0 {
		return q, nil
	}
	var err error
	if q.proposals.used, err = c.Engine.CountProposalsSince(ctx, uid, since); err != nil {
		return q, err
	}
	if q.directEdits.used, err = c.Engine.CountDirectEditsSince(ctx, uid, since); err != nil {
		return q, err
	}
	if q.openProposals.used, err = c.Engine.CountOpenProposals(ctx, uid); err != nil {
		return q, err
	}
	return q, nil
}

func (q editQuota) filingProblem() error {
	if q.proposals.exhausted() {
		return quotaExceeded("the proposal limit for this account is exhausted", q.proposals.used, *q.proposals.limit, q.window)
	}
	if q.openProposals.exhausted() {
		return problem.New(problem.CodeQuotaExceeded, "", "", fmt.Sprintf(
			"this account has %d proposals awaiting a decision, the most its tier may have open at once (%d). One must be decided or withdrawn before another is filed.",
			q.openProposals.used, *q.openProposals.limit))
	}
	return nil
}

func (c *Catalog) GetEditQuota(ctx context.Context) (repr.EditQuota, error) {
	if c == nil || c.Engine == nil {
		return repr.EditQuota{}, problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.EditQuota{}, err
	}
	if _, err := requireSite(ctx); err != nil {
		return repr.EditQuota{}, err
	}
	q, err := c.editQuota(ctx, uid)
	if err != nil {
		return repr.EditQuota{}, err
	}
	return repr.EditQuota{
		Object: "edit_quota", QuotaTier: q.tier, WindowHours: int(q.window.Hours()),
		Proposals: q.proposals.repr(), DirectEdits: q.directEdits.repr(), OpenProposals: q.openProposals.repr(),
	}, nil
}
