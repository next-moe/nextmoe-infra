package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/realtime"
	"api/internal/platform/chat/service"
	"api/internal/platform/devapi"
	"api/pkg/routepath"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

const (
	ScopeRead  = "chat:read"
	ScopeWrite = "chat:write"
	SpecPath   = "/v2/chat/openapi.json"
)

type Identity struct {
	UID      int64
	ClientID string
	Scopes   []string
}

type ClientInfo struct {
	Site  string
	Hosts []string
}

type Options struct {
	Chat        *service.Service
	Tokens      *realtime.TokenIssuer
	RealtimeURL string
	Identify    func(ctx context.Context, rawToken string) (Identity, error)
	Client      func(ctx context.Context, clientID string) (ClientInfo, error)
	// Access tokens outlive account deletion by up to their lifetime; a write
	// in that window would land after the hourly purge and never be erased.
	AccountActive func(ctx context.Context, uid int64) (bool, error)
}

type ctxKey string

const (
	ctxActor     ctxKey = "chat_actor"
	ctxRequestID ctxKey = "chat_request_id"
	ctxInstance  ctxKey = "chat_instance"
	localsActor         = "chat_actor"
)

var installOnce sync.Once

func Setup(app *fiber.App, opt Options) huma.API {
	installOnce.Do(func() {
		prev := huma.NewErrorWithContext
		huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
			if ctx != nil && strings.HasPrefix(ctx.URL().Path, "/v2") {
				return problem.FromHuma(ctx, status, msg, errs...)
			}
			return prev(ctx, status, msg, errs...)
		}
	})
	app.Use("/v2/chat", problem.RequestIDMiddleware, authenticate(opt))

	cfg := huma.DefaultConfig("NextMoe Chat API", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Info.Description = "Direct messages and groups shared by every NextMoe site and app. First-party clients only: " +
		"a user access token with chat:read (reads) or chat:write (everything; also grants reads)."
	cfg.Servers = []*huma.Server{{URL: "https://api.nextmoe.dev", Description: "Production. Paths already include /v2/chat."}}
	api := humafiber.New(app, cfg)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		fc := humafiber.Unwrap(ctx)
		ctx = huma.WithValue(ctx, ctxRequestID, problem.RequestID(fc))
		ctx = huma.WithValue(ctx, ctxInstance, problem.Instance(fc))
		if a, ok := fc.Locals(localsActor).(service.Actor); ok {
			ctx = huma.WithValue(ctx, ctxActor, a)
		}
		next(ctx)
	})
	h := &Handler{opt: opt}
	h.register(api)
	return api
}

type Handler struct{ opt Options }

func actorFrom(ctx context.Context) (service.Actor, error) {
	a, ok := ctx.Value(ctxActor).(service.Actor)
	if !ok || a.UserID <= 0 {
		return service.Actor{}, stamp(ctx, problem.New(problem.CodeUserIdentityRequired, "", "", "this operation requires a user access token."))
	}
	return a, nil
}

func stamp(ctx context.Context, p *problem.Problem) *problem.Problem {
	if p.RequestID == "" {
		p.RequestID, _ = ctx.Value(ctxRequestID).(string)
	}
	if p.Instance == "" {
		p.Instance, _ = ctx.Value(ctxInstance).(string)
	}
	return p
}

func authenticate(opt Options) fiber.Handler {
	return func(c fiber.Ctx) error {
		path := routepath.Normalize(c.Path())
		if path == SpecPath {
			return c.Next()
		}
		fail := func(code, detail string) error {
			return problem.WriteFiberError(c, problem.New(code, problem.RequestID(c), problem.Instance(c), detail))
		}
		h := c.Get("Authorization")
		if h == "" {
			return fail(problem.CodeMissingCredential, "Authorization Bearer token is required.")
		}
		token, ok := strings.CutPrefix(h, "Bearer ")
		token = strings.TrimSpace(token)
		if !ok || token == "" {
			return fail(problem.CodeInvalidCredential, "Authorization Bearer token is invalid.")
		}
		if devapi.HasKeyPrefix(token) {
			return fail(problem.CodeInvalidCredential, "chat requires a user access token, not an application key.")
		}
		if opt.Identify == nil {
			return fail(problem.CodeServiceUnavailable, "chat authentication is not configured.")
		}
		ident, err := opt.Identify(c.Context(), token)
		if err != nil {
			return fail(problem.CodeInvalidCredential, "Authorization Bearer token is invalid.")
		}
		if ident.UID <= 0 {
			return fail(problem.CodeUserIdentityRequired, "chat requires a user access token.")
		}
		write := slices.Contains(ident.Scopes, ScopeWrite)
		read := write || slices.Contains(ident.Scopes, ScopeRead)
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead:
			if !read {
				return fail(problem.CodeScopeRequired, "this operation requires the chat:read scope.")
			}
		default:
			if !write {
				return fail(problem.CodeScopeRequired, "this operation requires the chat:write scope.")
			}
		}
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead && opt.AccountActive != nil {
			active, err := opt.AccountActive(c.Context(), ident.UID)
			if err != nil {
				return fail(problem.CodeServiceUnavailable, "the account could not be checked.")
			}
			if !active {
				return fail(problem.CodeInvalidCredential, "the account behind this token has been deleted.")
			}
		}
		actor := service.Actor{UserID: ident.UID, Site: ident.ClientID}
		if opt.Client != nil && ident.ClientID != "" {
			info, err := opt.Client(c.Context(), ident.ClientID)
			if err != nil {
				return fail(problem.CodeServiceUnavailable, "the token's client could not be looked up.")
			}
			if info.Site != "" {
				actor.Site = info.Site
			}
			actor.Hosts = info.Hosts
		}
		c.Locals(localsActor, actor)
		return c.Next()
	}
}

type retryProblem struct {
	*problem.Problem
	seconds int
}

func (r *retryProblem) GetHeaders() http.Header {
	h := http.Header{}
	h.Set("Retry-After", strconv.Itoa(r.seconds))
	return h
}

func fail(ctx context.Context, op string, err error) error {
	var (
		invalid   *service.InvalidError
		notAccept *service.NotAcceptingError
		limited   *service.RateLimitError
		p         *problem.Problem
	)
	switch {
	case errors.As(err, &p):
		return stamp(ctx, p)
	case errors.Is(err, service.ErrNotFound):
		return stamp(ctx, problem.New(problem.CodeNotFound, "", "", "no such conversation or message, or it is not yours to see."))
	case errors.Is(err, service.ErrBlocked):
		return stamp(ctx, problem.New(problem.CodeChatBlocked, "", "", ""))
	case errors.As(err, &notAccept):
		return stamp(ctx, problem.New(problem.CodeChatNotAccepting, "", "", notAccept.Reason))
	case errors.Is(err, service.ErrRequestLimit):
		return stamp(ctx, problem.New(problem.CodeChatRequestLimit, "", "", ""))
	case errors.Is(err, service.ErrEditWindow):
		return stamp(ctx, problem.New(problem.CodeChatEditWindowClosed, "", "", ""))
	case errors.Is(err, service.ErrNotPermitted):
		return stamp(ctx, problem.New(problem.CodeChatNotPermitted, "", "", ""))
	case errors.Is(err, service.ErrImagesDisabled):
		return stamp(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "image messages are not available."))
	case errors.Is(err, service.ErrImageQuota):
		return stamp(ctx, problem.New(problem.CodeQuotaExceeded, "", "", "the chat image quota is exhausted."))
	case errors.Is(err, service.ErrImageRejected):
		pr := stamp(ctx, problem.New(problem.CodeValidationFailed, "", "", "these bytes were rejected: not an accepted image, or refused by moderation."))
		pr.Errors = []problem.FieldError{{Pointer: "/file", Reason: problem.ReasonNotAllowedValue, Detail: "rejected by the image service"}}
		return pr
	case errors.As(err, &invalid):
		pr := stamp(ctx, problem.New(problem.CodeValidationFailed, "", "", invalid.Field+": "+invalid.Reason))
		pr.Errors = []problem.FieldError{{Pointer: "/" + strings.ReplaceAll(strings.ReplaceAll(invalid.Field, "[", "/"), "]", ""),
			Reason: problem.ReasonNotAllowedValue, Detail: invalid.Reason}}
		return pr
	case errors.As(err, &limited):
		secs := int(limited.RetryAfter.Seconds()) + 1
		return &retryProblem{Problem: stamp(ctx, problem.New(problem.CodeRateLimited, "", "", "slow down.")), seconds: secs}
	default:
		slog.Error("chat "+op, "err", err)
		return stamp(ctx, problem.New(problem.CodeInternalError, "", "", ""))
	}
}
