package service

import (
	"sort"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanSplitTables(t *testing.T) {
	t.Helper()
	cleanTables(t)
	for _, table := range []string{"catalog_split_request", "catalog_match_candidate", "catalog_match_rejection",
		"catalog_merge_proposal", "catalog_merge_movement", "catalog_redirect", "catalog_revision", "catalog_credit"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

type splitFixture struct {
	orig                     *model.CatalogWork
	keep, moveA, moveB       *model.CatalogRelease
	creditOnMoved, creditAll int64
}

func seedSplitFixture(t *testing.T) splitFixture {
	t.Helper()
	orig := createWork(t, "Rewrite")
	keep := createRelease(t, orig.ID, 2011, 6, 24)
	moveA := createRelease(t, orig.ID, 2016, 7, 29)
	moveB := createRelease(t, orig.ID, 2021, 12, 20)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET title = 'Rewrite', lang = 'ja', extra = '{"vndb_id":"r1513"}' WHERE id = ?`, keep.ID).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET title = 'Rewrite+', lang = 'ja', extra = '{"vndb_id":"r45775"}' WHERE id = ?`, moveA.ID).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET title = 'Rewrite+ Steam Edition', lang = 'en', extra = '{}' WHERE id = ?`, moveB.ID).Error)
	addExternalRef(t, model.EntityTypeWork, orig.ID, model.SourceVNDB, "v751", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, orig.ID, model.SourceBangumi, "4022", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, orig.ID, model.SourceBangumi, "491620", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeRelease, moveA.ID, model.SourceVNDB, "r45775", model.LinkKindExact)
	role := seededRoleID(t)
	onMoved := createCredit(t, orig.ID, createCreditName(t, nil, "Plus 限定").ID, role, nil)
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit SET release_id = ? WHERE id = ?`, moveA.ID, onMoved.ID).Error)
	everywhere := createCredit(t, orig.ID, createCreditName(t, nil, "通用").ID, role, nil)
	creditOnMoved, creditAll := onMoved.ID, everywhere.ID
	return splitFixture{orig: orig, keep: keep, moveA: moveA, moveB: moveB, creditOnMoved: creditOnMoved, creditAll: creditAll}
}

func splitPlus(f splitFixture) SplitInput {
	return SplitInput{
		WorkID: f.orig.ID, ReleaseIDs: []int64{f.moveA.ID, f.moveB.ID},
		DisplayName: "Rewrite+", OLang: "ja",
		Titles:      []model.SplitTitle{{Lang: "ja", Title: "Rewrite+", Kind: model.WorkTitleKindOfficial}},
		PromoteRefs: []model.SplitRef{{Source: "bangumi", ExternalID: "491620"}},
		Note:        "Bangumi 491620 单独收录",
	}
}

func approvedSplit(t *testing.T, svc *SplitService, in SplitInput) int64 {
	t.Helper()
	r, err := svc.Request(t.Context(), in, 3)
	require.NoError(t, err)
	require.NoError(t, svc.Approve(t.Context(), r.ID, 7))
	require.NoError(t, testDB.Exec(`UPDATE catalog_split_request SET execute_after = now() - interval '1 minute' WHERE id = ?`, r.ID).Error)
	return r.ID
}

type splitRefState struct {
	Source     int16
	ExternalID string
	LinkKind   int16
	MatchedBy  string
}

func splitWorkRefs(t *testing.T, workID int64) []splitRefState {
	t.Helper()
	var out []splitRefState
	require.NoError(t, testDB.Raw(`SELECT source_id AS source, external_id, link_kind, matched_by FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? ORDER BY source_id, external_id`, model.EntityTypeWork, workID).Scan(&out).Error)
	return out
}

func splitLiveReleases(t *testing.T, workID int64) []int64 {
	t.Helper()
	var ids []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_release WHERE work_id = ? AND deleted_at IS NULL ORDER BY id`, workID).Scan(&ids).Error)
	return ids
}

func TestSplitRequestRefusals(t *testing.T) {
	cleanSplitTables(t)
	ctx := t.Context()
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)
	other := createWork(t, "other")
	addExternalRef(t, model.EntityTypeWork, other.ID, model.SourceErogameScape, "20240", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, f.orig.ID, model.SourceErogameScape, "20240", model.LinkKindRelated)

	cases := map[string]struct {
		mutate func(*SplitInput)
		want   error
	}{
		"no releases":        {func(in *SplitInput) { in.ReleaseIDs = nil }, ErrSplitReleases},
		"every release":      {func(in *SplitInput) { in.ReleaseIDs = []int64{f.keep.ID, f.moveA.ID, f.moveB.ID} }, ErrSplitReleases},
		"foreign release":    {func(in *SplitInput) { in.ReleaseIDs = []int64{f.moveA.ID + 999} }, ErrSplitReleases},
		"own identity":       {func(in *SplitInput) { in.PromoteRefs = []model.SplitRef{{Source: "bangumi", ExternalID: "4022"}} }, ErrSplitRef},
		"not linked":         {func(in *SplitInput) { in.PromoteRefs = []model.SplitRef{{Source: "bangumi", ExternalID: "1"}} }, ErrSplitRef},
		"held elsewhere":     {func(in *SplitInput) { in.PromoteRefs = []model.SplitRef{{Source: "erogamescape", ExternalID: "20240"}} }, ErrSplitRef},
		"not edition source": {func(in *SplitInput) { in.PromoteRefs = []model.SplitRef{{Source: "vndb", ExternalID: "v751"}} }, ErrSplitRef},
		"empty name":         {func(in *SplitInput) { in.DisplayName = "  " }, ErrSplitDraft},
		"bad olang":          {func(in *SplitInput) { in.OLang = "Japanese" }, ErrSplitDraft},
	}
	for name, c := range cases {
		in := splitPlus(f)
		c.mutate(&in)
		_, err := svc.Request(ctx, in, 3)
		require.ErrorIs(t, err, c.want, name)
	}

	r, err := svc.Request(ctx, splitPlus(f), 3)
	require.NoError(t, err)
	_, err = svc.Request(ctx, splitPlus(f), 4)
	require.ErrorIs(t, err, ErrSplitPending)
	require.NoError(t, svc.Approve(ctx, r.ID, 7))
	_, err = svc.Execute(ctx, r.ID, nil)
	require.ErrorIs(t, err, ErrCoolingOff)
	require.NoError(t, svc.Reject(ctx, r.ID, 7, "keep them together"))
	got, err := svc.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusRejected, got.Status)
}

func TestSplitAmendBeforeApproval(t *testing.T) {
	cleanSplitTables(t)
	ctx := t.Context()
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)
	r, err := svc.Request(ctx, splitPlus(f), 3)
	require.NoError(t, err)

	in := splitPlus(f)
	in.ReleaseIDs = []int64{f.moveA.ID}
	in.DisplayName = "Rewrite+ 修正"
	in.PromoteRefs = nil
	require.NoError(t, svc.Amend(ctx, r.ID, 7, in))
	got, err := svc.Get(ctx, r.ID)
	require.NoError(t, err)
	stored, err := splitInputOf(got)
	require.NoError(t, err)
	assert.Equal(t, []int64{f.moveA.ID}, stored.ReleaseIDs)
	assert.Equal(t, "Rewrite+ 修正", stored.DisplayName)
	assert.Empty(t, stored.PromoteRefs)
	require.NotNil(t, got.AmendedBy)
	assert.Equal(t, int64(7), *got.AmendedBy)

	in.ReleaseIDs = []int64{f.keep.ID, f.moveA.ID, f.moveB.ID}
	require.ErrorIs(t, svc.Amend(ctx, r.ID, 7, in), ErrSplitReleases, "an amendment is validated like a filing")
}

func TestSplitPreviewLaysOutTheFacts(t *testing.T) {
	cleanSplitTables(t)
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)

	p, err := svc.Preview(t.Context(), f.orig.ID, []int64{f.moveA.ID, f.moveB.ID}, []model.SplitRef{{Source: "bangumi", ExternalID: "491620"}}, 0)
	require.NoError(t, err)
	assert.Equal(t, "v751", p.VNDBAnchor)
	require.Len(t, p.Releases, 3)
	assert.Empty(t, p.Blockers)
	codes := func(issues []PreviewIssue) []string {
		var out []string
		for _, i := range issues {
			out = append(out, i.Code)
		}
		return out
	}
	assert.Contains(t, codes(p.Facts), "vndb_same_vn")
	assert.Contains(t, codes(p.Facts), "listed_separately")
	assert.Contains(t, codes(p.Warnings), "vndb_updates_stop")
	require.Len(t, p.Candidates, 1)
	assert.Equal(t, "491620", p.Candidates[0].ExternalID)
	assert.True(t, p.Candidates[0].Selected)
	assert.Equal(t, "Rewrite+", p.SuggestedName, "edition suffixes are trimmed off the suggestion")
	moves := map[string]int64{}
	for _, c := range p.Moves {
		moves[c.Key] = c.Count
	}
	assert.Equal(t, int64(2), moves["releases"])
	assert.Equal(t, int64(1), moves["tombstones"], "only the release VNDB knows needs a tombstone")
	assert.Equal(t, int64(1), moves["credits"])

	all, err := svc.Preview(t.Context(), f.orig.ID, []int64{f.keep.ID, f.moveA.ID, f.moveB.ID}, nil, 0)
	require.NoError(t, err)
	assert.Contains(t, codes(all.Blockers), "all_releases")

	r, err := svc.Request(t.Context(), splitPlus(f), 3)
	require.NoError(t, err)
	other, err := svc.Preview(t.Context(), f.orig.ID, nil, nil, 0)
	require.NoError(t, err)
	assert.Contains(t, codes(other.Blockers), "pending_request", "another filer is told a split is pending")
	own, err := svc.Preview(t.Context(), f.orig.ID, nil, nil, r.ID)
	require.NoError(t, err)
	assert.NotContains(t, codes(own.Blockers), "pending_request", "the request's own page does not report itself")
}

func TestSplitExecuteAndUndoRoundTrip(t *testing.T) {
	cleanSplitTables(t)
	ctx := t.Context()
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)
	refsBefore := splitWorkRefs(t, f.orig.ID)
	releasesBefore := splitLiveReleases(t, f.orig.ID)
	reviewer := int64(7)

	id := approvedSplit(t, svc, splitPlus(f))
	closed, err := svc.Execute(ctx, id, &reviewer)
	require.NoError(t, err)
	require.False(t, closed)
	r, err := svc.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, model.ProposalStatusExecuted, r.Status)
	require.NotNil(t, r.NewWorkID)
	nw := *r.NewWorkID

	assert.Equal(t, []int64{f.keep.ID}, splitLiveReleases(t, f.orig.ID))
	assert.Equal(t, []int64{f.moveA.ID, f.moveB.ID}, splitLiveReleases(t, nw))
	var tomb []struct {
		VndbID string
		Moved  string
	}
	require.NoError(t, testDB.Raw(`SELECT extra->>'vndb_id' AS vndb_id, extra->>'moved_to_work' AS moved FROM catalog_release
		WHERE work_id = ? AND deleted_at IS NOT NULL`, f.orig.ID).Scan(&tomb).Error)
	require.Len(t, tomb, 1, "one tombstone, for the release VNDB knows")
	assert.Equal(t, "r45775", tomb[0].VndbID)

	var newWork model.CatalogWork
	require.NoError(t, testDB.First(&newWork, nw).Error)
	assert.Equal(t, "Rewrite+", newWork.DisplayName)
	var titles []string
	require.NoError(t, testDB.Raw(`SELECT title FROM catalog_work_title WHERE work_id = ?`, nw).Scan(&titles).Error)
	assert.Equal(t, []string{"Rewrite+"}, titles)

	var creditWork int64
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_credit WHERE id = ?`, f.creditOnMoved).Scan(&creditWork).Error)
	assert.Equal(t, nw, creditWork, "a credit limited to a moved release moves with it")
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_credit WHERE id = ?`, f.creditAll).Scan(&creditWork).Error)
	assert.Equal(t, f.orig.ID, creditWork)

	promoted := splitWorkRefs(t, nw)
	require.Len(t, promoted, 1)
	assert.Equal(t, model.LinkKindExact, promoted[0].LinkKind)
	assert.Equal(t, "491620", promoted[0].ExternalID)
	var rejected, parked int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_rejection WHERE entity_type = ? AND entity_id = ? AND external_id = '491620'`,
		model.EntityTypeWork, f.orig.ID).Scan(&rejected).Error)
	assert.Equal(t, int64(1), rejected, "attach lanes must not pull the entry back to the original")
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate WHERE entity_type = ? AND status = ?`,
		model.EntityTypeWork, model.CandidateStatusRejected).Scan(&parked).Error)
	assert.Equal(t, int64(1), parked, "the pair is parked so the dedup census leaves it alone")
	var revisions int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_revision WHERE entity_id IN (?, ?) AND action = ?`,
		f.orig.ID, nw, model.RevisionActionSplit).Scan(&revisions).Error)
	assert.Equal(t, int64(2), revisions)

	events, err := svc.Events(ctx, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	firstChange := *events[0].ChangedAt

	require.NoError(t, svc.Undo(ctx, id, reviewer, "VNDB 拆分前先合回"))
	r, err = svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusUndone, r.Status)
	require.NotNil(t, r.UndoMergeID)

	assert.Equal(t, releasesBefore, splitLiveReleases(t, f.orig.ID))
	assert.Equal(t, refsBefore, splitWorkRefs(t, f.orig.ID), "every ref is back exactly as it was")
	var tombs, rejections, candidates int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_release WHERE deleted_at IS NOT NULL`).Scan(&tombs).Error)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_rejection`).Scan(&rejections).Error)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate`).Scan(&candidates).Error)
	assert.Zero(t, tombs)
	assert.Zero(t, rejections)
	assert.Zero(t, candidates)
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_credit WHERE id = ?`, f.creditOnMoved).Scan(&creditWork).Error)
	assert.Equal(t, f.orig.ID, creditWork)
	var origTitles []string
	require.NoError(t, testDB.Raw(`SELECT title FROM catalog_work_title WHERE work_id = ? ORDER BY title`, f.orig.ID).Scan(&origTitles).Error)
	assert.NotContains(t, origTitles, "Rewrite+", "the split's own titles do not come back as aliases")

	var gone struct {
		Status  int16
		Deleted bool
	}
	require.NoError(t, testDB.Raw(`SELECT status, deleted_at IS NOT NULL AS deleted FROM catalog_work WHERE id = ?`, nw).Scan(&gone).Error)
	assert.True(t, gone.Deleted)
	assert.Equal(t, model.WorkStatusMerged, gone.Status)
	var redirect int64
	require.NoError(t, testDB.Raw(`SELECT current_id FROM catalog_redirect WHERE entity_type = ? AND old_id = ?`, model.EntityTypeWork, nw).Scan(&redirect).Error)
	assert.Equal(t, f.orig.ID, redirect, "the new id points back at the original")

	events, err = svc.Events(ctx, firstChange, id, 10)
	require.NoError(t, err)
	require.Len(t, events, 1, "an undo moves the split to the tail of the feed")
	assert.Equal(t, model.ProposalStatusUndone, events[0].Status)
}

func TestSplitUndoWindowAndState(t *testing.T) {
	cleanSplitTables(t)
	ctx := t.Context()
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)
	id := approvedSplit(t, svc, splitPlus(f))
	require.ErrorIs(t, svc.Undo(ctx, id, 7, ""), ErrProposalState, "nothing to undo before execution")
	_, err := svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_split_request SET executed_at = now() - interval '400 days' WHERE id = ?`, id).Error)
	require.ErrorIs(t, svc.Undo(ctx, id, 7, ""), ErrSplitUndo)
}

func TestSplitExecuteClosesWhenTheWorkIsGone(t *testing.T) {
	cleanSplitTables(t)
	ctx := t.Context()
	svc := NewSplitService(testDB, testResolve, NewMergeService(testDB, testResolve))
	f := seedSplitFixture(t)
	id := approvedSplit(t, svc, splitPlus(f))
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, f.orig.ID).Error)
	closed, err := svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	assert.True(t, closed)
	rep, err := svc.ExecuteDue(ctx)
	require.NoError(t, err)
	assert.Zero(t, rep.Due)
}

// Every column that names a release is either moved with it or follows it.
func TestReleaseColumnsAreAllAccountedFor(t *testing.T) {
	var cols []string
	require.NoError(t, testDB.Raw(`SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name LIKE '%release_id%'`).Scan(&cols).Error)
	sort.Strings(cols)
	var known []string
	for col := range releaseKeyedColumns {
		known = append(known, col)
	}
	sort.Strings(known)
	assert.Equal(t, known, cols)
	for _, rr := range releaseScopedWorkRows {
		assert.Contains(t, releaseKeyedColumns, rr.table+".release_id")
	}
}
