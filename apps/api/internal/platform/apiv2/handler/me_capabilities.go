package handler

import (
	"context"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
)

func (c *Catalog) GetEditCapabilities(ctx context.Context, object string, entityID int64) (repr.EditCapabilities, error) {
	entityType := schemaEntityType(object)
	if entityType == "" {
		return repr.EditCapabilities{}, problem.New(problem.CodeNotFound, "", "", "No schema for family "+object+".")
	}
	if c == nil || c.Engine == nil {
		return repr.EditCapabilities{}, problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	actor, err := c.policyActor(ctx)
	if err != nil {
		return repr.EditCapabilities{}, err
	}
	quota, err := c.editQuota(ctx, actor.UserID)
	if err != nil {
		return repr.EditCapabilities{}, err
	}
	if quota.demotesEditor() {
		actor.TrustTier = 0
	}
	proj, err := c.Engine.SchemaProjection(ctx, entityType, entityID, actor)
	if err != nil {
		return repr.EditCapabilities{}, proposalErr(err)
	}

	blocked := quota.blocksFiling()
	fields := make([]repr.FieldCapability, 0, len(proj))
	for _, p := range proj {
		fields = append(fields, repr.FieldCapability{
			Key: p.Key, Locked: p.Locked, CanPropose: p.CanPropose && !blocked,
			CanReview: p.CanReview, WouldAutomerge: p.WouldAutomerge && !blocked,
		})
	}
	out := repr.EditCapabilities{
		Object: "edit_capabilities", TargetObject: object,
		EntityType: entityType, QuotaTier: quota.tier, QuotaExhausted: blocked,
		Central: isCentralSite(actor.Site), Fields: fields,
		Creation: c.createCapability(entityType, actor, blocked),
	}
	if entityID != 0 {
		id := repr.ID(entityID)
		out.EntityID = &id
	}
	return out, nil
}
