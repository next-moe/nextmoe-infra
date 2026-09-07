package handler

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/apiv2/vocab"
	"api/internal/platform/devapi"
	"api/internal/platform/settings/keys"
	"api/pkg/routepath"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

// Exported on purpose: an anonymous embed of an unexported type is an
// unexported field, which huma skips when it collects query parameters. While
// this was collectionInput, six embedding routes (search, redirects, calendar,
// credit-names, me/playtimes, me/proposals) shipped with cursor/limit/view
// undocumented and unbound, and the credit-names ids= batch lane silently
// answered page 1 instead.
type CollectionInput struct {
	Cursor       string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit        string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
	View         string `query:"view" maxLength:"16" doc:"basic (default) or full. Closed vocabulary."`
	Include      string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. Unknown token is 400 UNKNOWN_INCLUDE."`
	Fields       string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept."`
	IDs          string `query:"ids" maxLength:"4096" doc:"Comma-separated ids, max 100. Batch lane: no pagination."`
	Refs         string `query:"refs" maxLength:"4096" doc:"Comma-separated source:external_id, max 100. Batch lane: no pagination."`
	IncludeTotal string `query:"include_total" maxLength:"8" doc:"true to include total. Only true or false."`
	Facets       string `query:"facets" maxLength:"512" doc:"Comma-separated facet names. Unknown token is 400 UNKNOWN_FACET."`
	Sort         string `query:"sort" maxLength:"32" doc:"Closed per-collection sort key."`
	NSFW         string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false."`
}

type listVocabOutput struct {
	Body repr.List[vocab.Vocabulary]
}
type listProblemsOutput struct {
	Body repr.List[problemType]
}
type listWorksOutput struct {
	Body repr.List[repr.Work]
}

// The 500 here is redundant, not authoritative: huma.Register appends
// http.StatusInternalServerError to op.Errors for every operation that declares
// any error at all (huma.go:736-741), so all 88 v2 operations publish a 500
// whatever this list says. Dropping it from the two compiled-in lanes
// (/v2/problems, /v2/vocabularies) was tried and changed nothing in the
// generated document; removing it there would mean deleting a response huma
// injects, from two operations out of 88, in annotateSpec — a hand-maintained
// exception list, which is the shape this file exists to avoid.
func collectionErrors(extra ...int) []int {
	out := []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError}
	return append(out, extra...)
}

type listWorksInput struct {
	Cursor         string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit          string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
	View           string `query:"view" maxLength:"16" doc:"basic (default) or full. Closed vocabulary."`
	Include        string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. Unknown token is 400 UNKNOWN_INCLUDE."`
	Fields         string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept."`
	IDs            string `query:"ids" maxLength:"4096" doc:"Comma-separated ids, max 100. Batch lane: no pagination."`
	Refs           string `query:"refs" maxLength:"4096" doc:"Comma-separated source:external_id, max 100. Batch lane: no pagination."`
	IncludeTotal   string `query:"include_total" maxLength:"8" doc:"true to include total. Only true or false."`
	Facets         string `query:"facets" maxLength:"512" doc:"Comma-separated facet names. Unknown token is 400 UNKNOWN_FACET."`
	Sort           string `query:"sort" maxLength:"32" doc:"Closed per-collection sort key."`
	NSFW           string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false."`
	Q              string `query:"q" maxLength:"512" doc:"Work title search. Switches this collection to the search index; sort defaults to relevance. Must not be used as a discriminant."`
	ContentRating  string `query:"content_rating" maxLength:"16" doc:"Closed: all_ages, sensitive, r18. r18 requires nsfw=true."`
	Claimed        string `query:"claimed" maxLength:"8" doc:"true or false. Absent = no gate."`
	ClaimState     string `query:"claim_state" maxLength:"128" doc:"Comma-separated closed states: none, live, draft. pending, declined and hidden are the per-site moderation queue: they need a key holding claim_events:read and a site= naming the caller's own site, and are otherwise not in the vocabulary."`
	ContentLimit   string `query:"content_limit" maxLength:"32" doc:"Comma-separated closed editorial axis: sfw, nsfw."`
	Site           string `query:"site" maxLength:"64" doc:"Claiming site key. Open vocabulary; unknown values match nothing."`
	OwnerUID       string `query:"owner_uid" maxLength:"20" doc:"The claiming site's own user id of the claim owner. Requires site=. Live registry filter; cannot be combined with q= or search sorts."`
	CompanyID      string `query:"company_id" maxLength:"20" doc:"Catalog company id. Live registry filter when q= is absent."`
	CompanyRollup  string `query:"company_rollup" maxLength:"8" doc:"true expands company_id one hop down imprint/subsidiary. Only true or false."`
	TagID          string `query:"tag_id" maxLength:"256" doc:"Comma-separated canonical tag ids, AND, max 10."`
	SeriesID       string `query:"series_id" maxLength:"20" doc:"Catalog series id."`
	EngineID       string `query:"engine_id" maxLength:"20" doc:"Catalog engine id."`
	Platform       string `query:"platform" maxLength:"64" doc:"Open vocabulary platform token. Unknown matches nothing."`
	ReleasedAfter  string `query:"released_after" maxLength:"10" doc:"YYYY-MM-DD inclusive, earliest release per work."`
	ReleasedBefore string `query:"released_before" maxLength:"10" doc:"YYYY-MM-DD inclusive, earliest release per work."`
	OLang          string `query:"olang" maxLength:"64" doc:"Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = no language gate."`
}

func registerCollections(api huma.API, works WorksFunc, cat *Catalog) {
	tags := []string{"meta"}
	huma.Register(api, huma.Operation{
		OperationID:        "listProblemTypes",
		Method:             http.MethodGet,
		Path:               "/v2/problems",
		Summary:            "List every top-level error code",
		Description:        "The closed registry of top-level error codes. Keyset-paginated. Unauthenticated.",
		Tags:               tags,
		Errors:             collectionErrors(),
		SkipValidateParams: true,
	}, listProblemTypes)
	huma.Register(api, huma.Operation{
		OperationID:        "listVocabularies",
		Method:             http.MethodGet,
		Path:               "/v2/vocabularies",
		Summary:            "List published vocabularies",
		Description:        "Closed and seed-open vocabularies. Keyset-paginated. Unauthenticated.",
		Tags:               tags,
		Errors:             collectionErrors(),
		SkipValidateParams: true,
	}, listVocabularies)
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogWorks",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/works",
		Summary:            "List catalog works",
		Description:        "Keyset-paginated work collection. q= switches to search (sort defaults to relevance). company_id=/tag_id=/series_id= filter the live registry when q= is absent. Requires an application key or a user access token with catalog:read. view/include/fields/ids/refs/facets follow the v2 collection contract. include=titles,refs,intros,covers,companies,ratings,tags,credits fills on every lane; view=full is all of them except credits, which is an explicit ask. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations/releases/popularity/playtimes/series/platforms/screenshots/characters/engines/links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listWorks(works, cat))
}

type WorksFunc func(ctx context.Context, q collect.Query) (repr.List[repr.Work], error)

func listVocabularies(ctx context.Context, in *CollectionInput) (*listVocabOutput, error) {
	q, err := collect.Parse(rawFrom(in), collect.VocabSpec())
	if err != nil {
		return nil, withIdent(ctx, err)
	}
	page, perr := collect.SliceErr(vocab.All(), q, func(v vocab.Vocabulary) string { return v.Name })
	if perr != nil {
		return nil, withIdent(ctx, perr)
	}
	return &listVocabOutput{Body: page}, nil
}

func listProblemTypes(ctx context.Context, in *CollectionInput) (*listProblemsOutput, error) {
	q, err := collect.Parse(rawFrom(in), collect.ProblemSpec())
	if err != nil {
		return nil, withIdent(ctx, err)
	}
	all := make([]problemType, 0, len(problem.Codes))
	for _, d := range problem.Codes {
		all = append(all, problemTypeFrom(d))
	}
	page, perr := collect.SliceErr(all, q, func(v problemType) string { return v.Code })
	if perr != nil {
		return nil, withIdent(ctx, perr)
	}
	return &listProblemsOutput{Body: page}, nil
}

func listWorks(src WorksFunc, cat *Catalog) func(context.Context, *listWorksInput) (*listWorksOutput, error) {
	return func(ctx context.Context, in *listWorksInput) (*listWorksOutput, error) {
		if in == nil {
			in = &listWorksInput{}
		}
		q, err := collect.Parse(rawFrom(&CollectionInput{
			Cursor: in.Cursor, Limit: in.Limit, View: in.View, Include: in.Include,
			Fields: in.Fields, IDs: in.IDs, Refs: in.Refs, IncludeTotal: in.IncludeTotal,
			Facets: in.Facets, Sort: in.Sort, NSFW: in.NSFW,
		}), collect.WorkListSpec())
		if err != nil {
			return nil, withIdent(ctx, err)
		}
		modSite, merr := cat.moderationClaimStateSite(ctx, in.ClaimState)
		if merr != nil {
			return nil, withIdent(ctx, merr)
		}
		filt, ferr := parseWorksFilter(in, modSite != "")
		if ferr != nil {
			return nil, withIdent(ctx, ferr)
		}
		if serr := fenceModerationSite(filt, modSite); serr != nil {
			return nil, withIdent(ctx, serr)
		}
		if cat != nil && cat.Public != nil {
			page, lerr := cat.ListWorksFiltered(ctx, q, filt)
			if lerr != nil {
				return nil, catalogErr(ctx, lerr)
			}
			return &listWorksOutput{Body: page}, nil
		}
		if src == nil {
			id, inst := ident(ctx)
			return nil, problem.New(problem.CodeServiceUnavailable, id, inst, "works collection is not bound.")
		}
		page, lerr := src(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWorksOutput{Body: page}, nil
	}
}

func rawFrom(in *CollectionInput) collect.Raw {
	if in == nil {
		return collect.Raw{}
	}
	return collect.Raw{
		Cursor: in.Cursor, Limit: in.Limit, View: in.View, Include: in.Include,
		Fields: in.Fields, IDs: in.IDs, Refs: in.Refs, IncludeTotal: in.IncludeTotal,
		Facets: in.Facets, Sort: in.Sort, NSFW: in.NSFW,
	}
}

func withIdent(ctx context.Context, p *problem.Problem) *problem.Problem {
	if p == nil {
		return nil
	}
	id, inst := ident(ctx)
	if p.RequestID == "" {
		p.RequestID = id
	}
	if p.Instance == "" {
		p.Instance = inst
	}
	return p
}

func credentialLimitIdentity(c fiber.Ctx) (protocol.LimitIdentity, bool) {
	if cred := devapi.CredentialFrom(c); cred != nil {
		rate, unlimited := cred.EffectiveRate()
		quota, _ := cred.EffectiveQuota()
		return protocol.LimitIdentity{
			Key:  "k" + strconv.FormatUint(uint64(cred.KeyID), 10),
			Rate: rate, Quota: quota, Unlimited: unlimited,
		}, true
	}
	// A user token authenticates but carries no application key, so this used to
	// fall through to the anonymous per-IP bucket. A first-party backend relays
	// every logged-in user's /v2/me call from one egress IP, so the whole site
	// shared one 10k/day quota: the forum's user plane answered "Daily quota
	// exceeded." from 2026-08-27T22:46Z on, re-tripping after each midnight reset.
	var uid int64
	switch id := c.Locals("user_id").(type) {
	case uint:
		uid = int64(id)
	case int64:
		uid = id
	}
	if uid > 0 {
		return protocol.LimitIdentity{
			Key:  "u" + strconv.FormatInt(uid, 10),
			Rate: int(keys.APIV2DefaultRatePerMinute.Get()), Quota: int(keys.APIV2DefaultQuotaPerDay.Get()),
		}, true
	}
	return protocol.LimitIdentity{}, false
}

const (
	securityAppKey    = "applicationKey"
	securityUserToken = "userToken"
)

// One authority for "what credential does this path take", read by the runtime
// gate and by the published document. The document carried no
// components.securitySchemes at all and 0 of 88 operations declared security,
// so every generated client and the MCP surface had no auth parameter to fill
// in; a second hand-maintained list next to the gate would have drifted from it
// the first time a prefix moved.
//
// A path with two schemes takes either one, never both at once: the OpenAPI
// requirement list is an OR, and the gate below dispatches on the single Bearer
// value (refs/api-v2 D1, one request one credential).
func v2Security(path string) (schemes []string, scope string) {
	switch {
	case strings.HasPrefix(path, "/v2/me/") || strings.HasPrefix(path, "/v2/moderation/"):
		return []string{securityUserToken}, ""
	case strings.HasPrefix(path, "/v2/catalog/"):
		if path == "/v2/catalog/openapi.json" || path == "/v2/catalog/stats" ||
			strings.HasPrefix(path, "/v2/catalog/schemas/") {
			return nil, ""
		}
		// claim_events:read is operator-granted and has no consent form a person
		// could tick, so the one catalog read that demands a scope beyond
		// catalog:read stays key-only on the wire as well as in the gate.
		if path == claimEventsPath {
			return []string{securityAppKey}, devapi.ScopeCatalogRead
		}
		return []string{securityAppKey, securityUserToken}, devapi.ScopeCatalogRead
	case path == "/v2/folders" || strings.HasPrefix(path, "/v2/folders/"):
		return []string{securityAppKey, securityUserToken}, devapi.ScopeCatalogRead
	case strings.HasPrefix(path, "/v2/store/prices"):
		return nil, ""
	case strings.HasPrefix(path, "/v2/store/"):
		return []string{securityAppKey}, devapi.ScopeStoreRead
	}
	return nil, ""
}

// The keyless lanes (stats, schemas, openapi.json, news, problems,
// vocabularies) returned before resolving anything, so an application key sent
// to them was thrown away and its holder counted into the shared anonymous
// per-IP bucket instead of its own tier — the same shape as the #92 outage one
// layer up, where a whole first-party backend shared one 10k/day quota behind
// one egress IP. Best-effort on purpose: these lanes demand no credential, so a
// bad key must stay anonymous rather than become a refusal.
func attachOptionalCredential(c fiber.Ctx, lookup func(context.Context, string) (*devapi.Credential, error)) {
	if lookup == nil {
		return
	}
	const pfx = "Bearer "
	h := c.Get("Authorization")
	if !strings.HasPrefix(h, pfx) {
		return
	}
	token := strings.TrimSpace(h[len(pfx):])
	if !devapi.IsV2KeyPrefix(token) || !devapi.ValidV2Key(token) {
		return
	}
	if cred, err := lookup(c.Context(), token); err == nil && cred != nil {
		devapi.WithCredential(c, cred)
	}
}

// The gate resolves the moderation verdict once and the handlers consume it,
// so no face re-derives policy from raw scopes.
type catalogAuthz struct {
	ClientID       string
	ModerationRead bool
}

const ctxCatalogAuthz ctxKey = "v2_catalog_authz"

func catalogAuthzFrom(ctx context.Context) catalogAuthz {
	v, _ := ctx.Value(ctxCatalogAuthz).(catalogAuthz)
	return v
}

const claimEventsPath = "/v2/catalog/claim-events"

// The dispatch key is the token's own prefix, not a second header and not a
// try-one-then-the-other: an application key that fails its lane must never get
// a second reading as a user token, or a revoked key would fall through to
// whatever the user lane makes of it. nmk_ is the only marker that separates
// the two credentials on a surface that accepts either (refs/api-v2 D1).
func catalogAuth(
	lookup func(context.Context, string) (*devapi.Credential, error),
	lookupUser func(context.Context, string) (UserIdentity, error),
) fiber.Handler {
	return func(c fiber.Ctx) error {
		path := routepath.Normalize(c.Path())
		schemes, scope := v2Security(path)
		if !slices.Contains(schemes, securityAppKey) {
			attachOptionalCredential(c, lookup)
			return c.Next()
		}
		h := c.Get("Authorization")
		const pfx = "Bearer "
		if h == "" {
			return problem.WriteFiberError(c, problem.New(problem.CodeMissingCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is required."))
		}
		if !strings.HasPrefix(h, pfx) {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is invalid."))
		}
		token := strings.TrimSpace(h[len(pfx):])
		if token == "" {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is invalid."))
		}
		if devapi.HasV1KeyPrefix(token) {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"v1 application keys are not accepted on /v2; mint a v2 key (nmk_) in the developer portal."))
		}
		if devapi.IsV2KeyPrefix(token) {
			return catalogAppKeyAuth(c, path, scope, token, lookup)
		}
		if lookupUser != nil && slices.Contains(schemes, securityUserToken) {
			return catalogUserTokenAuth(c, scope, token, lookupUser)
		}
		return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
			"Authorization Bearer token is invalid."))
	}
}

func catalogAppKeyAuth(c fiber.Ctx, path, scope, token string, lookup func(context.Context, string) (*devapi.Credential, error)) error {
	// This arm used to `return c.Next()`: with no credential store wired,
	// every gated /v2 face answered anonymously. A missing dependency is a
	// server fault, never a grant.
	if lookup == nil {
		return problem.WriteFiberError(c, problem.New(problem.CodeServiceUnavailable, problem.RequestID(c), problem.Instance(c),
			"credential store is unavailable."))
	}
	if !devapi.ValidV2Key(token) {
		return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
			"Authorization Bearer token is invalid."))
	}
	cred, err := lookup(c.Context(), token)
	if err != nil {
		return problem.WriteFiberError(c, problem.New(problem.CodeServiceUnavailable, problem.RequestID(c), problem.Instance(c),
			"credential store is unavailable."))
	}
	if cred == nil {
		return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
			"Authorization Bearer token is invalid."))
	}
	if !cred.HasScope(scope) {
		return problem.WriteFiberError(c, problem.New(problem.CodeScopeRequired, problem.RequestID(c), problem.Instance(c),
			"this operation requires the "+scope+" scope."))
	}
	if path == claimEventsPath && !cred.HasScope(devapi.ScopeClaimEventsRead) {
		return problem.WriteFiberError(c, problem.New(problem.CodeScopeRequired, problem.RequestID(c), problem.Instance(c),
			"this operation additionally requires the claim_events:read scope."))
	}
	devapi.WithCredential(c, cred)
	c.Locals(ctxCatalogAuthz, catalogAuthz{
		ClientID:       cred.ClientID,
		ModerationRead: cred.HasScope(devapi.ScopeClaimEventsRead),
	})
	return c.Next()
}

// No catalogAuthz is set: the moderation claim states are an application's own
// per-site queue, and a person's token carries no site to fence them against.
func catalogUserTokenAuth(c fiber.Ctx, scope, token string, lookupUser func(context.Context, string) (UserIdentity, error)) error {
	ident, err := lookupUser(c.Context(), token)
	if err != nil || ident.UID <= 0 {
		return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
			"Authorization Bearer token is invalid."))
	}
	if !slices.Contains(ident.Scopes, scope) {
		return problem.WriteFiberError(c, problem.New(problem.CodeScopeRequired, problem.RequestID(c), problem.Instance(c),
			"this operation requires the "+scope+" scope."))
	}
	applyUserIdentity(c, ident)
	return c.Next()
}
