package llmsuggest

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDossierSpellsOutTheCodesItShips(t *testing.T) {
	assert.True(t, strings.HasPrefix(entityTypeName(model.EntityTypeLabel), "Label"),
		"a judge reading a bare 3 called it a work")
	assert.True(t, strings.HasPrefix(entityTypeName(model.EntityTypeWork), "Work"))
	assert.Equal(t, "company", bgmPersonTypeName(2), "a judge reading a bare 2 called it a character")
	assert.Equal(t, "individual", bgmPersonTypeName(1))
	assert.Equal(t, "company", vndbProducerTypeName("co"))
	assert.Contains(t, entityTypeName(99), "99")
}

func TestLLMFamilyCoversEveryDossierBuilder(t *testing.T) {
	for _, et := range []int16{model.EntityTypeWork, model.EntityTypeLabel,
		model.EntityTypeCharacter, model.EntityTypeCreditName} {
		assert.True(t, llmFamily(et), entityTypeName(et))
	}
	assert.False(t, llmFamily(model.EntityTypeRelease), "release has no dossier builder")
	assert.False(t, llmFamily(model.EntityTypeTag))
}

func TestRefContextCarriesWhatTheLabelPublishes(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_external_ref, catalog_work, catalog_label RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var vndb, dlsite int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsite).Error)
	reg, err := loadSourceReg(db)
	require.NoError(t, err)

	mkLabel := func(name string) int64 {
		l := &model.CatalogLabel{DisplayName: name}
		require.NoError(t, db.Create(l).Error)
		return l.ID
	}
	attach := func(labelID int64, title string) {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: title,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		require.NoError(t, db.Create(&model.CatalogWorkLabel{WorkID: w.ID, LabelID: labelID}).Error)
	}

	published := mkLabel("Altair")
	attach(published, "Altair Game A")
	attach(published, "Altair Game B")
	require.NoError(t, db.Create(&model.CatalogLabelAlias{LabelID: published, Name: "Altair Soft"}).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeLabel, EntityID: published, SourceID: vndb,
		ExternalID: "p100", LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)

	// negative control: the same shape with nothing attached. Without it a
	// context loader that writes its fields unconditionally still passes.
	bare := mkLabel("Altair")

	items := []refItem{
		{EntityType: model.EntityTypeLabel, EntityID: published, SourceID: dlsite, ExternalID: "RJ1", Hash: "h1"},
		{EntityType: model.EntityTypeLabel, EntityID: bare, SourceID: dlsite, ExternalID: "RJ2", Hash: "h2"},
	}
	catalog := map[string]map[string]any{
		entityKey(model.EntityTypeLabel, published): {"id": published, "name": "Altair"},
		entityKey(model.EntityTypeLabel, bare):      {"id": bare, "name": "Altair"},
	}
	source := map[string]map[string]any{"h1": {"id": "RJ1"}, "h2": {"id": "RJ2"}}
	require.NoError(t, attachRefContext(db, nil, reg, items, catalog, source))

	got := catalog[entityKey(model.EntityTypeLabel, published)]
	assert.Equal(t, int64(2), got["works"])
	assert.ElementsMatch(t, []string{"Altair Game A", "Altair Game B"}, got["sample_works"])
	assert.Equal(t, []string{"Altair Soft"}, got["aliases"])
	assert.Equal(t, []string{"vndb:p100"}, got["already_linked"])

	empty := catalog[entityKey(model.EntityTypeLabel, bare)]
	assert.Equal(t, int64(0), empty["works"])
	assert.NotContains(t, empty, "sample_works")
	assert.NotContains(t, empty, "aliases")
	assert.NotContains(t, empty, "already_linked")
}
