package llmsuggest

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedJudgementIsNotDone(t *testing.T) {
	db := testCatalogDB(t)
	failed := workPairHash(11, 12)
	answered := workPairHash(21, 22)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 11, BID: 12,
		InputHash: failed, Model: "m", PromptVersion: PromptWorkPair,
		Error: `vllm http 429: {"error":{"message":"openai_error"}}`,
	}).Error)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 21, BID: 22,
		InputHash: answered, Model: "m", PromptVersion: PromptWorkPair,
		Verdict: VerdictSame, Confidence: 0.95,
	}).Error)

	done, err := loadDoneHashes(db, "src_llm.queue_verdict", "m", PromptWorkPair, "queue", QueueWorkPair)
	require.NoError(t, err)
	assert.False(t, done[failed], "a row whose model call failed must come back to the queue")
	assert.True(t, done[answered], "a row that carries a verdict must stay out of the queue")
}

func TestRetryOverwritesTheStoredFailure(t *testing.T) {
	db := testCatalogDB(t)
	h := workPairHash(31, 32)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 31, BID: 32,
		InputHash: h, Model: "m", PromptVersion: PromptWorkPair, Error: "vllm http 429",
	}).Error)

	persistQueueVerdict(db, &QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 31, BID: 32,
		InputHash: h, Model: "m", PromptVersion: PromptWorkPair,
		Verdict: VerdictSame, Reason: "same work", Confidence: 0.93,
	})

	var rows []QueueVerdict
	require.NoError(t, db.Where("input_hash = ?", h).Find(&rows).Error)
	require.Len(t, rows, 1, "the retry must land on the failed row, not beside it")
	assert.Equal(t, VerdictSame, rows[0].Verdict)
	assert.InDelta(t, 0.93, rows[0].Confidence, 0.001)
	assert.Empty(t, rows[0].Error, "apply reads only rows with an empty error")
}

func TestRetryLeavesAnAppliedVerdictAlone(t *testing.T) {
	db := testCatalogDB(t)
	h := workPairHash(41, 42)
	at := time.Now()
	by := int64(7)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 41, BID: 42,
		InputHash: h, Model: "m", PromptVersion: PromptWorkPair,
		Verdict: VerdictDifferent, Confidence: 0.97,
		AppliedAction: applyReject, AppliedAt: &at, AppliedBy: &by,
	}).Error)

	persistQueueVerdict(db, &QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork, AID: 41, BID: 42,
		InputHash: h, Model: "m", PromptVersion: PromptWorkPair,
		Verdict: VerdictSame, Confidence: 0.10,
	})

	var got QueueVerdict
	require.NoError(t, db.Where("input_hash = ?", h).First(&got).Error)
	assert.Equal(t, VerdictDifferent, got.Verdict, "a decided verdict must survive a later pass")
	assert.Equal(t, applyReject, got.AppliedAction)
}

func TestGoldsetRetryUsesItsOwnConflictKey(t *testing.T) {
	db := testCatalogDB(t)
	row := NamePairJudgment{
		Task: "goldset", InputHash: "h-gold", Model: "m", PromptVersion: PromptNamePairV1,
		A: "a", B: "b", Error: "vllm http 429",
	}
	require.NoError(t, db.Create(&row).Error)

	retry := row
	retry.ID, retry.Error, retry.Verdict, retry.Confidence = 0, "", VerdictSame, 0.88
	require.NoError(t, upsertJudgement(db, &retry, "name_pair_judgment",
		[]string{"task", "input_hash", "model", "prompt_version"},
		[]string{"a", "b", "gold_label", "source_rule", "verdict", "reason", "confidence", "error", "created_at"}))

	var rows []NamePairJudgment
	require.NoError(t, db.Where("input_hash = ?", "h-gold").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, VerdictSame, rows[0].Verdict)
	assert.Empty(t, rows[0].Error)
}
