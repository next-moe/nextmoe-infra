package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settleWorks(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = now() - interval '1 hour'`).Error)
}

func workUpdatedAt(t *testing.T, id int64) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, id).Scan(&ts).Error)
	return ts
}

func drainChanges(t *testing.T) string {
	t.Helper()
	page, err := newPublicSvc().Changes(t.Context(), "", 500)
	require.NoError(t, err)
	return page.NextCursor
}

func changesSince(t *testing.T, cursor string) []int64 {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = updated_at - interval '10 seconds'`).Error)
	page, err := newPublicSvc().Changes(t.Context(), cursor, 500)
	require.NoError(t, err)
	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestMergeCreditNameTouchesHostWorks(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	person := createPerson(t, "one person")
	target := createCreditName(t, &person.ID, "表記")
	source := createCreditName(t, &person.ID, "表記(旧)")

	role := seededRoleID(t)
	repointHost := createWork(t, "repoint-host")
	dedupHost := createWork(t, "dedup-host")
	bystander := createWork(t, "bystander")
	createCredit(t, repointHost.ID, source.ID, role, nil)
	createCredit(t, dedupHost.ID, target.ID, role, nil)
	createCredit(t, dedupHost.ID, source.ID, role, nil)

	settleWorks(t)
	before := map[int64]time.Time{
		repointHost.ID: workUpdatedAt(t, repointHost.ID),
		dedupHost.ID:   workUpdatedAt(t, dedupHost.ID),
		bystander.ID:   workUpdatedAt(t, bystander.ID),
	}

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeCreditName, source.ID, target.ID, 7, "same spelling")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	assert.True(t, workUpdatedAt(t, repointHost.ID).After(before[repointHost.ID]),
		"the work whose credit was repointed renders a different name now")
	assert.True(t, workUpdatedAt(t, dedupHost.ID).After(before[dedupHost.ID]),
		"the work that lost a duplicate credit renders one row fewer now")
	assert.True(t, workUpdatedAt(t, bystander.ID).Equal(before[bystander.ID]),
		"a work with no credit from either side must stay out of the feed")
}

func TestMergeLabelTouchesHostWorks(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := &model.CatalogLabel{DisplayName: "Brand", Kind: model.LabelKindGameBrand}
	require.NoError(t, testDB.Create(target).Error)
	source := &model.CatalogLabel{DisplayName: "brand", Kind: model.LabelKindGameBrand}
	require.NoError(t, testDB.Create(source).Error)

	edgeHost := createWork(t, "edge-host")
	dedupHost := createWork(t, "dedup-host")
	creditHost := createWork(t, "credit-host")
	bystander := createWork(t, "bystander")
	for _, e := range []model.CatalogWorkLabel{
		{WorkID: edgeHost.ID, LabelID: source.ID, Kind: model.WorkLabelKindCircle},
		{WorkID: dedupHost.ID, LabelID: target.ID, Kind: model.WorkLabelKindCircle},
		{WorkID: dedupHost.ID, LabelID: source.ID, Kind: model.WorkLabelKindCircle},
	} {
		require.NoError(t, testDB.Create(&e).Error)
	}
	person := createPerson(t, "staff")
	name := createCreditName(t, &person.ID, "名義")
	credit := createCredit(t, creditHost.ID, name.ID, seededRoleID(t), nil)
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit SET label_id = ? WHERE id = ?`, source.ID, credit.ID).Error)

	settleWorks(t)
	before := map[int64]time.Time{
		edgeHost.ID:   workUpdatedAt(t, edgeHost.ID),
		dedupHost.ID:  workUpdatedAt(t, dedupHost.ID),
		creditHost.ID: workUpdatedAt(t, creditHost.ID),
		bystander.ID:  workUpdatedAt(t, bystander.ID),
	}

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, source.ID, target.ID, 7, "same brand")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	for _, id := range []int64{edgeHost.ID, dedupHost.ID, creditHost.ID} {
		assert.True(t, workUpdatedAt(t, id).After(before[id]), "work %d renders the merged label now", id)
	}
	assert.True(t, workUpdatedAt(t, bystander.ID).Equal(before[bystander.ID]),
		"a work touching neither label must stay out of the feed")
}

func TestMergeCharacterTouchesHostWorks(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := createCharacter(t, "冬月十夜")
	source := createCharacter(t, "冬月 十夜")

	rosterHost := createWork(t, "roster-host")
	dedupHost := createWork(t, "dedup-host")
	creditHost := createWork(t, "credit-host")
	bystander := createWork(t, "bystander")
	createWorkCharacter(t, rosterHost.ID, source.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, dedupHost.ID, target.ID, model.WorkCharacterKindUnknown, model.SpoilerNone)
	createWorkCharacter(t, dedupHost.ID, source.ID, model.WorkCharacterKindSecondary, model.SpoilerSevere)
	person := createPerson(t, "VA")
	name := createCreditName(t, &person.ID, "声優")
	createCredit(t, creditHost.ID, name.ID, seededRoleID(t), &source.ID)

	settleWorks(t)
	before := map[int64]time.Time{}
	for _, id := range []int64{rosterHost.ID, dedupHost.ID, creditHost.ID, bystander.ID} {
		before[id] = workUpdatedAt(t, id)
	}

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeCharacter, source.ID, target.ID, 7, "same character")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	for _, id := range []int64{rosterHost.ID, dedupHost.ID, creditHost.ID} {
		assert.True(t, workUpdatedAt(t, id).After(before[id]), "work %d renders the merged character now", id)
	}
	assert.True(t, workUpdatedAt(t, bystander.ID).Equal(before[bystander.ID]),
		"a work with neither character must stay out of the feed")
}

func TestMergeWorkTouchesTargetAndRelationOtherEnd(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	dst := createWork(t, "target")
	src := createWork(t, "source")
	aSide := createWork(t, "a-side-neighbour")
	bSide := createWork(t, "b-side-neighbour")
	dupSide := createWork(t, "dup-neighbour")
	bystander := createWork(t, "bystander")
	createWorkRelation(t, src.ID, dst.ID)
	createWorkRelation(t, src.ID, aSide.ID)
	createWorkRelation(t, bSide.ID, src.ID)
	createWorkRelation(t, src.ID, dupSide.ID)
	createWorkRelation(t, dst.ID, dupSide.ID)
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: src.ID, Lang: "ja", Title: "移る題名", Kind: model.WorkTitleKindOfficial}).Error)

	settleWorks(t)
	before := map[int64]time.Time{}
	for _, id := range []int64{dst.ID, aSide.ID, bSide.ID, dupSide.ID, bystander.ID} {
		before[id] = workUpdatedAt(t, id)
	}
	cursor := drainChanges(t)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "same work")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	assert.True(t, workUpdatedAt(t, dst.ID).After(before[dst.ID]),
		"the merge target always gains the source's facets")
	for _, id := range []int64{aSide.ID, bSide.ID, dupSide.ID} {
		assert.True(t, workUpdatedAt(t, id).After(before[id]),
			"work %d is at the other end of a rewritten relation", id)
	}
	assert.True(t, workUpdatedAt(t, bystander.ID).Equal(before[bystander.ID]),
		"a work with no edge to either side must stay out of the feed")

	ids := changesSince(t, cursor)
	assert.ElementsMatch(t, []int64{src.ID, dst.ID, aSide.ID, bSide.ID, dupSide.ID}, ids,
		"the feed carries the retired source, the target and the rewritten neighbours, nothing else")
}

func TestMergeIdentityLayerTouchesNoWork(t *testing.T) {
	t.Run("person", func(t *testing.T) {
		cleanTables(t)
		ctx := t.Context()

		target := createPerson(t, "target")
		source := createPerson(t, "source")
		name := createCreditName(t, &source.ID, "旧名義")
		w := createWork(t, "credited")
		createCredit(t, w.ID, name.ID, seededRoleID(t), nil)

		settleWorks(t)
		before := workUpdatedAt(t, w.ID)

		p, err := testMerge.ProposeMerge(ctx, model.EntityTypePerson, source.ID, target.ID, 7, "same person")
		require.NoError(t, err)
		approveAndForceExecutable(t, p.ID)
		require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

		var moved model.CatalogCreditName
		require.NoError(t, testDB.First(&moved, name.ID).Error)
		require.NotNil(t, moved.PersonID)
		require.Equal(t, target.ID, *moved.PersonID, "the merge must actually have rehung the name")
		assert.True(t, workUpdatedAt(t, w.ID).Equal(before),
			"person_id lives on the name face — the work renders the same bytes")
	})
}

func TestMergeWorkClaimTransferTouchesTarget(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	dst := createWork(t, "unclaimed target")
	src := createWork(t, "claimed source")
	claimWork(t, src.ID, "own-ids", 4242)

	settleWorks(t)
	before := workUpdatedAt(t, dst.ID)
	cursor := drainChanges(t)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "claim moves")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogWork
	require.NoError(t, testDB.First(&merged, dst.ID).Error)
	require.NotNil(t, merged.Site)
	assert.Equal(t, "own-ids", *merged.Site)
	require.NotNil(t, merged.ProductWorkID)
	assert.Equal(t, int64(4242), *merged.ProductWorkID, "a site that numbers its own pages keeps its id with the claim")
	assert.True(t, workUpdatedAt(t, dst.ID).After(before), "the target now carries the claim")

	ids := changesSince(t, cursor)
	assert.ElementsMatch(t, []int64{src.ID, dst.ID}, ids,
		"the claim's new owner and the id it left enter the feed together")
}

func TestMergeWorkClaimOnACatalogIDSiteTakesTheSurvivorsID(t *testing.T) {
	for _, site := range []string{"kungal", "moyu", "letmoe"} {
		t.Run(site, func(t *testing.T) {
			cleanTables(t)
			ctx := t.Context()

			dst := createWork(t, "unclaimed target")
			src := createWork(t, "claimed source")
			claimWork(t, src.ID, site, src.ID)

			p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "claim moves")
			require.NoError(t, err)
			approveAndForceExecutable(t, p.ID)
			require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

			var merged model.CatalogWork
			require.NoError(t, testDB.First(&merged, dst.ID).Error)
			require.NotNil(t, merged.Site)
			assert.Equal(t, site, *merged.Site)
			require.NotNil(t, merged.ProductWorkID)
			assert.Equal(t, dst.ID, *merged.ProductWorkID,
				"the site's page for the survivor is the survivor's id; the loser's id is gone")
		})
	}
}
