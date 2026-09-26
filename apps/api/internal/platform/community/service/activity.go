package service

import (
	"context"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const (
	activityBatchMax       = 100
	activityKeyMax         = 128
	activityObjectKindMax  = 32
	activityLabelMax       = 16
	activityTitleMax       = 200
	activityExcerptMax     = 300
	activityURLMax         = 2048
	activityClockSkew      = 5 * time.Minute
	activityTombstoneKeep  = 30 * 24 * time.Hour
	activityFeedDefault    = 20
	activityFeedMax        = 50
	activityPreviewItems   = 3
	activityUnseenCap      = 100
	activityReconcileLimit = 1000
)

var (
	activityKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	objectKindPattern  = regexp.MustCompile(`^[a-z0-9_]+$`)
	imageHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const (
	ActivityCreated  = "created"
	ActivityUpdated  = "updated"
	ActivityRemoved  = "removed"
	ActivityRestored = "restored"
	ActivityStale    = "stale"
	ActivityInvalid  = "invalid"
)

type ActivityService struct{ db *gorm.DB }

func NewActivityService(db *gorm.DB) *ActivityService { return &ActivityService{db: db} }

type ActivityInput struct {
	Key            string
	ActorID        int64
	Verb           string
	ObjectKind     string
	ObjectLabel    string
	Title          string
	Excerpt        string
	URL            string
	CoverImageHash string
	WorkID         *int64
	ContentLimit   string
	Notify         bool
	OccurredAt     *time.Time
	Revision       int64
	Removed        bool
}

type ActivityResult struct {
	Key     string
	Outcome string
	Reason  string
}

// Items are written in key order and their groups recounted afterwards in
// group order, so two concurrent batches take row locks in one global order
// and cannot deadlock.
func (s *ActivityService) Write(ctx context.Context, site string, urlHosts []string, items []ActivityInput) ([]ActivityResult, error) {
	if len(items) == 0 || len(items) > activityBatchMax {
		return nil, &InvalidError{Reason: "items must hold 1-100 activities"}
	}
	now := time.Now()
	results := make([]ActivityResult, len(items))
	type accepted struct {
		idx      int
		write    repository.ActivityWrite
		eligible bool
	}
	var todo []accepted
	inBatch := make(map[string]bool, len(items))
	for i, in := range items {
		results[i].Key = in.Key
		w, reason := validateActivity(in, urlHosts, now)
		if reason == "" && inBatch[in.Key] {
			reason = "duplicate key in batch"
		}
		if reason != "" {
			results[i].Outcome, results[i].Reason = ActivityInvalid, reason
			continue
		}
		inBatch[in.Key] = true
		todo = append(todo, accepted{idx: i, write: w, eligible: !w.Removed && w.Notify})
	}
	slices.SortFunc(todo, func(a, b accepted) int { return strings.Compare(a.write.Key, b.write.Key) })

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		groups := map[string]repository.ActivityGroupKey{}
		for _, a := range todo {
			res, err := repository.UpsertActivityTx(tx, site, a.write, a.eligible)
			if err != nil {
				return err
			}
			results[a.idx].Outcome = activityOutcome(res)
			if !res.Applied {
				continue
			}
			groups[res.NewGroup.String()] = res.NewGroup
			if res.OldGroup != nil {
				groups[res.OldGroup.String()] = *res.OldGroup
			}
			if err := enqueueActivityEventTx(tx, site, res); err != nil {
				return err
			}
		}
		for _, k := range slices.Sorted(maps.Keys(groups)) {
			if err := repository.RefreshActivityGroupTx(tx, groups[k]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Outcome]++
	}
	slog.Info("community activities written", "site", site, "items", len(items),
		"created", counts[ActivityCreated], "updated", counts[ActivityUpdated],
		"removed", counts[ActivityRemoved], "restored", counts[ActivityRestored],
		"stale", counts[ActivityStale], "invalid", counts[ActivityInvalid],
		"took", time.Since(now))
	return results, nil
}

func activityOutcome(res repository.ActivityWriteResult) string {
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

// Notification rows are never touched here: seq belongs to the dispatcher.
func enqueueActivityEventTx(tx *gorm.DB, site string, res repository.ActivityWriteResult) error {
	var kind int16
	switch {
	case res.NotifiesNow:
		kind = model.EventKindActivityPublished
	case res.EverNotified && res.Existed && res.WasRemoved != res.IsRemoved:
		kind = model.EventKindActivityChanged
	default:
		return nil
	}
	id := res.ID
	return repository.EnqueueEventTx(tx, &model.CommunityEvent{
		Site: site, Kind: kind, ThreadID: 0, ActorID: res.NewGroup.ActorID,
		ActivityID: &id, AttemptAfter: time.Now(),
	})
}

func validateActivity(in ActivityInput, urlHosts []string, now time.Time) (repository.ActivityWrite, string) {
	w := repository.ActivityWrite{Key: in.Key, ActorID: in.ActorID, Revision: in.Revision, Removed: in.Removed}
	switch {
	case in.Key == "" || len(in.Key) > activityKeyMax || !activityKeyPattern.MatchString(in.Key):
		return w, "key must be 1-128 characters of A-Z a-z 0-9 . _ : -"
	case in.ActorID <= 0:
		return w, "actor_id must be positive"
	case in.Revision <= 0 || in.Revision > now.Add(activityClockSkew).UnixMicro():
		return w, "revision must be positive and not in the future (unix microseconds)"
	}
	if in.OccurredAt != nil && in.OccurredAt.After(now.Add(activityClockSkew)) {
		return w, "occurred_at is in the future"
	}
	w.OccurredAt = now
	if in.OccurredAt != nil {
		w.OccurredAt = *in.OccurredAt
	}
	if in.Verb != "" {
		verb, ok := model.ActivityVerbByName(in.Verb)
		if !ok {
			return w, "unknown verb"
		}
		w.Verb = verb
	}
	if in.ObjectKind != "" {
		if len(in.ObjectKind) > activityObjectKindMax || !objectKindPattern.MatchString(in.ObjectKind) {
			return w, "object_kind must be 1-32 characters of a-z 0-9 _"
		}
		w.ObjectKind = in.ObjectKind
	}
	if in.Removed {
		return w, ""
	}

	switch {
	case in.Verb == "":
		return w, "verb is required"
	case in.ObjectKind == "":
		return w, "object_kind is required"
	case in.OccurredAt == nil:
		return w, "occurred_at is required"
	}
	if reason := checkText("object_label", in.ObjectLabel, 1, activityLabelMax, false); reason != "" {
		return w, reason
	}
	if reason := checkText("title", in.Title, 1, activityTitleMax, false); reason != "" {
		return w, reason
	}
	if reason := checkText("excerpt", in.Excerpt, 0, activityExcerptMax, true); reason != "" {
		return w, reason
	}
	if reason := checkActivityURL(in.URL, urlHosts); reason != "" {
		return w, reason
	}
	w.ObjectLabel, w.Title, w.Excerpt, w.URL = in.ObjectLabel, in.Title, in.Excerpt, in.URL

	if in.CoverImageHash != "" {
		if !imageHashPattern.MatchString(in.CoverImageHash) {
			return w, "cover_image_hash must be an image service hash (64 lowercase hex)"
		}
		hash := in.CoverImageHash
		w.CoverImageHash = &hash
	}
	if in.WorkID != nil {
		if *in.WorkID <= 0 {
			return w, "work_id must be positive"
		}
		w.WorkID = in.WorkID
	}
	switch in.ContentLimit {
	case "sfw":
		w.ContentLimit = model.ContentLimitSFW
	case "nsfw", "":
		w.ContentLimit = model.ContentLimitNSFW
	default:
		return w, "content_limit must be sfw or nsfw"
	}
	if in.Notify && w.Verb != model.ActivityVerbPublish {
		return w, "notify is only for verb publish"
	}
	w.Notify = in.Notify
	return w, ""
}

func checkText(field, s string, minRunes, maxRunes int, multiline bool) string {
	if !utf8.ValidString(s) {
		return field + " is not valid UTF-8"
	}
	n := utf8.RuneCountInString(s)
	if n < minRunes || n > maxRunes || (minRunes > 0 && strings.TrimSpace(s) == "") {
		return field + " length is out of range"
	}
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) {
			return field + " contains a control character"
		}
	}
	return ""
}

func checkActivityURL(raw string, hosts []string) string {
	if raw == "" || len(raw) > activityURLMax {
		return "url is required (max 2048)"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return "url must be an absolute https URL"
	}
	if !slices.Contains(hosts, strings.ToLower(u.Hostname())) {
		return "url host is not one of this site's registered hosts"
	}
	return ""
}

func (s *ActivityService) ListOwn(site string, afterID int64, limit int) ([]model.CommunityActivity, int, error) {
	if limit <= 0 || limit > activityReconcileLimit {
		limit = activityReconcileLimit
	}
	rows, err := repository.ListSiteActivities(s.db, site, afterID, limit)
	return rows, limit, err
}

func (s *ActivityService) Prune(ctx context.Context) (int64, error) {
	return repository.PruneActivityTombstones(s.db.WithContext(ctx), activityTombstoneKeep)
}
