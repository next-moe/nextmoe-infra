package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/settings/keys"
	"api/pkg/trustclient"
)

const checkTimeout = 500 * time.Millisecond

const (
	checkAllow = "allow"
	checkDeny  = "deny"
	checkHold  = "hold"
)

type Checker interface {
	Check(ctx context.Context, req trustclient.CheckRequest) (decision string, matched []string, err error)
}

type CheckService struct {
	ck Checker
}

func NewCheckService(ck Checker) *CheckService {
	return &CheckService{ck: ck}
}

func (s *CheckService) Enabled() bool { return s != nil && s.ck != nil && keys.TrustCheckEnabled.Get() }

func (s *CheckService) Decision(ctx context.Context, site, text string, authorID *int64) (decision string, matched []string) {
	if !s.Enabled() {
		return checkAllow, nil
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	decision, matched, err := s.ck.Check(cctx, trustclient.CheckRequest{Site: site, Text: text, AuthorID: authorID})
	if err != nil {
		slog.Warn("community trust-check fail-open", "site", site, "err", err)
		return checkAllow, nil
	}
	switch decision {
	case checkDeny, checkHold:
		return decision, matched
	default:
		return checkAllow, nil
	}
}
