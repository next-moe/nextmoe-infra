package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type ActivityFeedParams struct {
	UserID   int64
	ViewerID int64
	SFW      bool
	Sites    []string
	Verbs    []int16
	Before   *repository.ActivityCursor
	Limit    int
}

type ActivityGroup struct {
	repository.ActivityGroupRow
	Items []repository.ActivityItemRow
}

type ActivityFeedPage struct {
	Groups []ActivityGroup
	Limit  int
	Hidden bool
}

func (p ActivityFeedParams) query() repository.ActivityGroupQuery {
	limit := p.Limit
	if limit <= 0 || limit > activityFeedMax {
		limit = activityFeedDefault
	}
	return repository.ActivityGroupQuery{SFW: p.SFW, Sites: p.Sites, Verbs: p.Verbs, Before: p.Before, Limit: limit}
}

func (s *ActivityService) FollowingFeed(p ActivityFeedParams) (ActivityFeedPage, error) {
	q := p.query()
	q.FollowerID = p.UserID
	rows, err := repository.ListFollowingActivityGroups(s.db, q)
	if err != nil {
		return ActivityFeedPage{}, err
	}
	groups, err := s.withPreviews(rows, p.SFW)
	return ActivityFeedPage{Groups: groups, Limit: q.Limit}, err
}

func (s *ActivityService) ActorFeed(p ActivityFeedParams) (ActivityFeedPage, error) {
	q := p.query()
	q.ActorID = p.UserID
	if p.ViewerID != p.UserID {
		hidden, err := repository.ActivitiesHidden(s.db, p.UserID)
		if err != nil {
			return ActivityFeedPage{}, err
		}
		if hidden {
			return ActivityFeedPage{Groups: []ActivityGroup{}, Hidden: true}, nil
		}
	}
	rows, err := repository.ListActorActivityGroups(s.db, q)
	if err != nil {
		return ActivityFeedPage{}, err
	}
	groups, err := s.withPreviews(rows, p.SFW)
	return ActivityFeedPage{Groups: groups, Limit: q.Limit}, err
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

func (s *ActivityService) GroupItems(groupID, viewerID int64, sfw bool, before *repository.ActivityCursor, limit int) ([]repository.ActivityItemRow, int, error) {
	g, err := repository.GetActivityGroup(s.db, groupID)
	if err != nil {
		return nil, 0, err
	}
	if g == nil {
		return nil, 0, ErrActivityGroupNotFound
	}
	if g.ActorID != viewerID {
		hidden, err := repository.ActivitiesHidden(s.db, g.ActorID)
		if err != nil {
			return nil, 0, err
		}
		if hidden {
			return nil, 0, ErrActivityGroupNotFound
		}
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

type ActivitySetting struct {
	Hidden    bool
	UpdatedAt *time.Time
}

func (s *ActivityService) Setting(userID int64) (ActivitySetting, error) {
	row, err := repository.GetActivitySetting(s.db, userID)
	if err != nil || row == nil {
		return ActivitySetting{}, err
	}
	return ActivitySetting{Hidden: row.Hidden, UpdatedAt: &row.UpdatedAt}, nil
}

func (s *ActivityService) SetHidden(ctx context.Context, userID int64, hidden bool) (ActivitySetting, error) {
	var at time.Time
	var retracted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The dispatcher reads the switch under this lock, so no fold for the
		// user can be written after the retraction below; the retraction's seq
		// also needs it to commit in order.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", NotifyDispatchLockKey).Error; err != nil {
			return err
		}
		var err error
		if at, err = repository.UpsertActivitySettingTx(tx, userID, hidden); err != nil {
			return err
		}
		if hidden {
			retracted, err = repository.RetractFolloweeActivityTx(tx, userID)
		}
		return err
	})
	if err != nil {
		return ActivitySetting{}, err
	}
	slog.Info("community activity setting", "user_id", userID, "hidden", hidden, "notifications_retracted", retracted)
	return ActivitySetting{Hidden: hidden, UpdatedAt: &at}, nil
}
