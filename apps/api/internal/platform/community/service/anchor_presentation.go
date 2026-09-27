package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const anchorIDMax = 128

type AnchorPresentationInput struct {
	AnchorKind     int16
	AnchorID       string
	Title          string
	URL            string
	WorkID         *int64
	CoverImageHash string
	ContentLimit   string
	Revision       int64
	Removed        bool
}

type AnchorPresentationResult struct {
	AnchorKind int16
	AnchorID   string
	Outcome    string
	Reason     string
}

func (s *ActivityService) WritePresentations(ctx context.Context, site string, urlHosts []string, items []AnchorPresentationInput) ([]AnchorPresentationResult, error) {
	if len(items) == 0 || len(items) > activityBatchMax {
		return nil, &InvalidError{Reason: "items must hold 1-100 presentations"}
	}
	now := time.Now()
	results := make([]AnchorPresentationResult, len(items))
	type accepted struct {
		idx   int
		write repository.AnchorPresentationWrite
	}
	var todo []accepted
	inBatch := map[string]bool{}
	for i, in := range items {
		results[i].AnchorKind, results[i].AnchorID = in.AnchorKind, in.AnchorID
		w, reason := validatePresentation(in, urlHosts, now)
		id := fmt.Sprintf("%d:%s", in.AnchorKind, in.AnchorID)
		if reason == "" && inBatch[id] {
			reason = "duplicate anchor in batch"
		}
		if reason != "" {
			results[i].Outcome, results[i].Reason = ActivityInvalid, reason
			continue
		}
		inBatch[id] = true
		todo = append(todo, accepted{idx: i, write: w})
	}
	slices.SortFunc(todo, func(a, b accepted) int {
		return cmp.Or(cmp.Compare(a.write.AnchorKind, b.write.AnchorKind), strings.Compare(a.write.AnchorID, b.write.AnchorID))
	})
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, a := range todo {
			res, err := repository.UpsertAnchorPresentationTx(tx, site, a.write)
			if err != nil {
				return err
			}
			results[a.idx].Outcome = presentationOutcome(res)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func presentationOutcome(res repository.AnchorPresentationResult) string {
	switch {
	case !res.Applied:
		return ActivityStale
	case res.IsRemoved:
		return ActivityRemoved
	case !res.Existed:
		return ActivityCreated
	case res.WasRemoved:
		return ActivityRestored
	default:
		return ActivityUpdated
	}
}

func validatePresentation(in AnchorPresentationInput, urlHosts []string, now time.Time) (repository.AnchorPresentationWrite, string) {
	w := repository.AnchorPresentationWrite{
		AnchorKind: in.AnchorKind, AnchorID: in.AnchorID, Revision: in.Revision, Removed: in.Removed,
		ContentLimit: model.ContentLimitNSFW,
	}
	switch {
	case in.AnchorKind != model.AnchorKindSiteGame && in.AnchorKind != model.AnchorKindSiteResource:
		return w, "anchor_kind must be 1 (site_game) or 2 (site_resource)"
	case in.Revision <= 0 || in.Revision > now.Add(activityClockSkew).UnixMicro():
		return w, "revision must be positive and not in the future (unix microseconds)"
	}
	if reason := checkText("anchor_id", in.AnchorID, 1, anchorIDMax, false); reason != "" {
		return w, reason
	}
	if in.Removed {
		return w, ""
	}
	if reason := checkText("title", in.Title, 1, activityTitleMax, false); reason != "" {
		return w, reason
	}
	if reason := checkActivityURL(in.URL, urlHosts); reason != "" {
		return w, reason
	}
	if in.WorkID != nil && *in.WorkID <= 0 {
		return w, "work_id must be positive"
	}
	if in.CoverImageHash != "" {
		if !imageHashPattern.MatchString(in.CoverImageHash) {
			return w, "cover_image_hash must be an image service hash (64 lowercase hex)"
		}
		hash := in.CoverImageHash
		w.CoverHash = &hash
	}
	switch in.ContentLimit {
	case "sfw":
		w.ContentLimit = model.ContentLimitSFW
	case "nsfw", "":
	default:
		return w, "content_limit must be sfw or nsfw"
	}
	w.Title, w.URL, w.WorkID = in.Title, in.URL, in.WorkID
	return w, ""
}

func (s *ActivityService) ListPresentations(site string, after *repository.AnchorPresentationCursor, limit int) ([]model.CommunityAnchorPresentation, int, error) {
	if limit <= 0 || limit > activityReconcileLimit {
		limit = activityReconcileLimit
	}
	rows, err := repository.ListAnchorPresentations(s.db, site, after, limit)
	return rows, limit, err
}
