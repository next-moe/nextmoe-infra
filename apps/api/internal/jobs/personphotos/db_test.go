package personphotos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	testBangumiSource int16 = 3
	testOtherSource   int16 = 14
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_CATALOG_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_CATALOG_DATABASE_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.CatalogSource{}, &model.CatalogPerson{}, &model.CatalogExternalRef{}))
	require.NoError(t, db.Exec(`TRUNCATE catalog_person, catalog_external_ref RESTART IDENTITY CASCADE`).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO catalog_source (id, key, trust_tier) VALUES (?,'bangumi',1), (?,'cien',2)
		 ON CONFLICT (id) DO NOTHING`, testBangumiSource, testOtherSource).Error)
	return db
}

func mkPerson(t *testing.T, db *gorm.DB, name, photo string) int64 {
	t.Helper()
	p := &model.CatalogPerson{DisplayName: name, PhotoHash: photo}
	require.NoError(t, db.Create(p).Error)
	return p.ID
}

func mkAnchor(t *testing.T, db *gorm.DB, entityType int16, entityID int64, source int16, extID string, kind int16) {
	t.Helper()
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: source,
		ExternalID: extID, LinkKind: kind, MatchedBy: "import:test",
	}).Error)
}

func TestDBResolveSourceID(t *testing.T) {
	db := openTestDB(t)
	got, err := resolveSourceID(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, testBangumiSource, got)
}

func TestDBLoadCandidates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	want := mkPerson(t, db, "wants a photo", "")
	mkAnchor(t, db, model.EntityTypePerson, want, testBangumiSource, "100", model.LinkKindExact)

	filled := mkPerson(t, db, "already has one", "deadbeef")
	mkAnchor(t, db, model.EntityTypePerson, filled, testBangumiSource, "101", model.LinkKindExact)

	probable := mkPerson(t, db, "probable anchor only", "")
	mkAnchor(t, db, model.EntityTypePerson, probable, testBangumiSource, "102", model.LinkKindProbable)

	related := mkPerson(t, db, "related anchor only", "")
	mkAnchor(t, db, model.EntityTypePerson, related, testBangumiSource, "103", model.LinkKindRelated)

	otherSource := mkPerson(t, db, "anchored elsewhere", "")
	mkAnchor(t, db, model.EntityTypePerson, otherSource, testOtherSource, "200", model.LinkKindExact)

	nameAnchored := mkPerson(t, db, "only its credit name is anchored", "")
	mkAnchor(t, db, model.EntityTypeCreditName, nameAnchored, testBangumiSource, "300", model.LinkKindExact)

	unanchored := mkPerson(t, db, "no anchor at all", "")
	_ = unanchored

	deleted := mkPerson(t, db, "soft deleted", "")
	mkAnchor(t, db, model.EntityTypePerson, deleted, testBangumiSource, "104", model.LinkKindExact)
	require.NoError(t, db.Delete(&model.CatalogPerson{}, deleted).Error)

	twice := mkPerson(t, db, "double anchored", "")
	mkAnchor(t, db, model.EntityTypePerson, twice, testBangumiSource, "401", model.LinkKindExact)
	mkAnchor(t, db, model.EntityTypePerson, twice, testBangumiSource, "400", model.LinkKindExact)

	got, err := loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)

	byID := map[int64]string{}
	for _, c := range got {
		byID[c.PersonID] = c.ExternalID
	}
	assert.Equal(t, map[int64]string{want: "100", twice: "400"}, byID,
		"only live, photo-less persons with an EXACT bangumi PERSON anchor, one row each")

	require.NoError(t, db.Exec(
		`UPDATE catalog_person SET field_provenance = '{"display_name":[{"source":"vndb","at":"2020-01-01T00:00:00Z"}]}'::jsonb WHERE id = ?`,
		want).Error)
	got, err = loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)
	for _, c := range got {
		if c.PersonID == want {
			assert.Contains(t, string(c.FieldProvenance), "vndb")
		}
	}
}

func TestDBFillWritesPhotoAndProvenance(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	id := mkPerson(t, db, "麻枝准", "")
	require.NoError(t, db.Exec(
		`UPDATE catalog_person SET field_provenance = '{"display_name":[{"source":"vndb","at":"2020-01-01T00:00:00Z"}]}'::jsonb WHERE id = ?`,
		id).Error)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "600", "logo.png"))
	m, err := loadMirror(root)
	require.NoError(t, err)

	cands, err := loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)
	require.Empty(t, cands, "no anchor yet")
	mkAnchor(t, db, model.EntityTypePerson, id, testBangumiSource, "600", model.LinkKindExact)
	cands, err = loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)
	require.Len(t, cands, 1)

	fake := &fakeUploader{hash: "1111111111111111111111111111111111111111111111111111111111111111"}
	r := &runner{db: db, cli: fake, mirror: m, stats: &Stats{}}
	res := r.fill(ctx, cands[0], true)
	assert.Equal(t, 1, res.uploaded)
	assert.Equal(t, fake.hash, res.hash)

	var row struct {
		PhotoHash       string `gorm:"column:photo_hash"`
		FieldProvenance string `gorm:"column:field_provenance"`
	}
	require.NoError(t, db.Raw(`SELECT photo_hash, field_provenance::text AS field_provenance FROM catalog_person WHERE id = ?`, id).Scan(&row).Error)
	assert.Equal(t, fake.hash, row.PhotoHash)
	assert.Contains(t, row.FieldProvenance, `"photo_hash"`)
	assert.Contains(t, row.FieldProvenance, `"bangumi"`)
	assert.Contains(t, row.FieldProvenance, `"vndb"`, "other fields' provenance must survive")

	r2 := &runner{db: db, cli: &fakeUploader{hash: "2222222222222222222222222222222222222222222222222222222222222222"},
		mirror: m, stats: &Stats{}}
	res = r2.fill(ctx, cands[0], true)
	assert.Equal(t, 1, res.raced, "losing the race is an ordinary outcome, not an error")

	require.NoError(t, db.Raw(`SELECT photo_hash, field_provenance::text AS field_provenance FROM catalog_person WHERE id = ?`, id).Scan(&row).Error)
	assert.Equal(t, fake.hash, row.PhotoHash, "the first writer keeps the slot")

	cands, err = loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)
	assert.Empty(t, cands, "a filled person is skipped before any byte read")
}

func TestDBLeavesAHumanClearedPhotoEmpty(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	cleared := mkPerson(t, db, "cleared in the editor", "")
	require.NoError(t, db.Exec(
		`UPDATE catalog_person SET field_provenance = '{"photo_hash":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}'::jsonb WHERE id = ?`,
		cleared).Error)
	mkAnchor(t, db, model.EntityTypePerson, cleared, testBangumiSource, "700", model.LinkKindExact)

	cands, err := loadCandidates(ctx, db, testBangumiSource)
	require.NoError(t, err)
	assert.Empty(t, cands, "a photo a person removed is not a gap to fill")

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "700", "logo.png"))
	m, err := loadMirror(root)
	require.NoError(t, err)
	r := &runner{db: db, cli: &fakeUploader{hash: "3333333333333333333333333333333333333333333333333333333333333333"},
		mirror: m, stats: &Stats{}}
	res := r.fill(ctx, candidate{PersonID: cleared, ExternalID: "700"}, true)
	assert.Equal(t, 1, res.raced, "a stale candidate list must not write past the human stamp either")
	var photo string
	require.NoError(t, db.Raw(`SELECT photo_hash FROM catalog_person WHERE id = ?`, cleared).Scan(&photo).Error)
	assert.Empty(t, photo)
}
