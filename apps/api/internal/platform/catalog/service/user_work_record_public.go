package service

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WorkRecordSetState    = "state"
	WorkRecordSetPlaytime = "playtime"
	WorkRecordSetAny      = "any"
)

type PublicWorkRecordFilter struct {
	Record        string
	States        []int16
	NoState       bool
	DisplayLimits []string
}

const (
	WorkRecordSortUpdated  = "updated"
	WorkRecordSortPlaytime = "playtime"
	WorkRecordSortRating   = "rating"
)

type WorkRecordSort struct {
	Key  string
	Desc bool
}

// WorkRecordKey is a row's position under one sort: At for updated, Value for
// playtime and rating, Null when the record has no such value.
type WorkRecordKey struct {
	Null   bool
	At     time.Time
	Value  int64
	WorkID int64
}

type PublicWorkRecord struct {
	WorkRecord
	Key WorkRecordKey
}

type WorkRecordCounts struct {
	ByState         map[int16]int64
	NoState         int64
	PlaytimeMinutes int64
}

type AccountPrivacy struct {
	Private                 bool
	FollowedCompaniesPublic bool
	UpdatedAt               *time.Time
}

type PrivacyChange struct {
	PlayRecordsPrivate      *bool
	FollowedCompaniesPublic *bool
}

func (s *UserWorkRecordService) Privacy(ctx context.Context, uid int64) (AccountPrivacy, error) {
	if uid <= 0 {
		return AccountPrivacy{}, ErrWorkRecordActorRequired
	}
	var rows []model.CatalogUserPrivacy
	if err := s.db.WithContext(ctx).Where("actor_uid = ?", uid).Limit(1).Find(&rows).Error; err != nil {
		return AccountPrivacy{}, err
	}
	if len(rows) == 0 {
		return AccountPrivacy{}, nil
	}
	at := rows[0].UpdatedAt
	return AccountPrivacy{Private: rows[0].PlayRecordsPrivate, FollowedCompaniesPublic: rows[0].FollowedCompaniesPublic, UpdatedAt: &at}, nil
}

func (s *UserWorkRecordService) SetPrivacy(ctx context.Context, uid int64, c PrivacyChange) (AccountPrivacy, error) {
	if uid <= 0 {
		return AccountPrivacy{}, ErrWorkRecordActorRequired
	}
	row := model.CatalogUserPrivacy{ActorUID: uid}
	set := map[string]any{"updated_at": time.Now()}
	var changed []clause.Expression
	if c.PlayRecordsPrivate != nil {
		row.PlayRecordsPrivate = *c.PlayRecordsPrivate
		set["play_records_private"] = *c.PlayRecordsPrivate
		changed = append(changed, clause.Expr{SQL: "catalog_user_privacy.play_records_private IS DISTINCT FROM excluded.play_records_private"})
	}
	if c.FollowedCompaniesPublic != nil {
		row.FollowedCompaniesPublic = *c.FollowedCompaniesPublic
		set["followed_companies_public"] = *c.FollowedCompaniesPublic
		changed = append(changed, clause.Expr{SQL: "catalog_user_privacy.followed_companies_public IS DISTINCT FROM excluded.followed_companies_public"})
	}
	if len(changed) == 0 {
		return s.Privacy(ctx, uid)
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "actor_uid"}},
			DoUpdates: clause.Assignments(set),
			Where:     clause.Where{Exprs: []clause.Expression{clause.Or(changed...)}},
		}).Create(&row).Error; err != nil {
			return err
		}
		if c.PlayRecordsPrivate != nil && *c.PlayRecordsPrivate {
			if err := retractPlayRecordFeedTx(tx, uid); err != nil {
				return err
			}
		}
		if c.FollowedCompaniesPublic != nil && !*c.FollowedCompaniesPublic {
			return retractCompanyFollowsFeedTx(tx, uid)
		}
		return nil
	})
	if err != nil {
		return AccountPrivacy{}, err
	}
	return s.Privacy(ctx, uid)
}

type workRecordRow struct {
	WorkID     int64
	State      *int16
	Completion *int16
	StCreated  *time.Time
	StUpdated  *time.Time
	Minutes    *int64
	Overall    *int64
	UpdatedAt  time.Time
}

func (s *UserWorkRecordService) recordSet(uid int64, f PublicWorkRecordFilter) (string, []any) {
	stateKeys := "SELECT work_id FROM catalog_user_work_state WHERE actor_uid = ?"
	playtimeKeys := "SELECT work_id FROM catalog_user_playtime WHERE actor_uid = ?"
	ratingKeys := "SELECT work_id FROM catalog_user_work_rating WHERE actor_uid = ?"
	var keys string
	var args []any
	switch f.Record {
	case WorkRecordSetPlaytime:
		keys, args = playtimeKeys, []any{uid}
	case WorkRecordSetAny:
		keys, args = stateKeys+" UNION "+playtimeKeys+" UNION "+ratingKeys, []any{uid, uid, uid}
	default:
		keys, args = stateKeys, []any{uid}
	}
	sql := `WITH k AS (SELECT DISTINCT work_id FROM (` + keys + `) u),
rec AS (
	SELECT k.work_id, st.state, st.completion, st.created_at AS st_created, st.updated_at AS st_updated,
		pt.minutes, rt.overall,
		COALESCE(st.updated_at, GREATEST(pt.updated_at, rt.updated_at)) AS updated_at
	FROM k
	JOIN ` + workDirectory + ` w ON w.id = k.work_id AND w.status = ?
	LEFT JOIN catalog_user_work_state st ON st.actor_uid = ? AND st.work_id = k.work_id
	LEFT JOIN (SELECT work_id, max(minutes)::bigint AS minutes, max(updated_at) AS updated_at
		FROM catalog_user_playtime WHERE actor_uid = ? GROUP BY work_id) pt ON pt.work_id = k.work_id
	LEFT JOIN catalog_user_work_rating rt ON rt.actor_uid = ? AND rt.work_id = k.work_id
	WHERE true`
	args = append(args, model.WorkStatusLive, uid, uid, uid)
	switch {
	case len(f.States) > 0 && f.NoState:
		sql += " AND (st.state IN ? OR st.state IS NULL)"
		args = append(args, f.States)
	case len(f.States) > 0:
		sql += " AND st.state IN ?"
		args = append(args, f.States)
	case f.NoState:
		sql += " AND st.state IS NULL"
	}
	if pred, pargs := displayLimitWhere(f.DisplayLimits); pred != "" {
		sql += " AND " + pred
		args = append(args, pargs...)
	}
	return sql + "\n)\n", args
}

func workRecordSortColumn(key string) string {
	switch key {
	case WorkRecordSortPlaytime:
		return "minutes"
	case WorkRecordSortRating:
		return "overall"
	default:
		return "updated_at"
	}
}

// Rows without the sort value come last in both directions; work_id breaks
// ties in the sort's own direction.
func workRecordPage(sort WorkRecordSort, after *WorkRecordKey) (string, []any) {
	col := workRecordSortColumn(sort.Key)
	cmp, dir := ">", "ASC"
	if sort.Desc {
		cmp, dir = "<", "DESC"
	}
	order := " ORDER BY (" + col + " IS NULL) ASC, " + col + " " + dir + ", work_id " + dir
	if after == nil {
		return order, nil
	}
	if after.Null {
		return " WHERE " + col + " IS NULL AND work_id " + cmp + " ?" + order, []any{after.WorkID}
	}
	var v any = after.Value
	if sort.Key == WorkRecordSortUpdated {
		v = after.At
	}
	return " WHERE ((" + col + ", work_id) " + cmp + " (?, ?) OR " + col + " IS NULL)" + order, []any{v, after.WorkID}
}

func (s *UserWorkRecordService) ListPublic(ctx context.Context, uid int64, f PublicWorkRecordFilter, sort WorkRecordSort, after *WorkRecordKey, limit int) ([]PublicWorkRecord, error) {
	if uid <= 0 {
		return nil, ErrWorkRecordActorRequired
	}
	with, args := s.recordSet(uid, f)
	page, pargs := workRecordPage(sort, after)
	var rows []workRecordRow
	if err := s.db.WithContext(ctx).Raw(with+"SELECT * FROM rec"+page+" LIMIT ?", append(append(args, pargs...), limit)...).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.WorkID
	}
	playtimes, err := NewUserPlaytimeService(s.db).ListMineFor(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := NewUserRatingService(s.db).ListMineFor(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	playtime := make(map[int64]*UserWorkPlaytime, len(playtimes))
	for i := range playtimes {
		playtime[playtimes[i].WorkID] = &playtimes[i]
	}
	rating := make(map[int64]*RatingRecord, len(ratings))
	for i := range ratings {
		rating[ratings[i].WorkID] = &ratings[i]
	}
	out := make([]PublicWorkRecord, 0, len(rows))
	for _, r := range rows {
		rec := PublicWorkRecord{
			WorkRecord: WorkRecord{WorkID: r.WorkID, Playtime: playtime[r.WorkID], Rating: rating[r.WorkID]},
			Key:        workRecordKey(sort.Key, r),
		}
		if r.State != nil {
			rec.WorkState = &WorkStateRecord{
				WorkID: r.WorkID, State: *r.State, Completion: r.Completion,
				CreatedAt: *r.StCreated, UpdatedAt: *r.StUpdated,
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

func workRecordKey(sort string, r workRecordRow) WorkRecordKey {
	k := WorkRecordKey{WorkID: r.WorkID}
	var v *int64
	switch sort {
	case WorkRecordSortPlaytime:
		v = r.Minutes
	case WorkRecordSortRating:
		v = r.Overall
	default:
		k.At = r.UpdatedAt
		return k
	}
	if v == nil {
		k.Null = true
	} else {
		k.Value = *v
	}
	return k
}

func (s *UserWorkRecordService) CountPublic(ctx context.Context, uid int64, f PublicWorkRecordFilter) (WorkRecordCounts, error) {
	out := WorkRecordCounts{ByState: map[int16]int64{}}
	if uid <= 0 {
		return out, ErrWorkRecordActorRequired
	}
	with, args := s.recordSet(uid, f)
	var rows []struct {
		State   *int16
		N       int64
		Minutes int64
	}
	if err := s.db.WithContext(ctx).Raw(with+"SELECT state, count(*) AS n, COALESCE(sum(minutes), 0)::bigint AS minutes FROM rec GROUP BY state", args...).
		Scan(&rows).Error; err != nil {
		return out, err
	}
	for _, r := range rows {
		if r.State == nil {
			out.NoState = r.N
		} else {
			out.ByState[*r.State] = r.N
		}
		out.PlaytimeMinutes += r.Minutes
	}
	return out, nil
}
