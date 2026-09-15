package llmsuggest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSurvivorDirectionClaimed(t *testing.T) {
	s := workPairSides{AID: 10, BID: 20, ClaimedA: true, ClaimedB: false, ExactA: 0, ExactB: 9}
	src, tgt := survivorTarget(s)
	assert.Equal(t, int64(20), src)
	assert.Equal(t, int64(10), tgt)

	s = workPairSides{AID: 10, BID: 20, ClaimedA: false, ClaimedB: true, ExactA: 9, ExactB: 0}
	src, tgt = survivorTarget(s)
	assert.Equal(t, int64(10), src)
	assert.Equal(t, int64(20), tgt)
}

func TestSurvivorDirectionExactRefs(t *testing.T) {
	s := workPairSides{AID: 10, BID: 20, ExactA: 3, ExactB: 1}
	src, tgt := survivorTarget(s)
	assert.Equal(t, int64(20), src)
	assert.Equal(t, int64(10), tgt)

	s = workPairSides{AID: 10, BID: 20, ExactA: 1, ExactB: 4}
	src, tgt = survivorTarget(s)
	assert.Equal(t, int64(10), src)
	assert.Equal(t, int64(20), tgt)
}

func TestSurvivorDirectionLowerID(t *testing.T) {
	s := workPairSides{AID: 10, BID: 20, ExactA: 2, ExactB: 2}
	src, tgt := survivorTarget(s)
	assert.Equal(t, int64(20), src)
	assert.Equal(t, int64(10), tgt)

	s = workPairSides{AID: 20, BID: 10, ExactA: 2, ExactB: 2}
	src, tgt = survivorTarget(s)
	assert.Equal(t, int64(20), src)
	assert.Equal(t, int64(10), tgt)
}

func TestBothClaimedFreeze(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2, ClaimedA: true, ClaimedB: true}
	assert.True(t, bothClaimed(s))
	p := planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole"))
	assert.Equal(t, skipFrozenBothClaimed, p.Skip)
	assert.Empty(t, p.Action)
}

func TestVerdictActionMapping(t *testing.T) {
	p := planCreditName(VerdictSame, 0.95, 0.9, 0.7)
	assert.Equal(t, applyAccept, p.Action)
	p = planCreditName(VerdictDifferent, 0.95, 0.9, 0.7)
	assert.Equal(t, applyReject, p.Action)
	p = planCreditName(VerdictUnsure, 1, 0.9, 0.7)
	assert.Equal(t, skipUnsure, p.Skip)

	s := workPairSides{AID: 1, BID: 2}
	p = planWorkPair(VerdictDifferent, 0.95, 0.7, s, soleName("sole"))
	assert.Equal(t, applyReject, p.Action)
	p = planWorkPair(VerdictSame, 0.95, 0.7, s, soleName("sole"))
	assert.Equal(t, applyAccept, p.Action)
	assert.Equal(t, int64(2), p.Source)
	assert.Equal(t, int64(1), p.Target)
}

func TestNeverRejectRef(t *testing.T) {
	p := planRef(VerdictDifferent, 1, 0.9, false, refEvidence{})
	assert.NotEqual(t, applyReject, p.Action)
	assert.NotEqual(t, applyConfirm, p.Action)
	assert.Equal(t, skipRefDifferent, p.Skip)

	p = planRef(VerdictSame, 0.95, 0.9, false, refEvidence{Corroborator: "vndb v1"})
	assert.Equal(t, applyConfirm, p.Action)
	p = planRef(VerdictChainVerified, 1, 0.9, false, refEvidence{})
	assert.Equal(t, applyConfirm, p.Action)
	p = planRef(VerdictChainUnproven, 0, 0.9, false, refEvidence{})
	assert.Equal(t, skipChainUnproven, p.Skip)
	p = planRef(VerdictUnsure, 1, 0.9, false, refEvidence{})
	assert.Equal(t, skipUnsure, p.Skip)
}

func TestChainHashStable(t *testing.T) {
	a := refInputHash(6, 10, 2, "r50")
	b := refInputHash(6, 10, 2, "r50")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, refInputHash(6, 11, 2, "r50"))
	assert.Equal(t, creditNameHash(1, 2, "A", "B"), creditNameHash(1, 2, "A", "B"))
	assert.NotEqual(t, creditNameHash(1, 2, "A", "B"), creditNameHash(1, 2, "A", "C"))
	assert.Equal(t, workPairHash(3, 4), workPairHash(3, 4))
}

func TestGoldQueueSuffixIsolation(t *testing.T) {
	assert.Equal(t, "workpair-gold", goldQueue(QueueWorkPair))
	assert.Equal(t, "ref-gold", goldQueue(QueueRef))
	assert.True(t, isLiveQueue(QueueCreditName))
	assert.True(t, isLiveQueue(QueueWorkPair))
	assert.True(t, isLiveQueue(QueueRef))
	assert.False(t, isLiveQueue(goldQueue(QueueWorkPair)))
	assert.False(t, isLiveQueue(goldQueue(QueueRef)))
	assert.True(t, isGoldQueue(goldQueue(QueueRef)))
	assert.False(t, isGoldQueue(QueueRef))
}

func TestApplyRefusesGoldQueue(t *testing.T) {
	_, err := RunApply(t.Context(), nil, StagingDBs{}, nil, Options{Queue: goldQueue(QueueRef), Actor: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refuses")
	_, err = RunApply(t.Context(), nil, StagingDBs{}, nil, Options{Queue: "nope", Actor: 1})
	require.Error(t, err)
}

func TestVNDBDateEquals(t *testing.T) {
	y, m, d := int16(2020), int16(1), int16(15)
	assert.True(t, vndbDateEquals(&y, &m, &d, 20200115))
	assert.False(t, vndbDateEquals(&y, &m, &d, 20200116))
	assert.False(t, vndbDateEquals(nil, nil, nil, 20200115))
	y2 := int16(2020)
	assert.True(t, vndbDateEquals(&y2, nil, nil, 20200000))
	assert.False(t, vndbDateEquals(&y2, nil, nil, 20200115))
}

func TestClientSendsBearer(t *testing.T) {
	t.Setenv("KUN_LLM_API_KEY", "secret-token")
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"verdict":"same","reason":"r","confidence":1}`}, "finish_reason": "stop"}},
			"usage":   map[string]any{"completion_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "mock")
	_, err := judge(t.Context(), c, "sys", "user", 32)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret-token", got)
}

func TestChainResumeIdempotent(t *testing.T) {
	db := testCatalogDB(t)
	h := refInputHash(6, 9, 2, "r1")
	row := QueueVerdict{
		Queue: QueueRef, Lane: LaneChain, InputHash: h,
		Model: ChainModel, PromptVersion: PromptChain,
		Verdict: VerdictChainVerified, Confidence: 1,
	}
	require.NoError(t, db.Create(&row).Error)
	done, err := loadDoneHashes(db, "src_llm.queue_verdict", ChainModel, PromptChain, "queue", QueueRef)
	require.NoError(t, err)
	assert.True(t, done[h])
	dup := row
	dup.ID = 0
	err = db.Create(&dup).Error
	require.Error(t, err)
}

func TestGoldAndLiveShareHash(t *testing.T) {
	db := testCatalogDB(t)
	h := refInputHash(5, 1, 3, "99")
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueRef, InputHash: h, Model: "m", PromptVersion: "v", Lane: LaneLLM,
	}).Error)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: goldQueue(QueueRef), InputHash: h, Model: "m", PromptVersion: "v", Lane: LaneLLM,
	}).Error)
	var n int64
	db.Table("src_llm.queue_verdict").Count(&n)
	assert.Equal(t, int64(2), n)
}

func TestNormVNDBID(t *testing.T) {
	assert.Equal(t, "v17", normVNDBID("v17"))
	assert.Equal(t, "v17", normVNDBID("17"))
	assert.Equal(t, "v17", normVNDBID("V17"))
}
