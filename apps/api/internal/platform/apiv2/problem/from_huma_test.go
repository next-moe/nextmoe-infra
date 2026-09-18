package problem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/danielgtaylor/huma/v2/validation"
	"github.com/gofiber/fiber/v3"
)

func TestFromHumaMapsHumaMessages(t *testing.T) {
	cases := []struct {
		msg      string
		loc      string
		reason   string
		pointer  string
		param    string
		maxLen   *int
		minLen   *int
		maxItems *int
		minItems *int
		minimum  *float64
		maximum  *float64
		allowed  []string
	}{
		{fmt.Sprintf(validation.MsgExpectedMaxLength, 8), "body.title", ReasonTooLong, "/title", "", Ptr(8), nil, nil, nil, nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedMinLength, 2), "body.title", ReasonTooShort, "/title", "", nil, Ptr(2), nil, nil, nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedMaxItems, 4), "body.tags", ReasonTooManyItems, "/tags", "", nil, nil, Ptr(4), nil, nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedMinItems, 1), "body.tags", ReasonTooFewItems, "/tags", "", nil, nil, nil, Ptr(1), nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedMinimumNumber, 3), "body.n", ReasonOutOfRange, "/n", "", nil, nil, nil, nil, Ptr(3.0), nil, nil},
		{fmt.Sprintf(validation.MsgExpectedExclusiveMinimumNumber, 0), "body.n", ReasonOutOfRange, "/n", "", nil, nil, nil, nil, Ptr(0.0), nil, nil},
		{fmt.Sprintf(validation.MsgExpectedMaximumNumber, 9), "body.n", ReasonOutOfRange, "/n", "", nil, nil, nil, nil, nil, Ptr(9.0), nil},
		{fmt.Sprintf(validation.MsgExpectedExclusiveMaximumNumber, 10), "body.n", ReasonOutOfRange, "/n", "", nil, nil, nil, nil, nil, Ptr(10.0), nil},
		{validation.MsgExpectedArrayItemsUnique, "body.tags", ReasonDuplicateItem, "/tags", "", nil, nil, nil, nil, nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedOneOf, "a, b, c"), "body.color", ReasonUnknownValue, "/color", "", nil, nil, nil, nil, nil, nil, []string{"a", "b", "c"}},
		{fmt.Sprintf(validation.MsgExpectedConst, "alpha"), "body.locked", ReasonUnknownValue, "/locked", "", nil, nil, nil, nil, nil, nil, []string{"alpha"}},
		{fmt.Sprintf(validation.MsgExpectedRequiredProperty, "title"), "body.nested", ReasonRequired, "/nested/title", "", nil, nil, nil, nil, nil, nil, nil},
		{fmt.Sprintf(validation.MsgExpectedRequiredProperty, "a/b~c"), "body.nested", ReasonRequired, "/nested/a~1b~0c", "", nil, nil, nil, nil, nil, nil, nil},
		{"required query parameter is missing", "query.q", ReasonRequired, "", "q", nil, nil, nil, nil, nil, nil, nil},
		{validation.MsgExpectedRFC3339DateTime, "body.when", ReasonInvalidFormat, "/when", "", nil, nil, nil, nil, nil, nil, nil},
		{validation.MsgExpectedMatchPattern, "body.pat", ReasonInvalidFormat, "/pat", "", nil, nil, nil, nil, nil, nil, nil},
	}
	for _, tc := range cases {
		p := FromHuma(nil, http.StatusUnprocessableEntity, "validation failed",
			&huma.ErrorDetail{Message: tc.msg, Location: tc.loc})
		if p.Code != CodeValidationFailed {
			t.Fatalf("%s: code %s", tc.msg, p.Code)
		}
		if len(p.Errors) != 1 {
			t.Fatalf("%s: errors %d", tc.msg, len(p.Errors))
		}
		got := p.Errors[0]
		if got.Reason != tc.reason || got.Pointer != tc.pointer || got.Parameter != tc.param {
			t.Fatalf("%s: reason=%s pointer=%s param=%s want %s %s %s",
				tc.msg, got.Reason, got.Pointer, got.Parameter, tc.reason, tc.pointer, tc.param)
		}
		assertParams(t, tc.msg, got.Params, tc.maxLen, tc.minLen, tc.maxItems, tc.minItems, tc.minimum, tc.maximum, tc.allowed)
	}
}

func TestHumaMsgConstantsStayMapped(t *testing.T) {
	mapped := []struct {
		msg    string
		reason string
	}{
		{fmt.Sprintf(validation.MsgExpectedMaxLength, 1), ReasonTooLong},
		{fmt.Sprintf(validation.MsgExpectedMinLength, 1), ReasonTooShort},
		{fmt.Sprintf(validation.MsgExpectedMaxItems, 1), ReasonTooManyItems},
		{fmt.Sprintf(validation.MsgExpectedMinItems, 1), ReasonTooFewItems},
		{fmt.Sprintf(validation.MsgExpectedMinimumNumber, 1), ReasonOutOfRange},
		{fmt.Sprintf(validation.MsgExpectedExclusiveMinimumNumber, 1), ReasonOutOfRange},
		{fmt.Sprintf(validation.MsgExpectedMaximumNumber, 1), ReasonOutOfRange},
		{fmt.Sprintf(validation.MsgExpectedExclusiveMaximumNumber, 1), ReasonOutOfRange},
		{validation.MsgExpectedArrayItemsUnique, ReasonDuplicateItem},
		{fmt.Sprintf(validation.MsgExpectedOneOf, "x"), ReasonUnknownValue},
		{fmt.Sprintf(validation.MsgExpectedConst, "x"), ReasonUnknownValue},
		{fmt.Sprintf(validation.MsgExpectedRequiredProperty, "x"), ReasonRequired},
		{validation.MsgUnexpectedProperty, ReasonInvalidFormat},
		{validation.MsgExpectedRFC3339DateTime, ReasonInvalidFormat},
		{validation.MsgExpectedRFC1123DateTime, ReasonInvalidFormat},
		{validation.MsgExpectedRFC3339Date, ReasonInvalidFormat},
		{validation.MsgExpectedRFC3339Time, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedRFC5322Email, "e"), ReasonInvalidFormat},
		{validation.MsgExpectedRFC5322EmailBare, ReasonInvalidFormat},
		{validation.MsgExpectedRFC5890Hostname, ReasonInvalidFormat},
		{validation.MsgExpectedRFC2673IPv4, ReasonInvalidFormat},
		{validation.MsgExpectedRFC2373IPv6, ReasonInvalidFormat},
		{validation.MsgExpectedRFCIPAddr, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedRFC3986URI, "u"), ReasonInvalidFormat},
		{validation.MsgExpectedRFC3986AbsoluteURI, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedRFC4122UUID, "u"), ReasonInvalidFormat},
		{validation.MsgExpectedRFC6570URITemplate, ReasonInvalidFormat},
		{validation.MsgExpectedRFC6901JSONPointer, ReasonInvalidFormat},
		{validation.MsgExpectedRFC6901RelativeJSONPointer, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedRegexp, "r"), ReasonInvalidFormat},
		{validation.MsgExpectedMatchAtLeastOneSchema, ReasonInvalidFormat},
		{validation.MsgExpectedMatchExactlyOneSchema, ReasonInvalidFormat},
		{validation.MsgExpectedNotMatchSchema, ReasonInvalidFormat},
		{validation.MsgExpectedPropertyNameInObject, ReasonInvalidFormat},
		{validation.MsgExpectedBoolean, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedDuration, "d"), ReasonInvalidFormat},
		{validation.MsgExpectedNumber, ReasonInvalidFormat},
		{validation.MsgExpectedInteger, ReasonInvalidFormat},
		{validation.MsgExpectedString, ReasonInvalidFormat},
		{validation.MsgExpectedBase64String, ReasonInvalidFormat},
		{validation.MsgExpectedArray, ReasonInvalidFormat},
		{validation.MsgExpectedObject, ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedNumberBeMultipleOf, 2), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedBePattern, "digits"), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedMatchPattern, "^a$"), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedMinProperties, 1), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedMaxProperties, 1), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedDependentRequiredProperty, "a", "b"), ReasonInvalidFormat},
		{fmt.Sprintf(validation.MsgExpectedResolvableSchemaRef, "#/x"), ReasonInvalidFormat},
	}
	for _, tc := range mapped {
		p := FromHuma(nil, http.StatusUnprocessableEntity, "validation failed",
			&huma.ErrorDetail{Message: tc.msg, Location: "body.x"})
		if len(p.Errors) != 1 || p.Errors[0].Reason != tc.reason {
			got := ""
			if len(p.Errors) > 0 {
				got = p.Errors[0].Reason
			}
			t.Fatalf("%q -> %s want %s", tc.msg, got, tc.reason)
		}
	}
	p := FromHuma(nil, http.StatusUnprocessableEntity, "validation failed",
		&huma.ErrorDetail{Message: "expected length <= 3", Location: "body.x"})
	if p.Errors[0].Reason != ReasonTooLong {
		t.Fatal("positive control: maxLength must map to TOO_LONG")
	}
}

func TestFromHumaThroughThrowawayOperation(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: WriteFiberError})
	cfg := huma.DefaultConfig("t", "0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	api := humafiber.New(app, cfg)
	prev, prevCtx := huma.NewError, huma.NewErrorWithContext
	t.Cleanup(func() {
		huma.NewError, huma.NewErrorWithContext = prev, prevCtx
	})
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return FromHuma(nil, status, msg, errs...)
	}
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return FromHuma(ctx, status, msg, errs...)
	}

	type nested struct {
		Title string `json:"title,omitempty" required:"true"`
	}
	type probeBody struct {
		Name   string   `json:"name" maxLength:"3"`
		Short  string   `json:"short" minLength:"3"`
		MinN   float64  `json:"min_n" minimum:"10"`
		ExMin  float64  `json:"ex_min" exclusiveMinimum:"0"`
		MaxN   float64  `json:"max_n" maximum:"5"`
		ExMax  float64  `json:"ex_max" exclusiveMaximum:"10"`
		Tags   []string `json:"tags" maxItems:"2"`
		Need   []string `json:"need" minItems:"2"`
		Uniq   []string `json:"uniq" uniqueItems:"true"`
		Color  string   `json:"color" enum:"red,blue"`
		Locked string   `json:"locked" const:"alpha"`
		Pat    string   `json:"pat" pattern:"^[a-z]+$"`
		URI    string   `json:"uri" format:"uri"`
		Nested nested   `json:"nested"`
	}
	type probeInput struct {
		Q    string `query:"q" required:"true"`
		Body probeBody
	}
	type probeOut struct {
		OK bool `json:"ok"`
	}
	type probeOutput struct {
		Body probeOut
	}
	huma.Register(api, huma.Operation{
		OperationID: "probe", Method: http.MethodPost, Path: "/v2/probe",
	}, func(context.Context, *probeInput) (*probeOutput, error) {
		return &probeOutput{Body: probeOut{OK: true}}, nil
	})

	valid := probeBody{
		Name: "ab", Short: "abc", MinN: 10, ExMin: 1, MaxN: 5, ExMax: 9,
		Tags: []string{"a"}, Need: []string{"a", "b"}, Uniq: []string{"a", "b"},
		Color: "red", Locked: "alpha", Pat: "ok", URI: "https://example.test/x",
		Nested: nested{Title: "t"},
	}
	post := func(q string, body any) Problem {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		path := "/v2/probe"
		if q != "" {
			path += "?q=" + q
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var p Problem
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatalf("decode status=%d body=%q: %v", resp.StatusCode, b, err)
		}
		return p
	}
	find := func(p Problem, pointer, param string) FieldError {
		t.Helper()
		for _, e := range p.Errors {
			if pointer != "" && e.Pointer == pointer {
				return e
			}
			if param != "" && e.Parameter == param {
				return e
			}
		}
		t.Fatalf("no error at pointer=%s param=%s in %+v", pointer, param, p.Errors)
		return FieldError{}
	}

	body := valid
	body.Name = "abcd"
	e := find(post("x", body), "/name", "")
	if e.Reason != ReasonTooLong || e.Params == nil || e.Params.MaxLength == nil || *e.Params.MaxLength != 3 {
		t.Fatalf("maxLength: %+v", e)
	}

	body = valid
	body.Short = "ab"
	e = find(post("x", body), "/short", "")
	if e.Reason != ReasonTooShort || e.Params == nil || e.Params.MinLength == nil || *e.Params.MinLength != 3 {
		t.Fatalf("minLength: %+v", e)
	}

	body = valid
	body.MinN = 9
	e = find(post("x", body), "/min_n", "")
	if e.Reason != ReasonOutOfRange || e.Params == nil || e.Params.Minimum == nil || *e.Params.Minimum != 10 {
		t.Fatalf("minimum: %+v", e)
	}

	body = valid
	body.ExMin = 0
	e = find(post("x", body), "/ex_min", "")
	if e.Reason != ReasonOutOfRange || e.Params == nil || e.Params.Minimum == nil || *e.Params.Minimum != 0 {
		t.Fatalf("exclusiveMinimum: %+v", e)
	}

	body = valid
	body.MaxN = 6
	e = find(post("x", body), "/max_n", "")
	if e.Reason != ReasonOutOfRange || e.Params == nil || e.Params.Maximum == nil || *e.Params.Maximum != 5 {
		t.Fatalf("maximum: %+v", e)
	}

	body = valid
	body.ExMax = 10
	e = find(post("x", body), "/ex_max", "")
	if e.Reason != ReasonOutOfRange || e.Params == nil || e.Params.Maximum == nil || *e.Params.Maximum != 10 {
		t.Fatalf("exclusiveMaximum: %+v", e)
	}

	body = valid
	body.Tags = []string{"a", "b", "c"}
	e = find(post("x", body), "/tags", "")
	if e.Reason != ReasonTooManyItems || e.Params == nil || e.Params.MaxItems == nil || *e.Params.MaxItems != 2 {
		t.Fatalf("maxItems: %+v", e)
	}

	body = valid
	body.Need = []string{"a"}
	e = find(post("x", body), "/need", "")
	if e.Reason != ReasonTooFewItems || e.Params == nil || e.Params.MinItems == nil || *e.Params.MinItems != 2 {
		t.Fatalf("minItems: %+v", e)
	}

	body = valid
	body.Uniq = []string{"a", "a"}
	e = find(post("x", body), "/uniq", "")
	if e.Reason != ReasonDuplicateItem {
		t.Fatalf("uniqueItems: %+v", e)
	}

	body = valid
	body.Color = "green"
	e = find(post("x", body), "/color", "")
	if e.Reason != ReasonUnknownValue || e.Params == nil || e.Params.Allowed == nil {
		t.Fatalf("enum: %+v", e)
	}

	body = valid
	body.Locked = "beta"
	e = find(post("x", body), "/locked", "")
	if e.Reason != ReasonUnknownValue || e.Params == nil || e.Params.Allowed == nil {
		t.Fatalf("const: %+v", e)
	}

	body = valid
	body.Pat = "OK"
	e = find(post("x", body), "/pat", "")
	if e.Reason != ReasonInvalidFormat {
		t.Fatalf("pattern: %+v", e)
	}

	body = valid
	body.URI = "not a url"
	e = find(post("x", body), "/uri", "")
	if e.Reason != ReasonInvalidFormat {
		t.Fatalf("format: %+v", e)
	}

	body = valid
	body.Nested = nested{}
	e = find(post("x", body), "/nested/title", "")
	if e.Reason != ReasonRequired {
		t.Fatalf("nested required: %+v", e)
	}

	e = find(post("", valid), "", "q")
	if e.Reason != ReasonRequired {
		t.Fatalf("required query: %+v", e)
	}

	okBody := post("x", valid)
	if okBody.Code != "" && len(okBody.Errors) > 0 && okBody.Status >= 400 {
		t.Fatalf("valid payload failed: %+v", okBody)
	}
}

func assertParams(t *testing.T, msg string, p *FieldParams, maxLen, minLen, maxItems, minItems *int, minimum, maximum *float64, allowed []string) {
	t.Helper()
	if maxLen == nil && minLen == nil && maxItems == nil && minItems == nil && minimum == nil && maximum == nil && allowed == nil {
		if p != nil {
			t.Fatalf("%s: unexpected params %+v", msg, p)
		}
		return
	}
	if p == nil {
		t.Fatalf("%s: missing params", msg)
	}
	eqInt := func(name string, got, want *int) {
		t.Helper()
		if want == nil {
			if got != nil {
				t.Fatalf("%s: unexpected %s", msg, name)
			}
			return
		}
		if got == nil || *got != *want {
			t.Fatalf("%s: %s got %v want %v", msg, name, got, *want)
		}
	}
	eqFloat := func(name string, got, want *float64) {
		t.Helper()
		if want == nil {
			if got != nil {
				t.Fatalf("%s: unexpected %s", msg, name)
			}
			return
		}
		if got == nil || *got != *want {
			t.Fatalf("%s: %s got %v want %v", msg, name, got, *want)
		}
	}
	eqInt("max_length", p.MaxLength, maxLen)
	eqInt("min_length", p.MinLength, minLen)
	eqInt("max_items", p.MaxItems, maxItems)
	eqInt("min_items", p.MinItems, minItems)
	eqFloat("minimum", p.Minimum, minimum)
	eqFloat("maximum", p.Maximum, maximum)
	if allowed == nil {
		if p.Allowed != nil {
			t.Fatalf("%s: unexpected allowed", msg)
		}
		return
	}
	if p.Allowed == nil || len(*p.Allowed) != len(allowed) {
		t.Fatalf("%s: allowed %v want %v", msg, p.Allowed, allowed)
	}
	for i := range allowed {
		if (*p.Allowed)[i] != allowed[i] {
			t.Fatalf("%s: allowed %v want %v", msg, *p.Allowed, allowed)
		}
	}
}
