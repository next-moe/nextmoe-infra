package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBangumiDeclaredWorknoAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "DeclaredHostGame")
	insertBgmInfobox(t, 4101, "store https://www.dlsite.com/maniax/work/=/product_id/RJ410100.html", "")
	seedBgmExact(t, w, 4101)
	insertDL(t, "RJ410100", "DeclaredProductGame", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.DeclaredGroups)
	assert.Equal(t, w, workOfSKU(t, "RJ410100"))
	assert.Equal(t, ruleDLsiteBgmXlink, matchedBySKU(t, "RJ410100"))
}

func TestDeclaredTokenIsWholeNotPrefix(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "PrefixHostGameXX")
	insertBgmInfobox(t, 4102, "https://www.dlsite.com/maniax/work/=/product_id/RJ01225370.html", "")
	seedBgmExact(t, w, 4102)
	insertDL(t, "RJ012253", "PrefixProductGameYY", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Zero(t, st.DeclaredGroups)
	assert.NotEqual(t, w, workOfSKU(t, "RJ012253"))
	assert.Equal(t, ruleDLsiteGameImport, matchedBySKU(t, "RJ012253"))
}

func TestDeclaredByTwoWorksIsSplit(t *testing.T) {
	requireDLGamesDB(t)
	w1 := seedLiveWork(t, "DeclaredSplitOne")
	w2 := seedLiveWork(t, "DeclaredSplitTwo")
	insertBgmInfobox(t, 4103, "RJ410300", "")
	insertBgmInfobox(t, 4104, "see RJ410300 too", "")
	seedBgmExact(t, w1, 4103)
	seedBgmExact(t, w2, 4104)
	insertDL(t, "RJ410300", "DeclaredSplitProduct", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.SplitGroups)
	assert.Zero(t, st.DeclaredGroups)
	assert.Zero(t, workOfSKU(t, "RJ410300"))
}

func TestDeadOrProbableBangumiRefDeclaresNothing(t *testing.T) {
	requireDLGamesDB(t)
	wProb := seedLiveWork(t, "ProbableDeclareHost")
	wDead := seedLiveWork(t, "DeadDeclareHost")
	insertBgmInfobox(t, 4105, "RJ410500", "")
	insertBgmInfobox(t, 4106, "RJ410600", "")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, '4105', 1, 'rule:probable-guess')`, wProb).Error)
	seedBgmExact(t, wDead, 4106)
	require.NoError(t, testDB.Exec(`UPDATE catalog_external_ref SET dead_at = now() WHERE entity_id = ? AND source_id = 3 AND external_id = '4106'`, wDead).Error)
	insertDL(t, "RJ410500", "ProbableProductGame", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJ410600", "DeadProductGameXX", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Zero(t, st.DeclaredGroups)
	assert.NotEqual(t, wProb, workOfSKU(t, "RJ410500"))
	assert.NotEqual(t, wDead, workOfSKU(t, "RJ410600"))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOfSKU(t, "RJ410500")))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOfSKU(t, "RJ410600")))
}
