package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dlMakerLabelKind(t *testing.T, maker string) int16 {
	t.Helper()
	var kinds []int16
	require.NoError(t, testDB.Raw(`SELECT l.kind FROM catalog_label l
		JOIN catalog_external_ref r ON r.entity_type = 3 AND r.entity_id = l.id AND r.source_id = 4
		WHERE r.external_id = ?`, maker).Scan(&kinds).Error)
	require.Len(t, kinds, 1, "one label anchored to %s", maker)
	return kinds[0]
}

func dlMakerEdgeKinds(t *testing.T, maker string) []int16 {
	t.Helper()
	var kinds []int16
	require.NoError(t, testDB.Raw(`SELECT wl.kind FROM catalog_work_label wl
		JOIN catalog_external_ref r ON r.entity_type = 3 AND r.entity_id = wl.label_id AND r.source_id = 4
		WHERE r.external_id = ? ORDER BY wl.work_id, wl.kind`, maker).Scan(&kinds).Error)
	return kinds
}

func TestDLsiteHouseMakerKind(t *testing.T) {
	assert.Equal(t, model.LabelKindPublisher, dlLabelKind("RG01001"))
	assert.Equal(t, model.LabelKindPublisher, egdlLabelKind("RG01001"))
	assert.Equal(t, model.LabelKindDoujinCircle, dlLabelKind("RG01002"))
	assert.Equal(t, model.LabelKindDoujinCircle, egdlLabelKind("RG01002"))
	assert.Equal(t, model.LabelKindPublisher, dlLabelKind("BG200"))
	assert.Equal(t, model.LabelKindPublisher, egdlLabelKind("BG200"))
	assert.Equal(t, model.LabelKindPublisher, egdlLabelKind("VG200"))
}

func TestDLsiteGamesHouseMakerLandsAsPublisher(t *testing.T) {
	requireDLGamesDB(t)
	insertDLFull(t, "RJHG001", "HouseBrandPortGame", "", "RG01001", "DLsite", "3", "ADV", "", "{}")
	insertDLFull(t, "RJCG001", "OrdinaryCircleGame", "", "RGCIRC1", "サークルY", "3", "ADV", "", "{}")

	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 2, st.MintedGroups)
	assert.Equal(t, model.LabelKindPublisher, dlMakerLabelKind(t, "RG01001"))
	assert.Equal(t, []int16{model.WorkLabelKindPublisher}, dlMakerEdgeKinds(t, "RG01001"))
	assert.Equal(t, model.LabelKindDoujinCircle, dlMakerLabelKind(t, "RGCIRC1"))
	assert.Equal(t, []int16{model.WorkLabelKindCircle}, dlMakerEdgeKinds(t, "RGCIRC1"))
}

func TestEGDLsiteHouseMakerLandsAsPublisher(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES (910,'RJHE001'), (920,'RJCE001')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJHE001','移植ゲーム','RG01001','DLsite','3','アドベンチャー','fetched','{}'::jsonb),
		('RJCE001','同人ゲーム','RG300','サークルZ','3','アドベンチャー','fetched','{}'::jsonb)`).Error)

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Minted)
	assert.Equal(t, model.LabelKindPublisher, dlMakerLabelKind(t, "RG01001"))
	assert.Equal(t, []int16{model.WorkLabelKindPublisher}, dlMakerEdgeKinds(t, "RG01001"))
	assert.Equal(t, model.LabelKindDoujinCircle, dlMakerLabelKind(t, "RG300"))
	assert.Equal(t, []int16{model.WorkLabelKindCircle}, dlMakerEdgeKinds(t, "RG300"))
}
