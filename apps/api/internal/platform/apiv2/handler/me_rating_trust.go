package handler

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/pkg/trustclient"
)

const (
	RatingTrustSite        = "nextmoe"
	RatingTrustSubjectKind = "user_rating"

	ratingCheckTimeout = 500 * time.Millisecond
	ratingScanTimeout  = 15 * time.Second
)

type RatingTrust interface {
	Check(ctx context.Context, req trustclient.CheckRequest) (decision string, matched []string, err error)
	Scan(ctx context.Context, req trustclient.ScanRequest) (scanID int64, truncated bool, err error)
}

// First-party sites check and scan their own writes before they reach the
// catalog; a third-party app's short review has no site in front of it.
func (c *Catalog) checkRatingSummary(ctx context.Context, uid, workID int64, summary, pointer string) (bool, error) {
	if c.RatingTrust == nil || !thirdPartyFrom(ctx) || summary == "" {
		return false, nil
	}
	prev, err := c.Ratings.GetMine(ctx, uid, workID)
	if err != nil {
		return false, err
	}
	if prev != nil && prev.ShortSummary == summary {
		return false, nil
	}
	cctx, cancel := context.WithTimeout(ctx, ratingCheckTimeout)
	defer cancel()
	decision, matched, err := c.RatingTrust.Check(cctx, trustclient.CheckRequest{Site: RatingTrustSite, Text: summary, AuthorID: &uid})
	switch {
	case err != nil:
		slog.Warn("rating trust-check fail-open", "uid", uid, "work_id", workID, "err", err)
	case decision == "deny":
		p := problem.New(problem.CodeValidationFailed, "", "", "short_summary contains content that cannot be published.")
		p.Errors = []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonNotAllowedValue,
			Detail: "short_summary was refused by the content check; nothing was written."}}
		return false, p
	case decision == "hold":
		slog.Info("rating short review held by trust", "uid", uid, "work_id", workID, "matched", matched)
	}
	return true, nil
}

func (c *Catalog) scanRatingSummary(ratingID, uid int64, summary string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ratingScanTimeout)
		defer cancel()
		if _, _, err := c.RatingTrust.Scan(ctx, trustclient.ScanRequest{
			Site: RatingTrustSite, SubjectKind: RatingTrustSubjectKind,
			SubjectID: strconv.FormatInt(ratingID, 10), Text: summary, AuthorID: &uid,
		}); err != nil {
			slog.Warn("rating trust-scan", "rating_id", ratingID, "err", err)
		}
	}()
}
