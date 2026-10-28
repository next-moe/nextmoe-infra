package handler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/agent/stream"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/oidctoken"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

const heartbeat = 15 * time.Second

func Setup(app *fiber.App, s *Server, verifier *oidctoken.Verifier, clients *siteRepo.OAuthClientRepository, sites *SiteResolver) huma.API {
	anyAuth := authenticate(verifier, clients, sites, false)
	userAuth := authenticate(verifier, clients, sites, true)
	for _, prefix := range []string{"/v2/agent/persona", "/v2/agent/search", "/v2/agent/pow"} {
		app.Use(prefix, anyAuth)
	}
	app.Use("/v2/agent", func(c fiber.Ctx) error {
		if callerOf(c) != nil {
			return c.Next()
		}
		return userAuth(c)
	})
	app.Get("/v2/agent/runs/:id/stream", s.stream)

	cfg := huma.DefaultConfig("NextMoe Agent API", "0.1.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	cfg.Info.Description = "The NextMoe AI assistant: conversations, streamed replies and search. Called by a site's backend with the user's access token (or the site's client credentials for signed-out search). Replies stream as the AI SDK UI message stream protocol (SSE) from GET /v2/agent/runs/{id}/stream, which honours Last-Event-ID and answers 204 when the run has no live stream."
	api := humafiber.New(app, cfg)
	s.Register(api)
	return api
}

func (s *Server) stream(c fiber.Ctx) error {
	who := callerOf(c)
	if who == nil || who.UserID == 0 {
		return problem(c, fiber.StatusUnauthorized, "a user access token is required")
	}
	runID := strings.Clone(c.Params("id"))
	if _, err := s.Store.Run(c.Context(), who.UserID, runID); err != nil {
		return problem(c, fiber.StatusNotFound, "no such run")
	}
	after := 0
	if v := c.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.Atoi(v)
	} else if v := c.Query("after"); v != "" {
		after, _ = strconv.Atoi(v)
	}
	if !s.Broker.Known(runID) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache, no-transform")
	c.Set("X-Accel-Buffering", "no")
	c.Set("x-vercel-ai-ui-message-stream", "v1")
	broker := s.Broker
	return c.SendStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		frames := make(chan stream.Frame, 64)
		done := make(chan error, 1)
		go func() {
			done <- broker.Subscribe(ctx, runID, after, func(f stream.Frame) error {
				select {
				case frames <- f:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		tick := time.NewTicker(heartbeat)
		defer tick.Stop()
		write := func(s string) bool {
			if _, err := w.WriteString(s); err != nil {
				return false
			}
			return w.Flush() == nil
		}
		for {
			select {
			case f := <-frames:
				if !write(fmt.Sprintf("id: %d\ndata: %s\n\n", f.Seq, f.Data)) {
					return
				}
			case <-tick.C:
				if !write(": ping\n\n") {
					return
				}
			case err := <-done:
				for {
					select {
					case f := <-frames:
						if !write(fmt.Sprintf("id: %d\ndata: %s\n\n", f.Seq, f.Data)) {
							return
						}
						continue
					default:
					}
					break
				}
				if err == nil || errors.Is(err, stream.ErrUnknownRun) {
					write("data: [DONE]\n\n")
				}
				return
			}
		}
	})
}
