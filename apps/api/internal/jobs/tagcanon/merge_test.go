package tagcanon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMergeFile(t *testing.T, recs ...mergeRec) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "merges.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range recs {
		require.NoError(t, enc.Encode(r))
	}
	return path
}

func mkTag(t *testing.T, name string, sexual bool) model.CatalogTag {
	t.Helper()
	tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: sexual}
	require.NoError(t, testDB.Create(&tag).Error)
	return tag
}

func mkMap(t *testing.T, source int16, name string, tagID int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{SourceID: source, SourceName: name, TagID: tagID}).Error)
}

func TestMergeCanonicalTags(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	bgm := srcID(t, sourceKeyBangumi)
	vndb := srcID(t, sourceKeyVNDB)
	curated := srcID(t, sourceKeyCurated)

	winner := mkTag(t, "致郁", false)
	loser := mkTag(t, "致郁（鬱ゲー）", false)
	otome := mkTag(t, "乙女类游戏", false)
	otomeAlt := mkTag(t, "乙女ゲー", false)

	mkMap(t, bgm, "致郁", winner.ID)
	mkMap(t, vndb, "致郁（鬱ゲー）", loser.ID)
	mkMap(t, curated, "致郁（鬱ゲー）", loser.ID)
	mkMap(t, bgm, "乙女游戏", otome.ID)
	mkMap(t, bgm, "乙女ゲー", otomeAlt.ID)

	require.NoError(t, testDB.Create(&[]model.CatalogTagIntro{
		{TagID: winner.ID, Lang: "zh", Intro: "winner zh", SourceID: curated},
		{TagID: loser.ID, Lang: "zh", Intro: "loser zh", SourceID: curated},
		{TagID: loser.ID, Lang: "ja", Intro: "loser ja", SourceID: curated},
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogTagWorkCount{TagID: loser.ID, NAll: 7, ComputedAt: time.Now()}).Error)

	// a curated edge written under the losing canonical's own name: nothing but
	// the alias row keeps it pointing anywhere after the tag is deleted
	var medium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key='galgame'`).Scan(&medium).Error)
	wA, wB := mkBodylessWork(t, medium), mkBodylessWork(t, medium)
	mkWorkTag(t, wA, "致郁（鬱ゲー）", 1, curated)
	mkWorkTag(t, wB, "乙女ゲー", 1, curated)

	path := writeMergeFile(t,
		mergeRec{FromID: loser.ID, From: "致郁（鬱ゲー）", IntoID: winner.ID, Into: "致郁", Reason: "synonym split"},
		mergeRec{FromID: otomeAlt.ID, From: "乙女ゲー", IntoID: otome.ID, Into: "乙女类游戏"},
	)

	dry, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path})
	require.NoError(t, err)
	assert.Equal(t, 2, dry.Planned)
	assert.Equal(t, 0, dry.TagsDeleted)
	assert.Equal(t, 0, dry.Errors)

	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 0, st.Errors)
	assert.Equal(t, 2, st.TagsDeleted)
	assert.Equal(t, 3, st.MapsRepointed)
	assert.Equal(t, 1, st.CuratedAliases, "only the loser without a curated map row needs one minted")
	assert.Equal(t, 1, st.IntrosMoved)
	assert.Equal(t, 1, st.IntrosDropped, "the lang+source the winner already answers for")
	assert.Equal(t, 1, st.CountsDropped)

	var gone int64
	testDB.Model(&model.CatalogTag{}).Where("id IN ?", []int64{loser.ID, otomeAlt.ID}).Count(&gone)
	assert.Zero(t, gone)

	var resolved []struct {
		WorkID int64  `gorm:"column:work_id"`
		Name   string `gorm:"column:name"`
	}
	require.NoError(t, testDB.Raw(`
		SELECT wt.work_id, t.name FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		JOIN catalog_tag t ON t.id = m.tag_id
		WHERE wt.work_id IN (?, ?) ORDER BY wt.work_id`, wA, wB).Scan(&resolved).Error)
	require.Len(t, resolved, 2, "every curated edge must still reach a canonical")
	assert.Equal(t, "致郁", resolved[0].Name)
	assert.Equal(t, "乙女类游戏", resolved[1].Name)

	var intros int64
	testDB.Model(&model.CatalogTagIntro{}).Where("tag_id = ?", winner.ID).Count(&intros)
	assert.EqualValues(t, 2, intros)

	again, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, again.AlreadyMerged)
	assert.Equal(t, 0, again.Planned)
	assert.Equal(t, 0, again.Errors)
}

func TestMergeRefusesStaleAndSelfRecords(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	keep := mkTag(t, "丸吞", true)
	other := mkTag(t, "丸呑", true)

	path := writeMergeFile(t,
		mergeRec{FromID: other.ID, From: "丸新", IntoID: keep.ID, Into: "丸吞"},
		mergeRec{FromID: other.ID, From: "丸呑", IntoID: keep.ID, Into: "丸飲"},
		mergeRec{FromID: keep.ID, From: "丸吞", IntoID: keep.ID, Into: "丸吞"},
		mergeRec{FromID: other.ID, From: "丸呑", IntoID: 99999999, Into: "不存在"},
	)
	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 4, st.Errors)
	assert.Equal(t, 0, st.Planned)
	assert.Equal(t, 0, st.TagsDeleted)

	var alive int64
	testDB.Model(&model.CatalogTag{}).Where("id IN ?", []int64{keep.ID, other.ID}).Count(&alive)
	assert.EqualValues(t, 2, alive, "a record that does not match the database must not touch it")
}

func TestMergeMovesHierarchyEdgesToTheWinner(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	winner := mkTag(t, "致郁", false)
	loser := mkTag(t, "致郁（鬱ゲー）", false)
	parent := mkTag(t, "戏剧", false)
	child := mkTag(t, "致郁结局", false)
	require.NoError(t, testDB.Create(&[]model.CatalogTagParent{
		{TagID: loser.ID, ParentID: parent.ID, Origin: model.TagParentOriginVNDB},
		{TagID: winner.ID, ParentID: parent.ID, Origin: model.TagParentOriginVNDB},
		{TagID: loser.ID, ParentID: winner.ID, Origin: model.TagParentOriginVNDB},
		{TagID: child.ID, ParentID: loser.ID, Origin: model.TagParentOriginCurated, Note: "reviewed"},
	}).Error)

	path := writeMergeFile(t, mergeRec{FromID: loser.ID, From: "致郁（鬱ゲー）", IntoID: winner.ID, Into: "致郁"})
	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 0, st.Errors)
	assert.Equal(t, 1, st.TagsDeleted)
	assert.Equal(t, 1, st.ParentsMoved)

	var edges []model.CatalogTagParent
	require.NoError(t, testDB.Order("tag_id, parent_id").Find(&edges).Error)
	require.Len(t, edges, 2)
	assert.Equal(t, [2]int64{winner.ID, parent.ID}, [2]int64{edges[0].TagID, edges[0].ParentID})
	assert.Equal(t, [2]int64{child.ID, winner.ID}, [2]int64{edges[1].TagID, edges[1].ParentID})
	assert.Equal(t, model.TagParentOriginCurated, edges[1].Origin)
	assert.Equal(t, "reviewed", edges[1].Note)
}

func TestMergeMovesTagBlocksToTheWinner(t *testing.T) {
	cleanTagcanon(t)
	ctx := context.Background()
	winner := mkTag(t, "致郁", false)
	loser := mkTag(t, "致郁（鬱ゲー）", false)
	block := func(uid, tag int64, mode int16) model.CatalogUserTagBlock {
		return model.CatalogUserTagBlock{ActorUID: uid, TagID: tag, Mode: mode, ClientID: "kungal"}
	}
	require.NoError(t, testDB.Create(&[]model.CatalogUserTagBlock{
		block(1, loser.ID, model.TagBlockModeTree),
		block(2, loser.ID, model.TagBlockModeTree), block(2, winner.ID, model.TagBlockModeExact),
		block(3, loser.ID, model.TagBlockModeExact), block(3, winner.ID, model.TagBlockModeTree),
		block(4, winner.ID, model.TagBlockModeExact),
	}).Error)

	path := writeMergeFile(t, mergeRec{FromID: loser.ID, From: "致郁（鬱ゲー）", IntoID: winner.ID, Into: "致郁"})
	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 0, st.Errors)
	assert.Equal(t, 1, st.BlocksMoved)

	var rows []model.CatalogUserTagBlock
	require.NoError(t, testDB.Order("actor_uid").Find(&rows).Error)
	got := map[int64][2]int64{}
	for _, r := range rows {
		got[r.ActorUID] = [2]int64{r.TagID, int64(r.Mode)}
	}
	tree, exact := int64(model.TagBlockModeTree), int64(model.TagBlockModeExact)
	assert.Equal(t, map[int64][2]int64{
		1: {winner.ID, tree},
		2: {winner.ID, tree},
		3: {winner.ID, tree},
		4: {winner.ID, exact},
	}, got, "the wider of the two modes survives")
}
