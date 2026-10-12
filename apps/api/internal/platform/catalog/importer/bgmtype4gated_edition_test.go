package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const galgameTags = `["Galgame","PC","游戏"]`

func seedReleaseOn(t *testing.T, workID int64, source int16, extID, title string) {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_release (work_id, kind, title, extra, created_at)
		VALUES (?, 0, ?, '{}', now()) RETURNING id`, workID, title).Scan(&id).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (6, ?, ?, ?, 0, 'rule:test-release')`, id, source, extID).Error)
}

func editionRefCount(t *testing.T, ext string) int64 {
	t.Helper()
	return countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = ? AND matched_by = ?`,
		model.EntityTypeWork, bangumiSource, ext, model.LinkKindRelated, ruleBgmEditionGuard)
}

func TestBgmType4GatedEditionGuard(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	base := seedVNDBWork(t, "v51")
	seedReleaseOn(t, base, vndbSource, "r1", "ワンエディション完全版")
	seedReleaseOn(t, base, vndbSource, "r2", "ワンエディション完全版")
	seedSubject(t, 4001, "ワン エディション 完全版", "", galgameTags, "", false)

	zhBase := seedVNDBWork(t, "v52")
	seedReleaseOn(t, zhBase, vndbSource, "r3", "中文原作发行标题")
	seedSubject(t, 4004, "", "中文原作发行标题", galgameTags, "", false)

	twinA := seedVNDBWork(t, "v60")
	twinB := seedVNDBWork(t, "v61")
	seedReleaseOn(t, twinA, vndbSource, "r4", "二作同梱リリース名")
	seedReleaseOn(t, twinB, vndbSource, "r5", "二作同梱リリース名")
	seedSubject(t, 4002, "二作同梱リリース名", "", galgameTags, "", false)

	seedSubject(t, 4003, "無関係な新規作品名", "", galgameTags, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 4, dry.GatedTotal)
	assert.Equal(t, 2, dry.EditionGuarded, "4001 by its space-folded name, 4004 by its CN name")
	assert.Equal(t, 1, dry.EditionAmbiguous)
	require.Len(t, dry.EditionAmbiguousSubjects, 1)
	assert.Equal(t, int64(4002), dry.EditionAmbiguousSubjects[0].SubjectID)
	assert.Equal(t, []int64{twinA, twinB}, dry.EditionAmbiguousSubjects[0].WorkIDs)
	assert.Equal(t, 1, dry.ToCreate)
	assert.Zero(t, dry.EditionRefsCreated)
	assert.Zero(t, editionRefCount(t, "4001"))

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.EditionRefsCreated)
	assert.Equal(t, 1, st.WorksCreated, "only the unrelated subject mints")

	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = '4001' AND link_kind = ? AND matched_by = ?`,
		model.EntityTypeWork, base, bangumiSource, model.LinkKindRelated, ruleBgmEditionGuard))
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = '4004' AND link_kind = ?`,
		model.EntityTypeWork, zhBase, bangumiSource, model.LinkKindRelated))
	assert.Zero(t, countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE source_id = ? AND external_id = '4002'`, bangumiSource))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workIDByBgmExt(t, "4003")))
	assert.Equal(t, int64(5), scalarInt(t, `SELECT count(*) FROM catalog_work`), "four seeded holders plus one mint")

	again, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Zero(t, again.EditionGuarded, "an attached edition leaves the pool")
	assert.Equal(t, 1, again.EditionAmbiguous, "the ambiguous subject stays parked")
	assert.Zero(t, again.WorksCreated)
}

func TestBgmType4GatedEditionGuardBoundaries(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	dlOnly := seedVNDBWork(t, "v70")
	seedReleaseOn(t, dlOnly, dlsiteSource, "RJ7000", "ダウンロード限定リリース名")
	seedSubject(t, 4101, "ダウンロード限定リリース名", "", galgameTags, "", false)

	rejected := seedVNDBWork(t, "v71")
	seedReleaseOn(t, rejected, vndbSource, "r71", "却下済みエディション名")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_match_rejection (entity_type, entity_id, source_id, external_id, reason)
		VALUES (?, ?, ?, '4102', 'not this work')`, model.EntityTypeWork, rejected, bangumiSource).Error)
	seedSubject(t, 4102, "却下済みエディション名", "", galgameTags, "", false)

	gone := seedVNDBWork(t, "v72")
	seedReleaseOn(t, gone, vndbSource, "r72", "削除済み作品のリリース名")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, gone).Error)
	seedSubject(t, 4103, "削除済み作品のリリース名", "", galgameTags, "", false)

	own := seedExistingWork(t, "原作そのものの題名")
	seedReleaseOn(t, own, vndbSource, "r73", "原作そのものの題名")
	seedSubject(t, 4104, "原作そのものの題名", "", galgameTags, "", false)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Zero(t, st.EditionGuarded)
	assert.Zero(t, st.EditionAmbiguous)
	assert.Equal(t, 1, st.TitleCollisions, "a subject naming the work itself still goes to the work-pair judge")
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 4, st.WorksCreated)
	for _, ext := range []string{"4101", "4102", "4103"} {
		assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workIDByBgmExt(t, ext)), ext)
	}
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, workIDByBgmExt(t, "4104")))
	assert.Zero(t, countWhere(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by = ?`, ruleBgmEditionGuard))
}
