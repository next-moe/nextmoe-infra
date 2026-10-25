package walkthroughdraft

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough/extract"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/walkthroughdraft")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("catalog/walkthroughdraft", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("catalog/walkthroughdraft", "catalog migration failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

const pasted = `【START】から開始
帰る
寧々END`

const modelReply = `{"no_chart":false,
 "characters":[{"key":"c1","name":"寧々","aliases":[]}],
 "sections":[],"entries":[{"key":"start","label":"START","unlocked_by":null}],"saves":[],
 "segments":[{"key":"g1","section":null,"from":{"node_type":"entry","key":"start"},"condition":null,
   "steps":[{"step_type":"choice","text":"帰る","alt":[],"achievement":false,"save":null}],
   "to":{"node_type":"ending","ending":{"name":"寧々END","ending_type":"heroine","characters":["c1"]}}}]}`

func fakeModel(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, pasted, req.Messages[1].Content)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": modelReply}, "finish_reason": "stop"}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDraftChartsTheTextAndCountsAgainstTheDailyCap(t *testing.T) {
	require.NoError(t, testDB.Exec("TRUNCATE catalog_walkthrough_draft_log").Error)
	calls := 0
	s := NewService(testDB, fakeModel(t, &calls).URL, "test-model")

	draft, err := s.Draft(t.Context(), 7, pasted)
	require.NoError(t, err)
	assert.Equal(t, extract.VerdictOK, draft.Verdict)
	require.NotNil(t, draft.Chart)
	require.Len(t, draft.Chart.Endings, 1)
	assert.Equal(t, "寧々END", draft.Chart.Endings[0].Name)
	assert.Equal(t, PerDay-1, draft.Remaining)

	var logged []model.CatalogWalkthroughDraftLog
	require.NoError(t, testDB.Find(&logged).Error)
	require.Len(t, logged, 1)
	assert.Equal(t, len([]rune(pasted)), logged[0].InputRunes)

	for i := 1; i < PerDay; i++ {
		_, err := s.Draft(t.Context(), 7, pasted)
		require.NoError(t, err)
	}
	_, err = s.Draft(t.Context(), 7, pasted)
	assert.True(t, errors.Is(err, ErrQuota), "draft %d: %v", PerDay+1, err)
	assert.Equal(t, PerDay, calls, "a refused draft never reaches the model")

	_, err = s.Draft(t.Context(), 8, pasted)
	assert.NoError(t, err, "the cap is per user")
}

func TestDraftWithoutAModelIsUnavailable(t *testing.T) {
	_, err := NewService(testDB, "", "").Draft(t.Context(), 7, pasted)
	assert.True(t, errors.Is(err, ErrUnavailable))
}
