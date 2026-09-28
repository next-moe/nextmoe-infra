package storeanchors

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: cannot connect: %v\n", err)
		default:
			if err := migrate.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog migrate failed: %v\n", err)
			} else if err := srcvndb.EnsureSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: src_vndb migrate failed: %v\n", err)
			} else if err := seed.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog seed failed: %v\n", err)
			} else {
				testDB = db
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "DB TESTS SKIPPED: TEST_DATABASE_DSN is unset")
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skipf(t, "the catalog test database is unavailable")
	}
	clean(t)
	return testDB
}

func clean(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{
		"catalog_match_rejection", "catalog_external_ref", "catalog_release", "catalog_work",
		"src_vndb.releases_extlinks", "src_vndb.extlinks",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "the seed must carry the %q source row", key)
	return id
}

func fixture(t *testing.T, name, vndbReleaseID string, tier int16, links map[string]string) (int64, int64) {
	t.Helper()
	var medium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key='galgame'`).Scan(&medium).Error)

	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	rel := model.CatalogRelease{WorkID: w.ID, Kind: 0}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: sourceID(t, "vndb"),
		ExternalID: vndbReleaseID, LinkKind: tier, MatchedBy: "import:test"}).Error)

	for site, value := range links {
		addLink(t, vndbReleaseID, site, value)
	}
	return rel.ID, w.ID
}

func addLink(t *testing.T, vndbReleaseID, site, value string) {
	t.Helper()
	var id int
	require.NoError(t, testDB.Raw(
		`INSERT INTO src_vndb.extlinks (id, site, value)
		 VALUES ((SELECT coalesce(max(id),0)+1 FROM src_vndb.extlinks), ?, ?) RETURNING id`,
		site, value).Scan(&id).Error)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_vndb.releases_extlinks (id, link) VALUES (?, ?)`, vndbReleaseID, id).Error)
}

func workUpdatedAt(t *testing.T, workID int64) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, workID).Scan(&ts).Error)
	return ts
}

func refsFor(t *testing.T, entityID int64, source int16) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ? AND source_id = ?",
		model.EntityTypeRelease, entityID, source).Order("external_id").Find(&out).Error)
	return out
}

func TestLanesWriteExactReleaseAnchors(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	steam, dmm, dlsite := sourceID(t, "steam"), sourceID(t, "dmm"), sourceID(t, "dlsite")

	relSteam, workSteam := fixture(t, "steam-release", "r100", model.LinkKindExact,
		map[string]string{"steam": "2258770"})
	relDmm, _ := fixture(t, "dmm-release", "r200", model.LinkKindExact,
		map[string]string{"dmm": "www.dmm.co.jp/mono/pcgame/-/detail/=/cid=2212apc13900/"})
	relDl, _ := fixture(t, "dlsite-release", "r300", model.LinkKindExact,
		map[string]string{"dlsite": "RJ264149"})
	relEN, _ := fixture(t, "dlsite-en-release", "r400", model.LinkKindExact,
		map[string]string{"dlsiteen": "RE245678"})
	relOther, _ := fixture(t, "itch-only", "r500", model.LinkKindExact,
		map[string]string{"itch": "somegame.itch.io/x"})

	beforeTouch := workUpdatedAt(t, workSteam)

	st, err := RunWithDB(ctx, db, Opts{})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Lanes[LaneSteam].Planned)
	assert.Equal(t, 1, st.Lanes[LaneDmm].Planned)
	assert.Equal(t, 1, st.Lanes[LaneDlsite].Planned)
	assert.Equal(t, 1, st.Lanes[LaneDlsiteEN].Planned)
	for _, ls := range st.Lanes {
		assert.Zero(t, ls.Written, "a dry run writes nothing")
	}
	var n int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM catalog_external_ref WHERE source_id IN ?`,
		[]int16{steam, dmm, dlsite}).Scan(&n).Error)
	assert.EqualValues(t, 0, n)

	st, err = RunWithDB(ctx, db, Opts{Apply: true})
	require.NoError(t, err)
	for name, ls := range st.Lanes {
		assert.Equal(t, 1, ls.Written, "lane %s", name)
		assert.Zero(t, ls.Errors, "lane %s", name)
	}

	got := refsFor(t, relSteam, steam)
	require.Len(t, got, 1)
	assert.Equal(t, "2258770", got[0].ExternalID)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind,
		"the ref is exactly as strong as the vndb ref it rides on")
	assert.Equal(t, "rule:vndb-extlink-steam", got[0].MatchedBy)
	assert.Equal(t, model.EntityTypeRelease, got[0].EntityType,
		"a store page is a SKU, so the anchor hangs on the release")

	require.Len(t, refsFor(t, relDmm, dmm), 1)
	assert.Equal(t, "2212apc13900", refsFor(t, relDmm, dmm)[0].ExternalID,
		"the DMM URL is reduced to the bare cid the catalog already uses")
	assert.Equal(t, "RJ264149", refsFor(t, relDl, dlsite)[0].ExternalID)

	en := refsFor(t, relEN, dlsite)
	require.Len(t, en, 1)
	assert.Equal(t, "RE245678", en[0].ExternalID)
	assert.Equal(t, "rule:vndb-extlink-dlsite-en", en[0].MatchedBy,
		"the English storefront keeps its own provenance tag inside the shared dlsite source")

	assert.Empty(t, refsFor(t, relOther, steam), "itch is not this job's business")

	assert.True(t, workUpdatedAt(t, workSteam).After(beforeTouch),
		"the parent work is touched when its release gains an anchor")

	st, err = RunWithDB(ctx, db, Opts{Apply: true})
	require.NoError(t, err)
	for name, ls := range st.Lanes {
		assert.Zero(t, ls.Candidates, "lane %s: an anchored release leaves the candidate set", name)
		assert.Zero(t, ls.Planned, "lane %s", name)
		assert.Zero(t, ls.Written, "lane %s", name)
	}
}

func TestProbableVndbAnchorIsNotAChain(t *testing.T) {
	db := requireDB(t)
	fixture(t, "probable-vndb", "r600", model.LinkKindProbable, map[string]string{"steam": "999999"})

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	assert.Zero(t, st.Lanes[LaneSteam].Candidates)
	assert.Zero(t, st.Lanes[LaneSteam].Written)
}

func TestNeverRegradesAnExistingAnchor(t *testing.T) {
	db := requireDB(t)
	steam := sourceID(t, "steam")
	rel, _ := fixture(t, "already-anchored", "r700", model.LinkKindExact, map[string]string{"steam": "222222"})
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel, SourceID: steam,
		ExternalID: "111111", LinkKind: model.LinkKindProbable, MatchedBy: "human:review"}).Error)

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	assert.Zero(t, st.Lanes[LaneSteam].Candidates, "an already-anchored release is not a candidate")

	got := refsFor(t, rel, steam)
	require.Len(t, got, 1)
	assert.Equal(t, "111111", got[0].ExternalID, "the existing id survives")
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind, "the existing tier survives")
	assert.Equal(t, "human:review", got[0].MatchedBy)
}

func TestValueHeldByAnotherReleaseIsSkipped(t *testing.T) {
	db := requireDB(t)
	dlsite := sourceID(t, "dlsite")
	holder, _ := fixture(t, "holder", "r800", model.LinkKindExact, nil)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: holder, SourceID: dlsite,
		ExternalID: "RJ111111", LinkKind: model.LinkKindExact, MatchedBy: "rule:dlsite-work-import"}).Error)
	claimant, _ := fixture(t, "claimant", "r801", model.LinkKindExact, map[string]string{"dlsite": "RJ111111"})

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneDlsite})
	require.NoError(t, err)
	ls := st.Lanes[LaneDlsite]
	assert.Equal(t, 1, ls.Candidates)
	assert.Equal(t, 1, ls.SkippedValueTaken)
	assert.Zero(t, ls.Planned)
	assert.Equal(t, []string{"RJ111111"}, ls.TakenSamples, "the blocked id is reported as merge worklist")
	assert.Empty(t, refsFor(t, claimant, dlsite))
}

func TestAmbiguousValueIsSkippedNotArbitrated(t *testing.T) {
	db := requireDB(t)
	steam := sourceID(t, "steam")
	relA, _ := fixture(t, "base-edition", "r900", model.LinkKindExact, map[string]string{"steam": "424242"})
	relB, _ := fixture(t, "patched-edition", "r901", model.LinkKindExact, map[string]string{"steam": "424242"})

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	ls := st.Lanes[LaneSteam]
	assert.Equal(t, 2, ls.Candidates)
	assert.Equal(t, 2, ls.SkippedAmbiguous)
	assert.Zero(t, ls.Planned)
	assert.Empty(t, refsFor(t, relA, steam))
	assert.Empty(t, refsFor(t, relB, steam))
}

func TestRejectionBlocksReassertion(t *testing.T) {
	db := requireDB(t)
	steam := sourceID(t, "steam")
	rel, _ := fixture(t, "rejected-pair", "r950", model.LinkKindExact, map[string]string{"steam": "777777"})
	require.NoError(t, db.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeRelease, EntityID: rel, SourceID: steam,
		ExternalID: "777777", Reason: "human: not this release"}).Error)

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	ls := st.Lanes[LaneSteam]
	assert.Equal(t, 1, ls.SkippedRejection)
	assert.Zero(t, ls.Planned)
	assert.Empty(t, refsFor(t, rel, steam))
}

func TestMalformedValueIsNeverGuessed(t *testing.T) {
	db := requireDB(t)
	dmm := sourceID(t, "dmm")
	rel, _ := fixture(t, "landing-page-only", "r960", model.LinkKindExact,
		map[string]string{"dmm": "dlsoft.dmm.co.jp/original/yuho/"})

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneDmm})
	require.NoError(t, err)
	ls := st.Lanes[LaneDmm]
	assert.Equal(t, 1, ls.Candidates)
	assert.Equal(t, 1, ls.SkippedMalformed)
	assert.Zero(t, ls.Planned)
	assert.Empty(t, refsFor(t, rel, dmm))
}

func TestMultipleIdsOnOneRelease(t *testing.T) {
	db := requireDB(t)
	dmm := sourceID(t, "dmm")
	rel, _ := fixture(t, "two-shops", "r970", model.LinkKindExact,
		map[string]string{"dmm": "dlsoft.dmm.co.jp/detail/next_0031/"})
	addLink(t, "r970", "dmm", "www.dmm.co.jp/mono/pcgame/-/detail/=/cid=next_0031dl/")

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneDmm})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Lanes[LaneDmm].Written)
	got := refsFor(t, rel, dmm)
	require.Len(t, got, 2)
	assert.Equal(t, "next_0031", got[0].ExternalID)
	assert.Equal(t, "next_0031dl", got[1].ExternalID)
}

func TestSameWorkEditionsAnchorTheWork(t *testing.T) {
	db := requireDB(t)
	steam := sourceID(t, "steam")
	relJA, work := fixture(t, "one-title", "r980", model.LinkKindExact, map[string]string{"steam": "515151"})
	relEN := model.CatalogRelease{WorkID: work, Kind: 0}
	require.NoError(t, db.Create(&relEN).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: relEN.ID, SourceID: sourceID(t, "vndb"),
		ExternalID: "r981", LinkKind: model.LinkKindExact, MatchedBy: "import:test"}).Error)
	addLink(t, "r981", "steam", "515151")

	st, err := RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	ls := st.Lanes[LaneSteam]
	assert.Equal(t, 1, ls.PlannedWorkGrain)
	assert.Equal(t, 1, ls.Written)
	assert.Equal(t, 1, ls.SkippedSibling)
	assert.Empty(t, refsFor(t, relJA, steam))
	assert.Empty(t, refsFor(t, relEN.ID, steam))

	var got []model.CatalogExternalRef
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ? AND source_id = ?",
		model.EntityTypeWork, work, steam).Find(&got).Error)
	require.Len(t, got, 1)
	assert.Equal(t, "515151", got[0].ExternalID)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)

	st, err = RunWithDB(context.Background(), db, Opts{Apply: true, Only: LaneSteam})
	require.NoError(t, err)
	assert.Zero(t, st.Lanes[LaneSteam].Written)
	assert.Equal(t, 1, st.Lanes[LaneSteam].SkippedWorkHeld)
}
