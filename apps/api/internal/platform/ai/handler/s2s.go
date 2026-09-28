package handler

import (
	"context"
	"log/slog"
	"net/http"

	"api/internal/platform/ai/dto"
	"api/internal/platform/ai/service"
	"api/pkg/errors"
	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type Server struct {
	moderation *service.ModerationService
}

func Setup(app *fiber.App, moderation *service.ModerationService) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN AI Gateway", "1.0.1")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(S2SBridge)

	s := &Server{moderation: moderation}
	s.register(api)
	wireshape.Publish(api.OpenAPI())
	return api
}

func (s *Server) register(api huma.API) {
	tags := []string{"ai"}
	huma.Register(api, huma.Operation{
		OperationID: "moderateText", Method: http.MethodPost, Path: "/api/v1/ai/moderate-text",
		Summary: "Scan a text for policy violations (async, fail-open; tenant derived from the client binding)",
		Tags:    tags,
	}, s.moderateText)
}

type moderateTextInput struct{ Body dto.ModerateTextRequest }
type moderateTextOutput struct {
	Body Envelope[dto.ModerateTextResponse]
}

func (s *Server) moderateText(ctx context.Context, in *moderateTextInput) (*moderateTextOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	res, err := s.moderation.Moderate(ctx, service.ModerateParams{
		Site: site, SubjectSite: in.Body.SubjectSite, Text: in.Body.Text, AuthorID: in.Body.AuthorID, SubjectKind: in.Body.SubjectKind,
	})
	if err != nil {
		slog.Error("ai moderate-text", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &moderateTextOutput{Body: okEnvelope(dto.ModerateTextResponse{
		Route:      res.Route,
		Flagged:    res.Flagged,
		Categories: res.Categories,
		Score:      res.Score,
		Channel:    res.Channel,
		Degraded:   res.Degraded,
	})}, nil
}
