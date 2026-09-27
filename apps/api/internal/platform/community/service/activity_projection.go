package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const (
	OwnActivityKeyPrefix = "community:"
	// 0x63617072 ("capr") must differ from NotifyDispatchLockKey and
	// dbtest.suiteLockKey: advisory lock keys share one space per instance.
	ActivityProjectionLockKey int64 = 0x63617072
	projectionBatchSize             = 200
	projectionInterval              = 2 * time.Second
)

const (
	RoleTopic         = "topic"
	RoleReply         = "reply"
	RoleComment       = "comment"
	RoleFeedback      = "feedback"
	RoleFeedbackReply = "feedback_reply"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

type ProjectionRule struct {
	AnchorKind  int16  `json:"anchor_kind"`
	Prefix      string `json:"prefix,omitempty"`
	Role        string `json:"role"`
	Verb        string `json:"verb"`
	ObjectKind  string `json:"object_kind"`
	ObjectLabel string `json:"object_label"`
	Notify      bool   `json:"notify,omitempty"`
}

type projectionSite struct {
	enabled      bool
	threadURL    string
	postFragment string
	rules        []ProjectionRule
}

func parseProjectionSite(row model.CommunityActivitySite) (projectionSite, error) {
	s := projectionSite{enabled: row.Enabled, threadURL: row.ThreadURL, postFragment: row.PostFragment}
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

func (c projectionSite) notifiesTopics(anchorKind int16, anchorID string) bool {
	r, ok := c.rule(anchorKind, anchorID, RoleTopic)
	return c.enabled && ok && r.Notify
}

// topicsNotifyAsActivities reports whether a topic opened on this thread's
// site notifies followers as kind 10, through its projected activity, instead
// of kind 9.
func topicsNotifyAsActivities(tx *gorm.DB, thread *model.CommunityThread) (bool, error) {
	row, err := repository.ActivitySiteTx(tx, thread.Site)
	if err != nil || row == nil {
		return false, err
	}
	site, err := parseProjectionSite(*row)
	if err != nil {
		return false, err
	}
	return site.notifiesTopics(thread.AnchorKind, thread.AnchorID), nil
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
		url += strings.ReplaceAll(site.postFragment, "{post_id}", strconv.FormatInt(p.PostID, 10))
	}
	title := ""
	if p.ThreadTitle != nil {
		title = strings.TrimSpace(*p.ThreadTitle)
	}
	if title == "" && p.AnchorLive {
		title = p.AnchorTitle
	}
	if title = cutRunes(stripControl(title), activityTitleMax); title == "" {
		return w, false
	}

	w = repository.ActivityWrite{
		Key: ownActivityKey(p.PostID), ActorID: p.AuthorID, Verb: verb,
		ObjectKind: rule.ObjectKind, ObjectLabel: rule.ObjectLabel,
		Title: title, Excerpt: plainExcerpt(p.ContentHTML, activityExcerptMax), URL: url,
		ContentLimit: model.ContentLimitSFW, Notify: rule.Notify && verb == model.ActivityVerbPublish,
		OccurredAt: p.CreatedAt, Revision: rev,
	}
	if p.PostRating != model.ContentRatingAll || p.ThreadRating != model.ContentRatingAll ||
		(p.AnchorLive && p.AnchorContentLimit != model.ContentLimitSFW) {
		w.ContentLimit = model.ContentLimitNSFW
	}
	if p.AnchorLive {
		w.WorkID = p.AnchorWorkID
	}
	return w, true
}

func plainExcerpt(content string, maxRunes int) string {
	text := html.UnescapeString(htmlTagPattern.ReplaceAllString(content, " "))
	return cutRunes(strings.Join(strings.Fields(stripControl(text)), " "), maxRunes)
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
		for name, row := range siteRows {
			site, err := parseProjectionSite(row)
			if err != nil {
				slog.Error("community activity projection: bad site config", "err", err)
				continue
			}
			sites[name] = site
		}

		bySite := map[string][]activityJob{}
		for _, c := range claims {
			key := ownActivityKey(c.PostID)
			liveSite := ""
			if p, ok := posts[c.PostID]; ok {
				if w, ok := projectPost(p, sites[p.Site], rev); ok {
					liveSite = p.Site
					bySite[p.Site] = append(bySite[p.Site], activityJob{write: w, eligible: w.Notify && !c.Backfill})
				}
			}
			for _, row := range stored[key] {
				if row.Removed || row.Site == liveSite {
					continue
				}
				bySite[row.Site] = append(bySite[row.Site], activityJob{write: repository.ActivityWrite{
					Key: key, ActorID: row.ActorID, Verb: row.Verb, ObjectKind: row.ObjectKind,
					Revision: rev, Removed: true, OccurredAt: time.Now(),
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
