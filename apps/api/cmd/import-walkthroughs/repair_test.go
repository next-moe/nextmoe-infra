package main

import (
	"context"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/provenance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestParseRepairPlan(t *testing.T) {
	plan, err := parseRepairPlan(strings.NewReader(
		"walkthrough_id\twork_id\ttarget\tbucket\n41\t213401\t8733\ta\tadaptation_of\r\n\n1125\t183529\thide\tb\n"))
	require.NoError(t, err)
	assert.Equal(t, []repairRow{{41, 213401, 8733}, {1125, 183529, 0}}, plan)

	for name, in := range map[string]string{
		"empty target":   "41\t213401\t\n",
		"two columns":    "41\t213401\n",
		"planned twice":  "41\t213401\t8733\n41\t213401\t8734\n",
		"bad id":         "x41\t213401\t8733\n",
		"zero target":    "41\t213401\t0\n",
		"header not 1st": "41\t213401\t8733\nwalkthrough_id\twork_id\ttarget\n",
	} {
		_, err := parseRepairPlan(strings.NewReader(in))
		assert.Error(t, err, name)
	}
}

func TestRepairAction(t *testing.T) {
	galgame := map[int64]bool{10: true}
	on := func(work int64, medium string) *repairCurrent {
		return &repairCurrent{WorkID: work, Medium: medium, FieldProvenance: "{}"}
	}
	human := on(1, "anime")
	human.FieldProvenance = `{"pins":[{"source":"user","at":"2026-11-01T00:00:00Z"}]}`
	machine := on(1, "anime")
	machine.FieldProvenance = `{"chart":[{"source":"seiya","at":"2026-11-01T00:00:00Z"}]}`
	hidden := on(1, "asmr")
	hidden.Status = model.WalkthroughStatusHidden

	for _, tc := range []struct {
		row  repairRow
		cur  *repairCurrent
		want string
	}{
		{repairRow{1, 1, 10}, nil, repairMissing},
		{repairRow{1, 1, 10}, on(10, "galgame"), repairUnchanged},
		{repairRow{1, 1, 0}, hidden, repairUnchanged},
		{repairRow{1, 1, 10}, on(2, "anime"), repairMoved},
		{repairRow{1, 1, 10}, on(1, "galgame"), repairRepoint},
		{repairRow{1, 1, 10}, human, repairHuman},
		{repairRow{1, 1, 10}, machine, repairRepoint},
		{repairRow{1, 1, 0}, on(1, "asmr"), repairHide},
		{repairRow{1, 1, 11}, on(1, "anime"), repairBadTarget},
		{repairRow{1, 1, 10}, on(1, "anime"), repairRepoint},
	} {
		assert.Equal(t, tc.want, repairAction(tc.row, tc.cur, galgame), "%+v on %+v", tc.row, tc.cur)
	}
}

type repairFixture struct {
	anime, game, asmr, other          int64
	animeChar, placeholder, gameChar  int64
	stray, hidden, moved, human, wide int64
	url                               string
}

func seedStrays(t *testing.T, db *gorm.DB) repairFixture {
	t.Helper()
	f := repairFixture{url: "https://seiya-saiga.com/game/lilith/aneharamix.html"}
	f.anime = mkWork(t, db, "anime", "姉☆孕みっくす")
	f.game = mkWork(t, db, "galgame", "姉☆孕みっくす～姉＆死神お姉さんと一週間～")
	f.asmr = mkWork(t, db, "asmr", "ハピメア音声作品")
	f.other = mkWork(t, db, "anime", "別のアニメ")
	f.animeChar = mkCharacter(t, db, f.anime, "芹香", "bangumi")
	f.gameChar = mkCharacter(t, db, f.game, "葛城 芹香", "bangumi")

	c := chartOf(
		walkthrough.Character{Key: "a", Name: "芹香", Aliases: []string{}},
		walkthrough.Character{Key: "b", Name: "モブ子", Aliases: []string{}},
	)
	f.stray = mkWalkthrough(t, db, f.anime, f.url, c, `{}`)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		f.placeholder, err = createNameOnlyCharacter(tx, f.anime, model.SourceSeiya, c.Characters[1])
		return err
	}))
	mkLink(t, db, f.stray, "a", f.animeChar, ruleSameName)
	mkLink(t, db, f.stray, "b", f.placeholder, matchedByWalkthrough)

	empty := chartOf()
	f.hidden = mkWalkthrough(t, db, f.asmr, "https://seiya-saiga.com/game/p/happymare.html", empty, `{}`)
	f.moved = mkWalkthrough(t, db, f.other, "https://seiya-saiga.com/game/p/moved.html", empty, `{}`)
	f.human = mkWalkthrough(t, db, f.asmr, "https://seiya-saiga.com/game/p/human.html", empty,
		`{"work_id":[{"source":"user","at":"2026-11-01T00:00:00Z"}]}`)
	f.wide = mkWalkthrough(t, db, f.anime, "https://seiya-saiga.com/game/p/wide.html", empty, `{}`)
	return f
}

func (f repairFixture) plan() []repairRow {
	return []repairRow{
		{f.stray, f.anime, f.game},
		{f.hidden, f.asmr, 0},
		{f.moved, f.anime, f.game},
		{f.human, f.asmr, f.game},
		{f.wide, f.anime, f.asmr},
	}
}

func TestRepairDryRunWritesNothing(t *testing.T) {
	db := freshDB(t)
	f := seedStrays(t, db)
	var before []model.CatalogWalkthrough
	require.NoError(t, db.Order("id").Find(&before).Error)

	require.NoError(t, runRepair(context.Background(), db, f.plan(), false))

	var after []model.CatalogWalkthrough
	require.NoError(t, db.Order("id").Find(&after).Error)
	assert.Equal(t, before, after)
	assert.Equal(t, f.animeChar, linksOf(t, db, f.stray)["a"].CharacterID)
	var characters int64
	require.NoError(t, db.Model(&model.CatalogCharacter{}).Where("deleted_at IS NULL").Count(&characters).Error)
	assert.EqualValues(t, 3, characters)
}

func TestRepairMovesStraysToTheirGalgame(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	f := seedStrays(t, db)

	require.NoError(t, runRepair(ctx, db, f.plan(), true))

	var stray model.CatalogWalkthrough
	require.NoError(t, db.Take(&stray, f.stray).Error)
	assert.Equal(t, f.game, stray.WorkID)
	assert.Equal(t, provenance.SourceCurated, provenance.FirstSource(stray.FieldProvenance, "work_id"))
	assert.Equal(t, model.WalkthroughStatusLive, stray.Status)

	links := linksOf(t, db, f.stray)
	require.Len(t, links, 2)
	assert.Equal(t, f.gameChar, links["a"].CharacterID, "芹香 re-pins to the end of 葛城 芹香 on the galgame roster")
	assert.Equal(t, ruleNamePart, links["a"].MatchedBy)
	assert.NotEqual(t, f.placeholder, links["b"].CharacterID)
	assert.Equal(t, matchedByWalkthrough, links["b"].MatchedBy)
	var edge model.CatalogWorkCharacter
	require.NoError(t, db.Where("character_id = ?", links["b"].CharacterID).Take(&edge).Error)
	assert.Equal(t, f.game, edge.WorkID, "the new name-only character sits on the galgame roster")

	var retired model.CatalogCharacter
	require.NoError(t, db.Unscoped().Take(&retired, f.placeholder).Error)
	assert.True(t, retired.DeletedAt.Valid, "the anime's orphaned placeholder is retired")
	var animeChar model.CatalogCharacter
	require.NoError(t, db.Unscoped().Take(&animeChar, f.animeChar).Error)
	assert.False(t, animeChar.DeletedAt.Valid, "a real character is never retired")

	var hidden model.CatalogWalkthrough
	require.NoError(t, db.Take(&hidden, f.hidden).Error)
	assert.Equal(t, model.WalkthroughStatusHidden, hidden.Status)
	assert.Equal(t, f.asmr, hidden.WorkID)
	assert.Equal(t, provenance.SourceCurated, provenance.FirstSource(hidden.FieldProvenance, "status"))

	for id, work := range map[int64]int64{f.moved: f.other, f.human: f.asmr, f.wide: f.anime} {
		var w model.CatalogWalkthrough
		require.NoError(t, db.Take(&w, id).Error)
		assert.Equal(t, work, w.WorkID, "walkthrough %d", id)
		assert.Equal(t, model.WalkthroughStatusLive, w.Status, "walkthrough %d", id)
	}

	var characters int64
	require.NoError(t, db.Model(&model.CatalogCharacter{}).Where("deleted_at IS NULL").Count(&characters).Error)
	require.NoError(t, runRepair(ctx, db, f.plan(), true))
	var again int64
	require.NoError(t, db.Model(&model.CatalogCharacter{}).Where("deleted_at IS NULL").Count(&again).Error)
	assert.Equal(t, characters, again, "a second run is all unchanged")
	assert.Equal(t, links, linksOf(t, db, f.stray))

	c := chartOf(
		walkthrough.Character{Key: "a", Name: "芹香", Aliases: []string{}},
		walkthrough.Character{Key: "b", Name: "モブ子", Aliases: []string{}},
	)
	stageEntry(t, db, 1, f.anime, c, map[string]characterPin{"a": {CharacterID: f.animeChar, Rule: ruleSameName}})
	require.NoError(t, runApply(ctx, db, db, []sourceEntry{{ID: 1, Site: "seiya", SourceURL: f.url}}, true))
	require.NoError(t, db.Take(&stray, f.stray).Error)
	assert.Equal(t, f.game, stray.WorkID, "re-applying the stale staged match does not move it back")
	assert.Equal(t, links, linksOf(t, db, f.stray))
}
