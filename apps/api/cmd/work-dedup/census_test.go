package main

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	srcb "api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkDedupCensusPairFacts(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	kungal := "kungal"

	twins := func(title, nameA, nameB string, siteA *string) (int64, int64) {
		a := mkWork(t, medium, nameA, siteA)
		b := mkWork(t, medium, nameB, nil)
		mkTitle(t, a, title)
		mkTitle(t, b, title)
		return a, b
	}

	kg, eg := twins("事実検証共有タイトル一", "事実エイ作品テスト一", "事実ビー作品テスト一", &kungal)
	mkAnchor(t, kg, 2, "v100")
	mkAnchor(t, kg, 3, "100")
	require.NoError(t, testDB.Create(&srcb.Subject{
		ID: 100, Type: 4, Name: "subject-100", Date: "2011-03-04",
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}).Error)
	mkAnchor(t, eg, 5, "500")
	mkRelease(t, eg, 2012, 5, 6)

	vn, bg := twins("事実検証共有タイトル二", "事実エイ作品テスト二", "事実ビー作品テスト二", nil)
	mkAnchor(t, vn, 2, "v200")
	mkAnchor(t, vn, 3, "202")
	mkAnchor(t, bg, 3, "203")

	vv1, vv2 := twins("事実検証共有タイトル六", "事実エイ作品テスト六", "事実ビー作品テスト六", nil)
	mkAnchor(t, vv1, 2, "v300")
	mkAnchor(t, vv2, 2, "v301")

	dlA, dlB := twins("事実検証共有タイトル三", "事実エイ作品テスト三", "事実ビー作品テスト三", nil)
	mkDLsiteRelease(t, dlA, "RJ01000001")
	mkDLsiteRelease(t, dlB, "RJ01000002")

	ciA, ciB := twins("事実検証共有タイトル四", "事実エイ作品テスト四", "事実ビー作品テスト四", nil)
	mkDLsiteRelease(t, ciA, "RJ02000001")
	mkDLsiteRelease(t, ciB, "rj02000001")

	gone, live := twins("事実検証共有タイトル五", "事実エイ作品テスト五", "事実ビー作品テスト五", nil)
	mkDLsiteRelease(t, gone, "RJ03000001")
	require.NoError(t, testDB.Where("work_id = ?", gone).Delete(&model.CatalogRelease{}).Error)
	mkDLsiteRelease(t, live, "RJ03000002")

	c, err := buildCensus(context.Background(), testDB)
	require.NoError(t, err)
	require.Len(t, c.rows, 6, "%+v", c.rows)

	row, _ := findPair(t, c, kg, eg)
	assert.Equal(t, [2]string{"kungal", "eg"}, [2]string{row.LaneA, row.LaneB})
	assert.Equal(t, [2]int{2, 1}, [2]int{row.AnchorsA, row.AnchorsB})
	assert.False(t, row.AnchorConflict, "no source in common")
	require.NotNil(t, row.DateA)
	require.NotNil(t, row.DateB)
	assert.Equal(t, "2011-03-04", row.DateA.Format(time.DateOnly), "bangumi date stands in for a work without releases")
	assert.Equal(t, "2012-05-06", row.DateB.Format(time.DateOnly))

	row, _ = findPair(t, c, vn, bg)
	assert.Equal(t, [2]string{"vndb", "bgm"}, [2]string{row.LaneA, row.LaneB})
	assert.Equal(t, [2]int{2, 1}, [2]int{row.AnchorsA, row.AnchorsB})
	assert.False(t, row.VetoConflict, "bangumi 202 vs 203: Bangumi keeps a subject per edition")
	assert.True(t, row.AnchorConflict)
	assert.False(t, row.RelConflict)

	row, _ = findPair(t, c, vv1, vv2)
	assert.True(t, row.VetoConflict, "vndb v300 vs v301")

	row, _ = findPair(t, c, dlA, dlB)
	assert.Equal(t, [2]string{"dlsite", "dlsite"}, [2]string{row.LaneA, row.LaneB})
	assert.True(t, row.RelConflict)
	assert.False(t, row.AnchorConflict)
	assert.Empty(t, row.SharedKey)

	row, _ = findPair(t, c, ciA, ciB)
	assert.True(t, row.RelConflict, "the ids differ byte-wise and none is equal")
	assert.Equal(t, "dlsite:rj02000001", row.SharedKey, "RJ02000001 and rj02000001 fold together")

	row, _ = findPair(t, c, gone, live)
	assert.Equal(t, [2]string{"other", "dlsite"}, [2]string{row.LaneA, row.LaneB}, "a deleted release carries no lane")
	assert.False(t, row.RelConflict, "a deleted release carries no conflict")
	assert.Equal(t, [2]int{0, 0}, [2]int{row.AnchorsA, row.AnchorsB})
}

func TestWorkDedupCensusFlagsAHiddenClaim(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	kungal := "kungal"

	hidden := mkWork(t, medium, "隠し主張テスト作品", &kungal)
	plain := mkWork(t, medium, "隠し主張テスト作品", nil)
	mkAnchor(t, plain, 3, "321")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET claim_state = ? WHERE id = ?`, model.ClaimStateHidden, hidden).Error)

	c, err := buildCensus(context.Background(), testDB)
	require.NoError(t, err)
	row, verdict := findPair(t, c, hidden, plain)
	assert.Equal(t, [2]bool{true, false}, [2]bool{row.HiddenA, row.HiddenB})
	assert.Equal(t, bucketHiddenClaim, verdict)
	assert.Empty(t, c.groups, "a hidden claim never lands in a merge group")

	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET claim_state = ? WHERE id = ?`, model.ClaimStateDraft, hidden).Error)
	c, err = buildCensus(context.Background(), testDB)
	require.NoError(t, err)
	row, _ = findPair(t, c, hidden, plain)
	assert.False(t, row.HiddenA, "a draft claim is not hidden")
}

// 54607 and 224163 are one game, 黄莓C: forum's unpublished draft holds the
// VNDB entry and a release carrying Steam 3919400, while the Bangumi mint
// holds the same appid as a probable work ref and the published forum page.
// The census saw neither the three-rune title nor, before #509, the id.
func TestWorkDedupCensusMergesAShortTitledTwinIntoThePublishedClaim(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	kungal := "kungal"

	draft := mkWork(t, medium, "黄莓C", &kungal)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET claim_state = ? WHERE id = ?`, model.ClaimStateDraft, draft).Error)
	mkTitle(t, draft, "黄莓C")
	mkAnchor(t, draft, model.SourceVNDB, "v58618")
	mkReleaseRef(t, draft, model.SourceSteam, "3919400")

	published := mkWork(t, medium, "黄莓Ｃ", &kungal)
	mkTitle(t, published, "黄莓C")
	mkAnchor(t, published, model.SourceBangumi, "589607")
	mkRef(t, published, model.SourceSteam, "3919400", model.LinkKindProbable, nil)

	c, err := buildCensus(context.Background(), testDB)
	require.NoError(t, err)
	row, verdict := findPair(t, c, draft, published)
	assert.Equal(t, "steam:3919400", row.SharedKey)
	assert.Equal(t, 0, row.SharedNorms, "the title is below the long rule")
	assert.Equal(t, 1, row.SharedOfficial)
	assert.Equal(t, [2]int{1, 3}, [2]int{row.ClaimA, row.ClaimB})
	assert.Equal(t, bucketAuto, verdict)
	require.Len(t, c.groups, 1)
	assert.Equal(t, published, c.groups[0].survivor, "the published page survives its draft twin")
}
