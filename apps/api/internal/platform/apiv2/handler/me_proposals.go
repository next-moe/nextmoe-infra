package handler

import (
	"context"
	"errors"
	"strconv"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/authz"
	catalogPerm "api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
)

func (c *Catalog) policyActor(ctx context.Context) (editing.PolicyContext, error) {
	uid, _, err := requireUser(ctx)
	if err != nil {
		return editing.PolicyContext{}, err
	}
	site, err := requireSite(ctx)
	if err != nil {
		return editing.PolicyContext{}, err
	}
	roles := rolesFrom(ctx)
	return editing.PolicyContext{
		UserID: uid, Site: site,
		ModerationCapped: thirdPartyFrom(ctx),
		TrustTier:        trustTier(ctx),
		HasPerm: func(key string) bool {
			return catalogPerm.Resolver.Can(roles, authz.Permission(key))
		},
	}, nil
}

// v1's userEditActor set the trusted tier from exactly this pair, and wave R3
// carried neither half to v2. PolicyContext.TrustTier ended up set by nothing,
// so every ProposeTrusted field was proposable by nobody -- which is the whole
// of letmoe's edit surface (editspec/work.go:57). The mint lane lost the other
// half: it read the permission without the third-party test, so a
// developer-owned app could publish a claim straight to live.
func actsAsTrusted(ctx context.Context) bool {
	return !thirdPartyFrom(ctx) &&
		catalogPerm.Resolver.Can(rolesFrom(ctx), catalogPerm.EditTrusted)
}

func trustTier(ctx context.Context) int16 {
	if actsAsTrusted(ctx) {
		return editing.TrustedTier
	}
	return 0
}

// Standing to review ONE item is the engine's own per-field rule, never a
// permission list. OwnerReview lives only in the kungal site overlay for
// catalog.work and catalog.release (editspec/work.go:65, release.go:54), so a
// gate built on a hand-maintained permission union cannot see entityID and
// cannot express the owner channel at all — it refused exactly the owners the
// forum's own gate admits (CanModerate OR isGameOwner). entityID 0 means "no
// entity", which nobody owns: that is what keeps the type-level queue
// type-level rather than leaking it to every claimant.
func (c *Catalog) reviewsAnyField(ctx context.Context, entityType, site string, entityID int64, actor editing.PolicyContext) bool {
	if c.EditTypes == nil {
		return false
	}
	spec, ok := c.EditTypes.Type(entityType)
	if !ok {
		return false
	}
	if entityID > 0 && actor.UserID > 0 && spec.OwnerUserID != nil {
		owner, err := spec.OwnerUserID(ctx, entityID)
		if err == nil && owner != nil && *owner == actor.UserID {
			actor.IsEntityOwner = true
		}
	}
	if site == "" {
		site = actor.Site
	}
	for i := range spec.Fields {
		if spec.EffectivePolicy(spec.Fields[i].Key, site).AllowsReview(actor) {
			return true
		}
	}
	return false
}

func permissionRequired(detail string) error {
	return problem.New(problem.CodePermissionRequired, "", "", detail)
}

// The read faces have fenced on Site since they shipped; the write faces
// compared neither Site nor ProposerUID, so review authority on one tenant
// decided, amended and withdrew another tenant's proposals. If-Match did not
// stand in the way either: `If-Match: *` matches any non-empty validator.
func (c *Catalog) fenceProposal(ctx context.Context, prop *editing.Proposal, actor editing.PolicyContext, proposerOrReviewer bool) error {
	if prop.Site != actor.Site {
		return problem.New(problem.CodeTenantMismatch, "", "",
			"this proposal was filed on another catalog site.")
	}
	if proposerOrReviewer && prop.ProposerUID != actor.UserID &&
		!c.reviewsAnyField(ctx, prop.EntityType, prop.Site, prop.EntityID, actor) {
		return permissionRequired("only the proposer, or someone with review standing on this entity, may change this proposal.")
	}
	return nil
}

func (c *Catalog) fenceEntitySite(ctx context.Context, entityType string, entityID int64, site string) error {
	if c.EditTypes == nil {
		return nil
	}
	spec, ok := c.EditTypes.Type(entityType)
	if !ok || spec.OwnerSite == nil {
		return nil
	}
	owner, err := spec.OwnerSite(ctx, entityID)
	if err != nil {
		return proposalErr(err)
	}
	if owner == nil || *owner == "" || *owner == site {
		return nil
	}
	return problem.New(problem.CodeTenantMismatch, "", "",
		"this entity is claimed by another catalog site.")
}

func proposalFrom(p *editing.Proposal) repr.ProposalRecord {
	st := editing.StatusName[p.Status]
	if st == "" {
		st = "open"
	}
	rec := repr.ProposalRecord{
		Object: "proposal", ID: repr.ID(p.ID), State: st,
		TargetObject: schemaObject(p.EntityType),
		EntityType:   p.EntityType, EntityID: repr.ID(p.EntityID), Note: p.Note,
		ProposerUID: repr.ID(p.ProposerUID), Site: p.Site,
		BaseRevisionSeq: p.BaseRevisionSeq,
		DecidedByUID:    idPtr(p.DecidedByUID),
		CreatedAt:       rfc3339(p.CreatedAt), UpdatedAt: rfc3339(p.UpdatedAt),
	}
	if p.DecidedAt != nil {
		at := rfc3339(*p.DecidedAt)
		rec.DecidedAt = &at
	}
	return rec
}

// The decision note is the reviewer's internal reasoning and is the one field
// the v1 proposal view carries that the public transparency face must not.
func (c *Catalog) proposalDetail(ctx context.Context, id int64, include []string) (repr.ProposalRecord, string, error) {
	prop, amendments, effective, err := c.Engine.GetProposal(ctx, id)
	if err != nil {
		return repr.ProposalRecord{}, "", proposalErr(err)
	}
	rec := proposalFrom(prop)
	if hasToken(include, "amendments") {
		list := amendmentsFrom(amendments)
		rec.Amendments = &list
	}
	if hasToken(include, "patch") {
		patch := decodeJSONObject(prop.Patch)
		rec.Patch = &patch
		if effective == nil {
			effective = map[string]any{}
		}
		rec.EffectivePatch = &effective
	}
	return rec, proposalETag(prop), nil
}

// Microseconds, like the news validator: at whole-second granularity two
// amendments inside the same second left a stale validator matching.
func proposalETag(p *editing.Proposal) string {
	return `"p` + repr.ID(p.ID) + "." + strconv.FormatInt(p.UpdatedAt.UnixMicro(), 10) + `"`
}

// entity_type=/entity_id= were declared nowhere and set nowhere while all
// three consumers sent them, so "existing proposals on this work" answered
// with the caller's proposals on every entity they had ever edited; an unknown
// state= was dropped by a bare `if ok` and the face answered every state. Both
// die by routing this lane through the same query builder the public and queue
// lanes already use.
func (c *Catalog) ListMyProposals(ctx context.Context, q collect.Query, f proposalFilter) (repr.List[repr.ProposalRecord], error) {
	if c == nil || c.Engine == nil {
		return repr.List[repr.ProposalRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.ProposalRecord]{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	before, berr := claimEventCursor(q.Cursor)
	if berr != nil {
		return repr.List[repr.ProposalRecord]{}, berr
	}
	f.Site = siteFrom(ctx)
	f.ProposerScoped = true
	filter, ferr := c.proposalQuery(f, limit+1, before)
	if ferr != nil {
		return repr.List[repr.ProposalRecord]{}, ferr
	}
	filter.ProposerUID = uid
	rows, total, lerr := c.Engine.ListProposalsWithTotal(ctx, filter)
	if lerr != nil {
		return repr.List[repr.ProposalRecord]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		next = &s
	}
	items := make([]repr.ProposalRecord, 0, len(rows))
	for i := range rows {
		items = append(items, proposalFrom(&rows[i]))
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetMyProposal(ctx context.Context, id int64, include []string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	rec, etag, derr := c.proposalDetail(ctx, id, include)
	if derr != nil {
		return repr.ProposalRecord{}, "", derr
	}
	if rec.ProposerUID != repr.ID(uid) {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeNotFound, "", "", "No proposal with this id.")
	}
	return rec, etag, nil
}

func (c *Catalog) GetModerationProposal(ctx context.Context, id int64, include []string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, aerr := c.policyActor(ctx)
	if aerr != nil {
		return repr.ProposalRecord{}, "", aerr
	}
	rec, etag, derr := c.proposalDetail(ctx, id, include)
	if derr != nil {
		return repr.ProposalRecord{}, "", derr
	}
	if rec.Site != actor.Site {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeTenantMismatch, "", "",
			"this proposal was filed on another catalog site.")
	}
	eid, _ := repr.ParseID(rec.EntityID)
	if !c.reviewsAnyField(ctx, rec.EntityType, rec.Site, eid, actor) {
		return repr.ProposalRecord{}, "", permissionRequired(
			"this face publishes another contributor's patch; it needs review standing on this entity.")
	}
	return rec, etag, nil
}

func (c *Catalog) CreateProposal(ctx context.Context, entityType, entityID string, patch map[string]any, note string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	eid, ok := repr.ParseID(entityID)
	if !ok {
		p := problem.New(problem.CodeValidationFailed, "", "", "entity_id must be a decimal catalog id.")
		p.Errors = []problem.FieldError{{Pointer: "/entity_id", Reason: problem.ReasonInvalidFormat, Detail: entityID}}
		return repr.ProposalRecord{}, "", p
	}
	prop, _, cerr := c.Engine.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: eid, Patch: patch, Note: note, Actor: actor,
	})
	if cerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(cerr)
	}
	return proposalFrom(prop), proposalETag(prop), nil
}

func (c *Catalog) PatchProposal(ctx context.Context, id int64, state string, patch map[string]any, ifMatch string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	prop, _, _, gerr := c.Engine.GetProposal(ctx, id)
	if gerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(gerr)
	}
	if ferr := c.fenceProposal(ctx, prop, actor, true); ferr != nil {
		return repr.ProposalRecord{}, "", ferr
	}
	if err := requireIfMatch(ifMatch, proposalETag(prop)); err != nil {
		return repr.ProposalRecord{}, "", err
	}
	if state == "withdrawn" {
		if werr := c.Engine.WithdrawProposal(ctx, id, actor); werr != nil {
			return repr.ProposalRecord{}, "", proposalErr(werr)
		}
	} else if len(patch) > 0 {
		if _, aerr := c.Engine.AmendProposal(ctx, id, editing.AmendInput{Set: patch, Actor: actor}); aerr != nil {
			return repr.ProposalRecord{}, "", proposalErr(aerr)
		}
	} else {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeValidationFailed, "", "", "patch must withdraw or amend.")
	}
	return c.proposalAfterWrite(ctx, id)
}

func (c *Catalog) AmendProposal(ctx context.Context, id int64, set map[string]any, unset []string, note, ifMatch string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	prop, _, _, gerr := c.Engine.GetProposal(ctx, id)
	if gerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(gerr)
	}
	if ferr := c.fenceProposal(ctx, prop, actor, true); ferr != nil {
		return repr.ProposalRecord{}, "", ferr
	}
	if err := requireIfMatch(ifMatch, proposalETag(prop)); err != nil {
		return repr.ProposalRecord{}, "", err
	}
	if _, aerr := c.Engine.AmendProposal(ctx, id, editing.AmendInput{Set: set, Unset: unset, Note: note, Actor: actor}); aerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(aerr)
	}
	return c.proposalAfterWrite(ctx, id)
}

// The amendment lane answered a ProposalRecord carrying the *proposal's* id
// and note, with seq and amender_uid at zero, so a client could not tell what
// it had just appended. The chain the re-read already returns says it exactly,
// and it is the same block include=amendments publishes.
func (c *Catalog) proposalAfterWrite(ctx context.Context, id int64) (repr.ProposalRecord, string, error) {
	prop, amendments, _, gerr := c.Engine.GetProposal(ctx, id)
	if gerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(gerr)
	}
	rec := proposalFrom(prop)
	list := amendmentsFrom(amendments)
	rec.Amendments = &list
	return rec, proposalETag(prop), nil
}

// Two lanes, two authorities. The whole type's queue needs a type-level review
// permission; naming object=+entity_id= narrows it to one entity, which its
// owner may read even holding no permission at all. Reading the whole queue
// must never be reachable through the owner arm — that is the failure this
// split exists to prevent, and the entity filter is applied to the query, not
// only to the check.
func (c *Catalog) ListModerationProposals(ctx context.Context, q collect.Query, object, entityType, entityID string) (repr.List[repr.ProposalRecord], error) {
	if c == nil || c.Engine == nil {
		return repr.List[repr.ProposalRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, aerr := c.policyActor(ctx)
	if aerr != nil {
		return repr.List[repr.ProposalRecord]{}, aerr
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	before, berr := claimEventCursor(q.Cursor)
	if berr != nil {
		return repr.List[repr.ProposalRecord]{}, berr
	}
	f, ferr := c.proposalQuery(proposalFilter{
		Object: object, EntityType: entityType, EntityID: entityID, Site: actor.Site,
	}, limit+1, before)
	if ferr != nil {
		return repr.List[repr.ProposalRecord]{}, ferr
	}
	f.Status = editing.StatusOpen
	if f.EntityID > 0 {
		if terr := c.fenceEntitySite(ctx, f.EntityType, f.EntityID, actor.Site); terr != nil {
			return repr.List[repr.ProposalRecord]{}, terr
		}
		if !c.reviewsAnyField(ctx, f.EntityType, actor.Site, f.EntityID, actor) {
			return repr.List[repr.ProposalRecord]{}, permissionRequired(
				"this entity's queue needs review standing on it, which its owner has.")
		}
	} else if !catalogPerm.Moderates(rolesFrom(ctx)) {
		return repr.List[repr.ProposalRecord]{}, permissionRequired(
			"the whole queue requires a catalog review permission; name object= and entity_id= to read one entity's.")
	}
	rows, total, lerr := c.Engine.ListProposalsWithTotal(ctx, f)
	if lerr != nil {
		return repr.List[repr.ProposalRecord]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		next = &s
	}
	items := make([]repr.ProposalRecord, 0, len(rows))
	for i := range rows {
		items = append(items, proposalFrom(&rows[i]))
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) DecideProposal(ctx context.Context, id int64, decision, note, ifMatch string) (repr.ProposalDecisionRecord, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalDecisionRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalDecisionRecord{}, err
	}
	prop, _, _, gerr := c.Engine.GetProposal(ctx, id)
	if gerr != nil {
		return repr.ProposalDecisionRecord{}, proposalErr(gerr)
	}
	if ferr := c.fenceProposal(ctx, prop, actor, false); ferr != nil {
		return repr.ProposalDecisionRecord{}, ferr
	}
	if err := requireIfMatch(ifMatch, proposalETag(prop)); err != nil {
		return repr.ProposalDecisionRecord{}, err
	}
	from := editing.StatusName[prop.Status]
	switch decision {
	case "merge":
		if _, merr := c.Engine.MergeProposal(ctx, id, actor, note); merr != nil {
			return repr.ProposalDecisionRecord{}, proposalErr(merr)
		}
	case "decline":
		if derr := c.Engine.DeclineProposal(ctx, id, actor, note); derr != nil {
			return repr.ProposalDecisionRecord{}, proposalErr(derr)
		}
	default:
		p := problem.New(problem.CodeValidationFailed, "", "", "decision must be merge or decline.")
		p.Errors = []problem.FieldError{{Pointer: "/decision", Reason: problem.ReasonUnknownValue, Detail: "merge or decline"}}
		return repr.ProposalDecisionRecord{}, p
	}
	// Read the outcome back rather than deriving it from the verb: a
	// suppression rule can close a merge as declined, and the claim face's
	// sibling defect was exactly a client deriving the resulting state.
	rec := repr.ProposalDecisionRecord{Object: "decision", ID: repr.ID(id), Decision: decision, Note: note}
	if from != "" {
		rec.FromState = &from
	}
	if after, _, _, aerr := c.Engine.GetProposal(ctx, id); aerr == nil {
		rec.ToState = editing.StatusName[after.Status]
	}
	if rec.ToState == "" {
		rec.ToState = decisionEndState(decision)
	}
	return rec, nil
}

func decisionEndState(decision string) string {
	if decision == "merge" {
		return editing.StatusName[editing.StatusMerged]
	}
	return editing.StatusName[editing.StatusDeclined]
}

func (c *Catalog) RevertRevision(ctx context.Context, revisionID, reason string) (repr.ProposalRecord, string, error) {
	if c == nil || c.Engine == nil {
		return repr.ProposalRecord{}, "", problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.ProposalRecord{}, "", err
	}
	rid, ok := repr.ParseID(revisionID)
	if !ok {
		p := problem.New(problem.CodeValidationFailed, "", "", "revision_id must be a decimal id.")
		p.Errors = []problem.FieldError{{Pointer: "/revision_id", Reason: problem.ReasonInvalidFormat, Detail: revisionID}}
		return repr.ProposalRecord{}, "", p
	}
	rev, rerr := c.Engine.RevisionByID(ctx, rid)
	if rerr != nil {
		return repr.ProposalRecord{}, "", proposalErr(rerr)
	}
	if ferr := c.fenceEntitySite(ctx, rev.EntityType, rev.EntityID, actor.Site); ferr != nil {
		return repr.ProposalRecord{}, "", ferr
	}
	prop, _, verr := c.Engine.Revert(ctx, editing.RevertInput{
		EntityType: rev.EntityType, EntityID: rev.EntityID, ToSeq: rev.Seq, Note: reason, Actor: actor,
	})
	if verr != nil {
		return repr.ProposalRecord{}, "", proposalErr(verr)
	}
	return proposalFrom(prop), proposalETag(prop), nil
}

func (c *Catalog) GetSnapshot(ctx context.Context, object, id string) (repr.SnapshotRecord, error) {
	if c == nil || c.Engine == nil {
		return repr.SnapshotRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "snapshots are not bound.")
	}
	if _, _, err := requireUser(ctx); err != nil {
		return repr.SnapshotRecord{}, err
	}
	entityType := schemaEntityType(object)
	if entityType == "" {
		return repr.SnapshotRecord{}, problem.New(problem.CodeNotFound, "", "", "No schema for family "+object+".")
	}
	eid, ok := repr.ParseID(id)
	if !ok {
		return repr.SnapshotRecord{}, problemInvalidID(id)
	}
	vals, err := c.Engine.CurrentSnapshot(ctx, entityType, eid)
	if err != nil {
		return repr.SnapshotRecord{}, proposalErr(err)
	}
	if vals == nil {
		vals = map[string]any{}
	}
	return repr.SnapshotRecord{Object: "snapshot", EntityType: entityType, EntityID: repr.ID(eid), FieldValues: vals}, nil
}

func proposalStateValue(state string) (int16, bool) {
	switch state {
	case "pending", "open":
		return editing.StatusOpen, true
	case "merged":
		return editing.StatusMerged, true
	case "declined":
		return editing.StatusDeclined, true
	case "withdrawn":
		return editing.StatusWithdrawn, true
	default:
		return -1, false
	}
}

func proposalErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, editing.ErrProposalNotFound), errors.Is(err, editing.ErrRevisionNotFound),
		errors.Is(err, editing.ErrUnknownEntityType), errors.Is(err, editing.ErrEntityNotFound):
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	case errors.Is(err, editing.ErrEmptyPatch), errors.Is(err, editing.ErrEmptyDelta):
		p := problem.New(problem.CodeValidationFailed, "", "", err.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch", Reason: problem.ReasonRequired, Detail: err.Error()}}
		return p
	case errors.Is(err, editing.ErrNoEffectiveChanges):
		p := problem.New(problem.CodeValidationFailed, "", "", err.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch", Reason: problem.ReasonNotAllowedValue, Detail: err.Error()}}
		return p
	// A trusted proposer's edit auto-merges on creation, so the very next decide
	// lands on a closed proposal. Unmapped this surfaced as a 500.
	case errors.Is(err, editing.ErrNotOpen):
		return problem.New(problem.CodeDecisionAlreadyMade, "", "", err.Error())
	case errors.Is(err, editing.ErrNotProposer):
		return problem.New(problem.CodePermissionRequired, "", "", err.Error())
	}
	var conflict *editing.ConflictError
	if errors.As(err, &conflict) {
		p := problem.New(problem.CodeValidationFailed, "", "", conflict.Error())
		for _, key := range conflict.Keys {
			p.Errors = append(p.Errors, problem.FieldError{
				Pointer: "/patch/" + key, Reason: problem.ReasonInconsistentWith,
				Detail: "another revision changed this key since the proposal was written",
			})
		}
		return p
	}
	var unknownField *editing.UnknownFieldError
	if errors.As(err, &unknownField) {
		p := problem.New(problem.CodeValidationFailed, "", "", unknownField.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch/" + unknownField.Key, Reason: problem.ReasonUnknownValue, Detail: unknownField.Error()}}
		return p
	}
	var lockedField *editing.LockedFieldError
	if errors.As(err, &lockedField) {
		p := problem.New(problem.CodeValidationFailed, "", "", lockedField.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch/" + lockedField.Key, Reason: problem.ReasonImmutable, Detail: lockedField.Error()}}
		return p
	}
	var perm *editing.PermissionError
	if errors.As(err, &perm) {
		p := problem.New(problem.CodePermissionRequired, "", "", perm.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch/" + perm.Key, Reason: problem.ReasonNotPermitted, Detail: perm.Error()}}
		return p
	}
	var val *editing.ValidationError
	if errors.As(err, &val) {
		p := problem.New(problem.CodeValidationFailed, "", "", val.Error())
		p.Errors = []problem.FieldError{{Pointer: "/patch/" + val.Key, Reason: problem.ReasonUnknownValue, Detail: val.Error()}}
		return p
	}
	return err
}
