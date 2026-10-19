package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

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
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
			os.Exit(1)
		}
		for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run} {
			if err := step(db); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: catalog test setup: %v\n", err)
				os.Exit(1)
			}
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func setup(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{"catalog_credit", "catalog_credit_name", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func roleID(t *testing.T, key string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_role WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, key)
	return id
}

func mkWork(t *testing.T, medium string) int64 {
	t.Helper()
	var mid int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = ?`, medium).Scan(&mid).Error)
	w := &model.CatalogWork{MediumID: mid, OLang: "ja", DisplayName: "w", ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(w).Error)
	return w.ID
}

func mkName(t *testing.T, name string) int64 {
	t.Helper()
	cn := &model.CatalogCreditName{Name: name, Lang: "ja", Kind: model.CreditNameKindMain, LinkVisibility: model.LinkVisibilityPublic}
	require.NoError(t, testDB.Create(cn).Error)
	return cn.ID
}

func mkCredit(t *testing.T, work, name, role int64, source int16) int64 {
	t.Helper()
	c := &model.CatalogCredit{WorkID: work, CreditNameID: name, RoleID: role, SourceID: &source}
	require.NoError(t, testDB.Create(c).Error)
	return c.ID
}

func roleOf(t *testing.T, creditID int64) (int64, bool) {
	t.Helper()
	var roles []int64
	require.NoError(t, testDB.Raw(`SELECT role_id FROM catalog_credit WHERE id = ?`, creditID).Scan(&roles).Error)
	if len(roles) == 0 {
		return 0, false
	}
	return roles[0], true
}

func TestSeedMapsBangumiGamePositionsToSharedRoles(t *testing.T) {
	setup(t)
	scenario := roleID(t, "scenario")
	require.NoError(t, testDB.Exec(`UPDATE catalog_source_role_map SET role_id = ? WHERE source_id = 3 AND source_role = '4:1004'`,
		roleID(t, "剧本")).Error)
	require.NoError(t, seed.Run(testDB))

	var mapped int64
	require.NoError(t, testDB.Raw(`SELECT role_id FROM catalog_source_role_map WHERE source_id = 3 AND source_role = '4:1004'`).Scan(&mapped).Error)
	assert.Equal(t, scenario, mapped, "a re-seed must move an existing mapping row, not only insert new ones")

	var retired []string
	require.NoError(t, testDB.Raw(`SELECT key FROM catalog_role WHERE is_deprecated ORDER BY key`).Scan(&retired).Error)
	assert.ElementsMatch(t, []string{"制作总指挥", "剧本", "原画", "音乐"}, retired)
}

func TestRepairMovesBangumiCreditsOntoSharedRoles(t *testing.T) {
	setup(t)
	generated, err := seed.GenerateBangumiRoles()
	require.NoError(t, err)
	scenario, juben := roleID(t, "scenario"), roleID(t, "剧本")
	music, yinyue := roleID(t, "music"), roleID(t, "音乐")

	game, anime := mkWork(t, "galgame"), mkWork(t, "anime")
	a, b, c := mkName(t, "えんぎよし"), mkName(t, "魁"), mkName(t, "ハサマ")
	vndbTwin := mkCredit(t, game, a, scenario, 2)
	twinned := mkCredit(t, game, a, juben, 3)
	moved := mkCredit(t, game, b, juben, 3)
	movedMusic := mkCredit(t, game, c, yinyue, 3)
	otherSource := mkCredit(t, game, c, juben, 2)
	notAGame := mkCredit(t, anime, b, yinyue, 3)

	var dry bytes.Buffer
	st, err := run(context.Background(), testDB, generated.GameRoleToShared, false, &dry)
	require.NoError(t, err)
	assert.Equal(t, stats{Moved: 2, Deleted: 1, Works: 1}, st)
	r, _ := roleOf(t, moved)
	assert.Equal(t, juben, r, "a dry run writes nothing")

	var receipts bytes.Buffer
	st, err = run(context.Background(), testDB, generated.GameRoleToShared, true, &receipts)
	require.NoError(t, err)
	assert.Equal(t, stats{Moved: 2, Deleted: 1, Works: 1}, st)

	_, ok := roleOf(t, twinned)
	assert.False(t, ok, "a credit whose twin exists under the shared role is deleted")
	r, ok = roleOf(t, vndbTwin)
	assert.True(t, ok)
	assert.Equal(t, scenario, r)
	r, _ = roleOf(t, moved)
	assert.Equal(t, scenario, r)
	r, _ = roleOf(t, movedMusic)
	assert.Equal(t, music, r)
	r, _ = roleOf(t, otherSource)
	assert.Equal(t, juben, r, "only bangumi-sourced credits move")
	r, _ = roleOf(t, notAGame)
	assert.Equal(t, yinyue, r, "only credits on game works move")

	lines := strings.Split(strings.TrimSpace(receipts.String()), "\n")
	require.Len(t, lines, 3)
	actions := map[string]int{}
	for _, l := range lines {
		var rec receipt
		require.NoError(t, json.Unmarshal([]byte(l), &rec))
		actions[rec.Action]++
		if rec.Action == "delete" {
			require.NotNil(t, rec.TwinID)
			assert.Equal(t, vndbTwin, *rec.TwinID)
			assert.Equal(t, twinned, rec.Before.ID)
		}
	}
	assert.Equal(t, map[string]int{"move": 2, "delete": 1}, actions)

	st, err = run(context.Background(), testDB, generated.GameRoleToShared, true, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, stats{}, st, "a second run finds nothing to move")
}

func suppress(t *testing.T, work int64, key string) {
	t.Helper()
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: work, FieldKey: editspec.FieldWorkCredits, IdentityKey: key,
	}).Error)
}

func suppressedKeys(t *testing.T, work int64) []string {
	t.Helper()
	var keys []string
	require.NoError(t, testDB.Raw(`SELECT identity_key FROM edit_suppressed_row
		WHERE entity_type = ? AND entity_id = ? AND field_key = ? ORDER BY identity_key`,
		editspec.TypeWork, work, editspec.FieldWorkCredits).Scan(&keys).Error)
	return keys
}

func TestRepairCarriesSuppressionsWithTheCredits(t *testing.T) {
	setup(t)
	require.NoError(t, testDB.Exec("TRUNCATE edit_suppressed_row RESTART IDENTITY").Error)
	generated, err := seed.GenerateBangumiRoles()
	require.NoError(t, err)
	scenario, juben := roleID(t, "scenario"), roleID(t, "剧本")

	game, other := mkWork(t, "galgame"), mkWork(t, "galgame")
	a, b := mkName(t, "えんぎよし"), mkName(t, "魁")
	mkCredit(t, game, a, scenario, 2)
	mkCredit(t, game, a, juben, 3)
	mkCredit(t, game, b, juben, 3)
	mkCredit(t, other, b, scenario, 2)

	suppress(t, game, editspec.CreditIdentity(juben, a, 0))
	suppress(t, game, editspec.CreditIdentity(juben, b, 0))
	suppress(t, other, editspec.CreditIdentity(scenario, b, 0))

	st, err := run(context.Background(), testDB, generated.GameRoleToShared, false, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, stats{Moved: 1, Deleted: 1, Works: 1, SuppressionsMoved: 1, SuppressionsDropped: 1}, st)
	assert.Len(t, suppressedKeys(t, game), 2, "a dry run writes nothing")

	st, err = run(context.Background(), testDB, generated.GameRoleToShared, true, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, stats{Moved: 1, Deleted: 1, Works: 1, SuppressionsMoved: 1, SuppressionsDropped: 1}, st)

	assert.Equal(t, []string{editspec.CreditIdentity(scenario, b, 0)}, suppressedKeys(t, game),
		"the moved credit stays hidden, and the twin of the deleted one stays visible")
	assert.Equal(t, []string{editspec.CreditIdentity(scenario, b, 0)}, suppressedKeys(t, other),
		"another work's suppressions are untouched")

	var hidden int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_credit c WHERE c.work_id = ? AND NOT `+
		editspec.NotSuppressedCreditSQL("c"), game).Scan(&hidden).Error)
	assert.EqualValues(t, 1, hidden, "the read path still drops exactly the credit that was hidden")
}
