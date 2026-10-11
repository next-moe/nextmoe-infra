package service

import (
	"context"
	stderrors "errors"
	"time"
	"unicode/utf8"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRatingActorRequired   = stderrors.New("catalog: a rating requires a reporting user")
	ErrRatingWorkUnavailable = stderrors.New("catalog: work not available for ratings")
	ErrRatingNotFound        = stderrors.New("catalog: no such rating")
	ErrRatingBadOverall      = stderrors.New("catalog: overall must be between 1 and 10")
	ErrRatingBadAspect       = stderrors.New("catalog: an aspect score must be between 1 and 10")
	ErrRatingBadRecommend    = stderrors.New("catalog: unknown recommendation")
	ErrRatingBadSpoiler      = stderrors.New("catalog: unknown spoiler level")
	ErrRatingSummaryTooLong  = stderrors.New("catalog: short summary exceeds 2000 characters")
	ErrRatingSummaryEncoding = stderrors.New("catalog: short summary is not valid UTF-8")
)

type RatingAspects struct {
	Art, Story, Music, Character, Route, System, Voice, ReplayValue *int16
}

func (a RatingAspects) all() []*int16 {
	return []*int16{a.Art, a.Story, a.Music, a.Character, a.Route, a.System, a.Voice, a.ReplayValue}
}

type RatingInput struct {
	Overall      int16
	Aspects      RatingAspects
	Recommend    int16
	SpoilerLevel int16
	ShortSummary string
}

func (in RatingInput) Validate() error {
	if in.Overall < model.RatingScoreMin || in.Overall > model.RatingScoreMax {
		return ErrRatingBadOverall
	}
	for _, v := range in.Aspects.all() {
		if v != nil && (*v < model.RatingScoreMin || *v > model.RatingScoreMax) {
			return ErrRatingBadAspect
		}
	}
	if in.Recommend < model.RecommendStrongNo || in.Recommend > model.RecommendStrongYes {
		return ErrRatingBadRecommend
	}
	switch in.SpoilerLevel {
	case model.SpoilerNone, model.SpoilerMild, model.SpoilerSevere:
	default:
		return ErrRatingBadSpoiler
	}
	if !utf8.ValidString(in.ShortSummary) {
		return ErrRatingSummaryEncoding
	}
	if utf8.RuneCountInString(in.ShortSummary) > model.RatingSummaryMaxRunes {
		return ErrRatingSummaryTooLong
	}
	return nil
}

type RatingRecord struct {
	ID       int64
	ActorUID int64
	WorkID   int64
	RatingInput
	ClientID    string
	Site        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ChangedAt   time.Time
	AuthorState *WorkStateRecord
}

func ratingRecord(r model.CatalogUserWorkRating) RatingRecord {
	return RatingRecord{
		ID: r.ID, ActorUID: r.ActorUID, WorkID: r.WorkID,
		RatingInput: RatingInput{
			Overall: r.Overall,
			Aspects: RatingAspects{
				Art: r.Art, Story: r.Story, Music: r.Music, Character: r.Character,
				Route: r.Route, System: r.System, Voice: r.Voice, ReplayValue: r.ReplayValue,
			},
			Recommend: r.Recommend, SpoilerLevel: r.SpoilerLevel, ShortSummary: r.ShortSummary,
		},
		ClientID: r.ClientID, Site: r.Site, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ChangedAt: r.ChangedAt,
	}
}

func ratingRecords(db *gorm.DB, rows []model.CatalogUserWorkRating) ([]RatingRecord, error) {
	out := make([]RatingRecord, 0, len(rows))
	pairs := make([][]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, ratingRecord(r))
		pairs = append(pairs, []any{r.ActorUID, r.WorkID})
	}
	if len(pairs) == 0 {
		return out, nil
	}
	var states []model.CatalogUserWorkState
	if err := db.Where("(actor_uid, work_id) IN ?", pairs).Find(&states).Error; err != nil {
		return nil, err
	}
	by := make(map[[2]int64]model.CatalogUserWorkState, len(states))
	for _, s := range states {
		by[[2]int64{s.ActorUID, s.WorkID}] = s
	}
	for i := range out {
		if s, ok := by[[2]int64{out[i].ActorUID, out[i].WorkID}]; ok {
			out[i].AuthorState = &WorkStateRecord{
				WorkID: s.WorkID, State: s.State, Completion: s.Completion,
				CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
			}
		}
	}
	return out, nil
}

func oneRating(db *gorm.DB, row model.CatalogUserWorkRating) (*RatingRecord, error) {
	recs, err := ratingRecords(db, []model.CatalogUserWorkRating{row})
	if err != nil {
		return nil, err
	}
	return &recs[0], nil
}

func touchRating(tx *gorm.DB, uid, workID int64) error {
	return tx.Exec(`UPDATE catalog_user_work_rating SET changed_at = now() WHERE actor_uid = ? AND work_id = ?`,
		uid, workID).Error
}

type UserRatingService struct{ db *gorm.DB }

func NewUserRatingService(db *gorm.DB) *UserRatingService {
	return &UserRatingService{db: db}
}

func (s *UserRatingService) Set(ctx context.Context, uid, workID int64, clientID, site string, in RatingInput) (*RatingRecord, error) {
	if uid <= 0 {
		return nil, ErrRatingActorRequired
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var out *RatingRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertReportableWork(tx, workID); err != nil {
			if err == ErrPlaytimeWorkUnavailable {
				return ErrRatingWorkUnavailable
			}
			return err
		}
		rec, err := setRatingTx(tx, uid, workID, clientID, site, in)
		out = rec
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// An unchanged PUT must not bump updated_at: mirrors pull on it, and a folder
// PUT that did churned every synced client.
func setRatingTx(tx *gorm.DB, uid, workID int64, clientID, site string, in RatingInput) (*RatingRecord, error) {
	a := in.Aspects
	row := model.CatalogUserWorkRating{
		ActorUID: uid, WorkID: workID, Overall: in.Overall,
		Art: a.Art, Story: a.Story, Music: a.Music, Character: a.Character,
		Route: a.Route, System: a.System, Voice: a.Voice, ReplayValue: a.ReplayValue,
		Recommend: in.Recommend, SpoilerLevel: in.SpoilerLevel, ShortSummary: in.ShortSummary,
		ClientID: clientID, Site: site,
	}
	content := []string{"overall", "art", "story", "music", "character", "route", "system",
		"voice", "replay_value", "recommend", "spoiler_level", "short_summary"}
	res := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "actor_uid"}, {Name: "work_id"}},
		DoUpdates: append(clause.AssignmentColumns(content),
			clause.Assignment{Column: clause.Column{Name: "updated_at"}, Value: gorm.Expr("now()")},
			clause.Assignment{Column: clause.Column{Name: "changed_at"}, Value: gorm.Expr("now()")}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr(rowDiffers("catalog_user_work_rating", content))}},
	}).Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	var stored model.CatalogUserWorkRating
	if err := tx.Where("actor_uid = ? AND work_id = ?", uid, workID).Take(&stored).Error; err != nil {
		return nil, err
	}
	if res.RowsAffected > 0 {
		if _, err := refreshRatingAggregates(tx, []int64{workID}); err != nil {
			return nil, err
		}
	}
	return oneRating(tx, stored)
}

func rowDiffers(table string, cols []string) string {
	left, right := "", ""
	for i, c := range cols {
		if i > 0 {
			left += ", "
			right += ", "
		}
		left += table + `."` + c + `"`
		right += `EXCLUDED."` + c + `"`
	}
	return "(" + left + ") IS DISTINCT FROM (" + right + ")"
}

func (s *UserRatingService) Get(ctx context.Context, id int64) (*RatingRecord, error) {
	var row model.CatalogUserWorkRating
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRatingNotFound
	}
	if err != nil {
		return nil, err
	}
	return oneRating(s.db.WithContext(ctx), row)
}

func (s *UserRatingService) GetMine(ctx context.Context, uid, workID int64) (*RatingRecord, error) {
	got, err := s.ListMineFor(ctx, uid, []int64{workID})
	if err != nil || len(got) == 0 {
		return nil, err
	}
	return &got[0], nil
}

func (s *UserRatingService) ListMineFor(ctx context.Context, uid int64, workIDs []int64) ([]RatingRecord, error) {
	if uid <= 0 {
		return nil, ErrRatingActorRequired
	}
	if len(workIDs) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserWorkRating
	if err := s.db.WithContext(ctx).
		Where("actor_uid = ? AND work_id IN ?", uid, workIDs).
		Order("work_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return ratingRecords(s.db.WithContext(ctx), rows)
}

func (s *UserRatingService) ListMine(ctx context.Context, uid int64, since time.Time, sinceWorkID int64, limit int) ([]RatingRecord, error) {
	if uid <= 0 {
		return nil, ErrRatingActorRequired
	}
	q := s.db.WithContext(ctx).Where("actor_uid = ?", uid)
	if !since.IsZero() {
		q = q.Where("(changed_at, work_id) > (?, ?)", since, sinceWorkID)
	}
	var rows []model.CatalogUserWorkRating
	if err := q.Order("changed_at ASC, work_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ratingRecords(s.db.WithContext(ctx), rows)
}

func (s *UserRatingService) CountMine(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrRatingActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserWorkRating{}).Where("actor_uid = ?", uid).Count(&n).Error
	return n, err
}

func (s *UserRatingService) DeleteMine(ctx context.Context, uid, workID int64) error {
	if uid <= 0 {
		return ErrRatingActorRequired
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := deleteRatingsTx(tx, "actor_uid = ? AND work_id = ?", uid, workID)
		return err
	})
}

func (s *UserRatingService) ModerationDelete(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := deleteRatingsTx(tx, "id = ?", id)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrRatingNotFound
		}
		return nil
	})
}

func deleteRatingsTx(tx *gorm.DB, where string, args ...any) (int64, error) {
	var gone []struct {
		ID     int64
		WorkID int64
	}
	if err := tx.Raw(`WITH gone AS (DELETE FROM catalog_user_work_rating WHERE `+where+` RETURNING id, work_id)
		INSERT INTO catalog_user_work_rating_tombstone (id, work_id, deleted_at)
		SELECT id, work_id, now() FROM gone
		RETURNING id, work_id`, args...).Scan(&gone).Error; err != nil {
		return 0, err
	}
	if len(gone) == 0 {
		return 0, nil
	}
	works := make([]int64, 0, len(gone))
	for _, g := range gone {
		works = append(works, g.WorkID)
	}
	if _, err := refreshRatingAggregates(tx, works); err != nil {
		return 0, err
	}
	return int64(len(gone)), nil
}

type RatingFilter struct {
	WorkID int64
	UserID int64
}

func (f RatingFilter) scope(q *gorm.DB) *gorm.DB {
	if f.WorkID > 0 {
		q = q.Where("work_id = ?", f.WorkID)
	}
	if f.UserID > 0 {
		q = q.Where("actor_uid = ?", f.UserID)
	}
	return q
}

func (s *UserRatingService) List(ctx context.Context, f RatingFilter, beforeAt time.Time, beforeID int64, limit int) ([]RatingRecord, error) {
	q := f.scope(s.db.WithContext(ctx))
	if !beforeAt.IsZero() {
		q = q.Where("(created_at, id) < (?, ?)", beforeAt, beforeID)
	}
	var rows []model.CatalogUserWorkRating
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ratingRecords(s.db.WithContext(ctx), rows)
}

func (s *UserRatingService) Count(ctx context.Context, f RatingFilter) (int64, error) {
	var n int64
	err := f.scope(s.db.WithContext(ctx).Model(&model.CatalogUserWorkRating{})).Count(&n).Error
	return n, err
}

func (s *UserRatingService) ChangesTotal(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw(`SELECT (SELECT count(*) FROM catalog_user_work_rating) +
		(SELECT count(*) FROM catalog_user_work_rating_tombstone)`).Scan(&n).Error
	return n, err
}

type RatingChange struct {
	ID        int64
	WorkID    int64
	ChangedAt time.Time
	Gone      bool
	Rating    *RatingRecord
}

// The five-second settle is /v2/catalog/changes's: a transaction stamps
// now() at its start, so a slow commit can land behind a cursor that has
// already passed its timestamp.
func (s *UserRatingService) Changes(ctx context.Context, afterAt time.Time, afterID int64, limit int) ([]RatingChange, error) {
	var heads []struct {
		ID        int64
		WorkID    int64
		ChangedAt time.Time
		Gone      bool
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id, work_id, changed_at, gone FROM (
		    SELECT id, work_id, changed_at, false AS gone
		      FROM catalog_user_work_rating
		     WHERE (changed_at, id) > (?, ?) AND changed_at < now() - interval '5 seconds'
		    UNION ALL
		    SELECT id, work_id, deleted_at, true
		      FROM catalog_user_work_rating_tombstone
		     WHERE (deleted_at, id) > (?, ?) AND deleted_at < now() - interval '5 seconds'
		) c
		ORDER BY changed_at ASC, id ASC
		LIMIT ?`, afterAt, afterID, afterAt, afterID, limit).Scan(&heads).Error; err != nil {
		return nil, err
	}
	var live []int64
	for _, h := range heads {
		if !h.Gone {
			live = append(live, h.ID)
		}
	}
	byID := map[int64]RatingRecord{}
	if len(live) > 0 {
		var rows []model.CatalogUserWorkRating
		if err := s.db.WithContext(ctx).Where("id IN ?", live).Find(&rows).Error; err != nil {
			return nil, err
		}
		recs, err := ratingRecords(s.db.WithContext(ctx), rows)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			byID[r.ID] = r
		}
	}
	out := make([]RatingChange, 0, len(heads))
	for _, h := range heads {
		ch := RatingChange{ID: h.ID, WorkID: h.WorkID, ChangedAt: h.ChangedAt, Gone: h.Gone}
		if !h.Gone {
			rec, ok := byID[h.ID]
			if !ok {
				continue
			}
			ch.Rating = &rec
		}
		out = append(out, ch)
	}
	return out, nil
}
