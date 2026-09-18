package egworks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldoutReportCountsAttachesAgainstTruth(t *testing.T) {
	requireDB(t)
	t.Run("correct", func(t *testing.T) {
		requireDB(t)
		vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
		w := mkWork(t, "Holdout Game Title")
		mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		mkRef(t, model.EntityTypeWork, w, vndb, "vH1", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, w, eg, "50", model.LinkKindExact)
		insertGame(t, game{ID: 50, Gamename: "Holdout Game Title", Sellday: "2001-01-01", Model: "PC"})

		before := refCount(t)
		st, err := Run(context.Background(), Opts{
			DSN: testDSN, EGDSN: egTestDSN, HoldoutReport: true,
			Now: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		assert.Equal(t, 1, st.Population)
		assert.Equal(t, 1, st.HoldoutCorrect)
		assert.Zero(t, st.HoldoutWrong)
		assert.Zero(t, st.HoldoutQuarantined)
		assert.Zero(t, st.HoldoutMinted)
		assert.Equal(t, before, refCount(t))
		require.NotEmpty(t, st.HoldoutRules)
		assert.Equal(t, ruleTitleDate, st.HoldoutRules[0].Rule)
		assert.Equal(t, 1, st.HoldoutRules[0].Correct)
	})
	t.Run("wrong", func(t *testing.T) {
		requireDB(t)
		vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
		truth := mkWork(t, "Shared Holdout Title")
		other := mkWork(t, "Shared Holdout Title")
		mkReleaseYMD(t, other, i16(2001), i16(1), i16(1))
		mkRef(t, model.EntityTypeWork, truth, vndb, "vH2", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, truth, eg, "51", model.LinkKindExact)
		insertGame(t, game{ID: 51, Gamename: "Shared Holdout Title", Sellday: "2001-01-01", Model: "PC"})

		path := filepath.Join(t.TempDir(), "wrong.jsonl")
		before := refCount(t)
		st, err := Run(context.Background(), Opts{
			DSN: testDSN, EGDSN: egTestDSN, HoldoutReport: true, Receipts: path, Apply: true,
			Now: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		assert.Equal(t, 1, st.HoldoutWrong)
		assert.Zero(t, st.HoldoutCorrect)
		assert.Equal(t, before, refCount(t), "holdout never writes")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var rec receipt
		require.NoError(t, json.Unmarshal(raw, &rec))
		assert.Equal(t, int64(51), rec.EgID)
		assert.Equal(t, other, rec.WorkID)
		assert.Equal(t, ruleTitleDate, rec.MatchedBy)
	})
}
