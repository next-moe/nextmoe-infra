package editspec_test

import (
	"strings"
	"testing"

	"api/internal/jobs/seriesorder"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

const sequelOfTypeID = 2

func relationCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_work_relation r WHERE `+where, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWorkRelationsRoundTripFromBothEnds(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "前作")
	b := createWork(t, "続編")

	rel := []any{map[string]any{"work_id": a.ID, "relation": "sequel_of"}}
	snap := mergeField(t, e, b.ID, editspec.FieldWorkRelations, rel)
	sameJSON(t, "relations on b", snap[editspec.FieldWorkRelations], rel)
	if n := relationCount(t, "r.a_work_id = ? AND r.b_work_id = ? AND r.relation_type_id = ? AND r.source_id = ?",
		b.ID, a.ID, sequelOfTypeID, curatedSource); n != 1 {
		t.Fatalf("the edge must be stored once from the work that is the sequel, got %d rows", n)
	}

	fromA, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "relations on a", fromA[editspec.FieldWorkRelations],
		[]any{map[string]any{"work_id": b.ID, "relation": "sequel_of", "reverse": true}})

	// A save from a that echoes b's edge and adds one of its own keeps both.
	c := createWork(t, "FD")
	mergeField(t, e, a.ID, editspec.FieldWorkRelations, append(fromA[editspec.FieldWorkRelations].([]any),
		map[string]any{"work_id": c.ID, "relation": "fandisc_of", "reverse": true}))
	if n := relationCount(t, "r.source_id = ?", curatedSource); n != 2 {
		t.Fatalf("an echo from the other end must keep the edge, got %d curated rows", n)
	}
}

func TestWorkRelationsSymmetricEdgeStoredOnceSmallerFirst(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "一")
	b := createWork(t, "二")
	mergeField(t, e, b.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": a.ID, "relation": "same_series", "reverse": true}})
	if n := relationCount(t, "r.a_work_id = ? AND r.b_work_id = ?", a.ID, b.ID); n != 1 {
		t.Fatalf("a symmetric edge is stored smaller id first, got %d", n)
	}
	fromA, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "symmetric from a", fromA[editspec.FieldWorkRelations],
		[]any{map[string]any{"work_id": b.ID, "relation": "same_series"}})
}

func TestWorkRelationsUpstreamCollisionAndSelfRejected(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "上流")
	b := createWork(t, "上流2")
	src := vndbSource
	if err := testDB.Create(&model.CatalogWorkRelation{
		AWorkID: b.ID, BWorkID: a.ID, RelationTypeID: sequelOfTypeID, SourceID: &src,
	}).Error; err != nil {
		t.Fatal(err)
	}
	valErr := mergeMustReject(t, e, editspec.TypeWork, b.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": a.ID, "relation": "sequel_of"}})
	if !strings.Contains(valErr.Reason, "already provided upstream") {
		t.Fatalf("got %q", valErr.Reason)
	}
	mergeMustReject(t, e, editspec.TypeWork, b.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": b.ID, "relation": "sequel_of"}})
}

func TestWorkRelationSuppressionHidesTheEdgeFromBothEnds(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "隠す前作")
	b := createWork(t, "隠す続編")
	src := vndbSource
	edge := model.CatalogWorkRelation{AWorkID: b.ID, BWorkID: a.ID, RelationTypeID: sequelOfTypeID, SourceID: &src}
	if err := testDB.Create(&edge).Error; err != nil {
		t.Fatal(err)
	}
	visible := func() int64 {
		return relationCount(t, "r.a_work_id = ? AND r.b_work_id = ? AND "+editspec.NotSuppressedWorkRelationSQL("r"), b.ID, a.ID)
	}
	if visible() != 1 {
		t.Fatal("the upstream edge starts visible")
	}
	// Hidden from a's side: on a the edge is reversed (a is its b end).
	key := editspec.WorkRelationIdentity(sequelOfTypeID, true, b.ID)
	mergeField(t, e, a.ID, editspec.FieldWorkRelationsSuppr, []any{key})
	if visible() != 0 {
		t.Fatal("a suppression made on either end must hide the edge")
	}
	// The importers only insert with ON CONFLICT DO NOTHING; a re-run stays hidden.
	if err := testDB.Exec(`INSERT INTO catalog_work_relation (a_work_id, b_work_id, relation_type_id, source_id, created_at)
		VALUES (?, ?, ?, ?, now()) ON CONFLICT DO NOTHING`, b.ID, a.ID, sequelOfTypeID, vndbSource).Error; err != nil {
		t.Fatal(err)
	}
	if visible() != 0 {
		t.Fatal("an importer re-run must not resurrect a hidden edge")
	}
	mergeMustReject(t, e, editspec.TypeWork, a.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": b.ID, "relation": "sequel_of", "reverse": true}})
}

func TestCuratedRelationCannotBeSuppressed(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "人手前作")
	b := createWork(t, "人手続編")
	mergeField(t, e, b.ID, editspec.FieldWorkRelations, []any{map[string]any{"work_id": a.ID, "relation": "sequel_of"}})
	mergeMustReject(t, e, editspec.TypeWork, b.ID, editspec.FieldWorkRelationsSuppr,
		[]any{editspec.WorkRelationIdentity(sequelOfTypeID, false, a.ID)})
}

func TestWorkPlatformsCuratedLaneAndSuppression(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "機種")
	if err := testDB.Create(&model.CatalogWorkPlatform{WorkID: w.ID, Platform: "win", SourceID: 3}).Error; err != nil {
		t.Fatal(err)
	}
	snap := mergeField(t, e, w.ID, editspec.FieldWorkPlatforms, []any{"ps4"})
	sameJSON(t, "platforms", snap[editspec.FieldWorkPlatforms], []any{"ps4"})

	mergeField(t, e, w.ID, editspec.FieldWorkPlatformsSuppr, []any{editspec.WorkPlatformIdentity("win")})
	var shown []string
	if err := testDB.Raw(`SELECT wp.platform FROM catalog_work_platform wp WHERE wp.work_id = ? AND `+
		editspec.NotSuppressedWorkPlatformSQL("wp.work_id", "wp.platform")+` ORDER BY wp.platform`, w.ID).Scan(&shown).Error; err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "visible platforms", shown, []string{"ps4"})

	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkPlatforms, []any{"zz9"})
	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkPlatforms, []any{"ps4", "win"})
}

func createSeries(t *testing.T, sourceID int16, externalID string) *model.CatalogSeries {
	t.Helper()
	s := &model.CatalogSeries{DisplayName: "シリーズ " + externalID, SourceID: sourceID, ExternalID: externalID}
	if err := testDB.Create(s).Error; err != nil {
		t.Fatalf("create series: %v", err)
	}
	return s
}

func memberRow(t *testing.T, seriesID, workID int64) model.CatalogSeriesMember {
	t.Helper()
	var m model.CatalogSeriesMember
	if err := testDB.Where("series_id = ? AND work_id = ?", seriesID, workID).First(&m).Error; err != nil {
		t.Fatalf("membership %d/%d: %v", seriesID, workID, err)
	}
	return m
}

func TestSeriesMembersPlacementSurvivesSeriesOrder(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "外伝")
	s := createSeries(t, vndbSource, "s1")
	if err := testDB.Create(&model.CatalogSeriesMember{SeriesID: s.ID, WorkID: w.ID, Position: 1, Kind: model.SeriesMemberKindMain}).Error; err != nil {
		t.Fatal(err)
	}
	placed := []any{map[string]any{"series_id": s.ID, "position": int64(3), "kind": int64(model.SeriesMemberKindSideStory)}}
	snap := mergeField(t, e, w.ID, editspec.FieldWorkSeriesMembers, placed)
	sameJSON(t, "series_members", snap[editspec.FieldWorkSeriesMembers], placed)

	have, err := seriesorder.LoadCurrent(testCtx, testDB, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seriesorder.Apply(testCtx, testDB, s.ID,
		[]seriesorder.Assignment{{WorkID: w.ID, Position: 1, Kind: model.SeriesMemberKindMain}}, have, true); err != nil {
		t.Fatal(err)
	}
	m := memberRow(t, s.ID, w.ID)
	if m.Position != 3 || m.Kind != model.SeriesMemberKindSideStory {
		t.Fatalf("seriesorder overwrote a human placement: position %d kind %d", m.Position, m.Kind)
	}

	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkSeriesMembers,
		[]any{map[string]any{"series_id": int64(999999), "position": int64(1), "kind": int64(0)}})
}

func TestSeriesIDsKeepThePlacementOfAKeptMembership(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "続く")
	s1 := createSeries(t, curatedSource, "c1")
	s2 := createSeries(t, curatedSource, "c2")
	mergeField(t, e, w.ID, editspec.FieldWorkSeriesIDs, []any{s1.ID})
	mergeField(t, e, w.ID, editspec.FieldWorkSeriesMembers,
		[]any{map[string]any{"series_id": s1.ID, "position": int64(2), "kind": int64(model.SeriesMemberKindFandisc)}})
	mergeField(t, e, w.ID, editspec.FieldWorkSeriesIDs, []any{s1.ID, s2.ID})
	if m := memberRow(t, s1.ID, w.ID); m.Position != 2 || m.Kind != model.SeriesMemberKindFandisc {
		t.Fatalf("joining a second series reset the first one's placement: %+v", m)
	}
}

func createTagOn(t *testing.T, workID int64, name string, sourceID int16) int64 {
	t.Helper()
	tag := &model.CatalogTag{Name: name}
	if err := testDB.Create(tag).Error; err != nil {
		t.Fatalf("create tag: %v", err)
	}
	for _, src := range []int16{sourceID, curatedSource} {
		if err := testDB.Create(&model.CatalogTagSourceMap{SourceID: src, SourceName: name, TagID: tag.ID}).Error; err != nil {
			t.Fatalf("map tag: %v", err)
		}
	}
	if err := testDB.Create(&model.CatalogWorkTag{WorkID: workID, Name: name, SourceID: sourceID, Count: 3}).Error; err != nil {
		t.Fatalf("work tag: %v", err)
	}
	return tag.ID
}

func TestTagSpoilersStampEveryRowOfTheTag(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "ネタバレ")
	tagID := createTagOn(t, w.ID, "記憶喪失", vndbSource)
	set := []any{map[string]any{"tag_id": tagID, "spoiler": int64(model.SpoilerSevere)}}
	snap := mergeField(t, e, w.ID, editspec.FieldWorkTagSpoilers, set)
	sameJSON(t, "tag_spoilers", snap[editspec.FieldWorkTagSpoilers], set)

	var human int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_work_tag wt WHERE wt.work_id = ? AND wt.spoiler = ? AND `+
		editspec.WorkTagSpoilerIsHumanSQL("wt"), w.ID, model.SpoilerSevere).Scan(&human).Error; err != nil {
		t.Fatal(err)
	}
	if human != 1 {
		t.Fatalf("the upstream row must carry the human stamp, got %d", human)
	}
	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkTagSpoilers,
		[]any{map[string]any{"tag_id": int64(999999), "spoiler": int64(1)}})
}

func TestTagIDsKeepTheSpoilerOfAKeptCuratedTag(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "人手タグ")
	first := createTagOn(t, w.ID, "一番", vndbSource)
	second := createTagOn(t, w.ID, "二番", vndbSource)
	mergeField(t, e, w.ID, editspec.FieldWorkTagIDs, []any{first})
	mergeField(t, e, w.ID, editspec.FieldWorkTagSpoilers,
		[]any{map[string]any{"tag_id": first, "spoiler": int64(model.SpoilerMild)}})
	mergeField(t, e, w.ID, editspec.FieldWorkTagIDs, []any{first, second})
	var spoiler int16
	if err := testDB.Raw(`SELECT spoiler FROM catalog_work_tag WHERE work_id = ? AND source_id = ? AND name = ?`,
		w.ID, curatedSource, "一番").Scan(&spoiler).Error; err != nil {
		t.Fatal(err)
	}
	if spoiler != model.SpoilerMild {
		t.Fatalf("adding a tag reset a kept curated tag's spoiler to %d", spoiler)
	}
}

func TestCreditOptionalMembersSurviveAClientThatOmitsThem(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "職員")
	name := &model.CatalogCreditName{Name: "山田"}
	if err := testDB.Create(name).Error; err != nil {
		t.Fatal(err)
	}
	rel := createReleaseOn(t, w.ID)
	label := createLabelNamed(t, "スタジオ")
	var roleID int64
	if err := testDB.Raw(`SELECT id FROM catalog_role ORDER BY id LIMIT 1`).Scan(&roleID).Error; err != nil {
		t.Fatal(err)
	}
	full := []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID,
		"spoiler": int64(1), "release_id": rel.ID, "label_id": label.ID,
	}}
	snap := mergeField(t, e, w.ID, editspec.FieldWorkCredits, full)
	sameJSON(t, "credits", snap[editspec.FieldWorkCredits], full)

	snap = mergeField(t, e, w.ID, editspec.FieldWorkCredits, []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID, "note": "改",
	}})
	sameJSON(t, "credits after an old client", snap[editspec.FieldWorkCredits], []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID, "note": "改",
		"spoiler": int64(1), "release_id": rel.ID, "label_id": label.ID,
	}})

	snap = mergeField(t, e, w.ID, editspec.FieldWorkCredits, []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID, "release_id": nil, "label_id": nil, "spoiler": nil,
	}})
	sameJSON(t, "credits cleared", snap[editspec.FieldWorkCredits], []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID,
	}})

	other := createWork(t, "別作品")
	foreign := createReleaseOn(t, other.ID)
	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkCredits, []any{map[string]any{
		"role_id": roleID, "credit_name_id": name.ID, "release_id": foreign.ID,
	}})
}

func TestRosterAddsAndRemovesCuratedCharactersOnly(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "登場")
	upstream := createCharacter(t, "上流キャラ")
	added := createCharacter(t, "追加キャラ")
	if err := testDB.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, created_at, updated_at)
		VALUES (?, ?, 1, 0, 'import:character-roster-vndb', now(), now())`, w.ID, upstream.ID).Error; err != nil {
		t.Fatal(err)
	}
	roster := []any{
		map[string]any{"character_id": upstream.ID, "kind": int64(1), "spoiler": int64(0)},
		map[string]any{"character_id": added.ID, "kind": int64(2), "spoiler": int64(1)},
	}
	mergeField(t, e, w.ID, editspec.FieldWorkRoster, roster)
	var matchedBy string
	if err := testDB.Raw(`SELECT matched_by FROM catalog_work_character WHERE work_id = ? AND character_id = ?`,
		w.ID, added.ID).Scan(&matchedBy).Error; err != nil {
		t.Fatal(err)
	}
	if matchedBy != "curated" {
		t.Fatalf("an added character is a curated row, got matched_by %q", matchedBy)
	}
	var evidence int64
	if err := testDB.Raw(`SELECT count(*) FROM catalog_work_character wc WHERE wc.work_id = ? AND `+
		editspec.MachineRosterSQL("wc"), w.ID).Scan(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if evidence != 1 {
		t.Fatalf("identity readers must see only the imported row, got %d", evidence)
	}

	mergeField(t, e, w.ID, editspec.FieldWorkRoster, []any{})
	var left []int64
	if err := testDB.Raw(`SELECT character_id FROM catalog_work_character WHERE work_id = ? ORDER BY character_id`,
		w.ID).Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "roster after removal", left, []int64{upstream.ID})
}

func TestRosterRefusesACharacterHiddenOnTheWork(t *testing.T) {
	e := newEngine(t)
	w := createWork(t, "隠れ")
	c := createCharacter(t, "隠しキャラ")
	mergeField(t, e, w.ID, editspec.FieldWorkRosterSuppr, []any{editspec.RosterIdentity(c.ID)})
	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkRoster,
		[]any{map[string]any{"character_id": c.ID, "kind": int64(1), "spoiler": int64(0)}})
	mergeMustReject(t, e, editspec.TypeWork, w.ID, editspec.FieldWorkRoster,
		[]any{map[string]any{"character_id": int64(999999), "kind": int64(1), "spoiler": int64(0)}})
}
