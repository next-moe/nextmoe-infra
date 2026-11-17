package historyrepair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/editing"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err == nil {
		err = migrate.Run(db)
	}
	if err == nil {
		err = seed.Run(db)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: test database: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

var ctx = context.Background()

var editor = editing.PolicyContext{UserID: 7, Site: "editor", TrustTier: editing.TrustedTier,
	HasPerm: func(string) bool { return true }}

func fixture(t *testing.T) (*editing.Registry, *editing.Engine) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec(`TRUNCATE edit_revision, edit_proposal, edit_proposal_amendment,
		catalog_work_label, catalog_work_cover, catalog_work, catalog_label RESTART IDENTITY CASCADE`).Error)
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, testDB))
	return reg, editing.NewEngine(testDB, reg)
}

func newWork(t *testing.T) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "w", Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func newLabel(t *testing.T, name string) int64 {
	t.Helper()
	l := model.CatalogLabel{DisplayName: name, Kind: model.LabelKindDoujinCircle}
	require.NoError(t, testDB.Create(&l).Error)
	return l.ID
}

func edit(t *testing.T, e *editing.Engine, workID int64, patch map[string]any) {
	t.Helper()
	_, rev, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: workID, Patch: patch, Actor: editor,
	})
	require.NoError(t, err)
	require.NotNil(t, rev, "the edit must merge directly")
}

func galgameEraRevision(t *testing.T, workID int64, seq int, snapshot map[string]any) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`INSERT INTO edit_revision
		(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, actor_uid, site, created_at)
		VALUES ('catalog', ?, ?, ?, 1, '[]', ?, 1, 'galgame_wiki', ?)`,
		editspec.TypeWork, workID, seq, string(raw), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)).Error)
}

func TestBackfillPriorCorrectsTheFirstEditAfterTheGalgameEra(t *testing.T) {
	reg, e := fixture(t)
	w := newWork(t)
	fox, mango := newLabel(t, "Fox House"), newLabel(t, "Mango Party")
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{WorkID: w, LabelID: fox, Kind: model.WorkLabelKindBrand}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{WorkID: w, ImageHash: "vndb-art", SourceID: model.SourceVNDB}).Error)
	galgameEraRevision(t, w, 1, map[string]any{
		editspec.FieldWorkLabels: []any{map[string]any{"label_id": fox, "kind": int64(model.WorkLabelKindBrand)}},
		editspec.FieldWorkCovers: []any{map[string]any{"image_hash": "vndb-art"}, map[string]any{"image_hash": "wiki-upload"}},
	})
	edit(t, e, w, map[string]any{
		editspec.FieldWorkLabels: []any{map[string]any{"label_id": mango, "kind": int64(model.WorkLabelKindPublisher)}},
		editspec.FieldWorkCovers: []any{map[string]any{"image_hash": "fresh"}},
	})
	edit(t, e, w, map[string]any{editspec.FieldWorkCovers: []any{}})
	require.NoError(t, testDB.Exec(`UPDATE edit_revision SET prior_values = NULL`).Error)

	dry, err := BackfillPrior(ctx, testDB, reg, false)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.Written)
	var untouched int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM edit_revision WHERE prior_values IS NOT NULL`).Scan(&untouched).Error)
	assert.Zero(t, untouched, "a dry run writes nothing")

	st, err := BackfillPrior(ctx, testDB, reg, true)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Written, st.String())

	diffs, err := e.Diff(ctx, editspec.TypeWork, w, 1, 2)
	require.NoError(t, err)
	byKey := map[string]editing.FieldDiff{}
	for _, d := range diffs {
		byKey[d.Key] = d
	}
	assert.JSONEq(t, `[]`, mustJSON(t, byKey[editspec.FieldWorkLabels].From),
		"Fox House is an importer's edge, never the curated lane the edit replaced")
	assert.JSONEq(t, `[{"image_hash":"wiki-upload"}]`, mustJSON(t, byKey[editspec.FieldWorkCovers].From),
		"the VNDB-held cover is dropped; a cover no row holds is kept")

	next, err := e.Diff(ctx, editspec.TypeWork, w, 2, 3)
	require.NoError(t, err)
	require.Len(t, next, 1)
	assert.JSONEq(t, `[{"image_hash":"fresh"}]`, mustJSON(t, next[0].From), "an engine-era predecessor is taken as is")

	again, err := BackfillPrior(ctx, testDB, reg, true)
	require.NoError(t, err)
	assert.Zero(t, again.Written, "a rerun finds nothing to fill")
}

func TestStampUploadsMarksOnlyCatalogEraCuratedCovers(t *testing.T) {
	fixture(t)
	w := newWork(t)
	legacy := model.CatalogWorkCover{WorkID: w, ImageHash: "legacy", SourceID: model.SourceCurated,
		CreatedAt: time.Date(2026, 7, 29, 15, 45, 0, 0, time.UTC)}
	upload := model.CatalogWorkCover{WorkID: w, ImageHash: "upload", SourceID: model.SourceCurated,
		CreatedAt: time.Date(2026, 8, 10, 4, 34, 0, 0, time.UTC)}
	official := model.CatalogWorkCover{WorkID: w, ImageHash: "official", SourceID: model.SourceVNDB, Kind: "main"}
	require.NoError(t, testDB.Create(&[]model.CatalogWorkCover{legacy, upload, official}).Error)

	st, err := StampUploads(ctx, testDB, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Written, st.String())
	assert.Equal(t, []int64{w}, st.Works)

	var stamps []struct {
		ImageHash string
		At        *string
	}
	require.NoError(t, testDB.Raw(`SELECT image_hash, field_provenance -> ? -> 0 ->> 'at' AS at
		FROM catalog_work_cover WHERE work_id = ? ORDER BY image_hash`, model.CoverUploadStampColumn, w).Scan(&stamps).Error)
	require.Len(t, stamps, 3)
	assert.Nil(t, stamps[0].At, "legacy")
	assert.Nil(t, stamps[1].At, "official")
	require.NotNil(t, stamps[2].At)
	assert.Equal(t, "2026-08-10T04:34:00Z", *stamps[2].At, "the stamp dates from the upload, not the repair")

	again, err := StampUploads(ctx, testDB, true)
	require.NoError(t, err)
	assert.Zero(t, again.Written)
}

func TestRestoreLostCoversPutsBackOnlyEditorUploads(t *testing.T) {
	_, e := fixture(t)
	w := newWork(t)
	galgameEraRevision(t, w, 1, map[string]any{
		editspec.FieldWorkCovers: []any{map[string]any{"image_hash": "wiki-era"}},
	})
	edit(t, e, w, map[string]any{editspec.FieldWorkCovers: []any{
		map[string]any{"image_hash": "wiki-era"},
		map[string]any{"image_hash": "user-upload", "kind": "main", "sexual": int64(1)},
	}})
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_cover WHERE work_id = ?`, w).Error)

	dry, err := RestoreLostCovers(ctx, testDB, e, editor, false)
	require.NoError(t, err)
	assert.Equal(t, 1, dry.Scanned, "only the cover an engine revision added counts; the galgame-era one was meant to go")
	var rows int64
	require.NoError(t, testDB.Model(&model.CatalogWorkCover{}).Where("work_id = ?", w).Count(&rows).Error)
	assert.Zero(t, rows, "a dry run writes nothing")

	st, err := RestoreLostCovers(ctx, testDB, e, editor, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Written, st.String())

	var got model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ? AND image_hash = ?", w, "user-upload").Take(&got).Error)
	assert.Equal(t, model.SourceCurated, got.SourceID)
	assert.Equal(t, "main", got.Kind)
	assert.Equal(t, int16(1), got.Sexual)
	assert.Contains(t, string(got.FieldProvenance), model.CoverUploadStampColumn, "a restored upload is an upload")

	var note string
	require.NoError(t, testDB.Raw(`SELECT p.note FROM edit_revision r JOIN edit_proposal p ON p.id = r.proposal_id
		WHERE r.entity_id = ? AND r.seq = 3`, w).Scan(&note).Error)
	assert.Equal(t, restoreNote, note, "the restore is a revision of its own")

	again, err := RestoreLostCovers(ctx, testDB, e, editor, true)
	require.NoError(t, err)
	assert.Zero(t, again.Scanned)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
