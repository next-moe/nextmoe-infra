package main

import (
	"bytes"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDedupBatchKeepsCuratedCredits(t *testing.T) {
	for _, tbl := range []string{
		"catalog_credit", "catalog_credit_name", "catalog_character", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	var vaRole int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_role WHERE key = 'voice-actor'`).Scan(&vaRole).Error)
	require.NotZero(t, vaRole)

	work := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "作品",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&work).Error)
	ch := model.CatalogCharacter{DisplayName: "キャラ"}
	require.NoError(t, testDB.Create(&ch).Error)

	mkName := func(name string) int64 {
		cn := model.CatalogCreditName{Name: name, Lang: "ja"}
		require.NoError(t, testDB.Create(&cn).Error)
		return cn.ID
	}
	curatedName, upstreamName, stampedName := mkName("人手声優"), mkName("上流声優"), mkName("修正声優")
	curated, upstream := int16(12), int16(2)

	mk := func(nameID int64, charID *int64, source int16) int64 {
		c := model.CatalogCredit{WorkID: work.ID, CreditNameID: nameID, RoleID: vaRole,
			CharacterID: charID, SourceID: &source}
		require.NoError(t, testDB.Create(&c).Error)
		return c.ID
	}
	// Both names hold a characterless VA credit that an upstream row with a
	// character makes redundant — the exact shape runCleanup deletes.
	curatedRedundant := mk(curatedName, nil, curated)
	upstreamRedundant := mk(upstreamName, nil, upstream)
	stampedRedundant := mk(stampedName, nil, upstream)
	mk(curatedName, &ch.ID, upstream)
	mk(upstreamName, &ch.ID, upstream)
	mk(stampedName, &ch.ID, upstream)
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit SET note = '仅限 FD',
		field_provenance = '{"note":[{"source":"curated","at":"2026-10-21T00:00:00Z"}]}'
		WHERE id = ?`, stampedRedundant).Error)

	var out bytes.Buffer
	require.NoError(t, runCleanup(testDB, &out, false))
	assert.Contains(t, out.String(), "would_delete=1 kept_human=2",
		"the dry run must say out loud that it is stepping around hand-written and corrected rows")

	out.Reset()
	require.NoError(t, runCleanup(testDB, &out, true))
	assert.Contains(t, out.String(), "deleted=1 kept_human=2")

	var ids []int64
	require.NoError(t, testDB.Model(&model.CatalogCredit{}).
		Where("character_id IS NULL").Order("id").Pluck("id", &ids).Error)
	assert.Equal(t, []int64{curatedRedundant, stampedRedundant}, ids,
		"the hand-written row and the corrected imported row survive; only the untouched upstream one is deleted")
	var gone int64
	require.NoError(t, testDB.Model(&model.CatalogCredit{}).Where("id = ?", upstreamRedundant).Count(&gone).Error)
	assert.Zero(t, gone)
}
