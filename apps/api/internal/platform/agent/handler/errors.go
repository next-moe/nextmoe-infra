package handler

import (
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

const (
	CodeInvalidRequest        = "INVALID_REQUEST"
	CodeNotFound              = "NOT_FOUND"
	CodeLoginRequired         = "LOGIN_REQUIRED"
	CodeInvalidToken          = "INVALID_TOKEN"
	CodeInvalidClient         = "INVALID_CLIENT"
	CodeClientNotBound        = "CLIENT_NOT_BOUND"
	CodeAssistantDisabled     = "ASSISTANT_DISABLED"
	CodeRateLimited           = "RATE_LIMITED"
	CodeAnonDailyLimit        = "ANON_DAILY_LIMIT"
	CodeAnonGlobalLimit       = "ANON_GLOBAL_LIMIT"
	CodeFreeUnitsExhausted    = "FREE_UNITS_EXHAUSTED"
	CodeInsufficientPoints    = "INSUFFICIENT_MOEMOEPOINTS"
	CodeRunActive             = "RUN_ACTIVE"
	CodeIdempotencyKeyReused  = "IDEMPOTENCY_KEY_REUSED"
	CodeBadParent             = "BAD_PARENT"
	CodeEmptyMessage          = "EMPTY_MESSAGE"
	CodeUnsupportedAttachment = "UNSUPPORTED_ATTACHMENT"
	CodeInputTooLong          = "INPUT_TOO_LONG"
	CodePowRequired           = "POW_REQUIRED"
	CodePowRejected           = "POW_REJECTED"
	CodePowUnavailable        = "POW_UNAVAILABLE"
	CodeCatalogUnavailable    = "CATALOG_UNAVAILABLE"
	CodeUpstreamUnavailable   = "UPSTREAM_UNAVAILABLE"
	CodeServiceUnavailable    = "SERVICE_UNAVAILABLE"
	CodeInternal              = "INTERNAL"
)

type Problem struct {
	huma.ErrorModel
	Code string `json:"code" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" doc:"Machine-readable reason; branch on this, not on detail."`
}

func defaultCode(status int) string {
	switch {
	case status == http.StatusNotFound:
		return CodeNotFound
	case status == http.StatusTooManyRequests:
		return CodeRateLimited
	case status == http.StatusBadGateway:
		return CodeUpstreamUnavailable
	case status == http.StatusServiceUnavailable:
		return CodeServiceUnavailable
	case status >= 500:
		return CodeInternal
	default:
		return CodeInvalidRequest
	}
}

var installErrors sync.Once

func useProblems() {
	installErrors.Do(func() {
		prev := huma.NewError
		huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
			base := prev(status, msg, errs...)
			em, ok := base.(*huma.ErrorModel)
			if !ok {
				return base
			}
			return &Problem{ErrorModel: *em, Code: defaultCode(status)}
		}
	})
}

func fail(status int, code, detail string) error {
	err := huma.NewError(status, detail)
	if p, ok := err.(*Problem); ok {
		p.Code = code
	}
	return err
}

func problem(c fiber.Ctx, status int, code, detail string) error {
	c.Set("Content-Type", "application/problem+json")
	return c.Status(status).JSON(Problem{ErrorModel: huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail}, Code: code})
}
