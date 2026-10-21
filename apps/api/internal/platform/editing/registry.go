package editing

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"gorm.io/gorm"
)

type FieldKind string

const (
	KindText      FieldKind = "text"
	KindI18nMap   FieldKind = "i18nmap"
	KindEnum      FieldKind = "enum"
	KindInt       FieldKind = "int"
	KindDate      FieldKind = "date"
	KindList      FieldKind = "list"
	KindRef       FieldKind = "ref"
	KindImageHash FieldKind = "imagehash"
)

const (
	DiffHintInline = "inline"
	DiffHintLines  = "lines"
	DiffHintItems  = "items"
	DiffHintImage  = "image"
)

const (
	ProposeOpen    = "open"
	ProposeTrusted = "trusted"
	ProposeLocked  = "locked"
)

const (
	AutomergeNever           = "never"
	AutomergeTrusted         = "trusted"
	AutomergeAlways          = "always"
	AutomergeOwner           = "owner"
	AutomergeReview          = "review"
	AutomergeTrustedOrReview = "trusted_or_review"
)

const permPrefix = "perm:"

func ProposePerm(key string) string { return permPrefix + key }

func ReviewPerm(key string) string { return permPrefix + key }

type Policy struct {
	Propose     string
	Review      string
	Automerge   string
	OwnerReview bool
}

func (p Policy) AllowsPropose(pc PolicyContext) bool {
	switch {
	case p.Propose == ProposeOpen:
		return true
	case p.Propose == ProposeTrusted:
		return pc.TrustTier >= TrustedTier
	case strings.HasPrefix(p.Propose, permPrefix):
		return pc.hasPerm(strings.TrimPrefix(p.Propose, permPrefix))
	default:
		return false
	}
}

func (p Policy) AllowsReview(pc PolicyContext) bool {
	if pc.ModerationCapped {
		return false
	}
	if p.OwnerReview && pc.IsEntityOwner {
		return true
	}
	if !strings.HasPrefix(p.Review, permPrefix) {
		return false
	}
	return pc.hasPerm(strings.TrimPrefix(p.Review, permPrefix))
}

func (p Policy) AllowsAutomerge(pc PolicyContext) bool {
	return p.allowsAutomergeWithOwner(pc, nil)
}

func (p Policy) allowsAutomergeWithOwner(pc PolicyContext, owner *string) bool {
	if pc.ModerationCapped {
		return false
	}
	switch p.Automerge {
	case AutomergeAlways:
		return true
	case AutomergeTrusted:
		return pc.TrustTier >= TrustedTier
	case AutomergeReview:
		return p.AllowsReview(pc)
	case AutomergeTrustedOrReview:
		return pc.TrustTier >= TrustedTier || p.AllowsReview(pc)
	case AutomergeOwner:
		return owner != nil && *owner != "" && *owner == pc.Site
	default:
		return false
	}
}

type ApplyFunc func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error

// DefaultMaxElements is the element cap a list field inherits when it declares
// no MaxElements of its own.
const DefaultMaxElements = 200

type FieldSpec struct {
	Key           string
	Kind          FieldKind
	DiffHint      string
	Deprecated    bool
	Policy        *Policy
	Provenance    []ProvenanceTarget
	Identity      *IdentitySpec
	MaxSuppressed int
	MaxElements   int
	Value         *ValueSpec
	Validate      func(value any) error
	Apply         ApplyFunc
	Mirror        *MirrorSpec
}

// MirrorSpec is for a field whose Apply also rewrites the same field on other
// entities of its type: a relation stored once and read from both ends.
type MirrorSpec struct {
	// Others lists the entities, besides the one written, whose value of the
	// field can change when before is replaced by after.
	Others func(before, after any) []int64
	// Load reads one entity's value of the field through db, so the engine
	// sees what its own transaction wrote.
	Load func(ctx context.Context, db *gorm.DB, entityID int64) (any, error)
}

// ValueSpec is the declarative twin of Validate: it states, for the schema
// face, what the closure enforces. It is metadata only — nothing reads it on
// the write path, so a declaration that drifts from the closure is a silent
// lie to every editing client. TestSchemaValueSpecCompleteness and
// TestVocabularyTokensPassValidators (editspec package) are the gates that
// keep the two in step; a field must not gain a Vocabulary or Element that
// those tests cannot see.
type ValueSpec struct {
	// Vocabulary names a /v2/vocabularies vocabulary whose tokens the field
	// accepts. For an integer-coded field or member the vocabulary's tokens
	// are labels and the wire carries Base plus the token's index in the
	// vocabulary's published order.
	Vocabulary string
	// Coded says how the wire carries the value: true means Base plus the
	// token's index in the vocabulary's published order, false (the zero
	// value) means the token string itself. Nothing else on the schema face
	// separates the two, so leaving it unset on an integer-coded field
	// publishes a token contract the validator rejects.
	Coded bool
	// Base is the wire code of the vocabulary's first token. 0 for every
	// 0-coded enum; 1 where the model reserves 0 for null (gender,
	// blood_type).
	Base int
	// Nullable is true when the validator accepts null and null clears the
	// stored value.
	Nullable bool
	// Element describes one element of a KindList field.
	Element *ElementSpec
}

type ElementSpec struct {
	// Type is object, text or ref.
	Type string
	// Members lists an object element's keys, in the order the validator
	// checks them. Empty for scalar elements.
	Members []ElementMember
}

type ElementMember struct {
	Key string
	// Type is text, int, enum, bool, ref or imagehash.
	Type       string
	Vocabulary string
	// Base is the wire code of the vocabulary's first token, as on ValueSpec.
	Base int
	// Nullable is true when the member may be absent or empty in at least one
	// valid element; the validator stays the authority on when.
	Nullable bool
}

type MergeEvent struct {
	EntityID   int64
	ActorUID   int64
	AmenderUID *int64
	Action     int16
}

type EntityTypeSpec struct {
	Family        string
	Type          string
	LoadSnapshot  func(ctx context.Context, entityID int64) (map[string]any, error)
	OwnerSite     func(ctx context.Context, entityID int64) (*string, error)
	OwnerUserID   func(ctx context.Context, entityID int64) (*int64, error)
	OnMerge       func(ctx context.Context, ev MergeEvent) error
	Fields        []FieldSpec
	DefaultPolicy Policy
	Birth         *BirthSpec

	fields map[string]*FieldSpec
	reg    *Registry
}

func (s *EntityTypeSpec) Field(key string) (*FieldSpec, bool) {
	f, ok := s.fields[key]
	return f, ok
}

func (s *EntityTypeSpec) fieldForWrite(key string) (*FieldSpec, error) {
	f, ok := s.fields[key]
	if !ok {
		return nil, &UnknownFieldError{Key: key}
	}
	if f.Deprecated {
		return nil, &ValidationError{Key: key, Reason: "field is deprecated"}
	}
	return f, nil
}

func (s *EntityTypeSpec) CodePolicy(fieldKey string) Policy {
	if f, ok := s.fields[fieldKey]; ok && f.Policy != nil {
		return *f.Policy
	}
	return s.DefaultPolicy
}

func (s *EntityTypeSpec) EffectivePolicy(fieldKey string) Policy {
	p := s.CodePolicy(fieldKey)
	if s.reg == nil {
		return p
	}
	if table := s.reg.overrides.Load(); table != nil {
		p = table.apply(s.Type, fieldKey, p)
	}
	return p
}

type Registry struct {
	types     map[string]*EntityTypeSpec
	overrides atomic.Pointer[overrideTable]
	knownPerm func(key string) bool
}

func NewRegistry() *Registry {
	return &Registry{types: make(map[string]*EntityTypeSpec)}
}

var keyPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

func (r *Registry) Register(spec EntityTypeSpec) error {
	if spec.Family == "" || spec.Type == "" {
		return fmt.Errorf("editing: spec needs family and type")
	}
	if !strings.HasPrefix(spec.Type, spec.Family+".") {
		return fmt.Errorf("editing: type %q must be prefixed by family %q", spec.Type, spec.Family)
	}
	if spec.LoadSnapshot == nil {
		return fmt.Errorf("editing: type %q needs LoadSnapshot", spec.Type)
	}
	if len(spec.Fields) == 0 {
		return fmt.Errorf("editing: type %q registers no fields", spec.Type)
	}
	if _, dup := r.types[spec.Type]; dup {
		return fmt.Errorf("editing: type %q already registered", spec.Type)
	}
	if err := validatePolicy(spec.DefaultPolicy); err != nil {
		return fmt.Errorf("editing: type %q default policy: %w", spec.Type, err)
	}
	requireOwnerHook := func(where string, p Policy) error {
		if p.Automerge == AutomergeOwner && spec.OwnerSite == nil {
			return fmt.Errorf("editing: type %q %s uses automerge=owner but registers no OwnerSite hook", spec.Type, where)
		}
		return nil
	}
	if err := requireOwnerHook("default policy", spec.DefaultPolicy); err != nil {
		return err
	}
	spec.fields = make(map[string]*FieldSpec, len(spec.Fields))
	headTable := ""
	for i := range spec.Fields {
		f := &spec.Fields[i]
		if !keyPattern.MatchString(f.Key) {
			return fmt.Errorf("editing: field key %q is not lowercase-dotted", f.Key)
		}
		if !strings.HasPrefix(f.Key, spec.Type+".") {
			return fmt.Errorf("editing: field key %q must be prefixed by type %q", f.Key, spec.Type)
		}
		if _, dup := spec.fields[f.Key]; dup {
			return fmt.Errorf("editing: duplicate field key %q", f.Key)
		}
		if f.Validate == nil || f.Apply == nil {
			return fmt.Errorf("editing: field %q needs Validate and Apply", f.Key)
		}
		for _, target := range f.Provenance {
			if target.Table == "" || target.Column == "" {
				return fmt.Errorf("editing: field %q provenance target needs a table and a column", f.Key)
			}
			if target.Rows != nil {
				continue
			}
			// A target with no Rows resolver stamps WHERE id = <entity id>, so
			// its table must be the one whose primary key IS the entity id. Two
			// different tables here means one of them is a child table being
			// stamped by the parent's id, which does not fail: catalog_work and
			// catalog_work_character overlap on 73% of production work ids, so
			// the stamp silently lands on another work's row.
			if headTable == "" {
				headTable = target.Table
			} else if target.Table != headTable {
				return fmt.Errorf(
					"editing: field %q stamps %s.%s with the entity id, but %q already stamps %s: "+
						"a target on another table must declare a Rows resolver",
					f.Key, target.Table, target.Column, spec.Type, headTable)
			}
		}
		if f.Identity != nil {
			if err := validateIdentitySpec(*f.Identity); err != nil {
				return fmt.Errorf("editing: field %q identity: %w", f.Key, err)
			}
		}
		if f.Policy != nil {
			if err := validatePolicy(*f.Policy); err != nil {
				return fmt.Errorf("editing: field %q policy: %w", f.Key, err)
			}
			if err := requireOwnerHook(fmt.Sprintf("field %q policy", f.Key), *f.Policy); err != nil {
				return err
			}
		}
		spec.fields[f.Key] = f
	}
	for i := range spec.Fields {
		f := &spec.Fields[i]
		if !strings.HasSuffix(f.Key, SuppressedFieldSuffix) {
			continue
		}
		parent, ok := spec.fields[strings.TrimSuffix(f.Key, SuppressedFieldSuffix)]
		if !ok {
			return fmt.Errorf("editing: field %q suppresses a field that is not registered", f.Key)
		}
		if parent.Identity == nil {
			return fmt.Errorf("editing: field %q suppresses %q, which declares no Identity", f.Key, parent.Key)
		}
		// SuppressedFieldSpec resolved the effective cap (parent's declaration or
		// the 200 default) into the companion before registration. A parent that
		// declared nothing kept 0, and both schema faces published that 0 as "no
		// suppression set" while the companion enforced 200 — copy the resolved
		// cap back so MaxSuppressed reads as the effective limit everywhere.
		if parent.MaxSuppressed <= 0 {
			parent.MaxSuppressed = f.MaxSuppressed
		}
	}
	if spec.Birth != nil {
		if err := validateBirth(&spec, *spec.Birth); err != nil {
			return err
		}
	}
	spec.reg = r
	r.types[spec.Type] = &spec
	return nil
}

func (r *Registry) Type(entityType string) (*EntityTypeSpec, bool) {
	s, ok := r.types[entityType]
	return s, ok
}

func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.types))
	for name := range r.types {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func validatePolicy(p Policy) error {
	switch {
	case p.Propose == ProposeOpen, p.Propose == ProposeTrusted, p.Propose == ProposeLocked:
	case strings.HasPrefix(p.Propose, permPrefix) && len(p.Propose) > len(permPrefix):
	default:
		return fmt.Errorf("bad propose rule %q", p.Propose)
	}
	if !strings.HasPrefix(p.Review, permPrefix) || len(p.Review) == len(permPrefix) {
		return fmt.Errorf("bad review rule %q (must be perm:<key>)", p.Review)
	}
	switch p.Automerge {
	case AutomergeNever, AutomergeTrusted, AutomergeAlways, AutomergeOwner, AutomergeReview, AutomergeTrustedOrReview:
	default:
		return fmt.Errorf("bad automerge rule %q", p.Automerge)
	}
	return nil
}
