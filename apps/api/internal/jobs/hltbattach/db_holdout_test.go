package hltbattach

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldoutReportCountsAttachesAgainstTruth(t *testing.T) {
	requireDB(t)
	t.Run("correct", func(t *testing.T) {
		requireDB(t)
		vndb, hltb := sourceID(t, "vndb"), sourceID(t, "howlongtobeat")
		w := mkWork(t, "Holdout Game Title")
		mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		mkRef(t, model.EntityTypeWork, w, vndb, "vH1", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, w, hltb, "50", model.LinkKindExact)
		insertGame(t, game{ID: 50, Name: "Holdout Game Title", World: "2001-01-01"})

		before := refCount(t)
		st, err := Run(context.Background(), Opts{
			DSN: testDSN, HltbDSN: hltbTestDSN, HoldoutReport: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, st.Population)
		assert.Equal(t, 1, st.HoldoutCorrect)
		assert.Zero(t, st.HoldoutWrong)
		assert.Zero(t, st.HoldoutSkipped)
		assert.Equal(t, before, refCount(t))
	})
	t.Run("wrong", func(t *testing.T) {
		requireDB(t)
		vndb, hltb := sourceID(t, "vndb"), sourceID(t, "howlongtobeat")
		truth := mkWork(t, "Holdout Truth Only Title")
		other := mkWork(t, "Shared Holdout Title")
		mkReleaseYMD(t, other, i16(2001), i16(1), i16(1))
		mkRef(t, model.EntityTypeWork, truth, vndb, "vH2", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, truth, hltb, "51", model.LinkKindExact)
		insertGame(t, game{ID: 51, Name: "Shared Holdout Title", World: "2001-01-01"})

		path := filepath.Join(t.TempDir(), "wrong.jsonl")
		before := refCount(t)
		st, err := Run(context.Background(), Opts{
			DSN: testDSN, HltbDSN: hltbTestDSN, HoldoutReport: true, Receipts: path, Apply: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, st.HoldoutWrong)
		assert.Zero(t, st.HoldoutCorrect)
		assert.Equal(t, before, refCount(t), "holdout never writes")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var rec receipt
		require.NoError(t, json.Unmarshal(raw, &rec))
		assert.Equal(t, int64(51), rec.HltbID)
		assert.Equal(t, other, rec.WorkID)
		assert.Equal(t, ruleTitleDate, rec.MatchedBy)
	})
}
