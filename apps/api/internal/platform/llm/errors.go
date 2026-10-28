package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
)

type ErrorKind string

const (
	ErrKindAuth            ErrorKind = "auth"
	ErrKindQuota           ErrorKind = "quota"
	ErrKindNotFound        ErrorKind = "not_found"
	ErrKindBadRequest      ErrorKind = "bad_request"
	ErrKindContextLength   ErrorKind = "context_length"
	ErrKindContentFiltered ErrorKind = "content_filtered"
	ErrKindRateLimit       ErrorKind = "rate_limit"
	ErrKindUpstream        ErrorKind = "upstream"
	ErrKindNetwork         ErrorKind = "network"
	ErrKindTimeout         ErrorKind = "timeout"
	ErrKindMalformed       ErrorKind = "malformed"
	ErrKindCanceled        ErrorKind = "canceled"
	ErrKindNoRoute         ErrorKind = "no_route"
)

type Error struct {
	Kind   ErrorKind
	Status int
	Msg    string
}

func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("llm %s (http %d): %s", e.Kind, e.Status, e.Msg)
	}
	return fmt.Sprintf("llm %s: %s", e.Kind, e.Msg)
}

func KindOf(err error) ErrorKind {
	var le *Error
	if errors.As(err, &le) {
		return le.Kind
	}
	return ""
}

func (k ErrorKind) transient() bool {
	switch k {
	case ErrKindRateLimit, ErrKindUpstream, ErrKindNetwork, ErrKindTimeout, ErrKindMalformed:
		return true
	}
	return false
}

func (k ErrorKind) failsOver() bool {
	return k != ErrKindContentFiltered && k != ErrKindCanceled && k != ErrKindBadRequest
}

var (
	reContext  = regexp.MustCompile(`(?i)context[ _-]?(length|window)|too many tokens|maximum context|上下文.*(过长|超出)`)
	reFiltered = regexp.MustCompile(`(?i)usage guidelines|content[ _-]?(filter|policy)|request blocked|敏感|违规`)
	reCapacity = regexp.MustCompile(`(?i)at capacity|overloaded|temporarily unavailable|unknown internal error|负载|繁忙`)
	reQuota    = regexp.MustCompile(`(?i)insufficient[ _-]?(balance|quota)|quota exceeded|billing|余额不足|额度`)
)

func classifyHTTP(status int, body string) *Error {
	e := &Error{Status: status, Msg: truncate(body, 300)}
	switch {
	case reFiltered.MatchString(body):
		e.Kind = ErrKindContentFiltered
	case reContext.MatchString(body):
		e.Kind = ErrKindContextLength
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.Kind = ErrKindAuth
	case status == http.StatusPaymentRequired || reQuota.MatchString(body):
		e.Kind = ErrKindQuota
	case status == http.StatusNotFound:
		e.Kind = ErrKindNotFound
	case status == http.StatusTooManyRequests:
		e.Kind = ErrKindRateLimit
	case status >= 500 || reCapacity.MatchString(body):
		e.Kind = ErrKindUpstream
	default:
		e.Kind = ErrKindBadRequest
	}
	return e
}

func classifyTransport(ctx context.Context, err error) *Error {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return &Error{Kind: ErrKindCanceled, Msg: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Kind: ErrKindTimeout, Msg: err.Error()}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &Error{Kind: ErrKindTimeout, Msg: err.Error()}
	}
	return &Error{Kind: ErrKindNetwork, Msg: err.Error()}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
