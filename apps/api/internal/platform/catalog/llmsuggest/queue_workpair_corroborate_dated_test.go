package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func addTitle(t *testing.T, db *gorm.DB, workID int64, lang, title string, kind, provenance int16) {
	t.Helper()
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: workID, Lang: lang, Title: title, Kind: kind, Provenance: provenance,
	}).Error)
}

func addRelease(t *testing.T, db *gorm.DB, workID int64, y, m, d int16) int64 {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault, ReleasedY: &y, ReleasedM: &m, ReleasedD: &d}
	require.NoError(t, db.Create(rel).Error)
	return rel.ID
}

func addRef(t *testing.T, db *gorm.DB, entityType int16, entityID int64, source, ext string, linkKind int16) {
	t.Helper()
	var sourceID int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, source).Scan(&sourceID).Error)
	require.NotZero(t, sourceID, source)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: sourceID,
		ExternalID: ext, LinkKind: linkKind, MatchedBy: "test",
	}).Error)
}

func runWorkPairApply(t *testing.T, db *gorm.DB) map[string]int {
	t.Helper()
	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	return st.Counts
}

func TestDatedTitleCorroboratesAliasTwin(t *testing.T) {
	db, medium := setupWorkPairDB(t)

	// 61790 carries the bangumi mint's display name as one of its aliases.
	dc := createLiveWork(t, db, medium, "D.C.5 to 4 ～ダ・カーポ5トゥ4～ ルミナスメモリーズ")
	addTitle(t, db, dc, "", "D.C.5 to 4 Luminous Memories", model.WorkTitleKindAlias, model.WorkTitleProvenanceSource)
	addRelease(t, db, dc, 2026, 10, 30)
	mint := createLiveWork(t, db, medium, "D.C.5 to 4 Luminous Memories")
	addRelease(t, db, mint, 2026, 10, 30)
	fileWorkPair(t, db, dc, mint, VerdictSame, "hash-dc5")

	// 16532 and 27910 share a machine title and sit 23 years apart.
	love := createLiveWork(t, db, medium, "ラブレッスン")
	addTitle(t, db, love, "zh-Hans", "恋爱课程", model.WorkTitleKindOfficial, model.WorkTitleProvenanceMachine)
	addRelease(t, db, love, 1999, 4, 1)
	lessons := createLiveWork(t, db, medium, "Lessons in Love")
	addTitle(t, db, lessons, "zh-Hans", "恋爱课程", model.WorkTitleKindOfficial, model.WorkTitleProvenanceMachine)
	addRelease(t, db, lessons, 2022, 2, 14)
	fileWorkPair(t, db, love, lessons, VerdictSame, "hash-love")

	// the same alias shape with dates a year apart is a sequel's shape
	early := createLiveWork(t, db, medium, "Alias Agreement Original")
	addTitle(t, db, early, "", "Alias Agreement Sequel", model.WorkTitleKindAlias, model.WorkTitleProvenanceSource)
	addRelease(t, db, early, 2020, 1, 10)
	late := createLiveWork(t, db, medium, "Alias Agreement Sequel")
	addRelease(t, db, late, 2021, 3, 10)
	fileWorkPair(t, db, early, late, VerdictSame, "hash-far")

	counts := runWorkPairApply(t, db)
	assert.Equal(t, 1, counts["applied_"+applyAccept], "counts: %v", counts)
	assert.Equal(t, 2, counts["applied_"+stampHeldSameUncorroborated], "counts: %v", counts)
	assert.Equal(t, model.CandidateStatusAccepted, pairStatus(t, db, dc, mint))
	assert.Equal(t, model.CandidateStatusNeedsManual, pairStatus(t, db, love, lessons))
	assert.Equal(t, model.CandidateStatusNeedsManual, pairStatus(t, db, early, late))

	var note string
	require.NoError(t, db.Raw(`SELECT note FROM catalog_merge_proposal WHERE entity_type = ?`, model.EntityTypeWork).Scan(&note).Error)
	assert.Contains(t, note, `sole holders of title "d.c.5to4luminousmemories"`)
}

func TestDatedTitleNeedsExactlyTwoHolders(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "Crowded Original Title")
	addTitle(t, db, a, "", "Crowded Shared Alias", model.WorkTitleKindAlias, model.WorkTitleProvenanceSource)
	addRelease(t, db, a, 2024, 5, 1)
	b := createLiveWork(t, db, medium, "Crowded Shared Alias")
	addRelease(t, db, b, 2024, 5, 1)
	createLiveWork(t, db, medium, "Crowded Shared Alias")
	fileWorkPair(t, db, a, b, VerdictSame, "hash-crowded-alias")

	counts := runWorkPairApply(t, db)
	assert.Equal(t, 1, counts["applied_"+stampHeldSameUncorroborated], "counts: %v", counts)
}

func TestDatedStoreIDCorroboratesAcrossGrains(t *testing.T) {
	db, medium := setupWorkPairDB(t)

	// 6253: VNDB lists dmm hobc_0507 on its release. 217311: EG anchors it on
	// the work, and Bangumi dates the subject the same day.
	vndb := createLiveWork(t, db, medium, "面影レイルバック・エピソード捌　泥亀の月")
	rel := addRelease(t, db, vndb, 2017, 5, 2)
	addRef(t, db, model.EntityTypeRelease, rel, "dmm", "hobc_0507", model.LinkKindExact)
	bgm := createLiveWork(t, db, medium, "泥亀の月")
	addRef(t, db, model.EntityTypeWork, bgm, "dmm", "hobc_0507", model.LinkKindExact)
	addRef(t, db, model.EntityTypeWork, bgm, "bangumi", "226725", model.LinkKindExact)
	require.NoError(t, db.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, parser_version, ingested_at)
		VALUES (226725, 4, '泥亀の月', '', '', '', 4001, '', false, '2017-05-02', false, 0, 0, 'v', now())
		ON CONFLICT (id) DO UPDATE SET date = EXCLUDED.date`).Error)
	fileWorkPair(t, db, vndb, bgm, VerdictSame, "hash-doroki")

	// a DLC set released a year later shares its base game's store id
	base := createLiveWork(t, db, medium, "Store Shared Base")
	baseRel := addRelease(t, db, base, 2018, 6, 1)
	addRef(t, db, model.EntityTypeRelease, baseRel, "dmm", "base_0001", model.LinkKindExact)
	dlc := createLiveWork(t, db, medium, "Store Shared Base DLC Set")
	addRelease(t, db, dlc, 2019, 6, 1)
	addRef(t, db, model.EntityTypeWork, dlc, "dmm", "base_0001", model.LinkKindExact)
	fileWorkPair(t, db, base, dlc, VerdictSame, "hash-dlc")

	// both at the work grain is the two-EG-editions shape and proves nothing
	ed1 := createLiveWork(t, db, medium, "Same Grain Edition One")
	addRelease(t, db, ed1, 2015, 3, 1)
	addRef(t, db, model.EntityTypeWork, ed1, "dmm", "grain_0001", model.LinkKindExact)
	ed2 := createLiveWork(t, db, medium, "Same Grain Edition Two")
	addRelease(t, db, ed2, 2015, 3, 1)
	addRef(t, db, model.EntityTypeWork, ed2, "dmm", "grain_0001", model.LinkKindProbable)
	fileWorkPair(t, db, ed1, ed2, VerdictSame, "hash-grain")

	counts := runWorkPairApply(t, db)
	assert.Equal(t, 1, counts["applied_"+applyAccept], "counts: %v", counts)
	assert.Equal(t, model.CandidateStatusAccepted, pairStatus(t, db, vndb, bgm))
	assert.Equal(t, model.CandidateStatusNeedsManual, pairStatus(t, db, base, dlc))
	assert.Equal(t, model.CandidateStatusNeedsManual, pairStatus(t, db, ed1, ed2))
}
