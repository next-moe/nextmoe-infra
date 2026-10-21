package editing

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"api/internal/platform/editorigin"

	"gorm.io/gorm"
)

// CreateFieldKey addresses a type's create policy in the override table and
// in permission errors; it can never collide with a field key, which always
// contains a dot-separated family prefix.
const CreateFieldKey = "@create"

var ErrNotCreatable = errors.New("editing: entity type cannot be created through the engine")

type BirthParam struct {
	Key      string
	Kind     FieldKind
	Required bool
}

type BirthSpec struct {
	Params []BirthParam
	// Required lists field keys a birth patch must carry.
	Required []string
	Policy   *Policy
	// Check runs at filing and again inside the merge transaction.
	Check func(ctx context.Context, db *gorm.DB, params, patch map[string]any) error
	// Create inserts the bare row and returns its id; the patch is applied
	// afterwards through ApplyField in the same transaction.
	Create     func(ctx context.Context, tx *gorm.DB, params, patch map[string]any) (int64, error)
	Duplicates func(ctx context.Context, db *gorm.DB, params, patch map[string]any) ([]DuplicateHint, error)
}

type DuplicateHint struct {
	ID   int64
	Name string
}

func validateBirth(spec *EntityTypeSpec, b BirthSpec) error {
	if b.Create == nil {
		return fmt.Errorf("editing: type %q birth needs Create", spec.Type)
	}
	for _, key := range b.Required {
		if _, ok := spec.fields[key]; !ok {
			return fmt.Errorf("editing: type %q birth requires unregistered field %q", spec.Type, key)
		}
	}
	if b.Policy != nil {
		if err := validatePolicy(*b.Policy); err != nil {
			return fmt.Errorf("editing: type %q birth policy: %w", spec.Type, err)
		}
		if b.Policy.Automerge == AutomergeOwner || b.Policy.OwnerReview {
			return fmt.Errorf("editing: type %q birth policy cannot use the owner channel: nothing owns an entity before it exists", spec.Type)
		}
	}
	return nil
}

// AttachBirth lets a family's creation path live apart from its field
// registration.
func (r *Registry) AttachBirth(entityType string, b BirthSpec) error {
	spec, ok := r.types[entityType]
	if !ok {
		return fmt.Errorf("editing: attach birth to unregistered type %q", entityType)
	}
	if err := validateBirth(spec, b); err != nil {
		return err
	}
	spec.Birth = &b
	return nil
}

type CreateBirthInput struct {
	EntityType string
	Params     map[string]any
	Patch      map[string]any
	Note       string
	Actor      PolicyContext
}

func (s *EntityTypeSpec) CreatePolicy() Policy {
	p := s.DefaultPolicy
	if s.Birth != nil && s.Birth.Policy != nil {
		p = *s.Birth.Policy
	}
	if s.reg != nil {
		if table := s.reg.overrides.Load(); table != nil {
			p = table.applyCreate(s.Type, p)
		}
	}
	return p
}

func (t overrideTable) applyCreate(entityType string, p Policy) Policy {
	byField, ok := t[entityType]
	if !ok {
		return p
	}
	if o, ok := byField[""]; ok {
		p = o.applyTo(p)
	}
	if o, ok := byField[CreateFieldKey]; ok {
		p = o.applyTo(p)
	}
	return p
}

type CreateCapability struct {
	CanCreate      bool
	WouldAutomerge bool
}

func (e *Engine) CreateCapability(entityType string, pc PolicyContext) (CreateCapability, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return CreateCapability{}, err
	}
	if spec.Birth == nil {
		return CreateCapability{}, nil
	}
	pol := spec.CreatePolicy()
	can := pol.AllowsPropose(pc)
	return CreateCapability{CanCreate: can, WouldAutomerge: can && pol.allowsAutomergeWithOwner(pc, nil)}, nil
}

func (e *Engine) BirthDuplicates(ctx context.Context, entityType string, params, patch map[string]any) ([]DuplicateHint, error) {
	spec, err := e.resolveSpec(entityType)
	if err != nil {
		return nil, err
	}
	if spec.Birth == nil || spec.Birth.Duplicates == nil {
		return nil, nil
	}
	return spec.Birth.Duplicates(ctx, e.db.WithContext(ctx), params, patch)
}

func (e *Engine) checkBirth(ctx context.Context, db *gorm.DB, spec *EntityTypeSpec, params, patch map[string]any) error {
	if params == nil {
		params = map[string]any{}
	}
	for _, p := range spec.Birth.Params {
		if _, ok := params[p.Key]; p.Required && !ok {
			return &ValidationError{Key: CreateFieldKey, Reason: fmt.Sprintf("param %q is required", p.Key)}
		}
	}
	known := make(map[string]struct{}, len(spec.Birth.Params))
	for _, p := range spec.Birth.Params {
		known[p.Key] = struct{}{}
	}
	for key := range params {
		if _, ok := known[key]; !ok {
			return &ValidationError{Key: CreateFieldKey, Reason: fmt.Sprintf("unknown param %q", key)}
		}
	}
	for _, key := range spec.Birth.Required {
		if _, ok := patch[key]; !ok {
			return &ValidationError{Key: key, Reason: "required to create the entry"}
		}
	}
	if spec.Birth.Check != nil {
		return spec.Birth.Check(ctx, db, params, patch)
	}
	return nil
}

// CreateBirth files a proposal that creates a new entity. It carries
// EntityID 0 until it merges; the merge creates the row, applies the patch and
// rewrites the proposal's entity_id to the new id.
func (e *Engine) CreateBirth(ctx context.Context, in CreateBirthInput) (*Proposal, *Revision, error) {
	spec, err := e.resolveSpec(in.EntityType)
	if err != nil {
		return nil, nil, err
	}
	if spec.Birth == nil {
		return nil, nil, ErrNotCreatable
	}
	if in.Patch == nil {
		in.Patch = map[string]any{}
	}
	createPol := spec.CreatePolicy()
	if !createPol.AllowsPropose(in.Actor) {
		return nil, nil, &PermissionError{Key: CreateFieldKey, Action: "propose"}
	}
	automerge := createPol.allowsAutomergeWithOwner(in.Actor, nil)
	for _, key := range sortedKeys(in.Patch) {
		f, err := spec.fieldForWrite(key)
		if err != nil {
			return nil, nil, err
		}
		pol := spec.EffectivePolicy(key)
		if pol.Propose == ProposeLocked {
			return nil, nil, &LockedFieldError{Key: key}
		}
		if !pol.AllowsPropose(in.Actor) {
			return nil, nil, &PermissionError{Key: key, Action: "propose"}
		}
		if err := f.Validate(in.Patch[key]); err != nil {
			return nil, nil, &ValidationError{Key: key, Reason: err.Error()}
		}
		if !pol.allowsAutomergeWithOwner(in.Actor, nil) {
			automerge = false
		}
	}
	if err := e.checkBirth(ctx, e.db.WithContext(ctx), spec, in.Params, in.Patch); err != nil {
		return nil, nil, err
	}
	params := in.Params
	if params == nil {
		params = map[string]any{}
	}
	rawParams, err := encodeJSON(params)
	if err != nil {
		return nil, nil, err
	}
	rawPatch, err := encodeJSON(in.Patch)
	if err != nil {
		return nil, nil, err
	}
	prop := &Proposal{
		EntityFamily: spec.Family, EntityType: spec.Type,
		Patch: rawPatch, BirthParams: rawParams,
		ProposerUID: in.Actor.UserID, Note: in.Note, Site: in.Actor.Site, Status: StatusOpen,
		OriginSite: editorigin.Ptr(ctx),
	}
	if !automerge {
		if err := e.db.WithContext(ctx).Create(prop).Error; err != nil {
			return nil, nil, err
		}
		return prop, nil, nil
	}
	var rev *Revision
	err = e.db.WithContext(ctx).Transaction(func(etx *gorm.DB) error {
		if err := etx.Create(prop).Error; err != nil {
			return err
		}
		r, err := e.mergeBirthLocked(ctx, etx, spec, prop, in.Patch, nil, in.Actor.UserID, "")
		if err != nil {
			return err
		}
		rev = r
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	e.afterMerge(ctx, rev)
	return prop, rev, nil
}

func (e *Engine) reviewBirth(spec *EntityTypeSpec, eff map[string]any, actor PolicyContext) error {
	if spec.Birth == nil {
		return ErrNotCreatable
	}
	if !spec.CreatePolicy().AllowsReview(actor) {
		return &PermissionError{Key: CreateFieldKey, Action: "review"}
	}
	for _, key := range sortedKeys(eff) {
		f, err := spec.fieldForWrite(key)
		if err != nil {
			return err
		}
		if !spec.EffectivePolicy(key).AllowsReview(actor) {
			return &PermissionError{Key: key, Action: "review"}
		}
		if err := f.Validate(eff[key]); err != nil {
			return &ValidationError{Key: key, Reason: err.Error()}
		}
	}
	return nil
}

func (e *Engine) mergeBirthLocked(
	ctx context.Context, etx *gorm.DB, spec *EntityTypeSpec, prop *Proposal,
	eff map[string]any, amenderUID *int64, decidedBy int64, decisionNote string,
) (*Revision, error) {
	params, err := decodeObject(prop.BirthParams)
	if err != nil {
		return nil, err
	}
	if err := e.checkBirth(ctx, etx, spec, params, eff); err != nil {
		return nil, err
	}
	entityID, err := spec.Birth.Create(ctx, etx, params, eff)
	if err != nil {
		return nil, err
	}
	keys := sortedKeys(eff)
	for _, key := range keys {
		f, _ := spec.Field(key)
		if err := ApplyField(ctx, etx, f, entityID, eff[key]); err != nil {
			return nil, err
		}
	}
	sort.Strings(keys)
	rawChanged, err := encodeJSON(keys)
	if err != nil {
		return nil, err
	}
	rawSnapshot, err := encodeJSON(eff)
	if err != nil {
		return nil, err
	}
	rev := &Revision{
		EntityFamily: spec.Family, EntityType: spec.Type, EntityID: entityID,
		Seq: 1, Action: ActionCreated,
		ChangedFields: rawChanged, Snapshot: rawSnapshot,
		ActorUID: prop.ProposerUID, AmenderUID: amenderUID, ProposalID: &prop.ID,
		Site: prop.Site, OriginSite: prop.OriginSite,
	}
	if err := etx.Create(rev).Error; err != nil {
		return nil, err
	}
	if err := etx.Model(&Proposal{}).Where("id = ?", prop.ID).UpdateColumn("entity_id", entityID).Error; err != nil {
		return nil, err
	}
	if err := closeProposal(etx, prop.ID, StatusMerged, decidedBy, decisionNote); err != nil {
		return nil, err
	}
	prop.EntityID = entityID
	prop.Status = StatusMerged
	prop.DecidedByUID = &decidedBy
	prop.DecisionNote = decisionNote
	return rev, nil
}

func (p *Proposal) IsBirth() bool { return len(p.BirthParams) > 0 }
