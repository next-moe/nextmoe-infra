package handler

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"api/internal/platform/telemetry/dto"
	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/pkg/errors"
)

func validatePrefixes(prefixes []string) error {
	if len(prefixes) > 20 {
		return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "in_app_prefixes must have at most 20 entries")
	}
	for _, p := range prefixes {
		n := utf8.RuneCountInString(p)
		if n < 1 || n > 100 {
			return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "each in_app_prefix must be 1–100 characters")
		}
	}
	return nil
}

type listIssuesInput struct {
	AppID   int64  `query:"app_id" required:"true"`
	Kind    string `query:"kind"`
	Status  string `query:"status"`
	Version string `query:"version"`
	From    string `query:"from"`
	To      string `query:"to"`
	Sort    string `query:"sort"`
	Limit   int    `query:"limit"`
	Offset  int    `query:"offset"`
}
type listIssuesOutput struct {
	Body Envelope[[]dto.IssueListItem]
}

func (s *AdminServer) listIssues(ctx context.Context, in *listIssuesInput) (*listIssuesOutput, error) {
	to := time.Now().UTC()
	if in.To != "" {
		t, err := time.Parse("2006-01-02", in.To)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "to must be YYYY-MM-DD")
		}
		to = t
	}
	from := to.AddDate(0, 0, -13)
	if in.From != "" {
		t, err := time.Parse("2006-01-02", in.From)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "from must be YYYY-MM-DD")
		}
		from = t
	}
	if from.After(to) {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "from must not be after to")
	}
	status := in.Status
	if status == "" {
		status = model.IssueOpen
	}
	switch status {
	case model.IssueOpen, model.IssueResolved, model.IssueIgnored:
	default:
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "status must be open, resolved, or ignored")
	}
	sort := in.Sort
	if sort == "" {
		sort = "events"
	}
	if sort != "events" && sort != "last_seen" {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "sort must be events or last_seen")
	}
	rows, err := s.store.ListIssues(ctx, store.IssueListParams{
		AppID:   in.AppID,
		Kind:    in.Kind,
		Status:  status,
		Version: in.Version,
		From:    from,
		To:      to,
		Sort:    sort,
		Limit:   in.Limit,
		Offset:  in.Offset,
	})
	if err != nil {
		return nil, mapAdminErr("list issues", err)
	}
	if rows == nil {
		rows = []dto.IssueListItem{}
	}
	return &listIssuesOutput{Body: okEnvelope(rows)}, nil
}

type getIssueInput struct {
	ID int64 `path:"id"`
}
type getIssueOutput struct {
	Body Envelope[dto.IssueDetail]
}

func (s *AdminServer) getIssue(ctx context.Context, in *getIssueInput) (*getIssueOutput, error) {
	row, err := s.store.GetIssue(ctx, in.ID, time.Now().UTC())
	if err != nil {
		return nil, mapAdminErr("get issue", err)
	}
	return &getIssueOutput{Body: okEnvelope(*row)}, nil
}

type updateIssueInput struct {
	ID   int64 `path:"id"`
	Body dto.UpdateIssueRequest
}
type updateIssueOutput struct {
	Body Envelope[dto.IssueView]
}

func (s *AdminServer) updateIssue(ctx context.Context, in *updateIssueInput) (*updateIssueOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	switch in.Body.Status {
	case model.IssueOpen, model.IssueResolved, model.IssueIgnored:
	default:
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "status must be open, resolved, or ignored")
	}
	row, err := s.store.UpdateIssue(ctx, in.ID, in.Body.Status, in.Body.ResolvedInVersion)
	if err != nil {
		return nil, mapAdminErr("update issue", err)
	}
	return &updateIssueOutput{Body: okEnvelope(dto.IssueViewFrom(*row))}, nil
}
