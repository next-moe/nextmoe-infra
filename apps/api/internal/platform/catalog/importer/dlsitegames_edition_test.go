package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackProductIsNeverMinted(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJPACK01", "PackOnlyGameTitle", "RGPACK", "3", "ADV", "", `{"is_pack_parent":true}`)
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.Population)
	assert.Equal(t, 1, st.PackProducts)
	assert.Zero(t, st.MintedGroups)
	assert.Zero(t, st.TotalGroups)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE display_name = 'PackOnlyGameTitle'`))
}

func TestEditionOfAnchoredProductAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "EditionHostGame")
	seedDLRef(t, w, "RJANC001")
	insertDL(t, "RJANC001", "EditionHostGame", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJNEW001", "EditionSiblingGame", "RG1", "3", "ADV", "", editionsJSON("RJANC001", "RJNEW001"))
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.EditionGroups)
	assert.Equal(t, w, workOfSKU(t, "RJNEW001"))
	assert.Equal(t, ruleDLsiteEdition, matchedBySKU(t, "RJNEW001"))
}

func TestLanguageEditionOfAnchoredProductAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "LangHostGame")
	seedDLRef(t, w, "RJJPN001")
	insertDL(t, "RJJPN001", "LangHostGame", "RG1", "3", "ADV", "", `{"language_editions":[{"workno":"RJJPN001","lang":"JPN"},{"workno":"RJENG001","lang":"ENG"}]}`)
	insertDL(t, "RJENG001", "LangSiblingGame", "RG1", "3", "ADV", "", `{"language_editions":[{"workno":"RJJPN001","lang":"JPN"},{"workno":"RJENG001","lang":"ENG"}]}`)
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.EditionGroups)
	assert.Equal(t, w, workOfSKU(t, "RJENG001"))
	assert.Equal(t, ruleDLsiteEdition, matchedBySKU(t, "RJENG001"))
	var lang string
	require.NoError(t, testDB.Raw(`SELECT lang FROM catalog_release WHERE work_id = ? AND id IN (
		SELECT entity_id FROM catalog_external_ref WHERE external_id = 'RJENG001' AND entity_type = 6)`, w).Scan(&lang).Error)
	assert.Equal(t, "ENG", lang)
}

func TestSplitGroupAttachesNothing(t *testing.T) {
	requireDLGamesDB(t)
	w1 := seedLiveWork(t, "SplitHostOne")
	w2 := seedLiveWork(t, "SplitHostTwo")
	seedDLRef(t, w1, "RJSP001A")
	seedDLRef(t, w2, "RJSP001B")
	insertDL(t, "RJSP001A", "SplitHostOne", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJSP001B", "SplitHostTwo", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJSP001C", "SplitUnanchoredGame", "RG1", "3", "ADV", "", editionsJSON("RJSP001A", "RJSP001B", "RJSP001C"))
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.SplitGroups)
	assert.Zero(t, st.EditionGroups)
	assert.Zero(t, workOfSKU(t, "RJSP001C"))
}

func TestNonGameTypesAreNotPopulation(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJSOU001", "VoiceOnlyTitleXX", "RG1", "3", "SOU", "", "{}")
	insertDL(t, "RJMOV001", "MovieOnlyTitleYY", "RG1", "3", "MOV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Zero(t, st.Population)
	assert.Zero(t, st.MintedGroups)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE external_id IN ('RJSOU001','RJMOV001')`))
}

func TestLimitCapsMintedGroupsNotAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w1 := seedLiveWork(t, "LimitHostOne")
	w2 := seedLiveWork(t, "LimitHostTwo")
	seedDLRef(t, w1, "RJLIMA")
	seedDLRef(t, w2, "RJLIMB")
	insertDL(t, "RJLIMA", "LimitHostOne", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJLIMB", "LimitHostTwo", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJLIM1", "LimitAttachOne", "RG1", "3", "ADV", "", editionsJSON("RJLIMA", "RJLIM1"))
	insertDL(t, "RJLIM2", "LimitAttachTwo", "RG1", "3", "ADV", "", editionsJSON("RJLIMB", "RJLIM2"))
	insertDL(t, "RJMINTA", "LimitMintAlphaGame", "RG1", "3", "ADV", "2020-01-01", "{}")
	insertDL(t, "RJMINTB", "LimitMintBetaGame", "RG1", "3", "ADV", "2020-02-01", "{}")
	insertDL(t, "RJMINTC", "LimitMintGammaGame", "RG1", "3", "ADV", "2020-03-01", "{}")
	st := runDLGames(t, false, 1, "")
	assert.Equal(t, 2, st.EditionGroups)
	assert.Equal(t, w1, workOfSKU(t, "RJLIM1"))
	assert.Equal(t, w2, workOfSKU(t, "RJLIM2"))
	assert.Equal(t, 1, st.MintedGroups)
	assert.Equal(t, 2, st.LimitedGroups)
	minted := scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type = 6 AND source_id = 4 AND external_id IN ('RJMINTA','RJMINTB','RJMINTC')`)
	assert.Equal(t, int64(1), minted)
}

func TestDryRunWritesNothing(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJDRY001", "DryRunOnlyGameTitle", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, true, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.Written)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE display_name = 'DryRunOnlyGameTitle'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_release`))
}

func TestSecondRunPlansNothing(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJSEC001", "SecondRunGameTitle", "RG1", "3", "ADV", "", "{}")
	st1 := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st1.MintedGroups)
	st2 := runDLGames(t, false, 0, "")
	assert.Zero(t, st2.Population)
	assert.Zero(t, st2.MintedGroups)
	assert.Zero(t, st2.ReleasesPlanned)
}
