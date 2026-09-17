package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanVNPool(t *testing.T) {
	t.Helper()
	for _, tb := range []string{"src_vndb.vn", "src_vndb.vn_titles"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tb+" RESTART IDENTITY CASCADE").Error)
	}
}

func insVN(t *testing.T, id, olang string, devstatus int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.vn
		(id, olang, image, c_image, description, c_votecount, c_lengthnum, length, devstatus, alias, ingested_at)
		VALUES (?,?,'','','',0,0,0,?,'',now())`, id, olang, devstatus).Error)
}

func insVNTitle(t *testing.T, id, lang string, official bool, title, latin string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.vn_titles (id, lang, official, title, latin)
		VALUES (?,?,?,?,?)`, id, lang, official, title, latin).Error)
}

func insVNRelease(t *testing.T, id string, minage *int16, patch, hasEro bool) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases
		(id, gtin, olang, released, voiced, reso_x, reso_y, minage, ani_story, ani_ero, has_ero, patch, freeware, official, catalog, notes, engine)
		VALUES (?,0,'ja',20200101,0,0,0,?,0,0,?,?,false,true,'','','')`,
		id, minage, hasEro, patch).Error)
}

func seedVNDBProbableWork(t *testing.T, vid string) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'ja','folded',0,0,'{}','{}',false) RETURNING id`).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 2, ?, 1, 'rule:merge-demoted')`, workID, vid).Error)
	return workID
}

// seedVNDBWorkPool lays out one of every admission decision the mint has to
// make, so each assertion below names the vid it is about.
func seedVNDBWorkPool(t *testing.T) {
	t.Helper()
	clean(t)
	cleanReleases(t)
	cleanVNPool(t)

	// v100 — finished, olang ja, official en title, non-patch 18+ release.
	insVN(t, "v100", "ja", 0)
	insVNTitle(t, "v100", "ja", true, "作品エー", "Sakuhin A")
	insVNTitle(t, "v100", "en", true, "Work A", "")
	insVNRelease(t, "r100", i16(18), false, false)
	insReleaseVN(t, "r100", "v100", "complete")

	// v101 — in development, no en title, no releases at all.
	insVN(t, "v101", "ja", 1)
	insVNTitle(t, "v101", "ja", true, "開発中作品", "")

	// v102 — cancelled.
	insVN(t, "v102", "ja", 2)
	insVNTitle(t, "v102", "ja", true, "中止作品", "")

	// v103 — already carries an exact anchor.
	insVN(t, "v103", "ja", 0)
	insVNTitle(t, "v103", "ja", true, "既錨作品", "")
	seedVNDBWork(t, "v103")

	// v104 — anchor demoted to probable by a merge.
	insVN(t, "v104", "ja", 0)
	insVNTitle(t, "v104", "ja", true, "合併済作品", "")
	seedVNDBProbableWork(t, "v104")

	// v105 — finished, but the only title row is for a language that is not olang.
	insVN(t, "v105", "ja", 0)
	insVNTitle(t, "v105", "en", true, "Nameless", "")

	// v106 — only an 18+ PATCH release; the work itself is all-ages.
	insVN(t, "v106", "ja", 0)
	insVNTitle(t, "v106", "ja", true, "パッチ作品", "")
	insVNRelease(t, "r106", i16(18), true, true)
	insReleaseVN(t, "r106", "v106", "complete")

	// v107 — no minage, but a non-patch release flagged has_ero.
	insVN(t, "v107", "ja", 0)
	insVNTitle(t, "v107", "ja", true, "エロ有作品", "")
	insVNRelease(t, "r107", nil, false, true)
	insReleaseVN(t, "r107", "v107", "complete")

	// v108 — an unofficial en title must not become a work title row.
	insVN(t, "v108", "en", 0)
	insVNTitle(t, "v108", "en", true, "English Original", "")
	insVNTitle(t, "v108", "ja", false, "非公式邦題", "")
}

func TestVNDBWorksMintDryPlan(t *testing.T) {
	seedVNDBWorkPool(t)

	st, err := New(testDB, nil, Options{Source: "vndb", DryRun: true}).RunVNDBWorks()
	require.NoError(t, err)

	assert.Equal(t, 7, st.PoolUnanchored, "v100..v108 minus the two already-anchored vids")
	assert.Equal(t, 1, st.SkippedCancelled, "v102")
	assert.Equal(t, 1, st.SkippedNoOLangTitle, "v105")
	assert.Equal(t, 5, st.Planned, "v100 v101 v106 v107 v108")
	assert.Equal(t, 2, st.PlannedR18, "v100 minage, v107 has_ero")
	assert.Equal(t, st.Planned, st.WorksCreated, "DRY reports the plan through works_created — run.sh greps it")
	assert.Equal(t, 6, st.TitlesCreated, "one olang row each, plus v100's official en row")

	var works, refs int64
	testDB.Raw(`SELECT count(*) FROM catalog_work WHERE display_name LIKE '%作品%'`).Scan(&works)
	testDB.Raw(`SELECT count(*) FROM catalog_external_ref WHERE matched_by = ?`, ruleVNDBWork).Scan(&refs)
	assert.Zero(t, works, "DRY writes no work")
	assert.Zero(t, refs, "DRY writes no anchor")
}

func TestVNDBWorksMintApply(t *testing.T) {
	seedVNDBWorkPool(t)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 5, st.WorksCreated)
	assert.Equal(t, 6, st.TitlesCreated)
	assert.Equal(t, 5, st.AnchorsCreated)
	assert.Equal(t, 5, st.RevisionsCreated)

	workOf := func(vid string) int64 {
		var id int64
		require.NoError(t, testDB.Raw(`SELECT entity_id FROM catalog_external_ref
			WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
			model.EntityTypeWork, vndbSource, vid, model.LinkKindExact).Scan(&id).Error)
		return id
	}

	w100 := workOf("v100")
	require.NotZero(t, w100)
	var name, olang string
	var rating, status, medium int16
	require.NoError(t, testDB.Raw(`SELECT display_name, olang, content_rating, status, medium_id FROM catalog_work WHERE id = ?`, w100).
		Row().Scan(&name, &olang, &rating, &status, &medium))
	assert.Equal(t, "作品エー", name, "display_name is the olang title verbatim, never the latin field")
	assert.Equal(t, "ja", olang)
	assert.Equal(t, model.ContentRatingR18, rating, "non-patch release with minage 18")
	assert.Equal(t, model.WorkStatusLive, status)
	assert.Equal(t, mediumGalgame, medium)

	var claimState *int16
	var nsfw bool
	require.NoError(t, testDB.Raw(`SELECT claim_state, display_nsfw FROM catalog_work WHERE id = ?`, w100).Row().Scan(&claimState, &nsfw))
	assert.Nil(t, claimState, "an imported work is unclaimed")
	assert.False(t, nsfw)

	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = ? AND lang = 'ja' AND title = '作品エー' AND kind = 0 AND provenance = 0 AND src_hash IS NULL`, w100))
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = ? AND lang = 'en' AND title = 'Work A' AND provenance = 0`, w100))
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_revision WHERE entity_type = 5 AND entity_id = ? AND action = ?`, w100, model.RevisionActionImported))

	assert.Equal(t, model.ContentRatingAllAges, ratingOf(t, workOf("v106")), "an 18+ PATCH does not make the work r18")
	assert.Equal(t, model.ContentRatingR18, ratingOf(t, workOf("v107")), "has_ero on a non-patch release is enough")
	assert.Equal(t, model.ContentRatingAllAges, ratingOf(t, workOf("v101")), "no releases at all")

	w108 := workOf("v108")
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = ?`, w108),
		"olang=en writes one row; the unofficial ja title is not source supply and the en row is not duplicated")
	assert.Equal(t, "English Original", scalarStr(t, `SELECT title FROM catalog_work_title WHERE work_id = ?`, w108))

	assert.Zero(t, countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE external_id IN ('v102','v105') AND matched_by = ?`, ruleVNDBWork))

	var anchorKindV103 int16
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_external_ref WHERE entity_type = 5 AND source_id = 2 AND external_id = 'v103'`).Scan(&anchorKindV103).Error)
	assert.Equal(t, int16(1), anchorKindV103, "the already-anchored vid keeps exactly its one anchor")
}

func ratingOf(t *testing.T, workID int64) int16 {
	t.Helper()
	var r int16
	require.NoError(t, testDB.Raw(`SELECT content_rating FROM catalog_work WHERE id = ?`, workID).Scan(&r).Error)
	return r
}

// A merge folds two VNDB works into one and demotes both exact anchors to
// probable. Reading only exact anchors here would report the surviving vid as
// unanchored and re-mint the work the merge just folded away.
func TestVNDBWorksMintSkipsProbableAnchorResurrection(t *testing.T) {
	seedVNDBWorkPool(t)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 5, st.WorksCreated)

	assert.Zero(t, countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = 5 AND source_id = 2 AND external_id = 'v104' AND link_kind = ?`, model.LinkKindExact),
		"v104 must not get a fresh exact anchor")
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = 5 AND source_id = 2 AND external_id = 'v104'`),
		"v104 still holds only the probable ref the merge left")
}

func TestVNDBWorksMintIsIdempotent(t *testing.T) {
	seedVNDBWorkPool(t)

	first, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	require.Equal(t, 5, first.WorksCreated)

	second, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Zero(t, second.Planned, "the anchors the first pass minted take every vid out of the pool")
	assert.Zero(t, second.WorksCreated)
	assert.Zero(t, second.TitlesCreated)
	assert.Equal(t, int64(5), countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by = ?`, ruleVNDBWork))
}

func TestVNDBWorksMintLimit(t *testing.T) {
	seedVNDBWorkPool(t)

	dry, err := New(testDB, nil, Options{Source: "vndb", DryRun: true, Limit: 2}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 2, dry.Planned)
	assert.Equal(t, 3, dry.CappedByLimit)
	assert.Equal(t, []string{"v100", "v101"}, planVIDs(dry), "the cap takes the lowest vids, so a canary is reproducible")

	st, err := New(testDB, nil, Options{Source: "vndb", Limit: 2}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 2, st.WorksCreated)
	assert.Equal(t, int64(2), countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by = ?`, ruleVNDBWork))
}

func planVIDs(st VNDBWorksStats) []string {
	out := make([]string, 0, len(st.Plans))
	for _, p := range st.Plans {
		out = append(out, p.VID)
	}
	return out
}

func TestVNDBWorksMintQuarantineOnTitleCollision(t *testing.T) {
	clean(t)
	cleanReleases(t)
	cleanVNPool(t)

	liveID := seedExistingWork(t, "衝突する既存タイトル作品")
	deadID := seedExistingWork(t, "削除済みタイトル作品")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, deadID).Error)

	insVN(t, "v200", "ja", 0)
	insVNTitle(t, "v200", "ja", true, "衝突する既存タイトル作品", "")
	insVN(t, "v201", "ja", 0)
	insVNTitle(t, "v201", "ja", true, "削除済みタイトル作品", "")
	insVN(t, "v202", "ja", 0)
	insVNTitle(t, "v202", "ja", true, "衝突しない新規タイトル作品", "")

	dry, err := New(testDB, nil, Options{Source: "vndb", DryRun: true}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 1, dry.TitleCollisions)
	assert.Equal(t, 1, dry.ToQuarantine)
	assert.Equal(t, 3, dry.Planned)
	assert.Equal(t, 3, dry.WorksCreated)
	assert.Zero(t, countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by = ?`, ruleVNDBWork))

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 3, st.WorksCreated)
	assert.Equal(t, 1, st.Quarantined)

	workOf := func(vid string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, testDB.Raw(`SELECT entity_id FROM catalog_external_ref
			WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
			model.EntityTypeWork, vndbSource, vid, model.LinkKindExact).Scan(&id).Error)
		return id
	}

	qID := workOf("v200")
	require.NotZero(t, qID)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, qID))
	a, b := liveID, qID
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = ? AND a_id = ? AND b_id = ? AND reason = ? AND status = ?`,
		model.EntityTypeWork, a, b, model.CandidateReasonNameNormEqual, model.CandidateStatusPending))

	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOf("v201")))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOf("v202")))
}

func TestVNDBWorksMintQuarantinesAPunctuationVariant(t *testing.T) {
	clean(t)
	cleanReleases(t)
	cleanVNPool(t)

	existing := seedExistingWork(t, "CARTAGRAN ～少女狩猟機～")
	insVN(t, "v300", "ja", 0)
	insVNTitle(t, "v300", "ja", true, "CARTAGRAN －少女狩猟機－", "")
	insVN(t, "v301", "ja", 0)
	insVNTitle(t, "v301", "ja", true, "CARTAGRAN －少女狩猟機2－", "")

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunVNDBWorks()
	require.NoError(t, err)
	assert.Equal(t, 1, st.TitleCollisions)
	assert.Equal(t, 1, st.Quarantined)

	workOf := func(vid string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, testDB.Raw(`SELECT entity_id FROM catalog_external_ref
			WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
			model.EntityTypeWork, vndbSource, vid, model.LinkKindExact).Scan(&id).Error)
		return id
	}
	q := workOf("v300")
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, q))
	a, b := existing, q
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = ? AND a_id = ? AND b_id = ? AND status = ?`,
		model.EntityTypeWork, a, b, model.CandidateStatusPending))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOf("v301")))
}
