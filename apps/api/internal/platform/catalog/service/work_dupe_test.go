package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const animeMediumID int16 = 4

func TestSubmitWorkFilesSpacingDupeCandidate(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "既存作品の表示名")
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: existing.ID, Lang: "ja", Title: "重複検出タイトル", Kind: model.WorkTitleKindOfficial,
	}).Error)

	// The receipt lane only runs on a mint that got past the pre-mint gate, and
	// this collision is exactly what that gate refuses — so the receipt is now
	// only observable through the confirm door.
	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90501, ActorUID: 7,
		Fields: submitFields("重複 検出 タイトル"), ConfirmDuplicates: true,
	})
	require.NoError(t, err)

	var cands []model.CatalogMatchCandidate
	require.NoError(t, testDB.Order("a_id, b_id").Find(&cands).Error)
	require.Len(t, cands, 1, "the mint must file one receipt, not one per matching norm")
	got := cands[0]
	assert.Equal(t, model.EntityTypeWork, got.EntityType)
	assert.Equal(t, min(existing.ID, res.WorkID), got.AID)
	assert.Equal(t, max(existing.ID, res.WorkID), got.BID)
	assert.Equal(t, model.CandidateReasonNameNormEqual, got.Reason)
	assert.Equal(t, model.CandidateStatusPending, got.Status)
	assert.Nil(t, got.DecidedBy)
}

func TestSubmitWorkCrossMediumCollisionFilesNothing(t *testing.T) {
	s := newLifecycle(t)
	anime := createWorkX(t, animeMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "既存作品の表示名")
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: anime.ID, Lang: "ja", Title: "重複検出タイトル", Kind: model.WorkTitleKindOfficial,
	}).Error)

	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90505, ActorUID: 7,
		Fields: submitFields("重複 検出 タイトル"),
	})
	require.NoError(t, err)

	var filed int64
	require.NoError(t, testDB.Model(&model.CatalogMatchCandidate{}).Count(&filed).Error)
	assert.Zero(t, filed, "an anime sharing the title must not pair with a galgame mint")
}

func TestSubmitWorkWithoutCollisionFilesNothing(t *testing.T) {
	s := newLifecycle(t)
	createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全く別の既存作品")

	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90502, ActorUID: 7,
		Fields: submitFields("衝突しない新規作品"),
	})
	require.NoError(t, err)

	var filed int64
	require.NoError(t, testDB.Model(&model.CatalogMatchCandidate{}).Count(&filed).Error)
	assert.Zero(t, filed)
}

func TestWorkDupeNormEligible(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"红楼梦", true},
		{"紅樓夢", true},
		{"abc", false},
		{"红楼", false},
		{"ローズ", false},
		{"abcd", true},
		{"红楼梦x", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, WorkDupeNormEligible(tc.in), tc.in)
	}
}

func TestSubmitWorkFilesHanThreeRuneDupeCandidate(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "红楼梦")

	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90503, ActorUID: 7,
		Fields: submitFields("红楼梦"), ConfirmDuplicates: true,
	})
	require.NoError(t, err)

	var cands []model.CatalogMatchCandidate
	require.NoError(t, testDB.Order("a_id, b_id").Find(&cands).Error)
	require.Len(t, cands, 1)
	got := cands[0]
	assert.Equal(t, model.EntityTypeWork, got.EntityType)
	assert.Equal(t, min(existing.ID, res.WorkID), got.AID)
	assert.Equal(t, max(existing.ID, res.WorkID), got.BID)
	assert.Equal(t, model.CandidateReasonNameNormEqual, got.Reason)
	assert.Equal(t, model.CandidateStatusPending, got.Status)
}

func TestSubmitWorkSkipsShortASCIICollision(t *testing.T) {
	s := newLifecycle(t)
	createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "abc")

	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90504, ActorUID: 7,
		Fields: submitFields("abc"),
	})
	require.NoError(t, err)

	var filed int64
	require.NoError(t, testDB.Model(&model.CatalogMatchCandidate{}).Count(&filed).Error)
	assert.Zero(t, filed)
}

func TestSubmitWorkFilesTrailingWaveDashDupeCandidate(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive,
		"隣人妄想～団地族の昼下がり～")

	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90506, ActorUID: 7,
		Fields: submitFields("隣人妄想～団地族の昼下がり"), ConfirmDuplicates: true,
	})
	require.NoError(t, err)

	var cands []model.CatalogMatchCandidate
	require.NoError(t, testDB.Order("a_id, b_id").Find(&cands).Error)
	require.Len(t, cands, 1, "a subtitle that lost its closing wave dash is the same title")
	assert.Equal(t, min(existing.ID, res.WorkID), cands[0].AID)
	assert.Equal(t, max(existing.ID, res.WorkID), cands[0].BID)
	assert.Equal(t, model.CandidateReasonNameNormEqual, cands[0].Reason)
}

func TestSubmitWorkStillSkipsShortTitleBaredByTheWaveDashStrip(t *testing.T) {
	s := newLifecycle(t)
	createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "幽閉～")

	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90507, ActorUID: 7,
		Fields: submitFields("幽閉"),
	})
	require.NoError(t, err)

	var filed int64
	require.NoError(t, testDB.Model(&model.CatalogMatchCandidate{}).Count(&filed).Error)
	assert.Zero(t, filed, "stripping the dash must not push a 2-rune norm past the eligibility floor")
}
