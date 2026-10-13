package handler

import (
	"context"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

var recommendWords = []string{"strong_no", "no", "neutral", "yes", "strong_yes"}

var spoilerWords = []string{"none", "portion", "serious"}

func recommendValue(w string) int16 {
	for i, v := range recommendWords {
		if v == w {
			return model.RecommendStrongNo + int16(i)
		}
	}
	return 0
}

func spoilerValue(w string) int16 {
	for i, v := range spoilerWords {
		if v == w {
			return int16(i)
		}
	}
	return -1
}

func wordAt(words []string, i int) string {
	if i < 0 || i >= len(words) {
		return ""
	}
	return words[i]
}

func aspectValue(p *int) *int16 {
	if p == nil {
		return nil
	}
	v := int16(*p)
	return &v
}

func aspectInt(p *int16) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

type ratingAspectsBody struct {
	Art         *int `json:"art,omitempty" minimum:"1" maximum:"10" example:"8" doc:"Absent or null when not rated."`
	Story       *int `json:"story,omitempty" minimum:"1" maximum:"10" example:"8" doc:"Absent or null when not rated."`
	Music       *int `json:"music,omitempty" minimum:"1" maximum:"10" example:"9" doc:"Absent or null when not rated."`
	Character   *int `json:"character,omitempty" minimum:"1" maximum:"10" example:"8" doc:"Absent or null when not rated."`
	Route       *int `json:"route,omitempty" minimum:"1" maximum:"10" example:"7" doc:"Absent or null when not rated."`
	System      *int `json:"system,omitempty" minimum:"1" maximum:"10" example:"7" doc:"Absent or null when not rated."`
	Voice       *int `json:"voice,omitempty" minimum:"1" maximum:"10" example:"8" doc:"Absent or null when not rated."`
	ReplayValue *int `json:"replay_value,omitempty" minimum:"1" maximum:"10" example:"8" doc:"Absent or null when not rated."`
}

type ratingBody struct {
	Overall      int                `json:"overall" minimum:"1" maximum:"10" example:"8" doc:"The overall score, a whole number from 1 to 10."`
	AspectScores *ratingAspectsBody `json:"aspect_scores,omitempty" doc:"Per-aspect scores. Absent means none rated."`
	Recommend    string             `json:"recommend" enum:"strong_no,no,neutral,yes,strong_yes" example:"yes" doc:"How strongly the author recommends the work."`
	SpoilerLevel string             `json:"spoiler_level,omitempty" enum:"none,portion,serious" example:"none" doc:"How much short_summary gives away. Absent means none."`
	ShortSummary string             `json:"short_summary,omitempty" maxLength:"2000" example:"画面和音乐都很出色，主线一周目通关后还想再走一遍。" doc:"Short review, plain text. Absent or empty means none. Free text; must not be used as a discriminant."`
}

func (b ratingBody) input() catsvc.RatingInput {
	in := catsvc.RatingInput{
		Overall:      int16(b.Overall),
		Recommend:    recommendValue(b.Recommend),
		SpoilerLevel: model.SpoilerNone,
		ShortSummary: b.ShortSummary,
	}
	if b.SpoilerLevel != "" {
		in.SpoilerLevel = spoilerValue(b.SpoilerLevel)
	}
	if a := b.AspectScores; a != nil {
		in.Aspects = catsvc.RatingAspects{
			Art: aspectValue(a.Art), Story: aspectValue(a.Story), Music: aspectValue(a.Music),
			Character: aspectValue(a.Character), Route: aspectValue(a.Route), System: aspectValue(a.System),
			Voice: aspectValue(a.Voice), ReplayValue: aspectValue(a.ReplayValue),
		}
	}
	return in
}

func userRatingRepr(r catsvc.RatingRecord) repr.UserRating {
	a := r.Aspects
	return repr.UserRating{
		Object: "rating", ID: repr.ID(r.ID), WorkID: repr.ID(r.WorkID), UserID: repr.ID(r.ActorUID),
		Overall: int(r.Overall),
		AspectScores: repr.RatingAspectScores{
			Art: aspectInt(a.Art), Story: aspectInt(a.Story), Music: aspectInt(a.Music),
			Character: aspectInt(a.Character), Route: aspectInt(a.Route), System: aspectInt(a.System),
			Voice: aspectInt(a.Voice), ReplayValue: aspectInt(a.ReplayValue),
		},
		Recommend:    wordAt(recommendWords, int(r.Recommend-model.RecommendStrongNo)),
		SpoilerLevel: wordAt(spoilerWords, int(r.SpoilerLevel)),
		ShortSummary: r.ShortSummary,
		WorkState:    ratingAuthorState(r.AuthorState),
		ClientID:     r.ClientID, Site: r.Site,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func ratingAuthorState(s *catsvc.WorkStateRecord) *repr.RatingAuthorState {
	if s == nil {
		return nil
	}
	st, comp := workStateToWords(s.State, s.Completion)
	return &repr.RatingAuthorState{State: st, Completion: comp}
}

func userRatingPtr(r *catsvc.RatingRecord) *repr.UserRating {
	if r == nil {
		return nil
	}
	v := userRatingRepr(*r)
	return &v
}

// A rating is public text on every site at once, and only the sites run the
// trust check before writing it; an application a developer registered would
// publish around that check.
func requireFirstPartyWriter(ctx context.Context) error {
	if thirdPartyFrom(ctx) {
		return problem.New(problem.CodeFirstPartyOnly, "", "", "ratings are written through first-party sites; a third-party application may read them.")
	}
	return nil
}

func (c *Catalog) ListMyRatings(ctx context.Context, q collect.Query, workIDs []string) (repr.List[repr.UserRating], error) {
	var empty repr.List[repr.UserRating]
	if c == nil || c.Ratings == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	if len(workIDs) > 0 {
		ids, perr := parseWorkIDs(workIDs)
		if perr != nil {
			return empty, perr
		}
		rows, gerr := c.Ratings.ListMineFor(ctx, uid, ids)
		if gerr != nil {
			return empty, gerr
		}
		byWork := make(map[int64]catsvc.RatingRecord, len(rows))
		for _, r := range rows {
			byWork[r.WorkID] = r
		}
		items := make([]repr.UserRating, 0, len(ids))
		var missing []string
		for i, id := range ids {
			r, ok := byWork[id]
			if !ok {
				missing = append(missing, workIDs[i])
				continue
			}
			items = append(items, userRatingRepr(r))
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	since, sinceWorkID := time.Time{}, int64(0)
	if q.Cursor != "" {
		ts, id, ok := strings.Cut(q.Cursor, "|")
		wid, idOK := repr.ParseID(id)
		t, terr := time.Parse(time.RFC3339Nano, ts)
		if !ok || !idOK || terr != nil {
			return empty, collectInvalidCursor()
		}
		since, sinceWorkID = t, wid
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.Ratings.ListMine(ctx, uid, since, sinceWorkID, limit+1)
	if lerr != nil {
		return empty, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		s := last.ChangedAt.UTC().Format(time.RFC3339Nano) + "|" + repr.ID(last.WorkID)
		next = &s
	}
	items := make([]repr.UserRating, 0, len(rows))
	for _, r := range rows {
		items = append(items, userRatingRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Ratings.CountMine(ctx, uid); lerr != nil {
			return empty, lerr
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetMyRating(ctx context.Context, workID int64) (repr.UserRating, error) {
	if c == nil || c.Ratings == nil {
		return repr.UserRating{}, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserRating{}, err
	}
	row, gerr := c.Ratings.GetMine(ctx, uid, workID)
	if gerr != nil {
		return repr.UserRating{}, gerr
	}
	if row == nil {
		return repr.UserRating{}, problem.New(problem.CodeNotFound, "", "", "No rating on this work.")
	}
	return userRatingRepr(*row), nil
}

func (c *Catalog) PutMyRating(ctx context.Context, workID int64, body ratingBody) (repr.UserRating, error) {
	if c == nil || c.Ratings == nil {
		return repr.UserRating{}, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return repr.UserRating{}, err
	}
	if err := requireFirstPartyWriter(ctx); err != nil {
		return repr.UserRating{}, err
	}
	rec, gerr := c.Ratings.Set(ctx, uid, workID, client, siteFrom(ctx), body.input())
	if gerr != nil {
		return repr.UserRating{}, ratingErr(gerr, "")
	}
	return userRatingRepr(*rec), nil
}

func (c *Catalog) DeleteMyRating(ctx context.Context, workID int64) error {
	if c == nil || c.Ratings == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return ratingErr(c.Ratings.DeleteMine(ctx, uid, workID), "")
}

func ratingErr(err error, at string) error {
	if err == nil {
		return nil
	}
	field := func(pointer, reason, msg string, params *problem.FieldParams) error {
		p := problem.New(problem.CodeValidationFailed, "", "", msg)
		p.Errors = []problem.FieldError{{Pointer: at + pointer, Reason: reason, Detail: err.Error(), Params: params}}
		return p
	}
	scale := &problem.FieldParams{Minimum: problem.Ptr(1.0), Maximum: problem.Ptr(10.0)}
	switch err {
	case catsvc.ErrRatingBadOverall:
		return field("/overall", problem.ReasonOutOfRange, "overall is out of range.", scale)
	case catsvc.ErrRatingBadAspect:
		return field("/aspect_scores", problem.ReasonOutOfRange, "an aspect score is out of range.", scale)
	case catsvc.ErrRatingBadRecommend:
		return field("/recommend", problem.ReasonUnknownValue, "recommend is not in the vocabulary.",
			&problem.FieldParams{Allowed: &recommendWords})
	case catsvc.ErrRatingBadSpoiler:
		return field("/spoiler_level", problem.ReasonUnknownValue, "spoiler_level is not in the vocabulary.",
			&problem.FieldParams{Allowed: &spoilerWords})
	case catsvc.ErrRatingSummaryTooLong:
		return field("/short_summary", problem.ReasonTooLong, "short_summary is too long.",
			&problem.FieldParams{MaxLength: problem.Ptr(model.RatingSummaryMaxRunes)})
	case catsvc.ErrRatingSummaryEncoding:
		return field("/short_summary", problem.ReasonInvalidFormat, "short_summary is not valid UTF-8.", nil)
	case catsvc.ErrRatingWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for ratings.")
	case catsvc.ErrRatingNotFound:
		return problem.New(problem.CodeNotFound, "", "", "No such rating.")
	case catsvc.ErrRatingActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	}
	return err
}
