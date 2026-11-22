package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Problem keeps v2's problem+json shape (D20): a closed code beside the
// RFC 9457 fields, and object/current_id for a merged id. Downstream clients
// generated from v2 read code, never title or detail.
type Problem struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Status    int      `json:"status"`
	Detail    string   `json:"detail"`
	Instance  string   `json:"instance"`
	Code      string   `json:"code"`
	Errors    []string `json:"errors"`
	Object    string   `json:"object,omitempty"`
	CurrentID string   `json:"current_id,omitempty"`
	// Current is the server's state when a client write lost to a newer one.
	Current any `json:"current,omitempty"`
}

func (p *Problem) Error() string  { return p.Detail }
func (p *Problem) GetStatus() int { return p.Status }

func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" || ct == "" {
		return "application/problem+json"
	}
	return ct
}

var codeByStatus = map[int]string{
	http.StatusBadRequest:          "INVALID_PARAMETER",
	http.StatusUnauthorized:        "INVALID_CREDENTIAL",
	http.StatusForbidden:           "FORBIDDEN",
	http.StatusNotFound:            "NOT_FOUND",
	http.StatusConflict:            "INVALID_STATE_TRANSITION",
	http.StatusGone:                "GONE",
	http.StatusUnprocessableEntity: "VALIDATION_FAILED",
}

func problem(status int, code, detail string) *Problem {
	if code == "" {
		code = codeByStatus[status]
		if code == "" {
			code = "INTERNAL_ERROR"
		}
	}
	return &Problem{
		Type:   "https://developer.nextmoe.dev/problems/catalog/" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		Title:  http.StatusText(status),
		Status: status, Detail: detail, Code: code, Errors: []string{},
	}
}

func merged(object string, currentID int64) *Problem {
	p := problem(http.StatusNotFound, "ENTITY_MERGED", object+" was merged into "+id(currentID))
	p.Object, p.CurrentID = object, id(currentID)
	return p
}

func init() {
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		p := problem(status, "", msg)
		if ctx != nil {
			u := ctx.URL()
			p.Instance = u.RequestURI()
		}
		for _, e := range errs {
			if e != nil {
				p.Errors = append(p.Errors, e.Error())
			}
		}
		return p
	}
}
