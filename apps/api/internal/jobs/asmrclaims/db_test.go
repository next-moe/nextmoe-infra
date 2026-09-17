package asmrclaims

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed asmrclaims tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
	}
	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog migrate failed: %v\n", err)
		os.Exit(1)
	}
	if err := seed.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog seed failed: %v\n", err)
		os.Exit(1)
	}
	testDB, testDSN = db, dsn
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec("TRUNCATE catalog_work RESTART IDENTITY CASCADE").Error)
}

func mkWork(t *testing.T, medium int16, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	require.NotZero(t, w.ID)
	return w.ID
}

func setClaim(t *testing.T, workID, productWorkID int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET site = ?, product_work_id = ?, claim_state = ? WHERE id = ?`,
		siteKungal, productWorkID, model.ClaimStateHidden, workID,
	).Error)
}

type workClaim struct {
	ID            int64      `gorm:"column:id"`
	MediumID      int16      `gorm:"column:medium_id"`
	Site          *string    `gorm:"column:site"`
	ProductWorkID *int64     `gorm:"column:product_work_id"`
	ClaimState    *int16     `gorm:"column:claim_state"`
	DeletedAt     *time.Time `gorm:"column:deleted_at"`
}

func loadClaim(t *testing.T, id int64) workClaim {
	t.Helper()
	var row workClaim
	require.NoError(t, testDB.Unscoped().Raw(
		`SELECT id, medium_id, site, product_work_id, claim_state, deleted_at FROM catalog_work WHERE id = ?`,
		id,
	).Scan(&row).Error)
	require.Equal(t, id, row.ID)
	return row
}

func assertCleared(t *testing.T, id int64) {
	t.Helper()
	got := loadClaim(t, id)
	assert.Nil(t, got.Site)
	assert.Nil(t, got.ProductWorkID)
	require.NotNil(t, got.ClaimState)
	assert.Equal(t, model.ClaimStateHidden, *got.ClaimState)
}

func assertKungalClaim(t *testing.T, id, productWorkID int64) {
	t.Helper()
	got := loadClaim(t, id)
	require.NotNil(t, got.Site)
	assert.Equal(t, siteKungal, *got.Site)
	require.NotNil(t, got.ProductWorkID)
	assert.Equal(t, productWorkID, *got.ProductWorkID)
	require.NotNil(t, got.ClaimState)
	assert.Equal(t, model.ClaimStateHidden, *got.ClaimState)
}

func run(t *testing.T, apply bool) map[string]any {
	t.Helper()
	sum, err := Run(context.Background(), &config.Config{}, Opts{DSN: testDSN, Apply: apply})
	require.NoError(t, err)
	require.NotNil(t, sum)
	return sum
}

func selfClaimWithRival(t *testing.T) (candidateID, rivalID int64) {
	t.Helper()
	candidateID = mkWork(t, mediumASMR, "asmr-self")
	setClaim(t, candidateID, candidateID)
	rivalID = mkWork(t, 1, "galgame-holder")
	setClaim(t, rivalID, candidateID)
	return candidateID, rivalID
}

func claimEventCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_claim_event`).Scan(&n).Error)
	return n
}

func TestUndoSQL(t *testing.T) {
	assert.Equal(t,
		"UPDATE catalog_work SET site = 'kungal', product_work_id = id WHERE id IN (62210, 62211);",
		undoSQL([]int64{62210, 62211}))
}

func TestClearsMedium5SelfClaimWithRival(t *testing.T) {
	clean(t)
	cand, rival := selfClaimWithRival(t)

	// A delta, not an absolute: clean() truncates catalog_work only, so any
	// claim_event another suite left in the shared test database would fail an
	// assert.Zero here for a reason that has nothing to do with this command.
	eventsBefore := claimEventCount(t)

	sum := run(t, true)
	assert.Equal(t, 1, sum["candidates"])
	assert.Equal(t, 1, sum["cleared"])
	assert.Equal(t, 0, sum["skipped_no_rival"])
	assert.Equal(t, true, sum["apply"])

	assertCleared(t, cand)
	assertKungalClaim(t, rival, cand)
	assert.Equal(t, int16(1), loadClaim(t, rival).MediumID)
	assert.Equal(t, eventsBefore, claimEventCount(t),
		"removing a claim binding is not a claim-state transition and must write no event")
}

// The public changes feed reads (updated_at, id), so a clear that leaves
// updated_at alone never reaches a consumer. survivorship.go's unclaim sets it
// for the same reason.
func TestClearBumpsUpdatedAt(t *testing.T) {
	clean(t)
	cand, rival := selfClaimWithRival(t)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET updated_at = now() - interval '30 days' WHERE id IN (?, ?)`,
		cand, rival).Error)
	before := updatedAt(t, cand)
	rivalBefore := updatedAt(t, rival)

	run(t, true)

	assert.True(t, updatedAt(t, cand).After(before),
		"the cleared row's updated_at must move, or the changes feed never sees it")
	assert.Equal(t, rivalBefore, updatedAt(t, rival), "the rival must not be touched")
}

func updatedAt(t *testing.T, id int64) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, testDB.Raw(
		`SELECT updated_at FROM catalog_work WHERE id = ?`, id).Scan(&ts).Error)
	return ts
}

func TestLeavesMedium5SelfClaimWithNoRival(t *testing.T) {
	clean(t)
	cand := mkWork(t, mediumASMR, "asmr-orphan")
	setClaim(t, cand, cand)

	sum := run(t, true)
	assert.Equal(t, 1, sum["candidates"])
	assert.Equal(t, 0, sum["cleared"])
	assert.Equal(t, 1, sum["skipped_no_rival"])
	assert.Equal(t, true, sum["apply"])

	assertKungalClaim(t, cand, cand)
}

func TestLeavesMedium1SelfClaimWithRival(t *testing.T) {
	clean(t)
	galgame := mkWork(t, 1, "galgame-self")
	setClaim(t, galgame, galgame)
	other := mkWork(t, mediumASMR, "asmr-other")
	setClaim(t, other, galgame)

	sum := run(t, true)
	assert.Equal(t, 0, sum["candidates"])
	assert.Equal(t, 0, sum["cleared"])
	assert.Equal(t, 0, sum["skipped_no_rival"])

	assertKungalClaim(t, galgame, galgame)
	assertKungalClaim(t, other, galgame)
}

func TestLeavesMedium5ClaimNotEqualToOwnID(t *testing.T) {
	clean(t)
	cand := mkWork(t, mediumASMR, "asmr-foreign-claim")
	product := cand + 1000
	setClaim(t, cand, product)

	sum := run(t, true)
	assert.Equal(t, 0, sum["candidates"])
	assert.Equal(t, 0, sum["cleared"])

	assertKungalClaim(t, cand, product)
}

func TestDryRunChangesNothing(t *testing.T) {
	clean(t)
	cand, rival := selfClaimWithRival(t)

	dry := run(t, false)
	assert.Equal(t, 1, dry["candidates"])
	assert.Equal(t, 0, dry["cleared"])
	assert.Equal(t, 0, dry["skipped_no_rival"])
	assert.Equal(t, false, dry["apply"])

	assertKungalClaim(t, cand, cand)
	assertKungalClaim(t, rival, cand)

	applied := run(t, true)
	assert.Equal(t, dry["candidates"], applied["candidates"])
	assert.Equal(t, 1, applied["cleared"])
	assertCleared(t, cand)
	assertKungalClaim(t, rival, cand)
}

func TestSoftDeletedMedium5SelfClaimIsNotACandidate(t *testing.T) {
	clean(t)
	cand := mkWork(t, mediumASMR, "asmr-deleted")
	setClaim(t, cand, cand)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, cand).Error)

	sum := run(t, true)
	assert.Equal(t, 0, sum["candidates"])
	assert.Equal(t, 0, sum["cleared"])
	assert.Equal(t, 0, sum["skipped_no_rival"])

	got := loadClaim(t, cand)
	require.NotNil(t, got.DeletedAt)
	assertKungalClaim(t, cand, cand)
}
