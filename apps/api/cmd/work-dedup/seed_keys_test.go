package main

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	srcb "api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runSeedKeysBuf(t *testing.T, limit int, run bool) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, runSeedKeys(context.Background(), testDB, &buf, 1, limit, run))
	return buf.String()
}

func parseSeedKeysSummary(t *testing.T, out string) map[string]int {
	t.Helper()
	i := strings.Index(out, "[seed-keys]")
	require.GreaterOrEqual(t, i, 0, out)
	got := map[string]int{}
	for _, f := range strings.Fields(out[i:])[1:] {
		k, v, ok := strings.Cut(f, "=")
		require.True(t, ok, f)
		n, err := strconv.Atoi(v)
		require.NoError(t, err, f)
		got[k] = n
	}
	return got
}

func mkBgmSubject(t *testing.T, id int64, infobox string) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcb.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id),
		InfoboxRaw: infobox, ParseError: "", Summary: "", Date: "",
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}).Error)
}

func mkDLsiteRelease(t *testing.T, workID int64, workno string) {
	t.Helper()
	var dlsite int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsite).Error)
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	require.NoError(t, testDB.Create(rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: dlsite,
		ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "test:seed-keys",
	}).Error)
}

func TestSeedKeysFilesStrippedPair(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	a := mkWork(t, medium, "【スマホ版】シードキー作品", nil)
	b := mkWork(t, medium, "シードキー作品", nil)

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Contains(t, out, "APPLIED")
	assert.Equal(t, 1, sum["stripped_pairs"])
	assert.Equal(t, 0, sum["declared_pairs"])
	assert.Equal(t, 0, sum["existing_skips"])
	assert.Equal(t, 0, sum["conflict_skips"])
	assert.Equal(t, 0, sum["limited"])
	assert.Equal(t, 1, sum["written"])
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, a, b))
	got := candidateOf(t, a, b)
	assert.Equal(t, model.CandidateReasonNameFuzzy, got.Reason)
}

func TestSeedKeysFilesDeclaredPair(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	var bgm int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&bgm).Error)
	a := mkWork(t, medium, "seed declared alpha", nil)
	b := mkWork(t, medium, "seed declared beta", nil)
	mkBgmSubject(t, 9101, `DLsite RJ010101`)
	mkRef(t, a, bgm, "9101", model.LinkKindExact, nil)
	mkDLsiteRelease(t, b, "RJ010101")

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 0, sum["stripped_pairs"])
	assert.Equal(t, 1, sum["declared_pairs"])
	assert.Equal(t, 1, sum["written"])
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, a, b))
	got := candidateOf(t, a, b)
	assert.Equal(t, model.CandidateReasonSharedExternalID, got.Reason)
}

func TestSeedKeysSkipsExistingCandidateInAnyStatus(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	a := mkWork(t, medium, "【スマホ版】既存スキップ作品", nil)
	b := mkWork(t, medium, "既存スキップ作品", nil)
	require.NoError(t, testDB.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusRejected,
	}).Error)

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 1, sum["stripped_pairs"])
	assert.Equal(t, 1, sum["existing_skips"])
	assert.Equal(t, 0, sum["written"])
	got := candidateOf(t, a, b)
	assert.Equal(t, model.CandidateStatusRejected, got.Status)
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_match_candidate`))
}

func TestSeedKeysSkipsConflictingExact(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	a := mkWork(t, medium, "【スマホ版】衝突スキップ作品", nil)
	b := mkWork(t, medium, "衝突スキップ作品", nil)
	mkAnchor(t, a, 2, "v9001")
	mkAnchor(t, b, 2, "v9002")

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 1, sum["stripped_pairs"])
	assert.Equal(t, 1, sum["conflict_skips"])
	assert.Equal(t, 0, sum["written"])
	assert.Equal(t, int64(0), countRows(t, `SELECT count(*) FROM catalog_match_candidate`))
}

func TestSeedKeysSeedsAcrossAnEditionSplittingConflict(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	a := mkWork(t, medium, "【スマホ版】版違い作品", nil)
	b := mkWork(t, medium, "版違い作品", nil)
	mkAnchor(t, a, model.SourceErogameScape, "9001")
	mkAnchor(t, b, model.SourceErogameScape, "9002")

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 0, sum["conflict_skips"])
	assert.Equal(t, 1, sum["written"])
}

func TestSeedKeysSeedsGalgameOnly(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	var anime int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'anime'`).Scan(&anime).Error)
	mkWork(t, anime, "【スマホ版】アニメ同名作品", nil)
	mkWork(t, anime, "アニメ同名作品", nil)

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 0, sum["stripped_pairs"])
	assert.Equal(t, 0, sum["written"])
}

func TestSeedKeysSkipsOtherMediumAndQuarantined(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	var anime int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'anime'`).Scan(&anime).Error)

	ga := mkWork(t, medium, "クロスメディア作品", nil)
	ab := mkWork(t, anime, "【スマホ版】クロスメディア作品", nil)

	qa := mkWork(t, medium, "隔離スキップ作品", nil)
	qb := mkWork(t, medium, "【スマホ版】隔離スキップ作品", nil)
	require.NoError(t, testDB.Model(&model.CatalogWork{}).Where("id = ?", qb).
		Update("status", model.WorkStatusQuarantine).Error)

	va := mkWork(t, medium, "許可シード作品", nil)
	vb := mkWork(t, medium, "【スマホ版】許可シード作品", nil)

	out := runSeedKeysBuf(t, 10, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 1, sum["stripped_pairs"])
	assert.Equal(t, 1, sum["written"])
	assert.Equal(t, int64(0), countRows(t,
		`SELECT count(*) FROM catalog_match_candidate WHERE a_id = ? AND b_id = ?`, min(ga, ab), max(ga, ab)))
	assert.Equal(t, int64(0), countRows(t,
		`SELECT count(*) FROM catalog_match_candidate WHERE a_id = ? AND b_id = ?`, min(qa, qb), max(qa, qb)))
	assert.Equal(t, model.CandidateStatusPending, candidateStatus(t, va, vb))
}

func TestSeedKeysLimit(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	mkWork(t, medium, "リミット作品エー", nil)
	mkWork(t, medium, "【スマホ版】リミット作品エー", nil)
	mkWork(t, medium, "リミット作品ビー", nil)
	mkWork(t, medium, "【スマホ版】リミット作品ビー", nil)

	out := runSeedKeysBuf(t, 1, true)
	sum := parseSeedKeysSummary(t, out)
	assert.Equal(t, 2, sum["stripped_pairs"])
	assert.Equal(t, 1, sum["limited"])
	assert.Equal(t, 1, sum["written"])
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_match_candidate`))
}

func TestSeedKeysDryWritesNothing(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	mkWork(t, medium, "ドライラン作品", nil)
	mkWork(t, medium, "【スマホ版】ドライラン作品", nil)

	out := runSeedKeysBuf(t, 10, false)
	sum := parseSeedKeysSummary(t, out)
	assert.Contains(t, out, "DRY-RUN")
	assert.Equal(t, 1, sum["stripped_pairs"])
	assert.Equal(t, 0, sum["written"])
	assert.Equal(t, int64(0), countRows(t, `SELECT count(*) FROM catalog_match_candidate`))
}

func TestSeedKeysSecondRunWritesNothing(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	mkWork(t, medium, "二回目作品", nil)
	mkWork(t, medium, "【スマホ版】二回目作品", nil)

	first := parseSeedKeysSummary(t, runSeedKeysBuf(t, 10, true))
	assert.Equal(t, 1, first["written"])
	second := parseSeedKeysSummary(t, runSeedKeysBuf(t, 10, true))
	assert.Equal(t, 1, second["existing_skips"])
	assert.Equal(t, 0, second["written"])
	assert.Equal(t, int64(1), countRows(t, `SELECT count(*) FROM catalog_match_candidate`))
}
