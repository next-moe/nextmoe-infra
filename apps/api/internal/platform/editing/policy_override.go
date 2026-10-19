package editing

import (
	"fmt"
	"sort"
	"strings"
)

// PolicyOverride replaces some attributes of the code policy for one entity
// type (FieldKey "") or one field. A nil attribute inherits.
type PolicyOverride struct {
	EntityType  string  `json:"entity_type"`
	FieldKey    string  `json:"field_key"`
	Propose     *string `json:"propose,omitempty"`
	Review      *string `json:"review,omitempty"`
	Automerge   *string `json:"automerge,omitempty"`
	OwnerReview *bool   `json:"owner_review,omitempty"`
}

func (o PolicyOverride) Empty() bool {
	return o.Propose == nil && o.Review == nil && o.Automerge == nil && o.OwnerReview == nil
}

func (o PolicyOverride) applyTo(p Policy) Policy {
	if o.Propose != nil {
		p.Propose = *o.Propose
	}
	if o.Review != nil {
		p.Review = *o.Review
	}
	if o.Automerge != nil {
		p.Automerge = *o.Automerge
	}
	if o.OwnerReview != nil {
		p.OwnerReview = *o.OwnerReview
	}
	return p
}

type overrideTable map[string]map[string]PolicyOverride

func (t overrideTable) apply(entityType, fieldKey string, p Policy) Policy {
	byField, ok := t[entityType]
	if !ok {
		return p
	}
	if o, ok := byField[""]; ok {
		p = o.applyTo(p)
	}
	if parent, ok := strings.CutSuffix(fieldKey, SuppressedFieldSuffix); ok {
		if o, ok := byField[parent]; ok {
			p = o.applyTo(p)
		}
	}
	if o, ok := byField[fieldKey]; ok {
		p = o.applyTo(p)
	}
	return p
}

func (r *Registry) SetPermissionValidator(known func(key string) bool) {
	r.knownPerm = known
}

func (r *Registry) ValidateOverride(o PolicyOverride) error {
	spec, ok := r.types[o.EntityType]
	if !ok {
		return fmt.Errorf("unknown entity type %q", o.EntityType)
	}
	if o.FieldKey != "" {
		if _, ok := spec.fields[o.FieldKey]; !ok {
			return fmt.Errorf("unknown field %q on %s", o.FieldKey, o.EntityType)
		}
	}
	if o.Empty() {
		return fmt.Errorf("override sets nothing")
	}
	if o.Propose != nil {
		if err := r.validateRule("propose", *o.Propose, true); err != nil {
			return err
		}
	}
	if o.Review != nil {
		if err := r.validateRule("review", *o.Review, false); err != nil {
			return err
		}
	}
	if o.Automerge != nil {
		switch *o.Automerge {
		case AutomergeNever, AutomergeTrusted, AutomergeAlways, AutomergeReview, AutomergeTrustedOrReview:
		case AutomergeOwner:
			if spec.OwnerSite == nil {
				return fmt.Errorf("automerge=owner needs an owner hook, which %s does not register", o.EntityType)
			}
		default:
			return fmt.Errorf("bad automerge rule %q", *o.Automerge)
		}
	}
	return nil
}

func (r *Registry) validateRule(attr, rule string, allowNamed bool) error {
	if allowNamed {
		switch rule {
		case ProposeOpen, ProposeTrusted, ProposeLocked:
			return nil
		}
	}
	key, ok := strings.CutPrefix(rule, permPrefix)
	if !ok || key == "" {
		return fmt.Errorf("bad %s rule %q", attr, rule)
	}
	if r.knownPerm != nil && !r.knownPerm(key) {
		return fmt.Errorf("%s rule names unknown permission %q", attr, key)
	}
	return nil
}

func (r *Registry) SetPolicyOverrides(rows []PolicyOverride) []error {
	table := overrideTable{}
	var skipped []error
	for _, o := range rows {
		if err := r.ValidateOverride(o); err != nil {
			skipped = append(skipped, fmt.Errorf("%s/%s: %w", o.EntityType, o.FieldKey, err))
			continue
		}
		if table[o.EntityType] == nil {
			table[o.EntityType] = map[string]PolicyOverride{}
		}
		table[o.EntityType][o.FieldKey] = o
	}
	r.overrides.Store(&table)
	return skipped
}

func (r *Registry) PolicyOverrides() []PolicyOverride {
	table := r.overrides.Load()
	if table == nil {
		return nil
	}
	var out []PolicyOverride
	for _, byField := range *table {
		for _, o := range byField {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EntityType != out[j].EntityType {
			return out[i].EntityType < out[j].EntityType
		}
		return out[i].FieldKey < out[j].FieldKey
	})
	return out
}

type PolicyMatrixRow struct {
	EntityType string
	FieldKey   string
	Default    Policy
	Override   *PolicyOverride
	Effective  Policy
}

func (r *Registry) PolicyMatrix() []PolicyMatrixRow {
	table := overrideTable{}
	if t := r.overrides.Load(); t != nil {
		table = *t
	}
	var out []PolicyMatrixRow
	for _, name := range r.Types() {
		spec := r.types[name]
		typeRow := PolicyMatrixRow{EntityType: name, Default: spec.DefaultPolicy, Effective: spec.DefaultPolicy}
		if o, ok := table[name][""]; ok {
			o := o
			typeRow.Override = &o
			typeRow.Effective = o.applyTo(spec.DefaultPolicy)
		}
		out = append(out, typeRow)
		for i := range spec.Fields {
			key := spec.Fields[i].Key
			row := PolicyMatrixRow{EntityType: name, FieldKey: key, Default: spec.CodePolicy(key), Effective: spec.EffectivePolicy(key)}
			if o, ok := table[name][key]; ok {
				o := o
				row.Override = &o
			}
			out = append(out, row)
		}
	}
	return out
}
