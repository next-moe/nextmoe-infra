package workengines

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcbangumi"
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
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/workengines")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/workengines", "cannot connect: %v", err)
	}
	for _, step := range []func(*gorm.DB) error{migrate.Run, srcvndb.EnsureSchema, srcbangumi.EnsureSchema, seed.Run} {
		if err := step(db); err != nil {
			dbtest.SkipMainf("jobs/workengines", "schema setup failed: %v", err)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

type fx struct {
	t      *testing.T
	medium int16
	vndb   int16
	bgm    int16
	nextR  int
}

func newFx(t *testing.T) *fx {
	t.Helper()
	for _, tbl := range []string{
		"catalog_external_ref", "catalog_work_engine", "catalog_release", "catalog_engine", "catalog_work",
		"src_vndb.engines", "src_vndb.releases", "src_bangumi.subject",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" CASCADE").Error)
	}
	f := &fx{t: t}
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&f.medium).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&f.vndb).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&f.bgm).Error)
	return f
}

func (f *fx) vndbEngine(id, name string) {
	require.NoError(f.t, testDB.Create(&srcvndb.Engine{ID: id, Name: name, Description: "about " + name}).Error)
}

func (f *fx) catalogEngine(name string) int64 {
	e := model.CatalogEngine{Name: name, Aliases: []byte("[]")}
	require.NoError(f.t, testDB.Create(&e).Error)
	return e.ID
}

func (f *fx) work(name string) int64 {
	w := model.CatalogWork{MediumID: f.medium, OLang: "ja", DisplayName: name}
	require.NoError(f.t, testDB.Create(&w).Error)
	return w.ID
}

func (f *fx) release(workID int64, vndbEngine string, extra string) int64 {
	f.nextR++
	rid := fmt.Sprintf("r%d", f.nextR)
	rel := model.CatalogRelease{WorkID: workID, Extra: []byte(extra)}
	require.NoError(f.t, testDB.Create(&rel).Error)
	require.NoError(f.t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: f.vndb,
		ExternalID: rid, LinkKind: model.LinkKindExact, MatchedBy: "import:test"}).Error)
	require.NoError(f.t, testDB.Create(&srcvndb.Release{ID: rid, Official: true, Engine: vndbEngine}).Error)
	return rel.ID
}

func (f *fx) bgmSubject(workID, subject int64, engine string) {
	info, err := json.Marshal(map[string]any{"Fields": []map[string]any{{"Key": bgmEngineField, "Value": engine}}})
	require.NoError(f.t, err)
	require.NoError(f.t, testDB.Create(&srcbangumi.Subject{
		ID: subject, Type: 4, Name: "s", InfoboxParsed: info, IngestedAt: time.Now()}).Error)
	require.NoError(f.t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: f.bgm,
		ExternalID: strconv.FormatInt(subject, 10), LinkKind: model.LinkKindExact, MatchedBy: "import:test"}).Error)
}

func releaseEngine(t *testing.T, releaseID int64) *int64 {
	t.Helper()
	var rel model.CatalogRelease
	require.NoError(t, testDB.Unscoped().First(&rel, releaseID).Error)
	return rel.EngineID
}

func engineNamed(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_engine WHERE name = ?`, name).Scan(&id).Error)
	require.NotZero(t, id, "engine %q must exist", name)
	return id
}

func workEngines(t *testing.T, workID int64) []string {
	t.Helper()
	var names []string
	require.NoError(t, testDB.Raw(`SELECT e.name FROM `+model.WorkEnginesSQL+` we
		JOIN catalog_engine e ON e.id = we.engine_id WHERE we.work_id = ? ORDER BY e.name`, workID).
		Scan(&names).Error)
	return names
}

func TestVNDBLaneProjectsReleaseEngines(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("47", "Ren'Py")
	f.vndbEngine("140360", "Ren'py")
	f.vndbEngine("515", "TyranoScript")
	f.vndbEngine("12828", "Tyranobuilder")
	f.vndbEngine("38", "KiriKiri")
	legacy := f.catalogEngine("KiriKiri")

	w1 := f.work("renpy title")
	r1 := f.release(w1, "47", `{"official":true}`)
	r2 := f.release(w1, "140360", `{"official":true}`)
	w2 := f.work("tyrano title")
	r3 := f.release(w2, "12828", `{}`)
	w3 := f.work("kirikiri title")
	r4 := f.release(w3, "38", `{}`)
	w4 := f.work("unrecorded")
	r5 := f.release(w4, "", `{}`)
	w5 := f.work("fan port")
	r6 := f.release(w5, "47", `{"official":false}`)

	st, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	assert.Equal(t, 2, st.EnginesCreated, "Ren'Py and TyranoScript; KiriKiri already exists")
	assert.Equal(t, 4, st.EnginesLinked, "every vndb id a release uses gets a ref")
	assert.Equal(t, 5, st.ReleasesFilled)
	assert.Equal(t, 5, st.ReleasesWritten)

	renpy := engineNamed(t, "Ren'Py")
	assert.Equal(t, renpy, *releaseEngine(t, r1))
	assert.Equal(t, renpy, *releaseEngine(t, r2), "a case variant folds into one engine")
	assert.Equal(t, engineNamed(t, "TyranoScript"), *releaseEngine(t, r3), "Tyranobuilder folds into TyranoScript")
	assert.Equal(t, legacy, *releaseEngine(t, r4), "the legacy row is reused, not duplicated")
	assert.Nil(t, releaseEngine(t, r5))
	assert.Equal(t, renpy, *releaseEngine(t, r6), "the release records its engine whatever its status")

	assert.Equal(t, []string{"Ren'Py"}, workEngines(t, w1))
	assert.Empty(t, workEngines(t, w5), "an unofficial release does not speak for the work")

	var desc string
	require.NoError(t, testDB.Raw(`SELECT description FROM catalog_engine WHERE id = ?`, renpy).Scan(&desc).Error)
	assert.Equal(t, "about Ren'Py", desc)

	st, err = RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	assert.Zero(t, st.EnginesCreated)
	assert.Zero(t, st.EnginesLinked)
	assert.Zero(t, st.ReleasesWritten, "a second run writes nothing")
	assert.Equal(t, 6, st.ReleasesSame)
}

func TestVNDBLaneFollowsUpstreamButNotPeople(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("47", "Ren'Py")
	f.vndbEngine("38", "KiriKiri")
	w := f.work("title")
	changed := f.release(w, "47", `{}`)
	cleared := f.release(w, "38", `{}`)
	human := f.release(w, "47", `{}`)

	_, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	kirikiri := engineNamed(t, "KiriKiri")
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET engine_id = ?,
		field_provenance = '{"engine_id":[{"source":"user","at":"2026-09-28T00:00:00Z"}]}' WHERE id = ?`,
		kirikiri, human).Error)
	require.NoError(t, testDB.Exec(`UPDATE src_vndb.releases SET engine = '38' WHERE id = 'r1'`).Error)
	require.NoError(t, testDB.Exec(`UPDATE src_vndb.releases SET engine = '' WHERE id = 'r2'`).Error)

	st, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	assert.Equal(t, 1, st.ReleasesChanged)
	assert.Equal(t, 1, st.ReleasesCleared)
	assert.Equal(t, 1, st.ReleasesHuman)
	assert.Equal(t, kirikiri, *releaseEngine(t, changed))
	assert.Nil(t, releaseEngine(t, cleared))
	assert.Equal(t, kirikiri, *releaseEngine(t, human), "a person's choice stands")
}

func TestBgmLaneFillsOnlyWorksNothingElseCovers(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("38", "KiriKiri")
	f.vndbEngine("515", "TyranoScript")
	f.vndbEngine("221", "RPG Maker")

	bare := f.work("bangumi only")
	f.bgmSubject(bare, 100, "吉里吉里2/KAG3")
	multi := f.work("two engines")
	f.bgmSubject(multi, 101, "ティラノビルダー, RPGツクールMV")
	junk := f.work("junk value")
	f.bgmSubject(junk, 102, "PC")
	vndbCovered := f.work("vndb has it")
	f.release(vndbCovered, "515", `{}`)
	f.bgmSubject(vndbCovered, 103, "吉里吉里")
	curated := f.work("a person set it")
	f.bgmSubject(curated, 104, "吉里吉里")

	_, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	var curatedSource int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curatedSource).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkEngine{
		WorkID: curated, EngineID: engineNamed(t, "TyranoScript"), SourceID: curatedSource}).Error)

	st, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneBgm})
	require.NoError(t, err)
	assert.Equal(t, 5, st.BgmStated)
	assert.Equal(t, 2, st.BgmCovered)
	assert.Equal(t, 2, st.BgmWorks)
	assert.Equal(t, 3, st.BgmAdd)
	assert.Equal(t, 1, st.Unmapped["PC"])

	assert.Equal(t, []string{"KiriKiri"}, workEngines(t, bare))
	assert.Equal(t, []string{"RPG Maker", "TyranoScript"}, workEngines(t, multi))
	assert.Empty(t, workEngines(t, junk))
	assert.Equal(t, []string{"TyranoScript"}, workEngines(t, vndbCovered), "VNDB's release outranks the infobox")
	assert.Equal(t, []string{"TyranoScript"}, workEngines(t, curated))

	f.release(bare, "38", `{}`)
	_, err = RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	st, err = RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneBgm})
	require.NoError(t, err)
	assert.Equal(t, 1, st.BgmDrop, "the fallback row goes once VNDB covers the work")
	assert.Zero(t, st.BgmAdd)
	var n int64
	require.NoError(t, testDB.Model(&model.CatalogWorkEngine{}).Where("work_id = ? AND source_id = ?", bare, f.bgm).Count(&n).Error)
	assert.Zero(t, n)
	assert.Equal(t, []string{"KiriKiri"}, workEngines(t, bare))
}

func TestDryRunWritesNothing(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("47", "Ren'Py")
	w := f.work("title")
	r := f.release(w, "47", `{}`)
	f.bgmSubject(f.work("bgm"), 100, "Ren'Py")

	st, err := RunWithDB(context.Background(), testDB, Opts{Lane: LaneAll})
	require.NoError(t, err)
	assert.Equal(t, 1, st.EnginesCreated)
	assert.Equal(t, 1, st.ReleasesFilled)
	assert.Equal(t, 1, st.BgmAdd)
	assert.Nil(t, releaseEngine(t, r))
	var n int64
	require.NoError(t, testDB.Model(&model.CatalogEngine{}).Count(&n).Error)
	assert.Zero(t, n)
}

func TestARenamedEngineKeepsItsGroup(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("47", "Ren'Py")
	f.vndbEngine("208", "Renpy")
	w := f.work("title")
	f.release(w, "47", `{}`)
	_, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	renpy := engineNamed(t, "Ren'Py")
	require.NoError(t, testDB.Exec(`UPDATE catalog_engine SET name = 'Ren''Py 引擎' WHERE id = ?`, renpy).Error)

	r := f.release(w, "208", `{}`)
	st, err := RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneVNDB})
	require.NoError(t, err)
	assert.Zero(t, st.EnginesCreated, "a rename must not fork the engine for the next VNDB variant")
	assert.Equal(t, renpy, *releaseEngine(t, r))
}

func TestDryRunForecastsBangumiAgainstTheVNDBLane(t *testing.T) {
	f := newFx(t)
	f.vndbEngine("47", "Ren'Py")
	f.vndbEngine("38", "KiriKiri")
	both := f.work("vndb will cover it")
	f.release(both, "47", `{}`)
	f.bgmSubject(both, 100, "吉里吉里")
	patchOnly := f.work("only a patch")
	patch := f.release(patchOnly, "47", `{}`)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET kind = ? WHERE id = ?`, model.ReleaseKindPatch, patch).Error)
	f.bgmSubject(patchOnly, 101, "吉里吉里")

	dry, err := RunWithDB(context.Background(), testDB, Opts{Lane: LaneAll})
	require.NoError(t, err)
	_, err = RunWithDB(context.Background(), testDB, Opts{Apply: true, Lane: LaneAll})
	require.NoError(t, err)
	again, err := RunWithDB(context.Background(), testDB, Opts{Lane: LaneBgm})
	require.NoError(t, err)

	assert.Equal(t, 1, dry.BgmAdd, "only the work a patch cannot speak for")
	assert.Equal(t, 1, dry.BgmCovered)
	assert.Zero(t, again.BgmAdd, "the dry run forecast what the apply did")
	assert.Zero(t, again.BgmDrop)
	assert.Equal(t, []string{"KiriKiri"}, workEngines(t, patchOnly))
	assert.Equal(t, []string{"Ren'Py"}, workEngines(t, both))
}
