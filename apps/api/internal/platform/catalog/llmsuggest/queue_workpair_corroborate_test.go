package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/platform/editing"

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
		"TRUNCATE catalog_merge_proposal, catalog_match_candidate, catalog_work_title, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)
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
	assert.Equal(t, 2, st.Counts["applied_"+stampKeptApartUncorroborated], "counts: %v", st.Counts)

	assert.Equal(t, model.CandidateStatusAccepted, status(soleA, soleB))
	assert.Equal(t, model.CandidateStatusDeferred, status(aliasA, aliasB))
	assert.Equal(t, model.CandidateStatusDeferred, status(crowdedA, crowdedB))
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

func TestLooseNameEvidenceFromCorpus(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_work_title, catalog_work, edit_suppressed_row RESTART IDENTITY CASCADE").Error)

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
	a := mkWork("淫虐の学園 ～猥堕に蔓延る復讐の罠～")
	b := mkWork("淫虐の学園 〜猥堕に蔓延る復讐の罠〜")
	rows := []QueueVerdict{{ID: 1, AID: a, BID: b}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusive(), "the middle wave dash is not the SQL fold")
	assert.True(t, ev[1].exclusiveLoose(), "titleKey must drop U+FF5E vs U+301C")

	suppressed := mkWork("unrelated suppressed host")
	alias := "淫虐の学園 ～猥堕に蔓延る復讐の罠～"
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: suppressed, Lang: "ja", Title: alias, Kind: model.WorkTitleKindAlias,
	}).Error)
	require.NoError(t, db.Create(&editing.SuppressedRow{
		EntityType:  editspec.TypeWork,
		EntityID:    suppressed,
		FieldKey:    editspec.FieldWorkTitles,
		IdentityKey: editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "ja", alias),
	}).Error)
	ev, err = loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.True(t, ev[1].exclusiveLoose(), "a suppressed title is not a third holder")

	third := mkWork("unrelated third host")
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: third, Lang: "ja", Title: alias, Kind: model.WorkTitleKindAlias,
	}).Error)
	ev, err = loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusiveLoose(), "a kind-1 title is a holder")
	assert.Equal(t, 3, ev[1].LooseHolders)
}

func TestSharedRecordEvidenceFromRefs(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium, eg int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'erogamescape'`).Scan(&eg).Error)
	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkRef := func(id int64, ext string, kind int16, matched string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: id, SourceID: eg,
			ExternalID: ext, LinkKind: kind, MatchedBy: matched,
		}).Error)
	}

	twinA, twinB := mkWork("twin A"), mkWork("twin B")
	mkRef(twinA, "111", model.LinkKindExact, "test")
	mkRef(twinB, "111", model.LinkKindRelated, matchedByEGXlinkTwin)

	multiA, multiB := mkWork("multi A"), mkWork("multi B")
	mkRef(multiA, "222", model.LinkKindRelated, matchedByEGXlinkMulti)
	mkRef(multiB, "222", model.LinkKindRelated, matchedByEGXlinkMulti)

	crowdA, crowdB, crowdC := mkWork("crowd A"), mkWork("crowd B"), mkWork("crowd C")
	mkRef(crowdA, "333", model.LinkKindExact, "test")
	mkRef(crowdB, "333", model.LinkKindRelated, matchedByEGXlinkTwin)
	mkRef(crowdC, "333", model.LinkKindRelated, "rule:other")

	// A store record derived from each side's own EG anchor: two EG edition
	// entries can name one DMM or Steam product, so sharing one proves nothing.
	var dmm int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'dmm'`).Scan(&dmm).Error)
	storeA, storeB := mkWork("store A"), mkWork("store B")
	for _, id := range []int64{storeA, storeB} {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: id, SourceID: dmm,
			ExternalID: "d_444", LinkKind: model.LinkKindProbable, MatchedBy: "rule:eg-dmm",
		}).Error)
	}

	rows := []QueueVerdict{
		{ID: 1, AID: twinA, BID: twinB},
		{ID: 2, AID: multiA, BID: multiB},
		{ID: 3, AID: crowdA, BID: crowdB},
		{ID: 4, AID: storeA, BID: storeB},
	}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.True(t, ev[1].exclusiveShared(), "twin related plus exact is an identity record")
	assert.Equal(t, "erogamescape", ev[1].SharedSourceKey)
	assert.Equal(t, "111", ev[1].SharedExternalID)
	assert.False(t, ev[2].exclusiveShared(), "a bundle record is not evidence of identity")
	assert.False(t, ev[3].exclusiveShared(), "a third holder at link kind 2 breaks it")
	assert.False(t, ev[4].exclusiveShared(), "a shared store record is not an identity record")
}
