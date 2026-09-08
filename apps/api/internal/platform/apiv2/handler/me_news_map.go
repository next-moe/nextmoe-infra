package handler

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	newsmodel "api/internal/platform/news/model"
	newssvc "api/internal/platform/news/service"
)

func newsSummaryErrors(pointer, value string) []problem.FieldError {
	if strings.TrimSpace(value) == "" {
		return []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonRequired, Detail: "the lede the source wrote"}}
	}
	if utf8.RuneCountInString(value) > newsmodel.PreviewMaxRunes {
		return []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonTooLong, Detail: "at most 200 runes; the body lives at source_url"}}
	}
	return nil
}

func newsSourceURLErrors(pointer, value string) []problem.FieldError {
	if strings.TrimSpace(value) == "" {
		return []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonRequired, Detail: "attribution must carry a link to the original"}}
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return []problem.FieldError{{Pointer: pointer, Reason: problem.ReasonInvalidFormat, Detail: "expected an absolute http or https URL"}}
	}
	return nil
}

func newsBannerErrors(pointer, value string) []problem.FieldError {
	bad := []problem.FieldError{{
		Pointer: pointer, Reason: problem.ReasonInvalidFormat,
		Detail: "expected a 64-character lowercase hex image-service hash",
	}}
	if value == "" {
		return nil
	}
	if len(value) != 64 {
		return bad
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return bad
		}
	}
	return nil
}

func newsPublishedAt(raw string) (time.Time, []problem.FieldError) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, []problem.FieldError{{Pointer: "/published_at", Reason: problem.ReasonInvalidFormat, Detail: "expected RFC 3339, e.g. 2026-08-25T12:00:00Z"}}
	}
	return t, nil
}

func newsWorkIDs(raw []string) ([]int64, []problem.FieldError) {
	out := make([]int64, 0, len(raw))
	var errs []problem.FieldError
	for i, s := range raw {
		n, ok := repr.ParseID(s)
		if !ok {
			errs = append(errs, problem.FieldError{
				Pointer: "/work_ids/" + strconv.Itoa(i), Reason: problem.ReasonInvalidFormat,
				Detail: "expected a decimal catalog work id",
			})
			continue
		}
		out = append(out, n)
	}
	return out, errs
}

func newsTransitionRefusal(status int16) error {
	switch status {
	case newsmodel.StatusPending:
		return problem.New(problem.CodeInvalidStateTransition, "", "",
			"the item is pending; publishing and rejection happen in the moderation queue, and withdrawal is only legal once it is published.")
	case newsmodel.StatusPublished:
		return problem.New(problem.CodeInvalidStateTransition, "", "",
			`the item is published; the only legal transition is {"status":"withdrawn"}.`)
	case newsmodel.StatusRejected:
		return problem.New(problem.CodeInvalidStateTransition, "", "",
			"the item is rejected, which is terminal; submit a new item instead.")
	default:
		return problem.New(problem.CodeInvalidStateTransition, "", "",
			"the item is withdrawn; it has no legal transition on this face.")
	}
}

func newsItemCursor(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, collectInvalidCursor()
	}
	return n, nil
}

func newsWriteErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, newssvc.ErrSourceNotYours):
		return problem.New(problem.CodeSourceNotYours, "", "", "this news source is not bound to your account.")
	case errors.Is(err, newssvc.ErrSourceInactive):
		return problem.New(problem.CodeSourceInactive, "", "", "this news source is deactivated; ask a NextMoe operator to restore it.")
	case errors.Is(err, newssvc.ErrNotFound):
		return problem.New(problem.CodeNotFound, "", "", "no news item with this id under your sources.")
	case errors.Is(err, newssvc.ErrNotEditable):
		return problem.New(problem.CodeInvalidStateTransition, "", "", "text is editable only while the item is pending.")
	case errors.Is(err, newssvc.ErrIllegalTransition):
		return problem.New(problem.CodeInvalidStateTransition, "", "", err.Error())
	case errors.Is(err, newssvc.ErrPreviewTooLong):
		p := problem.New(problem.CodeValidationFailed, "", "", "the edit is not acceptable.")
		p.Errors = []problem.FieldError{{Pointer: "/summary", Reason: problem.ReasonTooLong, Detail: "at most 200 runes; the body lives at source_url"}}
		return p
	}
	return err
}

func newsSubmissionRecord(s newssvc.Submission) repr.NewsSubmission {
	works := make([]string, 0, len(s.WorkIDs))
	for _, id := range s.WorkIDs {
		works = append(works, repr.ID(id))
	}
	return repr.NewsSubmission{
		Object: "news_submission",
		ID:     repr.ID(s.ID),
		Source: repr.NewsSource{Object: "news_source", Name: s.SourceKey,
			DisplayName: s.SourceDisplayName, HomepageURL: s.SourceHomepageURL,
			Attribution: s.SourceAttribution, ColumnURL: s.SourceColumnURL},
		Lane:        s.Lane,
		Status:      newsStatusToken(s.Status),
		Title:       s.Title,
		Summary:     s.Preview,
		SourceURL:   s.SourceURL,
		Banner:      newsBanner(s.BannerHash, s.BannerURL),
		PublishedAt: repr.TimeUTC(s.PublishedAt),
		WorkIDs:     works,
	}
}

func newsStatusToken(status int16) string {
	switch status {
	case newsmodel.StatusPublished:
		return "published"
	case newsmodel.StatusRejected:
		return "rejected"
	case newsmodel.StatusWithdrawn:
		return "withdrawn"
	default:
		return "pending"
	}
}

func newsETag(s newssvc.Submission) string {
	return `"n` + repr.ID(s.ID) + "." + strconv.FormatInt(s.UpdatedAt.UnixMicro(), 10) + `"`
}
