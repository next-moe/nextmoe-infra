package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pairsNote = "op:work-pairs"

func writePairsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pairs.tsv")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestLoadPairsParsesCommentsBlanksAndSpacedEvidence(t *testing.T) {
	path := writePairsFile(t, ""+
		"# heading\n"+
		"\n"+
		"  # indented comment\n"+
		"10\t20\tevidence with spaces inside\n"+
		"\n"+
		"30\t20\tsecond source into the same target\n")
	got, err := loadPairs(path)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(10), got[0].source)
	assert.Equal(t, int64(20), got[0].target)
	assert.Equal(t, "evidence with spaces inside", got[0].evidence)
	assert.Equal(t, int64(30), got[1].source)
	assert.Equal(t, int64(20), got[1].target)
	assert.Equal(t, "second source into the same target", got[1].evidence)
}

func TestLoadPairsMalformedShapes(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"two fields", "1\t2\n", "want 3 tab-separated fields"},
		{"non-integer id", "abc\t2\tevidence\n", "source_work_id"},
		{"zero id", "0\t2\tevidence\n", "must be a positive id"},
		{"negative id", "-1\t2\tevidence\n", "must be a positive id"},
		{"blank evidence", "1\t2\t   \n", "evidence is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writePairsFile(t, tc.body)
			_, err := loadPairs(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), path+":1:")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadPairsEmptyFile(t *testing.T) {
	path := writePairsFile(t, "")
	_, err := loadPairs(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path+": no pairs")
}

func TestLoadPairsSourceEqualsTarget(t *testing.T) {
	path := writePairsFile(t, "7\t7\tevidence\n")
	_, err := loadPairs(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path+":1:")
	assert.Contains(t, err.Error(), "source and target are both 7")
}

func TestLoadPairsDuplicateSource(t *testing.T) {
	path := writePairsFile(t, "1\t2\tevidence\n1\t3\tother\n")
	_, err := loadPairs(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path+":2:")
	assert.Contains(t, err.Error(), "work 1 is already a source on line 1")
}

func TestLoadPairsSourceAndTarget(t *testing.T) {
	path := writePairsFile(t, "1\t2\tevidence\n2\t3\tother\n")
	_, err := loadPairs(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path+":2:")
	assert.Contains(t, err.Error(), "work 2 is a target on line 1 and a source here")
}

func TestLoadPairsSourceThenTarget(t *testing.T) {
	path := writePairsFile(t, "1\t2\tevidence\n3\t1\tother\n")
	_, err := loadPairs(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path+":2:")
	assert.Contains(t, err.Error(), "work 1 is a source on line 1 and a target here")
}

func TestRunPairsNoteGuard(t *testing.T) {
	path := writePairsFile(t, "1\t2\tevidence\n")
	cases := []struct {
		name, note, want string
	}{
		{"empty", "", "note must not be empty"},
		{"whitespace", "  \t ", "note must not be empty"},
		{"wave tag w1", waveTagW1, `note must not be "rule:work-dedup w1"`},
		{"contains rule:work-dedup", "rule:work-dedup nightly", `contains reserved tag "rule:work-dedup"`},
		{"contains llm:queue-adjudicator", "llm:queue-adjudicator 1", `contains reserved tag "llm:queue-adjudicator"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runPairs(context.Background(), nil, &out, nil, nil, 1, tc.note, path, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, out.String())
		})
	}
}

func TestRunPairsDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	src := mkWork(t, medium, "ペアドライ源作品", nil)
	tgt := mkWork(t, medium, "ペアドライ先作品", nil)
	path := writePairsFile(t, fmt.Sprintf("%d\t%d\toperator evidence\n", src, tgt))

	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	var out bytes.Buffer
	require.NoError(t, runPairs(ctx, testDB, &out, merge, resolve, 4242, pairsNote, path, false))
	assert.Contains(t, out.String(), fmt.Sprintf("PLAN %d <- %d  operator evidence", tgt, src))
	assert.Contains(t, out.String(), "DRY-RUN (pass -run to file) [pairs]")
	assert.Contains(t, out.String(), "proposed=1")
	assert.Contains(t, out.String(), "skipped_existing=0")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))
}

func TestRunPairsFilesOpenProposalsAndSkipsExisting(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	src := mkWork(t, medium, "ペア実行源作品", nil)
	tgt := mkWork(t, medium, "ペア実行先作品", nil)
	path := writePairsFile(t, fmt.Sprintf("%d\t%d\toperator evidence\n", src, tgt))

	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	var out bytes.Buffer
	require.NoError(t, runPairs(ctx, testDB, &out, merge, resolve, 4242, pairsNote, path, true))

	var p model.CatalogMergeProposal
	require.NoError(t, testDB.Where("entity_type = ?", model.EntityTypeWork).First(&p).Error)
	assert.Equal(t, src, p.SourceEntityID)
	assert.Equal(t, tgt, p.TargetEntityID)
	assert.Equal(t, model.ProposalStatusOpen, p.Status)
	assert.Contains(t, p.Note, pairsNote)
	assert.Contains(t, p.Note, "operator evidence")
	assert.Contains(t, out.String(), fmt.Sprintf("OK proposal #%d %d <- %d", p.ID, tgt, src))
	assert.Contains(t, out.String(), "APPLIED [pairs]")
	assert.Contains(t, out.String(), "proposed=1")

	out.Reset()
	require.NoError(t, runPairs(ctx, testDB, &out, merge, resolve, 4242, pairsNote, path, true))
	assert.Contains(t, out.String(), fmt.Sprintf("SKIP existing proposal #%d (open) %d <- %d", p.ID, tgt, src))
	assert.Contains(t, out.String(), "skipped_existing=1")
	assert.Contains(t, out.String(), "proposed=0")
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))
}

func TestRunPairsMovedSourceReturnsErrorAndFilesNothing(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	const actor = int64(4242)
	survivor := mkWork(t, medium, "ペア移動生存作品", nil)
	member := mkWork(t, medium, "ペア移動既マージ源", nil)
	later := mkWork(t, medium, "ペア移動後の提案先", nil)

	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, member, survivor, actor, "setup")
	require.NoError(t, err)
	require.NoError(t, merge.ApproveMerge(ctx, p.ID, actor))
	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour' WHERE id = ?`, p.ID).Error)
	executedBy := actor
	require.NoError(t, merge.ExecuteMerge(ctx, p.ID, &executedBy))

	cleanSrc := mkWork(t, medium, "ペア移動前の健全な源", nil)
	cleanTgt := mkWork(t, medium, "ペア移動前の健全な先", nil)
	path := writePairsFile(t, fmt.Sprintf("%d\t%d\tclean row first\n%d\t%d\tstale adjudication\n",
		cleanSrc, cleanTgt, member, later))
	var out bytes.Buffer
	err = runPairs(ctx, testDB, &out, merge, resolve, actor, pairsNote, path, true)
	require.Error(t, err)
	assert.Contains(t, out.String(), fmt.Sprintf("SKIP moved %d→%d %d→%d", member, survivor, later, later))
	assert.Contains(t, out.String(), "moved=1")
	assert.Contains(t, out.String(), "proposed=0")
	assert.Zero(t, countRows(t,
		`SELECT count(*) FROM catalog_merge_proposal WHERE id <> ?`, p.ID),
		"a moved row anywhere in the file files nothing, the clean row ahead of it included")
}

func TestRunPairsEndToEndApproveExecute(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	kungal := "kungal"
	const actor = int64(4242)
	srcName := "ペアA源表示名"

	tgtA := mkWork(t, medium, "ペアA先空名", &kungal)
	var productID int64
	require.NoError(t, testDB.Raw(`SELECT product_work_id FROM catalog_work WHERE id = ?`, tgtA).Scan(&productID).Error)
	require.NotZero(t, productID)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET display_name = '' WHERE id = ?`, tgtA).Error)
	srcA := mkWork(t, medium, srcName, nil)
	mkAnchor(t, srcA, model.SourceBangumi, "91001")

	tgtB := mkWork(t, medium, "ペアB先アンカー衝突", nil)
	srcB := mkWork(t, medium, "ペアB源アンカー衝突", nil)
	mkAnchor(t, srcB, vndbSource, "v201")
	mkAnchor(t, tgtB, vndbSource, "v202")

	path := writePairsFile(t, fmt.Sprintf("%d\t%d\tpair A evidence\n%d\t%d\tpair B evidence\n",
		srcA, tgtA, srcB, tgtB))
	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	var out bytes.Buffer
	require.NoError(t, runPairs(ctx, testDB, &out, merge, resolve, actor, pairsNote, path, true))
	assert.Equal(t, int64(2), countRows(t, `SELECT count(*) FROM catalog_merge_proposal WHERE status = ?`,
		model.ProposalStatusOpen))

	out.Reset()
	require.NoError(t, runApprove(ctx, testDB, &out, merge, resolve, actor, pairsNote, 0, true))

	var propA, propB model.CatalogMergeProposal
	require.NoError(t, testDB.Where("source_entity_id = ?", srcA).First(&propA).Error)
	require.NoError(t, testDB.Where("source_entity_id = ?", srcB).First(&propB).Error)
	assert.Equal(t, model.ProposalStatusApproved, propA.Status)
	assert.Equal(t, model.ProposalStatusRejected, propB.Status)
	assert.Contains(t, propB.Note, "ref-conflict")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour' WHERE id = ?`,
		propA.ID).Error)

	out.Reset()
	require.NoError(t, runExecute(ctx, testDB, &out, merge, resolve, actor, pairsNote, 0, true))

	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_redirect WHERE entity_type = ? AND old_id = ? AND current_id = ?`,
		model.EntityTypeWork, srcA, tgtA))
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_work WHERE id = ? AND status = ?`, srcA, model.WorkStatusMerged))

	var gotName, gotSite string
	var gotProduct int64
	require.NoError(t, testDB.Raw(
		`SELECT display_name, site, product_work_id FROM catalog_work WHERE id = ?`, tgtA,
	).Row().Scan(&gotName, &gotSite, &gotProduct))
	assert.Equal(t, srcName, gotName)
	assert.Equal(t, kungal, gotSite)
	assert.Equal(t, productID, gotProduct)

	before := countRows(t, `SELECT count(*) FROM catalog_merge_proposal`)
	out.Reset()
	require.NoError(t, runPairs(ctx, testDB, &out, merge, resolve, actor, pairsNote, path, true),
		"re-running an executed worklist is a no-op, not a moved-endpoint failure")
	assert.Contains(t, out.String(), fmt.Sprintf("SKIP existing proposal #%d (executed)", propA.ID))
	assert.Contains(t, out.String(), fmt.Sprintf("SKIP existing proposal #%d (rejected)", propB.ID))
	assert.Contains(t, out.String(), "skipped_existing=2")
	assert.Equal(t, before, countRows(t, `SELECT count(*) FROM catalog_merge_proposal`),
		"a ref-conflict rejection is a decision; the rerun must not file the pair again")
}

func TestRunPairsProposeFailureIsAnError(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	src := mkWork(t, medium, "ペア失敗源作品", nil)
	gone := mkWork(t, medium, "ペア失敗削除済み先", nil)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, gone).Error)
	okSrc := mkWork(t, medium, "ペア失敗横の健全な源", nil)
	okTgt := mkWork(t, medium, "ペア失敗横の健全な先", nil)
	path := writePairsFile(t, fmt.Sprintf("%d\t%d\tdeleted target\n%d\t%d\thealthy neighbour\n", src, gone, okSrc, okTgt))

	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	var out bytes.Buffer
	err := runPairs(ctx, testDB, &out, merge, resolve, 4242, pairsNote, path, true)
	require.Error(t, err)
	assert.Contains(t, out.String(), fmt.Sprintf("%d->%d: propose ERROR", src, gone))
	assert.Contains(t, out.String(), "proposed=1")
	assert.Contains(t, out.String(), "failed=1")
	assert.Equal(t, int64(1), countRows(t,
		`SELECT count(*) FROM catalog_merge_proposal WHERE source_entity_id = ? AND target_entity_id = ?`, okSrc, okTgt))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_merge_proposal WHERE source_entity_id = ?`, src))
}
