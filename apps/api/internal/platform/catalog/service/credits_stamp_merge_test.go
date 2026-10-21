package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stampedNote = `{"note":[{"source":"curated","at":"2026-10-21T00:00:00Z"}],
	"spoiler":[{"source":"curated","at":"2026-10-21T00:00:00Z"}]}`

func stampCredit(t *testing.T, id int64, note string, spoiler int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit SET note = ?, spoiler = ?, field_provenance = ?::jsonb
		WHERE id = ?`, note, spoiler, stampedNote, id).Error)
}

func creditOn(t *testing.T, where string, args ...any) model.CatalogCredit {
	t.Helper()
	var c model.CatalogCredit
	require.NoError(t, testDB.Where(where, args...).First(&c).Error)
	return c
}

func stampHead(t *testing.T, id int64, column string) string {
	t.Helper()
	var head string
	require.NoError(t, testDB.Raw(`SELECT COALESCE(field_provenance -> ? -> 0 ->> 'source', '')
		FROM catalog_credit WHERE id = ?`, column, id).Scan(&head).Error)
	return head
}

func TestMergeCarriesCorrectedCreditColumns(t *testing.T) {
	t.Run("work axis", func(t *testing.T) {
		cleanTables(t)
		role := seededRoleID(t)
		name := createCreditName(t, nil, "原画家")
		target, source := createWork(t, "同じ作品"), createWork(t, "同じ 作品")
		kept := createCredit(t, target.ID, name.ID, role, nil)
		lost := createCredit(t, source.ID, name.ID, role, nil)
		stampCredit(t, lost.ID, "仅限 FD", model.SpoilerSevere)

		executeMerge(t, model.EntityTypeWork, source.ID, target.ID, "same work")

		got := creditOn(t, "id = ?", kept.ID)
		assert.Equal(t, "仅限 FD", got.Note)
		assert.Equal(t, model.SpoilerSevere, got.Spoiler)
		assert.Equal(t, "curated", stampHead(t, kept.ID, "note"))
		assert.Equal(t, "", stampHead(t, kept.ID, "release_id"), "an unstamped column carries no stamp over")
	})

	t.Run("credit-name axis keeps the survivor's own correction", func(t *testing.T) {
		cleanTables(t)
		role := seededRoleID(t)
		work := createWork(t, "作品")
		target, source := createCreditName(t, nil, "表記"), createCreditName(t, nil, "表記(旧)")
		kept := createCredit(t, work.ID, target.ID, role, nil)
		lost := createCredit(t, work.ID, source.ID, role, nil)
		stampCredit(t, kept.ID, "本人确认", model.SpoilerNone)
		stampCredit(t, lost.ID, "旧备注", model.SpoilerMild)

		executeMerge(t, model.EntityTypeCreditName, source.ID, target.ID, "same name")

		got := creditOn(t, "id = ?", kept.ID)
		assert.Equal(t, "本人确认", got.Note, "the survivor's own correction wins")
		assert.Equal(t, model.SpoilerNone, got.Spoiler)
	})

	t.Run("character axis", func(t *testing.T) {
		cleanTables(t)
		var va int64
		require.NoError(t, testDB.Raw(`SELECT id FROM catalog_role WHERE key = 'voice-actor'`).Scan(&va).Error)
		work := createWork(t, "作品")
		name := createCreditName(t, nil, "声優")
		target, source := createCharacter(t, "主人公"), createCharacter(t, "主人公(重複)")
		kept := createCredit(t, work.ID, name.ID, va, &target.ID)
		lost := createCredit(t, work.ID, name.ID, va, &source.ID)
		stampCredit(t, lost.ID, "幼少期", model.SpoilerMild)

		executeMerge(t, model.EntityTypeCharacter, source.ID, target.ID, "same character")

		got := creditOn(t, "id = ?", kept.ID)
		assert.Equal(t, "幼少期", got.Note)
		assert.Equal(t, model.SpoilerMild, got.Spoiler)
	})
}
