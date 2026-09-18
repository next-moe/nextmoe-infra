package problem

import (
	"crypto/rand"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/validation"
	"github.com/gofiber/fiber/v3"
)

type FieldParams struct {
	MaxLength *int      `json:"max_length,omitempty" minimum:"0" doc:"Maximum string length the value exceeded."`
	MinLength *int      `json:"min_length,omitempty" minimum:"0" doc:"Minimum string length the value failed."`
	Minimum   *float64  `json:"minimum,omitempty" minimum:"-9007199254740991" maximum:"9007199254740991" doc:"Inclusive numeric lower bound the value missed."`
	Maximum   *float64  `json:"maximum,omitempty" minimum:"-9007199254740991" maximum:"9007199254740991" doc:"Inclusive numeric upper bound the value missed."`
	MaxItems  *int      `json:"max_items,omitempty" minimum:"0" doc:"Maximum array length the value exceeded."`
	MinItems  *int      `json:"min_items,omitempty" minimum:"0" doc:"Minimum array length the value failed."`
	Allowed   *[]string `json:"allowed,omitempty" maxItems:"256" maxLength:"128" pattern:"^[\\x20-\\x7E]+$" doc:"Closed vocabulary members that were expected."`
}

type FieldError struct {
	_         struct{}     `json:"-" additionalProperties:"true"`
	Pointer   string       `json:"pointer,omitempty" maxLength:"256" pattern:"^/" doc:"JSON Pointer (RFC 6901) into the request body. Exactly one of pointer, parameter, or header is set."`
	Parameter string       `json:"parameter,omitempty" maxLength:"64" pattern:"^[A-Za-z_][A-Za-z0-9_]*$" doc:"Query or path parameter name. Exactly one of pointer, parameter, or header is set."`
	Header    string       `json:"header,omitempty" maxLength:"64" pattern:"^[A-Za-z0-9-]+$" doc:"Request header name. Exactly one of pointer, parameter, or header is set."`
	Reason    string       `json:"reason" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" maxLength:"63" doc:"Field-level reason from the closed reason registry."`
	Detail    string       `json:"detail" maxLength:"2048" doc:"English, request-specific. Must not be used as a discriminant."`
	Params    *FieldParams `json:"params,omitempty" doc:"Constraint values for this reason. Omitted when the reason carries none."`
}

func Ptr[T any](v T) *T { return &v }

type Problem struct {
	_         struct{}     `json:"-" additionalProperties:"true"`
	Type      string       `json:"type" format:"uri" maxLength:"256" doc:"Stable problem type URI of the form https://developer.nextmoe.dev/problems/{domain}/{kebab-code}."`
	Title     string       `json:"title" maxLength:"128" pattern:"^[ -~]+$" doc:"Stable English phrase for this type. Does not vary per request."`
	Status    int          `json:"status" minimum:"400" maximum:"599" doc:"HTTP status. Matches the response status line."`
	Detail    string       `json:"detail" maxLength:"2048" doc:"English, request-specific. Must not be used as a discriminant. Empty when there is nothing to add."`
	Instance  string       `json:"instance" maxLength:"2048" pattern:"^(/.*)?$" doc:"Request path and query string that failed. Empty only if the path is unknown."`
	Code      string       `json:"code" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" maxLength:"63" doc:"Top-level error code from the closed registry. UPPER_SNAKE."`
	RequestID string       `json:"request_id" pattern:"^req_[0-9A-HJKMNP-TV-Z]{26}$" minLength:"30" maxLength:"30" doc:"Same value as X-Request-ID. Prefix req_ plus a 26-character ULID."`
	Errors    []FieldError `json:"errors" doc:"Field-level failures. Empty array when this is not a field-level error."`
	Object    string       `json:"object,omitempty" maxLength:"32" pattern:"^[a-z][a-z0-9_]*$" doc:"Entity family when code is ENTITY_MERGED."`
	CurrentID string       `json:"current_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Canonical id when code is ENTITY_MERGED."`
	Suspects  []Suspect    `json:"suspects,omitempty" doc:"Live works sharing a submitted title when code is DUPLICATE_SUSPECTS."`
}

type Suspect struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	ID          string   `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog work id."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"The live work's display name. Must not be used as a discriminant."`
}

func (p *Problem) Error() string {
	if p == nil {
		return ""
	}
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

func (p *Problem) GetStatus() int {
	if p == nil {
		return http.StatusInternalServerError
	}
	return p.Status
}

func (p *Problem) GetHeaders() http.Header {
	if p == nil || p.Code != CodeEntityMerged || p.CurrentID == "" {
		return nil
	}
	seg := objectPath[p.Object]
	if seg == "" {
		return nil
	}
	h := make(http.Header)
	h.Set("Link", "</v2/catalog/"+seg+"/"+p.CurrentID+">; rel=\"canonical\"")
	return h
}

var objectPath = map[string]string{
	"work": "works", "release": "releases", "character": "characters",
	"credit_name": "credit-names", "person": "persons", "company": "companies",
	"tag": "tags", "trait": "traits", "series": "series", "engine": "engines",
}

func Merged(object, currentID, requestID, instance, detail string) *Problem {
	p := New(CodeEntityMerged, requestID, instance, detail)
	p.Object = object
	p.CurrentID = currentID
	return p
}

func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" || ct == "" {
		return "application/problem+json"
	}
	if ct == "application/cbor" {
		return "application/problem+cbor"
	}
	return ct
}

func New(code, requestID, instance, detail string) *Problem {
	def, ok := Lookup(code)
	if !ok {
		def, _ = Lookup(CodeInternalError)
	}
	return &Problem{
		Type:      def.TypeURI(),
		Title:     def.Title,
		Status:    def.Status,
		Detail:    detail,
		Instance:  instance,
		Code:      def.Code,
		RequestID: requestID,
		Errors:    []FieldError{},
	}
}

func OfStatus(status int, requestID, instance, detail string) *Problem {
	return New(StatusToCode(status), requestID, instance, detail)
}

func FromHuma(ctx huma.Context, status int, msg string, errs ...error) *Problem {
	requestID, instance := "", ""
	if ctx != nil {
		u := ctx.URL()
		instance = u.RequestURI()
		if v := ctx.Header("X-Request-ID"); strings.HasPrefix(v, "req_") {
			requestID = v
		}
	}
	if requestID == "" {
		requestID = "req_" + newULID()
	}
	code := StatusToCode(status)
	if status == http.StatusUnauthorized {
		if ctx == nil || ctx.Header("Authorization") == "" {
			code = CodeMissingCredential
		} else {
			code = CodeInvalidCredential
		}
	}
	p := New(code, requestID, instance, msg)
	for _, err := range errs {
		if err == nil {
			continue
		}
		p.Errors = append(p.Errors, fieldFromHuma(err))
	}
	if len(p.Errors) > 0 && p.Code != CodeValidationFailed && status == http.StatusUnprocessableEntity {
		p = New(CodeValidationFailed, requestID, instance, msg)
		for _, err := range errs {
			if err == nil {
				continue
			}
			p.Errors = append(p.Errors, fieldFromHuma(err))
		}
	}
	return p
}

func WriteFiberError(c fiber.Ctx, err error) error {
	var p *Problem
	if errors.As(err, &p) && p != nil {
		if p.RequestID == "" {
			p.RequestID = RequestID(c)
		}
		if p.Instance == "" {
			p.Instance = Instance(c)
		}
	} else {
		status := http.StatusInternalServerError
		msg := "Internal Server Error"
		var fe *fiber.Error
		if errors.As(err, &fe) && fe != nil {
			status = fe.Code
			msg = fe.Message
		} else if err != nil {
			msg = err.Error()
		}
		p = OfStatus(status, RequestID(c), Instance(c), msg)
	}
	c.Set("X-Request-ID", p.RequestID)
	c.Set("Cache-Control", "no-store")
	if h := p.GetHeaders(); h != nil {
		for k, vs := range h {
			if len(vs) > 0 {
				c.Set(k, vs[0])
			}
		}
	}
	if p.Status == http.StatusUnauthorized {
		c.Set("WWW-Authenticate", `Bearer realm="nextmoe", error="invalid_token"`)
	}
	if err := c.Status(p.Status).JSON(p); err != nil {
		return err
	}
	c.Set("Content-Type", "application/problem+json")
	return nil
}

func Instance(c fiber.Ctx) string {
	path := c.Path()
	if q := c.Request().URI().QueryArgs(); q != nil && q.Len() > 0 {
		return path + "?" + string(q.QueryString())
	}
	return path
}

func RequestID(c fiber.Ctx) string {
	if v, ok := c.Locals("request_id").(string); ok && strings.HasPrefix(v, "req_") {
		return v
	}
	if v := c.Get("X-Request-ID"); strings.HasPrefix(v, "req_") {
		c.Locals("request_id", v)
		return v
	}
	id := "req_" + newULID()
	c.Locals("request_id", id)
	c.Set("X-Request-ID", id)
	return id
}

func RequestIDMiddleware(c fiber.Ctx) error {
	_ = RequestID(c)
	return c.Next()
}

var bodyLoc = regexp.MustCompile(`\[(\d+)]`)

func fieldFromHuma(err error) FieldError {
	var d *huma.ErrorDetail
	if !errors.As(err, &d) || d == nil {
		return FieldError{Reason: ReasonInvalidFormat, Detail: err.Error()}
	}
	reason, params, requiredProp := mapReason(d.Message)
	fe := FieldError{Detail: d.Message, Reason: reason, Params: params}
	loc := d.Location
	switch {
	case strings.HasPrefix(loc, "query."):
		fe.Parameter = strings.TrimPrefix(loc, "query.")
	case strings.HasPrefix(loc, "path."):
		fe.Parameter = strings.TrimPrefix(loc, "path.")
	case strings.HasPrefix(loc, "header."):
		fe.Header = strings.TrimPrefix(loc, "header.")
	case loc == "body" || strings.HasPrefix(loc, "body."):
		parent := ""
		if strings.HasPrefix(loc, "body.") {
			parent = bodyPointer(strings.TrimPrefix(loc, "body."))
		}
		if requiredProp != "" {
			fe.Pointer = joinPointer(parent, requiredProp)
		} else {
			fe.Pointer = parent
		}
	default:
		if loc != "" {
			fe.Parameter = loc
		}
	}
	return fe
}

func mapReason(msg string) (reason string, params *FieldParams, requiredProp string) {
	if name, ok := parseRequiredProperty(msg); ok {
		return ReasonRequired, nil, name
	}
	if strings.HasPrefix(msg, "required ") && strings.HasSuffix(msg, " parameter is missing") {
		return ReasonRequired, nil, ""
	}
	if n, ok := parseAfter(msg, "expected array length <= "); ok {
		return ReasonTooManyItems, &FieldParams{MaxItems: Ptr(n)}, ""
	}
	if n, ok := parseAfter(msg, "expected array length >= "); ok {
		return ReasonTooFewItems, &FieldParams{MinItems: Ptr(n)}, ""
	}
	if n, ok := parseAfter(msg, "expected length <= "); ok {
		return ReasonTooLong, &FieldParams{MaxLength: Ptr(n)}, ""
	}
	if n, ok := parseAfter(msg, "expected length >= "); ok {
		return ReasonTooShort, &FieldParams{MinLength: Ptr(n)}, ""
	}
	if f, ok := parseFloatAfter(msg, "expected number >= "); ok {
		return ReasonOutOfRange, &FieldParams{Minimum: Ptr(f)}, ""
	}
	if f, ok := parseFloatAfter(msg, "expected number > "); ok {
		return ReasonOutOfRange, &FieldParams{Minimum: Ptr(f)}, ""
	}
	if f, ok := parseFloatAfter(msg, "expected number <= "); ok {
		return ReasonOutOfRange, &FieldParams{Maximum: Ptr(f)}, ""
	}
	if f, ok := parseFloatAfter(msg, "expected number < "); ok {
		return ReasonOutOfRange, &FieldParams{Maximum: Ptr(f)}, ""
	}
	if msg == validation.MsgExpectedArrayItemsUnique {
		return ReasonDuplicateItem, nil, ""
	}
	if allowed, ok := parseOneOf(msg); ok {
		return ReasonUnknownValue, &FieldParams{Allowed: Ptr(allowed)}, ""
	}
	if v, ok := strings.CutPrefix(msg, "expected value to be "); ok && v != "" && !strings.HasPrefix(v, "one of ") {
		return ReasonUnknownValue, &FieldParams{Allowed: Ptr([]string{v})}, ""
	}
	return ReasonInvalidFormat, nil, ""
}

func parseRequiredProperty(msg string) (string, bool) {
	const pre, suf = "expected required property ", " to be present"
	if !strings.HasPrefix(msg, pre) || !strings.HasSuffix(msg, suf) {
		return "", false
	}
	name := msg[len(pre) : len(msg)-len(suf)]
	if name == "" {
		return "", false
	}
	return name, true
}

func parseAfter(msg, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseFloatAfter(msg, prefix string) (float64, bool) {
	rest, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func parseOneOf(msg string) ([]string, bool) {
	const pre, suf = `expected value to be one of "`, `"`
	if !strings.HasPrefix(msg, pre) || !strings.HasSuffix(msg, suf) {
		return nil, false
	}
	inner := msg[len(pre) : len(msg)-len(suf)]
	if inner == "" {
		return []string{}, true
	}
	return strings.Split(inner, ", "), true
}

func joinPointer(parent, name string) string {
	token := escapePointerToken(name)
	if parent == "" || parent == "/" {
		return "/" + token
	}
	return parent + "/" + token
}

func escapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

func bodyPointer(loc string) string {
	loc = bodyLoc.ReplaceAllString(loc, "/$1")
	loc = strings.ReplaceAll(loc, ".", "/")
	if loc == "" {
		return ""
	}
	if !strings.HasPrefix(loc, "/") {
		loc = "/" + loc
	}
	return loc
}

func newULID() string {
	var raw [16]byte
	ms := uint64(time.Now().UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	_, _ = rand.Read(raw[6:])
	return encodeULID(raw)
}

func encodeULID(id [16]byte) string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	dst := [26]byte{
		alphabet[(id[0]&224)>>5],
		alphabet[id[0]&31],
		alphabet[(id[1]&248)>>3],
		alphabet[((id[1]&7)<<2)|((id[2]&192)>>6)],
		alphabet[(id[2]&62)>>1],
		alphabet[((id[2]&1)<<4)|((id[3]&240)>>4)],
		alphabet[((id[3]&15)<<1)|((id[4]&128)>>7)],
		alphabet[(id[4]&124)>>2],
		alphabet[((id[4]&3)<<3)|((id[5]&224)>>5)],
		alphabet[id[5]&31],
		alphabet[(id[6]&248)>>3],
		alphabet[((id[6]&7)<<2)|((id[7]&192)>>6)],
		alphabet[(id[7]&62)>>1],
		alphabet[((id[7]&1)<<4)|((id[8]&240)>>4)],
		alphabet[((id[8]&15)<<1)|((id[9]&128)>>7)],
		alphabet[(id[9]&124)>>2],
		alphabet[((id[9]&3)<<3)|((id[10]&224)>>5)],
		alphabet[id[10]&31],
		alphabet[(id[11]&248)>>3],
		alphabet[((id[11]&7)<<2)|((id[12]&192)>>6)],
		alphabet[(id[12]&62)>>1],
		alphabet[((id[12]&1)<<4)|((id[13]&240)>>4)],
		alphabet[((id[13]&15)<<1)|((id[14]&128)>>7)],
		alphabet[(id[14]&124)>>2],
		alphabet[((id[14]&3)<<3)|((id[15]&224)>>5)],
		alphabet[id[15]&31],
	}
	return string(dst[:])
}
