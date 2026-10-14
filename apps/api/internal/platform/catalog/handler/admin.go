package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type AdminServer struct {
	queues    *service.AdminQueueService
	merge     *service.MergeService
	claims    *service.ClaimLifecycleService
	imageRefs *service.ImageReferenceService
}

func SetupAdmin(app *fiber.App, queues *service.AdminQueueService, merge *service.MergeService, claims *service.ClaimLifecycleService, imageRefs *service.ImageReferenceService) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Catalog Admin API", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(AdminBridge)

	s := &AdminServer{queues: queues, merge: merge, claims: claims, imageRefs: imageRefs}
	s.register(api)
	s.registerClaims(api)
	return api
}

func (s *AdminServer) register(api huma.API) {
	tags := []string{"catalog-admin"}
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogCandidates", Method: http.MethodGet, Path: "/api/v1/admin/catalog/candidates",
		Summary: "List match candidates (the ambiguity bucket)", Tags: tags,
	}, s.listCandidates)
	huma.Register(api, huma.Operation{
		OperationID: "catalogQueueSummary", Method: http.MethodGet, Path: "/api/v1/admin/catalog/candidates/summary",
		Summary: "Queue depth: candidates grouped by entity type × status, probable refs grouped by entity type", Tags: tags,
	}, s.queueSummary)
	huma.Register(api, huma.Operation{
		OperationID: "decideCatalogCandidate", Method: http.MethodPost, Path: "/api/v1/admin/catalog/candidates/decide",
		Summary: "Decide a match candidate: accept (credit_name → link a person; else opens a merge proposal), reject (kept forever) or defer", Tags: tags,
	}, s.decideCandidate)
	huma.Register(api, huma.Operation{
		OperationID: "releaseCatalogWork", Method: http.MethodPost, Path: "/api/v1/admin/catalog/works/release",
		Summary: "Release a quarantined work to live (the reject-without-merge exit of the mint gate)", Tags: tags,
	}, s.releaseWork)
	huma.Register(api, huma.Operation{
		OperationID: "detachCatalogName", Method: http.MethodPost, Path: "/api/v1/admin/catalog/names/detach",
		Summary: "Detach a credit name from its person (reverses a person link; removes the person if it empties)", Tags: tags,
	}, s.detachName)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogProposals", Method: http.MethodGet, Path: "/api/v1/admin/catalog/proposals",
		Summary: "List merge proposals", Tags: tags,
	}, s.listProposals)
	huma.Register(api, huma.Operation{
		OperationID: "actOnCatalogProposal", Method: http.MethodPost, Path: "/api/v1/admin/catalog/proposals/{id}/{action}",
		Summary: "Approve / reject / withdraw / execute a merge proposal (execute enforces the 48h cooling-off)", Tags: tags,
	}, s.actOnProposal)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogProbableRefs", Method: http.MethodGet, Path: "/api/v1/admin/catalog/refs/probable",
		Summary: "List unconfirmed probable external refs (the confirmation bucket; includes merge-demoted exacts)", Tags: tags,
	}, s.listProbableRefs)
	huma.Register(api, huma.Operation{
		OperationID: "confirmCatalogRef", Method: http.MethodPost, Path: "/api/v1/admin/catalog/refs/confirm",
		Summary: "Promote a probable external ref to exact (409 when the exact slot is taken)", Tags: tags,
	}, s.confirmRef)
	huma.Register(api, huma.Operation{
		OperationID: "rejectCatalogRef", Method: http.MethodPost, Path: "/api/v1/admin/catalog/refs/reject",
		Summary: "Reject a wrong external ref: deletes it and records first-class negative knowledge (reason required)", Tags: tags,
	}, s.rejectRef)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogImageReferences", Method: http.MethodGet, Path: "/api/v1/admin/catalog/image-references",
		Summary: "List the catalog rows that reference an image hash (the pre-delete check)", Tags: tags,
	}, s.listImageReferences)
	huma.Register(api, huma.Operation{
		OperationID: "detachCatalogImageReferences", Method: http.MethodPost, Path: "/api/v1/admin/catalog/image-references/detach",
		Summary: "Release every catalog reference to an image hash (run before deleting the bytes)", Tags: tags,
	}, s.detachImageReferences)
}

type imageReferencesInput struct {
	Hash string `query:"hash" doc:"The image's sha-256 (64 hex characters)"`
}

type imageReferenceItem struct {
	Kind     string `json:"kind" doc:"work_cover / work_screenshot / character_bust / character_bust_source / character_figure / character_figure_source / label_logo / person_photo"`
	EntityID int64  `json:"entity_id" doc:"The owning work / character / label / person"`
	Label    string `json:"label" doc:"The owning entity's display name"`
}

type imageReferencesData struct {
	Items []imageReferenceItem `json:"items"`
	Total int                  `json:"total"`
}

type imageReferencesOutput struct {
	Body Envelope[imageReferencesData]
}

func validImageHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *AdminServer) listImageReferences(ctx context.Context, in *imageReferencesInput) (*imageReferencesOutput, error) {
	if !validImageHash(in.Hash) {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, "hash must be 64 lowercase hex characters")
	}
	refs, err := s.imageRefs.List(ctx, in.Hash)
	if err != nil {
		slog.Error("catalog admin list image references", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	items := make([]imageReferenceItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, imageReferenceItem{Kind: r.Kind, EntityID: r.EntityID, Label: r.Label})
	}
	return &imageReferencesOutput{Body: okEnvelope(imageReferencesData{Items: items, Total: len(items)})}, nil
}

type detachImageReferencesInput struct {
	Body struct {
		Hash string `json:"hash" doc:"The image's sha-256 (64 hex characters)"`
	}
}

type detachImageReferencesData struct {
	Removed      map[string]int64 `json:"removed" doc:"Rows released per kind"`
	TotalRemoved int64            `json:"total_removed"`
}

type detachImageReferencesOutput struct {
	Body Envelope[detachImageReferencesData]
}

func (s *AdminServer) detachImageReferences(ctx context.Context, in *detachImageReferencesInput) (*detachImageReferencesOutput, error) {
	if !validImageHash(in.Body.Hash) {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, "hash must be 64 lowercase hex characters")
	}
	removed, err := s.imageRefs.Detach(ctx, in.Body.Hash)
	if err != nil {
		slog.Error("catalog admin detach image references", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	var total int64
	for _, n := range removed {
		total += n
	}
	return &detachImageReferencesOutput{Body: okEnvelope(detachImageReferencesData{Removed: removed, TotalRemoved: total})}, nil
}

type candidatesInput struct {
	Status     int16 `query:"status" default:"-1" doc:"0=pending 1=accepted 2=rejected 3=deferred; -1 = all"`
	EntityType int16 `query:"entity_type" default:"-1" doc:"-1 = all"`
	Reason     int16 `query:"reason" default:"-1" doc:"0=shared_external_id 1=name_norm_equal 2=name_fuzzy 3=importer_suggest 4=llm_suggest; -1 = all"`
	Page       int   `query:"page" doc:"1-based page number"`
	Limit      int   `query:"limit" doc:"Items per page (max 200)"`
}

type candidatesOutput struct {
	Body Envelope[dto.Page[service.CandidateItem]]
}

func (s *AdminServer) listCandidates(ctx context.Context, in *candidatesInput) (*candidatesOutput, error) {
	items, total, err := s.queues.ListCandidates(ctx, service.CandidateFilters{
		Status: optionalFilter(in.Status), EntityType: optionalFilter(in.EntityType),
		Reason: optionalFilter(in.Reason), Page: in.Page, Limit: in.Limit,
	})
	if err != nil {
		slog.Error("catalog admin list candidates", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &candidatesOutput{Body: okEnvelope(dto.Page[service.CandidateItem]{Items: items, Total: total})}, nil
}

type queueSummaryOutput struct {
	Body Envelope[service.QueueSummary]
}

func (s *AdminServer) queueSummary(ctx context.Context, _ *struct{}) (*queueSummaryOutput, error) {
	summary, err := s.queues.QueueSummary(ctx)
	if err != nil {
		slog.Error("catalog admin queue summary", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &queueSummaryOutput{Body: okEnvelope(summary)}, nil
}

type decideCandidateInput struct {
	Body struct {
		EntityType int16  `json:"entity_type"`
		AID        int64  `json:"a_id"`
		BID        int64  `json:"b_id"`
		Action     string `json:"action" enum:"accept,reject,defer"`
		SourceID   int64  `json:"source_id,omitempty" doc:"merge accept only (non-credit_name): the entity absorbed by the merge (must be a_id or b_id)"`
		TargetID   int64  `json:"target_id,omitempty" doc:"merge accept only (non-credit_name): the surviving entity"`
		Note       string `json:"note,omitempty" doc:"merge accept only: carried onto the opened proposal"`
	}
}

type decideCandidateData struct {
	Decided       bool    `json:"decided"`
	ProposalID    *int64  `json:"proposal_id,omitempty" doc:"Set when a merge-candidate accept opened a proposal"`
	PersonID      *int64  `json:"person_id,omitempty"`
	PersonCreated bool    `json:"person_created,omitempty"`
	NeedsManual   bool    `json:"needs_manual,omitempty"`
	Released      []int64 `json:"released,omitempty"`
}

type decideCandidateOutput struct {
	Body Envelope[decideCandidateData]
}

func (s *AdminServer) decideCandidate(ctx context.Context, in *decideCandidateInput) (*decideCandidateOutput, error) {
	outcome, err := s.queues.DecideCandidate(ctx, service.CandidateDecision{
		EntityType: in.Body.EntityType, AID: in.Body.AID, BID: in.Body.BID,
		Action: in.Body.Action, SourceID: in.Body.SourceID, TargetID: in.Body.TargetID,
		Note: in.Body.Note, DecidedBy: adminIDFromCtx(ctx),
	})
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		case stderrors.Is(err, service.ErrDuplicateOpenProposal),
			stderrors.Is(err, service.ErrSameEntity),
			stderrors.Is(err, service.ErrEntityNotLive),
			stderrors.Is(err, service.ErrHiddenSurvivor),
			stderrors.Is(err, service.ErrProposalState):
			return nil, apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
		}
		slog.Error("catalog admin decide candidate", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	data := decideCandidateData{Decided: true, Released: outcome.Released}
	if outcome.Proposal != nil {
		data.ProposalID = &outcome.Proposal.ID
	}
	if outcome.Link != nil {
		data.NeedsManual = outcome.Link.NeedsManual
		data.PersonCreated = outcome.Link.Created
		if !outcome.Link.NeedsManual {
			data.PersonID = &outcome.Link.PersonID
		}
	}
	return &decideCandidateOutput{Body: okEnvelope(data)}, nil
}

type detachNameInput struct {
	Body struct {
		CreditNameID int64 `json:"credit_name_id" minimum:"1" doc:"The credit name to detach from its person (person_id → NULL); an emptied auto-linked person is removed"`
	}
}

type detachNameData struct {
	Detached bool `json:"detached"`
}

type detachNameOutput struct {
	Body Envelope[detachNameData]
}

func (s *AdminServer) detachName(ctx context.Context, in *detachNameInput) (*detachNameOutput, error) {
	actor := adminIDFromCtx(ctx)
	err := s.queues.DetachName(ctx, in.Body.CreditNameID, &actor)
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		case stderrors.Is(err, service.ErrProposalState):
			return nil, apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
		}
		slog.Error("catalog admin detach name", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &detachNameOutput{Body: okEnvelope(detachNameData{Detached: true})}, nil
}

type proposalsInput struct {
	Status     int16 `query:"status" default:"-1" doc:"0=open 1=approved 2=executed 3=rejected 4=withdrawn; -1 = all"`
	EntityType int16 `query:"entity_type" default:"-1" doc:"-1 = all"`
	Page       int   `query:"page"`
	Limit      int   `query:"limit"`
}

type proposalsOutput struct {
	Body Envelope[dto.Page[service.ProposalItem]]
}

func (s *AdminServer) listProposals(ctx context.Context, in *proposalsInput) (*proposalsOutput, error) {
	items, total, err := s.queues.ListProposals(ctx, service.ProposalFilters{
		Status: optionalFilter(in.Status), EntityType: optionalFilter(in.EntityType),
		Page: in.Page, Limit: in.Limit,
	})
	if err != nil {
		slog.Error("catalog admin list proposals", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &proposalsOutput{Body: okEnvelope(dto.Page[service.ProposalItem]{Items: items, Total: total})}, nil
}

type proposalActionInput struct {
	ID     int64  `path:"id"`
	Action string `path:"action" enum:"approve,reject,withdraw,execute"`
	Body   struct {
		Reason string `json:"reason,omitempty" doc:"reject only"`
	}
}

type proposalActionData struct {
	ID           int64      `json:"id"`
	Action       string     `json:"action"`
	ExecuteAfter *time.Time `json:"execute_after,omitempty"`
}

type proposalActionOutput struct {
	Body Envelope[proposalActionData]
}

func (s *AdminServer) actOnProposal(ctx context.Context, in *proposalActionInput) (*proposalActionOutput, error) {
	actor := adminIDFromCtx(ctx)
	var err error
	switch in.Action {
	case "approve":
		err = s.merge.ApproveMerge(ctx, in.ID, actor)
	case "reject":
		err = s.merge.RejectMerge(ctx, in.ID, actor, in.Body.Reason)
	case "withdraw":
		err = s.merge.WithdrawMerge(ctx, in.ID, actor)
	case "execute":
		err = s.merge.ExecuteMerge(ctx, in.ID, &actor)
	}
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		case stderrors.Is(err, service.ErrCoolingOff):
			if p, perr := s.queues.GetProposal(ctx, in.ID); perr == nil && p != nil {
				return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrOperationFailed,
					coolingOffMessage(p.ExecuteAfter))
			}
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrOperationFailed, err.Error())
		case stderrors.Is(err, service.ErrProposalState), stderrors.Is(err, service.ErrEntityNotLive),
			stderrors.Is(err, service.ErrAbsorbVNDBAnchor), stderrors.Is(err, service.ErrMergeMode),
			stderrors.Is(err, service.ErrHiddenSurvivor):
			return nil, apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
		}
		slog.Error("catalog admin proposal action", "action", in.Action, "id", in.ID, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	data := proposalActionData{ID: in.ID, Action: in.Action}
	if in.Action == "approve" {
		if p, perr := s.queues.GetProposal(ctx, in.ID); perr == nil && p != nil {
			data.ExecuteAfter = p.ExecuteAfter
		}
	}
	return &proposalActionOutput{Body: okEnvelope(data)}, nil
}

func optionalFilter(v int16) *int16 {
	if v < 0 {
		return nil
	}
	return &v
}

func coolingOffMessage(after *time.Time) string {
	if after == nil {
		return "cooling-off window has not elapsed"
	}
	return "cooling-off window has not elapsed; executable after " + after.Format(time.RFC3339)
}

type probableRefsInput struct {
	SourceID   int16 `query:"source_id" default:"-1" doc:"-1 = all"`
	EntityType int16 `query:"entity_type" default:"-1" doc:"-1 = all"`
	Page       int   `query:"page"`
	Limit      int   `query:"limit"`
}

type probableRefsOutput struct {
	Body Envelope[dto.Page[service.ProbableRefItem]]
}

func (s *AdminServer) listProbableRefs(ctx context.Context, in *probableRefsInput) (*probableRefsOutput, error) {
	items, total, err := s.queues.ListProbableRefs(ctx, service.RefFilters{
		SourceID: optionalFilter(in.SourceID), EntityType: optionalFilter(in.EntityType),
		Page: in.Page, Limit: in.Limit,
	})
	if err != nil {
		slog.Error("catalog admin list probable refs", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &probableRefsOutput{Body: okEnvelope(dto.Page[service.ProbableRefItem]{Items: items, Total: total})}, nil
}

type refKeyBody struct {
	EntityType int16  `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	SourceID   int16  `json:"source_id"`
	ExternalID string `json:"external_id" minLength:"1"`
}

type confirmRefInput struct {
	Body refKeyBody
}

type refActionData struct {
	Done bool `json:"done"`
}

type refActionOutput struct {
	Body Envelope[refActionData]
}

func (s *AdminServer) confirmRef(ctx context.Context, in *confirmRefInput) (*refActionOutput, error) {
	err := s.queues.ConfirmRef(ctx, service.RefKey{
		EntityType: in.Body.EntityType, EntityID: in.Body.EntityID,
		SourceID: in.Body.SourceID, ExternalID: in.Body.ExternalID,
	}, adminIDFromCtx(ctx))
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		case stderrors.Is(err, service.ErrExactTaken):
			return nil, apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
		case stderrors.Is(err, service.ErrProposalState):
			return nil, apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, err.Error())
		}
		slog.Error("catalog admin confirm ref", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &refActionOutput{Body: okEnvelope(refActionData{Done: true})}, nil
}

// Fields spelled out (not an embedded refKeyBody): Huma's schema reflection
// does not flatten anonymous embedded structs, which silently produced a
// body schema with ONLY `reason` and rejected every key field as an
// unexpected property (caught live in the step-07 smoke).
type rejectRefInput struct {
	Body struct {
		EntityType int16  `json:"entity_type"`
		EntityID   int64  `json:"entity_id"`
		SourceID   int16  `json:"source_id"`
		ExternalID string `json:"external_id" minLength:"1"`
		Reason     string `json:"reason" minLength:"1" doc:"Why the pairing is wrong — recorded as permanent negative knowledge"`
	}
}

func (s *AdminServer) rejectRef(ctx context.Context, in *rejectRefInput) (*refActionOutput, error) {
	err := s.queues.RejectRef(ctx, service.RefKey{
		EntityType: in.Body.EntityType, EntityID: in.Body.EntityID,
		SourceID: in.Body.SourceID, ExternalID: in.Body.ExternalID,
	}, in.Body.Reason, adminIDFromCtx(ctx))
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		case stderrors.Is(err, service.ErrProposalState):
			return nil, apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, err.Error())
		}
		slog.Error("catalog admin reject ref", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &refActionOutput{Body: okEnvelope(refActionData{Done: true})}, nil
}
