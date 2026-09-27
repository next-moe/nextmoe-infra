package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/sanitize"

	"gorm.io/gorm"
)

const (
	OwnActivityKeyPrefix = "community:"
	// 0x63617072 ("capr") must differ from NotifyDispatchLockKey and
	// dbtest.suiteLockKey: advisory lock keys share one space per instance.
	ActivityProjectionLockKey int64 = 0x63617072
	projectionBatchSize             = 200
	projectionInterval              = 2 * time.Second
	projectionMargin                = 10 * time.Minute
)

const (
	RoleTopic         = "topic"
	RoleReply         = "reply"
	RoleComment       = "comment"
	RoleFeedback      = "feedback"
	RoleFeedbackReply = "feedback_reply"
)

type ProjectionRule struct {
	AnchorKind  int16  `json:"anchor_kind"`
	Prefix      string `json:"prefix,omitempty"`
	Role        string `json:"role"`
	Verb        string `json:"verb"`
	ObjectKind  string `json:"object_kind"`
	ObjectLabel string `json:"object_label"`
	Notify      bool   `json:"notify,omitempty"`
	Fragment    string `json:"fragment,omitempty"`
	NoExcerpt   bool   `json:"no_excerpt,omitempty"`
}

type projectionSite struct {
	enabled     bool
	threadURL   string
	notifyAfter time.Time
	rules       []ProjectionRule
}

func parseProjectionSite(row model.CommunityActivitySite) (projectionSite, error) {
	s := projectionSite{enabled: row.Enabled, threadURL: row.ThreadURL, notifyAfter: row.NotifyAfter}
	if err := json.Unmarshal(row.Rules, &s.rules); err != nil {
		return s, fmt.Errorf("site %s rules: %w", row.Site, err)
	}
	return s, nil
}

// rule picks the longest matching prefix, so "rating:" wins over "" on the
// same anchor kind.
func (c projectionSite) rule(anchorKind int16, anchorID, role string) (ProjectionRule, bool) {
	best := -1
	var out ProjectionRule
	for _, r := range c.rules {
		if r.AnchorKind == anchorKind && r.Role == role && strings.HasPrefix(anchorID, r.Prefix) && len(r.Prefix) > best {
			best, out = len(r.Prefix), r
		}
	}
	return out, best >= 0
}

// notifies reports whether a projected post may notify followers as kind 10.
// Posts written before the site was switched on, or before its rules last
// changed, already raised kind 9.
func (c projectionSite) notifies(w repository.ActivityWrite, createdAt time.Time) bool {
	return w.Notify && !createdAt.Before(c.notifyAfter)
}

// topicNotifiesAsActivity reports whether a new topic's projected activity
// will notify its author's followers as kind 10, in which case kind 9 stands
// down. It mirrors the projection and the write path's 24-hour window, so a
// topic the projection cannot shape, or one approved from the review queue a
// day late, still raises kind 9. A rules document that does not parse keeps
// kind 9.
func topicNotifiesAsActivity(tx *gorm.DB, site string, postID int64) (bool, error) {
	row, err := repository.ActivitySiteTx(tx, site)
	if err != nil || row == nil {
		return false, err
	}
	cfg, err := parseProjectionSite(*row)
	if err != nil {
		return false, nil
	}
	posts, err := repository.ProjectedPostsTx(tx, []int64{postID})
	if err != nil {
		return false, err
	}
	p, ok := posts[postID]
	if !ok || p.Site != site {
		return false, nil
	}
	w, ok := projectPost(p, cfg, 0)
	// The margin covers the delay until the projection writes: a topic near the
	// window's end raises both kinds rather than neither.
	return ok && cfg.notifies(w, p.CreatedAt) && time.Since(p.CreatedAt) < activityNotifyWindow-projectionMargin, nil
}

func postRole(threadKind int16, postNumber int32) string {
	switch threadKind {
	case model.ThreadKindTopic:
		if postNumber == 1 {
			return RoleTopic
		}
		return RoleReply
	case model.ThreadKindFeedback:
		if postNumber == 1 {
			return RoleFeedback
		}
		return RoleFeedbackReply
	default:
		return RoleComment
	}
}

func ownActivityKey(postID int64) string {
	return OwnActivityKeyPrefix + "post:" + strconv.FormatInt(postID, 10)
}

// projectPost is the activity a post should have now, or false when it should
// have none.
func projectPost(p repository.ProjectedPostRow, site projectionSite, rev int64) (repository.ActivityWrite, bool) {
	var w repository.ActivityWrite
	if !site.enabled || !model.AnchorIsSiteLocal(p.AnchorKind) || p.Merged ||
		p.PostStatus != model.PostStatusVisible ||
		(p.ThreadStatus != model.ThreadStatusOpen && p.ThreadStatus != model.ThreadStatusClosed) {
		return w, false
	}
	role := postRole(p.ThreadKind, p.PostNumber)
	rule, ok := site.rule(p.AnchorKind, p.AnchorID, role)
	if !ok {
		return w, false
	}
	verb, ok := model.ActivityVerbByName(rule.Verb)
	if !ok || rule.ObjectKind == "" || len(rule.ObjectKind) > activityObjectKindMax ||
		!objectKindPattern.MatchString(rule.ObjectKind) || rule.ObjectLabel == "" ||
		utf8.RuneCountInString(rule.ObjectLabel) > activityLabelMax {
		return w, false
	}

	var url string
	if p.AnchorKind == model.AnchorKindBoard {
		if site.threadURL == "" {
			return w, false
		}
		url = strings.ReplaceAll(site.threadURL, "{thread_id}", strconv.FormatInt(p.ThreadID, 10))
	} else {
		if !p.AnchorLive {
			return w, false
		}
		url = p.AnchorURL
	}
	if role != RoleTopic {
		url += strings.ReplaceAll(rule.Fragment, "{post_id}", strconv.FormatInt(p.PostID, 10))
	}
	title := ""
	if p.ThreadTitle != nil {
		title = strings.TrimSpace(*p.ThreadTitle)
	}
	if title == "" && p.AnchorLive {
		title = p.AnchorTitle
	}
	if title = cutRunes(strings.Join(strings.Fields(stripControl(title)), " "), activityTitleMax); title == "" {
		return w, false
	}
	if len(url) > activityURLMax {
		return w, false
	}

	w = repository.ActivityWrite{
		Key: ownActivityKey(p.PostID), ActorID: p.AuthorID, Verb: verb,
		ObjectKind: rule.ObjectKind, ObjectLabel: rule.ObjectLabel,
		Title: title, URL: url,
		ContentLimit: model.ContentLimitSFW, Notify: rule.Notify && verb == model.ActivityVerbPublish,
		OccurredAt: p.CreatedAt, Revision: rev,
	}
	if p.PostRating != model.ContentRatingAll || p.ThreadRating != model.ContentRatingAll ||
		(p.AnchorLive && p.AnchorContentLimit != model.ContentLimitSFW) {
		w.ContentLimit = model.ContentLimitNSFW
	}
	if p.AnchorLive {
		w.WorkID, w.CoverImageHash = p.AnchorWorkID, p.AnchorCover
	}
	if !rule.NoExcerpt {
		w.Excerpt = plainExcerpt(p.ContentRaw, activityExcerptMax)
	}
	return w, true
}

func plainExcerpt(markdown string, maxRunes int) string {
	return cutRunes(stripControl(sanitize.PlainText(markdown)), maxRunes)
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

func cutRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}

func (s *ActivityService) RunProjection(ctx context.Context) {
	t := time.NewTicker(projectionInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				n, err := s.ProjectBatch(ctx)
				if err != nil {
					slog.Error("community activity projection", "err", err)
				}
				if err != nil || n < projectionBatchSize || ctx.Err() != nil {
					break
				}
			}
		}
	}
}

// ProjectBatch rewrites the activities of up to projectionBatchSize queued
// posts from their current state. It returns how many queue rows it claimed.
func (s *ActivityService) ProjectBatch(ctx context.Context) (int, error) {
	claimed := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?) AS locked", ActivityProjectionLockKey).
			Scan(&locked).Error; err != nil || !locked {
			return err
		}
		claims, err := repository.ClaimActivityProjectionsTx(tx, projectionBatchSize)
		if err != nil || len(claims) == 0 {
			return err
		}
		claimed = len(claims)
		rev := time.Now().UnixMicro()
		ids := make([]int64, len(claims))
		keys := make([]string, len(claims))
		for i, c := range claims {
			ids[i], keys[i] = c.PostID, ownActivityKey(c.PostID)
		}
		posts, err := repository.ProjectedPostsTx(tx, ids)
		if err != nil {
			return err
		}
		stored, err := repository.OwnActivitiesByKeyTx(tx, keys)
		if err != nil {
			return err
		}
		siteRows, err := repository.ActivitySitesTx(tx)
		if err != nil {
			return err
		}
		sites := map[string]projectionSite{}
		broken := map[string]bool{}
		for name, row := range siteRows {
			site, err := parseProjectionSite(row)
			if err != nil {
				slog.Error("community activity projection: site rules do not parse; its items are left as they are", "site", name, "err", err)
				broken[name] = true
				continue
			}
			sites[name] = site
		}

		bySite := map[string][]activityJob{}
		for _, c := range claims {
			key := ownActivityKey(c.PostID)
			p, found := posts[c.PostID]
			// A wall-clock step back must not make a real change stale.
			itemRev := rev
			for _, row := range stored[key] {
				itemRev = max(itemRev, row.Revision+1)
			}
			liveSite := ""
			if found {
				if w, ok := projectPost(p, sites[p.Site], itemRev); ok {
					liveSite = p.Site
					bySite[p.Site] = append(bySite[p.Site], activityJob{write: w, eligible: sites[p.Site].notifies(w, p.CreatedAt)})
				}
			}
			for _, row := range stored[key] {
				if row.Removed || row.Site == liveSite || broken[row.Site] {
					continue
				}
				bySite[row.Site] = append(bySite[row.Site], activityJob{write: repository.ActivityWrite{
					Key: key, ActorID: row.ActorID, Verb: row.Verb, ObjectKind: row.ObjectKind,
					Revision: itemRev, Removed: true, OccurredAt: time.Now(),
				}})
			}
		}
		for _, site := range slices.Sorted(maps.Keys(bySite)) {
			jobs := bySite[site]
			results := make([]ActivityResult, len(jobs))
			for i := range jobs {
				jobs[i].idx = i
				results[i].Key = jobs[i].write.Key
			}
			if err := writeActivitiesTx(tx, site, jobs, results); err != nil {
				return err
			}
		}
		return repository.AckActivityProjectionsTx(tx, claims)
	})
	return claimed, err
}
