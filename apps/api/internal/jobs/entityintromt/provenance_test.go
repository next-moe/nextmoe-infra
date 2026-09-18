package entityintromt

import (
	"context"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func labelLane(t *testing.T) laneDef {
	t.Helper()
	l, err := selectLanes(LaneLabel)
	require.NoError(t, err)
	return l[0]
}

func curatedSourceID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func labelIntroRow(t *testing.T, labelID int64, lang string) (model.CatalogLabelIntro, bool) {
	t.Helper()
	var rows []model.CatalogLabelIntro
	require.NoError(t, testDB.Where("label_id = ? AND lang = ?", labelID, lang).Find(&rows).Error)
	if len(rows) == 0 {
		return model.CatalogLabelIntro{}, false
	}
	require.Len(t, rows, 1)
	return rows[0], true
}

func snapshotIntroLangs(t *testing.T, e *editing.Engine, labelID int64) []string {
	t.Helper()
	snap, err := e.CurrentSnapshot(context.Background(), editspec.TypeLabel, labelID)
	require.NoError(t, err)
	raw, ok := snap[editspec.FieldLabelIntros].([]any)
	require.True(t, ok, "snapshot intros: %#v", snap[editspec.FieldLabelIntros])
	out := make([]string, 0, len(raw))
	for _, el := range raw {
		out = append(out, el.(map[string]any)["lang"].(string))
	}
	return out
}

func editLabelIntros(t *testing.T, e *editing.Engine, labelID int64, intros ...[2]string) {
	t.Helper()
	value := make([]any, 0, len(intros))
	for _, in := range intros {
		value = append(value, map[string]any{"lang": in[0], "intro": in[1]})
	}
	actor := func(uid int64, role string) editing.PolicyContext {
		return editing.PolicyContext{
			UserID: uid, Site: "kungal",
			HasPerm: func(key string) bool { return perm.Resolver.Can([]string{role}, authz.Permission(key)) },
		}
	}
	ctx := context.Background()
	prop, _, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeLabel, EntityID: labelID,
		Patch: map[string]any{editspec.FieldLabelIntros: value}, Actor: actor(100, "admin"),
	})
	require.NoError(t, err)
	_, err = e.MergeProposal(ctx, prop.ID, actor(200, "ren"), "")
	require.NoError(t, err)
}

func TestLabelIntrosSurviveTheMachineTranslator(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()
	curated := curatedSourceID(t)

	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterTaxonomy(reg, testDB))
	e := editing.NewEngine(testDB, reg)

	label := mkLabel(t, "ブランド")
	editLabelIntros(t, e, label,
		[2]string{"ja", "老舗ブランドです。"},
		[2]string{"zh-Hans", "人工写的中文简介。"})

	r := &runner{db: testDB, lane: labelLane(t), stats: &LaneStats{Lane: LaneLabel}}
	rows, _, err := r.writeMachine(ctx,
		candidate{EntityID: label, SourceID: curated, Text: "老舗ブランドです。"},
		"机器译文", hashSource("老舗ブランドです。"), "mock:stub")
	require.NoError(t, err)
	assert.Zero(t, rows, "the DO UPDATE guard must refuse a provenance=0 row on the curated lane")

	kept, ok := labelIntroRow(t, label, "zh-Hans")
	require.True(t, ok)
	assert.Equal(t, "人工写的中文简介。", kept.Intro)
	assert.EqualValues(t, model.IntroProvenanceSource, kept.Provenance)
	assert.Empty(t, kept.MTModel)

	require.NoError(t, testDB.Create(&model.CatalogLabelIntro{
		LabelID: label, Lang: "zh-Hant", Intro: "機器譯文。", SourceID: curated,
		Provenance: model.IntroProvenanceMachine, SrcHash: "deadbeef", MTModel: "mock:stub",
	}).Error)
	assert.Equal(t, []string{"ja", "zh-Hans"}, snapshotIntroLangs(t, e, label),
		"a machine row on the curated lane must not come back as a human intro")

	editLabelIntros(t, e, label, [2]string{"ja", "老舗ブランドです。改"})
	mt, ok := labelIntroRow(t, label, "zh-Hant")
	require.True(t, ok, "a save that does not claim zh-Hant leaves the machine row alone")
	assert.EqualValues(t, model.IntroProvenanceMachine, mt.Provenance)
	_, ok = labelIntroRow(t, label, "zh-Hans")
	assert.False(t, ok, "the human zh-Hans row is replaced by the new curated set")

	editLabelIntros(t, e, label,
		[2]string{"ja", "老舗ブランドです。改"},
		[2]string{"zh-Hant", "人工寫的繁體簡介。"})
	taken, ok := labelIntroRow(t, label, "zh-Hant")
	require.True(t, ok)
	assert.Equal(t, "人工寫的繁體簡介。", taken.Intro, "writing a lang takes it over from the translator")
	assert.EqualValues(t, model.IntroProvenanceSource, taken.Provenance)
	assert.Empty(t, taken.MTModel)
}
