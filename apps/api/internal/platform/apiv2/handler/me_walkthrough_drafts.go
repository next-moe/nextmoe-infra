package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/walkthroughdraft"

	"github.com/danielgtaylor/huma/v2"
)

type walkthroughDraftInput struct {
	Body struct {
		Text string `json:"text" minLength:"1" maxLength:"60000" doc:"A walkthrough as plain text, such as choices written one per line under route headings. It is sent to a language model and not stored. Must not be used as a discriminant."`
	}
}

type walkthroughDraftOutput struct {
	Body walkthroughDraftBody
}

type walkthroughDraftBody struct {
	_           struct{}       `json:"-" additionalProperties:"true"`
	Object      string         `json:"object" enum:"walkthrough_draft" doc:"Type discriminant. Always walkthrough_draft."`
	Verdict     string         `json:"verdict" enum:"ok,review,rejected,no_chart" doc:"The import gate's verdict on the draft: review means choices or endings look missing, rejected means some text is not in the input, no_chart means the text holds no choice chart."`
	Chart       map[string]any `json:"chart" doc:"The draft chart, the same value catalog.walkthrough.chart takes. null when verdict is no_chart."`
	Coverage    float64        `json:"coverage" minimum:"0" maximum:"1" doc:"Share of the input's choice lines the chart carries."`
	Missed      []string       `json:"missed" doc:"Input lines the chart does not carry, at most 8. Empty array, never null."`
	NotVerbatim []string       `json:"not_verbatim" doc:"Chart texts that are not in the input. Empty array, never null."`
	Remaining   int            `json:"remaining" minimum:"0" doc:"Drafts the caller has left in the rolling 24-hour window."`
}

func registerMeWalkthroughDrafts(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "createMyWalkthroughDraft",
		Method:      http.MethodPost,
		Path:        "/v2/me/walkthrough-drafts",
		Summary:     "Turn a text walkthrough into a draft chart",
		Description: fmt.Sprintf("A language model reads the text and returns a choice chart in the shape catalog.walkthrough.chart takes, with the import gate's verdict on it. Nothing is written to the catalog and the text is not stored; the caller edits the draft and files it as a proposal. "+
			"Each caller has %d drafts per rolling 24 hours; past it 429 QUOTA_EXCEEDED. 503 when no model is configured. "+
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.", walkthroughdraft.PerDay),
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, createMyWalkthroughDraft(cat))
}

func createMyWalkthroughDraft(cat *Catalog) func(context.Context, *walkthroughDraftInput) (*walkthroughDraftOutput, error) {
	return func(ctx context.Context, in *walkthroughDraftInput) (*walkthroughDraftOutput, error) {
		uid, _, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		if in == nil || strings.TrimSpace(in.Body.Text) == "" {
			return nil, problem.New(problem.CodeValidationFailed, "", "", "text is required.")
		}
		draft, derr := cat.WalkthroughDrafts.Draft(ctx, uid, in.Body.Text)
		switch {
		case errors.Is(derr, walkthroughdraft.ErrUnavailable):
			return nil, problem.New(problem.CodeServiceUnavailable, "", "", "walkthrough drafts are not available.")
		case errors.Is(derr, walkthroughdraft.ErrQuota):
			return nil, problem.New(problem.CodeQuotaExceeded, "", "", fmt.Sprintf("the daily limit of %d drafts is used up.", walkthroughdraft.PerDay))
		case derr != nil:
			slog.Warn("walkthrough draft failed", "uid", uid, "err", derr)
			return nil, problem.New(problem.CodeServiceUnavailable, "", "", "the model could not draft this text; try again or shorten it.")
		}
		body := walkthroughDraftBody{
			Object: "walkthrough_draft", Verdict: draft.Verdict, Coverage: draft.Metrics.Coverage,
			Missed: orEmptyStrings(draft.Metrics.Missed), NotVerbatim: orEmptyStrings(draft.Metrics.NotVerbatim),
			Remaining: max(draft.Remaining, 0),
		}
		if draft.Chart != nil {
			raw, err := json.Marshal(draft.Chart)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &body.Chart); err != nil {
				return nil, err
			}
		}
		return &walkthroughDraftOutput{Body: body}, nil
	}
}
