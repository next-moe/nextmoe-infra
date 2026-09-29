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
	require.Len(t, c.rows, 5, "%+v", c.rows)

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
	assert.True(t, row.AnchorConflict, "bangumi 202 vs 203")
	assert.False(t, row.RelConflict)

	row, _ = findPair(t, c, dlA, dlB)
	assert.Equal(t, [2]string{"dlsite", "dlsite"}, [2]string{row.LaneA, row.LaneB})
	assert.True(t, row.RelConflict)
	assert.False(t, row.AnchorConflict)
	assert.False(t, row.RefOverlap)
	assert.False(t, row.RefOverlapCI)

	row, _ = findPair(t, c, ciA, ciB)
	assert.True(t, row.RelConflict, "the ids differ byte-wise and none is equal")
	assert.False(t, row.RefOverlap)
	assert.True(t, row.RefOverlapCI, "RJ02000001 and rj02000001 fold together")

	row, _ = findPair(t, c, gone, live)
	assert.Equal(t, [2]string{"other", "dlsite"}, [2]string{row.LaneA, row.LaneB}, "a deleted release carries no lane")
	assert.False(t, row.RelConflict, "a deleted release carries no conflict")
	assert.Equal(t, [2]int{0, 0}, [2]int{row.AnchorsA, row.AnchorsB})
}
