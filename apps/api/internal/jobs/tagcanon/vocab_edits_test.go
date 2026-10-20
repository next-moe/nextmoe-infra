package tagcanon

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureTagFindsARenamedTagThroughItsSourceMap(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	bgm, vndb := srcID(t, sourceKeyBangumi), srcID(t, sourceKeyVNDB)

	renamed := mkTag(t, "鬱", false)
	mkMap(t, bgm, "致郁", renamed.ID)

	st := &Stats{}
	w := &writer{db: testDB, stats: st}
	w.writeGroup(ctx, group{CanonicalName: "致郁", Members: []vocabEntry{
		{SourceID: bgm, Name: "致郁"}, {SourceID: vndb, Name: "鬱ゲー"},
	}}, true)
	require.Zero(t, st.Errors)

	var tags int64
	require.NoError(t, testDB.Model(&model.CatalogTag{}).Count(&tags).Error)
	assert.EqualValues(t, 1, tags, "the canonical name a person renamed away must not come back as a second tag")
	var mapped int64
	require.NoError(t, testDB.Raw(`SELECT tag_id FROM catalog_tag_source_map WHERE source_id = ? AND source_name = ?`,
		vndb, "鬱ゲー").Scan(&mapped).Error)
	assert.Equal(t, renamed.ID, mapped, "a new spelling joins the tag its siblings already map to")
}

func TestStampTierKindLeavesHumanColumnsAlone(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	tag := mkTag(t, "泣きゲー", false)
	require.NoError(t, testDB.Exec(`UPDATE catalog_tag SET field_provenance = ?::jsonb WHERE id = ?`,
		`{"tier":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}`, tag.ID).Error)

	assert.Equal(t, 1, stampTierKind(ctx, testDB, tag.ID, model.TagTierLongtail, model.TagKindMeta))
	var row model.CatalogTag
	require.NoError(t, testDB.First(&row, tag.ID).Error)
	assert.Equal(t, model.TagTierCore, row.Tier, "a tier a person set survives the reviewed file")
	assert.Equal(t, model.TagKindMeta, row.Kind)

	assert.Zero(t, stampTierKind(ctx, testDB, tag.ID, model.TagTierLongtail, model.TagKindMeta),
		"a run that can change nothing writes nothing")
}

func TestMergeCarriesTagParentSuppressionsAndClosesProposals(t *testing.T) {
	cleanTagcanon(t)
	truncate := func() {
		require.NoError(t, testDB.Exec("TRUNCATE edit_suppressed_row, edit_proposal, catalog_tag_parent RESTART IDENTITY CASCADE").Error)
	}
	truncate()
	t.Cleanup(truncate)
	ctx := context.Background()
	bgm, vndb := srcID(t, sourceKeyBangumi), srcID(t, sourceKeyVNDB)

	winner, loser := mkTag(t, "致郁", false), mkTag(t, "致郁（鬱ゲー）", false)
	parent, child := mkTag(t, "剧情", false), mkTag(t, "胃痛", false)
	mkMap(t, bgm, "致郁", winner.ID)
	mkMap(t, vndb, "致郁（鬱ゲー）", loser.ID)
	require.NoError(t, testDB.Create(&model.CatalogTagParent{TagID: loser.ID, ParentID: parent.ID, Origin: model.TagParentOriginVNDB}).Error)

	suppress := func(tagID, parentID int64) {
		require.NoError(t, testDB.Create(&editing.SuppressedRow{
			EntityType: editspec.TypeTag, EntityID: tagID, FieldKey: editspec.FieldTagParents,
			IdentityKey: editspec.ParentIdentity(parentID),
		}).Error)
	}
	suppress(winner.ID, parent.ID)
	suppress(child.ID, loser.ID)
	require.NoError(t, testDB.Create(&editing.Proposal{
		EntityFamily: "catalog", EntityType: editspec.TypeTag, EntityID: loser.ID,
		Patch: []byte(`{"catalog.tag.note":"x"}`), ProposerUID: 9, Site: "editor", Status: editing.StatusOpen,
	}).Error)

	path := writeMergeFile(t, mergeRec{FromID: loser.ID, From: "致郁（鬱ゲー）", IntoID: winner.ID, Into: "致郁"})
	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Zero(t, st.Errors)

	var edges int64
	require.NoError(t, testDB.Model(&model.CatalogTagParent{}).Where("tag_id = ?", winner.ID).Count(&edges).Error)
	assert.Zero(t, edges, "the loser's VNDB edge must not land on a winner that hides it")

	var keys []string
	require.NoError(t, testDB.Model(&editing.SuppressedRow{}).Where("entity_id = ?", child.ID).
		Pluck("identity_key", &keys).Error)
	assert.Equal(t, []string{editspec.ParentIdentity(winner.ID)}, keys, "a suppression naming the loser now names the winner")

	var status int16
	require.NoError(t, testDB.Raw(`SELECT status FROM edit_proposal WHERE entity_id = ?`, loser.ID).Scan(&status).Error)
	assert.Equal(t, editing.StatusDeclined, status, "an open proposal on the merged-away tag is closed")
}
