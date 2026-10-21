package appface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

const (
	SettingsPath = "/v2/apps/{client_id}/settings"
	SpecPath     = "/v2/apps/openapi.json"
)

type Client struct {
	SiteID *uint
	Public bool
}

type Lookup func(ctx context.Context, clientID string) (client Client, found bool, err error)

type ctxKey string

const (
	ctxRequestID ctxKey = "appface_request_id"
	ctxInstance  ctxKey = "appface_instance"
)

var installOnce sync.Once

func Setup(app *fiber.App, lookup Lookup) huma.API {
	installOnce.Do(func() {
		prev := huma.NewErrorWithContext
		huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
			if ctx != nil && strings.HasPrefix(ctx.URL().Path, "/v2") {
				return problem.FromHuma(ctx, status, msg, errs...)
			}
			return prev(ctx, status, msg, errs...)
		}
	})

	cfg := huma.DefaultConfig("NextMoe App Settings API", "1.0.1")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Info.Description = "Runtime settings for first-party native apps, read from the NextMoe config center. " +
		"No credential: an app holds no secret, so it names itself by its public OAuth client_id. " +
		"Calls without a credential share the anonymous /v2 rate limit and daily quota of their IPv4 address or IPv6 /64; " +
		"a signed-in app may send its user access token as Authorization: Bearer, which changes only the bucket it is counted in."
	cfg.Servers = []*huma.Server{{URL: "https://api.nextmoe.dev", Description: "Production. Paths already include /v2/apps."}}
	api := humafiber.New(app, cfg)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		fc := humafiber.Unwrap(ctx)
		ctx = huma.WithValue(ctx, ctxRequestID, problem.RequestID(fc))
		ctx = huma.WithValue(ctx, ctxInstance, problem.Instance(fc))
		next(ctx)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getAppSettings",
		Method:      http.MethodGet,
		Path:        SettingsPath,
		Tags:        []string{"Settings"},
		Summary:     "The settings one native app must follow",
		Description: "Every public key of the config center, resolved for the site the client is bound to " +
			"(the same values a site reads from GET /api/v1/settings), plus the keys declared for this app. " +
			"A key the app does not know is to be ignored; new keys are only ever added. " +
			"Send the last ETag as If-None-Match: an unchanged body answers 304 with no body.",
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Responses: map[string]*huma.Response{
			"304": {Description: "Not Modified. The settings are unchanged since the ETag sent as If-None-Match."},
		},
	}, get(lookup))

	wireshape.PublishProblems(api.OpenAPI(), reflect.TypeFor[problem.Problem](), "Problem")
	return api
}

type settingsInput struct {
	ClientID    string `path:"client_id" maxLength:"50" pattern:"^[A-Za-z0-9._-]+$" doc:"The app's public OAuth client_id, e.g. kungal-app."`
	IfNoneMatch string `header:"If-None-Match" doc:"The ETag of the last 200 this app stored, sent back exactly as received. Compared weakly, so the strong and the W/ form both match; a match answers 304."`
}

type settingsOutput struct {
	ETag string `header:"ETag" doc:"Validator of this body. The origin sends it strong; a response the CDN compressed carries it as W/\"...\". Send it back unchanged as If-None-Match."`
	Body AppSettings
}

type AppSettings struct {
	_        struct{}         `json:"-" additionalProperties:"true"`
	ClientID string           `json:"client_id" doc:"The client_id from the path."`
	SiteID   *uint            `json:"site_id" doc:"The site the client is bound to, whose site overrides are applied; null when it has no site binding and every value is the platform one."`
	Settings AppSettingValues `json:"settings"`
}

type AppSettingValues map[string]any

func get(lookup Lookup) func(context.Context, *settingsInput) (*settingsOutput, error) {
	return func(ctx context.Context, in *settingsInput) (*settingsOutput, error) {
		if lookup == nil {
			return nil, stamp(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "client lookup is not configured."))
		}
		cl, found, err := lookup(ctx, in.ClientID)
		if err != nil {
			slog.Error("app settings: client lookup failed", "client_id", in.ClientID, "err", err)
			return nil, stamp(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "client lookup is unavailable."))
		}
		if !found || !cl.Public {
			return nil, stamp(ctx, problem.New(problem.CodeNotFound, "", "", "no public client has this client_id."))
		}

		values := AppSettingValues{}
		for _, e := range keys.Live().Entries() {
			if !e.Meta().Public {
				continue
			}
			if cl.SiteID != nil {
				values[e.Meta().Name] = e.CurrentForSite(*cl.SiteID)
			} else {
				values[e.Meta().Name] = e.Current()
			}
		}
		for _, e := range keys.AppKeys(in.ClientID) {
			values[e.Meta().Name] = e.Current()
		}

		body := AppSettings{ClientID: in.ClientID, SiteID: cl.SiteID, Settings: values}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		return &settingsOutput{ETag: `"` + hex.EncodeToString(sum[:16]) + `"`, Body: body}, nil
	}
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

func (AppSettingValues) Schema(huma.Registry) *huma.Schema {
	props := map[string]*huma.Schema{}
	for _, e := range keys.Live().Entries() {
		if e.Meta().Public {
			props[e.Meta().Name] = keySchema(e)
		}
	}
	for _, e := range keys.AllAppKeys() {
		props[e.Meta().Name] = keySchema(e)
	}
	return &huma.Schema{
		Type: huma.TypeObject,
		Description: "Setting key to its effective value. Every property is optional: an app falls back to its built-in default " +
			"for a key that is missing, and ignores a key it does not know.",
		Properties:           props,
		AdditionalProperties: true,
	}
}

func keySchema(e settings.Entry) *huma.Schema {
	m := e.Meta()
	s := &huma.Schema{Description: m.DescEN, Default: e.Default(), Minimum: m.Min, Maximum: m.Max}
	switch m.Kind {
	case settings.KindBool:
		s.Type = huma.TypeBoolean
	case settings.KindInt:
		s.Type, s.Format = huma.TypeInteger, "int64"
	case settings.KindFloat:
		s.Type = huma.TypeNumber
	case settings.KindString:
		s.Type, s.Pattern = huma.TypeString, m.Pattern
	case settings.KindEnum:
		s.Type = huma.TypeString
		for _, v := range m.Enum {
			s.Enum = append(s.Enum, v)
		}
	case settings.KindStringList:
		s.Type, s.Items = huma.TypeArray, &huma.Schema{Type: huma.TypeString, Pattern: m.Pattern}
	}
	return s
}
