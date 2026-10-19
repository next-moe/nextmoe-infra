package jobs

import (
	"context"
	"testing"

	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func dsnEnv(t *testing.T) string {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	return dsn
}

func openEditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsnEnv(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	var present bool
	require.NoError(t, db.Raw(`SELECT to_regclass('edit_revision') IS NOT NULL`).Scan(&present).Error)
	if !present {
		t.Skip("TEST_DATABASE_DSN has no engine tables")
	}
	require.NoError(t, db.Exec(`TRUNCATE edit_revision, edit_proposal RESTART IDENTITY CASCADE`).Error)
	return db
}

const (
	hRev    = "6666666666666666666666666666666666666666666666666666666666666666"
	hPatch  = "7777777777777777777777777777777777777777777777777777777777777777"
	hLegacy = "8888888888888888888888888888888888888888888888888888888888888888"
	hShot   = "9999999999999999999999999999999999999999999999999999999999999999"
)

func TestRefpingEngineLaneMatchesBothKeySpellings(t *testing.T) {
	db := openEditTestDB(t)
	ctx := context.Background()

	require.NoError(t, db.Exec(`
		INSERT INTO edit_revision
			(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, actor_uid, site, created_at)
		VALUES ('galgame', 'galgame.game', 1, 1, 0, '[]'::jsonb, ?::jsonb, 1, 'kungal', now())`,
		`{"galgame.game.covers":[{"image_hash":"`+hRev+`"}]}`).Error)

	require.NoError(t, db.Exec(`
		INSERT INTO edit_revision
			(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, actor_uid, site, created_at)
		VALUES ('catalog', 'catalog.work', 2, 1, 0, '[]'::jsonb, ?::jsonb, 1, 'kungal', now())`,
		`{"catalog.work.screenshots":[{"image_hash":"`+hShot+`"}]}`).Error)

	require.NoError(t, db.Exec(`
		INSERT INTO edit_proposal
			(entity_family, entity_type, entity_id, base_revision_seq, patch, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('catalog', 'catalog.work', 3, 0, ?::jsonb, 1, '', 'kungal', 0, '', now(), now())`,
		`{"catalog.work.covers":[{"image_hash":"`+hPatch+`"}]}`).Error)

	require.NoError(t, db.Exec(`
		INSERT INTO edit_proposal
			(entity_family, entity_type, entity_id, base_revision_seq, patch, legacy_meta, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('galgame', 'galgame.game', 4, 0, '{}'::jsonb, ?::jsonb, 1, '', 'kungal', 1, '', now(), now())`,
		`{"snapshot":{"covers":[{"image_hash":"`+hLegacy+`"}]}}`).Error)

	got, err := collectEditRefpingHashes(ctx, db)
	require.NoError(t, err)
	set := make(map[string]bool, len(got))
	for _, h := range got {
		set[h] = true
	}
	assert.True(t, set[hRev], "pre-rekey revision hash")
	assert.True(t, set[hShot], "post-rekey revision hash")
	assert.True(t, set[hPatch], "open proposal's proposed hash")
	assert.True(t, set[hLegacy], "archived legacy snapshot hash")
	assert.Len(t, got, 4, "no duplicates, nothing invented")
}

func TestRefpingKeepsACoverOnlyTheBaselineHolds(t *testing.T) {
	db := openEditTestDB(t)
	ctx := context.Background()

	const hBase = "5555555555555555555555555555555555555555555555555555555555555555"
	require.NoError(t, db.Exec(`
		INSERT INTO edit_revision
			(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, baseline_snapshot, actor_uid, site, created_at)
		VALUES ('catalog', 'catalog.work', 5, 1, 0, '["catalog.work.covers"]'::jsonb, ?::jsonb, ?::jsonb, 1, 'kungal', now())`,
		`{"catalog.work.covers":[]}`, `{"catalog.work.covers":[{"image_hash":"`+hBase+`"}]}`).Error)

	got, err := collectEditRefpingHashes(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{hBase}, got)
}

func TestRefpingKeepsScalarImageFieldsAlive(t *testing.T) {
	db := openEditTestDB(t)
	ctx := context.Background()

	const (
		hPortrait = "4444444444444444444444444444444444444444444444444444444444444444"
		hFigure   = "3333333333333333333333333333333333333333333333333333333333333333"
		hLogo     = "2222222222222222222222222222222222222222222222222222222222222222"
	)
	require.NoError(t, db.Exec(`
		INSERT INTO edit_revision
			(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, baseline_snapshot, actor_uid, site, created_at)
		VALUES ('catalog', 'catalog.character', 7, 1, 0, '["catalog.character.image_hash"]'::jsonb, ?::jsonb, ?::jsonb, 1, 'kungal', now())`,
		`{"catalog.character.image_hash":null,"catalog.character.figure_hash":"`+hFigure+`"}`,
		`{"catalog.character.image_hash":"`+hPortrait+`"}`).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO edit_proposal
			(entity_family, entity_type, entity_id, base_revision_seq, patch, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('catalog', 'catalog.label', 8, 0, ?::jsonb, 1, '', 'kungal', 0, '', now(), now())`,
		`{"catalog.label.logo_hash":"`+hLogo+`"}`).Error)

	got, err := collectEditRefpingHashes(ctx, db)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{hPortrait, hFigure, hLogo}, got)
}

func TestRefpingKeepsAPendingPersonPhoto(t *testing.T) {
	db := openEditTestDB(t)
	ctx := context.Background()

	const hPhoto = "4444444444444444444444444444444444444444444444444444444444444444"
	require.NoError(t, db.Exec(`
		INSERT INTO edit_proposal
			(entity_family, entity_type, entity_id, base_revision_seq, patch, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('catalog', 'catalog.person', 7, 0, ?::jsonb, 1, '', 'editor', 0, '', now(), now())`,
		`{"catalog.person.photo":"`+hPhoto+`"}`).Error)

	got, err := collectEditRefpingHashes(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{hPhoto}, got, "an uploaded photo waiting for review must outlive the image TTL")
}

func TestRefpingEngineLaneSkipsOtherFamilies(t *testing.T) {
	db := openEditTestDB(t)
	ctx := context.Background()

	require.NoError(t, db.Exec(`
		INSERT INTO edit_revision
			(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, actor_uid, site, created_at)
		VALUES ('catalog', 'catalog.character', 9, 1, 0, '[]'::jsonb, ?::jsonb, 1, 'kungal', now())`,
		`{"catalog.work.covers":[{"image_hash":"`+hRev+`"}]}`).Error)

	got, err := collectEditRefpingHashes(ctx, db)
	require.NoError(t, err)
	assert.Empty(t, got)
}
