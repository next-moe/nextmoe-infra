package parse

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
)

const MaxPageLimit = 100

func Bool(raw, name string) (bool, *problem.Problem) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, invalid(name, "expected true or false")
	}
}

func Int(raw, name string) (int, *problem.Problem) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalid(name, "expected an integer")
	}
	return n, nil
}

func Limit(raw string, def, max int) (int, *problem.Problem) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, invalid("limit", "expected a positive integer")
	}
	if max <= 0 {
		max = MaxPageLimit
	}
	if n > max {
		p := problem.New(problem.CodeLimitTooLarge, "", "", "limit must be at most "+strconv.Itoa(max)+".")
		p.Errors = []problem.FieldError{{
			Parameter: "limit",
			Reason:    problem.ReasonOutOfRange,
			Detail:    "maximum " + strconv.Itoa(max),
			Params:    &problem.FieldParams{Maximum: problem.Ptr(float64(max))},
		}}
		return 0, p
	}
	return n, nil
}

func Enum(raw, name string, allowed []string) (string, *problem.Problem) {
	for _, a := range allowed {
		if raw == a {
			return raw, nil
		}
	}
	p := problem.New(problem.CodeUnknownEnumValue, "", "", name+" is not in the closed vocabulary.")
	p.Errors = []problem.FieldError{{
		Parameter: name,
		Reason:    problem.ReasonUnknownValue,
		Detail:    "allowed values: " + strings.Join(allowed, ", "),
		Params:    &problem.FieldParams{Allowed: problem.Ptr(append([]string(nil), allowed...))},
	}}
	return "", p
}

func Date(raw, name string) (time.Time, *problem.Problem) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, invalid(name, "expected YYYY-MM-DD")
	}
	return t, nil
}

func DateTimeUTC(raw, name string) (time.Time, *problem.Problem) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || !strings.HasSuffix(raw, "Z") {
		return time.Time{}, invalid(name, "expected RFC 3339 UTC ending in Z")
	}
	return t.UTC(), nil
}

func invalid(name, detail string) *problem.Problem {
	p := problem.New(problem.CodeInvalidParameter, "", "", fmt.Sprintf("%s is invalid.", name))
	p.Errors = []problem.FieldError{{
		Parameter: name,
		Reason:    problem.ReasonInvalidFormat,
		Detail:    detail,
	}}
	return p
}
