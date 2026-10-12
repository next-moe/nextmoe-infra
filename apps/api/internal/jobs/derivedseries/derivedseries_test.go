package derivedseries

import (
	"context"
	"fmt"
	"os"
	"testing"

	"api/internal/jobs/seriesorder"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/derivedseries")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/derivedseries", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/derivedseries", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/derivedseries", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_series_member", "catalog_series", "catalog_series_name_override",
		"catalog_work_relation", "catalog_release", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mediumID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func mkWork(t *testing.T, name string, y int16) int64 {
	t.Helper()
	w := &model.CatalogWork{MediumID: mediumID(t), OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(w).Error)
	if y != 0 {
		require.NoError(t, testDB.Create(&model.CatalogRelease{WorkID: w.ID, ReleasedY: &y}).Error)
	}
	return w.ID
}

func mkEdge(t *testing.T, a, b, typ int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkRelation{AWorkID: a, BWorkID: b, RelationTypeID: typ}).Error)
}

func mkOwnedSeries(t *testing.T, sourceKey, externalID string, works ...int64) int64 {
	t.Helper()
	var srcID int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, sourceKey).Scan(&srcID).Error)
	require.NotZero(t, srcID)
	s := &model.CatalogSeries{DisplayName: externalID, SourceID: srcID, ExternalID: externalID}
	require.NoError(t, testDB.Create(s).Error)
	for _, w := range works {
		require.NoError(t, testDB.Create(&model.CatalogSeriesMember{SeriesID: s.ID, WorkID: w}).Error)
	}
	return s.ID
}

func derivedSeries(t *testing.T) []model.CatalogSeries {
	t.Helper()
	var out []model.CatalogSeries
	require.NoError(t, testDB.Raw(`
		SELECT s.* FROM catalog_series s JOIN catalog_source src ON src.id = s.source_id
		WHERE src.key = 'derived' ORDER BY s.external_id`).Scan(&out).Error)
	return out
}

func TestBuildDryApplyIdempotent(t *testing.T) {
	clean(t)
	ctx := context.Background()

	a := mkWork(t, "白詰草話 -Episode of the Clovers-", 2004)
	b := mkWork(t, "白詰草話 セカンド", 2006)
	c := mkWork(t, "白詰草話 FD", 2007)
	mkEdge(t, b, a, seriesorder.RelSequelOf)
	mkEdge(t, c, a, seriesorder.RelFandiscOf)

	dry, err := RunWithDB(ctx, testDB, Opts{})
	require.NoError(t, err)
	require.Equal(t, 1, dry.Components)
	require.Equal(t, 1, dry.Eligible)
	require.Equal(t, 1, dry.SeriesCreated)
	require.Equal(t, 3, dry.MembersAdded)
	require.Equal(t, 1, dry.NamedByPrefix)
	require.Empty(t, derivedSeries(t), "a dry run must not write")

	app, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, app.SeriesCreated)
	require.Equal(t, 3, app.MembersAdded)
	require.Equal(t, 3, app.OrderChanged)
	require.Equal(t, 3, app.TouchedWorks)

	got := derivedSeries(t)
	require.Len(t, got, 1)
	require.Equal(t, fmt.Sprintf("comp:%d", a), got[0].ExternalID)
	require.Equal(t, "白詰草話", got[0].DisplayName, "the shared prefix, trimmed of its trailing separator")

	var rows []struct {
		WorkID   int64 `gorm:"column:work_id"`
		Position int16 `gorm:"column:position"`
		Kind     int16 `gorm:"column:kind"`
	}
	require.NoError(t, testDB.Raw(`SELECT work_id, position, kind FROM catalog_series_member
		WHERE series_id = ? ORDER BY position`, got[0].ID).Scan(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, []int64{a, b, c}, []int64{rows[0].WorkID, rows[1].WorkID, rows[2].WorkID})
	require.Equal(t, model.SeriesMemberKindMain, rows[0].Kind)
	require.Equal(t, model.SeriesMemberKindFandisc, rows[2].Kind)

	second, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Zero(t, second.SeriesCreated+second.SeriesRenamed+second.SeriesDeleted)
	require.Zero(t, second.MembersAdded+second.MembersStale+second.OrderChanged)
	require.Zero(t, second.TouchedWorks, "a steady-state re-run must touch nothing")
}

func TestOverlapWithOwnedLaneIsRefused(t *testing.T) {
	clean(t)
	ctx := context.Background()

	a := mkWork(t, "Owned A", 2001)
	b := mkWork(t, "Owned B", 2002)
	mkEdge(t, b, a, seriesorder.RelSequelOf)
	ownedID := mkOwnedSeries(t, "curated", "wiki:1", a)

	wl := t.TempDir() + "/worklist.jsonl"
	st, err := RunWithDB(ctx, testDB, Opts{Apply: true, Worklist: wl})
	require.NoError(t, err)
	require.Equal(t, 1, st.SkippedOverlap)
	require.Zero(t, st.SeriesCreated)
	require.Empty(t, derivedSeries(t))

	body, err := os.ReadFile(wl)
	require.NoError(t, err)
	require.Contains(t, string(body), `"reason":"overlap"`)
	require.Contains(t, string(body), fmt.Sprintf(`"hit_series_ids":[%d]`, ownedID))
}

func TestGiantComponentSplitsOnStrongEdges(t *testing.T) {
	clean(t)
	ctx := context.Background()

	var lineA, lineB []int64
	for i := range 16 {
		lineA = append(lineA, mkWork(t, fmt.Sprintf("Alpha Chronicle %d", i), int16(2000+i)))
		lineB = append(lineB, mkWork(t, fmt.Sprintf("Beta Chronicle %d", i), int16(2000+i)))
		if i > 0 {
			mkEdge(t, lineA[i], lineA[i-1], seriesorder.RelSequelOf)
			mkEdge(t, lineB[i], lineB[i-1], seriesorder.RelSequelOf)
		}
	}
	mkEdge(t, lineB[0], lineA[0], seriesorder.RelSideStoryOf)

	wl := t.TempDir() + "/worklist.jsonl"
	st, err := RunWithDB(ctx, testDB, Opts{Apply: true, Worklist: wl})
	require.NoError(t, err)
	require.Equal(t, 1, st.Components)
	require.Equal(t, 1, st.SkippedGiant)
	require.Equal(t, 1, st.GiantsSplit)
	require.Equal(t, 2, st.Eligible, "the two chains survive the split")

	got := derivedSeries(t)
	require.Len(t, got, 2)
	names := []string{got[0].DisplayName, got[1].DisplayName}
	require.Contains(t, names, "Alpha Chronicle")
	require.Contains(t, names, "Beta Chronicle")

	body, err := os.ReadFile(wl)
	require.NoError(t, err)
	require.Contains(t, string(body), `"reason":"giant"`)
	require.Contains(t, string(body), `"size":32`)
}

func TestRemakeJoinsItsOriginalsLine(t *testing.T) {
	clean(t)
	ctx := context.Background()

	original := mkWork(t, "ONE～輝く季節へ～", 1998)
	fd := mkWork(t, "ONE～輝く季節へ～ ファンディスク", 1999)
	mkEdge(t, fd, original, seriesorder.RelFandiscOf)
	_, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	before := derivedSeries(t)
	require.Len(t, before, 1)

	remake := mkWork(t, "ONE.", 2025)
	mkEdge(t, remake, original, seriesorder.RelRemakeOf)
	st, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Zero(t, st.SeriesCreated+st.SeriesDeleted, "the remake joins the line it remakes")
	require.Equal(t, 1, st.MembersAdded)

	after := derivedSeries(t)
	require.Len(t, after, 1)
	require.Equal(t, before[0].ID, after[0].ID)
	require.Equal(t, fmt.Sprintf("comp:%d", original), after[0].ExternalID)

	var rows []struct {
		WorkID   int64 `gorm:"column:work_id"`
		Position int16 `gorm:"column:position"`
		Kind     int16 `gorm:"column:kind"`
	}
	require.NoError(t, testDB.Raw(`SELECT work_id, position, kind FROM catalog_series_member
		WHERE series_id = ? ORDER BY position`, after[0].ID).Scan(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, []int64{original, fd, remake}, []int64{rows[0].WorkID, rows[1].WorkID, rows[2].WorkID})
	require.Equal(t, model.SeriesMemberKindMain, rows[2].Kind)

	requireQuiescent(t)
}

func TestCrossoverWithARemakeIsStillCut(t *testing.T) {
	clean(t)
	a1 := mkWork(t, "Alpha First", 2000)
	a2 := mkWork(t, "Alpha Second", 2001)
	b1 := mkWork(t, "Beta First", 2002)
	b2 := mkWork(t, "Beta Second", 2003)
	x := mkWork(t, "Crossover Festival", 2004)
	xr := mkWork(t, "Crossover Festival Remake", 2010)
	mkEdge(t, a2, a1, seriesorder.RelSequelOf)
	mkEdge(t, b2, b1, seriesorder.RelSequelOf)
	mkEdge(t, x, a1, seriesorder.RelFandiscOf)
	mkEdge(t, x, b1, seriesorder.RelFandiscOf)
	mkEdge(t, xr, x, seriesorder.RelRemakeOf)

	st := runApply(t)
	require.Equal(t, 1, st.Bridges)

	var rows []struct {
		SeriesID int64 `gorm:"column:series_id"`
		WorkID   int64 `gorm:"column:work_id"`
	}
	require.NoError(t, testDB.Raw(`SELECT series_id, work_id FROM catalog_series_member`).Scan(&rows).Error)
	seriesOf := map[int64]int64{}
	for _, r := range rows {
		seriesOf[r.WorkID] = r.SeriesID
	}
	require.Equal(t, seriesOf[a1], seriesOf[a2])
	require.Equal(t, seriesOf[b1], seriesOf[b2])
	require.NotEqual(t, seriesOf[a1], seriesOf[b1])
	require.NotEqual(t, seriesOf[a1], seriesOf[x])
	require.NotEqual(t, seriesOf[b1], seriesOf[x])

	requireQuiescent(t)
}

func TestARemakeEdgeDoesNotMakeASideStoryABridge(t *testing.T) {
	clean(t)
	a1 := mkWork(t, "Alpha First", 2000)
	a2 := mkWork(t, "Alpha Second", 2001)
	other := mkWork(t, "Another Tale", 2002)
	stay := mkWork(t, "Stay Memories", 2003)
	mkEdge(t, a2, a1, seriesorder.RelSequelOf)
	mkEdge(t, other, a1, seriesorder.RelSideStoryOf)
	mkEdge(t, stay, a1, seriesorder.RelSideStoryOf)
	mkEdge(t, stay, other, seriesorder.RelRemakeOf)

	st := runApply(t)
	require.Zero(t, st.Bridges)

	var got []int64
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_series_member`).Scan(&got).Error)
	require.ElementsMatch(t, []int64{a1, a2, other, stay}, got)

	requireQuiescent(t)
}

func TestFallbackNameIsTheEarliestMemberVerbatim(t *testing.T) {
	clean(t)
	ctx := context.Background()

	old := mkWork(t, "月姫", 2000)
	newer := mkWork(t, "歌月十夜", 2001)
	mkEdge(t, newer, old, seriesorder.RelFandiscOf)

	st, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, st.NamedByFallback)
	require.Zero(t, st.NamedByPrefix)
	got := derivedSeries(t)
	require.Len(t, got, 1)
	require.Equal(t, "月姫", got[0].DisplayName)
}

func TestComponentsMergingMovesTheIdentity(t *testing.T) {
	clean(t)
	ctx := context.Background()

	a1 := mkWork(t, "Aria One", 2001)
	a2 := mkWork(t, "Aria Two", 2002)
	b1 := mkWork(t, "Brave One", 2003)
	b2 := mkWork(t, "Brave Two", 2004)
	mkEdge(t, a2, a1, seriesorder.RelSequelOf)
	mkEdge(t, b2, b1, seriesorder.RelSequelOf)

	_, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Len(t, derivedSeries(t), 2)

	mkEdge(t, b1, a1, seriesorder.RelSameSeries)
	st, err := RunWithDB(ctx, testDB, Opts{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, st.SeriesDeleted, "the component whose minimum id moved is retired")
	require.Equal(t, 2, st.MembersAdded)

	got := derivedSeries(t)
	require.Len(t, got, 1)
	require.Equal(t, fmt.Sprintf("comp:%d", a1), got[0].ExternalID)
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_series_member WHERE series_id = ?`,
		got[0].ID).Scan(&n).Error)
	require.EqualValues(t, 4, n)
}

func TestCommonPrefixTrimsTrailingJunk(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		in         []string
	}{
		{"separator", "Clannad", []string{"Clannad - After Story", "Clannad "}},
		{"number", "英雄伝説", []string{"英雄伝説 6", "英雄伝説 7"}},
		{"fullwidth folds to ascii", "Fate", []string{"Ｆａｔｅ／stay night", "Fate/hollow ataraxia"}},
		{"one shared rune is still a prefix", "恋", []string{"恋する乙女", "恋わずらい"}},
		{"nothing shared", "", []string{"Alpha", "Beta"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, commonPrefix(tc.in))
		})
	}
}

func TestShortPrefixFallsBackToTheEarliestTitle(t *testing.T) {
	name, byPrefix := nameComponent([]string{"恋する乙女", "恋わずらい"}, "恋する乙女")
	require.False(t, byPrefix)
	require.Equal(t, "恋する乙女", name)
}
