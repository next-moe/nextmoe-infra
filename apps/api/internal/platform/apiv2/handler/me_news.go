package handler

import (
	"context"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	newsmodel "api/internal/platform/news/model"
	newssvc "api/internal/platform/news/service"
)

type newsSubmissionBody struct {
	Source      string
	Lane        string
	Title       string
	Summary     string
	SourceURL   string
	PublishedAt string
	BannerHash  string
	WorkIDs     []string
}

type newsPatchBody struct {
	Status     *string
	Title      *string
	Summary    *string
	SourceURL  *string
	BannerHash *string
	WorkIDs    *[]string
}

func (c *Catalog) ListMyNews(ctx context.Context, q collect.Query) (repr.List[repr.NewsSubmission], error) {
	uid, err := c.newsActor(ctx)
	if err != nil {
		return repr.List[repr.NewsSubmission]{}, err
	}
	before, berr := newsItemCursor(q.Cursor)
	if berr != nil {
		return repr.List[repr.NewsSubmission]{}, berr
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.NewsWrite.List(ctx, uid, before, limit+1)
	if lerr != nil {
		return repr.List[repr.NewsSubmission]{}, newsWriteErr(lerr)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		next = &s
	}
	var total int64
	if q.IncludeTotal {
		total, lerr = c.NewsWrite.Count(ctx, uid)
		if lerr != nil {
			return repr.List[repr.NewsSubmission]{}, newsWriteErr(lerr)
		}
	}
	items := make([]repr.NewsSubmission, 0, len(rows))
	for _, r := range rows {
		items = append(items, newsSubmissionRecord(r))
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetMyNews(ctx context.Context, id int64) (repr.NewsSubmission, string, error) {
	uid, err := c.newsActor(ctx)
	if err != nil {
		return repr.NewsSubmission{}, "", err
	}
	row, gerr := c.NewsWrite.Get(ctx, uid, id)
	if gerr != nil {
		return repr.NewsSubmission{}, "", newsWriteErr(gerr)
	}
	return newsSubmissionRecord(row), newsETag(row), nil
}

func (c *Catalog) CreateMyNews(ctx context.Context, in newsSubmissionBody) (repr.NewsSubmission, string, error) {
	uid, err := c.newsActor(ctx)
	if err != nil {
		return repr.NewsSubmission{}, "", err
	}
	p := problem.New(problem.CodeValidationFailed, "", "", "the submission is not acceptable.")
	if strings.TrimSpace(in.Source) == "" {
		p.Errors = append(p.Errors, problem.FieldError{Pointer: "/source", Reason: problem.ReasonRequired, Detail: "name one of your news sources"})
	}
	lane := strings.TrimSpace(in.Lane)
	if lane == "" {
		lane = newsmodel.LaneNews
	}
	if !newsmodel.IsKnownLane(lane) {
		p.Errors = append(p.Errors, problem.FieldError{Pointer: "/lane", Reason: problem.ReasonUnknownValue, Detail: "expected one of: news, column",
			Params: &problem.FieldParams{Allowed: &[]string{"news", "column"}}})
	}
	if strings.TrimSpace(in.Title) == "" {
		p.Errors = append(p.Errors, problem.FieldError{Pointer: "/title", Reason: problem.ReasonRequired, Detail: "a news item needs a title"})
	}
	p.Errors = append(p.Errors, newsSummaryErrors("/summary", in.Summary)...)
	p.Errors = append(p.Errors, newsSourceURLErrors("/source_url", in.SourceURL)...)
	p.Errors = append(p.Errors, newsBannerErrors("/banner_hash", in.BannerHash)...)
	published, perrs := newsPublishedAt(in.PublishedAt)
	p.Errors = append(p.Errors, perrs...)
	workIDs, werrs := newsWorkIDs(in.WorkIDs)
	p.Errors = append(p.Errors, werrs...)
	if len(p.Errors) > 0 {
		return repr.NewsSubmission{}, "", p
	}
	row, cerr := c.NewsWrite.Create(ctx, newssvc.CreateParams{
		PublisherUID: uid, SourceKey: strings.TrimSpace(in.Source), Lane: lane,
		Title: in.Title, Preview: in.Summary, SourceURL: in.SourceURL,
		BannerHash: in.BannerHash, PublishedAt: published, WorkIDs: workIDs,
	})
	if cerr != nil {
		return repr.NewsSubmission{}, "", newsWriteErr(cerr)
	}
	return newsSubmissionRecord(row), newsETag(row), nil
}

func (c *Catalog) PatchMyNews(ctx context.Context, id int64, in newsPatchBody, ifMatch string) (repr.NewsSubmission, string, error) {
	uid, err := c.newsActor(ctx)
	if err != nil {
		return repr.NewsSubmission{}, "", err
	}
	cur, gerr := c.NewsWrite.Get(ctx, uid, id)
	if gerr != nil {
		return repr.NewsSubmission{}, "", newsWriteErr(gerr)
	}
	etag := newsETag(cur)
	edits := in.Title != nil || in.Summary != nil || in.SourceURL != nil || in.BannerHash != nil || in.WorkIDs != nil
	if in.Status == nil && !edits {
		p := problem.New(problem.CodeValidationFailed, "", "", "this patch changes nothing.")
		p.Errors = []problem.FieldError{{Pointer: "/status", Reason: problem.ReasonRequired, Detail: "send status=withdrawn, or at least one of title, summary, source_url, banner_hash, work_ids"}}
		return repr.NewsSubmission{}, "", p
	}
	if in.Status != nil {
		if edits {
			p := problem.New(problem.CodeValidationFailed, "", "", "a withdrawal cannot carry a text edit.")
			p.Errors = []problem.FieldError{{Pointer: "/status", Reason: problem.ReasonInconsistentWith, Detail: "withdraw on its own; the other members of this body edit text and are only legal while pending"}}
			return repr.NewsSubmission{}, "", p
		}
		if *in.Status != "withdrawn" {
			p := problem.New(problem.CodeValidationFailed, "", "", "the write face only performs the withdrawal transition.")
			p.Errors = []problem.FieldError{{Pointer: "/status", Reason: problem.ReasonUnknownValue, Detail: "expected one of: withdrawn",
				Params: &problem.FieldParams{Allowed: &[]string{"withdrawn"}}}}
			return repr.NewsSubmission{}, "", p
		}
		if merr := requireIfMatch(ifMatch, etag); merr != nil {
			return repr.NewsSubmission{}, "", merr
		}
		if cur.Status != newsmodel.StatusPublished {
			return repr.NewsSubmission{}, "", newsTransitionRefusal(cur.Status)
		}
		row, werr := c.NewsWrite.Withdraw(ctx, uid, id)
		if werr != nil {
			return repr.NewsSubmission{}, "", newsWriteErr(werr)
		}
		return newsSubmissionRecord(row), newsETag(row), nil
	}
	if cur.Status != newsmodel.StatusPending {
		return repr.NewsSubmission{}, "", newsTransitionRefusal(cur.Status)
	}
	if strings.TrimSpace(ifMatch) != "" {
		if merr := requireIfMatch(ifMatch, etag); merr != nil {
			return repr.NewsSubmission{}, "", merr
		}
	}
	p := problem.New(problem.CodeValidationFailed, "", "", "the edit is not acceptable.")
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		p.Errors = append(p.Errors, problem.FieldError{Pointer: "/title", Reason: problem.ReasonRequired, Detail: "a news item needs a title"})
	}
	if in.Summary != nil {
		p.Errors = append(p.Errors, newsSummaryErrors("/summary", *in.Summary)...)
	}
	if in.SourceURL != nil {
		p.Errors = append(p.Errors, newsSourceURLErrors("/source_url", *in.SourceURL)...)
	}
	if in.BannerHash != nil {
		p.Errors = append(p.Errors, newsBannerErrors("/banner_hash", *in.BannerHash)...)
	}
	params := newssvc.UpdateParams{
		Title: in.Title, Preview: in.Summary, SourceURL: in.SourceURL, BannerHash: in.BannerHash,
	}
	if in.WorkIDs != nil {
		ids, werrs := newsWorkIDs(*in.WorkIDs)
		p.Errors = append(p.Errors, werrs...)
		params.WorkIDs = &ids
	}
	if len(p.Errors) > 0 {
		return repr.NewsSubmission{}, "", p
	}
	row, uerr := c.NewsWrite.Update(ctx, uid, id, params)
	if uerr != nil {
		return repr.NewsSubmission{}, "", newsWriteErr(uerr)
	}
	return newsSubmissionRecord(row), newsETag(row), nil
}

func (c *Catalog) newsActor(ctx context.Context) (int64, error) {
	if c == nil || c.NewsWrite == nil {
		return 0, problem.New(problem.CodeServiceUnavailable, "", "", "news submissions are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return 0, err
	}
	return uid, nil
}
