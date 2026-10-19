package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"
	"time"

	"api/internal/platform/editing"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

const AdminEditPolicyPrefix = "/api/v1/admin/catalog/edit-policy"

type editPolicyServer struct {
	store *editing.PolicyStore
}

type policyRules struct {
	Propose     string `json:"propose" doc:"open, trusted, locked, or perm:<catalog permission key>"`
	Review      string `json:"review" doc:"perm:<catalog permission key>"`
	Automerge   string `json:"automerge" doc:"never, trusted, always, owner, review, or trusted_or_review"`
	OwnerReview bool   `json:"owner_review" doc:"Whether the entity's owner may review proposals on it, with no permission."`
}

type policyOverrideBody struct {
	EntityType  string  `json:"entity_type" minLength:"1" maxLength:"64" doc:"Editing-engine type, e.g. catalog.work."`
	FieldKey    string  `json:"field_key,omitempty" maxLength:"128" doc:"Empty for the whole type; a field key overrides that field only."`
	Propose     *string `json:"propose,omitempty" doc:"Omitted inherits from below."`
	Review      *string `json:"review,omitempty" doc:"Omitted inherits from below."`
	Automerge   *string `json:"automerge,omitempty" doc:"Omitted inherits from below."`
	OwnerReview *bool   `json:"owner_review,omitempty" doc:"Omitted inherits from below."`
}

type policyMatrixRow struct {
	EntityType string              `json:"entity_type"`
	FieldKey   string              `json:"field_key" doc:"Empty on the type-level row, whose effective rules every field without an override of its own inherits."`
	Default    policyRules         `json:"default" doc:"The rules the code ships."`
	Override   *policyOverrideBody `json:"override" doc:"The runtime override on exactly this row, or null."`
	Effective  policyRules         `json:"effective" doc:"What the engine applies now: default, then the type-level override, then this row's."`
}

type policyMatrixData struct {
	Rows           []policyMatrixRow `json:"rows"`
	ProposeModes   []string          `json:"propose_modes"`
	AutomergeModes []string          `json:"automerge_modes"`
}

type getEditPolicyOutput struct {
	Body Envelope[policyMatrixData]
}

type putEditPolicyInput struct {
	Body policyOverrideBody
}

type putEditPolicyData struct {
	Override  policyOverrideBody `json:"override"`
	UpdatedBy int64              `json:"updated_by"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type putEditPolicyOutput struct {
	Body Envelope[putEditPolicyData]
}

type deleteEditPolicyInput struct {
	EntityType string `query:"entity_type" minLength:"1" maxLength:"64" required:"true"`
	FieldKey   string `query:"field_key" maxLength:"128"`
}

type deleteEditPolicyOutput struct {
	Body Envelope[struct{}]
}

type listEditPolicyAuditInput struct {
	Limit    int   `query:"limit" minimum:"0" maximum:"200" doc:"Items per page (max 200, default 50)"`
	BeforeID int64 `query:"before_id" minimum:"0" doc:"Cursor: entries with a smaller id"`
}

type editPolicyAuditEntry struct {
	ID         int64               `json:"id"`
	EntityType string              `json:"entity_type"`
	FieldKey   string              `json:"field_key"`
	Before     *policyOverrideBody `json:"before" doc:"The override before the change; null when there was none."`
	After      *policyOverrideBody `json:"after" doc:"The override after the change; null when it was removed."`
	ActorUID   int64               `json:"actor_uid"`
	CreatedAt  time.Time           `json:"created_at"`
}

type listEditPolicyAuditOutput struct {
	Body Envelope[[]editPolicyAuditEntry]
}

// RegisterEditPolicy mounts the runtime editing-policy console on the catalog
// admin face. A nil store still registers the routes, so the exported spec
// carries them, and answers 503.
func RegisterEditPolicy(api huma.API, store *editing.PolicyStore) {
	s := &editPolicyServer{store: store}
	tags := []string{"catalog-admin"}
	huma.Register(api, huma.Operation{
		OperationID: "getCatalogEditPolicy", Method: http.MethodGet, Path: AdminEditPolicyPrefix,
		Summary: "Effective editing policy: code default, runtime override and effective rules per entity type and field", Tags: tags,
	}, s.matrix)
	huma.Register(api, huma.Operation{
		OperationID: "putCatalogEditPolicyOverride", Method: http.MethodPut, Path: AdminEditPolicyPrefix + "/overrides",
		Summary: "Set the override on one entity type or field (replaces it whole; applies without a restart)", Tags: tags,
	}, s.put)
	huma.Register(api, huma.Operation{
		OperationID: "deleteCatalogEditPolicyOverride", Method: http.MethodDelete, Path: AdminEditPolicyPrefix + "/overrides",
		Summary: "Remove the override on one entity type or field", Tags: tags,
	}, s.delete)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogEditPolicyAudit", Method: http.MethodGet, Path: AdminEditPolicyPrefix + "/audit",
		Summary: "Who changed which override, when, and from what to what (newest first)", Tags: tags,
	}, s.audit)
}

func (s *editPolicyServer) unbound() error {
	if s.store == nil {
		return apiErrMsg(http.StatusServiceUnavailable, errors.ErrOperationFailed, "the editing policy store is not bound")
	}
	return nil
}

func rulesOf(p editing.Policy) policyRules {
	return policyRules{Propose: p.Propose, Review: p.Review, Automerge: p.Automerge, OwnerReview: p.OwnerReview}
}

func bodyOf(o editing.PolicyOverride) policyOverrideBody {
	return policyOverrideBody{
		EntityType: o.EntityType, FieldKey: o.FieldKey,
		Propose: o.Propose, Review: o.Review, Automerge: o.Automerge, OwnerReview: o.OwnerReview,
	}
}

func (s *editPolicyServer) matrix(ctx context.Context, _ *struct{}) (*getEditPolicyOutput, error) {
	if err := s.unbound(); err != nil {
		return nil, err
	}
	rows := s.store.Registry().PolicyMatrix()
	out := policyMatrixData{
		Rows: make([]policyMatrixRow, 0, len(rows)),
		ProposeModes: []string{
			editing.ProposeOpen, editing.ProposeTrusted, editing.ProposeLocked, "perm:<key>",
		},
		AutomergeModes: []string{
			editing.AutomergeNever, editing.AutomergeTrusted, editing.AutomergeAlways,
			editing.AutomergeOwner, editing.AutomergeReview, editing.AutomergeTrustedOrReview,
		},
	}
	for _, r := range rows {
		row := policyMatrixRow{
			EntityType: r.EntityType, FieldKey: r.FieldKey,
			Default: rulesOf(r.Default), Effective: rulesOf(r.Effective),
		}
		if r.Override != nil {
			b := bodyOf(*r.Override)
			row.Override = &b
		}
		out.Rows = append(out.Rows, row)
	}
	return &getEditPolicyOutput{Body: okEnvelope(out)}, nil
}

func (s *editPolicyServer) put(ctx context.Context, in *putEditPolicyInput) (*putEditPolicyOutput, error) {
	if err := s.unbound(); err != nil {
		return nil, err
	}
	b := in.Body
	row, err := s.store.Put(ctx, adminIDFromCtx(ctx), editing.PolicyOverride{
		EntityType: b.EntityType, FieldKey: b.FieldKey,
		Propose: b.Propose, Review: b.Review, Automerge: b.Automerge, OwnerReview: b.OwnerReview,
	})
	if err != nil {
		var verr *editing.ValidationError
		if stderrors.As(err, &verr) {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, verr.Reason)
		}
		slog.Error("catalog admin edit policy put", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &putEditPolicyOutput{Body: okEnvelope(putEditPolicyData{
		Override: policyOverrideBody{
			EntityType: row.EntityType, FieldKey: row.FieldKey,
			Propose: row.Propose, Review: row.Review, Automerge: row.Automerge, OwnerReview: row.OwnerReview,
		},
		UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt,
	})}, nil
}

func (s *editPolicyServer) delete(ctx context.Context, in *deleteEditPolicyInput) (*deleteEditPolicyOutput, error) {
	if err := s.unbound(); err != nil {
		return nil, err
	}
	if err := s.store.Delete(ctx, adminIDFromCtx(ctx), in.EntityType, in.FieldKey); err != nil {
		if stderrors.Is(err, editing.ErrOverrideNotFound) {
			return nil, apiErr(http.StatusNotFound, errors.ErrNotFound)
		}
		slog.Error("catalog admin edit policy delete", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	return &deleteEditPolicyOutput{Body: okEnvelope(struct{}{})}, nil
}

func (s *editPolicyServer) audit(ctx context.Context, in *listEditPolicyAuditInput) (*listEditPolicyAuditOutput, error) {
	if err := s.unbound(); err != nil {
		return nil, err
	}
	rows, err := s.store.Audit(ctx, in.Limit, in.BeforeID)
	if err != nil {
		slog.Error("catalog admin edit policy audit", "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
	out := make([]editPolicyAuditEntry, 0, len(rows))
	for _, r := range rows {
		e := editPolicyAuditEntry{
			ID: r.ID, EntityType: r.EntityType, FieldKey: r.FieldKey, ActorUID: r.ActorUID, CreatedAt: r.CreatedAt,
		}
		e.Before = auditBody(r.Before)
		e.After = auditBody(r.After)
		out = append(out, e)
	}
	return &listEditPolicyAuditOutput{Body: okEnvelope(out)}, nil
}

func auditBody(raw []byte) *policyOverrideBody {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	o, err := editing.DecodePolicyOverride(raw)
	if err != nil {
		return nil
	}
	b := bodyOf(o)
	return &b
}
