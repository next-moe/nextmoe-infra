package getchuchars

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAttrsLeavesAHumanClearedColumnEmpty(t *testing.T) {
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec("TRUNCATE catalog_character RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()

	height := int16(160)
	ch := model.CatalogCharacter{DisplayName: "美咲", HeightCm: &height}
	require.NoError(t, testDB.Create(&ch).Error)

	registry := editing.NewRegistry()
	require.NoError(t, editspec.RegisterCharacter(registry, testDB))
	spec, ok := registry.Type(editspec.TypeCharacter)
	require.True(t, ok)
	field, ok := spec.Field(editspec.FieldCharacterHeightCm)
	require.True(t, ok)
	require.NoError(t, editing.ApplyField(ctx, testDB, field, ch.ID, nil))

	st := &Stats{}
	writeAttrs(ctx, testDB, provenanceSource, Candidate{
		CharacterID: ch.ID,
		Attrs:       []byte(`{"身長":"158cm","体重":"45kg"}`),
	}, true, st)

	var got model.CatalogCharacter
	require.NoError(t, testDB.First(&got, ch.ID).Error)
	assert.Nil(t, got.HeightCm, "getchu must not refill a height a person cleared")
	require.NotNil(t, got.WeightKg, "an unstamped empty column still fills")
	assert.Equal(t, int16(45), *got.WeightKg)
	assert.Equal(t, "curated", provenance.FirstSource(got.FieldProvenance, "height_cm"))
	assert.Equal(t, provenanceSource, provenance.FirstSource(got.FieldProvenance, "weight_kg"))
}
