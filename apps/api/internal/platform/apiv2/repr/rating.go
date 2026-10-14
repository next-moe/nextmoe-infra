package repr

type RatingAspectScores struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Art         *int     `json:"art" minimum:"1" maximum:"10" doc:"null when not rated."`
	Story       *int     `json:"story" minimum:"1" maximum:"10" doc:"null when not rated."`
	Music       *int     `json:"music" minimum:"1" maximum:"10" doc:"null when not rated."`
	Character   *int     `json:"character" minimum:"1" maximum:"10" doc:"null when not rated."`
	Route       *int     `json:"route" minimum:"1" maximum:"10" doc:"null when not rated."`
	System      *int     `json:"system" minimum:"1" maximum:"10" doc:"null when not rated."`
	Voice       *int     `json:"voice" minimum:"1" maximum:"10" doc:"null when not rated."`
	ReplayValue *int     `json:"replay_value" minimum:"1" maximum:"10" doc:"null when not rated."`
}

type UserRating struct {
	_            struct{}           `json:"-" additionalProperties:"true"`
	Object       string             `json:"object" enum:"rating" doc:"Type discriminant. Always rating."`
	ID           string             `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Rating id. It survives edits and a merge of its work, and a deleted id never comes back, so a site that mirrors ratings keys on it."`
	WorkID       string             `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id this rating is on."`
	UserID       string             `json:"user_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The author. This is the central sign-in user id shared by every NextMoe site, not one site's own user id."`
	Overall      int                `json:"overall" minimum:"1" maximum:"10" doc:"The overall score, a whole number from 1 to 10."`
	AspectScores RatingAspectScores `json:"aspect_scores" doc:"Per-aspect scores; each is null when the author skipped it."`
	Recommend    string             `json:"recommend" enum:"strong_no,no,neutral,yes,strong_yes" doc:"How strongly the author recommends the work."`
	SpoilerLevel string             `json:"spoiler_level" enum:"none,portion,serious" doc:"How much short_summary gives away, as its author declared. A reader who hides spoilers sees the summary of anything above none masked."`
	ShortSummary string             `json:"short_summary" maxLength:"2000" doc:"The author's short review, plain text. Empty string if none. Must not be used as a discriminant."`
	ClientID     string             `json:"client_id" maxLength:"64" doc:"OAuth client of the app the rating was first written from; a later edit from another app does not change it. A site may compare it with its own client ids to tell the ratings written through it from the ones it only mirrors; beyond that it must not be used as a discriminant."`
	Site         string             `json:"site" maxLength:"64" doc:"Catalog site that app is bound to, empty when none. Open vocabulary; display only, must not be used as a discriminant."`
	CreatedAt    string             `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	WorkState    *RatingAuthorState `json:"work_state" doc:"The author's play state on this work as it stands now, not when the rating was written. null when the author has none. A change to it surfaces the rating on /v2/ratings/changes."`
	UpdatedAt    string             `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. When the author last edited the rating; a rating imported from a site keeps the site's time."`
}

type RatingAuthorState struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	State      string   `json:"state" enum:"wish,doing,done,on_hold,dropped" doc:"Closed vocabulary, as on /v2/me/work-states."`
	Completion *string  `json:"completion" enum:"one_route,main,all" doc:"How much is finished. null when unstated."`
}

type RatingChange struct {
	_         struct{}    `json:"-" additionalProperties:"true"`
	Object    string      `json:"object" enum:"rating_change" doc:"Type discriminant. Always rating_change."`
	ID        string      `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Rating id."`
	WorkID    string      `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Work the rating is on; on a gone entry, the work it was on when it was deleted."`
	Gone      bool        `json:"gone" doc:"true when the rating was deleted: drop the mirrored row."`
	ChangedAt string      `json:"changed_at" format:"date-time" maxLength:"40" doc:"RFC 3339 UTC with sub-second precision. When the catalog last changed the rating — an edit, its author's play state, a merge of its work, an import — or deleted it. The feed's order."`
	Rating    *UserRating `json:"rating" doc:"The rating as it stands now. null when gone."`
}

type WorkRecord struct {
	_         struct{}       `json:"-" additionalProperties:"true"`
	Object    string         `json:"object" enum:"work_record" doc:"Type discriminant. Always work_record."`
	WorkID    string         `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id."`
	WorkState *UserWorkState `json:"work_state" doc:"The account's play state, as GET /v2/me/work-states answers it to its owner. null when none is set; never null on /v2/work-records, which lists only works that have one."`
	Playtime  *UserPlaytime  `json:"playtime" doc:"The account's playtime, as GET /v2/me/playtimes answers it to its owner: the most minutes any app reported. null when none is recorded."`
	Rating    *UserRating    `json:"rating" doc:"The account's rating, as GET /v2/me/ratings answers it to its owner. null when none."`
}
