package ratingimport

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"
)

type rawRow struct {
	ID           int64
	UserID       int64
	WorkID       int64
	Overall      int16
	Art          *int16
	Story        *int16
	Music        *int16
	Character    *int16
	Route        *int16
	System       *int16
	Voice        *int16
	ReplayValue  *int16
	Recommend    string
	SpoilerLevel string
	ShortSummary string
	PlayStatus   string
	AllRoutes    bool
	Created      time.Time
	Updated      time.Time
}

const forumQuery = `SELECT id, user_id, work_id, overall, art, story, music, "character", route,
	"system", voice, replay_value, recommend, spoiler_level, short_summary, play_status,
	false AS all_routes, created, updated
	FROM galgame_rating ORDER BY id`

const letmoeQuery = `SELECT id, user_id, game_id AS work_id, overall, art, story, music, "character", route,
	"system", voice, replay_value, recommend, spoiler_level, short_summary, play_status,
	all_routes, created, updated
	FROM game_rating ORDER BY id`

var recommends = map[string]int16{
	"strong_no": model.RecommendStrongNo, "no": model.RecommendNo, "neutral": model.RecommendNeutral,
	"yes": model.RecommendYes, "strong_yes": model.RecommendStrongYes,
}

var spoilers = map[string]int16{
	"none": model.SpoilerNone, "portion": model.SpoilerMild, "serious": model.SpoilerSevere,
}

type playState struct {
	state      int16
	completion int16
}

var forumStates = map[string]playState{
	"wish":           {model.WorkStateWish, 0},
	"doing":          {model.WorkStateDoing, 0},
	"done_one_route": {model.WorkStateDone, model.WorkCompletionOneRoute},
	"done_main":      {model.WorkStateDone, model.WorkCompletionMain},
	"done_all":       {model.WorkStateDone, model.WorkCompletionAll},
	"on_hold":        {model.WorkStateOnHold, 0},
	"dropped":        {model.WorkStateDropped, 0},
}

var letmoeStates = map[string]playState{
	"in_progress": {model.WorkStateDoing, 0},
	"finished":    {model.WorkStateDone, 0},
	"dropped":     {model.WorkStateDropped, 0},
}

func readSource(ctx context.Context, src Source) ([]sourceRow, error) {
	query, states := forumQuery, forumStates
	if src.Site == SiteLetmoe {
		query, states = letmoeQuery, letmoeStates
	}
	var raw []rawRow
	if err := src.DB.WithContext(ctx).Raw(query).Scan(&raw).Error; err != nil {
		return nil, err
	}
	out := make([]sourceRow, 0, len(raw))
	for _, r := range raw {
		row := sourceRow{
			ID: r.ID, UserID: r.UserID, WorkID: r.WorkID, Overall: r.Overall,
			Aspects:      [8]*int16{aspect(r.Art), aspect(r.Story), aspect(r.Music), aspect(r.Character), aspect(r.Route), aspect(r.System), aspect(r.Voice), aspect(r.ReplayValue)},
			Recommend:    recommends[r.Recommend],
			SpoilerLevel: -1,
			ShortSummary: r.ShortSummary,
			Created:      r.Created, Updated: r.Updated,
		}
		if v, ok := spoilers[r.SpoilerLevel]; ok {
			row.SpoilerLevel = v
		}
		if ps, ok := states[r.PlayStatus]; ok {
			row.State = ps.state
			if ps.completion != 0 {
				c := ps.completion
				row.Completion = &c
			} else if src.Site == SiteLetmoe && r.AllRoutes && ps.state == model.WorkStateDone {
				c := model.WorkCompletionAll
				row.Completion = &c
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// The forum stores a skipped aspect as 0.
func aspect(v *int16) *int16 {
	if v == nil || *v == 0 {
		return nil
	}
	return v
}
