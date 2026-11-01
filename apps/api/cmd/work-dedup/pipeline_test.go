package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	srcb "api/internal/platform/catalog/srcbangumi"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

// TestMain keeps running when there is no database: the verdict table tests in
// this package are pure Go, and exiting early for a missing DSN would drop them
// from the DB-less `unit` CI job without any tell.
func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
			os.Exit(1)
		}
		for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run, srcb.EnsureSchema} {
			if err := step(db); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: catalog test setup: %v\n", err)
				os.Exit(1)
			}
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
}

func cleanPipeline(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_match_candidate", "catalog_merge_proposal", "catalog_merge_archive", "catalog_redirect",
		"catalog_external_ref", "catalog_revision", "catalog_entity_usage",
		"catalog_work_label", "catalog_work_title", "catalog_release", "catalog_work",
		"src_bangumi.subject",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func galgameMedium(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	require.NotZero(t, id, "galgame medium is not seeded")
	return id
}

func mkWork(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	if site != nil {
		w.Site = site
		product := int64(900000 + len(name))
		w.ProductWorkID = &product
	}
	require.NoError(t, testDB.Create(w).Error)
	return w.ID
}

func mkTitle(t *testing.T, workID int64, title string) {
	t.Helper()
	mkTitleKind(t, workID, title, model.WorkTitleKindOfficial)
}

func mkTitleKind(t *testing.T, workID int64, title string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: workID, Lang: "ja", Title: title, Kind: kind,
	}).Error)
}

func mkAnchor(t *testing.T, workID int64, sourceID int16, externalID string) {
	t.Helper()
	mkRef(t, workID, sourceID, externalID, model.LinkKindExact, nil)
}

func mkRef(t *testing.T, workID int64, sourceID int16, externalID string, linkKind int16, deadAt *time.Time) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID,
		ExternalID: externalID, LinkKind: linkKind, MatchedBy: "test:work-dedup",
		DeadAt: deadAt,
	}).Error)
}

func mkRelease(t *testing.T, workID int64, y, m, d int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogRelease{
		WorkID: workID, Kind: model.ReleaseKindDefault,
		ReleasedY: &y, ReleasedM: &m, ReleasedD: &d,
	}).Error)
}

func candidateStatus(t *testing.T, a, b int64) int16 {
	t.Helper()
	var status *int16
	require.NoError(t, testDB.Raw(
		`SELECT status FROM catalog_match_candidate WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		model.EntityTypeWork, min(a, b), max(a, b)).Scan(&status).Error)
	require.NotNil(t, status, "no candidate filed for (%d,%d)", a, b)
	return *status
}

func countRows(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(query, args...).Scan(&n).Error)
	return n
}

func TestWorkDedupPipeline(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	kungal := "kungal"
	const actor = int64(4242)

	// K1 is a claim carrying an official title; M1 is the import-minted twin
	// whose only identity string is a display_name spelled with extra spaces.
	k1 := mkWork(t, medium, "K1クレーム作品テスト", &kungal)
	mkTitle(t, k1, "重複タイトルテスト検証")
	mkRelease(t, k1, 2015, 8, 10)
	m1 := mkWork(t, medium, "重複 タイトル テスト 検証", nil)
	mkAnchor(t, m1, 3, "777")
	mkRelease(t, m1, 2015, 9, 9)

	k2 := mkWork(t, medium, "K2アンカー衝突元作品", nil)
	mkTitle(t, k2, "衝突アンカー作品検証")
	mkAnchor(t, k2, 3, "111")
	m2 := mkWork(t, medium, "M2アンカー衝突先作品", nil)
	mkTitle(t, m2, "衝突アンカー作品検証")
	mkAnchor(t, m2, 3, "222")

	k3 := mkWork(t, medium, "K3年違い元作品テスト", nil)
	mkTitle(t, k3, "年違い作品検証テスト")
	mkRelease(t, k3, 2015, 5, 1)
	m3 := mkWork(t, medium, "M3年違い先作品テスト", nil)
	mkTitle(t, m3, "年違い作品検証テスト")
	mkRelease(t, m3, 2019, 5, 1)

	c, err := buildCensus(ctx, testDB)
	require.NoError(t, err)
	require.Len(t, c.rows, 3, "three pairs: %+v", c.rows)
	verdicts := c.verdictByPair()
	assert.Equal(t, bucketAuto, verdicts[[2]int64{k1, m1}])
	assert.Equal(t, bucketAnchorConflict, verdicts[[2]int64{k2, m2}])
	assert.Equal(t, bucketDateClash, verdicts[[2]int64{k3, m3}])
	require.Len(t, c.groups, 1)
	assert.Equal(t, k1, c.groups[0].survivor, "the claimed work survives")

	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve)
	var out bytes.Buffer

	require.NoError(t, runSeed(ctx, testDB, &out, actor, false))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_match_candidate`), "dry seed writes nothing")

	out.Reset()
	require.NoError(t, runSeed(ctx, testDB, &out, actor, true))
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, k1, m1))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, k2, m2))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, k3, m3))

	out.Reset()
	require.NoError(t, runPropose(ctx, testDB, &out, merge, actor, waveTagW1, 0, false))
	assert.Contains(t, out.String(), fmt.Sprintf("PLAN %d <- %d", k1, m1))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_merge_proposal`), "dry propose writes nothing")
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, k1, m1))

	out.Reset()
	require.NoError(t, runPropose(ctx, testDB, &out, merge, actor, waveTagW1, 0, true))
	var proposal model.CatalogMergeProposal
	require.NoError(t, testDB.Where("entity_type = ?", model.EntityTypeWork).First(&proposal).Error)
	assert.Equal(t, m1, proposal.SourceEntityID)
	assert.Equal(t, k1, proposal.TargetEntityID)
	assert.Equal(t, model.ProposalStatusApproved, proposal.Status)
	assert.NotNil(t, proposal.ExecuteAfter, "approval must arm the 48h cooling window")
	assert.Contains(t, proposal.Note, waveTagW1)
	assert.Contains(t, proposal.Note, "ev=date(2015-08-10~2015-09-09)")
	assert.Contains(t, proposal.Note, "norm=重複タイトルテスト検証")
	assert.Equal(t, model.CandidateStatusAccepted, candidateStatus(t, k1, m1))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, k2, m2))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, k3, m3))

	out.Reset()
	require.NoError(t, runExecute(ctx, testDB, &out, merge, resolve, actor, waveTagW1, 0, true))
	assert.Contains(t, out.String(), "cooled=0", "an approved proposal must wait out its cooling window")

	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)
	out.Reset()
	require.NoError(t, runExecute(ctx, testDB, &out, merge, resolve, actor, waveTagW1, 0, true))
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_redirect WHERE entity_type = ? AND old_id = ? AND current_id = ?`,
		model.EntityTypeWork, m1, k1))
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_work WHERE id = ? AND deleted_at IS NOT NULL`, m1))
	require.NoError(t, testDB.First(&proposal, proposal.ID).Error)
	assert.Equal(t, model.ProposalStatusExecuted, proposal.Status)

	out.Reset()
	fresh, err := runWatch(ctx, testDB, &out)
	require.NoError(t, err)
	assert.Zero(t, fresh)
	assert.Contains(t, out.String(), "pairs=2 new=0 pending=0 needs_manual=2")
	assert.Contains(t, out.String(), "merged=1")

	out.Reset()
	require.NoError(t, runPropose(ctx, testDB, &out, merge, actor, waveTagW1, 0, true))
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_merge_proposal`),
		"a second propose pass must not re-open the merge")

	// ExecuteMerge deletes the pair's own candidate row, so the superseded lane
	// is reached by a candidate filed AFTER the merge — a detector pass that
	// still had the pre-merge pair in hand, or an operator re-filing it.
	require.NoError(t, testDB.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(k1, m1), BID: max(k1, m1),
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusPending,
	}).Error)
	out.Reset()
	require.NoError(t, runPropose(ctx, testDB, &out, merge, actor, waveTagW1, 0, true))
	assert.Contains(t, out.String(), "superseded=1")
	assert.Equal(t, model.CandidateStatusAccepted, candidateStatus(t, k1, m1))
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))
}

func TestNightlyChainsSeedProposeExecute(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	kungal := "kungal"
	const actor = int64(4242)

	k1 := mkWork(t, medium, "夜間クレーム作品テスト", &kungal)
	mkTitle(t, k1, "夜間重複タイトル検証")
	mkRelease(t, k1, 2015, 8, 10)
	m1 := mkWork(t, medium, "夜間 重複 タイトル 検証", nil)
	mkAnchor(t, m1, 3, "778")
	mkRelease(t, m1, 2015, 9, 9)

	k2 := mkWork(t, medium, "夜間衝突元作品テスト", nil)
	mkTitle(t, k2, "夜間衝突アンカー検証")
	mkAnchor(t, k2, 3, "112")
	m2 := mkWork(t, medium, "夜間衝突先作品テスト", nil)
	mkTitle(t, m2, "夜間衝突アンカー検証")
	mkAnchor(t, m2, 3, "223")

	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	var out bytes.Buffer

	fresh, err := runNightly(ctx, testDB, &out, merge, resolve, actor, waveTagW1, 0, false)
	require.NoError(t, err)
	assert.Zero(t, fresh, "a dry nightly inserts nothing, so it has nothing new to report")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_match_candidate`), "dry nightly writes nothing")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))

	out.Reset()
	fresh, err = runNightly(ctx, testDB, &out, merge, resolve, actor, waveTagW1, 0, true)
	require.NoError(t, err)
	assert.Equal(t, 1, fresh, "only the anchor-conflict pair lands in needs_manual")
	assert.Contains(t, out.String(), "needs_manual_new=1")
	assert.Contains(t, out.String(), "cooled=0",
		"a proposal approved in this same run must still wait out its cooling window")
	assert.Equal(t, model.CandidateStatusAccepted, candidateStatus(t, k1, m1))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, k2, m2))
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))

	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)
	out.Reset()
	fresh, err = runNightly(ctx, testDB, &out, merge, resolve, actor, waveTagW1, 0, true)
	require.NoError(t, err)
	assert.Zero(t, fresh, "the second night re-files nothing, so the cron stays quiet")
	assert.Contains(t, out.String(), "executed=1")
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_redirect WHERE entity_type = ? AND old_id = ? AND current_id = ?`,
		model.EntityTypeWork, m1, k1))
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_work WHERE id = ? AND deleted_at IS NOT NULL`, m1))
}

func TestWatchReportsUndecidedPairs(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)

	a := mkWork(t, medium, "監視対象作品エー", nil)
	mkTitle(t, a, "監視新規ペア検証")
	mkAnchor(t, a, 3, "555")
	b := mkWork(t, medium, "監視対象作品ビー", nil)
	mkTitle(t, b, "監視 新規 ペア 検証")
	mkAnchor(t, b, 3, "556")

	var out bytes.Buffer
	fresh, err := runWatch(ctx, testDB, &out)
	require.NoError(t, err)
	assert.Equal(t, 1, fresh, "an unfiled pair is the finding the cron alerts on")
	assert.Contains(t, out.String(), "pairs=1 new=1")
	assert.Contains(t, out.String(), fmt.Sprintf("new: %d<->%d", min(a, b), max(a, b)))

	require.NoError(t, runSeed(ctx, testDB, &out, 1, true))
	out.Reset()
	fresh, err = runWatch(ctx, testDB, &out)
	require.NoError(t, err)
	assert.Zero(t, fresh, "a filed pair is no longer new")
	assert.Contains(t, out.String(), "needs_manual=1")
}

func TestProposeLimitLeavesTheRestPending(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	kungal := "kungal"

	mk := func(title, spaced string, y int16) (int64, int64) {
		survivor := mkWork(t, medium, "限度テスト"+title, &kungal)
		mkTitle(t, survivor, title)
		mkRelease(t, survivor, y, 3, 1)
		member := mkWork(t, medium, spaced, nil)
		mkRelease(t, member, y, 3, 20)
		return survivor, member
	}
	s1, m1 := mk("限度検証作品アルファ", "限度 検証 作品 アルファ", 2016)
	s2, m2 := mk("限度検証作品ベータ", "限度 検証 作品 ベータ", 2017)

	var out bytes.Buffer
	require.NoError(t, runSeed(ctx, testDB, &out, 1, true))
	out.Reset()
	require.NoError(t, runPropose(ctx, testDB, &out, newMerge(t), 1, waveTagW1, 1, true))
	assert.Contains(t, out.String(), "left_pending=1")
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))

	first := candidateStatus(t, s1, m1)
	second := candidateStatus(t, s2, m2)
	assert.Equal(t, model.CandidateStatusAccepted, first)
	assert.Equal(t, model.CandidateStatusPending, second, "the capped pair keeps its place in the queue")
}

func newMerge(t *testing.T) *service.MergeService {
	t.Helper()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	return service.NewMergeService(testDB, resolve)
}

func findPair(t *testing.T, c *census, a, b int64) (pairRow, bucket) {
	t.Helper()
	lo, hi := min(a, b), max(a, b)
	for i, r := range c.rows {
		if r.A == lo && r.B == hi {
			return r, c.verdicts[i]
		}
	}
	t.Fatalf("no pair (%d,%d) in %d rows: %+v", lo, hi, len(c.rows), c.rows)
	return pairRow{}, ""
}

func hasPair(c *census, a, b int64) bool {
	lo, hi := min(a, b), max(a, b)
	for _, r := range c.rows {
		if r.A == lo && r.B == hi {
			return true
		}
	}
	return false
}

func candidateOf(t *testing.T, a, b int64) model.CatalogMatchCandidate {
	t.Helper()
	var cand model.CatalogMatchCandidate
	require.NoError(t, testDB.Where("entity_type = ? AND a_id = ? AND b_id = ?",
		model.EntityTypeWork, min(a, b), max(a, b)).First(&cand).Error)
	return cand
}

func TestWorkDedupCensusWidening(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	const officialSite int16 = 9

	t.Run("a_case_insensitive_ref", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "ケースエイ参照作品テスト", nil)
		b := mkWork(t, medium, "ケースビー参照作品テスト", nil)
		mkRef(t, a, officialSite, "www.example.com/home/x", model.LinkKindExact, nil)
		mkRef(t, b, officialSite, "www.example.com/Home/x", model.LinkKindProbable, nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.True(t, row.RefOverlapCI)
		assert.False(t, row.RefOverlap)
		assert.Equal(t, 0, row.SharedNorms)
		assert.Equal(t, bucketRefCI, verdict)

		var out bytes.Buffer
		require.NoError(t, runSeed(ctx, testDB, &out, 1, true))
		got := candidateOf(t, a, b)
		assert.Equal(t, model.CandidateStatusNeedsManual, got.Status)
		assert.Equal(t, model.CandidateReasonSharedExternalID, got.Reason)
	})

	t.Run("b_exact_ref_outranks_year_clash", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "年差エイ参照作品テスト", nil)
		b := mkWork(t, medium, "年差ビー参照作品テスト", nil)
		mkRef(t, a, officialSite, "www.example.com/ident/b", model.LinkKindProbable, nil)
		mkRef(t, b, officialSite, "www.example.com/ident/b", model.LinkKindExact, nil)
		mkRelease(t, a, 2014, 5, 1)
		mkRelease(t, b, 2016, 5, 1)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.True(t, row.RefOverlap)
		assert.Equal(t, bucketAuto, verdict)
	})

	t.Run("c_han_three_rune_floor", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "红楼梦", nil)
		b := mkWork(t, medium, "红楼梦", nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.True(t, hasPair(c, a, b), "3-rune all-Han display_name must pair")
	})

	t.Run("d_ascii_three_rune_no_pair", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "abc", nil)
		b := mkWork(t, medium, "abc", nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, a, b), "3-rune ASCII display_name must not pair")
	})

	t.Run("e_alias_only_and_abbreviation_excluded", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "エイ公式タイトルホルダー作品", nil)
		b := mkWork(t, medium, "ビー別名タイトルホルダー作品", nil)
		mkTitleKind(t, a, "共有別名タイトル検証", model.WorkTitleKindOfficial)
		mkTitleKind(t, b, "共有別名タイトル検証", model.WorkTitleKindAlias)
		lab := &model.CatalogLabel{DisplayName: "共有ブランド検証", Lang: "ja", Kind: model.LabelKindGameBrand, FieldProvenance: []byte(`{}`)}
		require.NoError(t, testDB.Create(lab).Error)
		require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
			WorkID: a, LabelID: lab.ID, Kind: model.WorkLabelKindBrand,
		}).Error)
		require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
			WorkID: b, LabelID: lab.ID, Kind: model.WorkLabelKindBrand,
		}).Error)

		cAbbrev := mkWork(t, medium, "シー短縮名ホルダー作品", nil)
		dAbbrev := mkWork(t, medium, "ディー短縮名ホルダー作品", nil)
		mkTitleKind(t, cAbbrev, "短縮名検証用", model.WorkTitleKindAbbreviation)
		mkTitleKind(t, dAbbrev, "短縮名検証用", model.WorkTitleKindAbbreviation)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.Equal(t, 1, row.SharedNorms)
		assert.Equal(t, 0, row.SharedOfficial)
		assert.True(t, row.LabelOverlap)
		assert.Equal(t, bucketAliasOnly, verdict)
		assert.False(t, hasPair(c, cAbbrev, dAbbrev), "kind=2 abbreviation must not generate a pair")
	})

	t.Run("f_dead_ref_no_pair", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "デッドエイ参照作品テスト", nil)
		b := mkWork(t, medium, "デッドビー参照作品テスト", nil)
		dead := time.Now()
		mkRef(t, a, officialSite, "www.example.com/dead/x", model.LinkKindProbable, &dead)
		mkRef(t, b, officialSite, "www.example.com/dead/x", model.LinkKindExact, nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, a, b), "a dead_at ref must not generate a pair")
	})

	t.Run("g_soft_deleted_work_no_pair", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "削除エイ参照作品テスト", nil)
		b := mkWork(t, medium, "削除ビー参照作品テスト", nil)
		mkRef(t, a, officialSite, "www.example.com/gone/x", model.LinkKindProbable, nil)
		mkRef(t, b, officialSite, "www.example.com/gone/x", model.LinkKindExact, nil)
		require.NoError(t, testDB.Delete(&model.CatalogWork{}, a).Error)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, a, b), "a soft-deleted work must not generate a pair")
	})

	t.Run("h_related_url_pair_needs_manual", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "関連エイ参照作品テスト", nil)
		b := mkWork(t, medium, "関連ビー参照作品テスト", nil)
		mkRef(t, a, officialSite, "www.example.com/related/h", model.LinkKindRelated, nil)
		mkRef(t, b, officialSite, "www.example.com/related/h", model.LinkKindRelated, nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.True(t, row.RefOverlapCI)
		assert.False(t, row.RefOverlap)
		assert.Equal(t, bucketRefCI, verdict)
	})

	t.Run("i_related_url_conflicting_anchors_no_pair", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "衝突関連エイ参照作品", nil)
		b := mkWork(t, medium, "衝突関連ビー参照作品", nil)
		mkRef(t, a, officialSite, "www.example.com/related/i", model.LinkKindRelated, nil)
		mkRef(t, b, officialSite, "www.example.com/related/i", model.LinkKindRelated, nil)
		mkAnchor(t, a, 3, "111")
		mkAnchor(t, b, 3, "222")

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, a, b), "kind=2 URL plus conflicting exact anchors must not pair")
	})

	t.Run("j_related_url_does_not_grant_auto", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		a := mkWork(t, medium, "年差関連エイ作品テスト", nil)
		b := mkWork(t, medium, "年差関連ビー作品テスト", nil)
		mkTitle(t, a, "関連年差作品検証テスト")
		mkTitle(t, b, "関連年差作品検証テスト")
		mkRef(t, a, officialSite, "www.example.com/related/j", model.LinkKindRelated, nil)
		mkRef(t, b, officialSite, "www.example.com/related/j", model.LinkKindRelated, nil)
		mkRelease(t, a, 2014, 5, 1)
		mkRelease(t, b, 2016, 5, 1)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.False(t, row.RefOverlap)
		assert.True(t, row.RefOverlapCI)
		assert.Equal(t, bucketDateClash, verdict)
	})

	t.Run("k_release_ref_pairs_a_work_ref", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		const vndb, bgm, dmm int16 = 2, 3, 15
		a := mkWork(t, medium, "面影レイルバック・エピソード捌　泥亀の月", nil)
		b := mkWork(t, medium, "泥亀の月", nil)
		mkAnchor(t, a, vndb, "v21522")
		mkAnchor(t, b, bgm, "226725")
		mkAnchor(t, b, dmm, "hobc_0507")
		mkReleaseRef(t, a, dmm, "hobc_0507")

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		row, verdict := findPair(t, c, a, b)
		assert.True(t, row.CrossRefOnly)
		assert.False(t, row.RefOverlap, "a cross-level id is a lead for the judge, not an auto merge")
		assert.Equal(t, bucketReleaseRef, verdict)

		var out bytes.Buffer
		require.NoError(t, runSeed(ctx, testDB, &out, 1, true))
		got := candidateOf(t, a, b)
		assert.Equal(t, model.CandidateStatusNeedsManual, got.Status)
		assert.Equal(t, model.CandidateReasonSharedExternalID, got.Reason)
	})

	t.Run("l_release_ref_vetoed_by_anchor_conflict", func(t *testing.T) {
		cleanPipeline(t)
		medium := galgameMedium(t)
		const vndb, dmm int16 = 2, 15
		a := mkWork(t, medium, "本編リリース参照作品", nil)
		b := mkWork(t, medium, "追加ディスク参照作品", nil)
		mkAnchor(t, a, vndb, "v1")
		mkAnchor(t, b, vndb, "v2")
		mkAnchor(t, b, dmm, "shared_0001")
		mkReleaseRef(t, a, dmm, "shared_0001")

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, a, b), "two VNDB entries sharing a store page are two works")
	})
}

func mkReleaseRef(t *testing.T, workID int64, sourceID int16, externalID string) {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	require.NoError(t, testDB.Create(rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: sourceID,
		ExternalID: externalID, LinkKind: model.LinkKindExact, MatchedBy: "test:work-dedup",
	}).Error)
}

func catalogMedium(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "%s medium is not seeded", key)
	return id
}

func mkCandidate(t *testing.T, a, b int64, status int16, decidedBy *int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
		Reason: model.CandidateReasonNameNormEqual, Status: status,
		DecidedBy: decidedBy,
	}).Error)
}

func TestWorkDedupCensusMediumGate(t *testing.T) {
	requireDB(t)
	ctx := context.Background()

	t.Run("title_collision", func(t *testing.T) {
		cleanPipeline(t)
		gal := galgameMedium(t)
		anime := catalogMedium(t, "anime")

		crossA := mkWork(t, gal, "クロスメディアエイ作品テスト", nil)
		crossB := mkWork(t, anime, "クロスメディアビー作品テスト", nil)
		mkTitle(t, crossA, "クロスメディア重複タイトル検証")
		mkTitle(t, crossB, "クロスメディア重複タイトル検証")

		sameA := mkWork(t, gal, "同メディアエイ作品テスト", nil)
		sameB := mkWork(t, gal, "同メディアビー作品テスト", nil)
		mkTitle(t, sameA, "同メディア重複タイトル検証")
		mkTitle(t, sameB, "同メディア重複タイトル検証")

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, crossA, crossB), "cross-medium title collision must not pair")
		assert.True(t, hasPair(c, sameA, sameB), "same-medium title collision must pair")
	})

	t.Run("ref_collision", func(t *testing.T) {
		cleanPipeline(t)
		gal := galgameMedium(t)
		anime := catalogMedium(t, "anime")
		const officialSite int16 = 9

		crossA := mkWork(t, gal, "クロスメディア参照エイ作品", nil)
		crossB := mkWork(t, anime, "クロスメディア参照ビー作品", nil)
		mkRef(t, crossA, officialSite, "www.example.com/medium/gate", model.LinkKindExact, nil)
		mkRef(t, crossB, officialSite, "www.example.com/medium/gate", model.LinkKindProbable, nil)

		sameA := mkWork(t, gal, "同メディア参照エイ作品テスト", nil)
		sameB := mkWork(t, gal, "同メディア参照ビー作品テスト", nil)
		mkRef(t, sameA, officialSite, "www.example.com/medium/gate-same", model.LinkKindExact, nil)
		mkRef(t, sameB, officialSite, "www.example.com/medium/gate-same", model.LinkKindProbable, nil)

		c, err := buildCensus(ctx, testDB)
		require.NoError(t, err)
		assert.False(t, hasPair(c, crossA, crossB), "cross-medium ref collision must not pair")
		assert.True(t, hasPair(c, sameA, sameB), "same-medium ref collision must pair")
	})
}

func TestCrossMediumRejectsOnlyOpenCrossMediumRows(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	gal := galgameMedium(t)
	anime := catalogMedium(t, "anime")
	const actor = int64(4242)

	pendingA := mkWork(t, gal, "クロスメディア未決エイ作品", nil)
	pendingB := mkWork(t, anime, "クロスメディア未決ビー作品", nil)
	mkCandidate(t, pendingA, pendingB, model.CandidateStatusPending, nil)

	deferredA := mkWork(t, gal, "クロスメディア保留エイ作品", nil)
	deferredB := mkWork(t, anime, "クロスメディア保留ビー作品", nil)
	mkCandidate(t, deferredA, deferredB, model.CandidateStatusDeferred, nil)

	manualA := mkWork(t, gal, "クロスメディア手動エイ作品", nil)
	manualB := mkWork(t, anime, "クロスメディア手動ビー作品", nil)
	mkCandidate(t, manualA, manualB, model.CandidateStatusNeedsManual, nil)

	prior := int64(7)
	rejectedA := mkWork(t, gal, "クロスメディア既決エイ作品", nil)
	rejectedB := mkWork(t, anime, "クロスメディア既決ビー作品", nil)
	mkCandidate(t, rejectedA, rejectedB, model.CandidateStatusRejected, &prior)

	sameA := mkWork(t, gal, "同メディア未決エイ作品テスト", nil)
	sameB := mkWork(t, gal, "同メディア未決ビー作品テスト", nil)
	mkCandidate(t, sameA, sameB, model.CandidateStatusPending, nil)

	goneA := mkWork(t, gal, "削除クロスメディアエイ作品", nil)
	goneB := mkWork(t, anime, "削除クロスメディアビー作品", nil)
	mkCandidate(t, goneA, goneB, model.CandidateStatusPending, nil)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, goneA).Error)

	var out bytes.Buffer
	require.NoError(t, runCrossMedium(ctx, testDB, &out, actor, false))
	assert.Contains(t, out.String(), "DRY-RUN")
	assert.Contains(t, out.String(), "n=4")
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, pendingA, pendingB))
	assert.Equal(t, model.CandidateStatusDeferred, candidateStatus(t, deferredA, deferredB))
	assert.Equal(t, model.CandidateStatusNeedsManual, candidateStatus(t, manualA, manualB))
	assert.Equal(t, model.CandidateStatusRejected, candidateStatus(t, rejectedA, rejectedB))
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, sameA, sameB))
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, goneA, goneB))

	out.Reset()
	require.NoError(t, runCrossMedium(ctx, testDB, &out, actor, true))
	assert.Contains(t, out.String(), "APPLIED")
	assert.Contains(t, out.String(), "rejected=4")

	assert.Equal(t, model.CandidateStatusRejected, candidateStatus(t, pendingA, pendingB))
	assert.Equal(t, model.CandidateStatusRejected, candidateStatus(t, deferredA, deferredB))
	assert.Equal(t, model.CandidateStatusRejected, candidateStatus(t, manualA, manualB))
	assert.Equal(t, model.CandidateStatusRejected, candidateStatus(t, goneA, goneB))
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, sameA, sameB),
		"same-medium row must be untouched")
	gotRej := candidateOf(t, rejectedA, rejectedB)
	assert.Equal(t, model.CandidateStatusRejected, gotRej.Status)
	require.NotNil(t, gotRej.DecidedBy)
	assert.Equal(t, prior, *gotRej.DecidedBy, "already-rejected row must keep its original actor")

	gotPending := candidateOf(t, pendingA, pendingB)
	require.NotNil(t, gotPending.DecidedBy)
	assert.Equal(t, actor, *gotPending.DecidedBy)
	require.NotNil(t, gotPending.DecidedAt)
}
