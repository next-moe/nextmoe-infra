package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"api/internal/canon/registry"
	"api/internal/canon/takedown"
	"api/internal/platform/markdown"

	"github.com/danielgtaylor/huma/v2"
)

// The words and codes are v2's, so one rating reads the same on both faces.
var (
	recommendWords = []string{"strong_no", "no", "neutral", "yes", "strong_yes"}
	spoilerWords   = []string{"none", "portion", "serious"}
)

const ratingSummaryMaxRunes = 2000

// legacyAspects are the game sub-scores v2 writes into their own columns; a
// rating v2 wrote shows them under these keys.
var legacyAspects = []string{"art", "story", "music", "character", "route", "system", "voice", "replay_value"}

type RatingBody struct {
	Overall      int            `json:"overall" minimum:"1" maximum:"10"`
	Dimensions   map[string]int `json:"dimensions,omitempty" doc:"Sub-scores of the work's medium (GET /v3/catalog/rating-dimensions), each 1 to 10; an absent one is unrated."`
	Recommend    string         `json:"recommend" enum:"strong_no,no,neutral,yes,strong_yes"`
	SpoilerLevel string         `json:"spoiler_level,omitempty" enum:"none,portion,serious" default:"none"`
	ShortSummary string         `json:"short_summary,omitempty" maxLength:"2000" doc:"Markdown, the dialect v2 takes; rendered into short_summary_html and short_summary_text."`
	ObservedAt   *time.Time     `json:"observed_at,omitempty" doc:"When the client last saw this rating; a later server write wins and answers 409 with it."`
}

type MyRating struct {
	WorkID           string         `json:"work_id"`
	Overall          int            `json:"overall"`
	Dimensions       map[string]int `json:"dimensions"`
	Recommend        string         `json:"recommend"`
	SpoilerLevel     string         `json:"spoiler_level"`
	ShortSummary     string         `json:"short_summary"`
	ShortSummaryHTML string         `json:"short_summary_html"`
	ShortSummaryText string         `json:"short_summary_text"`
	HasSpoiler       bool           `json:"short_summary_has_spoiler"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type PutRatingInput struct {
	AuthInput
	ID   int64 `path:"id" minimum:"1"`
	Body RatingBody
}

type DeleteRatingInput struct {
	AuthInput
	ID         int64  `path:"id" minimum:"1"`
	ObservedAt string `query:"observed_at" doc:"RFC 3339; as on PUT."`
}

type MyRatingOutput struct{ Body MyRating }

type DimensionSummary struct {
	Key   string  `json:"key"`
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
}

type RatingSummary struct {
	WorkID       string             `json:"work_id"`
	Count        int                `json:"count"`
	Mean         float64            `json:"mean" doc:"The plain mean of overall; 0 with no ratings."`
	Distribution []int              `json:"distribution" doc:"Ratings per overall score, index 0 being 1."`
	Recommend    map[string]int     `json:"recommend"`
	Dimensions   []DimensionSummary `json:"dimensions" doc:"In the medium's order; a dimension nobody rated is absent."`
}

type RatingSummaryOutput struct{ Body RatingSummary }

type RatingDimensionsOutput struct {
	Body map[string][]registry.RatingDimension
}

const ratingColumns = `overall, art, story, music, "character", route, "system", voice, replay_value, dimensions,
	recommend, spoiler_level, short_summary, short_summary_html, short_summary_text, short_summary_has_spoiler, updated_at, work_id`

func scanRating(rows *sql.Rows) (*MyRating, error) {
	var r MyRating
	aspects := make([]sql.NullInt16, len(legacyAspects))
	var dims []byte
	var recommend, spoiler int16
	var work int64
	dst := []any{&r.Overall}
	for i := range aspects {
		dst = append(dst, &aspects[i])
	}
	dst = append(dst, &dims, &recommend, &spoiler, &r.ShortSummary, &r.ShortSummaryHTML, &r.ShortSummaryText, &r.HasSpoiler, &r.UpdatedAt, &work)
	if err := rows.Scan(dst...); err != nil {
		return nil, err
	}
	r.WorkID = id(work)
	r.Dimensions = map[string]int{}
	if err := json.Unmarshal(dims, &r.Dimensions); err != nil {
		return nil, err
	}
	for i, a := range aspects {
		if _, set := r.Dimensions[legacyAspects[i]]; a.Valid && !set {
			r.Dimensions[legacyAspects[i]] = int(a.Int16)
		}
	}
	r.Recommend = wordAt(recommendWords, int(recommend)-1)
	r.SpoilerLevel = wordAt(spoilerWords, int(spoiler))
	return &r, nil
}

func wordAt(words []string, i int) string {
	if i < 0 || i >= len(words) {
		return ""
	}
	return words[i]
}

func indexOf(words []string, w string) int {
	for i, v := range words {
		if v == w {
			return i
		}
	}
	return -1
}

func (a *API) ratingsOf(ctx context.Context, uid int64, works []int64) (map[string]*MyRating, error) {
	rows, err := a.client.QueryContext(ctx, `SELECT `+ratingColumns+` FROM public.catalog_user_work_rating WHERE actor_uid = $1 AND work_id = ANY($2)`, uid, works)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*MyRating{}
	for rows.Next() {
		r, err := scanRating(rows)
		if err != nil {
			return nil, err
		}
		out[r.WorkID] = r
	}
	return out, rows.Err()
}

func (a *API) readRating(ctx context.Context, q queryFn, uid, work int64) (*MyRating, error) {
	rows, err := q(ctx, `SELECT `+ratingColumns+` FROM public.catalog_user_work_rating WHERE actor_uid = $1 AND work_id = $2`, uid, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanRating(rows)
}

// canonMedium is the medium of a work canon serves. A work still only in the
// legacy catalog is rated through v2, whose aggregates and game columns are
// its own, until its medium moves (P4).
func (a *API) canonMedium(ctx context.Context, n int64) (string, error) {
	medium, err := a.canonWork(ctx, n)
	if err != nil {
		return "", err
	}
	td, err := takedown.Gone(ctx, a.client, "work", n)
	if err != nil {
		return "", err
	}
	if td != nil {
		return "", huma.Error410Gone("work " + id(n) + " was taken down: " + td.Reason)
	}
	return medium, nil
}

func (a *API) canonWork(ctx context.Context, n int64) (string, error) {
	rows, err := a.client.QueryContext(ctx, `SELECT medium FROM canon.work WHERE id = $1`, n)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var medium string
	if rows.Next() {
		if err := rows.Scan(&medium); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if medium == "" {
		return "", huma.Error404NotFound("work " + id(n) + " is not served by v3; rate it through v2")
	}
	return medium, nil
}

func validateRating(medium string, b RatingBody) error {
	for k, v := range b.Dimensions {
		if !registry.HasRatingDimension(medium, k) {
			return huma.Error422UnprocessableEntity(fmt.Sprintf("%s is not a %s rating dimension (GET /v3/catalog/rating-dimensions)", k, medium))
		}
		if v < registry.RatingScoreMin || v > registry.RatingScoreMax {
			return huma.Error422UnprocessableEntity(fmt.Sprintf("dimension %s is %d; scores are %d to %d", k, v, registry.RatingScoreMin, registry.RatingScoreMax))
		}
	}
	if !utf8.ValidString(b.ShortSummary) || utf8.RuneCountInString(b.ShortSummary) > ratingSummaryMaxRunes {
		return huma.Error422UnprocessableEntity(fmt.Sprintf("short_summary is valid UTF-8 of at most %d characters", ratingSummaryMaxRunes))
	}
	return nil
}

// writeRating sets or (body nil) removes one rating, under the F16 rule
// writeState follows. An unchanged PUT leaves updated_at alone, as v2 does:
// mirrors pull on it.
func (a *API) writeRating(ctx context.Context, uid int64, clientID string, work int64, b *RatingBody, observed *time.Time) (*MyRating, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cur, err := a.readRating(ctx, func(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
		return tx.QueryContext(ctx, q+" FOR UPDATE", args...)
	}, uid, work)
	if err != nil {
		return nil, err
	}
	if cur != nil && observed != nil && cur.UpdatedAt.After(*observed) {
		p := problem(http.StatusConflict, "INVALID_STATE_TRANSITION", fmt.Sprintf("the rating changed at %s, after %s", cur.UpdatedAt.Format(time.RFC3339Nano), observed.Format(time.RFC3339Nano)))
		p.Current = cur
		return nil, p
	}
	if b == nil {
		if cur == nil {
			return nil, tx.Commit()
		}
		if _, err := tx.ExecContext(ctx, `WITH gone AS (DELETE FROM public.catalog_user_work_rating WHERE actor_uid = $1 AND work_id = $2 RETURNING id, work_id)
			INSERT INTO public.catalog_user_work_rating_tombstone (id, work_id, deleted_at) SELECT id, work_id, now() FROM gone`, uid, work); err != nil {
			return nil, err
		}
		if err := tx.LibraryTombstone.Create().SetActorUID(uid).SetKind("rating").SetSubjectID(work).Exec(ctx); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	dims, _ := json.Marshal(b.Dimensions)
	if b.Dimensions == nil {
		dims = []byte(`{}`)
	}
	cooked := markdown.CookShort(b.ShortSummary)
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.catalog_user_work_rating AS t (actor_uid, work_id, overall, dimensions, recommend, spoiler_level,
			short_summary, short_summary_html, short_summary_text, short_summary_has_spoiler, short_summary_cooker, client_id, site, created_at, updated_at, changed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, '', now(), now(), now())
		ON CONFLICT (actor_uid, work_id) DO UPDATE SET overall = EXCLUDED.overall, dimensions = EXCLUDED.dimensions,
			art = NULL, story = NULL, music = NULL, "character" = NULL, route = NULL, "system" = NULL, voice = NULL, replay_value = NULL,
			recommend = EXCLUDED.recommend, spoiler_level = EXCLUDED.spoiler_level, short_summary = EXCLUDED.short_summary,
			short_summary_html = EXCLUDED.short_summary_html, short_summary_text = EXCLUDED.short_summary_text,
			short_summary_has_spoiler = EXCLUDED.short_summary_has_spoiler, short_summary_cooker = EXCLUDED.short_summary_cooker,
			updated_at = now(), changed_at = now()
		WHERE (t.overall, t.dimensions, t.recommend, t.spoiler_level, t.short_summary, t.short_summary_cooker,
				t.art, t.story, t.music, t."character", t.route, t."system", t.voice, t.replay_value)
			IS DISTINCT FROM (EXCLUDED.overall, EXCLUDED.dimensions, EXCLUDED.recommend, EXCLUDED.spoiler_level, EXCLUDED.short_summary,
				EXCLUDED.short_summary_cooker, NULL::smallint, NULL::smallint, NULL::smallint, NULL::smallint, NULL::smallint, NULL::smallint, NULL::smallint, NULL::smallint)`,
		uid, work, b.Overall, dims, indexOf(recommendWords, b.Recommend)+1, max(indexOf(spoilerWords, b.SpoilerLevel), 0),
		b.ShortSummary, cooked.HTML, cooked.Text, cooked.HasSpoiler, cooked.Version, clientID); err != nil {
		return nil, err
	}
	r, err := a.readRating(ctx, tx.QueryContext, uid, work)
	if err != nil {
		return nil, err
	}
	return r, tx.Commit()
}

func (a *API) ratings(h huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity}
	huma.Register(h, huma.Operation{OperationID: "listRatingDimensions", Method: http.MethodGet, Path: "/v3/catalog/rating-dimensions",
		Summary: "Rating sub-scores by medium", Tags: []string{"library"}},
		func(context.Context, *struct{}) (*RatingDimensionsOutput, error) {
			return &RatingDimensionsOutput{Body: registry.RatingDimensions}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "getMyRating", Method: http.MethodGet, Path: "/v3/me/works/{id}/rating",
		Summary: "My rating of a work", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *MeWorkInput) (*MyRatingOutput, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			r, err := a.readRating(ctx, a.client.QueryContext, uid, in.ID)
			if err != nil {
				return nil, err
			}
			if r == nil {
				return nil, huma.Error404NotFound("no rating of work " + id(in.ID))
			}
			return &MyRatingOutput{Body: *r}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "putMyRating", Method: http.MethodPut, Path: "/v3/me/works/{id}/rating",
		Summary: "Rate a work", Description: "Replaces the whole rating. Only works canon serves; a legacy-only work is rated through v2.",
		Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *PutRatingInput) (*MyRatingOutput, error) {
			claims, err := a.viewer(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			medium, err := a.canonMedium(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := validateRating(medium, in.Body); err != nil {
				return nil, err
			}
			r, err := a.writeRating(ctx, int64(claims.ID), claims.ClientID, in.ID, &in.Body, in.Body.ObservedAt)
			if err != nil {
				return nil, err
			}
			return &MyRatingOutput{Body: *r}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "deleteMyRating", Method: http.MethodDelete, Path: "/v3/me/works/{id}/rating",
		Summary: "Remove my rating of a work", Tags: []string{"library"}, Errors: errs},
		func(ctx context.Context, in *DeleteRatingInput) (*struct{}, error) {
			uid, err := a.user(ctx, in.Authorization)
			if err != nil {
				return nil, err
			}
			if _, err := a.canonWork(ctx, in.ID); err != nil {
				return nil, err
			}
			var observed *time.Time
			if in.ObservedAt != "" {
				t, perr := time.Parse(time.RFC3339Nano, in.ObservedAt)
				if perr != nil {
					return nil, huma.Error400BadRequest("observed_at is RFC 3339")
				}
				observed = &t
			}
			_, err = a.writeRating(ctx, uid, "", in.ID, nil, observed)
			return &struct{}{}, err
		})
	huma.Register(h, huma.Operation{OperationID: "getWorkRatingSummary", Method: http.MethodGet, Path: "/v3/catalog/works/{id}/rating-summary",
		Summary: "How a work is rated", Tags: []string{"works"}, Errors: []int{http.StatusNotFound, http.StatusGone}},
		func(ctx context.Context, in *IDInput) (*RatingSummaryOutput, error) {
			medium, err := a.canonMedium(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			s, err := a.ratingSummary(ctx, in.ID, medium)
			if err != nil {
				return nil, err
			}
			return &RatingSummaryOutput{Body: *s}, nil
		})
}

func (a *API) ratingSummary(ctx context.Context, work int64, medium string) (*RatingSummary, error) {
	s := &RatingSummary{WorkID: id(work), Distribution: make([]int, registry.RatingScoreMax), Recommend: map[string]int{}, Dimensions: []DimensionSummary{}}
	for _, w := range recommendWords {
		s.Recommend[w] = 0
	}
	rows, err := a.client.QueryContext(ctx, `SELECT overall, recommend, count(*) FROM public.catalog_user_work_rating WHERE work_id = $1 GROUP BY 1, 2`, work)
	if err != nil {
		return nil, err
	}
	sum := 0
	for rows.Next() {
		var overall, recommend, n int
		if err := rows.Scan(&overall, &recommend, &n); err != nil {
			rows.Close()
			return nil, err
		}
		s.Count += n
		sum += overall * n
		if overall >= registry.RatingScoreMin && overall <= registry.RatingScoreMax {
			s.Distribution[overall-1] += n
		}
		if w := wordAt(recommendWords, recommend-1); w != "" {
			s.Recommend[w] += n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.Count > 0 {
		s.Mean = float64(sum) / float64(s.Count)
	}
	rows, err = a.client.QueryContext(ctx, `SELECT d.key, count(*), avg(d.value::int) FROM public.catalog_user_work_rating r, jsonb_each_text(r.dimensions) d
		WHERE r.work_id = $1 GROUP BY d.key`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]DimensionSummary{}
	for rows.Next() {
		var d DimensionSummary
		if err := rows.Scan(&d.Key, &d.Count, &d.Mean); err != nil {
			return nil, err
		}
		got[d.Key] = d
	}
	for _, d := range registry.RatingDimensions[medium] {
		if g, ok := got[d.Key]; ok {
			s.Dimensions = append(s.Dimensions, g)
		}
	}
	return s, rows.Err()
}
