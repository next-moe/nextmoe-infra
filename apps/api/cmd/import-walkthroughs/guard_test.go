package main

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchWorksTargetsOnlyGalgame(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	asmr := mkWork(t, db, "asmr", "ハミダシ音声作品")
	mkDLsiteRef(t, db, asmr, "RJ01000001")
	game := mkWork(t, db, "galgame", "ハミダシクリエイティブ")
	mkWork(t, db, "anime", "姉☆孕みっくす")
	coded := mkWork(t, db, "galgame", "コード一致作品")
	mkDLsiteRef(t, db, coded, "RJ01000002")

	got, err := matchWorks(ctx, db, []matchSource{
		{EntryID: 1, Title: "ハミダシクリエイティブ", DlsiteID: "rj01000001"},
		{EntryID: 2, Title: "姉☆孕みっくす"},
		{EntryID: 3, Title: "別名", DlsiteID: "RJ01000002"},
	})
	require.NoError(t, err)

	require.NotNil(t, got[1].WorkID, "a DLsite code on the tie-in ASMR falls through to the galgame title")
	assert.Equal(t, game, *got[1].WorkID)
	assert.Equal(t, matchTitle, got[1].Method)
	assert.Nil(t, got[2].WorkID, "a title only an anime carries matches nothing")
	assert.Equal(t, matchNone, got[2].Method)
	require.NotNil(t, got[3].WorkID)
	assert.Equal(t, coded, *got[3].WorkID)
	assert.Equal(t, matchDLsite, got[3].Method)
}

func TestMatchWorksPrefersAUniqueExactTitleOverASiblingsCode(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	original := mkWork(t, db, "galgame", "淫獣艦獄～DIRTY PRISON SHIP～")
	mkDLsiteRef(t, db, original, "RJ02000001")
	remake := mkWork(t, db, "galgame", "淫獣艦獄：RE")
	same := mkWork(t, db, "galgame", "同じ作品")
	mkDLsiteRef(t, db, same, "RJ02000002")
	mkWork(t, db, "galgame", "同名作品")
	mkWork(t, db, "galgame", "同名作品")
	shared := mkWork(t, db, "galgame", "コードの作品")
	mkDLsiteRef(t, db, shared, "RJ02000003")

	got, err := matchWorks(ctx, db, []matchSource{
		{EntryID: 1, Title: "淫獣艦獄：RE", DlsiteID: "RJ02000001"},
		{EntryID: 2, Title: "同じ作品", DlsiteID: "RJ02000002"},
		{EntryID: 3, Title: "同名作品", DlsiteID: "RJ02000003"},
	})
	require.NoError(t, err)

	require.NotNil(t, got[1].WorkID)
	assert.Equal(t, remake, *got[1].WorkID, "the sibling's code loses to the unique exact title")
	assert.Equal(t, matchTitle, got[1].Method)
	require.NotNil(t, got[2].WorkID)
	assert.Equal(t, same, *got[2].WorkID)
	assert.Equal(t, matchDLsite, got[2].Method, "a code that agrees with the title keeps its lane")
	require.NotNil(t, got[3].WorkID)
	assert.Equal(t, shared, *got[3].WorkID, "an ambiguous title leaves the code in charge")
	assert.Equal(t, matchDLsite, got[3].Method)
}

func TestApplyRefusesAStagedMatchOffGalgame(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	anime := mkWork(t, db, "anime", "アニメ版")
	game := mkWork(t, db, "galgame", "ゲーム版")
	c := chartOf(walkthrough.Character{Key: "a", Name: "芹香", Aliases: []string{}})
	stageEntry(t, db, 1, anime, c, map[string]characterPin{})
	stageEntry(t, db, 2, game, c, map[string]characterPin{})
	entries := []sourceEntry{
		{ID: 1, Site: "seiya", SourceURL: "https://seiya-saiga.com/game/x/anime.html"},
		{ID: 2, Site: "seiya", SourceURL: "https://seiya-saiga.com/game/x/game.html"},
	}

	require.NoError(t, runApply(ctx, db, db, entries, true))

	var rows []model.CatalogWalkthrough
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, game, rows[0].WorkID)
	assert.Equal(t, entries[1].SourceURL, rows[0].SourceURL)
}

func TestApplyKeepsTheLinksOfAWorkAPersonChose(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	staged := mkWork(t, db, "galgame", "まいてつ Last Run!!")
	held := mkWork(t, db, "galgame", "まいてつ")
	stagedChar := mkCharacter(t, db, staged, "ハチロク", "bangumi")
	heldChar := mkCharacter(t, db, held, "ハチロク", "bangumi")
	url := "https://seiya-saiga.com/game/lose/maitetsu.html"
	c := chartOf(walkthrough.Character{Key: "a", Name: "ハチロク", Aliases: []string{}})
	wt := mkWalkthrough(t, db, held, url, c, `{"work_id":[{"source":"curated","at":"2026-11-21T00:00:00Z"}]}`)
	mkLink(t, db, wt, "a", heldChar, ruleSameName)
	stageEntry(t, db, 1, staged, c, map[string]characterPin{"a": {CharacterID: stagedChar, Rule: ruleSameName}})

	require.NoError(t, runApply(ctx, db, db, []sourceEntry{{ID: 1, Site: "seiya", SourceURL: url}}, true))

	var got model.CatalogWalkthrough
	require.NoError(t, db.Take(&got, wt).Error)
	assert.Equal(t, held, got.WorkID)
	assert.Equal(t, heldChar, linksOf(t, db, wt)["a"].CharacterID, "pins staged against another work's roster are not applied")
}
