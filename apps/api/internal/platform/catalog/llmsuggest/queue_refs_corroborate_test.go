package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleKeyFoldsWhatTwoRegistriesSpellDifferently(t *testing.T) {
	assert.Equal(t, titleKey("ＤＥＡＲ　Ｍｙ　Ｆｒｉｅｎｄ"), titleKey("DEAR My Friend"))
	assert.Equal(t, titleKey("Clear－クリア－"), titleKey("Clear -クリア-"))
	assert.Empty(t, titleKey("～～"))

	assert.True(t, titlesAgree(titleKey("海の唄がきこえる"), titleKey("海の唄がきこえる 上巻")))
	assert.True(t, titlesAgree(titleKey("Clear－クリア－"), titleKey("Clear -クリア- 新しい風の吹く丘で")))
	assert.False(t, titlesAgree(titleKey("Grisaia no Kajitsu"), titleKey("Hades ASMR")))
	// two short keys must not agree by accident: ab and abcd are not one work
	assert.False(t, titlesAgree(titleKey("ab"), titleKey("abcd")))
}

// The gate has to separate two rows that differ only in whether anything
// corroborates them. Both labels here carry the same name, the same verdict and
// the same confidence — which is the shape that made 5771 Hades, a voice-drama
// circle, into the producer of The Fruit of Grisaia at confidence 1.00.
func TestRefConfirmNeedsSomethingBesidesTheName(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_external_ref, catalog_work, catalog_label RESTART IDENTITY CASCADE").Error)
	// The upstream tables come from srcbangumi, never from a stub here: the whole
	// suite shares one database, so a CREATE TABLE IF NOT EXISTS is a no-op once
	// srcbangumi's own tests have built the real thing, and the insert then hits
	// a NOT NULL column this file never declared. It passed locally and went red
	// in CI on subject.type for exactly that reason.
	require.NoError(t, srcbangumi.EnsureSchema(db))
	require.NoError(t, db.Exec(`TRUNCATE src_bangumi.subject, src_bangumi.subject_person`).Error)

	var medium, bgm int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&bgm).Error)

	upstream := func(subject int64, name string, person int64) {
		require.NoError(t, db.Create(&srcbangumi.Subject{ID: subject, Name: name}).Error)
		require.NoError(t, db.Create(&srcbangumi.SubjectPerson{SubjectID: subject, PersonID: person}).Error)
	}
	label := func(name, workTitle, workExternalID string) int64 {
		l := &model.CatalogLabel{DisplayName: name}
		require.NoError(t, db.Create(l).Error)
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: workTitle,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		require.NoError(t, db.Create(&model.CatalogWorkLabel{WorkID: w.ID, LabelID: l.ID}).Error)
		if workExternalID != "" {
			require.NoError(t, db.Create(&model.CatalogExternalRef{
				EntityType: model.EntityTypeWork, EntityID: w.ID, SourceID: bgm,
				ExternalID: workExternalID, LinkKind: model.LinkKindExact, MatchedBy: "test",
			}).Error)
		}
		return l.ID
	}
	queue := func(labelID int64, person string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeLabel, EntityID: labelID, SourceID: bgm,
			ExternalID: person, LinkKind: model.LinkKindProbable, MatchedBy: "rule:name",
		}).Error)
		require.NoError(t, db.Create(&QueueVerdict{
			Queue: QueueRef, Lane: LaneLLM, EntityType: model.EntityTypeLabel,
			EntityID: labelID, SourceID: bgm, ExternalID: person,
			InputHash:     refInputHash(model.EntityTypeLabel, labelID, bgm, person),
			Model:         "test-model",
			PromptVersion: PromptRef, Verdict: VerdictSame, Confidence: 1,
			Evidence: []byte(`{}`),
		}).Error)
	}
	kind := func(labelID int64, person string) int16 {
		var got int16
		require.NoError(t, db.Raw(`SELECT link_kind FROM catalog_external_ref
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			model.EntityTypeLabel, labelID, bgm, person).Scan(&got).Error)
		return got
	}

	upstream(100, "Mirai no Kimi", 55)      // corroborates by title
	upstream(300, "Unrelated Upstream", 77) // corroborates by the work's own bangumi id
	upstream(200, "Totally Other Game", 66) // corroborates nothing

	byTitle := label("Hades", "Mirai no Kimi", "")
	byID := label("Hades", "A Title Nobody Shares", "300")
	bare := label("Hades", "Voice Drama Vol. 3", "")
	queue(byTitle, "55")
	queue(byID, "77")
	queue(bare, "66")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueRef, Actor: 1, MinConfidence: 0.9,
	})
	require.NoError(t, err)

	assert.Equal(t, 2, st.Counts["applied_"+applyConfirm], "counts: %v", st.Counts)
	assert.Equal(t, 1, st.Counts[skipUncorroborated], "counts: %v", st.Counts)
	assert.Equal(t, model.LinkKindExact, kind(byTitle, "55"))
	assert.Equal(t, model.LinkKindExact, kind(byID, "77"))
	assert.Equal(t, model.LinkKindProbable, kind(bare, "66"), "a name agreeing with itself is not evidence")
}

// The erogamescape brands are half the queue and they live in another database,
// so an apply run without that mirror must say so rather than report the rows
// as judged and held.
func TestEGRefsWithoutTheMirrorAreNotSilentlyHeld(t *testing.T) {
	reg := sourceReg{idByKey: map[string]int16{sourceKeyEG: 9}, keyByID: map[int16]string{9: sourceKeyEG}}
	rows := []QueueVerdict{{
		ID: 1, EntityType: model.EntityTypeLabel, EntityID: 5,
		SourceID: 9, ExternalID: "2699", Verdict: VerdictSame, Confidence: 1,
	}}
	_, _, unavailable, err := sourceNeighbourWorks(nil, nil, reg, rows)
	require.NoError(t, err)
	assert.True(t, unavailable[1])
	assert.Equal(t, skipNoCorroborator, planRef(VerdictSame, 1, 0.9, 0, false, refEvidence{Unavailable: true}).Skip)
}
