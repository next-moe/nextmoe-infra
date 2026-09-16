package service

import (
	"context"

	"api/internal/platform/community/model"
)

type callerSiteKey struct{}

func WithCallerSite(ctx context.Context, site string) context.Context {
	return context.WithValue(ctx, callerSiteKey{}, site)
}

func callerSite(ctx context.Context) string {
	s, _ := ctx.Value(callerSiteKey{}).(string)
	return s
}

func deliverySite(ctx context.Context, threadSite string) string {
	if s := callerSite(ctx); s != "" {
		return s
	}
	return threadSite
}

func CrossTenant(callerSite, threadSite string, anchorKind int16) bool {
	return callerSite != "" && model.AnchorIsSiteLocal(anchorKind) && threadSite != callerSite
}

func crossTenantCtx(ctx context.Context, threadSite string, anchorKind int16) bool {
	return CrossTenant(callerSite(ctx), threadSite, anchorKind)
}
