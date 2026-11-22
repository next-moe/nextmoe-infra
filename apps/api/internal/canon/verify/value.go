// Package verify checks a property value twice: its shape on its own, and
// whether cited evidence entails it (plan 05 §6). Both the commit service and
// fanin call it; ingest runs the same rules from the contract as a pre-check.
package verify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"api/internal/canon/registry"

	"golang.org/x/text/language"
)

type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func refuse(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

var (
	partialDateRe = regexp.MustCompile(`^([0-9]{4}(-[0-9]{2}(-[0-9]{2})?)?|unknown)$`)
	currencyRe    = regexp.MustCompile(`^[A-Z]{3}$`)
	moneyRe       = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)
	refRe         = regexp.MustCompile(`^ref:[A-Za-z0-9_.-]{1,64}$`)
)

// Value decodes raw for p and returns the normalized Go value a column takes.
func Value(p registry.Property, raw json.RawMessage) (any, *Error) {
	switch p.Shape {
	case registry.ShapeText, registry.ShapeTitle, registry.ShapePersonName:
		s, err := str(raw)
		if err != nil || strings.TrimSpace(s) == "" {
			return nil, refuse("VALUE_NOT_ENTAILED", "want a non-empty string")
		}
		return strings.TrimSpace(s), nil
	case registry.ShapePartialDate:
		s, err := str(raw)
		if err != nil || !partialDateRe.MatchString(s) {
			return nil, refuse("VALUE_NOT_ENTAILED", "want YYYY, YYYY-MM, YYYY-MM-DD or unknown")
		}
		return s, nil
	case registry.ShapeISBN13, registry.ShapeJAN:
		s, err := str(raw)
		d := digits(s)
		if err != nil || len(d) != 13 || !ean13(d) {
			return nil, refuse("VALUE_NOT_ENTAILED", "want 13 digits with a valid check digit")
		}
		return d, nil
	case registry.ShapeMoney:
		s := strings.Trim(string(raw), `"`)
		if !moneyRe.MatchString(s) {
			return nil, refuse("VALUE_NOT_ENTAILED", "want a non-negative amount with at most two decimals")
		}
		return s, nil
	case registry.ShapeInteger:
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 0 {
			return nil, refuse("VALUE_NOT_ENTAILED", "want a non-negative integer")
		}
		return n, nil
	case registry.ShapeOrdinal:
		var f float64
		if json.Unmarshal(raw, &f) != nil || f < 0 {
			return nil, refuse("VALUE_NOT_ENTAILED", "want a non-negative number")
		}
		return f, nil
	case registry.ShapeVocab:
		s, err := str(raw)
		v, ok := registry.VocabularyByName(p.Vocab)
		if err != nil || !ok || term(v, s) == nil {
			return nil, refuse("VOCAB_TERM_UNKNOWN", "%q is not a term of %s", s, p.Vocab)
		}
		return s, nil
	case registry.ShapeLang:
		s, err := str(raw)
		if err != nil {
			return nil, refuse("VALUE_NOT_ENTAILED", "want a BCP-47 tag")
		}
		tag, perr := language.Parse(s)
		if perr != nil {
			return nil, refuse("VALUE_NOT_ENTAILED", "%q is not a BCP-47 tag", s)
		}
		return tag.String(), nil
	case registry.ShapeURL:
		s, err := str(raw)
		u, perr := url.Parse(s)
		if err != nil || perr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, refuse("VALUE_NOT_ENTAILED", "want an absolute http(s) URL")
		}
		return u.String(), nil
	case registry.ShapeCurrency:
		s, err := str(raw)
		if err != nil || !currencyRe.MatchString(s) {
			return nil, refuse("VALUE_NOT_ENTAILED", "want an ISO 4217 code")
		}
		return s, nil
	case registry.ShapeBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, refuse("VALUE_NOT_ENTAILED", "want true or false")
		}
		return b, nil
	case registry.ShapeRef:
		var id int64
		if json.Unmarshal(raw, &id) == nil && id > 0 {
			return id, nil
		}
		if s, err := str(raw); err == nil && refRe.MatchString(s) {
			return s, nil
		}
		return nil, refuse("VALUE_NOT_ENTAILED", "want a canon id or ref:<name>")
	}
	return nil, refuse("PROPERTY_UNKNOWN", "shape %q has no validator", p.Shape)
}

func str(raw json.RawMessage) (string, error) {
	var s string
	err := json.Unmarshal(raw, &s)
	return s, err
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r >= '０' && r <= '９' {
			b.WriteRune('0' + (r - '０'))
		}
	}
	return b.String()
}

func ean13(d string) bool {
	sum := 0
	for i := 0; i < 12; i++ {
		n, _ := strconv.Atoi(d[i : i+1])
		if i%2 == 1 {
			n *= 3
		}
		sum += n
	}
	check, _ := strconv.Atoi(d[12:])
	return (10-sum%10)%10 == check
}

func term(v registry.Vocabulary, key string) *registry.Term {
	for i := range v.Terms {
		if v.Terms[i].Key == key {
			return &v.Terms[i]
		}
	}
	return nil
}
