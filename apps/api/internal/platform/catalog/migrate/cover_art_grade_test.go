package migrate

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	triggerTestMedium = int16(1)
	srcDLsite         = int16(2)
	srcCensored       = int16(21)
)

func coverFixture(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`TRUNCATE catalog_work_cover, catalog_work RESTART IDENTITY CASCADE`).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_medium (id, key) VALUES (?, 'galgame')
		ON CONFLICT (id) DO NOTHING`, triggerTestMedium).Error)
	for id, key := range map[int16]string{srcDLsite: "dlsite", srcCensored: "censored"} {
		require.NoError(t, testDB.Exec(`
			INSERT INTO catalog_source (id, key, trust_tier) VALUES (?, ?, 0)
			ON CONFLICT (id) DO NOTHING`, id, key).Error)
	}
}

func newWork(t *testing.T, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: triggerTestMedium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func addCover(t *testing.T, workID int64, source int16, kind, hash string, sexual int16) int64 {
	t.Helper()
	c := model.CatalogWorkCover{WorkID: workID, ImageHash: hash, Kind: kind, Sexual: sexual, SourceID: source}
	require.NoError(t, testDB.Create(&c).Error)
	return c.ID
}

func gradeOf(t *testing.T, workID int64) bool {
	t.Helper()
	var got bool
	require.NoError(t, testDB.Raw(
		`SELECT cover_art_all_explicit FROM catalog_work WHERE id = ?`, workID).Scan(&got).Error)
	return got
}

func updatedAt(t *testing.T, workID int64) time.Time {
	t.Helper()
	var got time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, workID).Scan(&got).Error)
	return got
}

func TestCoverArtGradeFollowsEveryCoverWrite(t *testing.T) {
	coverFixture(t)
	w := newWork(t, "LOVERY LEAK")
	assert.False(t, gradeOf(t, w), "a work with no cover art owns nothing explicit")

	safe := addCover(t, w, srcDLsite, "main", strings.Repeat("a", 64), model.SexualSafe)
	assert.False(t, gradeOf(t, w))

	explicit := addCover(t, w, srcDLsite, "main", strings.Repeat("b", 64), model.SexualExplicit)
	assert.False(t, gradeOf(t, w), "one safe row is enough to stay on the sfw shelf")

	before := updatedAt(t, w)
	// The nightly grader's move: no write touches catalog_work, and this is the
	// path that put 961 works on a shelf they could not honour.
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_cover SET sexual = ? WHERE id = ?`, model.SexualExplicit, safe).Error)
	assert.True(t, gradeOf(t, w), "the last safe cover went explicit")
	assert.True(t, updatedAt(t, w).After(before),
		"a flip must bump updated_at or every downstream mirror of content_limit stays stale")

	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_cover WHERE id = ?`, explicit).Error)
	assert.True(t, gradeOf(t, w), "deleting one of two explicit rows leaves the work explicit-only")

	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_cover WHERE work_id = ?`, w).Error)
	assert.False(t, gradeOf(t, w), "a work with no cover art owns nothing explicit")
}

func TestCoverArtGradeCountsOnlyElectableArt(t *testing.T) {
	coverFixture(t)
	for _, tc := range []struct {
		name   string
		source int16
		kind   string
		want   bool
	}{
		{"a censored ghost is the fallback, not safe art", srcCensored, "main", true},
		{"a box photo is not a cover the election can use", srcDLsite, "pkgfront", true},
		{"a real safe cover keeps the work on the sfw shelf", srcDLsite, "main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWork(t, tc.name)
			addCover(t, w, srcDLsite, "main", strings.Repeat("c", 63)+string(rune('a'+len(tc.name)%26)), model.SexualExplicit)
			addCover(t, w, tc.source, tc.kind, strings.Repeat("d", 63)+string(rune('a'+len(tc.name)%26)), model.SexualSafe)
			assert.Equal(t, tc.want, gradeOf(t, w))
		})
	}
}

func TestCoverArtGradeSurvivesABulkRegrade(t *testing.T) {
	coverFixture(t)
	ids := make([]int64, 0, 20)
	for i := range 20 {
		w := newWork(t, "bulk")
		addCover(t, w, srcDLsite, "main", strings.Repeat("e", 60)+padHex(i), model.SexualSafe)
		ids = append(ids, w)
	}
	// One statement, twenty works: a row-level trigger would be twenty
	// recomputes and a statement trigger without a transition table would see
	// none of them.
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_cover SET sexual = ? WHERE work_id IN ?`, model.SexualExplicit, ids).Error)
	for _, id := range ids {
		assert.True(t, gradeOf(t, id), "work %d missed the bulk re-grade", id)
	}
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_cover WHERE work_id IN ?`, ids).Error)
	for _, id := range ids {
		assert.False(t, gradeOf(t, id), "work %d missed the bulk delete", id)
	}
}

func TestCoverArtGradeResetsOnTruncate(t *testing.T) {
	coverFixture(t)
	w := newWork(t, "truncated")
	addCover(t, w, srcDLsite, "main", strings.Repeat("f", 64), model.SexualExplicit)
	require.True(t, gradeOf(t, w))
	// TRUNCATE fires neither the statement nor the row triggers above, and it
	// needs CASCADE here because catalog_cover_vote has a FK onto this table.
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_work_cover CASCADE`).Error)
	assert.False(t, gradeOf(t, w))
}

func TestCoverArtGradeIsReadOnlyToTheORM(t *testing.T) {
	coverFixture(t)
	w := model.CatalogWork{
		MediumID: triggerTestMedium, OLang: "ja", DisplayName: "orm cannot write this",
		CoverArtAllExplicit: true,
	}
	require.NoError(t, testDB.Create(&w).Error)
	assert.False(t, gradeOf(t, w.ID), "Create must not carry the derived column")

	addCover(t, w.ID, srcDLsite, "main", strings.Repeat("9", 64), model.SexualExplicit)
	require.True(t, gradeOf(t, w.ID))
	w.CoverArtAllExplicit = false
	w.DisplayName = "saved with a stale struct"
	require.NoError(t, testDB.Save(&w).Error)
	assert.True(t, gradeOf(t, w.ID),
		"Save carrying a stale struct must not hand the work back to the sfw shelf")
}

func TestCoverArtGradeTriggerKnowsEveryPackagingKind(t *testing.T) {
	var def string
	require.NoError(t, testDB.Raw(
		`SELECT pg_get_functiondef('catalog_work_cover_art_grade()'::regprocedure)`).Scan(&def).Error)
	named := map[string]bool{}
	for _, m := range regexp.MustCompile(`'(pkg[a-z]+)'`).FindAllStringSubmatch(def, -1) {
		named[m[1]] = true
	}
	want := map[string]bool{}
	for _, k := range model.PackagingCoverKinds {
		want[k] = true
	}
	assert.Equal(t, want, named,
		"the deployed trigger excludes a different set of packaging kinds than model.PackagingCoverKinds — "+
			"a kind in one spelling and not the other lets the audit clear a work the election refuses to render")
}

func padHex(i int) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[i/16%16], hex[i%16], hex[i/16%16], hex[i%16]})
}

// A merge rehangs cover rows with UPDATE ... SET work_id, which is the one
// write that changes the answer for TWO works at once — the trigger has to
// recompute the row's old owner as well as its new one.
func TestCoverArtGradeFollowsAMergeRehang(t *testing.T) {
	coverFixture(t)
	src := newWork(t, "merged away")
	dst := newWork(t, "merge target")
	addCover(t, src, srcDLsite, "main", strings.Repeat("1", 64), model.SexualExplicit)
	addCover(t, dst, srcDLsite, "main", strings.Repeat("2", 64), model.SexualSafe)
	require.True(t, gradeOf(t, src))
	require.False(t, gradeOf(t, dst))

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_cover SET work_id = ? WHERE work_id = ?`, dst, src).Error)
	assert.False(t, gradeOf(t, src), "the source lost its last explicit row")
	assert.False(t, gradeOf(t, dst), "the target still owns a safe cover")

	require.NoError(t, testDB.Exec(
		`DELETE FROM catalog_work_cover WHERE work_id = ? AND sexual = ?`, dst, model.SexualSafe).Error)
	assert.True(t, gradeOf(t, dst), "the target kept only the inherited explicit row")
}
