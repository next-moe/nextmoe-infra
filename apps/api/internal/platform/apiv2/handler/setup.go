package handler

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/devapi"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

var installOnce sync.Once

type Options struct {
	Store            protocol.Store
	Works            WorksFunc
	Catalog          *Catalog
	LookupCredential func(ctx context.Context, rawToken string) (*devapi.Credential, error)
	LookupUser       func(ctx context.Context, rawToken string) (UserIdentity, error)
	LookupSite       func(ctx context.Context, clientID string) (SiteBinding, error)
}

func Setup(app *fiber.App) huma.API {
	return SetupWith(app, Options{})
}

func SetupWith(app *fiber.App, opt Options) huma.API {
	installOnce.Do(func() {
		prev := huma.NewErrorWithContext
		huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
			if ctx != nil {
				u := ctx.URL()
				if strings.HasPrefix(u.Path, "/v2") {
					return problem.FromHuma(ctx, status, msg, errs...)
				}
			}
			return prev(ctx, status, msg, errs...)
		}
	})

	// Registered inside protocol.Middleware's c.Next(), so the projection is
	// already applied when applyETag hashes the body. Filled after the
	// registrations below; the closure holds the map, not a copy.
	declaredFields := map[string]bool{}
	app.Use(protocol.Middleware(opt.Store))
	app.Use(fieldsProjection(declaredFields))
	app.Use(catalogAuth(opt.LookupCredential))
	app.Use(userAuth(opt.LookupUser, opt.LookupSite))
	app.Use(protocol.RateLimit(opt.Store, credentialLimitIdentity))
	app.Use(protocol.Idempotency(opt.Store, credentialLimitIdentity))

	cfg := huma.DefaultConfig("NextMoe Public API v2", "2.10.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Info.Description = "NextMoe public API v2. Public since 2026-08-25: any application mints its own nmk_ key in the developer portal, no approval required. The shape evolves additively; a breaking change to this document fails CI."
	cfg.Info.Extensions = map[string]any{"x-stability": "stable"}

	api := humafiber.New(app, cfg)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		fc := humafiber.Unwrap(ctx)
		id := problem.RequestID(fc)
		ctx = huma.WithValue(ctx, "request_id", id)
		ctx = huma.WithValue(ctx, "instance", problem.Instance(fc))
		switch id := fc.Locals("user_id").(type) {
		case uint:
			ctx = huma.WithValue(ctx, ctxUserID, int64(id))
		case int64:
			ctx = huma.WithValue(ctx, ctxUserID, id)
		}
		if clientID, ok := fc.Locals("token_client_id").(string); ok {
			ctx = huma.WithValue(ctx, ctxClientID, clientID)
		}
		if site, ok := fc.Locals("catalog_site").(string); ok {
			ctx = huma.WithValue(ctx, ctxSite, site)
		}
		if roles, ok := fc.Locals("user_roles").([]string); ok {
			ctx = huma.WithValue(ctx, ctxRoles, roles)
		}
		if tp, ok := fc.Locals("catalog_third_party").(bool); ok {
			ctx = huma.WithValue(ctx, ctxThirdParty, tp)
		}
		if cred := devapi.CredentialFrom(fc); cred != nil {
			ctx = huma.WithValue(ctx, ctxCredClient, cred.ClientID)
		}
		next(ctx)
	})
	prevErr := huma.NewError
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return problem.FromHuma(nil, status, msg, errs...)
	}
	works := opt.Works
	if works == nil && opt.Catalog != nil {
		works = opt.Catalog.ListWorks
	}
	registerMeta(api)
	registerCollections(api, works, opt.Catalog)
	registerCatalog(api, opt.Catalog)
	registerMe(api, opt.Catalog)
	registerMeWrite(api, opt.Catalog)
	registerMeNews(api, opt.Catalog)
	registerStore(api, opt.Catalog)
	huma.NewError = prevErr
	annotateSpec(api.OpenAPI())
	declaredFieldsOps(api.OpenAPI(), declaredFields)
	return api
}

func annotateSpec(doc *huma.OpenAPI) {
	if doc.Info != nil {
		if doc.Info.Extensions == nil {
			doc.Info.Extensions = map[string]any{}
		}
		doc.Info.Extensions["x-stability"] = "stable"
	}
	if len(doc.Servers) == 0 {
		doc.Servers = []*huma.Server{{
			URL:         "https://api.nextmoe.dev",
			Description: "Production. Paths in this document already include the /v2 prefix.",
		}}
	}
	var problemRef *huma.Schema
	if doc.Components != nil && doc.Components.Schemas != nil {
		problemRef = doc.Components.Schemas.Schema(reflect.TypeOf(problem.Problem{}), true, "Problem")
		for _, v := range []any{
			repr.Image{}, repr.Cover{}, repr.Names{}, repr.LocalizedText{},
			repr.WorkTitle{}, repr.EntityName{}, repr.Intro{}, repr.Ref{},
			repr.Claim{}, repr.Work{}, repr.FacetValue{},
			repr.Tag{}, repr.Company{}, repr.CreditName{}, repr.Character{},
			repr.Series{}, repr.Engine{}, repr.NewsSource{}, repr.NewsItem{},
			repr.CatalogStats{}, repr.Screenshot{}, repr.WorkTag{}, repr.WorkCharacter{},
			repr.CreditGroup{}, repr.CreditEntry{}, repr.Release{}, repr.Relation{},
			repr.WorkCompany{}, repr.ViaCompany{}, repr.WorkLink{}, repr.Rating{},
			repr.CalendarMeta{},
			repr.RatingBucket{}, repr.RatingStats{}, repr.Popularity{},
			repr.Playtime{}, repr.WorkPlatform{}, repr.WorkSeriesRef{},
			repr.Change{}, repr.Redirect{}, repr.SearchHit{},
			repr.CompanyGraph{}, repr.CompanyGraphNode{}, repr.CompanyGraphEdge{},
			repr.ObjectSchema{}, repr.SchemaField{},
			repr.Person{}, repr.Trait{}, repr.Measurements{}, repr.NameCredit{}, repr.NameCreditRole{}, repr.Appearance{},
			repr.CharacterTrait{}, repr.WorkEngineRef{},
			repr.UserPlaytime{}, repr.CoverVote{}, repr.ClaimRecord{},
			repr.PlaytimeBatchItem{}, repr.ProposalRecord{}, repr.ClaimDecisionRecord{}, repr.ProposalDecisionRecord{}, repr.SnapshotRecord{},
			repr.Revision{}, repr.FieldDiff{}, repr.Amendment{}, repr.EditImage{},
			repr.NewsSubmission{}, repr.ClaimEventRef{}, repr.ClaimEvent{},
			repr.StorePurchaseLinks{}, repr.StoreCampaign{},
			repr.StoreStats{}, repr.StoreStatRow{}, repr.StoreStatTotal{},
		} {
			doc.Components.Schemas.Schema(reflect.TypeOf(v), true, "")
		}
		// Registering a multipart operation makes huma register its own FormFile
		// struct as a named component that nothing $refs — the multipart body
		// gets an inline {type: string, format: binary} instead. Left in place
		// its four undocumented Go fields fail G2 and G14, which is how it was
		// found.
		delete(doc.Components.Schemas.Map(), "FormFile")
		for _, schema := range doc.Components.Schemas.Map() {
			markClosedEnums(schema)
			forceObjectOpen(schema)
			markOpenVocab(schema)
		}
	}
	declareSecuritySchemes(doc)
	for path, item := range doc.Paths {
		for _, op := range pathOps(item) {
			rewriteErrorResponses(path, op, problemRef)
			if scheme, _ := v2Security(path); scheme != "" {
				op.Security = []map[string][]string{{scheme: {}}}
			}
			for _, p := range op.Parameters {
				if p != nil && p.Schema != nil {
					markClosedEnums(p.Schema)
				}
			}
		}
	}
}

// Both are `type: http`, for which OpenAPI requires the per-operation scope
// list to be empty — the scope a key must hold stays in the operation's own
// description rather than being smuggled into a field that only means something
// for oauth2.
func declareSecuritySchemes(doc *huma.OpenAPI) {
	if doc.Components == nil {
		return
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	doc.Components.SecuritySchemes[securityAppKey] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer",
		Description: "An application key (nmk_...) minted in the developer portal. Required scopes are named in each operation's description; a key without them answers 403 SCOPE_REQUIRED.",
	}
	doc.Components.SecuritySchemes[securityUserToken] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer",
		Description: "An OAuth user access token. An application key sent here is refused: /v2/me and /v2/moderation act as a person, not as an app.",
	}
}

func pathOps(item *huma.PathItem) []*huma.Operation {
	if item == nil {
		return nil
	}
	ops := make([]*huma.Operation, 0, 8)
	for _, op := range []*huma.Operation{
		item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace,
	} {
		if op != nil {
			ops = append(ops, op)
		}
	}
	return ops
}

func rewriteErrorResponses(path string, op *huma.Operation, problemRef *huma.Schema) {
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	delete(op.Responses, "default")
	if _, ok := op.Responses["429"]; !ok {
		op.Responses["429"] = &huma.Response{Description: http.StatusText(http.StatusTooManyRequests)}
	}
	if op.Method == http.MethodGet || op.Method == http.MethodHead {
		if _, ok := op.Responses["304"]; !ok {
			op.Responses["304"] = &huma.Response{Description: "Not Modified. The representation is unchanged."}
		}
	}
	for status, resp := range op.Responses {
		if status == "200" || status == "201" || status == "204" || status == "207" || status == "304" {
			continue
		}
		if resp == nil {
			resp = &huma.Response{}
			op.Responses[status] = resp
		}
		if resp.Description == "" {
			resp.Description = http.StatusText(atoiStatus(status))
		}
		resp.Content = map[string]*huma.MediaType{
			"application/problem+json": {Schema: problemRef},
		}
	}
}

func forceObjectOpen(s *huma.Schema) {
	if s == nil {
		return
	}
	if s.Properties != nil {
		s.AdditionalProperties = true
	}
	if s.Type == "array" || s.Items != nil {
		s.Nullable = false
	}
	for _, p := range s.Properties {
		forceObjectOpen(p)
	}
	forceObjectOpen(s.Items)
	for _, x := range s.OneOf {
		forceObjectOpen(x)
	}
	for _, x := range s.AnyOf {
		forceObjectOpen(x)
	}
	for _, x := range s.AllOf {
		forceObjectOpen(x)
	}
}

func markOpenVocab(s *huma.Schema) {
	if s == nil {
		return
	}
	for name, p := range s.Properties {
		if p == nil {
			continue
		}
		switch name {
		case "source":
			if p.Ref != "" || p.Properties != nil {
				break
			}
			if p.Extensions == nil {
				p.Extensions = map[string]any{}
			}
			p.Extensions["x-vocabulary-closed"] = false
			p.Extensions["x-vocabulary"] = "sources"
		}
		markOpenVocab(p)
	}
	markOpenVocab(s.Items)
}

func markClosedEnums(s *huma.Schema) {
	if s == nil {
		return
	}
	if len(s.Enum) > 0 {
		if s.Extensions == nil {
			s.Extensions = map[string]any{}
		}
		if _, ok := s.Extensions["x-vocabulary-closed"]; !ok {
			s.Extensions["x-vocabulary-closed"] = true
		}
	}
	for _, p := range s.Properties {
		markClosedEnums(p)
	}
	markClosedEnums(s.Items)
	for _, x := range s.OneOf {
		markClosedEnums(x)
	}
	for _, x := range s.AnyOf {
		markClosedEnums(x)
	}
	for _, x := range s.AllOf {
		markClosedEnums(x)
	}
	markClosedEnums(s.Not)
}

func atoiStatus(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
