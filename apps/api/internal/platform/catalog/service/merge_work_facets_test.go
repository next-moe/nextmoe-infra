package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeWorkCarriesEveryFacet(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := createWork(t, "同じ作品")
	source := createWork(t, "同じ 作品")

	engineA := &model.CatalogEngine{Name: "KiriKiri", Description: "", Aliases: []byte(`[]`)}
	engineB := &model.CatalogEngine{Name: "Ren'Py", Description: "", Aliases: []byte(`[]`)}
	require.NoError(t, testDB.Create(engineA).Error)
	require.NoError(t, testDB.Create(engineB).Error)
	seriesA := &model.CatalogSeries{DisplayName: "series A", SourceID: 2, ExternalID: "SRI-A"}
	seriesB := &model.CatalogSeries{DisplayName: "series B", SourceID: 2, ExternalID: "SRI-B"}
	require.NoError(t, testDB.Create(seriesA).Error)
	require.NoError(t, testDB.Create(seriesB).Error)
	labelA := &model.CatalogLabel{DisplayName: "label A"}
	labelB := &model.CatalogLabel{DisplayName: "label B"}
	require.NoError(t, testDB.Create(labelA).Error)
	require.NoError(t, testDB.Create(labelB).Error)
	charA := createCharacter(t, "主人公")
	charB := createCharacter(t, "ヒロイン")

	create := func(rows ...any) {
		t.Helper()
		for _, r := range rows {
			require.NoError(t, testDB.Create(r).Error)
		}
	}

	create(
		&model.CatalogWorkIntro{WorkID: target.ID, Lang: "ja", SourceID: 2, Intro: "target ja"},
		&model.CatalogWorkIntro{WorkID: source.ID, Lang: "ja", SourceID: 2, Intro: "loser ja"},
		&model.CatalogWorkIntro{WorkID: source.ID, Lang: "en", SourceID: 2, Intro: "source en"},
	)
	create(
		&model.CatalogWorkCover{WorkID: target.ID, ImageHash: "hash-A", SortOrder: 0, SourceID: 2},
		&model.CatalogWorkCover{WorkID: source.ID, ImageHash: "hash-A", SortOrder: 9, SourceID: 3},
		&model.CatalogWorkCover{WorkID: source.ID, ImageHash: "hash-B", SortOrder: 1, SourceID: 3},
		&model.CatalogWorkScreenshot{WorkID: target.ID, ImageHash: "shot-A", SourceID: 2},
		&model.CatalogWorkScreenshot{WorkID: source.ID, ImageHash: "shot-A", SourceID: 3},
		&model.CatalogWorkScreenshot{WorkID: source.ID, ImageHash: "shot-B", SourceID: 3},
	)
	create(
		&model.CatalogWorkRating{WorkID: target.ID, SourceID: 2, Score: 7},
		&model.CatalogWorkRating{WorkID: source.ID, SourceID: 2, Score: 1},
		&model.CatalogWorkRating{WorkID: source.ID, SourceID: 3, Score: 8},
		&model.CatalogWorkPlaytime{WorkID: target.ID, SourceID: 2, Minutes: 600},
		&model.CatalogWorkPlaytime{WorkID: source.ID, SourceID: 2, Minutes: 60},
		&model.CatalogWorkPlaytime{WorkID: source.ID, SourceID: 3, Minutes: 900},
	)
	create(
		&model.CatalogWorkTag{WorkID: target.ID, Name: "百合", SourceID: 3, Count: 10},
		&model.CatalogWorkTag{WorkID: source.ID, Name: "百合", SourceID: 3, Count: 1},
		&model.CatalogWorkTag{WorkID: source.ID, Name: "拔作", SourceID: 3, Count: 5},
	)
	create(
		&model.CatalogWorkPopularity{WorkID: target.ID, SourceID: 2, Metric: 0, Value: 100},
		&model.CatalogWorkPopularity{WorkID: source.ID, SourceID: 2, Metric: 0, Value: 1},
		&model.CatalogWorkPopularity{WorkID: source.ID, SourceID: 2, Metric: 1, Value: 42},
	)
	create(
		&model.CatalogWorkPlatform{WorkID: target.ID, Platform: "win", SourceID: 3},
		&model.CatalogWorkPlatform{WorkID: source.ID, Platform: "win", SourceID: 3},
		&model.CatalogWorkPlatform{WorkID: source.ID, Platform: "psv", SourceID: 3},
	)
	create(
		&model.CatalogWorkEngine{WorkID: target.ID, EngineID: engineA.ID, SourceID: 3},
		&model.CatalogWorkEngine{WorkID: source.ID, EngineID: engineA.ID, SourceID: 3},
		&model.CatalogWorkEngine{WorkID: source.ID, EngineID: engineB.ID, SourceID: 3},
	)
	create(
		&model.CatalogSeriesMember{SeriesID: seriesA.ID, WorkID: target.ID},
		&model.CatalogSeriesMember{SeriesID: seriesA.ID, WorkID: source.ID},
		&model.CatalogSeriesMember{SeriesID: seriesB.ID, WorkID: source.ID},
	)
	create(
		&model.CatalogWorkLabel{WorkID: target.ID, LabelID: labelA.ID, Kind: model.WorkLabelKindCircle},
		&model.CatalogWorkLabel{WorkID: source.ID, LabelID: labelA.ID, Kind: model.WorkLabelKindCircle},
		&model.CatalogWorkLabel{WorkID: source.ID, LabelID: labelB.ID, Kind: model.WorkLabelKindCircle},
	)
	createWorkCharacter(t, target.ID, charA.ID, model.WorkCharacterKindUnknown, model.SpoilerNone)
	createWorkCharacter(t, source.ID, charA.ID, model.WorkCharacterKindMain, model.SpoilerSevere)
	createWorkCharacter(t, source.ID, charB.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, source.ID, target.ID, 7, "wave 170b")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	for _, table := range []string{
		"catalog_work_intro", "catalog_work_cover", "catalog_work_screenshot",
		"catalog_work_rating", "catalog_work_tag", "catalog_work_popularity",
		"catalog_work_playtime", "catalog_work_platform", "catalog_work_engine",
		"catalog_series_member", "catalog_work_label", "catalog_work_character",
	} {
		var onTarget, onSource int64
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM `+table+` WHERE work_id = ?`, target.ID).Scan(&onTarget).Error)
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM `+table+` WHERE work_id = ?`, source.ID).Scan(&onSource).Error)
		assert.EqualValues(t, 2, onTarget, "%s: the source-only row moves, the colliding one drops", table)
		assert.Zero(t, onSource, "%s: no row may stay on the merged-away work", table)
	}

	var cover model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ? AND image_hash = ?", target.ID, "hash-A").First(&cover).Error)
	assert.Equal(t, 0, cover.SortOrder, "the colliding cover keeps the survivor's row")
	assert.EqualValues(t, 2, cover.SourceID)

	var rating model.CatalogWorkRating
	require.NoError(t, testDB.Where("work_id = ? AND source_id = ?", target.ID, 2).First(&rating).Error)
	assert.EqualValues(t, 7, rating.Score)

	var tag model.CatalogWorkTag
	require.NoError(t, testDB.Where("work_id = ? AND name = ?", target.ID, "百合").First(&tag).Error)
	assert.Equal(t, 10, tag.Count)

	var roster model.CatalogWorkCharacter
	require.NoError(t, testDB.Where("work_id = ? AND character_id = ?", target.ID, charA.ID).First(&roster).Error)
	assert.Equal(t, model.WorkCharacterKindMain, roster.Kind, "unknown kind upgrades to the loser's typed value")
	assert.Equal(t, model.SpoilerSevere, roster.Spoiler, "spoiler takes the higher of the two edges")
}

func TestMergeLabelAndPersonCarryIntros(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	lTarget := &model.CatalogLabel{DisplayName: "ケロQ"}
	lSource := &model.CatalogLabel{DisplayName: "keroQ"}
	require.NoError(t, testDB.Create(lTarget).Error)
	require.NoError(t, testDB.Create(lSource).Error)
	require.NoError(t, testDB.Create(&model.CatalogLabelIntro{
		LabelID: lTarget.ID, Lang: "ja", SourceID: 2, Intro: "target ja"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogLabelIntro{
		LabelID: lSource.ID, Lang: "ja", SourceID: 2, Intro: "loser ja"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogLabelIntro{
		LabelID: lSource.ID, Lang: "en", SourceID: 2, Intro: "source en"}).Error)

	lp, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, lSource.ID, lTarget.ID, 7, "wave 170b")
	require.NoError(t, err)
	approveAndForceExecutable(t, lp.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, lp.ID, nil))

	var labelIntros []model.CatalogLabelIntro
	require.NoError(t, testDB.Where("label_id = ?", lTarget.ID).Order("lang").Find(&labelIntros).Error)
	require.Len(t, labelIntros, 2)
	assert.Equal(t, "source en", labelIntros[0].Intro, "the source-only language moves")
	assert.Equal(t, "target ja", labelIntros[1].Intro, "the colliding key keeps the survivor's text")
	var strandedLabel int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_label_intro WHERE label_id = ?`, lSource.ID).
		Scan(&strandedLabel).Error)
	assert.Zero(t, strandedLabel)

	pTarget := createPerson(t, "田中ロミオ")
	pSource := createPerson(t, "田中 ロミオ")
	require.NoError(t, testDB.Create(&model.CatalogPersonIntro{
		PersonID: pTarget.ID, Lang: "ja", SourceID: 2, Intro: "target ja"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogPersonIntro{
		PersonID: pSource.ID, Lang: "ja", SourceID: 2, Intro: "loser ja"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogPersonIntro{
		PersonID: pSource.ID, Lang: "en", SourceID: 2, Intro: "source en"}).Error)

	pp, err := testMerge.ProposeMerge(ctx, model.EntityTypePerson, pSource.ID, pTarget.ID, 7, "wave 170b")
	require.NoError(t, err)
	approveAndForceExecutable(t, pp.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, pp.ID, nil))

	var personIntros []model.CatalogPersonIntro
	require.NoError(t, testDB.Where("person_id = ?", pTarget.ID).Order("lang").Find(&personIntros).Error)
	require.Len(t, personIntros, 2)
	assert.Equal(t, "source en", personIntros[0].Intro)
	assert.Equal(t, "target ja", personIntros[1].Intro)
	var strandedPerson int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_person_intro WHERE person_id = ?`, pSource.ID).
		Scan(&strandedPerson).Error)
	assert.Zero(t, strandedPerson)
}

func TestMergeEntityRelationRehang(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := &model.CatalogLabel{DisplayName: "survivor"}
	source := &model.CatalogLabel{DisplayName: "loser"}
	other := &model.CatalogLabel{DisplayName: "third party"}
	dup := &model.CatalogLabel{DisplayName: "already related"}
	for _, l := range []*model.CatalogLabel{target, source, other, dup} {
		require.NoError(t, testDB.Create(l).Error)
	}
	relType := seededEntityRelationTypeID(t)

	mkEdge := func(a, b int64) {
		t.Helper()
		require.NoError(t, testDB.Create(&model.CatalogEntityRelation{
			EntityType: model.EntityTypeLabel, AID: a, BID: b, RelationTypeID: relType}).Error)
	}
	mkEdge(source.ID, target.ID)
	mkEdge(source.ID, other.ID)
	mkEdge(other.ID, source.ID)
	mkEdge(target.ID, dup.ID)
	mkEdge(source.ID, dup.ID)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, source.ID, target.ID, 7, "wave 170b")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var edges []model.CatalogEntityRelation
	require.NoError(t, testDB.Where("a_id = ? OR b_id = ?", target.ID, target.ID).
		Order("a_id, b_id").Find(&edges).Error)
	require.Len(t, edges, 3, "two moved edges plus the survivor's own, self-edge and duplicate dropped")

	var stranded int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM catalog_entity_relation WHERE a_id = ? OR b_id = ?`, source.ID, source.ID).
		Scan(&stranded).Error)
	assert.Zero(t, stranded, "no edge may stay on the merged-away label")
}

func seededEntityRelationTypeID(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_relation_type ORDER BY id LIMIT 1`).Scan(&id).Error)
	require.NotZero(t, id, "the seeds must provide at least one relation type")
	return id
}

// TestMergeWorkAxisKeepsHumanRoster is TestMergeKeepsHumanRosterKind on the
// other axis. The two survivorship statements are copies of each other and the
// work axis has only run 28 times in production, but catalog_work_cover's 12
// stranded rows came from exactly that reasoning.
func TestMergeWorkAxisKeepsHumanRoster(t *testing.T) {
	cleanTables(t)
	target, source := createWork(t, "同じ作品"), createWork(t, "同じ 作品")
	ch := createCharacter(t, "主人公")
	createWorkCharacter(t, target.ID, ch.ID, model.WorkCharacterKindUnknown, model.SpoilerNone)
	createWorkCharacter(t, source.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerSevere)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_character SET field_provenance =
		   '{"kind":[{"source":"curated","at":"2026-08-16T00:00:00Z"}],
		     "spoiler":[{"source":"user","at":"2026-08-16T00:00:00Z"}]}'::jsonb
		 WHERE work_id = ? AND character_id = ?`, target.ID, ch.ID).Error)

	executeMerge(t, model.EntityTypeWork, source.ID, target.ID, "same work")

	var edge model.CatalogWorkCharacter
	require.NoError(t, testDB.Where("work_id = ? AND character_id = ?", target.ID, ch.ID).First(&edge).Error)
	assert.Equal(t, model.WorkCharacterKindUnknown, edge.Kind, "a human 0 survives the kind upgrade")
	assert.EqualValues(t, model.SpoilerNone, edge.Spoiler, "a human 0 survives GREATEST")
}

func TestExecuteMergeReleasesQuarantinedTarget(t *testing.T) {
	cleanTables(t)
	target := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusQuarantine, "隔離先")
	source := createWork(t, "隔離元")
	executeMerge(t, model.EntityTypeWork, source.ID, target.ID, "quarantine target")

	var got model.CatalogWork
	require.NoError(t, testDB.First(&got, target.ID).Error)
	assert.Equal(t, model.WorkStatusLive, got.Status)
}
