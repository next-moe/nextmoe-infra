package service

import (
	"time"

	"api/internal/platform/community/repository"
)

type ActivityFeedParams struct {
	UserID int64
	SFW    bool
	Sites  []string
	Verbs  []int16
	Before *repository.ActivityCursor
	Limit  int
}

type ActivityGroup struct {
	repository.ActivityGroupRow
	Items []repository.ActivityItemRow
}

func (p ActivityFeedParams) query() repository.ActivityGroupQuery {
	limit := p.Limit
	if limit <= 0 || limit > activityFeedMax {
		limit = activityFeedDefault
	}
	return repository.ActivityGroupQuery{SFW: p.SFW, Sites: p.Sites, Verbs: p.Verbs, Before: p.Before, Limit: limit}
}

func (s *ActivityService) FollowingFeed(p ActivityFeedParams) ([]ActivityGroup, int, error) {
	q := p.query()
	q.FollowerID = p.UserID
	rows, err := repository.ListFollowingActivityGroups(s.db, q)
	if err != nil {
		return nil, 0, err
	}
	groups, err := s.withPreviews(rows, p.SFW)
	return groups, q.Limit, err
}

func (s *ActivityService) ActorFeed(p ActivityFeedParams) ([]ActivityGroup, int, error) {
	q := p.query()
	q.ActorID = p.UserID
	rows, err := repository.ListActorActivityGroups(s.db, q)
	if err != nil {
		return nil, 0, err
	}
	groups, err := s.withPreviews(rows, p.SFW)
	return groups, q.Limit, err
}

func (s *ActivityService) withPreviews(rows []repository.ActivityGroupRow, sfw bool) ([]ActivityGroup, error) {
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	items, err := repository.ListActivityGroupPreviews(s.db, ids, sfw, activityPreviewItems)
	if err != nil {
		return nil, err
	}
	byGroup := map[int64][]repository.ActivityItemRow{}
	for _, it := range items {
		byGroup[it.GroupID] = append(byGroup[it.GroupID], it)
	}
	out := make([]ActivityGroup, len(rows))
	for i := range rows {
		out[i] = ActivityGroup{ActivityGroupRow: rows[i], Items: byGroup[rows[i].ID]}
	}
	return out, nil
}

func (s *ActivityService) GroupItems(groupID int64, sfw bool, before *repository.ActivityCursor, limit int) ([]repository.ActivityItemRow, int, error) {
	g, err := repository.GetActivityGroup(s.db, groupID)
	if err != nil {
		return nil, 0, err
	}
	if g == nil {
		return nil, 0, ErrActivityGroupNotFound
	}
	if limit <= 0 || limit > activityFeedMax {
		limit = activityFeedDefault
	}
	items, err := repository.ListActivityGroupItems(s.db, g, sfw, before, limit)
	return items, limit, err
}

func (s *ActivityService) Unseen(p ActivityFeedParams) (int, *time.Time, error) {
	q := p.query()
	q.FollowerID = p.UserID
	n, err := repository.CountUnseenActivityGroups(s.db, q, activityUnseenCap)
	if err != nil {
		return 0, nil, err
	}
	seen, err := repository.GetFeedSeen(s.db, p.UserID)
	return n, seen, err
}

func (s *ActivityService) MarkSeen(userID int64, at *time.Time) (time.Time, error) {
	return repository.AdvanceFeedSeen(s.db, userID, at)
}
