package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanoutIsReleaseOnly(t *testing.T) {
	rel := func(id int64, ext string) refItem {
		return refItem{EntityType: model.EntityTypeRelease, EntityID: id, SourceID: 2, ExternalID: ext}
	}
	work := func(id int64, ext string) refItem {
		return refItem{EntityType: model.EntityTypeWork, EntityID: id, SourceID: 2, ExternalID: ext}
	}
	items := []refItem{
		rel(1, "r100"), rel(2, "r100"), rel(3, "r100"),
		rel(4, "r200"),
		// two works sharing one upstream id is the duplicate signal the work-pair
		// lane exists to act on, not a fan-out
		work(5, "v300"), work(6, "v300"),
	}
	m := fanoutMembers(items)
	assert.True(t, isFanout(m, rel(1, "r100")))
	assert.False(t, isFanout(m, rel(4, "r200")), "a singleton release is a real confirm candidate")
	assert.False(t, isFanout(m, work(5, "v300")), "duplicate works must stay out of this lane")
	assert.Equal(t, 3, m[exactSlotKey(model.EntityTypeRelease, 2, "r100")])
}

func TestRelatedConfirmsWithoutAConfidenceBar(t *testing.T) {
	assert.Equal(t, applyConfirmRelated, planRef(VerdictRelated, 1, 0.9, false, refEvidence{}).Action)
	assert.Equal(t, applyConfirmRelated, planRef(VerdictRelated, 0, 0.9, false, refEvidence{}).Action,
		"the fan-out lane counts rows, it does not estimate")
	_, refVerdicts := applySelection(Options{Queue: QueueRef, MinConfidence: 0.9})
	assert.Contains(t, refVerdicts, VerdictRelated)
	_, wpVerdicts := applySelection(Options{Queue: QueueWorkPair})
	assert.NotContains(t, wpVerdicts, VerdictRelated)
	assert.Contains(t, currentPrompts[QueueRef], PromptFanout)
}

func TestApplyLeavesNoMemberOfAFanoutGroupHoldingExact(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_external_ref, catalog_release, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var vndb int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)

	mkRelease := func(title string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: title,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		ja := "ja"
		r := &model.CatalogRelease{WorkID: w.ID, Title: &title, Lang: &ja}
		require.NoError(t, db.Create(r).Error)
		return r.ID
	}
	probable := func(id int64, ext string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeRelease, EntityID: id, SourceID: vndb,
			ExternalID: ext, LinkKind: model.LinkKindProbable,
			MatchedBy: "rule:vndb-release-import-probable",
		}).Error)
	}
	verdict := func(id int64, ext, v, prompt string, conf float64) {
		require.NoError(t, db.Create(&QueueVerdict{
			Queue: QueueRef, Lane: LaneChain, EntityType: model.EntityTypeRelease,
			EntityID: id, SourceID: vndb, ExternalID: ext,
			InputHash: refInputHash(model.EntityTypeRelease, id, vndb, ext),
			Model:     ChainModel, PromptVersion: prompt, Verdict: v, Confidence: conf,
			Evidence: []byte(`{}`),
		}).Error)
	}
	state := func(id int64, ext string) (int16, bool) {
		var got struct {
			LinkKind int16 `gorm:"column:link_kind"`
			Verified bool  `gorm:"column:verified"`
		}
		require.NoError(t, db.Raw(`SELECT link_kind, verified_at IS NOT NULL AS verified
			FROM catalog_external_ref WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			model.EntityTypeRelease, id, vndb, ext).Scan(&got).Error)
		return got.LinkKind, got.Verified
	}

	// one upstream collection release imported as three catalog releases
	var group []int64
	for _, title := range []string{"pack disc A", "pack disc B", "pack disc C"} {
		id := mkRelease(title)
		group = append(group, id)
		probable(id, "r900")
		verdict(id, "r900", VerdictRelated, PromptFanout, 1)
	}
	// negative control: a singleton must still be promoted, or this change would
	// simply have disabled ref confirmation. chain-verified is what the chain
	// lane writes for one: llmFamily excludes Release, so no release ref ever
	// carries the same verdict that planRef now asks a corroborator about.
	solo := mkRelease("standalone")
	probable(solo, "r901")
	verdict(solo, "r901", VerdictChainVerified, PromptChain, 1)

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueRef, Actor: 1, MinConfidence: 0.9,
	})
	require.NoError(t, err)

	assert.Equal(t, 3, st.Counts["applied_"+applyConfirmRelated], "counts: %v", st.Counts)
	assert.Equal(t, 1, st.Counts["applied_"+applyConfirm], "counts: %v", st.Counts)
	for _, id := range group {
		kind, verified := state(id, "r900")
		assert.Equal(t, model.LinkKindProbable, kind, "no member of a fan-out group may hold exact")
		assert.True(t, verified, "and every member must leave the queue")
	}
	kind, verified := state(solo, "r901")
	assert.Equal(t, model.LinkKindExact, kind)
	assert.True(t, verified)
}
