package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The four shapes differ from each other in one fact apiece, so a skip is
// attributable. sole is the positive control for both holds: crowded is sole
// plus a third work answering to the name, and alias is sole with the agreement
// moved off display_name onto a secondary title row. Without sole passing, two
// skipped_uncorroborated counts would read the same whether the gate works or
// whether the evidence query returns nothing at all.
func TestAMergeNeedsANameNoOtherWorkAnswersTo(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_work_title, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)
	require.NoError(t, db.Exec("TRUNCATE src_llm.queue_verdict RESTART IDENTITY").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkTitle := func(id int64, lang, title string, kind int16) {
		require.NoError(t, db.Create(&model.CatalogWorkTitle{
			WorkID: id, Lang: lang, Title: title, Kind: kind,
		}).Error)
	}
	mkPair := func(a, b int64, verdict string, conf float64, hash string) (int64, int64) {
		lo, hi := min(a, b), max(a, b)
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: lo, BID: hi,
			Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
		}).Error)
		require.NoError(t, db.Create(&QueueVerdict{
			Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
			AID: lo, BID: hi, InputHash: hash, Model: "test", PromptVersion: PromptWorkPair,
			Verdict: verdict, Confidence: conf, Evidence: []byte(`{}`),
		}).Error)
		return lo, hi
	}
	status := func(a, b int64) int16 {
		var got int16
		require.NoError(t, db.Raw(`SELECT status FROM catalog_match_candidate
			WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
			model.EntityTypeWork, min(a, b), max(a, b)).Scan(&got).Error)
		return got
	}

	// unsure at confidence 0: the other side is a bangumi row with nothing in
	// it, which is what the verdict records. The name is the evidence.
	soleA, soleB := mkWork("Aevum Aeterna"), mkWork("Aevum Aeterna")
	mkPair(soleA, soleB, VerdictUnsure, 0, "hash-sole")

	// works 16532 and 27910 on production: a 1999 Japanese title and a 2022
	// English one, agreeing only because the first carries the second as a
	// localised title row.
	aliasA, aliasB := mkWork("ラブレッスン"), mkWork("Lessons in Love")
	mkTitle(aliasA, "en", "Lessons in Love", model.WorkTitleKindAlias)
	mkPair(aliasA, aliasB, VerdictSame, 1, "hash-alias")

	// Memoria is carried by several unrelated visual novels.
	crowdedA, crowdedB := mkWork("Memoria"), mkWork("Memoria")
	mkWork("Memoria")
	mkPair(crowdedA, crowdedB, VerdictSame, 1, "hash-crowded")

	// the model named a discriminator, and the name gate does not overrule it
	vetoA, vetoB := mkWork("Twin Star Exorcists"), mkWork("Twin Star Exorcists")
	mkPair(vetoA, vetoB, VerdictDifferent, 1, "hash-veto")

	// positive control on the fixture: the alias pair really does share a
	// corpus norm, so its hold below is the display_name rule and not an
	// evidence query that found nothing for anyone.
	var shared int
	require.NoError(t, db.Raw(`WITH corpus AS (`+service.WorkDupeCorpusSQL()+`)
		SELECT count(*) FROM corpus a JOIN corpus b ON b.n = a.n
		WHERE a.work_id = ? AND b.work_id = ?`, aliasA, aliasB).Scan(&shared).Error)
	require.Positive(t, shared, "fixture does not reproduce the alias-agreement shape")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, 1, st.Counts["applied_"+applyReject], "counts: %v", st.Counts)
	assert.Equal(t, 2, st.Counts[skipUncorroborated], "counts: %v", st.Counts)

	assert.Equal(t, model.CandidateStatusAccepted, status(soleA, soleB))
	assert.Equal(t, model.CandidateStatusNeedsManual, status(aliasA, aliasB))
	assert.Equal(t, model.CandidateStatusNeedsManual, status(crowdedA, crowdedB))
	assert.Equal(t, model.CandidateStatusRejected, status(vetoA, vetoB))
}

// A name two records merely share is not the same thing as a name they both
// lead with, and the fold is what makes them comparable at all.
func TestNameEvidenceReadsDisplayNameThroughTheSharedFold(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_work_title, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	// production work 16269 against bangumi 222199: one rune of trailing wave
	// dash apart, and one whitespace variant apart.
	waveA, waveB := mkWork("隣人妄想～団地族の昼下がり～"), mkWork("隣人妄想～団地族の昼下がり")
	spaceA, spaceB := mkWork("ぴゅあぴゅあ"), mkWork("ぴゅあ　ぴゅあ")
	deletedA, deletedB := mkWork("Half A Year"), mkWork("Half A Year")
	gone := mkWork("Half A Year")
	require.NoError(t, db.Delete(&model.CatalogWork{}, gone).Error)
	shortA, shortB := mkWork("PC版"), mkWork("PC版")

	rows := []QueueVerdict{
		{ID: 1, AID: waveA, BID: waveB},
		{ID: 2, AID: spaceA, BID: spaceB},
		{ID: 3, AID: deletedA, BID: deletedB},
		{ID: 4, AID: shortA, BID: shortB},
	}
	ev, err := workPairNameEvidence(db, rows)
	require.NoError(t, err)

	assert.True(t, ev[1].exclusive(), "the trailing wave dash must fold away")
	assert.True(t, ev[2].exclusive(), "the ideographic space must fold away")
	assert.True(t, ev[3].exclusive(), "a merged-away work is not a third holder")
	assert.False(t, ev[4].exclusive(), "a name too short to identify anything is not evidence")
}
