package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkSeriesOf(t *testing.T, workIDs ...int64) int64 {
	t.Helper()
	s := model.CatalogSeries{DisplayName: "series", SourceID: sourceID(t, "vndb"), ExternalID: "s-test"}
	require.NoError(t, testDB.Create(&s).Error)
	for _, id := range workIDs {
		require.NoError(t, testDB.Create(&model.CatalogSeriesMember{SeriesID: s.ID, WorkID: id}).Error)
	}
	return s.ID
}

func TestExportBatchesCarriesClaimAndSeriesContext(t *testing.T) {
	clean(t)
	ctx := t.Context()

	claimed := mkClaimedWork(t, "認領", 42)
	mkTitle(t, claimed, "ja", "ソラの一", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	sib := mkWork(t, "兄弟")
	mkTitle(t, sib, "ja", "ソラの二", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	translated := mkWork(t, "已译")
	mkTitle(t, translated, "ja", "ソラのゼロ", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	mkTitle(t, translated, "zh-Hans", "空之零", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	stale := mkWork(t, "过期机译")
	mkTitle(t, stale, "ja", "ソラの三", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	_, err := writeMachineTitle(ctx, testDB, stale, "空之旧", "stale-hash", "m")
	require.NoError(t, err)
	fresh := mkWork(t, "在档机译")
	mkTitle(t, fresh, "ja", "ソラの四", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	_, err = writeMachineTitle(ctx, testDB, fresh, "空之四", hashSource("ソラの四"), "m")
	require.NoError(t, err)
	mkSeriesOf(t, claimed, sib, translated, stale, fresh)

	out := filepath.Join(t.TempDir(), "batches.jsonl")
	require.NoError(t, runExportBatches(ctx, testDB, autoOpts{Out: out}))

	f, err := os.Open(out)
	require.NoError(t, err)
	defer f.Close()
	var batches []exportBatch
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var b exportBatch
		require.NoError(t, json.Unmarshal(sc.Bytes(), &b))
		batches = append(batches, b)
	}
	require.NoError(t, sc.Err())

	require.Len(t, batches, 1, "the translated series member is context, not a member")
	require.Len(t, batches[0].Members, 4)
	byID := map[int64]exportMember{}
	for _, m := range batches[0].Members {
		byID[m.WorkID] = m
	}
	require.Contains(t, byID, claimed)
	require.Contains(t, byID, sib)
	assert.True(t, byID[claimed].Claimed)
	assert.False(t, byID[sib].Claimed)
	assert.Equal(t, "insert", byID[claimed].Decision)
	assert.Equal(t, "retranslate", byID[stale].Decision)
	assert.Equal(t, "unchanged", byID[fresh].Decision)
	assert.Equal(t, hashSource("ソラの一"), byID[claimed].SrcHash)
	assert.ElementsMatch(t, []string{"ソラのゼロ → 空之零", "ソラの四 → 空之四"}, batches[0].Known,
		"a translated non-candidate sibling and a hash-current machine title are context; a stale machine title is not")
}

func TestARemakeIsTitleSiblingOfItsOriginal(t *testing.T) {
	clean(t)
	ctx := t.Context()

	original := mkWork(t, "ONE～輝く季節へ～")
	mkTitle(t, original, "ja", "ONE～輝く季節へ～", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	mkTitle(t, original, "zh-Hans", "ONE～光辉的季节～", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	remake := mkWork(t, "ONE.")
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_work_relation (a_work_id, b_work_id, relation_type_id)
		SELECT ?, ?, id FROM catalog_relation_type WHERE key = 'remake_of'`, remake, original).Error)

	edges, err := loadSeriesEdges(ctx, testDB, []int64{original, remake})
	require.NoError(t, err)
	require.Equal(t, []siblingEdge{{A: original, B: remake}}, edges)

	known, err := loadKnownZhTitles(ctx, testDB, []int64{remake})
	require.NoError(t, err)
	require.Equal(t, []string{"ONE～輝く季節へ～ → ONE～光辉的季节～"}, known[remake])
}

func TestReadAcceptedCSVReassertsGates(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full.csv")
	body := strings.Join(autoCSVHeader, ",") + "\n" +
		"1,こころ," + hashSource("こころ") + ",insert,0,心,m,accept,心,心,\n" +
		"2,そら,x,insert,0,天空ソラ,m,accept,天空ソラ,,\n" +
		"3,そら,x,insert,0,这个提案比原名长了太多不可能是一个标题的翻译件,m,accept,,,\n"
	require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	rows, gateRefused, err := readAcceptedCSV(full)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "心", rows[0].ZhTitle)
	assert.Equal(t, 2, gateRefused)

	// Without a ja_title column there is nothing to gate against; the reviewed
	// rows install as-is, as they always have.
	minimal := filepath.Join(dir, "minimal.csv")
	require.NoError(t, os.WriteFile(minimal, []byte(
		"work_id,src_hash,proposed_zh,model\n1,x,天空ソラ,m\n"), 0o600))
	rows, gateRefused, err = readAcceptedCSV(minimal)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, gateRefused)
}
