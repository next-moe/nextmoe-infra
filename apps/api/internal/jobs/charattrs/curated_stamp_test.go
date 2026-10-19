package charattrs

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCuratedStampBlocksTheAttributePipeline(t *testing.T) {
	clean(t)
	ctx := context.Background()
	r := reg(t)

	ch := mkChar(t, "engine-edited")
	mkVNDB(t, "c1", "f", "a", "", 0, 175, 0, 0, 0, nil)
	mkAnchor(t, ch, r.vndbSource, "c1", "rule:vndb-character-import", model.LinkKindExact)

	registry := editing.NewRegistry()
	require.NoError(t, editspec.RegisterCharacter(registry, testDB))
	spec, ok := registry.Type(editspec.TypeCharacter)
	require.True(t, ok)
	height, ok := spec.Field(editspec.FieldCharacterHeightCm)
	require.True(t, ok)
	require.NoError(t, editing.ApplyField(ctx, testDB, height, ch, float64(163)))
	require.Equal(t, "curated", provSource(t, ch, "height_cm"))

	_, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)

	after := loadChar(t, ch)
	assert.Equal(t, int16(163), deref(after.Height), "vndb must not overwrite an engine-edited height")
	assert.Equal(t, "curated", provSource(t, ch, "height_cm"))
	assert.Equal(t, model.BloodTypeA, deref(after.Blood), "an unstamped column still fills from vndb")
}

// The test above once stood as proof that pipelineRank alone protects the
// engine's columns. It only covered a non-empty value: decide() filled an empty
// column before reading its head source, so a value a person cleared came back
// on the next run.
func TestCuratedStampKeepsAClearedColumnEmpty(t *testing.T) {
	clean(t)
	ctx := context.Background()
	r := reg(t)

	ch := mkChar(t, "engine-cleared")
	mkVNDB(t, "c1", "f", "a", "", 0, 175, 0, 0, 0, nil)
	mkAnchor(t, ch, r.vndbSource, "c1", "rule:vndb-character-import", model.LinkKindExact)

	registry := editing.NewRegistry()
	require.NoError(t, editspec.RegisterCharacter(registry, testDB))
	spec, ok := registry.Type(editspec.TypeCharacter)
	require.True(t, ok)
	height, ok := spec.Field(editspec.FieldCharacterHeightCm)
	require.True(t, ok)
	require.NoError(t, editing.ApplyField(ctx, testDB, height, ch, nil))
	require.Equal(t, "curated", provSource(t, ch, "height_cm"))

	_, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)

	after := loadChar(t, ch)
	assert.Nil(t, after.Height, "vndb must not refill a height a person cleared")
	assert.Equal(t, "curated", provSource(t, ch, "height_cm"))
	assert.Equal(t, model.BloodTypeA, deref(after.Blood), "an unstamped column still fills from vndb")
}
