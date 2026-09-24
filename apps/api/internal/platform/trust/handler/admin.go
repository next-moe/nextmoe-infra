package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/trust/dto"
	trustPerm "api/internal/platform/trust/perm"
	"api/internal/platform/trust/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type AdminServer struct {
	review       *service.ReviewService
	registry     *service.RegistryService
	dispositions *service.DispositionService
	terms        *service.TermService
	policies     *service.PolicyService
	clients      clientSiteLookup
}

func SetupAdmin(app *fiber.App, review *service.ReviewService, registry *service.RegistryService, dispositions *service.DispositionService, terms *service.TermService, policies *service.PolicyService, clients clientSiteLookup) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Trust Admin API", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(AdminBridge)

	s := &AdminServer{review: review, registry: registry, dispositions: dispositions, terms: terms, policies: policies, clients: clients}
	s.register(api)
	return api
}

func (s *AdminServer) register(api huma.API) {
	tags := []string{"trust-admin"}

	const reviewScopeNote = "Site-scoped: platform staff act across all sites; a site-granted moderator is confined to their own site."
	const foreignNotFound = " A site-scoped moderator acting on another site's item receives 404 (existence is not leaked)."
	const platformOnly = "Platform-staff only: a site-scoped moderator receives 403 (the registries and dead-letter surfaces are platform-ops)."

	huma.Register(api, huma.Operation{OperationID: "listTrustReviewItems", Method: http.MethodGet, Path: "/api/v1/admin/trust/review-items",
		Summary: "List the review inbox (per-site view, priority-ordered)", Tags: tags,
		Description: reviewScopeNote + " For a site-scoped caller the site filter is forced to their own site, overriding any requested `site`."}, s.listReviewItems)
	huma.Register(api, huma.Operation{OperationID: "getTrustReviewItem", Method: http.MethodGet, Path: "/api/v1/admin/trust/review-items/{id}",
		Summary: "Get a review item with its associated reports", Tags: tags,
		Description: reviewScopeNote + foreignNotFound}, s.getReviewItem)
	huma.Register(api, huma.Operation{OperationID: "claimTrustReviewItem", Method: http.MethodPost, Path: "/api/v1/admin/trust/review-items/{id}/claim",
		Summary: "Claim a pending review item (FOR UPDATE SKIP LOCKED; 409 if taken)", Tags: tags,
		Description: reviewScopeNote + foreignNotFound}, s.claimReviewItem)
	huma.Register(api, huma.Operation{OperationID: "decideTrustReviewItem", Method: http.MethodPost, Path: "/api/v1/admin/trust/review-items/{id}/decide",
		Summary: "Decide a review item (dismissed / actioned; actioned writes a disposition)", Tags: tags,
		Description: reviewScopeNote + foreignNotFound}, s.decideReviewItem)

	huma.Register(api, huma.Operation{OperationID: "listTrustSubjectKinds", Method: http.MethodGet, Path: "/api/v1/admin/trust/subject-kinds",
		Summary: "List subject-kind registry rows (optionally per-site; includes deprecated)", Tags: tags,
		Description: platformOnly}, s.listSubjectKinds)
	huma.Register(api, huma.Operation{OperationID: "createTrustSubjectKind", Method: http.MethodPost, Path: "/api/v1/admin/trust/subject-kinds",
		Summary: "Register a subject kind for a site", Tags: tags,
		Description: platformOnly}, s.createSubjectKind)
	huma.Register(api, huma.Operation{OperationID: "batchTrustSubjectKinds", Method: http.MethodPost, Path: "/api/v1/admin/trust/subject-kinds/batch",
		Summary: "Batch-register subject kinds for a site (declarative convergence; never revives deprecated)", Tags: tags,
		Description: platformOnly}, s.batchSubjectKinds)
	huma.Register(api, huma.Operation{OperationID: "patchTrustSubjectKind", Method: http.MethodPatch, Path: "/api/v1/admin/trust/subject-kinds/{id}",
		Summary: "Update a subject kind's callback config / deprecation (never deleted)", Tags: tags,
		Description: platformOnly}, s.patchSubjectKind)

	huma.Register(api, huma.Operation{OperationID: "listTrustReportReasons", Method: http.MethodGet, Path: "/api/v1/admin/trust/report-reasons",
		Summary: "List the reason taxonomy (global base + per-site extensions)", Tags: tags,
		Description: platformOnly}, s.listReasons)
	huma.Register(api, huma.Operation{OperationID: "createTrustReportReason", Method: http.MethodPost, Path: "/api/v1/admin/trust/report-reasons",
		Summary: "Register a reason (global or per-site)", Tags: tags,
		Description: platformOnly}, s.createReason)
	huma.Register(api, huma.Operation{OperationID: "patchTrustReportReason", Method: http.MethodPatch, Path: "/api/v1/admin/trust/report-reasons/{id}",
		Summary: "Update a reason's label / severity / deprecation (never deleted)", Tags: tags,
		Description: platformOnly}, s.patchReason)

	huma.Register(api, huma.Operation{OperationID: "listTrustDispositions", Method: http.MethodGet, Path: "/api/v1/admin/trust/dispositions",
		Summary: "List dispositions by callback status (e.g. dead_letter)", Tags: tags,
		Description: platformOnly}, s.listDispositions)
	huma.Register(api, huma.Operation{OperationID: "redeliverTrustDisposition", Method: http.MethodPost, Path: "/api/v1/admin/trust/dispositions/{id}/redeliver",
		Summary: "Replay a dead-lettered callback (reset to pending)", Tags: tags,
		Description: platformOnly}, s.redeliverDisposition)

	s.registerTerms(api)
	s.registerPolicies(api)
}

type adminScope struct{ site string }

func (sc adminScope) siteScoped() bool { return sc.site != "" }

func (s *AdminServer) resolveScope(ctx context.Context) (adminScope, *houseError) {
	if trustPerm.Resolver.Can(globalRolesFromCtx(ctx), trustPerm.QueueAccess) {
		return adminScope{}, nil
	}
	clientID := tokenClientIDFromCtx(ctx)
	if clientID == "" {
		return adminScope{}, apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"site-scoped moderator token is not bound to a client")
	}
	client, err := s.clients.FindByClientID(ctx, clientID)
	if err != nil || client == nil || client.CommunityTenant() == "" {
		return adminScope{}, apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"site-scoped moderator's client is not bound to a site")
	}
	return adminScope{site: client.CommunityTenant()}, nil
}

func (s *AdminServer) requireUnrestricted(ctx context.Context) *houseError {
	scope, he := s.resolveScope(ctx)
	if he != nil {
		return he
	}
	if scope.siteScoped() {
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"this surface is restricted to platform staff")
	}
	return nil
}

func (s *AdminServer) requireTermManage(ctx context.Context) *houseError {
	if trustPerm.Resolver.Can(globalRolesFromCtx(ctx), trustPerm.TermManage) {
		return nil
	}
	return apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
		"managing Tier0 terms requires trust.term_manage (admin/ren)")
}

type listReviewItemsInput struct {
	Site   string `query:"site" doc:"filter to one site; empty = all sites"`
	Status int16  `query:"status" default:"-1" doc:"0=pending 1=claimed 2=actioned 3=dismissed; -1 = all"`
	Source int16  `query:"source" default:"-1" doc:"0=reports 1=ai_text 2=ai_image 3=community_forward 4=mislabel 5=manual; -1 = all"`
	Page   int    `query:"page" doc:"1-based page number"`
	Limit  int    `query:"limit" doc:"items per page (max 200)"`
}
type reviewItemsOutput struct {
	Body Envelope[dto.Page[dto.ReviewItemView]]
}

func (s *AdminServer) listReviewItems(ctx context.Context, in *listReviewItemsInput) (*reviewItemsOutput, error) {
	scope, he := s.resolveScope(ctx)
	if he != nil {
		return nil, he
	}
	site := in.Site
	if scope.siteScoped() {
		site = scope.site
	}
	items, total, err := s.review.List(ctx, service.ReviewFilters{
		Site: site, Status: optionalFilter(in.Status), Source: optionalFilter(in.Source),
		Page: in.Page, Limit: in.Limit,
	})
	if err != nil {
		return nil, mapAdminErr("list review items", err)
	}
	return &reviewItemsOutput{Body: okEnvelope(dto.Page[dto.ReviewItemView]{
		Items: toReviewItemViews(items), Total: total,
	})}, nil
}

type reviewItemIDInput struct {
	ID int64 `path:"id"`
}
type reviewItemDetailOutput struct {
	Body Envelope[dto.ReviewItemDetail]
}

func (s *AdminServer) getReviewItem(ctx context.Context, in *reviewItemIDInput) (*reviewItemDetailOutput, error) {
	scope, he := s.resolveScope(ctx)
	if he != nil {
		return nil, he
	}
	item, reports, err := s.review.Get(ctx, in.ID, scope.site)
	if err != nil {
		return nil, mapAdminErr("get review item", err)
	}
	return &reviewItemDetailOutput{Body: okEnvelope(dto.ReviewItemDetail{
		Item: toReviewItemView(*item), Reports: toReportViews(reports),
	})}, nil
}

type okOutput struct {
	Body Envelope[dto.OKResponse]
}

func (s *AdminServer) claimReviewItem(ctx context.Context, in *reviewItemIDInput) (*okOutput, error) {
	scope, he := s.resolveScope(ctx)
	if he != nil {
		return nil, he
	}
	if err := s.review.Claim(ctx, in.ID, adminIDFromCtx(ctx), scope.site); err != nil {
		return nil, mapAdminErr("claim review item", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

type decideInput struct {
	ID   int64 `path:"id"`
	Body dto.DecideRequest
}
type decideOutput struct {
	Body Envelope[decideData]
}
type decideData struct {
	Decided       bool   `json:"decided"`
	DispositionID *int64 `json:"disposition_id,omitempty" doc:"set when an actioned decision wrote a disposition"`
}

func (s *AdminServer) decideReviewItem(ctx context.Context, in *decideInput) (*decideOutput, error) {
	scope, he := s.resolveScope(ctx)
	if he != nil {
		return nil, he
	}
	dispID, err := s.review.Decide(ctx, service.DecideParams{
		ID: in.ID, DecidedBy: adminIDFromCtx(ctx), Decision: in.Body.Decision,
		Action: in.Body.Action, ReasonCode: in.Body.ReasonCode, Statement: in.Body.Statement,
		SiteScope: scope.site,
	})
	if err != nil {
		return nil, mapAdminErr("decide review item", err)
	}
	return &decideOutput{Body: okEnvelope(decideData{Decided: true, DispositionID: dispID})}, nil
}

type listSubjectKindsAdminInput struct {
	Site string `query:"site" doc:"filter to one site; empty = all sites"`
}
type subjectKindsOutput struct {
	Body Envelope[[]dto.SubjectKindView]
}

func (s *AdminServer) listSubjectKinds(ctx context.Context, in *listSubjectKindsAdminInput) (*subjectKindsOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	kinds, err := s.registry.ListSubjectKinds(ctx, in.Site, true)
	if err != nil {
		return nil, mapAdminErr("list subject kinds", err)
	}
	return &subjectKindsOutput{Body: okEnvelope(toSubjectKindViews(kinds))}, nil
}

type createSubjectKindInput struct{ Body dto.CreateSubjectKindRequest }
type subjectKindOutput struct {
	Body Envelope[dto.SubjectKindView]
}

func (s *AdminServer) createSubjectKind(ctx context.Context, in *createSubjectKindInput) (*subjectKindOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	kind, err := s.registry.CreateSubjectKind(ctx, adminIDFromCtx(ctx),
		in.Body.Site, in.Body.Key, in.Body.CallbackURL, in.Body.CallbackSecret, boolOr(in.Body.NotifyOnDismiss, false))
	if err != nil {
		return nil, mapAdminErr("create subject kind", err)
	}
	return &subjectKindOutput{Body: okEnvelope(toSubjectKindView(*kind))}, nil
}

type batchSubjectKindsInput struct{ Body dto.BatchSubjectKindsRequest }
type batchSubjectKindsOutput struct {
	Body Envelope[dto.EnsureSubjectKindsResponse]
}

func (s *AdminServer) batchSubjectKinds(ctx context.Context, in *batchSubjectKindsInput) (*batchSubjectKindsOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	if in.Body.Site == "" {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed,
			"site is required")
	}
	if len(in.Body.Kinds) > maxEnsureKinds {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed,
			"at most 50 kinds per batch")
	}
	actorID := adminIDFromCtx(ctx)
	results, err := s.registry.EnsureSubjectKinds(ctx, &actorID, in.Body.Site, toEnsureItems(in.Body.Kinds))
	if err != nil {
		return nil, mapAdminErr("batch subject kinds", err)
	}
	return &batchSubjectKindsOutput{Body: okEnvelope(dto.EnsureSubjectKindsResponse{
		Results: toEnsureResultViews(results),
	})}, nil
}

type patchSubjectKindInput struct {
	ID   int64 `path:"id"`
	Body dto.PatchSubjectKindRequest
}

func (s *AdminServer) patchSubjectKind(ctx context.Context, in *patchSubjectKindInput) (*subjectKindOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	kind, err := s.registry.PatchSubjectKind(ctx, adminIDFromCtx(ctx), in.ID, service.SubjectKindPatch{
		CallbackURL: in.Body.CallbackURL, CallbackSecret: in.Body.CallbackSecret,
		IsDeprecated: in.Body.IsDeprecated, NotifyOnDismiss: in.Body.NotifyOnDismiss,
	})
	if err != nil {
		return nil, mapAdminErr("patch subject kind", err)
	}
	return &subjectKindOutput{Body: okEnvelope(toSubjectKindView(*kind))}, nil
}

type listReasonsInput struct {
	Site string `query:"site" doc:"include this site's extensions alongside the global base; empty = all"`
}
type reasonsOutput struct {
	Body Envelope[[]dto.ReasonView]
}

func (s *AdminServer) listReasons(ctx context.Context, in *listReasonsInput) (*reasonsOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	reasons, err := s.registry.ListReasons(ctx, in.Site)
	if err != nil {
		return nil, mapAdminErr("list reasons", err)
	}
	return &reasonsOutput{Body: okEnvelope(toReasonViews(reasons))}, nil
}

type createReasonInput struct{ Body dto.CreateReasonRequest }
type reasonOutput struct {
	Body Envelope[dto.ReasonView]
}

func (s *AdminServer) createReason(ctx context.Context, in *createReasonInput) (*reasonOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	reason, err := s.registry.CreateReason(ctx, adminIDFromCtx(ctx),
		in.Body.Key, in.Body.NameCN, in.Body.Site, in.Body.Severity)
	if err != nil {
		return nil, mapAdminErr("create reason", err)
	}
	return &reasonOutput{Body: okEnvelope(toReasonView(*reason))}, nil
}

type patchReasonInput struct {
	ID   int64 `path:"id"`
	Body dto.PatchReasonRequest
}

func (s *AdminServer) patchReason(ctx context.Context, in *patchReasonInput) (*reasonOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	reason, err := s.registry.PatchReason(ctx, adminIDFromCtx(ctx), in.ID, service.ReasonPatch{
		NameCN: in.Body.NameCN, Severity: in.Body.Severity, IsDeprecated: in.Body.IsDeprecated,
	})
	if err != nil {
		return nil, mapAdminErr("patch reason", err)
	}
	return &reasonOutput{Body: okEnvelope(toReasonView(*reason))}, nil
}

type listDispositionsInput struct {
	CallbackStatus int16 `query:"callback_status" default:"-1" doc:"0=pending 1=delivered 2=dead_letter; -1 = all"`
	Page           int   `query:"page"`
	Limit          int   `query:"limit"`
}
type dispositionsOutput struct {
	Body Envelope[dto.Page[dto.DispositionView]]
}

func (s *AdminServer) listDispositions(ctx context.Context, in *listDispositionsInput) (*dispositionsOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	items, total, err := s.dispositions.List(ctx, optionalFilter(in.CallbackStatus), in.Page, in.Limit)
	if err != nil {
		return nil, mapAdminErr("list dispositions", err)
	}
	return &dispositionsOutput{Body: okEnvelope(dto.Page[dto.DispositionView]{
		Items: toDispositionViews(items), Total: total,
	})}, nil
}

type dispositionIDInput struct {
	ID int64 `path:"id"`
}

func (s *AdminServer) redeliverDisposition(ctx context.Context, in *dispositionIDInput) (*okOutput, error) {
	if he := s.requireUnrestricted(ctx); he != nil {
		return nil, he
	}
	if err := s.dispositions.Redeliver(ctx, adminIDFromCtx(ctx), in.ID); err != nil {
		return nil, mapAdminErr("redeliver disposition", err)
	}
	return &okOutput{Body: okEnvelope(dto.OKResponse{OK: true})}, nil
}

func optionalFilter(v int16) *int16 {
	if v < 0 {
		return nil
	}
	return &v
}

func boolOr(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}

func mapAdminErr(op string, err error) *houseError {
	switch {
	case stderrors.Is(err, service.ErrReviewItemNotFound),
		stderrors.Is(err, service.ErrSubjectKindNotFound),
		stderrors.Is(err, service.ErrReasonNotFound),
		stderrors.Is(err, service.ErrTermNotFound),
		stderrors.Is(err, service.ErrDispositionNotFound):
		return apiErr(http.StatusNotFound, errors.ErrNotFound)
	case stderrors.Is(err, service.ErrAlreadyClaimed),
		stderrors.Is(err, service.ErrIllegalTransition),
		stderrors.Is(err, service.ErrSubjectKindExists),
		stderrors.Is(err, service.ErrTermExists),
		stderrors.Is(err, service.ErrNotDeadLetter):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
	case stderrors.Is(err, service.ErrInvalidDecision),
		stderrors.Is(err, service.ErrTermEmpty),
		stderrors.Is(err, service.ErrTermInvalidKind),
		stderrors.Is(err, service.ErrTermInvalidPurpose):
		return apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, err.Error())
	default:
		slog.Error("trust admin "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}
