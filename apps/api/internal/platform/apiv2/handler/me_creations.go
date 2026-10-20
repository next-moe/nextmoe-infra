package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/editing"

	"github.com/danielgtaylor/huma/v2"
)

type createCreationInput struct {
	Body struct {
		Object            string         `json:"object" enum:"company,character,release,engine,series,person,credit_name,tag,trait" doc:"Family of the new entry. Works are created through POST /v2/me/claims. Tags and traits take a reviewer-only create policy."`
		Params            map[string]any `json:"params,omitempty" doc:"Creation parameters; /v2/me/edit-capabilities/{object} creation.param_specs lists them (release takes work_id, person takes credit_name_id of an unattributed credit name)."`
		Patch             map[string]any `json:"patch,omitempty" doc:"Field key to initial value, the same keys and values POST /v2/me/proposals accepts for that family. creation.required_fields must be present."`
		Note              string         `json:"note,omitempty" maxLength:"2000" doc:"Proposer's own summary. Must not be used as a discriminant."`
		ConfirmDuplicates bool           `json:"confirm_duplicates,omitempty" doc:"Create even when entries with the same normalized name exist. Without it such a request is refused with 409 DUPLICATE_SUSPECTS naming them, and nothing is written."`
	}
}

func registerMeCreations(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "createMyCreation",
		Method:      http.MethodPost,
		Path:        "/v2/me/creations",
		Summary:     "Propose a new entry",
		Description: "Files a proposal that creates a new entry of one family. It is reviewed like any proposal (it appears in GET /v2/moderation/proposals with entity_id 0 and a creation block) unless the create policy lets the bearer apply it at once; once merged, entity_id is the new entry's id. " +
			"Entries whose normalized name already exists are refused with 409 DUPLICATE_SUSPECTS unless confirm_duplicates=true. Counts against the same proposal quota as POST /v2/me/proposals. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
			http.StatusServiceUnavailable),
		DefaultStatus:      http.StatusCreated,
		SkipValidateParams: true,
	}, createMyCreation(cat))
}

func createMyCreation(cat *Catalog) func(context.Context, *createCreationInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *createCreationInput) (*getProposalOutput, error) {
		if in == nil {
			in = &createCreationInput{}
		}
		rec, etag, err := cat.CreateCreation(ctx, in.Body.Object, in.Body.Params, in.Body.Patch, in.Body.Note, in.Body.ConfirmDuplicates)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func (c *Catalog) CreateCreation(ctx context.Context, object string, params, patch map[string]any, note string, confirm bool) (repr.ProposalRecord, string, error) {
	entityType := schemaEntityType(object)
	if entityType == "" {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeNotFound, "", "", "No schema for family "+object+".")
	}
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	quota, err := c.editQuota(ctx, actor.UserID)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	if perr := quota.filingProblem(); perr != nil {
		return repr.ProposalRecord{}, "", perr
	}
	if quota.demotesEditor() {
		actor.TrustTier = 0
	}
	if !confirm {
		hints, herr := c.Engine.BirthDuplicates(ctx, entityType, params, patch)
		if herr != nil {
			return repr.ProposalRecord{}, "", creationErr(herr)
		}
		if len(hints) > 0 {
			p := problem.New(problem.CodeDuplicateSuspects, "", "",
				"entries with the same name already exist; re-send with confirm_duplicates=true to create anyway.")
			for _, h := range hints {
				p.Suspects = append(p.Suspects, problem.Suspect{ID: repr.ID(h.ID), DisplayName: h.Name})
			}
			return repr.ProposalRecord{}, "", p
		}
	}
	prop, _, cerr := c.Engine.CreateBirth(ctx, editing.CreateBirthInput{
		EntityType: entityType, Params: params, Patch: patch, Note: note, Actor: actor,
	})
	if cerr != nil {
		return repr.ProposalRecord{}, "", creationErr(cerr)
	}
	return proposalFrom(prop), proposalETag(prop), nil
}

func creationErr(err error) error {
	if errors.Is(err, editing.ErrNotCreatable) {
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	}
	var perm *editing.PermissionError
	if errors.As(err, &perm) && perm.Key == editing.CreateFieldKey {
		p := problem.New(problem.CodePermissionRequired, "", "", "the create policy of this family does not let the bearer "+perm.Action+" a creation.")
		p.Errors = []problem.FieldError{{Pointer: "/object", Reason: problem.ReasonNotPermitted, Detail: perm.Error()}}
		return p
	}
	var val *editing.ValidationError
	if errors.As(err, &val) && val.Key == editing.CreateFieldKey {
		p := problem.New(problem.CodeValidationFailed, "", "", val.Reason)
		p.Errors = []problem.FieldError{{Pointer: "/params", Reason: problem.ReasonNotAllowedValue, Detail: val.Reason}}
		return p
	}
	return proposalErr(err)
}

func (c *Catalog) createCapability(entityType string, actor editing.PolicyContext, blocked bool) *repr.CreateCapability {
	if c.EditTypes == nil {
		return nil
	}
	spec, ok := c.EditTypes.Type(entityType)
	if !ok || spec.Birth == nil {
		return nil
	}
	capb, err := c.Engine.CreateCapability(entityType, actor)
	if err != nil {
		return nil
	}
	out := &repr.CreateCapability{
		CanCreate:      capb.CanCreate && !blocked,
		WouldAutomerge: capb.WouldAutomerge && !blocked,
		ParamSpecs:     make([]repr.CreateParam, 0, len(spec.Birth.Params)),
		RequiredFields: append([]string{}, spec.Birth.Required...),
	}
	for _, p := range spec.Birth.Params {
		out.ParamSpecs = append(out.ParamSpecs, repr.CreateParam{Key: p.Key, FieldType: string(p.Kind), Required: p.Required})
	}
	return out
}

func decodeCreationParams(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}, err
	}
	return out, nil
}
