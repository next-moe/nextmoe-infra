package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workStatus(t *testing.T, id int64) int16 {
	t.Helper()
	var s int16
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_work WHERE id = ?`, id).Scan(&s).Error)
	return s
}

func releaseRef(t *testing.T, workID int64, sourceID int16, ext string) {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	require.NoError(t, testDB.Create(rel).Error)
	addExternalRef(t, model.EntityTypeRelease, rel.ID, sourceID, ext, model.LinkKindExact)
}

func TestScreenMintedWorksQuarantinesAShortTitledTwin(t *testing.T) {
	cleanTables(t)
	existing := createWork(t, "黄莓C")
	minted := createWork(t, "黄莓Ｃ")

	hits, err := ScreenMintedWorks(testDB, []MintedWork{{ID: minted.ID}})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.True(t, hits[0].ByName)
	assert.Equal(t, model.WorkStatusQuarantine, workStatus(t, minted.ID))
	assert.Equal(t, model.WorkStatusLive, workStatus(t, existing.ID), "the screen never touches the other side")

	var cand model.CatalogMatchCandidate
	require.NoError(t, testDB.First(&cand).Error)
	assert.Equal(t, [2]int64{min(existing.ID, minted.ID), max(existing.ID, minted.ID)}, [2]int64{cand.AID, cand.BID})
	assert.Equal(t, model.CandidateReasonNameNormEqual, cand.Reason)
	assert.Equal(t, model.CandidateStatusPending, cand.Status)
}

func TestScreenMintedWorksMatchesAKeyAcrossLevelsAndCase(t *testing.T) {
	cleanTables(t)
	existing := createWork(t, "本編タイトル検証")
	releaseRef(t, existing.ID, model.SourceDMM, "HOBC_0507")
	minted := createWork(t, "別名で届いた作品")
	addExternalRef(t, model.EntityTypeWork, minted.ID, model.SourceDMM, "hobc_0507", model.LinkKindExact)

	hits, err := ScreenMintedWorks(testDB, []MintedWork{{ID: minted.ID}})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.False(t, hits[0].ByName)
	assert.Equal(t, model.WorkStatusQuarantine, workStatus(t, minted.ID))
}

func TestScreenMintedWorksReadsKeysTheLaneHasNotWritten(t *testing.T) {
	cleanTables(t)
	existing := createWork(t, "既存作品タイトル")
	releaseRef(t, existing.ID, model.SourceSteam, "3919400")
	minted := createWork(t, "まったく違う名前")

	hits, err := ScreenMintedWorks(testDB, []MintedWork{{ID: minted.ID, Keys: []IdentityKey{{model.SourceSteam, "3919400"}}}})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, existing.ID, hits[0].Other)
}

func TestScreenMintedWorksLeavesProvablyDistinctWorksLive(t *testing.T) {
	cleanTables(t)
	existing := createWork(t, "同名の別作品")
	addExternalRef(t, model.EntityTypeWork, existing.ID, model.SourceVNDB, "v1", model.LinkKindExact)
	minted := createWork(t, "同名の別作品")
	addExternalRef(t, model.EntityTypeWork, minted.ID, model.SourceVNDB, "v2", model.LinkKindExact)
	oneRune := createWork(t, "夏")
	createWork(t, "夏")

	hits, err := ScreenMintedWorks(testDB, []MintedWork{{ID: minted.ID}, {ID: oneRune.ID}})
	require.NoError(t, err)
	assert.Empty(t, hits, "two VNDB entries are two works, and one rune is no name to compare")
	assert.Equal(t, model.WorkStatusLive, workStatus(t, minted.ID))
}
