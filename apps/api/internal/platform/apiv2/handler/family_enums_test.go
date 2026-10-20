package handler

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
)

func enumOf(t *testing.T, v any, path ...string) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	var field reflect.StructField
	for _, name := range path {
		f, ok := typ.FieldByName(name)
		require.Truef(t, ok, "%s has no field %s", typ, name)
		field, typ = f, f.Type
	}
	raw := field.Tag.Get("enum")
	require.NotEmptyf(t, raw, "%s.%s declares no enum", reflect.TypeOf(v), strings.Join(path, "."))
	return strings.Split(raw, ",")
}

// A family registered with the editing engine but missing from one of these
// enums is served by the code and refused by the published schema: wave E2b
// shipped catalog.person and catalog.credit_name without them.
func TestFamilyEnumsCoverEveryEditingType(t *testing.T) {
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, nil))

	objectEnums := map[string][]string{
		"EditCapabilities.target_object": enumOf(t, repr.EditCapabilities{}, "TargetObject"),
		"ObjectSchema.target_object":     enumOf(t, repr.ObjectSchema{}, "TargetObject"),
		"ProposalRecord.target_object":   enumOf(t, repr.ProposalRecord{}, "TargetObject"),
		"Revision.target_object":         enumOf(t, repr.Revision{}, "TargetObject"),
		"EditContext.target_object":      enumOf(t, repr.EditContext{}, "TargetObject"),
		"GET schemas {object}":           enumOf(t, getSchemaInput{}, "Object"),
		"GET edit-capabilities {object}": enumOf(t, getCapabilitiesInput{}, "Object"),
		"GET edit-context {object}":      enumOf(t, getEditContextInput{}, "Object"),
	}
	typeEnums := map[string][]string{
		"EditCapabilities.entity_type": enumOf(t, repr.EditCapabilities{}, "EntityType"),
		"ObjectSchema.entity_type":     enumOf(t, repr.ObjectSchema{}, "EntityType"),
	}
	creatable := enumOf(t, createCreationInput{}, "Body", "Object")

	for _, entityType := range reg.Types() {
		object := schemaObject(entityType)
		require.NotEmptyf(t, object, "%s has no public family name", entityType)
		require.Equalf(t, entityType, schemaEntityType(object), "%s does not round-trip through %q", entityType, object)
		for name, enum := range objectEnums {
			require.Containsf(t, enum, object, "%s is missing %q", name, object)
		}
		for name, enum := range typeEnums {
			require.Containsf(t, enum, entityType, "%s is missing %q", name, entityType)
		}
		spec, _ := reg.Type(entityType)
		if spec.Birth != nil {
			require.Containsf(t, creatable, object, "%s can be created but POST /v2/me/creations does not list %q", entityType, object)
		}
	}
	for _, object := range creatable {
		spec, ok := reg.Type(schemaEntityType(object))
		require.Truef(t, ok, "creations list %q, which is not a registered family", object)
		require.NotNilf(t, spec.Birth, "creations list %q, which has no birth spec", object)
	}
	require.False(t, slices.Contains(creatable, "work"), "works are minted through claims")
}
