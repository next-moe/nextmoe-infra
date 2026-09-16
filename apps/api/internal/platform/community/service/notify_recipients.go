package service

import (
	"encoding/json"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type notifyCandidate struct {
	site       string
	userID     int64
	kind       int16
	fromAnchor bool
}

func kindPriority(kind int16) int {
	switch kind {
	case model.NotificationKindReplied:
		return 4
	case model.NotificationKindMentioned:
		return 3
	case model.NotificationKindThreadCreated:
		return 2
	case model.NotificationKindPosted:
		return 1
	default:
		return 0
	}
}

func recipientsForEvent(tx *gorm.DB, ev *model.CommunityEvent, thread *model.CommunityThread, post *model.CommunityPost) ([]notifyCandidate, error) {
	threadUsers, err := repository.ListThreadUsersTx(tx, thread.ID)
	if err != nil {
		return nil, err
	}
	anchorUsers, err := repository.ListAnchorUsersForThreadTx(tx, thread)
	if err != nil {
		return nil, err
	}

	byUser := make(map[int64]model.CommunityThreadUser, len(threadUsers))
	for _, tu := range threadUsers {
		byUser[tu.UserID] = tu
	}
	type auKey struct {
		site string
		user int64
	}
	byAnchor := make(map[auKey]model.CommunityAnchorUser, len(anchorUsers))
	for _, au := range anchorUsers {
		byAnchor[auKey{au.Site, au.UserID}] = au
	}

	deliveryFor := func(userID int64) string {
		if tu, ok := byUser[userID]; ok {
			if tu.Site != nil && *tu.Site != "" {
				return *tu.Site
			}
			return thread.Site
		}
		return ev.Site
	}

	type ck struct {
		site string
		user int64
	}
	cands := map[ck]notifyCandidate{}
	add := func(c notifyCandidate) {
		if c.userID == 0 {
			return
		}
		k := ck{c.site, c.userID}
		if prev, ok := cands[k]; ok {
			np, pp := kindPriority(c.kind), kindPriority(prev.kind)
			if np < pp {
				return
			}
			if np == pp && !prev.fromAnchor {
				return
			}
			if np == pp && prev.fromAnchor && c.fromAnchor {
				return
			}
		}
		cands[k] = c
	}

	switch ev.Kind {
	case model.EventKindPostCreated:
		if ev.TargetUserID != nil {
			uid := *ev.TargetUserID
			add(notifyCandidate{site: deliveryFor(uid), userID: uid, kind: model.NotificationKindReplied})
		}
		mentions, err := parseMentionIDs(ev.MentionUserIDs)
		if err != nil {
			return nil, err
		}
		for _, uid := range mentions {
			add(notifyCandidate{site: deliveryFor(uid), userID: uid, kind: model.NotificationKindMentioned})
		}
		if post != nil && post.PostNumber > 1 {
			for _, tu := range threadUsers {
				if tu.NotificationLevel == model.NotificationLevelWatching {
					add(notifyCandidate{site: deliveryFor(tu.UserID), userID: tu.UserID, kind: model.NotificationKindPosted})
				}
			}
		}
		firstNonComment := post != nil && post.PostNumber == 1 && thread.Kind != model.ThreadKindComments
		for _, au := range anchorUsers {
			var k int16
			switch au.NotificationLevel {
			case model.NotificationLevelWatching:
				if firstNonComment {
					k = model.NotificationKindThreadCreated
				} else {
					k = model.NotificationKindPosted
				}
			case model.NotificationLevelWatchingFirstPost:
				if !firstNonComment {
					continue
				}
				k = model.NotificationKindThreadCreated
			default:
				continue
			}
			add(notifyCandidate{site: au.Site, userID: au.UserID, kind: k, fromAnchor: true})
		}
	case model.EventKindPostLiked:
		if post != nil {
			add(notifyCandidate{site: deliveryFor(post.AuthorID), userID: post.AuthorID, kind: model.NotificationKindLiked})
		}
	case model.EventKindFeedbackStatusChanged:
		if _, ok := byUser[thread.CreatedBy]; ok {
			add(notifyCandidate{site: deliveryFor(thread.CreatedBy), userID: thread.CreatedBy, kind: model.NotificationKindFeedbackStatus})
		}
		for _, tu := range threadUsers {
			if tu.NotificationLevel == model.NotificationLevelWatching {
				add(notifyCandidate{site: deliveryFor(tu.UserID), userID: tu.UserID, kind: model.NotificationKindFeedbackStatus})
			}
		}
	case model.EventKindAnswerAccepted:
		if post != nil {
			add(notifyCandidate{site: deliveryFor(post.AuthorID), userID: post.AuthorID, kind: model.NotificationKindAnswerAccepted})
		}
	}

	effective := func(userID int64, deliverySite string) int16 {
		if tu, ok := byUser[userID]; ok {
			return tu.NotificationLevel
		}
		if au, ok := byAnchor[auKey{deliverySite, userID}]; ok {
			return au.NotificationLevel
		}
		return model.NotificationLevelNormal
	}

	out := make([]notifyCandidate, 0, len(cands))
	for _, c := range cands {
		if c.userID == ev.ActorID {
			continue
		}
		if effective(c.userID, c.site) == model.NotificationLevelMuted {
			continue
		}
		if c.fromAnchor {
			if _, ok := byUser[c.userID]; ok {
				continue
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func writeNotification(tx *gorm.DB, ev *model.CommunityEvent, thread *model.CommunityThread, post *model.CommunityPost, like *model.CommunityReaction, c *notifyCandidate) error {
	actorID := ev.ActorID
	n := model.CommunityNotification{
		Site:       c.site,
		UserID:     c.userID,
		Kind:       c.kind,
		ThreadID:   thread.ID,
		AnchorKind: thread.AnchorKind,
		AnchorID:   thread.AnchorID,
		ActorID:    &actorID,
		ActorCount: 1,
		ItemCount:  1,
	}
	if post != nil {
		postID, num := post.ID, post.PostNumber
		n.PostID, n.PostNumber, n.FirstPostNumber = &postID, &num, &num
	}

	switch c.kind {
	case model.NotificationKindPosted:
		key := repository.PostedFoldKey(thread.ID)
		n.FoldKey = &key
		row, err := repository.UpsertFoldedNotificationTx(tx, &n, false)
		if err != nil {
			return err
		}
		return repository.RecomputePostedFoldTx(tx, row.ID)
	case model.NotificationKindLiked:
		key := repository.LikedFoldKey(*n.PostID)
		n.FoldKey = &key
		if like != nil {
			t := like.CreatedAt
			n.SinceAt = &t
		}
		row, err := repository.UpsertFoldedNotificationTx(tx, &n, false)
		if err != nil {
			return err
		}
		return repository.RecomputeLikedFoldTx(tx, row.ID)
	case model.NotificationKindFeedbackStatus:
		key := repository.FeedbackFoldKey(thread.ID)
		n.FoldKey = &key
		_, err := repository.UpsertFoldedNotificationTx(tx, &n, true)
		return err
	default:
		return repository.InsertNotificationTx(tx, &n)
	}
}

func normalizeMentionIDs(authorID int64, ids []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || id == authorID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > mentionIDLimit {
		return nil, &InvalidError{Reason: "too many mention_user_ids (max 20)"}
	}
	return out, nil
}

func mentionIDsJSON(ids []int64) datatypes.JSON {
	if len(ids) == 0 {
		return nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

func parseMentionIDs(raw datatypes.JSON) ([]int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}
