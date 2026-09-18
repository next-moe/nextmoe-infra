package llmsuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"api/internal/platform/catalog/srcbangumi"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func mockLLM(t *testing.T, content func(call int) string) *Client {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		resp := map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content(n)},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"completion_tokens": 7},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "mock-model")
}

func TestJudgeParsesVerdict(t *testing.T) {
	c := mockLLM(t, func(int) string { return `{"verdict":"same","reason":"kanji vs simplified","confidence":0.92}` })
	v, err := judge(context.Background(), c, "sys", "user", 256)
	require.NoError(t, err)
	assert.Equal(t, VerdictSame, v.Verdict)
	assert.Equal(t, 0.92, v.Confidence)
}

func TestJudgeRetriesThenSucceeds(t *testing.T) {
	c := mockLLM(t, func(call int) string {
		if call == 0 {
			return "not json at all"
		}
		return `{"verdict":"different","reason":"different people","confidence":0.8}`
	})
	v, err := judge(context.Background(), c, "sys", "user", 256)
	require.NoError(t, err)
	assert.Equal(t, VerdictDifferent, v.Verdict)
}

func TestJudgeRejectsOutOfEnum(t *testing.T) {
	c := mockLLM(t, func(int) string { return `{"verdict":"maybe","reason":"x","confidence":0.5}` })
	_, err := judge(context.Background(), c, "sys", "user", 256)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of enum")
}

func TestExtractResidueValidatesJSON(t *testing.T) {
	c := mockLLM(t, func(call int) string {
		if call == 0 {
			return "```json broken"
		}
		return `{"type":"Game","fields":[{"key":"名","value":"X"}]}`
	})
	out, err := extractResidue(context.Background(), c, "raw")
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"Game","fields":[{"key":"名","value":"X"}]}`, string(out))
}

func testCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "no test database: %v", err)
	}
	require.NoError(t, EnsureSchema(db))
	require.NoError(t, srcbangumi.EnsureSchema(db))
	require.NoError(t, db.Exec("TRUNCATE src_llm.name_pair_judgment, src_llm.queue_verdict, src_llm.run RESTART IDENTITY").Error)
	return db
}

func TestGoldsetPersistAndResume(t *testing.T) {
	db := testCatalogDB(t)
	c := mockLLM(t, func(int) string { return `{"verdict":"same","reason":"r","confidence":0.9}` })

	path := filepath.Join(t.TempDir(), "gold.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	enc := json.NewEncoder(f)
	for i := 0; i < 3; i++ {
		require.NoError(t, enc.Encode(GoldPair{A: fmt.Sprintf("A%d", i), B: fmt.Sprintf("B%d", i), Label: VerdictSame, SourceRule: "test"}))
	}
	require.NoError(t, f.Close())

	opts := Options{Model: "mock-model", Concurrency: 2, GoldSetPath: path}
	judged, errs, err := RunGoldset(context.Background(), db, c, opts)
	require.NoError(t, err)
	assert.Equal(t, 3, judged)
	assert.Zero(t, errs)

	var count int64
	db.Table("src_llm.name_pair_judgment").Count(&count)
	assert.Equal(t, int64(3), count)

	judged2, _, err := RunGoldset(context.Background(), db, c, opts)
	require.NoError(t, err)
	assert.Zero(t, judged2, "resume must skip already-judged pairs")
	db.Table("src_llm.name_pair_judgment").Count(&count)
	assert.Equal(t, int64(3), count, "no duplicate rows on resume")

	metrics, err := Calibrate(db, "mock-model", PromptNamePairV1)
	require.NoError(t, err)
	assert.NotEmpty(t, metrics)
}
