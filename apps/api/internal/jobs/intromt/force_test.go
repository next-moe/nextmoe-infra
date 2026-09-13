package intromt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamedWorkIDsOutrankThePopularityCeiling(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)

	wPopular := mkWork(t, medium, "popular", nil)
	wObscure := mkWork(t, medium, "obscure", nil)
	mkIntro(t, wPopular, "ja", "人気作のあらすじ。", bangumi)
	mkIntro(t, wObscure, "ja", "無名作のあらすじ。", bangumi)
	mkPop(t, wPopular, dlsite, 0, 9000)

	st, err := Run(ctx, nil, Opts{DSN: testDSN, Top: 1, WorkIDs: []int64{wObscure}})
	require.NoError(t, err)
	require.Equal(t, 1, st.Candidates)
	assert.Equal(t, wObscure, st.Samples[0].WorkID,
		"--top 1 must not re-rank a named list down to its most popular member")
}

func TestForceRewritesWhatTheHashCallsCurrent(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)

	w := mkWork(t, medium, "force-me", nil)
	mkIntro(t, w, "ja", "あらすじ本文。", bangumi)

	n := 0
	tr := &fakeTranslator{model: "force-mt", fn: func(ja string) string {
		n++
		return fmt.Sprintf("[译%d] %s", n, ja)
	}}

	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, st.Inserted)

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkipUnchanged, "same ja text and glossary — nothing to redo")
	assert.Zero(t, st.Retranslated)

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Force: true})
	require.NoError(t, err)
	assert.Zero(t, st.SkipUnchanged)
	assert.Equal(t, 1, st.Retranslated, "force ignores a matching hash")

	var zh string
	require.NoError(t, testDB.Raw(
		`SELECT intro FROM catalog_work_intro WHERE work_id = ? AND lang = 'zh-Hans' AND provenance = 1`,
		w).Scan(&zh).Error)
	assert.Equal(t, "[译2] あらすじ本文。", zh, "the forced pass overwrote the row")
}

func TestReasoningEffortReachesTheWireAndIsOmittedByDefault(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = nil
		_ = json.Unmarshal(b, &raw)
		_, _ = io.WriteString(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	plain := NewHTTPTranslator(srv.URL, "t", "m", 64)
	_, _, err := plain.Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	_, present := raw["reasoning_effort"]
	assert.False(t, present, "no effort set — the key must not appear, so a lane that does not set it is unchanged")

	low := NewHTTPTranslator(srv.URL, "t", "m", 64)
	low.SetEffort("low")
	_, _, err = low.Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "low", raw["reasoning_effort"])
}
