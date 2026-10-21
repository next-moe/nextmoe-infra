package handler

import (
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/editorigin"
)

func originSiteParam(raw string) (string, error) {
	if raw == "" || editorigin.Valid(raw) {
		return raw, nil
	}
	allowed := editorigin.Sites()
	p := problem.New(problem.CodeUnknownEnumValue, "", "", "origin_site= is not a first-party property.")
	p.Errors = []problem.FieldError{{Parameter: "origin_site", Reason: problem.ReasonUnknownValue,
		Detail: "allowed values: " + strings.Join(allowed, ", "),
		Params: &problem.FieldParams{Allowed: &allowed}}}
	return "", p
}
