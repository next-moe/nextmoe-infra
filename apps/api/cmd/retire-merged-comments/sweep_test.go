package main

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	catmigrate "api/internal/platform/catalog/migrate"
	catmodel "api/internal/platform/catalog/model"
	catseed "api/internal/platform/catalog/seed"
	commigrate "api/internal/platform/community/migrate"
	commodel "api/internal/platform/community/model"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

// Both schemas go into the one test database. They live in separate databases
// in production and the tool holds a handle to each, so a single handle handed
// to it twice exercises exactly the same SQL.
func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/retire-merged-comments", "cannot connect to test database: %v", err)
	}
	for _, step := range []struct {
		name string
		run  func(*gorm.DB) error
	}{{"catalog migrate", catmigrate.Run}, {"catalog seed", catseed.Run}, {"community migrate", commigrate.Run}} {
		if err := step.run(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s: %v\n", step.name, err)
			os.Exit(1)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

// A site whose site_game anchor is still its own product id. The forum was one
// until its 2026-09-23 G0 renumber; see siteGameAnchorIsCatalogID.
const testSite = "letmoe"

// The four threads differ from each other in one fact apiece, so a thread that
// survives the sweep is attributable to that fact alone.
//
// coincidence is the one that matters. It is stranded's exact shape -- a post,
// a soft-deleted work of the same id, a redirect naming a survivor -- and
// differs only in that a LIVE work claims that number as its product_work_id.
// That was the production case before the forum's renumber: kungal thread 1406
// held three posts about a game whose gid 2656 belonged to work 2649, while
// catalog work 2656 was an unrelated merged-away work. A sweep without the
// claim clause deletes it and reports success.
func TestOnlyADeadAnchorLosesItsComments(t *testing.T) {
	if testDB == nil {
		t.Skip("no test DB")
	}
	for _, tbl := range []string{"community_post", "community_thread", "catalog_redirect", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}

	mkWork := func(id int64, name string, gid *int64, deleted bool) {
		site := testSite
		w := catmodel.CatalogWork{ID: id, MediumID: 1, OLang: "ja", DisplayName: name,
			Extra: []byte(`{}`), FieldProvenance: []byte(`{}`)}
		if gid != nil {
			w.Site, w.ProductWorkID = &site, gid
		}
		require.NoError(t, testDB.Create(&w).Error)
		if deleted {
			require.NoError(t, testDB.Exec(
				`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, id).Error)
		}
	}
	mkRedirect := func(old, current int64) {
		now := time.Now()
		require.NoError(t, testDB.Create(&catmodel.CatalogRedirect{
			EntityType: entityTypeWork, OldID: old, CurrentID: current, MergedAt: &now}).Error)
	}
	mkThread := func(anchor string, posts int) int64 {
		th := commodel.CommunityThread{Site: testSite, Kind: threadKindComments,
			AnchorKind: 1, AnchorID: anchor, ContentRating: 0, Status: 0, CreatedBy: 1}
		require.NoError(t, testDB.Create(&th).Error)
		for i := 1; i <= posts; i++ {
			require.NoError(t, testDB.Create(&commodel.CommunityPost{
				ThreadID: th.ID, PostNumber: int32(i), AuthorID: 1,
				ContentRaw: "hi", ContentHTML: "<p>hi</p>", SanitizerVersion: 1}).Error)
		}
		require.NoError(t, testDB.Exec(
			`UPDATE community_thread SET posts_count = ? WHERE id = ?`, posts, th.ID).Error)
		return th.ID
	}

	gid := func(v int64) *int64 { return &v }

	// stranded: merged away, nobody claims its number.
	mkWork(900001, "stranded", nil, true)
	mkWork(900002, "stranded survivor", gid(900007), false)
	mkRedirect(900001, 900002)
	stranded := mkThread("900001", 1)

	// coincidence: same shape, but 900003 is a live work's gid.
	mkWork(900003, "coincidence", nil, true)
	mkWork(900004, "coincidence survivor", nil, false)
	mkRedirect(900003, 900004)
	mkWork(900010, "the game that actually answers to 900003", gid(900003), false)
	coincidence := mkThread("900003", 1)

	// alive: no redirect at all.
	mkWork(900005, "alive", nil, false)
	alive := mkThread("900005", 1)

	// empty: merged away, but holds no post -- the lazy-comments wave's row.
	mkWork(900006, "empty", nil, true)
	mkRedirect(900006, 900002)
	empty := mkThread("900006", 0)

	// The fixture's own positive control: coincidence really is stranded's
	// shape, so the claim clause is the only thing that can separate them.
	var shaped int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_redirect r
		JOIN catalog_work w ON w.id = r.old_id AND w.deleted_at IS NOT NULL
		WHERE r.entity_type = ? AND r.old_id IN (900001, 900003)`, entityTypeWork).
		Scan(&shaped).Error)
	require.Equal(t, int64(2), shaped, "fixture: both anchors must be merged-away works")

	found, retired, err := run(testDB, testDB, []string{testSite}, 1, 0, false, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 1, found, "only stranded is actionable")
	assert.Equal(t, 0, retired, "a report writes nothing")
	assertStatus(t, map[int64]int16{stranded: 0, coincidence: 0, alive: 0, empty: 0})

	found, retired, err = run(testDB, testDB, []string{testSite}, 1, 0, true, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 1, found)
	assert.Equal(t, 1, retired)
	assertStatus(t, map[int64]int16{
		stranded:    threadStatusDeleted,
		coincidence: 0,
		alive:       0,
		empty:       0,
	})

	// The posts are still there: the retirement is a soft delete and the undo
	// the report prints has something to put back.
	var posts int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM community_post WHERE thread_id = ?`, stranded).
		Scan(&posts).Error)
	assert.Equal(t, int64(1), posts)

	// Re-running is a no-op: the retired thread has left the sweep's own input.
	found, retired, err = run(testDB, testDB, []string{testSite}, 1, 0, true, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 0, found)
	assert.Equal(t, 0, retired)
}

// TestTheSurvivorIsNamedInTheSitesOwnIdSpace guards the one number a person
// reads out of the report. The survivor work's catalog id is NOT the anchor the
// conversation would move to: work 32145 in production answers to gid 32330,
// and the number 32145 is a different game's gid. Printing the catalog id would
// hand a reviewer an anchor that opens the wrong page.
func TestTheSurvivorIsNamedInTheSitesOwnIdSpace(t *testing.T) {
	if testDB == nil {
		t.Skip("no test DB")
	}
	for _, tbl := range []string{"community_post", "community_thread", "catalog_redirect", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	site := testSite
	survivorGID := int64(910099)
	require.NoError(t, testDB.Create(&catmodel.CatalogWork{ID: 910001, MediumID: 1, OLang: "ja",
		DisplayName: "gone", Extra: []byte(`{}`), FieldProvenance: []byte(`{}`)}).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = 910001`).Error)
	require.NoError(t, testDB.Create(&catmodel.CatalogWork{ID: 910002, MediumID: 1, OLang: "ja",
		DisplayName: "survivor", Site: &site, ProductWorkID: &survivorGID,
		Extra: []byte(`{}`), FieldProvenance: []byte(`{}`)}).Error)
	now := time.Now()
	require.NoError(t, testDB.Create(&catmodel.CatalogRedirect{
		EntityType: entityTypeWork, OldID: 910001, CurrentID: 910002, MergedAt: &now}).Error)

	th := commodel.CommunityThread{Site: site, Kind: threadKindComments, AnchorKind: 1,
		AnchorID: "910001", Status: 0, CreatedBy: 1}
	require.NoError(t, testDB.Create(&th).Error)
	require.NoError(t, testDB.Create(&commodel.CommunityPost{ThreadID: th.ID, PostNumber: 1,
		AuthorID: 1, ContentRaw: "hi", ContentHTML: "<p>hi</p>", SanitizerVersion: 1}).Error)

	live, err := liveAnchors(testDB, sweepOpts{Site: site, AnchorKind: 1})
	require.NoError(t, err)
	rows, err := strandedAmong(testDB, site, 1, live)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(910002), rows[0].Survivor)
	assert.Equal(t, "910099", rows[0].SurvivorAnchor, "the site's gid, not the catalog id")
	assert.Equal(t, int64(1), rows[0].Posts)
}

func assertStatus(t *testing.T, want map[int64]int16) {
	t.Helper()
	for id, status := range want {
		var got int16
		require.NoError(t, testDB.Raw(`SELECT status FROM community_thread WHERE id = ?`, id).Scan(&got).Error)
		assert.Equal(t, status, got, "thread %d", id)
	}
}

// TestTheClaimExclusionIsOnlyForTheSitesOwnIdSpace is the inverse of the
// coincidence case. At anchor_kind 3 the anchor IS the catalog id, so a live
// claim carrying the same number says nothing about it -- applying the
// exclusion there would keep a genuinely dead anchor alive forever.
func TestTheClaimExclusionIsOnlyForTheSitesOwnIdSpace(t *testing.T) {
	if testDB == nil {
		t.Skip("no test DB")
	}
	for _, tbl := range []string{"community_post", "community_thread", "catalog_redirect", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	site := testSite
	claimed := int64(920001)
	mk := func(id int64, name string, gid *int64) {
		w := catmodel.CatalogWork{ID: id, MediumID: 1, OLang: "ja", DisplayName: name,
			Extra: []byte(`{}`), FieldProvenance: []byte(`{}`)}
		if gid != nil {
			w.Site, w.ProductWorkID = &site, gid
		}
		require.NoError(t, testDB.Create(&w).Error)
	}
	mk(920001, "merged away", nil)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = 920001`).Error)
	mk(920002, "survivor", nil)
	mk(920003, "an unrelated game whose gid is 920001", &claimed)
	now := time.Now()
	require.NoError(t, testDB.Create(&catmodel.CatalogRedirect{
		EntityType: entityTypeWork, OldID: 920001, CurrentID: 920002, MergedAt: &now}).Error)

	th := commodel.CommunityThread{Site: site, Kind: threadKindComments, AnchorKind: 3,
		AnchorID: "920001", Status: 0, CreatedBy: 1}
	require.NoError(t, testDB.Create(&th).Error)
	require.NoError(t, testDB.Create(&commodel.CommunityPost{ThreadID: th.ID, PostNumber: 1,
		AuthorID: 1, ContentRaw: "hi", ContentHTML: "<p>hi</p>", SanitizerVersion: 1}).Error)

	live, err := liveAnchors(testDB, sweepOpts{Site: site, AnchorKind: 3})
	require.NoError(t, err)
	require.Len(t, live, 1)

	atKind1, err := strandedAmong(testDB, site, 1, live)
	require.NoError(t, err)
	assert.Empty(t, atKind1, "a site-local anchor owned by a live claim is alive")

	atKind3, err := strandedAmong(testDB, site, 3, live)
	require.NoError(t, err)
	assert.Len(t, atKind3, 1, "a catalog anchor is dead regardless of who claims the number")

	_, err = strandedAmong(testDB, site, 2, live)
	assert.Error(t, err, "an anchor kind naming no catalog entity must not be guessed at")
}
