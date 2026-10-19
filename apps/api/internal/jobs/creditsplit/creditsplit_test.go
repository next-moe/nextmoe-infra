package creditsplit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/creditsplit")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/creditsplit", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/creditsplit", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/creditsplit", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "w.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadWorklistValidates(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"no detach sources", `{"credit_name_id":1,"detach_sources":[]}`, "no detach_sources"},
		{"bad id", `{"credit_name_id":0,"detach_sources":["eg"]}`, "positive id"},
		{"duplicate row", "{\"credit_name_id\":1,\"detach_sources\":[\"eg\"]}\n{\"credit_name_id\":1,\"detach_sources\":[\"vndb\"]}", "already listed"},
		{"empty", "", "no rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadWorklist(write(t, tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func fixture(t *testing.T) (cnID, personID int64) {
	t.Helper()
	for _, tbl := range []string{"catalog_revision", "catalog_external_ref", "catalog_credit_name", "catalog_person"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	p := model.CatalogPerson{DisplayName: "井上"}
	require.NoError(t, testDB.Create(&p).Error)
	cn := model.CatalogCreditName{Name: "井上", PersonID: &p.ID}
	require.NoError(t, testDB.Create(&cn).Error)
	for _, ref := range []model.CatalogExternalRef{
		{EntityType: model.EntityTypeCreditName, EntityID: cn.ID, SourceID: 3, ExternalID: "b1", LinkKind: model.LinkKindExact, MatchedBy: "import"},
		{EntityType: model.EntityTypeCreditName, EntityID: cn.ID, SourceID: 5, ExternalID: "e9", LinkKind: model.LinkKindExact, MatchedBy: "import"},
	} {
		require.NoError(t, testDB.Create(&ref).Error)
	}
	return cn.ID, p.ID
}

func anchors(t *testing.T, cnID int64) []model.CatalogExternalRef {
	t.Helper()
	var refs []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ?",
		model.EntityTypeCreditName, cnID).Order("source_id").Find(&refs).Error)
	return refs
}

func TestSplitDryThenApplyThenSecondPassIsZeroWrite(t *testing.T) {
	cnID, personID := fixture(t)
	wl := write(t, fmt.Sprintf(`{"credit_name_id":%d,"detach_sources":["eg"],"reason":"bare surname"}`, cnID))
	opts := Opts{DSN: testDSN, WorklistPath: wl}

	dry, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, 1, dry.Rows)
	assert.Equal(t, 1, dry.WouldDropAnchor)
	assert.Equal(t, 1, dry.WouldUnlink)
	assert.Zero(t, dry.AnchorsDropped, "a dry run must write nothing")
	assert.Len(t, anchors(t, cnID), 2)
	require.Len(t, dry.Receipts, 1)
	assert.Equal(t, "erogamescape", dry.Receipts[0].DroppedAnchors[0].SourceKey)

	opts.Apply = true
	got, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, 1, got.AnchorsDropped)
	assert.Equal(t, 1, got.PersonsUnlinked)
	assert.Equal(t, 1, got.Revisions)

	refs := anchors(t, cnID)
	require.Len(t, refs, 1, "only the eg anchor comes off")
	assert.EqualValues(t, 3, refs[0].SourceID)

	var cn model.CatalogCreditName
	require.NoError(t, testDB.First(&cn, cnID).Error)
	assert.Nil(t, cn.PersonID)
	require.NoError(t, testDB.First(&model.CatalogPerson{}, personID).Error)

	var rev model.CatalogRevision
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ?",
		model.EntityTypeCreditName, cnID).First(&rev).Error)
	assert.Equal(t, model.RevisionActionSplit, rev.Action)
	assert.Contains(t, string(rev.Snapshot), "e9",
		"the revision must carry the PRE-split anchor set, which is what makes this reversible")

	second, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Skipped)
	assert.Zero(t, second.AnchorsDropped)
	assert.Zero(t, second.PersonsUnlinked)
	assert.Zero(t, second.Revisions, "a second apply must add no revision")
	var revs int64
	require.NoError(t, testDB.Model(&model.CatalogRevision{}).Count(&revs).Error)
	assert.EqualValues(t, 1, revs)
}

func TestSplitKeepsAHumanAttribution(t *testing.T) {
	cnID, personID := fixture(t)
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit_name SET field_provenance =
		'{"person_id":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}'::jsonb WHERE id = ?`, cnID).Error)
	wl := write(t, fmt.Sprintf(`{"credit_name_id":%d,"detach_sources":["eg"],"reason":"bare surname"}`, cnID))

	got, err := Run(context.Background(), Opts{DSN: testDSN, WorklistPath: wl, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, got.AnchorsDropped, "the anchor still comes off")
	assert.Zero(t, got.PersonsUnlinked, "a person the editor attributed this name to is not undone by a split")
	var cn model.CatalogCreditName
	require.NoError(t, testDB.First(&cn, cnID).Error)
	require.NotNil(t, cn.PersonID)
	assert.Equal(t, personID, *cn.PersonID)
}

func TestSplitRefusesToStripEveryAnchor(t *testing.T) {
	cnID, _ := fixture(t)
	wl := write(t, fmt.Sprintf(`{"credit_name_id":%d,"detach_sources":["bgm","eg"]}`, cnID))
	st, err := Run(context.Background(), Opts{DSN: testDSN, WorklistPath: wl, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Refused)
	assert.Zero(t, st.AnchorsDropped)
	assert.Len(t, anchors(t, cnID), 2)

	wl2 := write(t, fmt.Sprintf(`{"credit_name_id":%d,"detach_sources":["nope"]}`, cnID))
	st2, err := Run(context.Background(), Opts{DSN: testDSN, WorklistPath: wl2, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st2.Refused)
	assert.Contains(t, st2.Refusals[0].Reason, "unknown source key")
}
